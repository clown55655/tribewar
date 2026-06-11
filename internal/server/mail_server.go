package server

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// MailServer 邮件服务器
type MailServer struct {
	*BaseServer
	mailRepo   *database.MailRepository
	userRepo   *database.UserRepository
	nextMailID uint64
	idMutex    sync.Mutex
}

// NewMailServer 创建邮件服务器
func NewMailServer(configFile, nodeID string) *MailServer {
	mailServer, err := NewMailServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create mail server: %v", err))
	}
	return mailServer
}

func NewMailServerWithError(configFile, nodeID string) (*MailServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "mail", nodeID, MailComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

	mailServer := &MailServer{
		BaseServer: baseServer,
		mailRepo:   database.NewMailRepository(baseServer.mongoManager),
		userRepo:   database.NewUserRepository(baseServer.mongoManager),
		nextMailID: 1,
	}

	if err := RegisterCommonServices(baseServer); err != nil {
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	mailService := NewMailService(mailServer)
	if err := baseServer.rpcServer.RegisterService(mailService); err != nil {
		return nil, fmt.Errorf("failed to register mail service: %v", err)
	}
	constructed = true
	return mailServer, nil
}

// generateMailID 生成邮件ID
func (s *MailServer) generateMailID() uint64 {
	s.idMutex.Lock()
	defer s.idMutex.Unlock()
	id := s.nextMailID
	s.nextMailID++
	return id
}

// MailService 邮件RPC服务
type MailService struct {
	server *MailServer
}

// NewMailService 创建邮件服务
func NewMailService(server *MailServer) *MailService {
	return &MailService{
		server: server,
	}
}

// GetName 获取服务名称
func (ms *MailService) GetName() string {
	return "MailService"
}

// RegisterMethods 注册方法
func (ms *MailService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["GetMailList"] = reflect.ValueOf(ms.GetMailList)
	methods["ReadMail"] = reflect.ValueOf(ms.ReadMail)
	methods["ClaimRewards"] = reflect.ValueOf(ms.ClaimRewards)
	methods["DeleteMail"] = reflect.ValueOf(ms.DeleteMail)
	methods["SendMail"] = reflect.ValueOf(ms.SendMail)

	return methods
}

// GetMailList 获取邮件列表
func (ms *MailService) GetMailList(ctx context.Context, req *proto.MailListRequest) (*proto.MailListResponse, error) {
	// 验证用户ID
	userID := ctx.Value("user_id")
	if userID == nil {
		return &proto.MailListResponse{
			Mails: []*proto.MailInfo{},
			Total: 0,
		}, fmt.Errorf("用户未登录")
	}

	toUserID := userID.(uint64)

	// 解析请求数据
	mailType := req.MailType
	limit := req.Limit
	offset := req.Offset

	// 设置默认值
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// 获取邮件列表
	mails, total, err := ms.server.mailRepo.GetMailsByUserID(toUserID, mailType, limit, offset)
	if err != nil {
		log.Printf("获取邮件列表失败: %v", err)
		return &proto.MailListResponse{
			Mails: []*proto.MailInfo{},
			Total: 0,
		}, err
	}

	// 转换为proto格式
	protoMails := make([]*proto.MailInfo, 0, len(mails))
	for _, mail := range mails {
		// 获取发送者昵称
		var fromNickname string
		if mail.FromUserID > 0 {
			// TODO: 获取用户昵称
			fromNickname = fmt.Sprintf("用户%d", mail.FromUserID)
		} else {
			fromNickname = "系统"
		}

		// 转换奖励列表
		protoRewards := make([]*proto.Reward, 0, len(mail.Rewards))
		for _, reward := range mail.Rewards {
			protoReward := &proto.Reward{
				ItemId:   uint32(reward.ItemID),
				ItemType: 1, // TODO: 从reward获取类型
				Quantity: 1, // TODO: 从reward获取数量
			}
			protoRewards = append(protoRewards, protoReward)
		}

		protoMail := &proto.MailInfo{
			MailId:       mail.MailID,
			FromUserId:   mail.FromUserID,
			FromNickname: fromNickname,
			ToUserId:     mail.ToUserID,
			MailType:     1, // TODO: 从mail获取类型
			Title:        mail.Title,
			Content:      mail.Content,
			Rewards:      protoRewards,
			IsRead:       mail.IsRead,
			IsClaimed:    mail.IsClaimed,
			SendTime:     uint32(time.Now().Unix()), // TODO: 从mail获取发送时间
			ExpireTime:   0,                         // TODO: 从mail获取过期时间
		}
		protoMails = append(protoMails, protoMail)
	}

	log.Printf("用户 %d 获取邮件列表成功，邮件类型: %d，邮件数: %d", toUserID, mailType, len(protoMails))

	return &proto.MailListResponse{
		Mails: protoMails,
		Total: int32(total),
	}, nil
}

// ReadMail 读取邮件
func (ms *MailService) ReadMail(ctx context.Context, req *proto.MailOperationRequest) (*proto.CommonResponse, error) {
	// 验证用户ID
	userID := ctx.Value("user_id")
	if userID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	toUserID := userID.(uint64)

	// 解析请求数据
	readReq := req

	// 验证邮件ID
	if readReq.MailId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "邮件ID不能为空",
		}, nil
	}

	// 获取邮件信息
	mail, err := ms.server.mailRepo.GetMailByID(readReq.MailId)
	if err != nil {
		log.Printf("获取邮件信息失败: %v", err)
		return &proto.CommonResponse{
			Code:    1003,
			Message: "邮件不存在",
		}, nil
	}

	// 检查邮件是否属于当前用户
	if mail.ToUserID != toUserID {
		return &proto.CommonResponse{
			Code:    1004,
			Message: "无权限访问此邮件",
		}, nil
	}

	// TODO: 检查邮件是否过期
	// 简化实现：假设邮件未过期
	if false {
		return &proto.CommonResponse{
			Code:    1005,
			Message: "邮件已过期",
		}, nil
	}

	// 如果邮件未读，标记为已读
	if !mail.IsRead {
		if err := ms.server.mailRepo.UpdateMailReadStatus(readReq.MailId, true); err != nil {
			log.Printf("更新邮件已读状态失败: %v", err)
			return &proto.CommonResponse{
				Code:    1006,
				Message: "更新邮件状态失败",
			}, nil
		}
	}

	log.Printf("用户 %d 读取邮件 %d 成功", toUserID, readReq.MailId)

	return &proto.CommonResponse{
		Code:    0,
		Message: "邮件读取成功",
	}, nil
}

// ClaimRewards 领取奖励
func (ms *MailService) ClaimRewards(ctx context.Context, req *proto.MailOperationRequest) (*proto.CommonResponse, error) {
	// 验证用户ID
	userID := ctx.Value("user_id")
	if userID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	toUserID := userID.(uint64)

	// 解析请求数据
	claimReq := req

	// 验证邮件ID
	if claimReq.MailId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "邮件ID不能为空",
		}, nil
	}

	// 获取邮件信息
	mail, err := ms.server.mailRepo.GetMailByID(claimReq.MailId)
	if err != nil {
		log.Printf("获取邮件信息失败: %v", err)
		return &proto.CommonResponse{
			Code:    1003,
			Message: "邮件不存在",
		}, nil
	}

	// 检查邮件是否属于当前用户
	if mail.ToUserID != toUserID {
		return &proto.CommonResponse{
			Code:    1004,
			Message: "无权限访问此邮件",
		}, nil
	}

	// TODO: 检查邮件是否过期
	// 简化实现：假设邮件未过期
	if false {
		return &proto.CommonResponse{
			Code:    1005,
			Message: "邮件已过期",
		}, nil
	}

	// 检查是否有奖励
	if len(mail.Rewards) == 0 {
		return &proto.CommonResponse{
			Code:    1006,
			Message: "此邮件没有奖励",
		}, nil
	}

	// 检查奖励是否已领取
	if mail.IsClaimed {
		return &proto.CommonResponse{
			Code:    1007,
			Message: "奖励已领取",
		}, nil
	}

	// TODO: 这里应该调用背包系统或物品系统来发放奖励
	// 目前只是简单标记为已领取
	for _, reward := range mail.Rewards {
		log.Printf("发放奖励给用户 %d: 物品ID=%d", toUserID, reward.ItemID)
		// 实际项目中这里需要调用物品系统API来发放奖励
	}

	// 标记奖励为已领取
	if err := ms.server.mailRepo.UpdateMailClaimStatus(claimReq.MailId, true); err != nil {
		log.Printf("更新邮件领取状态失败: %v", err)
		return &proto.CommonResponse{
			Code:    1008,
			Message: "更新邮件状态失败",
		}, nil
	}

	// 如果邮件未读，同时标记为已读
	if !mail.IsRead {
		ms.server.mailRepo.UpdateMailReadStatus(claimReq.MailId, true)
	}

	log.Printf("用户 %d 领取邮件 %d 奖励成功，奖励数量: %d", toUserID, claimReq.MailId, len(mail.Rewards))

	return &proto.CommonResponse{
		Code:    0,
		Message: "奖励领取成功",
		Data:    []byte(fmt.Sprintf("{\"rewards_count\":%d}", len(mail.Rewards))),
	}, nil
}

// DeleteMail 删除邮件
func (ms *MailService) DeleteMail(ctx context.Context, req *proto.MailOperationRequest) (*proto.CommonResponse, error) {
	// 验证用户ID
	userID := ctx.Value("user_id")
	if userID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	toUserID := userID.(uint64)

	// 解析请求数据
	deleteReq := req

	// 验证邮件ID
	if deleteReq.MailId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "邮件ID不能为空",
		}, nil
	}

	// 获取邮件信息
	mail, err := ms.server.mailRepo.GetMailByID(deleteReq.MailId)
	if err != nil {
		log.Printf("获取邮件信息失败: %v", err)
		return &proto.CommonResponse{
			Code:    1003,
			Message: "邮件不存在",
		}, nil
	}

	// 检查邮件是否属于当前用户
	if mail.ToUserID != toUserID {
		return &proto.CommonResponse{
			Code:    1004,
			Message: "无权限删除此邮件",
		}, nil
	}

	// 检查是否有未领取的奖励
	if len(mail.Rewards) > 0 && !mail.IsClaimed {
		return &proto.CommonResponse{
			Code:    1005,
			Message: "邮件有未领取的奖励，无法删除",
		}, nil
	}

	// 删除邮件
	if err := ms.server.mailRepo.DeleteMail(deleteReq.MailId); err != nil {
		log.Printf("删除邮件失败: %v", err)
		if err.Error() == "邮件不存在" {
			return &proto.CommonResponse{
				Code:    1003,
				Message: "邮件不存在",
			}, nil
		}
		return &proto.CommonResponse{
			Code:    1006,
			Message: "删除邮件失败",
		}, nil
	}

	log.Printf("用户 %d 删除邮件 %d 成功", toUserID, deleteReq.MailId)

	return &proto.CommonResponse{
		Code:    0,
		Message: "邮件删除成功",
	}, nil
}

// SendMail 发送邮件
func (ms *MailService) SendMail(ctx context.Context, req *proto.SendMailRequest) (*proto.CommonResponse, error) {
	// 验证用户ID
	userID := ctx.Value("user_id")
	if userID == nil {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "用户未登录",
		}, nil
	}

	fromUserID := userID.(uint64)

	// 解析请求数据
	sendReq := req

	// 验证收件人ID
	if sendReq.ToUserId == 0 {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "收件人ID不能为空",
		}, nil
	}

	// 不能给自己发邮件
	if sendReq.ToUserId == fromUserID {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "不能给自己发邮件",
		}, nil
	}

	// 验证邮件标题
	if sendReq.Title == "" {
		return &proto.CommonResponse{
			Code:    1004,
			Message: "邮件标题不能为空",
		}, nil
	}

	// 验证邮件内容
	if sendReq.Content == "" {
		return &proto.CommonResponse{
			Code:    1005,
			Message: "邮件内容不能为空",
		}, nil
	}

	// TODO: 检查收件人是否存在
	logger.Debug(fmt.Sprintf("Checking if user %d exists", sendReq.ToUserId))

	// 生成邮件ID
	mailID := ms.server.generateMailID()

	// 转换奖励列表
	rewards := make([]database.MailReward, 0, len(sendReq.Rewards))
	for _, reward := range sendReq.Rewards {
		mailReward := database.MailReward{
			ItemID: int32(reward.ItemId),
			// TODO: 添加其他奖励字段
		}
		rewards = append(rewards, mailReward)
	}

	// TODO: 计算过期时间
	// 简化实现：暂时不设置过期时间}

	// 创建邮件
	mail := &database.Mail{
		MailID:     mailID,
		FromUserID: fromUserID,
		ToUserID:   sendReq.ToUserId,
		// TODO: 添加邮件类型字段
		Title:     sendReq.Title,
		Content:   sendReq.Content,
		Rewards:   rewards,
		IsRead:    false,
		IsClaimed: false,
		// TODO: 添加时间字段
	}

	// 保存邮件到数据库
	if err := ms.server.mailRepo.CreateMail(mail); err != nil {
		log.Printf("保存邮件失败: %v", err)
		return &proto.CommonResponse{
			Code:    1007,
			Message: "发送邮件失败",
		}, nil
	}

	// TODO: 这里可以发送邮件通知给收件人
	// 比如通过推送系统通知用户有新邮件

	log.Printf("用户 %d 发送邮件给用户 %d 成功，邮件ID: %d", fromUserID, sendReq.ToUserId, mailID)

	return &proto.CommonResponse{
		Code:    0,
		Message: "邮件发送成功",
		Data:    []byte(fmt.Sprintf("{\"mail_id\":%d}", mailID)),
	}, nil
}
