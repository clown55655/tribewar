package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nsqio/go-nsq"

	"tribeway/internal/logger"
)

const (
	defaultNSQDAddress        = "127.0.0.1:4150"
	defaultNSQLookupDAddress  = "127.0.0.1:4161"
	defaultNSQMaxInFlight     = 200
	defaultNSQDialTimeout     = time.Second
	defaultNSQReadTimeout     = 60 * time.Second
	defaultNSQWriteTimeout    = time.Second
	defaultNSQMessageTimeout  = 60 * time.Second
	defaultNSQHealthCheck     = 30 * time.Second
	defaultNSQProducerPool    = 1
	minNSQNetworkReadTimeout  = 100 * time.Millisecond
	minNSQNetworkWriteTimeout = 100 * time.Millisecond
)

// NSQConfig NSQ配置
type NSQConfig struct {
	// 单节点模式
	NSQDAddress       string `yaml:"nsqd_address"`
	NSQLookupDAddress string `yaml:"nsqlookupd_address"`

	// 集群模式
	ClusterMode         bool     `yaml:"cluster_mode"`
	NSQDAddresses       []string `yaml:"nsqd_addresses"`
	NSQLookupDAddresses []string `yaml:"nsqlookupd_addresses"`

	// 连接配置
	MaxInFlight    int           `yaml:"max_in_flight"`
	DialTimeout    time.Duration `yaml:"dial_timeout"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	MessageTimeout time.Duration `yaml:"message_timeout"`

	// 集群配置
	LoadBalancing       bool          `yaml:"load_balancing"`        // 负载均衡
	FailoverEnabled     bool          `yaml:"failover_enabled"`      // 故障转移
	HealthCheckInterval time.Duration `yaml:"health_check_interval"` // 健康检查间隔
	ProducerPoolSize    int           `yaml:"producer_pool_size"`    // 生产者池大小
}

// MessageHandler 消息处理器接口
type MessageHandler interface {
	HandleMessage(topic, channel string, data []byte) error
}

// NSQManager NSQ管理器
type NSQManager struct {
	config          *NSQConfig
	producers       []*nsq.Producer // 支持多个生产者（集群模式）
	producer        *nsq.Producer   // 主生产者（兼容性）
	consumers       map[string]*nsq.Consumer
	handlers        map[string]MessageHandler
	mutex           sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	mode            string // "single", "cluster"
	currentProducer int    // 当前使用的生产者索引（轮询）
}

// NewNSQManager 创建NSQ管理器
func NewNSQManager(config *NSQConfig) (*NSQManager, error) {
	if config == nil {
		return nil, fmt.Errorf("nsq config is required")
	}
	normalizedConfig := *config
	normalizedConfig.normalize()

	ctx, cancel := context.WithCancel(context.Background())

	manager := &NSQManager{
		config:    &normalizedConfig,
		consumers: make(map[string]*nsq.Consumer),
		handlers:  make(map[string]MessageHandler),
		ctx:       ctx,
		cancel:    cancel,
		producers: make([]*nsq.Producer, 0),
	}

	var err error
	if config.ClusterMode {
		manager.mode = "cluster"
		err = manager.initClusterMode()
	} else {
		manager.mode = "single"
		err = manager.initSingleMode()
	}

	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize NSQ: %v", err)
	}

	logger.Infof("NSQ manager initialized in %s mode", manager.mode)
	return manager, nil
}

func (config *NSQConfig) normalize() {
	if config.NSQDAddress == "" {
		config.NSQDAddress = defaultNSQDAddress
	}
	if config.NSQLookupDAddress == "" {
		config.NSQLookupDAddress = defaultNSQLookupDAddress
	}
	if config.MaxInFlight <= 0 {
		config.MaxInFlight = defaultNSQMaxInFlight
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultNSQDialTimeout
	}
	if config.ReadTimeout < minNSQNetworkReadTimeout {
		config.ReadTimeout = defaultNSQReadTimeout
	}
	if config.WriteTimeout < minNSQNetworkWriteTimeout {
		config.WriteTimeout = defaultNSQWriteTimeout
	}
	if config.MessageTimeout <= 0 {
		config.MessageTimeout = defaultNSQMessageTimeout
	}
	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = defaultNSQHealthCheck
	}
	if config.ProducerPoolSize <= 0 {
		config.ProducerPoolSize = defaultNSQProducerPool
	}
}

// initSingleMode 初始化单节点模式
func (nm *NSQManager) initSingleMode() error {
	producer, err := nsq.NewProducer(nm.config.NSQDAddress, nm.newProducerConfig())
	if err != nil {
		return fmt.Errorf("failed to create NSQ producer: %v", err)
	}

	// 测试连接
	if err := producer.Ping(); err != nil {
		producer.Stop()
		return fmt.Errorf("failed to ping NSQ: %v", err)
	}

	nm.producer = producer
	nm.producers = append(nm.producers, producer)

	logger.Infof("NSQ single mode initialized: %s", nm.config.NSQDAddress)
	return nil
}

// initClusterMode 初始化集群模式
func (nm *NSQManager) initClusterMode() error {
	if len(nm.config.NSQDAddresses) == 0 {
		return fmt.Errorf("NSQD addresses not configured for cluster mode")
	}

	// 为每个NSQD节点创建生产者
	for _, addr := range nm.config.NSQDAddresses {
		producer, err := nsq.NewProducer(addr, nm.newProducerConfig())
		if err != nil {
			// 清理已创建的生产者
			nm.closeProducers()
			return fmt.Errorf("failed to create NSQ producer for %s: %v", addr, err)
		}

		// 测试连接
		if err := producer.Ping(); err != nil {
			producer.Stop()
			// 如果不是故障转移模式，则失败
			if !nm.config.FailoverEnabled {
				nm.closeProducers()
				return fmt.Errorf("failed to ping NSQ %s: %v", addr, err)
			}
			logger.Warnf("NSQD %s unavailable, will retry later", addr)
			continue
		}

		nm.producers = append(nm.producers, producer)
		logger.Infof("Connected to NSQD: %s", addr)
	}

	if len(nm.producers) == 0 {
		return fmt.Errorf("no NSQD nodes available")
	}

	// 设置主生产者为第一个可用的
	nm.producer = nm.producers[0]

	logger.Infof("NSQ cluster mode initialized: %d producers", len(nm.producers))
	return nil
}

func (nm *NSQManager) newProducerConfig() *nsq.Config {
	config := nsq.NewConfig()
	config.DialTimeout = nm.config.DialTimeout
	config.ReadTimeout = nm.config.ReadTimeout
	config.WriteTimeout = nm.config.WriteTimeout
	return config
}

func (nm *NSQManager) newConsumerConfig() *nsq.Config {
	config := nm.newProducerConfig()
	config.MaxInFlight = nm.config.MaxInFlight
	config.MsgTimeout = nm.config.MessageTimeout
	return config
}

// closeProducers 关闭所有生产者
func (nm *NSQManager) closeProducers() {
	for _, producer := range nm.producers {
		producer.Stop()
	}
	nm.producers = nm.producers[:0]
}

// Publish 发布消息
func (nm *NSQManager) Publish(topic string, data []byte) error {
	if nm.mode == "cluster" && nm.config.LoadBalancing && len(nm.producers) > 1 {
		return nm.publishWithLoadBalancing(topic, data)
	}
	return nm.producer.Publish(topic, data)
}

// publishWithLoadBalancing 负载均衡发布消息
func (nm *NSQManager) publishWithLoadBalancing(topic string, data []byte) error {
	nm.mutex.Lock()
	// 轮询选择生产者
	producer := nm.producers[nm.currentProducer%len(nm.producers)]
	nm.currentProducer++
	nm.mutex.Unlock()

	err := producer.Publish(topic, data)

	// 如果当前生产者失败且启用了故障转移，尝试其他生产者
	if err != nil && nm.config.FailoverEnabled && len(nm.producers) > 1 {
		for i, fallbackProducer := range nm.producers {
			if fallbackProducer == producer {
				continue // 跳过失败的生产者
			}

			if fallbackErr := fallbackProducer.Publish(topic, data); fallbackErr == nil {
				logger.Warnf("Failover successful: switched from producer %d to %d", nm.currentProducer-1, i)
				return nil
			}
		}
		return fmt.Errorf("all NSQ producers failed: %v", err)
	}

	return err
}

// GetClusterStats 获取集群统计信息
func (nm *NSQManager) GetClusterStats() map[string]interface{} {
	stats := map[string]interface{}{
		"mode":      nm.mode,
		"producers": len(nm.producers),
		"consumers": len(nm.consumers),
	}

	if nm.mode == "cluster" {
		stats["nsqd_addresses"] = nm.config.NSQDAddresses
		stats["load_balancing"] = nm.config.LoadBalancing
		stats["failover_enabled"] = nm.config.FailoverEnabled

		// 生产者健康状态
		producerStatus := make([]map[string]interface{}, len(nm.producers))
		for i, producer := range nm.producers {
			status := map[string]interface{}{
				"index": i,
			}

			// 尝试ping检查健康状态
			if err := producer.Ping(); err == nil {
				status["healthy"] = true
			} else {
				status["healthy"] = false
				status["error"] = err.Error()
			}

			producerStatus[i] = status
		}
		stats["producer_status"] = producerStatus
	}

	return stats
}

// PublishJSON 发布JSON消息
func (nm *NSQManager) PublishJSON(topic string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return nm.Publish(topic, jsonData)
}

// DeferredPublish 延迟发布消息
func (nm *NSQManager) DeferredPublish(topic string, delay time.Duration, data []byte) error {
	return nm.producer.DeferredPublish(topic, delay, data)
}

// Subscribe 订阅主题
func (nm *NSQManager) Subscribe(topic, channel string, handler MessageHandler) error {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	key := fmt.Sprintf("%s_%s", topic, channel)
	if _, exists := nm.consumers[key]; exists {
		return fmt.Errorf("already subscribed to %s/%s", topic, channel)
	}

	consumer, err := nsq.NewConsumer(topic, channel, nm.newConsumerConfig())
	if err != nil {
		return fmt.Errorf("failed to create consumer: %v", err)
	}

	// 设置消息处理器
	consumer.AddHandler(&messageHandlerWrapper{
		handler: handler,
		topic:   topic,
		channel: channel,
	})

	// 连接到NSQLookupd
	if nm.mode == "cluster" && len(nm.config.NSQLookupDAddresses) > 0 {
		// 集群模式：连接到所有NSQLookupd
		for _, addr := range nm.config.NSQLookupDAddresses {
			if err := consumer.ConnectToNSQLookupd(addr); err != nil {
				if !nm.config.FailoverEnabled {
					return fmt.Errorf("failed to connect to NSQLookupd %s: %v", addr, err)
				}
				logger.Warnf("Failed to connect to NSQLookupd %s: %v", addr, err)
			} else {
				logger.Infof("Connected to NSQLookupd: %s", addr)
			}
		}
	} else {
		// 单机模式：连接到单个NSQLookupd
		if err := consumer.ConnectToNSQLookupd(nm.config.NSQLookupDAddress); err != nil {
			return fmt.Errorf("failed to connect to NSQLookupd: %v", err)
		}
	}

	nm.consumers[key] = consumer
	nm.handlers[key] = handler

	logger.Infof("Subscribed to topic: %s, channel: %s", topic, channel)
	return nil
}

// Unsubscribe 取消订阅
func (nm *NSQManager) Unsubscribe(topic, channel string) error {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	key := fmt.Sprintf("%s_%s", topic, channel)
	consumer, exists := nm.consumers[key]
	if !exists {
		return fmt.Errorf("not subscribed to %s/%s", topic, channel)
	}

	consumer.Stop()
	<-consumer.StopChan

	delete(nm.consumers, key)
	delete(nm.handlers, key)

	logger.Info(fmt.Sprintf("Unsubscribed from topic: %s, channel: %s", topic, channel))
	return nil
}

// Close 关闭NSQ管理器
func (nm *NSQManager) Ping() error {
	if nm == nil {
		return fmt.Errorf("nsq manager not initialized")
	}
	nm.mutex.RLock()
	producers := append([]*nsq.Producer(nil), nm.producers...)
	nm.mutex.RUnlock()

	if len(producers) == 0 {
		return fmt.Errorf("no nsq producers available")
	}
	var lastErr error
	for _, producer := range producers {
		if producer == nil {
			continue
		}
		if err := producer.Ping(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no nsq producers available")
}

func (nm *NSQManager) Close() error {
	nm.cancel()

	// 停止所有消费者
	nm.mutex.Lock()
	for key, consumer := range nm.consumers {
		consumer.Stop()
		<-consumer.StopChan
		logger.Debug(fmt.Sprintf("Stopped consumer: %s", key))
	}
	nm.mutex.Unlock()

	// 停止生产者
	nm.producer.Stop()

	logger.Info("NSQ manager closed")
	return nil
}

// messageHandlerWrapper NSQ消息处理器包装器
type messageHandlerWrapper struct {
	handler MessageHandler
	topic   string
	channel string
}

// HandleMessage 实现nsq.Handler接口
func (mhw *messageHandlerWrapper) HandleMessage(message *nsq.Message) error {
	start := time.Now()

	err := mhw.handler.HandleMessage(mhw.topic, mhw.channel, message.Body)

	duration := time.Since(start)
	logger.Debug(fmt.Sprintf("Handled message from %s/%s in %v", mhw.topic, mhw.channel, duration))

	return err
}

// GameMessage 游戏消息
type GameMessage struct {
	Type      string                 `json:"type"`
	RoomID    uint64                 `json:"room_id,omitempty"`
	UserID    uint64                 `json:"user_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// NewGameMessage 创建游戏消息
func NewGameMessage(msgType string, roomID, userID uint64, data map[string]interface{}) *GameMessage {
	return &GameMessage{
		Type:      msgType,
		RoomID:    roomID,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}
}

// GameMessageHandler 游戏消息处理器
type GameMessageHandler struct {
	handlers map[string]func(*GameMessage) error
	mutex    sync.RWMutex
}

// NewGameMessageHandler 创建游戏消息处理器
func NewGameMessageHandler() *GameMessageHandler {
	return &GameMessageHandler{
		handlers: make(map[string]func(*GameMessage) error),
	}
}

// RegisterHandler 注册消息类型处理器
func (gmh *GameMessageHandler) RegisterHandler(msgType string, handler func(*GameMessage) error) {
	gmh.mutex.Lock()
	defer gmh.mutex.Unlock()

	gmh.handlers[msgType] = handler
	logger.Debug(fmt.Sprintf("Registered handler for message type: %s", msgType))
}

// HandleMessage 处理消息
func (gmh *GameMessageHandler) HandleMessage(topic, channel string, data []byte) error {
	var gameMsg GameMessage
	if err := json.Unmarshal(data, &gameMsg); err != nil {
		return fmt.Errorf("failed to unmarshal game message: %v", err)
	}

	gmh.mutex.RLock()
	handler, exists := gmh.handlers[gameMsg.Type]
	gmh.mutex.RUnlock()

	if !exists {
		logger.Warn(fmt.Sprintf("No handler for message type: %s", gameMsg.Type))
		return nil
	}

	return handler(&gameMsg)
}

// ChatMessage 聊天消息
type ChatMessage struct {
	FromUserID uint64 `json:"from_user_id"`
	ToUserID   uint64 `json:"to_user_id"` // 0表示全服聊天
	Channel    int32  `json:"channel"`    // 聊天频道
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
}

// NewChatMessage 创建聊天消息
func NewChatMessage(fromUserID, toUserID uint64, channel int32, content string) *ChatMessage {
	return &ChatMessage{
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Channel:    channel,
		Content:    content,
		Timestamp:  time.Now().Unix(),
	}
}

// ChatMessageHandler 聊天消息处理器
type ChatMessageHandler struct {
	onMessage func(*ChatMessage) error
}

// NewChatMessageHandler 创建聊天消息处理器
func NewChatMessageHandler(onMessage func(*ChatMessage) error) *ChatMessageHandler {
	return &ChatMessageHandler{
		onMessage: onMessage,
	}
}

// HandleMessage 处理消息
func (cmh *ChatMessageHandler) HandleMessage(topic, channel string, data []byte) error {
	var chatMsg ChatMessage
	if err := json.Unmarshal(data, &chatMsg); err != nil {
		return fmt.Errorf("failed to unmarshal chat message: %v", err)
	}

	if cmh.onMessage != nil {
		return cmh.onMessage(&chatMsg)
	}

	return nil
}

// SystemMessage 系统消息
type SystemMessage struct {
	Type      string                 `json:"type"`
	Target    string                 `json:"target,omitempty"` // 目标节点ID，空表示广播
	Command   string                 `json:"command"`
	Args      map[string]interface{} `json:"args,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// NewSystemMessage 创建系统消息
func NewSystemMessage(msgType, target, command string, args map[string]interface{}) *SystemMessage {
	return &SystemMessage{
		Type:      msgType,
		Target:    target,
		Command:   command,
		Args:      args,
		Timestamp: time.Now().Unix(),
	}
}

// SystemMessageHandler 系统消息处理器
type SystemMessageHandler struct {
	nodeID   string
	handlers map[string]func(*SystemMessage) error
	mutex    sync.RWMutex
}

// NewSystemMessageHandler 创建系统消息处理器
func NewSystemMessageHandler(nodeID string) *SystemMessageHandler {
	return &SystemMessageHandler{
		nodeID:   nodeID,
		handlers: make(map[string]func(*SystemMessage) error),
	}
}

// RegisterHandler 注册命令处理器
func (smh *SystemMessageHandler) RegisterHandler(command string, handler func(*SystemMessage) error) {
	smh.mutex.Lock()
	defer smh.mutex.Unlock()

	smh.handlers[command] = handler
	logger.Debug(fmt.Sprintf("Registered system command handler: %s", command))
}

// HandleMessage 处理消息
func (smh *SystemMessageHandler) HandleMessage(topic, channel string, data []byte) error {
	var sysMsg SystemMessage
	if err := json.Unmarshal(data, &sysMsg); err != nil {
		return fmt.Errorf("failed to unmarshal system message: %v", err)
	}

	// 检查消息是否针对当前节点
	if sysMsg.Target != "" && sysMsg.Target != smh.nodeID {
		return nil // 不是发给当前节点的消息
	}

	smh.mutex.RLock()
	handler, exists := smh.handlers[sysMsg.Command]
	smh.mutex.RUnlock()

	if !exists {
		logger.Warn(fmt.Sprintf("No handler for system command: %s", sysMsg.Command))
		return nil
	}

	return handler(&sysMsg)
}

// MessageBroker 消息代理
type MessageBroker struct {
	nsq    *NSQManager
	nodeID string
}

// NewMessageBroker 创建消息代理
func NewMessageBroker(nsq *NSQManager, nodeID string) *MessageBroker {
	return &MessageBroker{
		nsq:    nsq,
		nodeID: nodeID,
	}
}

// PublishGameMessage 发布游戏消息
func (mb *MessageBroker) PublishGameMessage(msgType string, roomID, userID uint64, data map[string]interface{}) error {
	msg := NewGameMessage(msgType, roomID, userID, data)
	return mb.nsq.PublishJSON("game_events", msg)
}

// PublishChatMessage 发布聊天消息
func (mb *MessageBroker) PublishChatMessage(fromUserID, toUserID uint64, channel int32, content string) error {
	msg := NewChatMessage(fromUserID, toUserID, channel, content)
	return mb.nsq.PublishJSON("chat_messages", msg)
}

// PublishSystemMessage 发布系统消息
func (mb *MessageBroker) PublishSystemMessage(msgType, target, command string, args map[string]interface{}) error {
	msg := NewSystemMessage(msgType, target, command, args)
	return mb.nsq.PublishJSON("system_messages", msg)
}

// BroadcastSystemMessage 广播系统消息
func (mb *MessageBroker) BroadcastSystemMessage(command string, args map[string]interface{}) error {
	return mb.PublishSystemMessage("broadcast", "", command, args)
}

// SendToNode 发送消息到指定节点
func (mb *MessageBroker) SendToNode(target, command string, args map[string]interface{}) error {
	return mb.PublishSystemMessage("unicast", target, command, args)
}

// SubscribeGameEvents 订阅游戏事件
func (mb *MessageBroker) SubscribeGameEvents(handler *GameMessageHandler) error {
	return mb.nsq.Subscribe("game_events", mb.nodeID, handler)
}

// SubscribeChatMessages 订阅聊天消息
func (mb *MessageBroker) SubscribeChatMessages(handler *ChatMessageHandler) error {
	return mb.nsq.Subscribe("chat_messages", mb.nodeID, handler)
}

// SubscribeSystemMessages 订阅系统消息
func (mb *MessageBroker) SubscribeSystemMessages(handler *SystemMessageHandler) error {
	return mb.nsq.Subscribe("system_messages", mb.nodeID, handler)
}

// 消息类型常量
const (
	// 游戏事件
	MSG_GAME_ROOM_CREATED  = "game_room_created"
	MSG_GAME_ROOM_JOINED   = "game_room_joined"
	MSG_GAME_ROOM_LEFT     = "game_room_left"
	MSG_GAME_STARTED       = "game_started"
	MSG_GAME_ENDED         = "game_ended"
	MSG_PLAYER_ACTION      = "player_action"
	MSG_GAME_STATE_CHANGED = "game_state_changed"

	// 聊天频道
	CHAT_CHANNEL_WORLD  = 1 // 世界聊天
	CHAT_CHANNEL_ROOM   = 2 // 房间聊天
	CHAT_CHANNEL_FRIEND = 3 // 好友聊天
	CHAT_CHANNEL_GUILD  = 4 // 公会聊天

	// 系统命令
	SYS_CMD_RELOAD_CONFIG    = "reload_config"
	SYS_CMD_UPDATE_LOAD      = "update_load"
	SYS_CMD_SHUTDOWN         = "shutdown"
	SYS_CMD_HOT_UPDATE       = "hot_update"
	SYS_CMD_KICK_USER        = "kick_user"
	SYS_CMD_BROADCAST_NOTICE = "broadcast_notice"
)
