package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"tribeway/internal/logger"
)

// Message 消息接口
type Message interface {
	GetType() string
	GetData() []byte
}

// Actor 接口定义
type Actor interface {
	GetID() string
	GetType() string
	OnReceive(ctx context.Context, msg Message) error
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
	GetMailboxSize() int
}

// RuntimeActor 暴露Actor运行时能力，ActorSystem通过它接入mailbox。
type RuntimeActor interface {
	Actor
	Start(actor Actor) error
	Stop(actor Actor) error
	Tell(msg Message) error
	GetStats() ActorStats
}

// BaseActor Actor基础实现
type ActorStats struct {
	ID                 string        `json:"id"`
	Type               string        `json:"type"`
	Running            bool          `json:"running"`
	MailboxSize        int           `json:"mailbox_size"`
	MailboxCapacity    int           `json:"mailbox_capacity"`
	MessagesEnqueued   uint64        `json:"messages_enqueued"`
	MessagesProcessed  uint64        `json:"messages_processed"`
	MessagesFailed     uint64        `json:"messages_failed"`
	MessagesRejected   uint64        `json:"messages_rejected"`
	LastMessageUnix    int64         `json:"last_message_unix"`
	AverageProcessTime time.Duration `json:"average_process_time"`
}

type BaseActor struct {
	id                    string
	actorType             string
	mailbox               chan Message
	ctx                   context.Context
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	running               bool
	mutex                 sync.RWMutex
	messagesEnqueued      uint64
	messagesProcessed     uint64
	messagesFailed        uint64
	messagesRejected      uint64
	totalProcessTimeNanos uint64
	lastMessageUnix       int64
}

// NewBaseActor 创建基础Actor
func NewBaseActor(id, actorType string, mailboxSize int) *BaseActor {
	ctx, cancel := context.WithCancel(context.Background())
	return &BaseActor{
		id:        id,
		actorType: actorType,
		mailbox:   make(chan Message, mailboxSize),
		ctx:       ctx,
		cancel:    cancel,
		running:   false,
	}
}

// GetID 获取Actor ID
func (a *BaseActor) GetID() string {
	return a.id
}

// GetType 获取Actor类型
func (a *BaseActor) GetType() string {
	return a.actorType
}

// GetMailboxSize 获取邮箱大小
func (a *BaseActor) GetMailboxSize() int {
	return len(a.mailbox)
}

// Start 启动Actor
func (a *BaseActor) Start(actor Actor) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.running {
		return fmt.Errorf("actor %s already running", a.id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	a.cancel = cancel

	if err := actor.OnStart(a.ctx); err != nil {
		a.cancel()
		return err
	}

	a.running = true
	a.wg.Add(1)
	go a.run(actor)

	return nil
}

// Stop 停止Actor
func (a *BaseActor) Stop(actor Actor) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.running {
		return nil
	}

	a.running = false
	a.cancel()

	// 等待goroutine结束
	a.wg.Wait()

	return actor.OnStop(a.ctx)
}

// Tell 发送消息到Actor
func (a *BaseActor) Tell(msg Message) error {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	if !a.running {
		return fmt.Errorf("actor %s is not running", a.id)
	}

	select {
	case a.mailbox <- msg:
		atomic.AddUint64(&a.messagesEnqueued, 1)
		return nil
	case <-time.After(5 * time.Second):
		atomic.AddUint64(&a.messagesRejected, 1)
		return fmt.Errorf("mailbox full for actor %s", a.id)
	}
}

func (a *BaseActor) GetStats() ActorStats {
	a.mutex.RLock()
	running := a.running
	a.mutex.RUnlock()

	processed := atomic.LoadUint64(&a.messagesProcessed)
	totalNanos := atomic.LoadUint64(&a.totalProcessTimeNanos)
	var avg time.Duration
	if processed > 0 {
		avg = time.Duration(totalNanos / processed)
	}

	return ActorStats{
		ID:                 a.id,
		Type:               a.actorType,
		Running:            running,
		MailboxSize:        len(a.mailbox),
		MailboxCapacity:    cap(a.mailbox),
		MessagesEnqueued:   atomic.LoadUint64(&a.messagesEnqueued),
		MessagesProcessed:  processed,
		MessagesFailed:     atomic.LoadUint64(&a.messagesFailed),
		MessagesRejected:   atomic.LoadUint64(&a.messagesRejected),
		LastMessageUnix:    atomic.LoadInt64(&a.lastMessageUnix),
		AverageProcessTime: avg,
	}
}

// run Actor运行循环
func (a *BaseActor) run(actor Actor) {
	defer a.wg.Done()

	logger.Info(fmt.Sprintf("Actor %s started", a.id))

	for {
		select {
		case msg := <-a.mailbox:
			if msg == nil {
				continue
			}
			start := time.Now()
			if err := actor.OnReceive(a.ctx, msg); err != nil {
				atomic.AddUint64(&a.messagesFailed, 1)
				logger.Error(fmt.Sprintf("Actor %s handle message error: %v", a.id, err))
			}
			atomic.AddUint64(&a.messagesProcessed, 1)
			atomic.AddUint64(&a.totalProcessTimeNanos, uint64(time.Since(start).Nanoseconds()))
			atomic.StoreInt64(&a.lastMessageUnix, time.Now().Unix())

		case <-a.ctx.Done():
			logger.Info(fmt.Sprintf("Actor %s stopped", a.id))
			return
		}
	}
}

// ActorSystem Actor系统
type ActorSystem struct {
	actors map[string]Actor
	mutex  sync.RWMutex
	name   string
	ctx    context.Context
	cancel context.CancelFunc
}

// NewActorSystem 创建Actor系统
func NewActorSystem(name string) *ActorSystem {
	ctx, cancel := context.WithCancel(context.Background())
	return &ActorSystem{
		actors: make(map[string]Actor),
		name:   name,
		ctx:    ctx,
		cancel: cancel,
	}
}

// SpawnActor 创建Actor
func (sys *ActorSystem) SpawnActor(actor Actor) error {
	sys.mutex.Lock()
	defer sys.mutex.Unlock()

	id := actor.GetID()
	if _, exists := sys.actors[id]; exists {
		return fmt.Errorf("actor %s already exists", id)
	}

	runtimeActor, ok := actor.(RuntimeActor)
	if !ok {
		return fmt.Errorf("actor %s does not implement RuntimeActor", id)
	}

	if err := runtimeActor.Start(actor); err != nil {
		return err
	}

	sys.actors[id] = actor
	logger.Info(fmt.Sprintf("Actor %s spawned", id))

	return nil
}

// GetActor 获取Actor
func (sys *ActorSystem) GetActor(id string) (Actor, bool) {
	sys.mutex.RLock()
	defer sys.mutex.RUnlock()

	actor, exists := sys.actors[id]
	return actor, exists
}

// Tell 向Actor发送消息
func (sys *ActorSystem) Tell(actorID string, msg Message) error {
	actor, exists := sys.GetActor(actorID)
	if !exists {
		return fmt.Errorf("actor %s not found", actorID)
	}

	runtimeActor, ok := actor.(RuntimeActor)
	if !ok {
		return fmt.Errorf("actor %s does not implement RuntimeActor", actorID)
	}

	return runtimeActor.Tell(msg)
}

// Shutdown 关闭Actor系统
func (sys *ActorSystem) Shutdown() error {
	sys.mutex.Lock()
	defer sys.mutex.Unlock()

	logger.Info("Shutting down actor system")

	// 停止所有Actor
	for id, actor := range sys.actors {
		runtimeActor, ok := actor.(RuntimeActor)
		if !ok {
			logger.Error(fmt.Sprintf("Actor %s does not implement RuntimeActor", id))
			continue
		}
		if err := runtimeActor.Stop(actor); err != nil {
			logger.Error(fmt.Sprintf("Error stopping actor %s: %v", id, err))
		}
	}

	sys.cancel()
	sys.actors = make(map[string]Actor)

	return nil
}

// GetActorCount 获取Actor数量
func (sys *ActorSystem) GetActorCount() int {
	sys.mutex.RLock()
	defer sys.mutex.RUnlock()

	return len(sys.actors)
}

// ListActors 列出所有Actor
func (sys *ActorSystem) ListActors() []string {
	sys.mutex.RLock()
	defer sys.mutex.RUnlock()

	var actors []string
	for id := range sys.actors {
		actors = append(actors, id)
	}

	return actors
}

// MessageImpl 基础消息实现
type ActorSystemStats struct {
	Name                   string       `json:"name"`
	TotalActors            int          `json:"total_actors"`
	TotalMailboxSize       int          `json:"total_mailbox_size"`
	TotalMessagesEnqueued  uint64       `json:"total_messages_enqueued"`
	TotalMessagesProcessed uint64       `json:"total_messages_processed"`
	TotalMessagesFailed    uint64       `json:"total_messages_failed"`
	TotalMessagesRejected  uint64       `json:"total_messages_rejected"`
	Actors                 []ActorStats `json:"actors"`
}

func (sys *ActorSystem) GetStats() ActorSystemStats {
	sys.mutex.RLock()
	defer sys.mutex.RUnlock()

	stats := ActorSystemStats{
		Name:   sys.name,
		Actors: make([]ActorStats, 0, len(sys.actors)),
	}
	for _, actor := range sys.actors {
		runtimeActor, ok := actor.(RuntimeActor)
		if !ok {
			continue
		}
		actorStats := runtimeActor.GetStats()
		stats.TotalActors++
		stats.TotalMailboxSize += actorStats.MailboxSize
		stats.TotalMessagesEnqueued += actorStats.MessagesEnqueued
		stats.TotalMessagesProcessed += actorStats.MessagesProcessed
		stats.TotalMessagesFailed += actorStats.MessagesFailed
		stats.TotalMessagesRejected += actorStats.MessagesRejected
		stats.Actors = append(stats.Actors, actorStats)
	}
	return stats
}

type MessageImpl struct {
	Type string
	Data []byte
}

// GetType 获取消息类型
func (m *MessageImpl) GetType() string {
	return m.Type
}

// GetData 获取消息数据
func (m *MessageImpl) GetData() []byte {
	return m.Data
}

// NewMessage 创建新消息
func NewMessage(msgType string, data []byte) Message {
	return &MessageImpl{
		Type: msgType,
		Data: data,
	}
}

// 消息类型常量
const (
	MSG_TYPE_USER_LOGIN   = "user_login"
	MSG_TYPE_USER_LOGOUT  = "user_logout"
	MSG_TYPE_GAME_START   = "game_start"
	MSG_TYPE_GAME_END     = "game_end"
	MSG_TYPE_CHAT_MSG     = "chat_msg"
	MSG_TYPE_FRIEND_REQ   = "friend_req"
	MSG_TYPE_MAIL_SEND    = "mail_send"
	MSG_TYPE_GM_CMD       = "gm_cmd"
	MSG_TYPE_SYSTEM_CMD   = "system_cmd"
	MSG_TYPE_RPC_REQUEST  = "rpc_request"
	MSG_TYPE_RPC_RESPONSE = "rpc_response"
)
