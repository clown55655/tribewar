package server

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/internal/mq"
	"tribeway/pkg/proto"
)

// ChatServer 聊天服务器
type ChatServer struct {
	*BaseServer
	chatRepo      *database.ChatRepository
	userRepo      *database.UserRepository
	nextMessageID uint64
	idMutex       sync.Mutex
}

// NewChatServer 创建聊天服务器
func NewChatServer(configFile, nodeID string) *ChatServer {
	baseServer, err := NewBaseServerWithOptions(configFile, "chat", nodeID, ChatComponents())
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create base server: %v", err))
	}

	chatServer := &ChatServer{
		BaseServer: baseServer,
	}

	// 初始化数据库仓库
	chatServer.chatRepo = database.NewChatRepository(baseServer.mongoManager)
	chatServer.userRepo = database.NewUserRepository(baseServer.mongoManager)

	// TODO: 创建聊天消息处理器

	// 注册通用服务
	if err := RegisterCommonServices(baseServer); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register common services: %v", err))
	}

	// 注册聊天服务
	chatService := NewChatService(chatServer)
	if err := baseServer.rpcServer.RegisterService(chatService); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register chat service: %v", err))
	}

	// 订阅聊天消息 - 简化实现
	// TODO: 实现消息订阅逻辑

	return chatServer
}

// handleChatMessage 处理聊天消息
func (cs *ChatServer) handleChatMessage(msg *mq.ChatMessage) error {
	logger.Debug(fmt.Sprintf("Received chat message from %d to %d: %s", msg.FromUserID, msg.ToUserID, msg.Content))

	// TODO: 实现聊天消息处理逻辑
	// 比如过滤敏感词、存储历史记录等

	return nil
}

// ChatService 聊天RPC服务
type ChatService struct {
	server *ChatServer
}

// NewChatService 创建聊天服务
func NewChatService(server *ChatServer) *ChatService {
	return &ChatService{
		server: server,
	}
}

// GetName 获取服务名称
func (cs *ChatService) GetName() string {
	return "ChatService"
}

// RegisterMethods 注册方法
func (cs *ChatService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["SendMessage"] = reflect.ValueOf(cs.SendMessage)
	methods["GetChatHistory"] = reflect.ValueOf(cs.GetChatHistory)
	methods["BlockUser"] = reflect.ValueOf(cs.BlockUser)
	methods["UnblockUser"] = reflect.ValueOf(cs.UnblockUser)

	return methods
}

// SendMessage 发送消息
func (cs *ChatService) SendMessage(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// TODO: 实现发送消息逻辑
	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "message sent",
	}, nil
}

// GetChatHistory 获取聊天历史
func (cs *ChatService) GetChatHistory(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// TODO: 实现获取聊天历史逻辑
	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "success",
	}, nil
}

// BlockUser 屏蔽用户
func (cs *ChatService) BlockUser(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// TODO: 实现屏蔽用户逻辑
	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "user blocked",
	}, nil
}

// UnblockUser 取消屏蔽用户
func (cs *ChatService) UnblockUser(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// TODO: 实现取消屏蔽用户逻辑
	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "user unblocked",
	}, nil
}
