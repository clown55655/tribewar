package pool

import (
	"sync"
	"sync/atomic"
	"time"
)

// ObjectPool 通用对象池接口
type ObjectPool interface {
	Get() interface{}
	Put(obj interface{})
	Size() int
	Available() int
}

// GenericPool 通用对象池实现
type GenericPool struct {
	pool    chan interface{}
	factory func() interface{}
	reset   func(interface{})
	maxSize int
	created int64
	gotten  int64
	put     int64
}

// NewGenericPool 创建通用对象池
func NewGenericPool(maxSize int, factory func() interface{}, reset func(interface{})) *GenericPool {
	return &GenericPool{
		pool:    make(chan interface{}, maxSize),
		factory: factory,
		reset:   reset,
		maxSize: maxSize,
	}
}

// Get 获取对象
func (p *GenericPool) Get() interface{} {
	atomic.AddInt64(&p.gotten, 1)

	select {
	case obj := <-p.pool:
		return obj
	default:
		if int(atomic.LoadInt64(&p.created)) < p.maxSize {
			atomic.AddInt64(&p.created, 1)
			return p.factory()
		}

		// 等待对象可用
		select {
		case obj := <-p.pool:
			return obj
		case <-time.After(time.Millisecond * 100):
			// 超时后创建新对象
			return p.factory()
		}
	}
}

// Put 归还对象
func (p *GenericPool) Put(obj interface{}) {
	if obj == nil {
		return
	}

	atomic.AddInt64(&p.put, 1)

	// 重置对象状态
	if p.reset != nil {
		p.reset(obj)
	}

	select {
	case p.pool <- obj:
	default:
		// 池已满，丢弃对象
	}
}

// Size 获取池大小
func (p *GenericPool) Size() int {
	return int(atomic.LoadInt64(&p.created))
}

// Available 获取可用对象数
func (p *GenericPool) Available() int {
	return len(p.pool)
}

// Stats 获取统计信息
func (p *GenericPool) Stats() (created, gotten, put int64) {
	return atomic.LoadInt64(&p.created), atomic.LoadInt64(&p.gotten), atomic.LoadInt64(&p.put)
}

// MessagePool 消息对象池
type MessagePool struct {
	*GenericPool
}

// Message 可重用的消息对象
type Message struct {
	Type string
	Data []byte
	buf  []byte // 内部缓冲区
}

// Reset 重置消息
func (m *Message) Reset() {
	m.Type = ""
	m.Data = m.Data[:0]
	if len(m.buf) > 4096 {
		m.buf = make([]byte, 0, 1024) // 重新分配更小的缓冲区
	} else {
		m.buf = m.buf[:0]
	}
}

// SetData 设置消息数据
func (m *Message) SetData(data []byte) {
	if cap(m.buf) < len(data) {
		m.buf = make([]byte, len(data))
	}
	m.buf = m.buf[:len(data)]
	copy(m.buf, data)
	m.Data = m.buf
}

// NewMessagePool 创建消息池
func NewMessagePool(maxSize int) *MessagePool {
	return &MessagePool{
		GenericPool: NewGenericPool(
			maxSize,
			func() interface{} {
				return &Message{
					buf: make([]byte, 0, 1024),
				}
			},
			func(obj interface{}) {
				if msg, ok := obj.(*Message); ok {
					msg.Reset()
				}
			},
		),
	}
}

// GetMessage 获取消息对象
func (p *MessagePool) GetMessage() *Message {
	return p.Get().(*Message)
}

// PutMessage 归还消息对象
func (p *MessagePool) PutMessage(msg *Message) {
	p.Put(msg)
}

// Resettable 可重置接口
type Resettable interface {
	Reset()
}

// ConnectionPool 连接对象池
type ConnectionPool struct {
	*GenericPool
}

// NewConnectionPool 创建连接池
func NewConnectionPool(maxSize int, factory func() interface{}) *ConnectionPool {
	return &ConnectionPool{
		GenericPool: NewGenericPool(
			maxSize,
			factory,
			func(obj interface{}) {
				if resettable, ok := obj.(Resettable); ok {
					resettable.Reset()
				}
			},
		),
	}
}

// GetConnection 获取连接对象
func (p *ConnectionPool) GetConnection() interface{} {
	return p.Get()
}

// PutConnection 归还连接对象
func (p *ConnectionPool) PutConnection(conn interface{}) {
	p.Put(conn)
}

// ByteBufferPool 字节缓冲区池
type ByteBufferPool struct {
	pools map[int]*sync.Pool // 不同大小的池
	sizes []int              // 支持的大小
}

// NewByteBufferPool 创建字节缓冲区池
func NewByteBufferPool() *ByteBufferPool {
	sizes := []int{64, 256, 1024, 4096, 16384, 65536}
	pools := make(map[int]*sync.Pool)

	for _, size := range sizes {
		size := size // 闭包变量
		pools[size] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		}
	}

	return &ByteBufferPool{
		pools: pools,
		sizes: sizes,
	}
}

// GetBuffer 获取缓冲区
func (p *ByteBufferPool) GetBuffer(size int) []byte {
	// 找到合适的大小
	for _, poolSize := range p.sizes {
		if size <= poolSize {
			buf := p.pools[poolSize].Get().([]byte)
			return buf[:size]
		}
	}

	// 超大缓冲区直接分配
	return make([]byte, size)
}

// PutBuffer 归还缓冲区
func (p *ByteBufferPool) PutBuffer(buf []byte) {
	size := cap(buf)

	// 找到对应的池
	for _, poolSize := range p.sizes {
		if size == poolSize {
			p.pools[poolSize].Put(buf)
			return
		}
	}

	// 不在池范围内的直接丢弃，让GC回收
}

// ActorPool Actor对象池
type ActorPool struct {
	*GenericPool
}

// ActorMessage Actor消息
type ActorMessage struct {
	ID       string
	Type     string
	From     string
	To       string
	Data     []byte
	Callback func(interface{}, error)
	buf      []byte
}

// Reset 重置Actor消息
func (am *ActorMessage) Reset() {
	am.ID = ""
	am.Type = ""
	am.From = ""
	am.To = ""
	am.Data = nil
	am.Callback = nil
	am.buf = am.buf[:0]
}

// SetData 设置数据
func (am *ActorMessage) SetData(data []byte) {
	if cap(am.buf) < len(data) {
		am.buf = make([]byte, len(data))
	}
	am.buf = am.buf[:len(data)]
	copy(am.buf, data)
	am.Data = am.buf
}

// NewActorPool 创建Actor池
func NewActorPool(maxSize int) *ActorPool {
	return &ActorPool{
		GenericPool: NewGenericPool(
			maxSize,
			func() interface{} {
				return &ActorMessage{
					buf: make([]byte, 0, 512),
				}
			},
			func(obj interface{}) {
				if msg, ok := obj.(*ActorMessage); ok {
					msg.Reset()
				}
			},
		),
	}
}

// GetActorMessage 获取Actor消息
func (p *ActorPool) GetActorMessage() *ActorMessage {
	return p.Get().(*ActorMessage)
}

// PutActorMessage 归还Actor消息
func (p *ActorPool) PutActorMessage(msg *ActorMessage) {
	p.Put(msg)
}

// GlobalPools 全局对象池管理器
type GlobalPools struct {
	MessagePool    *MessagePool
	ConnectionPool *ConnectionPool
	ByteBufferPool *ByteBufferPool
	ActorPool      *ActorPool
}

var (
	globalPools     *GlobalPools
	globalPoolsOnce sync.Once
)

// GetGlobalPools 获取全局对象池
func GetGlobalPools() *GlobalPools {
	globalPoolsOnce.Do(func() {
		globalPools = &GlobalPools{
			MessagePool: NewMessagePool(10000),
			ConnectionPool: NewConnectionPool(1000, func() interface{} {
				return &struct {
					ID        uint64
					UserID    uint64
					SessionID string
				}{}
			}),
			ByteBufferPool: NewByteBufferPool(),
			ActorPool:      NewActorPool(5000),
		}
	})
	return globalPools
}

// PoolStats 池统计信息
type PoolStats struct {
	Name      string
	Size      int
	Available int
	Created   int64
	Gotten    int64
	Put       int64
}

// GetStats 获取所有池的统计信息
func (gp *GlobalPools) GetStats() []PoolStats {
	var stats []PoolStats

	// 消息池统计
	created, gotten, put := gp.MessagePool.Stats()
	stats = append(stats, PoolStats{
		Name:      "MessagePool",
		Size:      gp.MessagePool.Size(),
		Available: gp.MessagePool.Available(),
		Created:   created,
		Gotten:    gotten,
		Put:       put,
	})

	// 连接池统计
	created, gotten, put = gp.ConnectionPool.Stats()
	stats = append(stats, PoolStats{
		Name:      "ConnectionPool",
		Size:      gp.ConnectionPool.Size(),
		Available: gp.ConnectionPool.Available(),
		Created:   created,
		Gotten:    gotten,
		Put:       put,
	})

	// Actor池统计
	created, gotten, put = gp.ActorPool.Stats()
	stats = append(stats, PoolStats{
		Name:      "ActorPool",
		Size:      gp.ActorPool.Size(),
		Available: gp.ActorPool.Available(),
		Created:   created,
		Gotten:    gotten,
		Put:       put,
	})

	return stats
}
