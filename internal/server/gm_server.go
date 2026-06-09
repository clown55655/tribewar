package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// GMServer GM服务器
type GMServer struct {
	*BaseServer
	gmRepo   *database.GMRepository
	userRepo *database.UserRepository
}

// NewGMServer 创建GM服务器
func NewGMServer(configFile, nodeID string) *GMServer {
	baseServer, err := NewBaseServerWithOptions(configFile, "gm", nodeID, GMComponents())
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create base server: %v", err))
	}

	gmServer := &GMServer{
		BaseServer: baseServer,
		gmRepo:     database.NewGMRepository(baseServer.mongoManager),
		userRepo:   database.NewUserRepository(baseServer.mongoManager),
	}

	// 注册通用服务
	if err := RegisterCommonServices(baseServer); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register common services: %v", err))
	}

	// 注册GM服务
	gmService := NewGMService(gmServer)
	if err := baseServer.rpcServer.RegisterService(gmService); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register gm service: %v", err))
	}

	return gmServer
}

// GMService GM RPC服务
type GMService struct {
	server       *GMServer
	adminUserIDs map[uint64]struct{}
}

// NewGMService 创建GM服务
func NewGMService(server *GMServer) *GMService {
	adminUserIDs := loadGMAdminUserIDs(server.config.Security.GM.AdminUserIDs, server.config.Security.GM.AdminUserIDsEnv)
	if len(adminUserIDs) == 0 {
		logger.Warn("GM service has no admin user IDs configured; all GM operations will be denied")
	}
	return &GMService{
		server:       server,
		adminUserIDs: adminUserIDs,
	}
}

// GetName 获取服务名称
func (gs *GMService) GetName() string {
	return "GMService"
}

// RegisterMethods 注册方法
func (gs *GMService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["ExecuteCommand"] = reflect.ValueOf(gs.ExecuteCommand)
	methods["KickUser"] = reflect.ValueOf(gs.KickUser)
	methods["BanUser"] = reflect.ValueOf(gs.BanUser)
	methods["UnbanUser"] = reflect.ValueOf(gs.UnbanUser)
	methods["SendNotice"] = reflect.ValueOf(gs.SendNotice)
	methods["ReloadConfig"] = reflect.ValueOf(gs.ReloadConfig)

	return methods
}

// ExecuteCommand 执行GM命令
func loadGMAdminUserIDs(configured []uint64, envName string) map[uint64]struct{} {
	admins := make(map[uint64]struct{})
	for _, userID := range configured {
		if userID > 0 {
			admins[userID] = struct{}{}
		}
	}

	if envName == "" {
		envName = "TRIBEWAY_GM_ADMIN_USER_IDS"
	}
	for _, raw := range strings.Split(os.Getenv(envName), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		userID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || userID == 0 {
			logger.Warn(fmt.Sprintf("Ignoring invalid GM admin user id from %s: %s", envName, raw))
			continue
		}
		admins[userID] = struct{}{}
	}

	return admins
}

func (gs *GMService) authorizeGM(ctx context.Context) (uint64, *proto.CommonResponse) {
	gmID, ok := contextUserID(ctx)
	if !ok || gmID == 0 {
		return 0, &proto.CommonResponse{
			Code:    1001,
			Message: "user not authenticated",
		}
	}

	if _, allowed := gs.adminUserIDs[gmID]; !allowed {
		logger.Warn(fmt.Sprintf("GM permission denied for user %d", gmID))
		return 0, &proto.CommonResponse{
			Code:    1004,
			Message: "gm permission denied",
		}
	}

	return gmID, nil
}

func (gs *GMService) authorizeGMUser(gmID uint64) *proto.CommonResponse {
	if gmID == 0 {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "user not authenticated",
		}
	}
	if _, allowed := gs.adminUserIDs[gmID]; !allowed {
		logger.Warn(fmt.Sprintf("GM permission denied for user %d", gmID))
		return &proto.CommonResponse{
			Code:    1004,
			Message: "gm permission denied",
		}
	}
	return nil
}

func contextUserID(ctx context.Context) (uint64, bool) {
	value := ctx.Value("user_id")
	switch userID := value.(type) {
	case uint64:
		return userID, true
	case uint:
		return uint64(userID), true
	case int:
		if userID > 0 {
			return uint64(userID), true
		}
	case int64:
		if userID > 0 {
			return uint64(userID), true
		}
	case string:
		parsed, err := strconv.ParseUint(userID, 10, 64)
		return parsed, err == nil
	}
	return 0, false
}

func (gs *GMService) ExecuteCommand(ctx context.Context, req *proto.GMCommandRequest) (*proto.CommonResponse, error) {
	// 验证GM权限
	gmUserID := ctx.Value("user_id")
	if gmUserID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	gmID := gmUserID.(uint64)
	if authResp := gs.authorizeGMUser(gmID); authResp != nil {
		return authResp, nil
	}

	// TODO: 这里应该检查用户是否有GM权限
	// 目前简单假设所有登录用户都有GM权限

	// 解析请求数据
	cmdReq := req

	// 验证命令
	if cmdReq.Command == "" {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "命令不能为空",
		}, nil
	}

	// 执行GM命令
	result, err := gs.executeGMCommand(gmID, cmdReq.Command, cmdReq.Args)
	if err != nil {
		log.Printf("执行GM命令失败: %v", err)
		return &proto.CommonResponse{
			Code:    1003,
			Message: fmt.Sprintf("命令执行失败: %v", err),
		}, nil
	}

	// 记录GM操作日志
	details := fmt.Sprintf("命令: %s, 参数: %v, 结果: %s", cmdReq.Command, cmdReq.Args, result)
	gs.server.gmRepo.LogGMAction(gmID, "execute_command", 0, details)

	log.Printf("GM用户 %d 执行命令成功: %s", gmID, cmdReq.Command)

	return &proto.CommonResponse{
		Code:    0,
		Message: "命令执行成功",
		Data:    []byte(result),
	}, nil
}

// executeGMCommand 执行具体的GM命令
func (gs *GMService) executeGMCommand(gmUserID uint64, command string, args []string) (string, error) {
	switch strings.ToLower(command) {
	case "kick":
		if len(args) < 1 {
			return "", fmt.Errorf("kick命令需要用户ID参数")
		}
		userID, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return "", fmt.Errorf("无效的用户ID: %s", args[0])
		}
		reason := "GM踢出"
		if len(args) > 1 {
			reason = strings.Join(args[1:], " ")
		}
		// TODO: 实现向用户发送踢出消息
		logger.Info(fmt.Sprintf("Sending kick message to user %d: %v", userID, map[string]interface{}{
			"reason": reason,
		}))
		return fmt.Sprintf("用户 %d 已被踢出，原因: %s", userID, reason), nil

	case "ban":
		if len(args) < 2 {
			return "", fmt.Errorf("ban命令需要用户ID和时长参数")
		}
		userID, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return "", fmt.Errorf("无效的用户ID: %s", args[0])
		}
		duration, err := strconv.ParseUint(args[1], 10, 32)
		if err != nil {
			return "", fmt.Errorf("无效的时长: %s", args[1])
		}
		reason := "GM封禁"
		if len(args) > 2 {
			reason = strings.Join(args[2:], " ")
		}
		// 封禁用户
		if err := gs.server.gmRepo.BanUser(userID, gmUserID, reason, uint32(duration)); err != nil {
			return "", err
		}
		// TODO: 实现向用户发送封禁消息
		logger.Info(fmt.Sprintf("Sending ban message to user %d: %v", userID, map[string]interface{}{
			"reason": "账号已被封禁: " + reason,
		}))
		return fmt.Sprintf("用户 %d 已被封禁 %d 秒，原因: %s", userID, duration, reason), nil

	case "unban":
		if len(args) < 1 {
			return "", fmt.Errorf("unban命令需要用户ID参数")
		}
		userID, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return "", fmt.Errorf("无效的用户ID: %s", args[0])
		}
		// 解封用户
		if err := gs.server.gmRepo.UnbanUser(userID, gmUserID); err != nil {
			return "", err
		}
		return fmt.Sprintf("用户 %d 已被解封", userID), nil

	case "notice":
		if len(args) < 1 {
			return "", fmt.Errorf("notice命令需要公告内容参数")
		}
		content := strings.Join(args, " ")
		// TODO: 实现全服广播
		logger.Info(fmt.Sprintf("Broadcasting notice: %s", content))
		return fmt.Sprintf("全服公告已发送: %s", content), nil

	case "reload":
		// TODO: 重载配置
		logger.Info("Config reload command sent")
		return "配置重载命令已发送", nil

	case "status":
		// 获取服务器状态
		return fmt.Sprintf("服务器运行正常，当前时间: %s", time.Now().Format("2006-01-02 15:04:05")), nil

	default:
		return "", fmt.Errorf("未知命令: %s", command)
	}
}

// KickUser 踢出用户
func (gs *GMService) KickUser(ctx context.Context, req *proto.KickUserRequest) (*proto.CommonResponse, error) {
	// 验证GM权限
	gmUserID := ctx.Value("user_id")
	if gmUserID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	gmID := gmUserID.(uint64)
	if authResp := gs.authorizeGMUser(gmID); authResp != nil {
		return authResp, nil
	}

	// 解析请求数据
	kickReq := req

	// 验证目标用户ID
	if kickReq.TargetUserId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "目标用户ID不能为空",
		}, nil
	}

	// 不能踢出自己
	if kickReq.TargetUserId == gmID {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "不能踢出自己",
		}, nil
	}

	// TODO: 检查用户是否存在
	logger.Debug(fmt.Sprintf("Checking if user %d exists", kickReq.TargetUserId))

	// 设置默认踢出原因
	reason := kickReq.Reason
	if reason == "" {
		reason = "违反游戏规则"
	}

	// TODO: 实现向用户发送踢出消息
	logger.Info(fmt.Sprintf("Sending kick message to user %d: %v", kickReq.TargetUserId, map[string]interface{}{
		"reason": reason,
		"type":   "kick",
	}))

	// 记录GM操作日志
	details := fmt.Sprintf("踢出用户 %d，原因: %s", kickReq.TargetUserId, reason)
	gs.server.gmRepo.LogGMAction(gmID, "kick_user", kickReq.TargetUserId, details)

	log.Printf("GM用户 %d 踢出用户 %d 成功，原因: %s", gmID, kickReq.TargetUserId, reason)

	return &proto.CommonResponse{
		Code:    0,
		Message: "用户踢出成功",
		Data:    []byte(fmt.Sprintf("{\"target_user_id\":%d,\"reason\":\"%s\"}", kickReq.TargetUserId, reason)),
	}, nil
}

// BanUser 封禁用户
func (gs *GMService) BanUser(ctx context.Context, req *proto.BanUserRequest) (*proto.CommonResponse, error) {
	// 验证GM权限
	gmUserID := ctx.Value("user_id")
	if gmUserID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	gmID := gmUserID.(uint64)
	if authResp := gs.authorizeGMUser(gmID); authResp != nil {
		return authResp, nil
	}

	// 解析请求数据
	banReq := req

	// 验证目标用户ID
	if banReq.TargetUserId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "目标用户ID不能为空",
		}, nil
	}

	// 不能封禁自己
	if banReq.TargetUserId == gmID {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "不能封禁自己",
		}, nil
	}

	// TODO: 检查用户是否存在
	logger.Debug(fmt.Sprintf("Checking if user %d exists", banReq.TargetUserId))

	// 设置默认封禁原因和时长
	reason := banReq.Reason
	if reason == "" {
		reason = "违反游戏规则"
	}

	duration := banReq.Duration
	if duration == 0 {
		// 默认封禁24小时
		duration = 24 * 60 * 60
	}

	// 检查用户是否已被封禁
	banned, _, err := gs.server.gmRepo.IsUserBanned(banReq.TargetUserId)
	if err != nil {
		log.Printf("检查用户封禁状态失败: %v", err)
		return &proto.CommonResponse{
			Code:    1005,
			Message: "检查用户状态失败",
		}, nil
	}

	if banned {
		return &proto.CommonResponse{
			Code:    1006,
			Message: "用户已被封禁",
		}, nil
	}

	// 封禁用户
	if err := gs.server.gmRepo.BanUser(banReq.TargetUserId, gmID, reason, duration); err != nil {
		log.Printf("封禁用户失败: %v", err)
		return &proto.CommonResponse{
			Code:    1007,
			Message: "封禁用户失败",
		}, nil
	}

	// TODO: 实现向用户发送封禁消息
	logger.Info(fmt.Sprintf("Sending ban message to user %d: %v", banReq.TargetUserId, map[string]interface{}{
		"reason": "账号已被封禁: " + reason,
		"type":   "ban",
	}))

	// 记录GM操作日志
	details := fmt.Sprintf("封禁用户 %d，时长: %d秒，原因: %s", banReq.TargetUserId, duration, reason)
	gs.server.gmRepo.LogGMAction(gmID, "ban_user", banReq.TargetUserId, details)

	log.Printf("GM用户 %d 封禁用户 %d 成功，时长: %d秒，原因: %s", gmID, banReq.TargetUserId, duration, reason)

	return &proto.CommonResponse{
		Code:    0,
		Message: "用户封禁成功",
		Data:    []byte(fmt.Sprintf("{\"target_user_id\":%d,\"duration\":%d,\"reason\":\"%s\"}", banReq.TargetUserId, duration, reason)),
	}, nil
}

// UnbanUser 解封用户
func (gs *GMService) UnbanUser(ctx context.Context, req *proto.UnbanUserRequest) (*proto.CommonResponse, error) {
	// 验证GM权限
	gmUserID := ctx.Value("user_id")
	if gmUserID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	gmID := gmUserID.(uint64)
	if authResp := gs.authorizeGMUser(gmID); authResp != nil {
		return authResp, nil
	}

	// 解析请求数据
	unbanReq := req

	// 验证目标用户ID
	if unbanReq.TargetUserId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "目标用户ID不能为空",
		}, nil
	}

	// TODO: 检查用户是否存在
	logger.Debug(fmt.Sprintf("Checking if user %d exists", unbanReq.TargetUserId))

	// 检查用户是否被封禁
	banned, banRecord, err := gs.server.gmRepo.IsUserBanned(unbanReq.TargetUserId)
	if err != nil {
		log.Printf("检查用户封禁状态失败: %v", err)
		return &proto.CommonResponse{
			Code:    1004,
			Message: "检查用户状态失败",
		}, nil
	}

	if !banned {
		return &proto.CommonResponse{
			Code:    1005,
			Message: "用户未被封禁",
		}, nil
	}

	// 解封用户
	if err := gs.server.gmRepo.UnbanUser(unbanReq.TargetUserId, gmID); err != nil {
		log.Printf("解封用户失败: %v", err)
		return &proto.CommonResponse{
			Code:    1006,
			Message: "解封用户失败",
		}, nil
	}

	// 记录GM操作日志
	details := fmt.Sprintf("解封用户 %d，原封禁原因: %s", unbanReq.TargetUserId, banRecord.Reason)
	gs.server.gmRepo.LogGMAction(gmID, "unban_user", unbanReq.TargetUserId, details)

	log.Printf("GM用户 %d 解封用户 %d 成功", gmID, unbanReq.TargetUserId)

	return &proto.CommonResponse{
		Code:    0,
		Message: "用户解封成功",
		Data:    []byte(fmt.Sprintf("{\"target_user_id\":%d}", unbanReq.TargetUserId)),
	}, nil
}

// SendNotice 发送公告
func (gs *GMService) SendNotice(ctx context.Context, req *proto.SendNoticeRequest) (*proto.CommonResponse, error) {
	// 验证GM权限
	gmUserID := ctx.Value("user_id")
	if gmUserID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	gmID := gmUserID.(uint64)
	if authResp := gs.authorizeGMUser(gmID); authResp != nil {
		return authResp, nil
	}

	// 解析请求数据
	noticeReq := req

	// 验证公告标题
	if noticeReq.Title == "" {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "公告标题不能为空",
		}, nil
	}

	// 验证公告内容
	if noticeReq.Content == "" {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "公告内容不能为空",
		}, nil
	}

	// 构造公告消息
	noticeMsg := map[string]interface{}{
		"title":       noticeReq.Title,
		"content":     noticeReq.Content,
		"notice_type": noticeReq.NoticeType,
		"send_time":   time.Now().Unix(),
	}

	var targetCount int

	// 根据目标用户发送公告
	if len(noticeReq.TargetUsers) > 0 {
		// 发送给指定用户
		for _, userID := range noticeReq.TargetUsers {
			// TODO: 实现获取用户信息
			logger.Debug(fmt.Sprintf("Getting user info for ID: %d", userID))
			// TODO: 实现向用户发送公告
			logger.Info(fmt.Sprintf("Sending announcement to user %d: %v", userID, noticeMsg))
			targetCount++
		}
	} else {
		// TODO: 实现全服公告
		logger.Info(fmt.Sprintf("Broadcasting announcement: %v", noticeMsg))
		targetCount = -1 // -1表示全服
	}

	// 记录GM操作日志
	var details string
	if targetCount == -1 {
		details = fmt.Sprintf("发送全服公告，标题: %s，内容: %s", noticeReq.Title, noticeReq.Content)
	} else {
		details = fmt.Sprintf("发送定向公告给 %d 个用户，标题: %s，内容: %s", targetCount, noticeReq.Title, noticeReq.Content)
	}
	gs.server.gmRepo.LogGMAction(gmID, "send_notice", 0, details)

	log.Printf("GM用户 %d 发送公告成功，目标用户数: %d", gmID, targetCount)

	var resultMsg string
	if targetCount == -1 {
		resultMsg = "全服公告发送成功"
	} else {
		resultMsg = fmt.Sprintf("公告发送成功，目标用户数: %d", targetCount)
	}

	return &proto.CommonResponse{
		Code:    0,
		Message: resultMsg,
		Data:    []byte(fmt.Sprintf("{\"target_count\":%d,\"title\":\"%s\"}", targetCount, noticeReq.Title)),
	}, nil
}

// ReloadConfig 重新加载配置
func (gs *GMService) ReloadConfig(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	gmID, authResp := gs.authorizeGM(ctx)
	if authResp != nil {
		return &proto.BaseResponse{
			Header: req.GetHeader(),
			Code:   int32(authResp.Code),
			Msg:    authResp.Message,
		}, nil
	}

	// 广播配置重载命令
	gs.server.messageBroker.BroadcastSystemMessage("reload_config", nil)
	logger.Info(fmt.Sprintf("GM user %d requested config reload", gmID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "config reload requested",
	}, nil
}
