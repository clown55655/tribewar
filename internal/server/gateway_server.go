package server

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"tribeway/internal/actor"
	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/internal/network"
	"tribeway/pkg/proto"
)

// GatewayServer 网关服务器
type GatewayServer struct {
	*BaseServer
	messageHandler *GatewayMessageHandler
}

// NewGatewayServer 创建网关服务器
func NewGatewayServer(configFile, nodeID string) *GatewayServer {
	baseServer, err := NewBaseServerWithOptions(configFile, "gateway", nodeID, GatewayComponents())
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create base server: %v", err))
	}

	gatewayServer := &GatewayServer{
		BaseServer:     baseServer,
		messageHandler: NewGatewayMessageHandler(baseServer),
	}

	// 初始化TCP服务器
	tcpServer := network.NewTCPServer(
		"0.0.0.0",
		baseServer.config.Network.TCPPort,
		gatewayServer.messageHandler,
		baseServer.config.Network.MaxConnections,
	)
	tcpServer.SetTimeouts(
		time.Duration(baseServer.config.Network.ReadTimeout)*time.Second,
		time.Duration(baseServer.config.Network.WriteTimeout)*time.Second,
	)
	tcpServer.SetMaxFrameSize(baseServer.maxPacketSize())
	gatewayServer.tcpServer = tcpServer

	// 注册通用服务
	if err := RegisterCommonServices(baseServer); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register common services: %v", err))
	}

	// 注册网关服务
	gatewayService := NewGatewayService(gatewayServer)
	if err := baseServer.rpcServer.RegisterService(gatewayService); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register gateway service: %v", err))
	}

	// 创建网关Actor
	gatewayActor := NewGatewayActor(gatewayServer)
	if err := baseServer.actorSystem.SpawnActor(gatewayActor); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to spawn gateway actor: %v", err))
	}

	return gatewayServer
}

// Start 启动网关服务器
func (gs *GatewayServer) Start() error {
	// 启动基础服务器
	if err := gs.BaseServer.Start(); err != nil {
		return err
	}

	// 启动TCP服务器
	if err := gs.tcpServer.Start(); err != nil {
		return fmt.Errorf("failed to start tcp server: %v", err)
	}

	logger.Info(fmt.Sprintf("Gateway server %s started on TCP port %d",
		gs.nodeID, gs.config.Network.TCPPort))

	return nil
}

// Stop 停止网关服务器
func (gs *GatewayServer) Stop() error {
	if gs.tcpServer != nil {
		gs.tcpServer.Stop()
	}

	return gs.BaseServer.Stop()
}

// GatewayMessageHandler 网关消息处理器
type GatewayMessageHandler struct {
	server *BaseServer
}

// NewGatewayMessageHandler 创建网关消息处理器
func NewGatewayMessageHandler(server *BaseServer) *GatewayMessageHandler {
	return &GatewayMessageHandler{
		server: server,
	}
}

// HandleMessage 处理消息
func (gmh *GatewayMessageHandler) HandleMessage(conn *network.Connection, data []byte) error {
	// 解析消息头
	if len(data) < 4 {
		return fmt.Errorf("message too short")
	}

	// 解析消息ID
	msgID := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])

	// 解析Protobuf消息
	var request proto.BaseRequest
	if err := proto.Unmarshal(data[4:], &request); err != nil {
		return fmt.Errorf("failed to unmarshal request: %v", err)
	}

	logger.Debug(fmt.Sprintf("Received message ID: %d from connection %d", msgID, conn.ID))

	// 路由消息到对应的处理器
	return gmh.routeMessage(conn, msgID, &request)
}

// routeMessage 路由消息
func (gmh *GatewayMessageHandler) routeMessage(conn *network.Connection, msgID uint32, request *proto.BaseRequest) error {
	switch msgID {
	case 1001: // 用户登录
		return gmh.handleLogin(conn, request)
	case 1002: // 心跳
		return gmh.handleHeartbeat(conn, request)
	case 1003: // 用户登出
		return gmh.handleLogout(conn, request)
	default:
		// 转发到其他服务器
		return gmh.forwardMessage(conn, msgID, request)
	}
}

// handleLogin 处理登录
func (gmh *GatewayMessageHandler) handleLogin(conn *network.Connection, request *proto.BaseRequest) error {
	// 解析登录请求
	var loginReq proto.LoginRequest
	if err := proto.Unmarshal(request.Data, &loginReq); err != nil {
		return fmt.Errorf("failed to unmarshal login request: %v", err)
	}

	// 获取登录服务
	loginService := gmh.server.discovery.GetService("login")
	if loginService == nil {
		return gmh.sendError(conn, request, -1, "login service not available")
	}

	// TODO: 通过RPC调用登录服务
	// 简化实现：直接返回成功响应
	logger.Info(fmt.Sprintf("Login request for user: %s", loginReq.Username))

	// 模拟登录成功响应
	loginResp := proto.LoginResponse{
		UserId: 12345,
		Token:  "mock_token_" + loginReq.Username,
	}

	// 绑定连接到用户
	conn.UserID = loginResp.UserId

	// 设置用户在线状态
	userCache := database.NewUserCache(gmh.server.redisManager)
	userCache.SetUserOnline(loginResp.UserId, gmh.server.nodeID)

	// 发送响应
	return gmh.sendResponse(conn, request, 0, "login success", &loginResp)
}

// handleHeartbeat 处理心跳
func (gmh *GatewayMessageHandler) handleHeartbeat(conn *network.Connection, request *proto.BaseRequest) error {
	// 更新连接活动时间
	conn.LastActivity = time.Now()

	// 发送心跳响应
	return gmh.sendResponse(conn, request, 0, "pong", nil)
}

// handleLogout 处理登出
func (gmh *GatewayMessageHandler) handleLogout(conn *network.Connection, request *proto.BaseRequest) error {
	if conn.UserID != 0 {
		// 设置用户离线
		userCache := database.NewUserCache(gmh.server.redisManager)
		userCache.SetUserOffline(conn.UserID)

		logger.Info(fmt.Sprintf("User %d logged out from connection %d", conn.UserID, conn.ID))
	}

	// 关闭连接
	conn.Close()

	return nil
}

// forwardMessage 转发消息
func (gmh *GatewayMessageHandler) forwardMessage(conn *network.Connection, msgID uint32, request *proto.BaseRequest) error {
	// 根据消息ID确定目标服务
	var targetService string

	switch {
	case msgID >= 2000 && msgID < 3000:
		targetService = "lobby"
	case msgID >= 3000 && msgID < 4000:
		targetService = "game"
	case msgID >= 4000 && msgID < 5000:
		targetService = "friend"
	case msgID >= 5000 && msgID < 6000:
		targetService = "chat"
	case msgID >= 6000 && msgID < 7000:
		targetService = "mail"
	default:
		return gmh.sendError(conn, request, -1, "unknown message type")
	}

	// 获取目标服务实例
	service := gmh.server.discovery.GetService(targetService)
	if service == nil {
		return gmh.sendError(conn, request, -2, fmt.Sprintf("%s service not available", targetService))
	}

	// TODO: 通过RPC转发消息
	// 简化实现：直接返回成功响应
	logger.Info(fmt.Sprintf("Forwarding message ID %d to service: %s", msgID, targetService))

	// 模拟服务调用成功响应
	return gmh.sendResponse(conn, request, 0, "success", nil)
}

// sendResponse 发送响应
func (gmh *GatewayMessageHandler) sendResponse(conn *network.Connection, request *proto.BaseRequest, code int32, msg string, data proto.Message) error {
	response := &proto.BaseResponse{
		Header: request.Header,
		Code:   code,
		Msg:    msg,
	}

	if data != nil {
		responseData, err := proto.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal response data: %v", err)
		}
		response.Data = responseData
	}

	responseBytes, err := proto.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %v", err)
	}

	// 添加消息长度头
	length := len(responseBytes)
	message := make([]byte, 4+length)
	message[0] = byte(length >> 24)
	message[1] = byte(length >> 16)
	message[2] = byte(length >> 8)
	message[3] = byte(length)
	copy(message[4:], responseBytes)

	return conn.Write(message)
}

// sendError 发送错误响应
func (gmh *GatewayMessageHandler) sendError(conn *network.Connection, request *proto.BaseRequest, code int32, msg string) error {
	return gmh.sendResponse(conn, request, code, msg, nil)
}

// GatewayService 网关RPC服务
type GatewayService struct {
	server *GatewayServer
}

// NewGatewayService 创建网关服务
func NewGatewayService(server *GatewayServer) *GatewayService {
	return &GatewayService{
		server: server,
	}
}

// GetName 获取服务名称
func (gs *GatewayService) GetName() string {
	return "GatewayService"
}

// RegisterMethods 注册方法
func (gs *GatewayService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["GetConnectionCount"] = reflect.ValueOf(gs.GetConnectionCount)
	methods["SendToUser"] = reflect.ValueOf(gs.SendToUser)
	methods["BroadcastMessage"] = reflect.ValueOf(gs.BroadcastMessage)
	methods["KickUser"] = reflect.ValueOf(gs.KickUser)

	return methods
}

// GetConnectionCount 获取连接数
func (gs *GatewayService) GetConnectionCount(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	count := gs.server.tcpServer.GetConnectionCount()

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    fmt.Sprintf("connection count: %d", count),
	}

	return response, nil
}

// SendToUser 发送消息给指定用户
func (gs *GatewayService) SendToUser(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 这里需要从请求中解析目标用户ID和消息内容
	// 简化实现，实际需要定义具体的消息格式

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "message sent",
	}

	return response, nil
}

// BroadcastMessage 广播消息
func (gs *GatewayService) BroadcastMessage(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 广播消息给所有连接的用户
	gs.server.tcpServer.Broadcast(req.Data)

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "message broadcasted",
	}

	return response, nil
}

// KickUser 踢出用户
func (gs *GatewayService) KickUser(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 这里需要从请求中解析用户ID
	// 简化实现

	response := &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "user kicked",
	}

	return response, nil
}

// GatewayActor 网关Actor
type GatewayActor struct {
	*actor.BaseActor
	server *GatewayServer
}

// NewGatewayActor 创建网关Actor
func NewGatewayActor(server *GatewayServer) *GatewayActor {
	baseActor := actor.NewBaseActor("gateway_actor", "gateway", 1000)

	return &GatewayActor{
		BaseActor: baseActor,
		server:    server,
	}
}

// OnReceive 处理消息
func (ga *GatewayActor) OnReceive(ctx context.Context, msg actor.Message) error {
	switch msg.GetType() {
	case actor.MSG_TYPE_USER_LOGIN:
		return ga.handleUserLogin(msg)
	case actor.MSG_TYPE_USER_LOGOUT:
		return ga.handleUserLogout(msg)
	default:
		logger.Debug(fmt.Sprintf("Unknown message type: %s", msg.GetType()))
	}

	return nil
}

// OnStart 启动时处理
func (ga *GatewayActor) OnStart(ctx context.Context) error {
	logger.Info("Gateway actor started")
	return nil
}

// OnStop 停止时处理
func (ga *GatewayActor) OnStop(ctx context.Context) error {
	logger.Info("Gateway actor stopped")
	return nil
}

// handleUserLogin 处理用户登录
func (ga *GatewayActor) handleUserLogin(msg actor.Message) error {
	logger.Debug("Handling user login in gateway actor")
	// 处理登录相关逻辑
	return nil
}

// handleUserLogout 处理用户登出
func (ga *GatewayActor) handleUserLogout(msg actor.Message) error {
	logger.Debug("Handling user logout in gateway actor")
	// 处理登出相关逻辑
	return nil
}
