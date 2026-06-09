package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"tribeway/internal/logger"
	"tribeway/internal/network"
	"tribeway/internal/protocol"
	tribeproto "tribeway/pkg/proto"
)

type CallOptions struct {
	Timeout       time.Duration
	Retries       int
	RetryInterval time.Duration
}

type CircuitBreakerState string

const (
	CircuitClosed   CircuitBreakerState = "closed"
	CircuitOpen     CircuitBreakerState = "open"
	CircuitHalfOpen CircuitBreakerState = "half_open"
)

// RPCService RPC服务接口
type RPCService interface {
	GetName() string
	RegisterMethods() map[string]reflect.Value
}

// RPCRequest RPC请求
type RPCRequest struct {
	ID       uint64            `json:"id"`
	Service  string            `json:"service"`
	Method   string            `json:"method"`
	Args     []byte            `json:"args"`
	UserID   uint64            `json:"user_id,omitempty"`
	Session  string            `json:"session,omitempty"`
	Timeout  int64             `json:"timeout"`
	Callback chan *RPCResponse `json:"-"`
}

// RPCResponse RPC响应
type RPCResponse struct {
	ID    uint64 `json:"id"`
	Error string `json:"error,omitempty"`
	Data  []byte `json:"data,omitempty"`
}

// RPCServer RPC服务器
type RPCServer struct {
	address      string
	port         int
	listener     net.Listener
	services     map[string]RPCService
	methods      map[string]reflect.Value
	running      bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mutex        sync.RWMutex
	connCount    int64
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxFrameSize uint32
}

// NewRPCServer 创建RPC服务器
func NewRPCServer(address string, port int) *RPCServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &RPCServer{
		address:      address,
		port:         port,
		services:     make(map[string]RPCService),
		methods:      make(map[string]reflect.Value),
		ctx:          ctx,
		cancel:       cancel,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
		maxFrameSize: network.DefaultMaxFrame,
	}
}

// SetFrameOptions 设置RPC帧读写选项。
func (s *RPCServer) SetFrameOptions(readTimeout, writeTimeout time.Duration, maxFrameSize uint32) {
	if readTimeout > 0 {
		s.readTimeout = readTimeout
	}
	if writeTimeout > 0 {
		s.writeTimeout = writeTimeout
	}
	if maxFrameSize > 0 {
		s.maxFrameSize = maxFrameSize
	}
}

// RegisterService 注册服务
func (s *RPCServer) RegisterService(service RPCService) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	name := service.GetName()
	if _, exists := s.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	s.services[name] = service

	// 注册方法
	methods := service.RegisterMethods()
	for methodName, method := range methods {
		fullName := fmt.Sprintf("%s.%s", name, methodName)
		s.methods[fullName] = method
	}

	logger.Info(fmt.Sprintf("RPC service %s registered with %d methods", name, len(methods)))
	return nil
}

// Start 启动RPC服务器
func (s *RPCServer) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.address, s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on %s:%d: %v", s.address, s.port, err)
	}

	s.listener = listener
	s.running = true

	logger.Info(fmt.Sprintf("RPC server listening on %s:%d", s.address, s.port))

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop 停止RPC服务器
func (s *RPCServer) Stop() error {
	if !s.running {
		return nil
	}

	s.running = false
	s.cancel()

	if s.listener != nil {
		s.listener.Close()
	}

	s.wg.Wait()
	logger.Info("RPC server stopped")

	return nil
}

// acceptLoop 接受连接循环
func (s *RPCServer) acceptLoop() {
	defer s.wg.Done()

	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				logger.Error(fmt.Sprintf("Accept error: %v", err))
			}
			continue
		}

		atomic.AddInt64(&s.connCount, 1)
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection 处理连接
func (s *RPCServer) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		conn.Close()
		atomic.AddInt64(&s.connCount, -1)
	}()

	logger.Debug(fmt.Sprintf("New RPC connection from %s", conn.RemoteAddr()))

	for s.running {
		requestBuf, err := network.ReadFrameWithOptions(conn, network.FrameOptions{
			MaxFrameSize: s.maxFrameSize,
			ReadTimeout:  s.readTimeout,
		})
		if err != nil {
			break
		}

		// 处理请求
		response := s.handleRequest(requestBuf)

		// 发送响应
		responseData, _ := json.Marshal(response)
		if err := network.WriteFrameWithOptions(conn, responseData, network.FrameOptions{
			MaxFrameSize: s.maxFrameSize,
			WriteTimeout: s.writeTimeout,
		}); err != nil {
			logger.Error(fmt.Sprintf("Write RPC response error: %v", err))
			break
		}
	}
}

// handleRequest 处理RPC请求
func (s *RPCServer) handleRequest(data []byte) *RPCResponse {
	var request RPCRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return &RPCResponse{
			ID:    0,
			Error: fmt.Sprintf("unmarshal request error: %v", err),
		}
	}

	// 查找方法
	methodKey := fmt.Sprintf("%s.%s", request.Service, request.Method)
	s.mutex.RLock()
	method, exists := s.methods[methodKey]
	s.mutex.RUnlock()

	if !exists {
		return &RPCResponse{
			ID:    request.ID,
			Error: fmt.Sprintf("method %s not found", methodKey),
		}
	}

	// 调用方法
	start := time.Now()
	result, err := s.callMethod(method, request.Args, request.UserID, request.Session)
	duration := time.Since(start)

	logger.Debug(fmt.Sprintf("RPC call %s took %v", methodKey, duration))

	response := &RPCResponse{ID: request.ID}
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Data = result
	}

	return response
}

// callMethod 调用方法
func (s *RPCServer) callMethod(method reflect.Value, args []byte, requestUserID uint64, requestSession string) ([]byte, error) {
	methodType := method.Type()
	if methodType.NumIn() != 2 {
		return nil, fmt.Errorf("method must have exactly 2 parameters")
	}

	// 创建参数
	argsType := methodType.In(1)
	argsValue := reflect.New(argsType.Elem())

	// 反序列化参数
	if len(args) > 0 {
		if err := proto.Unmarshal(args, argsValue.Interface().(proto.Message)); err != nil {
			return nil, fmt.Errorf("unmarshal args error: %v", err)
		}
	}

	callCtx := context.Background()
	if requestUserID > 0 {
		callCtx = context.WithValue(callCtx, "user_id", requestUserID)
	}
	if requestSession != "" {
		callCtx = context.WithValue(callCtx, "session_id", requestSession)
	}
	if meta := rpcMetadataFromMessage(argsValue.Interface()); meta != nil {
		if meta.UserID > 0 {
			callCtx = context.WithValue(callCtx, "user_id", meta.UserID)
		}
		if meta.SessionID != "" {
			callCtx = context.WithValue(callCtx, "session_id", meta.SessionID)
		}
	}

	// 调用方法
	results := method.Call([]reflect.Value{
		reflect.ValueOf(callCtx),
		argsValue,
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("method must return exactly 2 values")
	}

	// 检查错误
	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	// 序列化结果
	if results[0].IsNil() {
		return nil, nil
	}

	return proto.Marshal(results[0].Interface().(proto.Message))
}

type rpcMessageMetadata struct {
	UserID    uint64
	SessionID string
}

func rpcMetadataFromMessage(msg interface{}) *rpcMessageMetadata {
	switch req := msg.(type) {
	case *tribeproto.BaseRequest:
		header := req.GetHeader()
		if header == nil {
			return nil
		}
		return &rpcMessageMetadata{
			UserID:    header.GetUserId(),
			SessionID: header.GetSessionId(),
		}
	default:
		return nil
	}
}

// GetConnectionCount 获取连接数
func (s *RPCServer) GetConnectionCount() int64 {
	return atomic.LoadInt64(&s.connCount)
}

// RPCClient RPC客户端
type RPCClient struct {
	address      string
	port         int
	conn         net.Conn
	mutex        sync.Mutex
	requestID    uint64
	callbacks    map[uint64]chan *RPCResponse
	running      bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	pool         *RPCConnectionPool
	dialTimeout  time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxFrameSize uint32
	breaker      *CircuitBreaker
}

type CircuitBreaker struct {
	mutex            sync.Mutex
	state            CircuitBreakerState
	failures         int
	failureThreshold int
	openUntil        time.Time
	openTimeout      time.Duration
}

func NewCircuitBreaker(failureThreshold int, openTimeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if openTimeout <= 0 {
		openTimeout = 30 * time.Second
	}
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	if cb.state == CircuitOpen && time.Now().After(cb.openUntil) {
		cb.state = CircuitHalfOpen
		return true
	}
	return cb.state != CircuitOpen
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.failures = 0
	cb.state = CircuitClosed
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.failures++
	if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
		cb.openUntil = time.Now().Add(cb.openTimeout)
	}
}

func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	return cb.state
}

// NewRPCClient 创建RPC客户端
func NewRPCClient(address string, port int) *RPCClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &RPCClient{
		address:      address,
		port:         port,
		callbacks:    make(map[uint64]chan *RPCResponse),
		ctx:          ctx,
		cancel:       cancel,
		dialTimeout:  5 * time.Second,
		readTimeout:  0,
		writeTimeout: 30 * time.Second,
		maxFrameSize: network.DefaultMaxFrame,
		breaker:      NewCircuitBreaker(5, 30*time.Second),
	}
}

// SetFrameOptions 设置RPC客户端帧读写选项。
func (c *RPCClient) SetFrameOptions(readTimeout, writeTimeout time.Duration, maxFrameSize uint32) {
	if readTimeout > 0 {
		c.readTimeout = readTimeout
	}
	if writeTimeout > 0 {
		c.writeTimeout = writeTimeout
	}
	if maxFrameSize > 0 {
		c.maxFrameSize = maxFrameSize
	}
}

// Connect 连接到RPC服务器
func (c *RPCClient) Connect() error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.address, c.port), c.dialTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to %s:%d: %v", c.address, c.port, err)
	}

	c.conn = conn
	c.running = true

	// 启动响应处理goroutine
	c.wg.Add(1)
	go c.responseLoop()

	logger.Debug(fmt.Sprintf("Connected to RPC server %s:%d", c.address, c.port))
	return nil
}

// Disconnect 断开连接
func (c *RPCClient) Disconnect() error {
	c.mutex.Lock()
	if !c.running {
		c.mutex.Unlock()
		return nil
	}

	c.running = false
	c.cancel()
	conn := c.conn

	for _, callback := range c.callbacks {
		select {
		case callback <- nil:
		default:
		}
	}
	c.callbacks = make(map[uint64]chan *RPCResponse)
	c.mutex.Unlock()

	if conn != nil {
		conn.Close()
	}

	c.wg.Wait()
	logger.Debug("Disconnected from RPC server")

	return nil
}

func (c *RPCClient) isRunning() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.running && c.conn != nil
}

// Call 同步调用RPC方法
func (c *RPCClient) Call(service, method string, args proto.Message, timeout time.Duration) ([]byte, error) {
	return c.CallWithOptions(service, method, args, CallOptions{Timeout: timeout})
}

func (c *RPCClient) CallWithOptions(service, method string, args proto.Message, options CallOptions) ([]byte, error) {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	if options.RetryInterval <= 0 {
		options.RetryInterval = 100 * time.Millisecond
	}
	attempts := options.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if !c.breaker.Allow() {
			return nil, protocol.NewError(protocol.CodeCircuitOpen, "rpc circuit breaker is open")
		}
		data, err := c.callOnce(service, method, args, options.Timeout)
		if err == nil {
			c.breaker.RecordSuccess()
			return data, nil
		}
		lastErr = err
		c.breaker.RecordFailure()
		if attempt+1 < attempts {
			time.Sleep(options.RetryInterval)
		}
	}
	return nil, lastErr
}

func (c *RPCClient) callOnce(service, method string, args proto.Message, timeout time.Duration) ([]byte, error) {
	c.mutex.Lock()
	if !c.running {
		c.mutex.Unlock()
		return nil, protocol.NewError(protocol.CodeUnavailable, "rpc client not connected")
	}
	c.mutex.Unlock()

	// 序列化参数
	var argsData []byte
	var err error
	if args != nil {
		argsData, err = proto.Marshal(args)
		if err != nil {
			return nil, protocol.Wrap(protocol.CodeInvalidRequest, "marshal args error", err)
		}
	}

	// 创建请求
	requestID := atomic.AddUint64(&c.requestID, 1)
	request := &RPCRequest{
		ID:      requestID,
		Service: service,
		Method:  method,
		Args:    argsData,
		Timeout: int64(timeout / time.Millisecond),
	}
	if meta := rpcMetadataFromMessage(args); meta != nil {
		request.UserID = meta.UserID
		request.Session = meta.SessionID
	}

	// 创建回调通道
	callback := make(chan *RPCResponse, 1)
	c.mutex.Lock()
	c.callbacks[requestID] = callback
	c.mutex.Unlock()

	// 发送请求
	requestData, _ := json.Marshal(request)

	c.mutex.Lock()
	writeTimeout := c.writeTimeout
	if timeout > 0 && timeout < writeTimeout {
		writeTimeout = timeout
	}
	err = network.WriteFrameWithOptions(c.conn, requestData, network.FrameOptions{
		MaxFrameSize: c.maxFrameSize,
		WriteTimeout: writeTimeout,
	})
	c.mutex.Unlock()

	if err != nil {
		c.mutex.Lock()
		delete(c.callbacks, requestID)
		c.mutex.Unlock()
		return nil, protocol.Wrap(protocol.CodeUnavailable, "send request error", err)
	}

	// 等待响应
	select {
	case response := <-callback:
		c.mutex.Lock()
		delete(c.callbacks, requestID)
		c.mutex.Unlock()

		if response == nil {
			return nil, protocol.NewError(protocol.CodeUnavailable, "rpc connection closed")
		}
		if response.Error != "" {
			return nil, protocol.NewError(protocol.CodeInternal, response.Error)
		}
		return response.Data, nil

	case <-time.After(timeout):
		c.mutex.Lock()
		delete(c.callbacks, requestID)
		c.mutex.Unlock()
		return nil, protocol.NewError(protocol.CodeTimeout, "rpc call timeout")
	}
}

// responseLoop 响应处理循环
func (c *RPCClient) responseLoop() {
	defer func() {
		c.mutex.Lock()
		if c.running {
			c.running = false
			if c.conn != nil {
				c.conn.Close()
			}
			for _, callback := range c.callbacks {
				select {
				case callback <- nil:
				default:
				}
			}
			c.callbacks = make(map[uint64]chan *RPCResponse)
		}
		c.mutex.Unlock()
		c.wg.Done()
	}()

	for c.running {
		responseBuf, err := network.ReadFrameWithOptions(c.conn, network.FrameOptions{
			MaxFrameSize: c.maxFrameSize,
			ReadTimeout:  c.readTimeout,
		})
		if err != nil {
			if c.running {
				logger.Error(fmt.Sprintf("Read response frame error: %v", err))
			}
			break
		}

		// 解析响应
		var response RPCResponse
		if err := json.Unmarshal(responseBuf, &response); err != nil {
			logger.Error(fmt.Sprintf("Unmarshal response error: %v", err))
			continue
		}

		// 处理响应
		c.mutex.Lock()
		if callback, exists := c.callbacks[response.ID]; exists {
			select {
			case callback <- &response:
			default:
				// 回调通道已满或已关闭
			}
		}
		c.mutex.Unlock()
	}
}

// RPCConnectionPool RPC连接池
type RPCConnectionPool struct {
	address      string
	port         int
	maxSize      int
	pool         chan *RPCClient
	created      int64
	mutex        sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxFrameSize uint32
}

// NewRPCConnectionPool 创建RPC连接池
func NewRPCConnectionPool(address string, port int, maxSize int) *RPCConnectionPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &RPCConnectionPool{
		address:      address,
		port:         port,
		maxSize:      maxSize,
		pool:         make(chan *RPCClient, maxSize),
		ctx:          ctx,
		cancel:       cancel,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
		maxFrameSize: network.DefaultMaxFrame,
	}
}

// SetFrameOptions 设置连接池创建的RPC客户端帧读写选项。
func (p *RPCConnectionPool) SetFrameOptions(readTimeout, writeTimeout time.Duration, maxFrameSize uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if readTimeout > 0 {
		p.readTimeout = readTimeout
	}
	if writeTimeout > 0 {
		p.writeTimeout = writeTimeout
	}
	if maxFrameSize > 0 {
		p.maxFrameSize = maxFrameSize
	}
}

// Get 获取连接
func (p *RPCConnectionPool) Get() (*RPCClient, error) {
	select {
	case client := <-p.pool:
		if !client.isRunning() {
			atomic.AddInt64(&p.created, -1)
			return p.Get()
		}
		return client, nil
	default:
		if atomic.LoadInt64(&p.created) < int64(p.maxSize) {
			client := NewRPCClient(p.address, p.port)
			client.SetFrameOptions(p.readTimeout, p.writeTimeout, p.maxFrameSize)
			if err := client.Connect(); err != nil {
				return nil, err
			}
			client.pool = p
			atomic.AddInt64(&p.created, 1)
			return client, nil
		}

		// 等待连接可用
		select {
		case client := <-p.pool:
			if !client.isRunning() {
				atomic.AddInt64(&p.created, -1)
				return p.Get()
			}
			return client, nil
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("connection pool timeout")
		}
	}
}

// Put 归还连接
func (p *RPCConnectionPool) Put(client *RPCClient) {
	if client == nil {
		return
	}
	if !client.isRunning() {
		atomic.AddInt64(&p.created, -1)
		return
	}

	select {
	case p.pool <- client:
	default:
		// 池已满，关闭连接
		client.Disconnect()
		atomic.AddInt64(&p.created, -1)
	}
}

// Close 关闭连接池
func (p *RPCConnectionPool) Close() {
	p.cancel()

	// 关闭所有连接
	close(p.pool)
	for client := range p.pool {
		client.Disconnect()
	}
}

// Size 获取池大小
func (p *RPCConnectionPool) Size() int {
	return len(p.pool)
}

// Created 获取已创建的连接数
type ConnectionPoolStats struct {
	Address   string `json:"address"`
	Port      int    `json:"port"`
	MaxSize   int    `json:"max_size"`
	Idle      int    `json:"idle"`
	Created   int64  `json:"created"`
	Available bool   `json:"available"`
}

func (p *RPCConnectionPool) Stats() ConnectionPoolStats {
	return ConnectionPoolStats{
		Address:   p.address,
		Port:      p.port,
		MaxSize:   p.maxSize,
		Idle:      len(p.pool),
		Created:   atomic.LoadInt64(&p.created),
		Available: atomic.LoadInt64(&p.created) < int64(p.maxSize) || len(p.pool) > 0,
	}
}

func (p *RPCConnectionPool) HealthCheck() error {
	if !p.Stats().Available {
		return protocol.NewError(protocol.CodeUnavailable, "rpc connection pool exhausted")
	}
	return nil
}

func (p *RPCConnectionPool) Created() int64 {
	return atomic.LoadInt64(&p.created)
}
