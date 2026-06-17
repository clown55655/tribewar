package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"tribeway/internal/actor"
	"tribeway/internal/database"
	"tribeway/internal/discovery"
	"tribeway/internal/logger"
	"tribeway/internal/mq"
	"tribeway/internal/network"
	"tribeway/internal/rpc"
)

// Server 服务器接口
type Server interface {
	Start() error
	Stop() error
	GetNodeID() string
	GetNodeType() string
	GetStatus() string
}

// ComponentOptions 声明当前节点需要初始化的基础组件。
type ComponentOptions struct {
	ActorSystem      bool
	RPCServer        bool
	RPCClient        bool
	Redis            bool
	MongoDB          bool
	NSQ              bool
	MessageBroker    bool
	Registry         bool
	ServiceDiscovery bool
}

// DefaultComponentOptions 保持旧 NewBaseServer 的全量组件初始化行为。
func DefaultComponentOptions() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func GatewayComponents() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func LoginComponents() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func MongoServiceComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:        true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func LobbyComponents() ComponentOptions {
	return MongoServiceComponents()
}

func GameComponents() ComponentOptions {
	return MongoServiceComponents()
}

func FriendComponents() ComponentOptions {
	return MongoServiceComponents()
}

func ChatComponents() ComponentOptions {
	return MongoServiceComponents()
}

func MailComponents() ComponentOptions {
	return MongoServiceComponents()
}

func GMComponents() ComponentOptions {
	return MongoServiceComponents()
}

func CenterComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:     true,
		NSQ:           true,
		MessageBroker: true,
		Registry:      true,
	}
}

func EnhancedGameComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:        true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

// BaseServer 基础服务器实现
type BaseServer struct {
	config   *ServerConfig
	nodeType string
	nodeID   string
	status   string

	// 组件
	actorSystem   *actor.ActorSystem
	tcpServer     *network.TCPServer
	rpcServer     *rpc.RPCServer
	rpcClient     *rpc.RPCClient
	redisManager  *database.RedisManager
	mongoManager  *database.MongoManager
	nsqManager    *mq.NSQManager
	messageBroker *mq.MessageBroker
	discovery     *discovery.ServiceDiscovery
	registry      *discovery.ETCDRegistry

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mutex  sync.RWMutex
}

// NewBaseServer 创建基础服务器
func NewBaseServer(configFile, nodeType, nodeID string) (*BaseServer, error) {
	return NewBaseServerWithOptions(configFile, nodeType, nodeID, DefaultComponentOptions())
}

// NewBaseServerWithOptions 按需创建基础服务器。
func NewBaseServerWithOptions(configFile, nodeType, nodeID string, opts ComponentOptions) (*BaseServer, error) {
	// 加载配置
	config, err := loadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	// 初始化日志
	logger.InitGlobalLogger(&config.Log)

	ctx, cancel := context.WithCancel(context.Background())

	server := &BaseServer{
		config:   config,
		nodeType: nodeType,
		nodeID:   nodeID,
		status:   "initializing",
		ctx:      ctx,
		cancel:   cancel,
	}

	// 初始化组件
	if err := server.initComponents(opts); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to init components: %v", err)
	}

	logger.Info(fmt.Sprintf("Server %s/%s initialized", nodeType, nodeID))
	return server, nil
}

// initComponents 初始化组件
func (bs *BaseServer) initComponents(opts ComponentOptions) error {
	if opts.ActorSystem {
		bs.actorSystem = actor.NewActorSystem(fmt.Sprintf("%s-%s", bs.nodeType, bs.nodeID))
	}

	if opts.Redis {
		redisManager, err := database.NewRedisManager(&bs.config.Database.Redis)
		if err != nil {
			return fmt.Errorf("failed to init redis: %v", err)
		}
		bs.redisManager = redisManager
	}

	if opts.MongoDB {
		mongoManager, err := database.NewMongoManager(&bs.config.Database.MongoDB)
		if err != nil {
			return fmt.Errorf("failed to init mongodb: %v", err)
		}
		bs.mongoManager = mongoManager
	}

	if opts.NSQ {
		nsqManager, err := mq.NewNSQManager(&bs.config.NSQ)
		if err != nil {
			return fmt.Errorf("failed to init nsq: %v", err)
		}
		bs.nsqManager = nsqManager
	}

	if opts.MessageBroker {
		if bs.nsqManager == nil {
			return fmt.Errorf("message broker requires nsq manager")
		}
		bs.messageBroker = mq.NewMessageBroker(bs.nsqManager, bs.nodeID)
	}

	if opts.Registry {
		registry, err := discovery.NewETCDRegistry(&bs.config.ETCD)
		if err != nil {
			return fmt.Errorf("failed to init etcd registry: %v", err)
		}
		bs.registry = registry
	}

	if opts.ServiceDiscovery {
		if bs.registry == nil {
			return fmt.Errorf("service discovery requires registry")
		}
		bs.discovery = discovery.NewServiceDiscovery(
			bs.registry,
			bs.nodeType,
			discovery.NewWeightedLoadBalancer(),
		)
	}

	if opts.RPCServer {
		bs.rpcServer = rpc.NewRPCServer("0.0.0.0", bs.config.Network.RPCPort)
		bs.rpcServer.SetFrameOptions(
			time.Duration(bs.config.Network.ReadTimeout)*time.Second,
			time.Duration(bs.config.Network.WriteTimeout)*time.Second,
			bs.maxPacketSize(),
		)
	}

	return nil
}

func (bs *BaseServer) maxPacketSize() uint32 {
	if bs.config.Network.MaxPacketSize <= 0 {
		return network.DefaultMaxFrame
	}
	return uint32(bs.config.Network.MaxPacketSize)
}

func (bs *BaseServer) authTokenSecret() ([]byte, error) {
	auth := bs.config.Security.Auth
	envName := auth.TokenSecretEnv
	if envName == "" {
		envName = "TRIBEWAY_TOKEN_SECRET"
	}
	if secret := os.Getenv(envName); secret != "" {
		return []byte(secret), nil
	}
	if auth.TokenSecret != "" {
		return []byte(auth.TokenSecret), nil
	}

	return nil, fmt.Errorf("auth token secret is required; set %s or security.auth.token_secret", envName)
}

func (bs *BaseServer) authTokenExpiry() time.Duration {
	hours := bs.config.Security.Auth.TokenExpiryHours
	if hours <= 0 {
		hours = 2
	}
	return time.Duration(hours) * time.Hour
}

func (bs *BaseServer) advertiseAddress() string {
	envName := bs.config.Network.AdvertiseAddressEnv
	if envName == "" {
		envName = "TRIBEWAY_ADVERTISE_ADDRESS"
	}
	if address := strings.TrimSpace(os.Getenv(envName)); address != "" {
		return address
	}
	if address := strings.TrimSpace(bs.config.Network.AdvertiseAddress); address != "" && address != "0.0.0.0" {
		return address
	}
	if address := firstNonLoopbackIPv4(); address != "" {
		return address
	}
	return "127.0.0.1"
}

func firstNonLoopbackIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				return ipv4.String()
			}
		}
	}
	return ""
}

// Start 启动服务器
func (bs *BaseServer) Start() error {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if bs.status != "initializing" {
		return fmt.Errorf("server already started")
	}

	logger.Info(fmt.Sprintf("Starting server %s/%s", bs.nodeType, bs.nodeID))

	if bs.rpcServer != nil {
		if err := bs.rpcServer.Start(); err != nil {
			return fmt.Errorf("failed to start rpc server: %v", err)
		}
	}

	if bs.registry != nil {
		advertiseAddress := bs.advertiseAddress()
		serviceInfo := &discovery.ServiceInfo{
			NodeID:     bs.nodeID,
			NodeType:   bs.nodeType,
			Address:    advertiseAddress,
			Port:       bs.config.Network.RPCPort,
			Load:       0,
			Status:     "online",
			Metadata:   map[string]string{},
			UpdateTime: time.Now().Unix(),
		}

		if err := bs.registry.Register(serviceInfo); err != nil {
			return fmt.Errorf("failed to register service: %v", err)
		}

		bs.wg.Add(1)
		go bs.loadUpdateLoop()
	}

	bs.status = "running"
	logger.Info(fmt.Sprintf("Server %s/%s started", bs.nodeType, bs.nodeID))

	return nil
}

// Stop 停止服务器
func (bs *BaseServer) Stop() error {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if bs.status != "running" {
		return nil
	}

	logger.Info(fmt.Sprintf("Stopping server %s/%s", bs.nodeType, bs.nodeID))

	bs.status = "stopping"
	bs.cancel()

	// 停止组件
	if bs.tcpServer != nil {
		bs.tcpServer.Stop()
	}

	if bs.rpcServer != nil {
		bs.rpcServer.Stop()
	}

	if bs.actorSystem != nil {
		bs.actorSystem.Shutdown()
	}

	if bs.nsqManager != nil {
		bs.nsqManager.Close()
	}

	if bs.registry != nil {
		bs.registry.Unregister(bs.nodeID)
		bs.registry.Close()
	}

	if bs.redisManager != nil {
		bs.redisManager.Close()
	}

	if bs.mongoManager != nil {
		bs.mongoManager.Close()
	}

	// 等待所有goroutine结束
	bs.wg.Wait()

	bs.status = "stopped"
	logger.Info(fmt.Sprintf("Server %s/%s stopped", bs.nodeType, bs.nodeID))

	return nil
}

// GetNodeID 获取节点ID
func (bs *BaseServer) GetNodeID() string {
	return bs.nodeID
}

// GetNodeType 获取节点类型
func (bs *BaseServer) GetNodeType() string {
	return bs.nodeType
}

// GetStatus 获取状态
func (bs *BaseServer) GetStatus() string {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	return bs.status
}

// loadUpdateLoop 负载更新循环
func (bs *BaseServer) loadUpdateLoop() {
	defer bs.wg.Done()

	if bs.registry == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 计算当前负载
			load := bs.calculateLoad()

			// 更新服务注册信息
			if err := bs.registry.UpdateLoad(bs.nodeID, load); err != nil {
				logger.Error(fmt.Sprintf("Failed to update load: %v", err))
			}

		case <-bs.ctx.Done():
			return
		}
	}
}

// calculateLoad 计算当前负载
func (bs *BaseServer) calculateLoad() int {
	// 基础负载计算：连接数 + Actor数量
	load := 0

	if bs.tcpServer != nil {
		load += bs.tcpServer.GetConnectionCount()
	}

	if bs.actorSystem != nil {
		load += bs.actorSystem.GetActorCount()
	}

	// 如果有RPC服务器，加上连接数
	if bs.rpcServer != nil {
		load += int(bs.rpcServer.GetConnectionCount())
	}

	return load
}

// GetActorSystem 获取Actor系统
func (bs *BaseServer) GetActorSystem() *actor.ActorSystem {
	return bs.actorSystem
}

// GetRedisManager 获取Redis管理器
func (bs *BaseServer) GetRedisManager() *database.RedisManager {
	return bs.redisManager
}

// GetMongoManager 获取MongoDB管理器
func (bs *BaseServer) GetMongoManager() *database.MongoManager {
	return bs.mongoManager
}

// GetMessageBroker 获取消息代理
func (bs *BaseServer) GetMessageBroker() *mq.MessageBroker {
	return bs.messageBroker
}

// GetDiscovery 获取服务发现
func (bs *BaseServer) GetDiscovery() *discovery.ServiceDiscovery {
	return bs.discovery
}

// NewServer 创建新服务器
func NewServer(configFile, nodeType, nodeID string) Server {
	switch nodeType {
	case "gateway":
		return NewGatewayServer(configFile, nodeID)
	case "login":
		return NewLoginServer(configFile, nodeID)
	case "lobby":
		return NewLobbyServer(configFile, nodeID)
	case "game":
		return NewGameServer(configFile, nodeID)
	case "enhanced_game":
		return NewEnhancedGameServer(configFile, nodeID)
	case "friend":
		return NewFriendServer(configFile, nodeID)
	case "chat":
		return NewChatServer(configFile, nodeID)
	case "mail":
		return NewMailServer(configFile, nodeID)
	case "gm":
		return NewGMServer(configFile, nodeID)
	case "center":
		return NewCenterServer(configFile, nodeID)
	default:
		logger.Fatal(fmt.Sprintf("Unknown node type: %s", nodeType))
		return nil
	}
}

// RegisterCommonServices 注册通用服务
func RegisterCommonServices(server *BaseServer) error {
	var systemService *SystemService
	if server.rpcServer != nil {
		systemService = NewSystemService(server)
		if err := server.rpcServer.RegisterService(systemService); err != nil {
			return fmt.Errorf("failed to register system service: %v", err)
		}
	}

	if server.messageBroker != nil && systemService != nil {
		systemHandler := mq.NewSystemMessageHandler(server.nodeID)
		systemHandler.RegisterHandler(mq.SYS_CMD_RELOAD_CONFIG, systemService.HandleReloadConfig)
		systemHandler.RegisterHandler(mq.SYS_CMD_UPDATE_LOAD, systemService.HandleUpdateLoad)
		systemHandler.RegisterHandler(mq.SYS_CMD_SHUTDOWN, systemService.HandleShutdown)
		systemHandler.RegisterHandler(mq.SYS_CMD_HOT_UPDATE, systemService.HandleHotUpdate)

		if err := server.messageBroker.SubscribeSystemMessages(systemHandler); err != nil {
			return fmt.Errorf("failed to subscribe system messages: %v", err)
		}
	}

	return nil
}
