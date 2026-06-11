package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"reflect"
	"strings"
	"time"

	"tribeway/internal/gameplay"
	"tribeway/internal/hotreload"
	"tribeway/internal/i18n"
	"tribeway/internal/logger"
	"tribeway/internal/monitoring"
	"tribeway/internal/security"
	"tribeway/pkg/proto"
)

// EnhancedGameServer 增强版游戏服务器
type EnhancedGameServer struct {
	*BaseServer
	gameplay    *gameplay.GameplayManager
	security    *security.SecurityManager
	monitoring  *monitoring.MonitoringManager
	i18n        *i18n.I18nManager
	hotReload   *hotreload.HotReloadManager
	pprofServer *http.Server
}

// NewEnhancedGameServer 创建增强版游戏服务器
func NewEnhancedGameServer(configFile, nodeID string) *EnhancedGameServer {
	enhancedServer, err := NewEnhancedGameServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create enhanced game server: %v", err))
	}
	return enhancedServer
}

func NewEnhancedGameServerWithError(configFile, nodeID string) (*EnhancedGameServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "game", nodeID, EnhancedGameComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

	enhancedServer := &EnhancedGameServer{
		BaseServer: baseServer,
	}

	if err := enhancedServer.initEnhancedComponents(); err != nil {
		return nil, fmt.Errorf("failed to init enhanced components: %v", err)
	}

	if err := RegisterCommonServices(baseServer); err != nil {
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	enhancedGameService := NewEnhancedGameService(enhancedServer)
	if err := baseServer.rpcServer.RegisterService(enhancedGameService); err != nil {
		return nil, fmt.Errorf("failed to register enhanced game service: %v", err)
	}

	constructed = true
	return enhancedServer, nil
}

// initEnhancedComponents 初始化增强组件
func (egs *EnhancedGameServer) initEnhancedComponents() error {
	var err error

	// 初始化安全管理器
	secrets := egs.config.Security.Secrets
	if secrets.EncryptionKeyEnv == "" {
		secrets.EncryptionKeyEnv = "TRIBEWAY_ENCRYPTION_KEY"
	}
	if secrets.JWTSecretEnv == "" {
		secrets.JWTSecretEnv = "TRIBEWAY_JWT_SECRET"
	}
	egs.security, err = security.NewSecurityManagerFromEnv(secrets.EncryptionKeyEnv, secrets.JWTSecretEnv)
	if err != nil {
		return fmt.Errorf("failed to init security manager: %v", err)
	}

	// 初始化监控管理器
	monitoringPort := egs.config.Network.HTTPPort
	monitoringOptions := monitoring.MonitoringOptions{
		BindAddress:            egs.config.Security.Monitoring.BindAddress,
		AdminTokenEnv:          egs.config.Security.Monitoring.AdminTokenEnv,
		AllowedCIDRs:           egs.config.Security.Monitoring.AllowedCIDRs,
		ProtectMetricsEndpoint: egs.config.Security.Monitoring.ProtectMetricsEndpoint,
	}
	egs.monitoring, err = monitoring.NewMonitoringManagerWithOptions(egs.nodeID, egs.nodeType, monitoringPort, monitoringOptions)
	if err != nil {
		return fmt.Errorf("failed to init monitoring manager: %v", err)
	}

	// 初始化国际化管理器
	egs.i18n = i18n.NewI18nManager("en")
	if err := egs.i18n.LoadLanguage("zh-CN"); err != nil {
		logger.Warn(fmt.Sprintf("Failed to load Chinese language: %v", err))
	}
	if err := egs.i18n.LoadLanguage("ja"); err != nil {
		logger.Warn(fmt.Sprintf("Failed to load Japanese language: %v", err))
	}

	// 初始化玩法管理器
	egs.gameplay = gameplay.NewGameplayManager()

	// 注册默认游戏模块
	cardGameModule := gameplay.NewCardGameModule()
	if err := egs.gameplay.RegisterModule(cardGameModule); err != nil {
		logger.Warn(fmt.Sprintf("Failed to register card game module: %v", err))
	}

	// 初始化热更新管理器
	egs.hotReload, err = hotreload.NewHotReloadManager()
	if err != nil {
		return fmt.Errorf("failed to init hot reload manager: %v", err)
	}

	// 注册配置文件热更新
	configParser := &hotreload.YAMLConfigParser{}
	if err := egs.hotReload.RegisterConfig("config/config.yaml", configParser); err != nil {
		logger.Warn(fmt.Sprintf("Failed to register config hot reload: %v", err))
	}

	// 启动pprof服务器
	egs.startPprofServer()

	logger.Info("Enhanced components initialized")
	return nil
}

// startPprofServer 启动pprof服务器
func (egs *EnhancedGameServer) startPprofServer() {
	pprofPort := egs.config.Network.HTTPPort + 1000
	bindAddress := egs.config.Security.Monitoring.BindAddress
	if bindAddress == "" {
		bindAddress = "127.0.0.1"
	}
	mux := http.NewServeMux()
	mux.Handle("/", egs.pprofAuthMiddleware(http.DefaultServeMux))

	egs.pprofServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", bindAddress, pprofPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info(fmt.Sprintf("pprof server listening on %s", egs.pprofServer.Addr))
		if err := egs.pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(fmt.Sprintf("pprof server error: %v", err))
		}
	}()
}

func (egs *EnhancedGameServer) pprofAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !egs.monitoringSourceAllowed(r.RemoteAddr) {
			http.Error(w, "source ip is not allowed", http.StatusForbidden)
			return
		}

		envName := egs.config.Security.Monitoring.AdminTokenEnv
		if envName == "" {
			envName = "TRIBEWAY_MONITORING_ADMIN_TOKEN"
		}
		expected := os.Getenv(envName)
		if expected == "" {
			http.Error(w, "monitoring admin token is not configured", http.StatusServiceUnavailable)
			return
		}

		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			http.Error(w, "invalid admin token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (egs *EnhancedGameServer) monitoringSourceAllowed(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	allowedCIDRs := egs.config.Security.Monitoring.AllowedCIDRs
	if len(allowedCIDRs) == 0 {
		allowedCIDRs = []string{"127.0.0.1/32", "::1/128"}
	}
	for _, cidr := range allowedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Warn(fmt.Sprintf("Invalid monitoring allowed CIDR ignored: %s", cidr))
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Start 启动增强版游戏服务器
func (egs *EnhancedGameServer) Start() error {
	// 启动基础服务器
	if err := egs.BaseServer.Start(); err != nil {
		return err
	}

	// 启动监控服务
	if err := egs.monitoring.Start(); err != nil {
		logger.Error(fmt.Sprintf("Failed to start monitoring: %v", err))
	}

	logger.Info(fmt.Sprintf("Enhanced game server %s started", egs.nodeID))
	return nil
}

// Stop 停止增强版游戏服务器
func (egs *EnhancedGameServer) Stop() error {
	// 停止监控服务
	if egs.monitoring != nil {
		egs.monitoring.Stop()
	}

	// 停止pprof服务器
	if egs.pprofServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		egs.pprofServer.Shutdown(ctx)
		cancel()
	}

	// 停止热更新管理器
	if egs.hotReload != nil {
		egs.hotReload.Close()
	}

	// 停止基础服务器
	return egs.BaseServer.Stop()
}

// EnhancedGameService 增强游戏RPC服务
type EnhancedGameService struct {
	server *EnhancedGameServer
}

// NewEnhancedGameService 创建增强游戏服务
func NewEnhancedGameService(server *EnhancedGameServer) *EnhancedGameService {
	return &EnhancedGameService{
		server: server,
	}
}

// GetName 获取服务名称
func (egs *EnhancedGameService) GetName() string {
	return "EnhancedGameService"
}

// RegisterMethods 注册方法
func (egs *EnhancedGameService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	// 基础游戏方法
	methods["CreateRoom"] = reflect.ValueOf(egs.CreateRoom)
	methods["JoinRoom"] = reflect.ValueOf(egs.JoinRoom)
	methods["LeaveRoom"] = reflect.ValueOf(egs.LeaveRoom)
	methods["GameAction"] = reflect.ValueOf(egs.GameAction)
	methods["GetRoomState"] = reflect.ValueOf(egs.GetRoomState)

	// 安全相关方法
	methods["ValidateToken"] = reflect.ValueOf(egs.ValidateToken)
	methods["CheckSecurity"] = reflect.ValueOf(egs.CheckSecurity)

	// 监控相关方法
	methods["GetMetrics"] = reflect.ValueOf(egs.GetMetrics)
	methods["GetAlerts"] = reflect.ValueOf(egs.GetAlerts)

	// 热更新方法
	methods["HotReload"] = reflect.ValueOf(egs.HotReload)

	return methods
}

// CreateRoom 创建游戏房间
func (egs *EnhancedGameService) CreateRoom(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 安全验证
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	// 限流检查
	if err := egs.server.security.CheckIPSecurity(session.IP); err != nil {
		return egs.createErrorResponse(req, -2, "rate_limit_exceeded", nil)
	}

	// 创建房间配置
	config := &gameplay.RoomConfig{
		MaxPlayers: 2,
		MinPlayers: 2,
		AutoStart:  true,
		TimeLimit:  30 * time.Minute,
	}

	// 创建房间
	room, err := egs.server.gameplay.CreateRoom("card_game", config)
	if err != nil {
		return egs.createErrorResponse(req, -3, "room_creation_failed", nil)
	}

	// 记录监控指标
	egs.server.monitoring.RecordMessage("create_room")

	// 返回本地化响应
	return egs.createSuccessResponse(req, "success.room_created", map[string]interface{}{
		"room_id": room.ID,
	})
}

// JoinRoom 加入房间
func (egs *EnhancedGameService) JoinRoom(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	// 解析请求参数
	params, err := egs.parseRequestParams(req)
	if err != nil {
		return egs.createErrorResponse(req, -2, "invalid_request_params", nil)
	}

	// 获取房间ID
	roomID, ok := params["room_id"].(float64)
	if !ok {
		return egs.createErrorResponse(req, -3, "missing_room_id", nil)
	}

	// 获取用户信息（这里简化处理）
	nickname := "Player"
	if userNickname, exists := params["nickname"].(string); exists {
		nickname = userNickname
	}

	// 创建玩家对象
	player := &gameplay.Player{
		UserID:   session.UserID,
		Nickname: nickname,
		Level:    1,
		Status:   gameplay.PlayerStatusWaiting,
	}

	// 加入房间
	if err := egs.server.gameplay.JoinRoom(uint64(roomID), player); err != nil {
		return egs.createErrorResponse(req, -4, "join_room_failed", nil)
	}

	egs.server.monitoring.RecordMessage("join_room")

	return egs.createSuccessResponse(req, "success.room_joined", map[string]interface{}{
		"room_id": uint64(roomID),
	})
}

// LeaveRoom 离开房间
func (egs *EnhancedGameService) LeaveRoom(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	// 解析请求参数
	params, err := egs.parseRequestParams(req)
	if err != nil {
		return egs.createErrorResponse(req, -2, "invalid_request_params", nil)
	}

	// 获取房间ID
	roomID, ok := params["room_id"].(float64)
	if !ok {
		return egs.createErrorResponse(req, -3, "missing_room_id", nil)
	}

	if err := egs.server.gameplay.LeaveRoom(uint64(roomID), session.UserID); err != nil {
		return egs.createErrorResponse(req, -4, "leave_room_failed", nil)
	}

	egs.server.monitoring.RecordMessage("leave_room")

	return egs.createSuccessResponse(req, "success.room_left", nil)
}

// GameAction 处理游戏操作
func (egs *EnhancedGameService) GameAction(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		egs.server.monitoring.RecordRequestDuration("POST", "/game_action", duration)
	}()

	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	// 解析请求参数
	params, err := egs.parseRequestParams(req)
	if err != nil {
		return egs.createErrorResponse(req, -2, "invalid_request_params", nil)
	}

	// 获取房间ID
	roomID, ok := params["room_id"].(float64)
	if !ok {
		return egs.createErrorResponse(req, -3, "missing_room_id", nil)
	}

	// 获取操作类型
	actionType, ok := params["action_type"].(string)
	if !ok {
		return egs.createErrorResponse(req, -4, "missing_action_type", nil)
	}

	// 反作弊检查 - 简化实现
	// TODO: 实现反作弊检查逻辑

	// 创建游戏操作对象
	action := &gameplay.GameAction{
		Type:      actionType,
		PlayerID:  session.UserID,
		Timestamp: time.Now(),
		Data:      params["action_data"],
	}

	result, err := egs.server.gameplay.ProcessAction(uint64(roomID), action)
	if err != nil {
		egs.server.monitoring.RecordError("game_action_failed")
		return egs.createErrorResponse(req, -6, "action_failed", nil)
	}

	egs.server.monitoring.RecordMessage("game_action")

	return egs.createSuccessResponse(req, "success.action_processed", map[string]interface{}{
		"result": result,
	})
}

// GetRoomState 获取房间状态
func (egs *EnhancedGameService) GetRoomState(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	// 解析请求参数
	params, err := egs.parseRequestParams(req)
	if err != nil {
		return egs.createErrorResponse(req, -2, "invalid_request_params", nil)
	}

	// 获取房间ID
	roomID, ok := params["room_id"].(float64)
	if !ok {
		return egs.createErrorResponse(req, -3, "missing_room_id", nil)
	}

	room, exists := egs.server.gameplay.GetRoom(uint64(roomID))
	if !exists {
		return egs.createErrorResponse(req, -4, "room_not_found", nil)
	}

	// 检查玩家权限
	if _, exists := room.GetPlayer(session.UserID); !exists {
		return egs.createErrorResponse(req, -5, "permission_denied", nil)
	}

	return egs.createSuccessResponse(req, "success", map[string]interface{}{
		"room_state": room,
	})
}

// ValidateToken 验证令牌
func (egs *EnhancedGameService) ValidateToken(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	tokenString := req.Header.SessionId
	if tokenString == "" {
		return egs.createErrorResponse(req, -1, "error.missing_token", nil)
	}

	// TODO: 检查认证状态
	// 简化实现：假设用户已认证
	logger.Debug(fmt.Sprintf("Checking authentication for token: %s", tokenString))

	return egs.createSuccessResponse(req, "success.token_valid", map[string]interface{}{
		"user_id": "dummy_user_id",
	})
}

// CheckSecurity 安全检查
func (egs *EnhancedGameService) CheckSecurity(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	metrics := egs.server.security.GetSecurityMetrics()

	return egs.createSuccessResponse(req, "success", metrics)
}

// GetMetrics 获取监控指标
func (egs *EnhancedGameService) GetMetrics(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证管理员权限
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	if !egs.hasPermission(session, "admin") {
		return egs.createErrorResponse(req, -2, "permission_denied", nil)
	}

	// 获取指标数据
	// 这里应该从监控系统获取指标
	metrics := map[string]interface{}{
		"node_id":   "enhanced_server",
		"node_type": "enhanced",
		"timestamp": time.Now().Unix(),
	}

	return egs.createSuccessResponse(req, "success", metrics)
}

// GetAlerts 获取告警信息
func (egs *EnhancedGameService) GetAlerts(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	if !egs.hasPermission(session, "admin") {
		return egs.createErrorResponse(req, -2, "permission_denied", nil)
	}

	// TODO: 从监控系统获取告警信息
	alerts := []interface{}{}

	return egs.createSuccessResponse(req, "success", map[string]interface{}{
		"alerts": alerts,
	})
}

// HotReload 热更新
func (egs *EnhancedGameService) HotReload(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	session, err := egs.validateRequest(req)
	if err != nil {
		return egs.createErrorResponse(req, -1, "security_validation_failed", nil)
	}

	if !egs.hasPermission(session, "admin") {
		return egs.createErrorResponse(req, -2, "permission_denied", nil)
	}

	// 解析请求参数
	params, err := egs.parseRequestParams(req)
	if err != nil {
		return egs.createErrorResponse(req, -3, "invalid_request_params", nil)
	}

	// 获取更新类型
	updateType, ok := params["update_type"].(string)
	if !ok {
		updateType = "config" // 默认配置更新
	}

	// 获取模块名称
	moduleName, ok := params["module_name"].(string)
	if !ok {
		moduleName = "game_config" // 默认游戏配置
	}

	// 执行热更新逻辑
	switch updateType {
	case "config":
		// TODO: 实现配置重载
		logger.Info(fmt.Sprintf("重载配置模块: %s", moduleName))
	case "script":
		// TODO: 实现脚本热重载
		logger.Info("Script hot reload requested")
	case "gameplay":
		// TODO: 实现模块热重载
		logger.Info("Module hot reload requested")
	default:
		return egs.createErrorResponse(req, -7, "unsupported_update_type", nil)
	}

	logger.Info(fmt.Sprintf("Hot reload completed: %s/%s by user %d",
		updateType, moduleName, session.UserID))

	return egs.createSuccessResponse(req, "success.hot_reload", map[string]interface{}{
		"update_type": updateType,
		"module_name": moduleName,
	})
}

// validateRequest 验证请求
func (egs *EnhancedGameService) validateRequest(req *proto.BaseRequest) (*security.Session, error) {
	// 验证会话
	sessionToken := req.Header.SessionId
	if sessionToken == "" {
		return nil, fmt.Errorf("missing session token")
	}

	// TODO: 验证会话
	// 简化实现：创建模拟会话
	session := &security.Session{
		UserID:      req.Header.UserId,
		Permissions: []string{"user"},
	}

	return session, nil
}

// hasPermission 检查权限
func (egs *EnhancedGameService) hasPermission(session *security.Session, permission string) bool {
	for _, perm := range session.Permissions {
		if perm == permission || perm == "admin" {
			return true
		}
	}
	return false
}

// createSuccessResponse 创建成功响应
func (egs *EnhancedGameService) createSuccessResponse(req *proto.BaseRequest, messageID string, data interface{}) (*proto.BaseResponse, error) {
	// 获取客户端语言
	langCode := egs.detectLanguage(req)

	// 本地化消息
	message := egs.server.i18n.Translate(langCode, messageID, nil)

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    message,
	}

	if data != nil {
		responseData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response data: %v", err)
		}
		response.Data = responseData
	}

	return response, nil
}

// createErrorResponse 创建错误响应
func (egs *EnhancedGameService) createErrorResponse(req *proto.BaseRequest, code int32, messageID string, data interface{}) (*proto.BaseResponse, error) {
	// 获取客户端语言
	langCode := egs.detectLanguage(req)

	// 本地化错误消息
	message := egs.server.i18n.Translate(langCode, messageID, nil)

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   code,
		Msg:    message,
	}

	if data != nil {
		responseData, err := json.Marshal(data)
		if err != nil {
			return response, nil // 忽略数据序列化错误
		}
		response.Data = responseData
	}

	// 记录错误指标
	egs.server.monitoring.RecordError(messageID)

	return response, nil
}

// detectLanguage 检测客户端语言
func (egs *EnhancedGameService) detectLanguage(req *proto.BaseRequest) string {
	// 可以从请求头或用户设置中获取语言偏好
	// 这里简化实现，返回默认语言
	return "en"
}

// SecurityMiddleware 安全中间件
type SecurityMiddleware struct {
	security *security.SecurityManager
}

// NewSecurityMiddleware 创建安全中间件
func NewSecurityMiddleware(security *security.SecurityManager) *SecurityMiddleware {
	return &SecurityMiddleware{
		security: security,
	}
}

// ValidateRequest 验证请求
func (sm *SecurityMiddleware) ValidateRequest(req *proto.BaseRequest, clientIP string) error {
	// IP安全检查
	if err := sm.security.CheckIPSecurity(clientIP); err != nil {
		return fmt.Errorf("IP security check failed: %v", err)
	}

	// TODO: 检查速率限制
	// 简化实现：暂时允许所有请求
	logger.Debug(fmt.Sprintf("Rate limit check for user: %d", req.Header.UserId))

	// 输入验证
	if err := sm.security.ValidateInput(req); err != nil {
		return fmt.Errorf("input validation failed: %v", err)
	}

	return nil
}

// parseRequestParams 解析请求参数
func (egs *EnhancedGameService) parseRequestParams(req *proto.BaseRequest) (map[string]interface{}, error) {
	if req.Data == nil || len(req.Data) == 0 {
		return make(map[string]interface{}), nil
	}

	var params map[string]interface{}
	if err := json.Unmarshal(req.Data, &params); err != nil {
		return nil, fmt.Errorf("failed to parse request data: %v", err)
	}

	// 输入验证和清理
	if err := egs.validateAndSanitizeParams(params); err != nil {
		return nil, fmt.Errorf("parameter validation failed: %v", err)
	}

	return params, nil
}

// validateAndSanitizeParams 验证和清理参数
func (egs *EnhancedGameService) validateAndSanitizeParams(params map[string]interface{}) error {
	// 检查参数数量限制
	if len(params) > 50 {
		return fmt.Errorf("too many parameters: %d", len(params))
	}

	// 验证每个参数
	for key, value := range params {
		// 检查键名长度
		if len(key) > 100 {
			return fmt.Errorf("parameter key too long: %s", key)
		}

		// 检查字符串值长度
		if str, ok := value.(string); ok {
			if len(str) > 10000 {
				return fmt.Errorf("parameter value too long for key: %s", key)
			}
			// 基本的XSS防护
			if egs.containsSuspiciousContent(str) {
				return fmt.Errorf("suspicious content detected in parameter: %s", key)
			}
		}

		// 检查数值范围
		if num, ok := value.(float64); ok {
			if num < -1e15 || num > 1e15 {
				return fmt.Errorf("numeric value out of range for key: %s", key)
			}
		}
	}

	return nil
}

// containsSuspiciousContent 检查是否包含可疑内容
func (egs *EnhancedGameService) containsSuspiciousContent(content string) bool {
	// 简单的XSS和SQL注入检测
	suspiciousPatterns := []string{
		"<script", "</script>", "javascript:", "onload=", "onerror=",
		"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "UNION",
		"--", "/*", "*/", "'", "\"",
	}

	contentLower := strings.ToLower(content)
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}
