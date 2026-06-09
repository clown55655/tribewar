package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"

	"tribeway/internal/network"
	"tribeway/pkg/proto"
)

// GameClient 游戏客户端
type GameClient struct {
	conn         net.Conn
	connected    bool
	userID       uint64
	token        string
	dialTimeout  time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxFrameSize uint32
}

// NewGameClient 创建游戏客户端
func NewGameClient() *GameClient {
	return &GameClient{
		connected:    false,
		dialTimeout:  5 * time.Second,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
		maxFrameSize: network.DefaultMaxFrame,
	}
}

// Connect 连接到服务器
func (gc *GameClient) Connect(address string) error {
	conn, err := net.DialTimeout("tcp", address, gc.dialTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}

	gc.conn = conn
	gc.connected = true

	log.Printf("Connected to server: %s", address)
	return nil
}

// Disconnect 断开连接
func (gc *GameClient) Disconnect() error {
	if gc.conn != nil {
		gc.connected = false
		return gc.conn.Close()
	}
	return nil
}

// sendMessage 发送消息
func (gc *GameClient) sendMessage(msgID uint32, data []byte) error {
	if !gc.connected {
		return fmt.Errorf("not connected to server")
	}

	// 构造消息：4字节消息ID + 消息数据
	message := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(message[0:4], msgID)
	copy(message[4:], data)

	return network.WriteFrameWithOptions(gc.conn, message, network.FrameOptions{
		MaxFrameSize: gc.maxFrameSize,
		WriteTimeout: gc.writeTimeout,
	})
}

// receiveMessage 接收消息
func (gc *GameClient) receiveMessage() ([]byte, error) {
	if !gc.connected {
		return nil, fmt.Errorf("not connected to server")
	}

	return network.ReadFrameWithOptions(gc.conn, network.FrameOptions{
		MaxFrameSize: gc.maxFrameSize,
		ReadTimeout:  gc.readTimeout,
	})
}

// Login 登录
func (gc *GameClient) Login(username, password string) error {
	log.Printf("Logging in as %s...", username)

	// 创建登录请求
	loginReq := &proto.LoginRequest{
		Username: username,
		Password: password,
		Platform: "test",
		Version:  "1.0.0",
	}

	// 序列化登录请求
	reqData, err := proto.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login request: %v", err)
	}

	// 创建基础请求
	baseReq := &proto.BaseRequest{
		Header: &proto.MessageHeader{
			MsgId:     1001,
			Seq:       1,
			Timestamp: uint32(time.Now().Unix()),
		},
		Data: reqData,
	}

	// 序列化基础请求
	baseReqData, err := proto.Marshal(baseReq)
	if err != nil {
		return fmt.Errorf("failed to marshal base request: %v", err)
	}

	// 发送登录消息
	if err := gc.sendMessage(1001, baseReqData); err != nil {
		return fmt.Errorf("failed to send login message: %v", err)
	}

	// 接收响应
	responseData, err := gc.receiveMessage()
	if err != nil {
		return fmt.Errorf("failed to receive response: %v", err)
	}

	// 解析响应
	var baseResp proto.BaseResponse
	if err := proto.Unmarshal(responseData, &baseResp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if baseResp.Code != 0 {
		return fmt.Errorf("login failed: %s", baseResp.Msg)
	}

	// 解析登录响应
	var loginResp proto.LoginResponse
	if err := proto.Unmarshal(baseResp.Data, &loginResp); err != nil {
		return fmt.Errorf("failed to unmarshal login response: %v", err)
	}

	gc.userID = loginResp.UserId
	gc.token = loginResp.Token

	log.Printf("Login successful! UserID: %d, Token: %s", gc.userID, gc.token[:10]+"...")
	log.Printf("Player Info - Nickname: %s, Level: %d, Gold: %d, Diamond: %d",
		loginResp.Nickname, loginResp.Level, loginResp.Gold, loginResp.Diamond)

	return nil
}

// SendHeartbeat 发送心跳
func (gc *GameClient) SendHeartbeat() error {
	log.Println("Sending heartbeat...")

	// 创建基础请求
	baseReq := &proto.BaseRequest{
		Header: &proto.MessageHeader{
			MsgId:     1002,
			Seq:       2,
			UserId:    gc.userID,
			Timestamp: uint32(time.Now().Unix()),
			SessionId: gc.token,
		},
	}

	// 序列化请求
	reqData, err := proto.Marshal(baseReq)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat request: %v", err)
	}

	// 发送心跳消息
	if err := gc.sendMessage(1002, reqData); err != nil {
		return fmt.Errorf("failed to send heartbeat: %v", err)
	}

	// 接收响应
	responseData, err := gc.receiveMessage()
	if err != nil {
		return fmt.Errorf("failed to receive heartbeat response: %v", err)
	}

	// 解析响应
	var baseResp proto.BaseResponse
	if err := proto.Unmarshal(responseData, &baseResp); err != nil {
		return fmt.Errorf("failed to unmarshal heartbeat response: %v", err)
	}

	if baseResp.Code != 0 {
		return fmt.Errorf("heartbeat failed: %s", baseResp.Msg)
	}

	log.Println("Heartbeat successful")
	return nil
}

// Logout 登出
func (gc *GameClient) Logout() error {
	log.Println("Logging out...")

	// 创建基础请求
	baseReq := &proto.BaseRequest{
		Header: &proto.MessageHeader{
			MsgId:     1003,
			Seq:       3,
			UserId:    gc.userID,
			Timestamp: uint32(time.Now().Unix()),
			SessionId: gc.token,
		},
	}

	// 序列化请求
	reqData, err := proto.Marshal(baseReq)
	if err != nil {
		return fmt.Errorf("failed to marshal logout request: %v", err)
	}

	// 发送登出消息
	if err := gc.sendMessage(1003, reqData); err != nil {
		return fmt.Errorf("failed to send logout: %v", err)
	}

	log.Println("Logout request sent")
	return nil
}

// runClient 运行客户端示例
func runClient() {
	client := NewGameClient()

	// 连接到服务器
	if err := client.Connect("127.0.0.1:8001"); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer client.Disconnect()

	// 登录
	if err := client.Login("testuser", "123456"); err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	// 发送几次心跳
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Second)
		if err := client.SendHeartbeat(); err != nil {
			log.Printf("Heartbeat failed: %v", err)
		}
	}

	// 登出
	if err := client.Logout(); err != nil {
		log.Printf("Logout failed: %v", err)
	}

	time.Sleep(1 * time.Second)
}

func main() {
	fmt.Println("=== Tribeway Game Client Demo ===")

	// 检查服务器是否可用
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8001", 5*time.Second)
	if err != nil {
		log.Fatalf("Server not available at 127.0.0.1:8001: %v", err)
	}
	conn.Close()

	log.Println("Server is available, starting client demo...")
	runClient()

	log.Println("Client demo completed!")
}
