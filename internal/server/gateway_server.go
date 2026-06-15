package server

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"tribeway/internal/actor"
	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/internal/network"
	"tribeway/internal/rpc"
	"tribeway/pkg/proto"
)

// GatewayServer 网关服务器
type GatewayServer struct {
	*BaseServer
	messageHandler *GatewayMessageHandler
}

// NewGatewayServer 创建网关服务器
func NewGatewayServer(configFile, nodeID string) *GatewayServer {
	gatewayServer, err := NewGatewayServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create gateway server: %v", err))
	}
	return gatewayServer
}

func NewGatewayServerWithError(configFile, nodeID string) (*GatewayServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "gateway", nodeID, GatewayComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

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
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	// 注册网关服务
	gatewayService := NewGatewayService(gatewayServer)
	if err := baseServer.rpcServer.RegisterService(gatewayService); err != nil {
		return nil, fmt.Errorf("failed to register gateway service: %v", err)
	}

	// 创建网关Actor
	gatewayActor := NewGatewayActor(gatewayServer)
	if err := baseServer.actorSystem.SpawnActor(gatewayActor); err != nil {
		return nil, fmt.Errorf("failed to spawn gateway actor: %v", err)
	}

	constructed = true
	return gatewayServer, nil
}

// Start 启动网关服务器
func (gs *GatewayServer) Start() error {
	// 启动基础服务器
	if err := gs.BaseServer.Start(); err != nil {
		return err
	}

	// 启动TCP服务器
	if err := gs.tcpServer.Start(); err != nil {
		if stopErr := gs.BaseServer.Stop(); stopErr != nil {
			logger.Warnf("Failed to stop base server after tcp start failure: %v", stopErr)
		}
		return fmt.Errorf("failed to start tcp server: %v", err)
	}

	logger.Info(fmt.Sprintf("Gateway server %s started on TCP port %d",
		gs.nodeID, gs.config.Network.TCPPort))

	return nil
}

// Stop 停止网关服务器
func (gs *GatewayServer) Stop() error {
	err := gs.BaseServer.Stop()
	if gs.messageHandler != nil {
		gs.messageHandler.Close()
	}
	return err
}

// GatewayMessageHandler 网关消息处理器
type GatewayMessageHandler struct {
	server   *BaseServer
	pools    map[string]*rpc.RPCConnectionPool
	poolsMux sync.Mutex
}

// NewGatewayMessageHandler 创建网关消息处理器
func NewGatewayMessageHandler(server *BaseServer) *GatewayMessageHandler {
	return &GatewayMessageHandler{
		server: server,
		pools:  make(map[string]*rpc.RPCConnectionPool),
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
		if err := gmh.validateSession(conn, request); err != nil {
			return err
		}
		return gmh.handleHeartbeat(conn, request)
	case 1003: // 用户登出
		if err := gmh.validateSession(conn, request); err != nil {
			return err
		}
		return gmh.handleLogout(conn, request)
	default:
		if err := gmh.validateSession(conn, request); err != nil {
			return err
		}
		// 转发到其他服务器
		return gmh.forwardMessage(conn, msgID, request)
	}
}

// handleLogin 处理登录
func (gmh *GatewayMessageHandler) handleLogin(conn *network.Connection, request *proto.BaseRequest) error {
	var loginReq proto.LoginRequest
	if err := proto.Unmarshal(request.Data, &loginReq); err != nil {
		return fmt.Errorf("failed to unmarshal login request: %v", err)
	}

	loginService := gmh.server.discovery.GetService("login")
	if loginService == nil {
		return gmh.sendError(conn, request, -1, "login service not available")
	}

	logger.Info(fmt.Sprintf("Login request for user: %s", loginReq.Username))

	pool := gmh.rpcPool(loginService.Address, loginService.Port)
	client, err := pool.Get()
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to get login RPC client %s:%d: %v", loginService.Address, loginService.Port, err))
		return gmh.sendError(conn, request, -2, "login service unavailable")
	}
	defer pool.Put(client)

	responseData, err := client.Call("LoginService", "Login", &loginReq, 5*time.Second)
	if err != nil {
		logger.Warn(fmt.Sprintf("Login RPC failed for user %s: %v", loginReq.Username, err))
		return gmh.sendError(conn, request, -3, "login failed")
	}

	var loginResp proto.LoginResponse
	if err := proto.Unmarshal(responseData, &loginResp); err != nil {
		logger.Error(fmt.Sprintf("Failed to unmarshal login response: %v", err))
		return gmh.sendError(conn, request, -4, "invalid login response")
	}
	if loginResp.UserId == 0 || loginResp.Token == "" {
		logger.Warn(fmt.Sprintf("Login service returned incomplete response for user %s", loginReq.Username))
		return gmh.sendError(conn, request, -5, "invalid login response")
	}

	conn.UserID = loginResp.UserId
	conn.SessionID = loginResp.Token

	userCache := database.NewUserCache(gmh.server.redisManager)
	if err := userCache.SetUserOnlineContext(context.Background(), loginResp.UserId, gmh.server.nodeID); err != nil {
		logger.Warn(fmt.Sprintf("Failed to set user online cache for %d: %v", loginResp.UserId, err))
	}

	return gmh.sendResponse(conn, request, 0, "login success", &loginResp)
}

func (gmh *GatewayMessageHandler) rpcPool(address string, port int) *rpc.RPCConnectionPool {
	key := fmt.Sprintf("%s:%d", address, port)

	gmh.poolsMux.Lock()
	defer gmh.poolsMux.Unlock()

	if pool := gmh.pools[key]; pool != nil {
		return pool
	}

	poolSize := gmh.server.config.RPC.PoolSize
	if poolSize <= 0 {
		poolSize = 8
	}
	pool := rpc.NewRPCConnectionPool(address, port, poolSize)
	pool.SetFrameOptions(
		time.Duration(gmh.server.config.Network.ReadTimeout)*time.Second,
		time.Duration(gmh.server.config.Network.WriteTimeout)*time.Second,
		gmh.server.maxPacketSize(),
	)
	gmh.pools[key] = pool
	return pool
}

func (gmh *GatewayMessageHandler) Close() {
	gmh.poolsMux.Lock()
	defer gmh.poolsMux.Unlock()

	for key, pool := range gmh.pools {
		pool.Close()
		delete(gmh.pools, key)
	}
}

func (gmh *GatewayMessageHandler) validateSession(conn *network.Connection, request *proto.BaseRequest) error {
	if request == nil || request.Header == nil {
		return gmh.rejectSession(conn, request, -10000, "invalid request")
	}

	header := request.Header
	if conn.UserID == 0 || conn.SessionID == "" {
		return gmh.rejectSession(conn, request, -10001, "unauthenticated")
	}
	if header.GetUserId() == 0 || header.GetSessionId() == "" {
		return gmh.rejectSession(conn, request, -10001, "missing session")
	}
	if header.GetUserId() != conn.UserID || header.GetSessionId() != conn.SessionID {
		return gmh.rejectSession(conn, request, -10002, "session mismatch")
	}

	sessionCache := database.NewSessionCache(gmh.server.redisManager)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	userID, err := sessionCache.GetSessionContext(ctx, header.GetSessionId())
	if err != nil {
		logger.Warn(fmt.Sprintf("Session validation failed for user %d: %v", header.GetUserId(), err))
		return gmh.rejectSession(conn, request, -10003, "invalid session")
	}
	if userID != header.GetUserId() {
		logger.Warn(fmt.Sprintf("Session user mismatch: request=%d redis=%d", header.GetUserId(), userID))
		return gmh.rejectSession(conn, request, -10002, "session mismatch")
	}
	if err := sessionCache.RefreshSessionContext(ctx, header.GetSessionId()); err != nil {
		logger.Warn(fmt.Sprintf("Failed to refresh session for user %d: %v", header.GetUserId(), err))
	}

	return nil
}

func (gmh *GatewayMessageHandler) rejectSession(conn *network.Connection, request *proto.BaseRequest, code int32, msg string) error {
	if err := gmh.sendError(conn, request, code, msg); err != nil {
		return err
	}
	return fmt.Errorf(msg)
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
		if err := userCache.SetUserOffline(conn.UserID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to set user offline cache for %d: %v", conn.UserID, err))
		}

		logger.Info(fmt.Sprintf("User %d logged out from connection %d", conn.UserID, conn.ID))
	}

	// 关闭连接
	conn.Close()

	return nil
}

// forwardMessage 转发消息
func (gmh *GatewayMessageHandler) forwardMessage(conn *network.Connection, msgID uint32, request *proto.BaseRequest) error {
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

	service := gmh.server.discovery.GetService(targetService)
	if service == nil {
		return gmh.sendError(conn, request, -2, fmt.Sprintf("%s service not available", targetService))
	}

	logger.Warn(fmt.Sprintf("Forwarding message ID %d to service %s is not implemented; target instance %s:%d",
		msgID, targetService, service.Address, service.Port))

	return gmh.sendError(conn, request, -3, "message forwarding not implemented")
}

// sendResponse 发送响应
func (gmh *GatewayMessageHandler) sendResponse(conn *network.Connection, request *proto.BaseRequest, code int32, msg string, data proto.Message) error {
	var header *proto.MessageHeader
	if request != nil {
		header = request.Header
	}
	response := &proto.BaseResponse{
		Header: header,
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
