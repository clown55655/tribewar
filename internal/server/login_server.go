package server

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"tribeway/internal/actor"
	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// LoginServer 登录服务器
type LoginServer struct {
	*BaseServer
	userRepo    *database.UserRepository
	userCache   *database.UserCache
	tokenSecret []byte
	tokenExpiry time.Duration
}

// NewLoginServer 创建登录服务器
func NewLoginServer(configFile, nodeID string) *LoginServer {
	loginServer, err := NewLoginServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create login server: %v", err))
	}
	return loginServer
}

func NewLoginServerWithError(configFile, nodeID string) (*LoginServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "login", nodeID, LoginComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

	loginServer := &LoginServer{
		BaseServer:  baseServer,
		userRepo:    database.NewUserRepository(baseServer.mongoManager),
		userCache:   database.NewUserCache(baseServer.redisManager),
		tokenExpiry: baseServer.authTokenExpiry(),
	}
	tokenSecret, err := baseServer.authTokenSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to load token secret: %v", err)
	}
	loginServer.tokenSecret = tokenSecret

	if err := RegisterCommonServices(baseServer); err != nil {
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	loginService := NewLoginService(loginServer)
	if err := baseServer.rpcServer.RegisterService(loginService); err != nil {
		return nil, fmt.Errorf("failed to register login service: %v", err)
	}

	loginActor := NewLoginActor(loginServer)
	if err := baseServer.actorSystem.SpawnActor(loginActor); err != nil {
		return nil, fmt.Errorf("failed to spawn login actor: %v", err)
	}

	constructed = true
	return loginServer, nil
}

// LoginService 登录RPC服务
type LoginService struct {
	server *LoginServer
}

// NewLoginService 创建登录服务
func NewLoginService(server *LoginServer) *LoginService {
	return &LoginService{
		server: server,
	}
}

// GetName 获取服务名称
func (ls *LoginService) GetName() string {
	return "LoginService"
}

// RegisterMethods 注册方法
func (ls *LoginService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["Login"] = reflect.ValueOf(ls.Login)
	methods["Register"] = reflect.ValueOf(ls.Register)
	methods["Logout"] = reflect.ValueOf(ls.Logout)
	methods["ValidateToken"] = reflect.ValueOf(ls.ValidateToken)
	methods["RefreshToken"] = reflect.ValueOf(ls.RefreshToken)

	return methods
}

// Login 用户登录
func (ls *LoginService) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	logger.Info(fmt.Sprintf("User login attempt: %s", req.Username))

	// 验证用户名和密码
	user, err := ls.server.userRepo.GetByUsernameContext(ctx, req.Username)
	if err != nil {
		logger.Warn(fmt.Sprintf("User not found: %s", req.Username))
		return nil, fmt.Errorf("invalid username or password")
	}

	// 验证密码
	passwordOK, needsRehash := ls.verifyPassword(req.Password, user.Password)
	if !passwordOK {
		logger.Warn(fmt.Sprintf("Password verification failed for user: %s", req.Username))
		return nil, fmt.Errorf("invalid username or password")
	}
	if needsRehash {
		if passwordHash, hashErr := ls.hashPassword(req.Password); hashErr != nil {
			logger.Warn(fmt.Sprintf("Failed to upgrade password hash for user %s: %v", req.Username, hashErr))
		} else if updateErr := ls.server.userRepo.UpdateFieldsContext(ctx, user.UserID, map[string]interface{}{"password": passwordHash}); updateErr != nil {
			logger.Warn(fmt.Sprintf("Failed to save upgraded password hash for user %s: %v", req.Username, updateErr))
		} else {
			user.Password = passwordHash
		}
	}

	// 检查用户状态
	if user.Status != 0 {
		logger.Warn(fmt.Sprintf("User is banned: %s", req.Username))
		return nil, fmt.Errorf("user is banned")
	}

	// 生成登录令牌
	token := ls.generateToken(user.UserID)

	// 更新用户登录信息
	err = ls.server.userRepo.UpdateFieldsContext(ctx, user.UserID, map[string]interface{}{
		"last_login_at": time.Now(),
		"last_login_ip": "0.0.0.0", // 实际应该从请求中获取
	})
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update user login info: %v", err))
	}

	// 缓存用户信息
	if err := ls.server.userCache.SetUserInfoContext(ctx, user.UserID, user); err != nil {
		logger.Warn(fmt.Sprintf("Failed to cache user info for %d: %v", user.UserID, err))
	}

	// 设置用户会话
	sessionCache := database.NewSessionCache(ls.server.redisManager)
	if err := sessionCache.SetSessionContext(ctx, token, user.UserID); err != nil {
		logger.Error(fmt.Sprintf("Failed to set session for user %d: %v", user.UserID, err))
		return nil, fmt.Errorf("failed to create session")
	}

	logger.Info(fmt.Sprintf("User login successful: %s (ID: %d)", req.Username, user.UserID))

	return &proto.LoginResponse{
		UserId:   user.UserID,
		Token:    token,
		Nickname: user.Nickname,
		Level:    user.Level,
		Exp:      user.Experience,
		Gold:     user.Gold,
		Diamond:  user.Diamond,
	}, nil
}

// Register 用户注册
func (ls *LoginService) Register(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	logger.Info(fmt.Sprintf("User registration attempt: %s", req.Username))

	// 检查用户名是否已存在
	existingUser, _ := ls.server.userRepo.GetByUsernameContext(ctx, req.Username)
	if existingUser != nil {
		return nil, fmt.Errorf("username already exists")
	}

	// 生成用户ID
	userID := uint64(time.Now().UnixNano())
	passwordHash, err := ls.hashPassword(req.Password)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to hash password: %v", err))
		return nil, fmt.Errorf("failed to create user")
	}

	// 创建新用户
	newUser := &database.User{
		UserID:      userID,
		Username:    req.Username,
		Password:    passwordHash,
		Nickname:    req.Username, // 默认昵称为用户名
		Level:       1,
		Experience:  0,
		Gold:        1000, // 初始金币
		Diamond:     100,  // 初始钻石
		Status:      0,    // 正常状态
		LastLoginAt: time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// 保存到数据库
	if err := ls.server.userRepo.CreateContext(ctx, newUser); err != nil {
		logger.Error(fmt.Sprintf("Failed to create user: %v", err))
		return nil, fmt.Errorf("failed to create user")
	}

	// 生成登录令牌
	token := ls.generateToken(userID)

	// 缓存用户信息
	if err := ls.server.userCache.SetUserInfoContext(ctx, userID, newUser); err != nil {
		logger.Warn(fmt.Sprintf("Failed to cache user info for %d: %v", userID, err))
	}

	// 设置用户会话
	sessionCache := database.NewSessionCache(ls.server.redisManager)
	if err := sessionCache.SetSessionContext(ctx, token, userID); err != nil {
		logger.Error(fmt.Sprintf("Failed to set session for user %d: %v", userID, err))
		return nil, fmt.Errorf("failed to create session")
	}

	logger.Info(fmt.Sprintf("User registration successful: %s (ID: %d)", req.Username, userID))

	return &proto.LoginResponse{
		UserId:   userID,
		Token:    token,
		Nickname: newUser.Nickname,
		Level:    newUser.Level,
		Exp:      newUser.Experience,
		Gold:     newUser.Gold,
		Diamond:  newUser.Diamond,
	}, nil
}

// Logout 用户登出
func (ls *LoginService) Logout(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	userID := req.Header.UserId

	if userID == 0 {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 清理会话
	sessionID := req.Header.SessionId
	if sessionID != "" {
		sessionCache := database.NewSessionCache(ls.server.redisManager)
		if err := sessionCache.DeleteSessionContext(ctx, sessionID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to delete session for user %d: %v", userID, err))
		}
	}

	// 设置用户离线
	ls.server.userCache.SetUserOffline(userID)

	logger.Info(fmt.Sprintf("User logout: %d", userID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "logout success",
	}, nil
}

// ValidateToken 验证令牌
func (ls *LoginService) ValidateToken(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	sessionID := req.Header.SessionId
	if sessionID == "" {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "missing session id",
		}, nil
	}

	claims, err := ls.validateToken(sessionID)
	if err != nil {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid token",
		}, nil
	}

	// 验证会话
	sessionCache := database.NewSessionCache(ls.server.redisManager)
	userID, err := sessionCache.GetSessionContext(ctx, sessionID)
	if err != nil {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid session",
		}, nil
	}
	if userID != claims.userID {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "session mismatch",
		}, nil
	}

	// 刷新会话
	if err := sessionCache.RefreshSessionContext(ctx, sessionID); err != nil {
		logger.Warn(fmt.Sprintf("Failed to refresh session for user %d: %v", userID, err))
	}

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "token valid",
		Data:   []byte(fmt.Sprintf(`{"user_id":%d}`, userID)),
	}, nil
}

// RefreshToken 刷新令牌
func (ls *LoginService) RefreshToken(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	userID := req.Header.UserId

	if userID == 0 {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}
	oldSessionID := req.Header.SessionId
	claims, err := ls.validateToken(oldSessionID)
	if oldSessionID == "" || err != nil || claims.userID != userID {
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid token",
		}, nil
	}

	// 生成新令牌
	newToken := ls.generateToken(userID)

	// 删除旧会话
	if oldSessionID != "" {
		sessionCache := database.NewSessionCache(ls.server.redisManager)
		if err := sessionCache.DeleteSessionContext(ctx, oldSessionID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to delete old session for user %d: %v", userID, err))
		}
	}

	// 创建新会话
	sessionCache := database.NewSessionCache(ls.server.redisManager)
	if err := sessionCache.SetSessionContext(ctx, newToken, userID); err != nil {
		logger.Error(fmt.Sprintf("Failed to set refreshed session for user %d: %v", userID, err))
		return nil, fmt.Errorf("failed to create session")
	}

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "token refreshed",
		Data:   []byte(fmt.Sprintf(`{"token":"%s"}`, newToken)),
	}, nil
}

// hashPassword 哈希密码
func (ls *LoginService) hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword 验证密码
func (ls *LoginService) verifyPassword(plainPassword, hashedPassword string) (bool, bool) {
	if isBcryptHash(hashedPassword) {
		return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword)) == nil, false
	}

	return ls.legacyMD5PasswordHash(plainPassword) == hashedPassword, true
}

func isBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") ||
		strings.HasPrefix(hash, "$2b$") ||
		strings.HasPrefix(hash, "$2y$")
}

func (ls *LoginService) legacyMD5PasswordHash(password string) string {
	hash := md5.Sum([]byte(password + "tribeway_game_salt"))
	return fmt.Sprintf("%x", hash)
}

// generateToken 生成令牌
func (ls *LoginService) generateToken(userID uint64) string {
	now := time.Now()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		nonce = []byte(strconv.FormatInt(now.UnixNano(), 10))
	}

	payload := fmt.Sprintf("%d.%d.%d.%s", userID, now.Unix(), now.Add(ls.server.tokenExpiry).Unix(), hex.EncodeToString(nonce))
	mac := hmac.New(sha256.New, ls.server.tokenSecret)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%s", payload, signature)
}

type loginTokenClaims struct {
	userID    uint64
	issuedAt  int64
	expiresAt int64
}

func (ls *LoginService) validateToken(token string) (*loginTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid token format")
	}

	payload := strings.Join(parts[:4], ".")
	mac := hmac.New(sha256.New, ls.server.tokenSecret)
	mac.Write([]byte(payload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[4])) {
		return nil, fmt.Errorf("invalid token signature")
	}

	userID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid token user id: %w", err)
	}
	issuedAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid token issued at: %w", err)
	}
	expiresAt, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid token expires at: %w", err)
	}
	if time.Now().Unix() > expiresAt {
		return nil, fmt.Errorf("token expired")
	}

	return &loginTokenClaims{
		userID:    userID,
		issuedAt:  issuedAt,
		expiresAt: expiresAt,
	}, nil
}

// LoginActor 登录Actor
type LoginActor struct {
	*actor.BaseActor
	server *LoginServer
}

// NewLoginActor 创建登录Actor
func NewLoginActor(server *LoginServer) *LoginActor {
	baseActor := actor.NewBaseActor("login_actor", "login", 1000)

	return &LoginActor{
		BaseActor: baseActor,
		server:    server,
	}
}

// OnReceive 处理消息
func (la *LoginActor) OnReceive(ctx context.Context, msg actor.Message) error {
	switch msg.GetType() {
	case actor.MSG_TYPE_USER_LOGIN:
		return la.handleUserLogin(msg)
	case actor.MSG_TYPE_USER_LOGOUT:
		return la.handleUserLogout(msg)
	default:
		logger.Debug(fmt.Sprintf("Unknown message type: %s", msg.GetType()))
	}

	return nil
}

// OnStart 启动时处理
func (la *LoginActor) OnStart(ctx context.Context) error {
	logger.Info("Login actor started")
	return nil
}

// OnStop 停止时处理
func (la *LoginActor) OnStop(ctx context.Context) error {
	logger.Info("Login actor stopped")
	return nil
}

// handleUserLogin 处理用户登录
func (la *LoginActor) handleUserLogin(msg actor.Message) error {
	logger.Debug("Handling user login in login actor")
	// 可以在这里处理登录相关的异步逻辑
	// 比如记录登录日志、更新统计信息等
	return nil
}

// handleUserLogout 处理用户登出
func (la *LoginActor) handleUserLogout(msg actor.Message) error {
	logger.Debug("Handling user logout in login actor")
	// 处理登出相关的异步逻辑
	return nil
}
