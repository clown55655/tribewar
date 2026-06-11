package server

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"runtime"
	"time"

	"tribeway/internal/discovery"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// CenterServer 中心服务器
type CenterServer struct {
	*BaseServer
}

// NewCenterServer 创建中心服务器
func NewCenterServer(configFile, nodeID string) *CenterServer {
	centerServer, err := NewCenterServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create center server: %v", err))
	}
	return centerServer
}

func NewCenterServerWithError(configFile, nodeID string) (*CenterServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "center", nodeID, CenterComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

	centerServer := &CenterServer{
		BaseServer: baseServer,
	}

	if err := RegisterCommonServices(baseServer); err != nil {
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	centerService := NewCenterService(centerServer)
	if err := baseServer.rpcServer.RegisterService(centerService); err != nil {
		return nil, fmt.Errorf("failed to register center service: %v", err)
	}

	constructed = true
	return centerServer, nil
}

// managementLoop 管理循环
func (cs *CenterServer) Start() error {
	if err := cs.BaseServer.Start(); err != nil {
		return err
	}

	cs.wg.Add(1)
	go cs.managementLoop()
	return nil
}

func (cs *CenterServer) managementLoop() {
	defer cs.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 执行定期管理任务
			cs.performHealthChecks()
			cs.collectStatistics()

		case <-cs.ctx.Done():
			return
		}
	}
}

// performHealthChecks 执行健康检查
func (cs *CenterServer) performHealthChecks() {
	// 获取所有注册的服务
	serviceTypes := []string{"gateway", "login", "lobby", "game", "friend", "chat", "mail", "gm"}

	for _, serviceType := range serviceTypes {
		services, err := cs.registry.GetServices(serviceType)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get services for %s: %v", serviceType, err))
			continue
		}

		logger.Debug(fmt.Sprintf("Health check for %s: %d services online", serviceType, len(services)))
	}
}

// collectStatistics 收集统计信息
func (cs *CenterServer) collectStatistics() {
	// TODO: 实现统计信息收集
	logger.Debug("Collecting server statistics")
}

// CenterService 中心RPC服务
type CenterService struct {
	server *CenterServer
}

// NewCenterService 创建中心服务
func NewCenterService(server *CenterServer) *CenterService {
	return &CenterService{
		server: server,
	}
}

// GetName 获取服务名称
func (cs *CenterService) GetName() string {
	return "CenterService"
}

// RegisterMethods 注册方法
func (cs *CenterService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["GetServiceList"] = reflect.ValueOf(cs.GetServiceList)
	methods["GetClusterStatus"] = reflect.ValueOf(cs.GetClusterStatus)
	methods["BroadcastMessage"] = reflect.ValueOf(cs.BroadcastMessage)
	methods["ShutdownService"] = reflect.ValueOf(cs.ShutdownService)
	methods["RestartService"] = reflect.ValueOf(cs.RestartService)

	return methods
}

// GetServiceList 获取服务列表
func (cs *CenterService) GetServiceList(ctx context.Context, req *proto.BaseRequest) (*proto.ServiceListResponse, error) {
	serviceTypes := []string{"gateway", "login", "lobby", "game", "friend", "chat", "mail", "gm", "center"}
	allServices := make([]*discovery.ServiceInfo, 0)

	for _, serviceType := range serviceTypes {
		services, err := cs.server.registry.GetServices(serviceType)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get services for %s: %v", serviceType, err))
			continue
		}
		allServices = append(allServices, services...)
	}

	// 转换为proto格式
	protoServices := make([]*proto.ServiceInfo, 0, len(allServices))
	for _, service := range allServices {
		// 获取端口
		port := service.Port
		status := "online"
		if time.Now().Unix()-service.UpdateTime > 60 {
			status = "offline"
		}

		protoService := &proto.ServiceInfo{
			ServiceId:     service.NodeID,
			ServiceType:   service.NodeType,
			Address:       service.Address,
			Port:          int32(port),
			Status:        status,
			LastHeartbeat: uint32(service.UpdateTime),
		}
		protoServices = append(protoServices, protoService)
	}

	log.Printf("获取服务列表成功，共 %d 个服务", len(protoServices))

	return &proto.ServiceListResponse{
		Services: protoServices,
		Total:    int32(len(protoServices)),
	}, nil
}

// GetClusterStatus 获取集群状态
func (cs *CenterService) GetClusterStatus(ctx context.Context, req *proto.BaseRequest) (*proto.ClusterStatusResponse, error) {
	serviceTypes := []string{"gateway", "login", "lobby", "game", "friend", "chat", "mail", "gm", "center"}
	allServices := make([]*discovery.ServiceInfo, 0)

	for _, serviceType := range serviceTypes {
		services, err := cs.server.registry.GetServices(serviceType)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get services for %s: %v", serviceType, err))
			continue
		}
		allServices = append(allServices, services...)
	}

	serviceStats := make(map[string]int32)
	totalCount := int32(0)
	onlineCount := int32(0)

	// 统计服务类型
	for _, service := range allServices {
		if time.Now().Unix()-service.UpdateTime <= 60 {
			onlineCount++
			if count, exists := serviceStats[service.NodeType]; exists {
				serviceStats[service.NodeType] = count + 1
			} else {
				serviceStats[service.NodeType] = 1
			}
		}
		totalCount++
	}

	// 获取系统信息
	systemInfo := cs.getSystemInfo()

	log.Printf("获取集群状态成功，总服务数: %d，在线服务数: %d", totalCount, onlineCount)

	return &proto.ClusterStatusResponse{
		TotalServices:  totalCount,
		OnlineServices: onlineCount,
		ServiceStats:   serviceStats,
		SystemInfo:     systemInfo,
	}, nil
}

// getSystemInfo 获取系统信息
func (cs *CenterService) getSystemInfo() *proto.SystemInfo {
	// 获取内存统计
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 计算内存使用率（简化计算）
	memoryUsage := float32(memStats.Alloc) / float32(memStats.Sys) * 100
	if memoryUsage > 100 {
		memoryUsage = 100
	}

	// 获取运行时间（从服务器启动开始）
	uptime := uint32(time.Since(time.Now().Add(-time.Hour)).Seconds()) // 临时使用1小时作为启动时间

	return &proto.SystemInfo{
		CpuUsage:    0.0, // TODO: 实现CPU使用率获取
		MemoryUsage: memoryUsage,
		DiskUsage:   0.0, // TODO: 实现磁盘使用率获取
		Uptime:      uptime,
	}
}

// BroadcastMessage 广播消息
func (cs *CenterService) BroadcastMessage(ctx context.Context, req *proto.BroadcastMessageRequest) (*proto.CommonResponse, error) {
	// 解析请求数据
	broadcastReq := req

	// 验证消息类型
	if broadcastReq.MessageType == "" {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "消息类型不能为空",
		}, nil
	}

	// 验证消息内容
	if broadcastReq.Content == "" {
		return &proto.CommonResponse{
			Code:    1002,
			Message: "消息内容不能为空",
		}, nil
	}

	// 构造广播消息
	messageData := map[string]interface{}{
		"type":      broadcastReq.MessageType,
		"content":   broadcastReq.Content,
		"timestamp": time.Now().Unix(),
		"from":      "center_server",
	}

	var targetCount int

	// 根据目标服务进行广播
	if len(broadcastReq.TargetServices) > 0 {
		// 广播给指定服务类型
		for _, serviceType := range broadcastReq.TargetServices {
			services, err := cs.server.registry.GetServices(serviceType)
			if err != nil {
				log.Printf("获取服务类型 %s 失败: %v", serviceType, err)
				continue
			}

			// 向该类型的所有服务发送消息
			for _, service := range services {
				if time.Now().Unix()-service.UpdateTime <= 60 {
					cs.server.messageBroker.SendToNode(service.NodeID, broadcastReq.MessageType, messageData)
					targetCount++
				}
			}
		}
	} else {
		// 广播给所有在线服务
		cs.server.messageBroker.BroadcastSystemMessage(broadcastReq.MessageType, messageData)
		targetCount = -1 // -1表示全服广播
	}

	log.Printf("广播消息成功，消息类型: %s，目标服务数: %d", broadcastReq.MessageType, targetCount)

	return &proto.CommonResponse{
		Code:    0,
		Message: "广播消息发送成功",
		Data:    []byte(fmt.Sprintf("{\"target_count\":%d,\"message_type\":\"%s\"}", targetCount, broadcastReq.MessageType)),
	}, nil
}

// ShutdownService 关闭服务
func (cs *CenterService) ShutdownService(ctx context.Context, req *proto.ServiceOperationRequest) (*proto.CommonResponse, error) {
	// 解析请求数据
	shutdownReq := req

	// 验证服务ID或服务类型
	if shutdownReq.ServiceId == "" && shutdownReq.ServiceType == "" {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "服务ID或服务类型不能为空",
		}, nil
	}

	var targetServices []*discovery.ServiceInfo
	var err error

	// 根据服务ID或服务类型获取目标服务
	if shutdownReq.ServiceId != "" {
		// 通过服务ID查找特定服务
		serviceTypes := []string{"gateway", "login", "lobby", "game", "friend", "chat", "mail", "gm"}
		for _, serviceType := range serviceTypes {
			services, err := cs.server.registry.GetServices(serviceType)
			if err != nil {
				continue
			}
			for _, service := range services {
				if service.NodeID == shutdownReq.ServiceId {
					targetServices = append(targetServices, service)
					break
				}
			}
		}
	} else {
		// 通过服务类型获取所有该类型的服务
		targetServices, err = cs.server.registry.GetServices(shutdownReq.ServiceType)
		if err != nil {
			log.Printf("获取服务类型 %s 失败: %v", shutdownReq.ServiceType, err)
			return &proto.CommonResponse{
				Code:    1002,
				Message: "获取目标服务失败",
			}, nil
		}
	}

	if len(targetServices) == 0 {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "未找到目标服务",
		}, nil
	}

	// 发送关闭命令
	successCount := 0
	for _, service := range targetServices {
		if time.Now().Unix()-service.UpdateTime <= 120 {
			cs.server.messageBroker.SendToNode(service.NodeID, "shutdown", map[string]interface{}{
				"reason":    "管理员关闭",
				"timestamp": time.Now().Unix(),
			})
			successCount++
			log.Printf("发送关闭命令给服务 %s (%s)", service.NodeID, service.NodeType)
		}
	}

	return &proto.CommonResponse{
		Code:    0,
		Message: fmt.Sprintf("关闭命令已发送给 %d 个服务", successCount),
		Data:    []byte(fmt.Sprintf("{\"target_count\":%d}", successCount)),
	}, nil
}

// RestartService 重启服务
func (cs *CenterService) RestartService(ctx context.Context, req *proto.ServiceOperationRequest) (*proto.CommonResponse, error) {
	// 解析请求数据
	restartReq := req

	// 验证服务ID或服务类型
	if restartReq.ServiceId == "" && restartReq.ServiceType == "" {
		return &proto.CommonResponse{
			Code:    1001,
			Message: "服务ID或服务类型不能为空",
		}, nil
	}

	var targetServices []*discovery.ServiceInfo
	var err error

	// 根据服务ID或服务类型获取目标服务
	if restartReq.ServiceId != "" {
		// 通过服务ID查找特定服务
		serviceTypes := []string{"gateway", "login", "lobby", "game", "friend", "chat", "mail", "gm"}
		for _, serviceType := range serviceTypes {
			services, err := cs.server.registry.GetServices(serviceType)
			if err != nil {
				continue
			}
			for _, service := range services {
				if service.NodeID == restartReq.ServiceId {
					targetServices = append(targetServices, service)
					break
				}
			}
		}
	} else {
		// 通过服务类型获取所有该类型的服务
		targetServices, err = cs.server.registry.GetServices(restartReq.ServiceType)
		if err != nil {
			log.Printf("获取服务类型 %s 失败: %v", restartReq.ServiceType, err)
			return &proto.CommonResponse{
				Code:    1002,
				Message: "获取目标服务失败",
			}, nil
		}
	}

	if len(targetServices) == 0 {
		return &proto.CommonResponse{
			Code:    1003,
			Message: "未找到目标服务",
		}, nil
	}

	// 发送重启命令
	successCount := 0
	for _, service := range targetServices {
		if time.Now().Unix()-service.UpdateTime <= 120 {
			cs.server.messageBroker.SendToNode(service.NodeID, "restart", map[string]interface{}{
				"reason":    "管理员重启",
				"timestamp": time.Now().Unix(),
			})
			successCount++
			log.Printf("发送重启命令给服务 %s (%s)", service.NodeID, service.NodeType)
		}
	}

	return &proto.CommonResponse{
		Code:    0,
		Message: fmt.Sprintf("重启命令已发送给 %d 个服务", successCount),
		Data:    []byte(fmt.Sprintf("{\"target_count\":%d}", successCount)),
	}, nil
}
