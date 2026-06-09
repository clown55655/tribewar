package server

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// FriendServer 好友服务器
type FriendServer struct {
	*BaseServer
	friendRepo *database.FriendRepository
}

// NewFriendServer 创建好友服务器
func NewFriendServer(configFile, nodeID string) *FriendServer {
	baseServer, err := NewBaseServerWithOptions(configFile, "friend", nodeID, FriendComponents())
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create base server: %v", err))
	}

	friendServer := &FriendServer{
		BaseServer: baseServer,
		friendRepo: database.NewFriendRepository(baseServer.mongoManager),
	}

	// 注册通用服务
	if err := RegisterCommonServices(baseServer); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register common services: %v", err))
	}

	// 注册好友服务
	friendService := NewFriendService(friendServer)
	if err := baseServer.rpcServer.RegisterService(friendService); err != nil {
		logger.Fatal(fmt.Sprintf("Failed to register friend service: %v", err))
	}

	return friendServer
}

// FriendService 好友RPC服务
type FriendService struct {
	server *FriendServer
}

// NewFriendService 创建好友服务
func NewFriendService(server *FriendServer) *FriendService {
	return &FriendService{
		server: server,
	}
}

// GetName 获取服务名称
func (fs *FriendService) GetName() string {
	return "FriendService"
}

// RegisterMethods 注册方法
func (fs *FriendService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["AddFriend"] = reflect.ValueOf(fs.AddFriend)
	methods["AcceptFriend"] = reflect.ValueOf(fs.AcceptFriend)
	methods["GetFriendList"] = reflect.ValueOf(fs.GetFriendList)
	methods["DeleteFriend"] = reflect.ValueOf(fs.DeleteFriend)

	return methods
}

// AddFriend 添加好友
func (fs *FriendService) AddFriend(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("AddFriend: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var addFriendReq proto.AddFriendRequest
	if err := proto.Unmarshal(req.Data, &addFriendReq); err != nil {
		logger.Error(fmt.Sprintf("AddFriend: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	friendID := addFriendReq.GetFriendId()
	message := addFriendReq.GetMessage()

	// 验证好友ID
	if friendID == 0 || friendID == userID {
		logger.Error(fmt.Sprintf("AddFriend: invalid friend id %d for user %d", friendID, userID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid friend id",
		}, nil
	}

	// 检查是否已经是好友或已发送请求
	existingFriends, err := fs.server.friendRepo.GetFriends(userID)
	if err != nil {
		logger.Error(fmt.Sprintf("AddFriend: failed to get existing friends: %v", err))
	} else {
		for _, friend := range existingFriends {
			if friend.FriendID == friendID {
				return &proto.BaseResponse{
					Header: req.Header,
					Code:   -4,
					Msg:    "already friends or request pending",
				}, nil
			}
		}
	}

	// 验证目标用户是否存在
	userRepo := database.NewUserRepository(fs.server.mongoManager)
	targetUser, err := userRepo.GetByUserID(friendID)
	if err != nil {
		logger.Error(fmt.Sprintf("AddFriend: target user %d not found: %v", friendID, err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "target user not found",
		}, nil
	}

	// 添加好友请求
	if err := fs.server.friendRepo.AddFriend(userID, friendID, message); err != nil {
		logger.Error(fmt.Sprintf("AddFriend: failed to add friend request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -6,
			Msg:    "failed to send friend request",
		}, nil
	}

	logger.Info(fmt.Sprintf("User %d sent friend request to %s (ID: %d)", userID, targetUser.Nickname, friendID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "friend request sent successfully",
	}, nil
}

// AcceptFriend 接受好友请求
func (fs *FriendService) AcceptFriend(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("AcceptFriend: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var acceptFriendReq proto.AcceptFriendRequest
	if err := proto.Unmarshal(req.Data, &acceptFriendReq); err != nil {
		logger.Error(fmt.Sprintf("AcceptFriend: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	friendID := acceptFriendReq.GetFriendId()

	// 验证好友ID
	if friendID == 0 || friendID == userID {
		logger.Error(fmt.Sprintf("AcceptFriend: invalid friend id %d for user %d", friendID, userID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid friend id",
		}, nil
	}

	// 验证发送请求的用户是否存在
	userRepo := database.NewUserRepository(fs.server.mongoManager)
	requesterUser, err := userRepo.GetByUserID(friendID)
	if err != nil {
		logger.Error(fmt.Sprintf("AcceptFriend: requester user %d not found: %v", friendID, err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "requester user not found",
		}, nil
	}

	// 接受好友请求
	if err := fs.server.friendRepo.AcceptFriend(userID, friendID); err != nil {
		logger.Error(fmt.Sprintf("AcceptFriend: failed to accept friend request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "failed to accept friend request",
		}, nil
	}

	logger.Info(fmt.Sprintf("User %d accepted friend request from %s (ID: %d)", userID, requesterUser.Nickname, friendID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "friend request accepted successfully",
	}, nil
}

// GetFriendList 获取好友列表
func (fs *FriendService) GetFriendList(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("GetFriendList: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 获取好友列表
	friends, err := fs.server.friendRepo.GetFriends(userID)
	if err != nil {
		logger.Error(fmt.Sprintf("GetFriendList: failed to get friends for user %d: %v", userID, err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "failed to get friend list",
		}, nil
	}

	// 获取用户详细信息
	userRepo := database.NewUserRepository(fs.server.mongoManager)
	var friendInfos []*proto.FriendInfo

	for _, friend := range friends {
		// 获取好友用户信息
		friendUser, err := userRepo.GetByUserID(friend.FriendID)
		if err != nil {
			logger.Warn(fmt.Sprintf("GetFriendList: failed to get friend user info %d: %v", friend.FriendID, err))
			continue
		}

		// 检查好友在线状态（这里简化处理，实际应该从Redis缓存中获取）
		online := false
		if time.Since(friendUser.LastLoginAt) < 30*time.Minute {
			online = true
		}

		friendInfo := &proto.FriendInfo{
			UserId:        friendUser.UserID,
			Nickname:      friendUser.Nickname,
			Level:         friendUser.Level,
			Avatar:        friendUser.Avatar,
			Online:        online,
			LastLoginTime: uint32(friendUser.LastLoginAt.Unix()),
		}

		friendInfos = append(friendInfos, friendInfo)
	}

	// 构造响应数据
	friendListResp := &proto.FriendListResponse{
		Friends: friendInfos,
	}

	responseData, err := proto.Marshal(friendListResp)
	if err != nil {
		logger.Error(fmt.Sprintf("GetFriendList: failed to marshal response: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "failed to marshal response",
		}, nil
	}

	logger.Info(fmt.Sprintf("User %d retrieved friend list with %d friends", userID, len(friendInfos)))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "success",
		Data:   responseData,
	}, nil
}

// DeleteFriend 删除好友
func (fs *FriendService) DeleteFriend(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("DeleteFriend: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var deleteFriendReq proto.AcceptFriendRequest // 复用AcceptFriendRequest结构
	if err := proto.Unmarshal(req.Data, &deleteFriendReq); err != nil {
		logger.Error(fmt.Sprintf("DeleteFriend: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	friendID := deleteFriendReq.GetFriendId()

	// 验证好友ID
	if friendID == 0 || friendID == userID {
		logger.Error(fmt.Sprintf("DeleteFriend: invalid friend id %d for user %d", friendID, userID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid friend id",
		}, nil
	}

	// 验证目标用户是否存在
	userRepo := database.NewUserRepository(fs.server.mongoManager)
	targetUser, err := userRepo.GetByUserID(friendID)
	if err != nil {
		logger.Error(fmt.Sprintf("DeleteFriend: target user %d not found: %v", friendID, err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "target user not found",
		}, nil
	}

	// 删除好友关系（需要删除双向关系）
	if err := fs.server.friendRepo.DeleteFriend(userID, friendID); err != nil {
		logger.Error(fmt.Sprintf("DeleteFriend: failed to delete friend relationship: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "failed to delete friend",
		}, nil
	}

	logger.Info(fmt.Sprintf("User %d deleted friend %s (ID: %d)", userID, targetUser.Nickname, friendID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "friend deleted successfully",
	}, nil
}
