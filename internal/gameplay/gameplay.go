package gameplay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tribeway/internal/actor"
	"tribeway/internal/logger"
)

// GameplayManager 玩法管理器
type GameplayManager struct {
	modules map[string]GameplayModule
	rooms   map[uint64]*GameRoom
	mutex   sync.RWMutex
}

// GameplayModule 玩法模块接口
type GameplayModule interface {
	GetName() string
	GetVersion() string
	Initialize() error
	CreateRoom(config *RoomConfig) (*GameRoom, error)
	ValidateAction(room *GameRoom, player *Player, action *GameAction) error
	ProcessAction(room *GameRoom, player *Player, action *GameAction) (*GameResult, error)
	GetRoomState(room *GameRoom) interface{}
	Cleanup() error
}

// GameRoom 游戏房间
type GameRoom struct {
	ID        uint64
	GameType  string
	Players   map[uint64]*Player
	State     GameState
	Config    *RoomConfig
	StartTime time.Time
	EndTime   time.Time
	GameData  interface{}
	Events    []GameEvent
	mutex     sync.RWMutex
}

// Player 游戏玩家
type Player struct {
	UserID   uint64
	Nickname string
	Level    int32
	Position int
	Status   PlayerStatus
	Score    int64
	Data     interface{}
	JoinTime time.Time
}

// GameAction 游戏操作
type GameAction struct {
	Type      string
	PlayerID  uint64
	Data      interface{}
	Timestamp time.Time
}

// GameResult 游戏结果
type GameResult struct {
	Success   bool
	Message   string
	Data      interface{}
	Events    []GameEvent
	NextState GameState
}

// GameEvent 游戏事件
type GameEvent struct {
	Type      string
	PlayerID  uint64
	Data      interface{}
	Timestamp time.Time
}

// RoomConfig 房间配置
type RoomConfig struct {
	MaxPlayers   int
	MinPlayers   int
	RoomPassword string
	AutoStart    bool
	TimeLimit    time.Duration
	CustomConfig map[string]interface{}
}

// GameState 游戏状态
type GameState int

const (
	GameStateWaiting GameState = iota
	GameStateStarting
	GameStateRunning
	GameStatePaused
	GameStateEnded
)

// PlayerStatus 玩家状态
type PlayerStatus int

const (
	PlayerStatusWaiting PlayerStatus = iota
	PlayerStatusReady
	PlayerStatusPlaying
	PlayerStatusFinished
	PlayerStatusDisconnected
)

// NewGameplayManager 创建玩法管理器
func NewGameplayManager() *GameplayManager {
	return &GameplayManager{
		modules: make(map[string]GameplayModule),
		rooms:   make(map[uint64]*GameRoom),
	}
}

// RegisterModule 注册玩法模块
func (gm *GameplayManager) RegisterModule(module GameplayModule) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	name := module.GetName()
	if _, exists := gm.modules[name]; exists {
		return fmt.Errorf("module %s already registered", name)
	}

	if err := module.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize module %s: %v", name, err)
	}

	gm.modules[name] = module
	logger.Info(fmt.Sprintf("Registered gameplay module: %s (version: %s)",
		name, module.GetVersion()))

	return nil
}

// CreateRoom 创建游戏房间
func (gm *GameplayManager) CreateRoom(gameType string, config *RoomConfig) (*GameRoom, error) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	module, exists := gm.modules[gameType]
	if !exists {
		return nil, fmt.Errorf("game type %s not found", gameType)
	}

	room, err := module.CreateRoom(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create room: %v", err)
	}

	gm.rooms[room.ID] = room
	logger.Info(fmt.Sprintf("Created game room: %d (type: %s)", room.ID, gameType))

	return room, nil
}

// JoinRoom 加入游戏房间
func (gm *GameplayManager) JoinRoom(roomID uint64, player *Player) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	room, exists := gm.rooms[roomID]
	if !exists {
		return fmt.Errorf("room %d not found", roomID)
	}

	return room.AddPlayer(player)
}

// LeaveRoom 离开游戏房间
func (gm *GameplayManager) LeaveRoom(roomID uint64, playerID uint64) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	room, exists := gm.rooms[roomID]
	if !exists {
		return fmt.Errorf("room %d not found", roomID)
	}

	return room.RemovePlayer(playerID)
}

// ProcessAction 处理游戏操作
func (gm *GameplayManager) ProcessAction(roomID uint64, action *GameAction) (*GameResult, error) {
	gm.mutex.RLock()
	room, exists := gm.rooms[roomID]
	if !exists {
		gm.mutex.RUnlock()
		return nil, fmt.Errorf("room %d not found", roomID)
	}

	module, moduleExists := gm.modules[room.GameType]
	gm.mutex.RUnlock()

	if !moduleExists {
		return nil, fmt.Errorf("game module %s not found", room.GameType)
	}

	player, playerExists := room.GetPlayer(action.PlayerID)
	if !playerExists {
		return nil, fmt.Errorf("player %d not in room", action.PlayerID)
	}

	// 验证操作
	if err := module.ValidateAction(room, player, action); err != nil {
		return nil, fmt.Errorf("invalid action: %v", err)
	}

	// 处理操作
	result, err := module.ProcessAction(room, player, action)
	if err != nil {
		return nil, fmt.Errorf("failed to process action: %v", err)
	}

	// 更新房间状态
	if result.NextState != room.State {
		room.SetState(result.NextState)
	}

	// 记录事件
	room.AddEvents(result.Events)

	return result, nil
}

// GetRoom 获取游戏房间
func (gm *GameplayManager) GetRoom(roomID uint64) (*GameRoom, bool) {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()

	room, exists := gm.rooms[roomID]
	return room, exists
}

// AddPlayer 添加玩家到房间
func (gr *GameRoom) AddPlayer(player *Player) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if len(gr.Players) >= gr.Config.MaxPlayers {
		return fmt.Errorf("room is full")
	}

	if _, exists := gr.Players[player.UserID]; exists {
		return fmt.Errorf("player already in room")
	}

	player.JoinTime = time.Now()
	player.Status = PlayerStatusWaiting
	gr.Players[player.UserID] = player

	logger.Info(fmt.Sprintf("Player %d joined room %d", player.UserID, gr.ID))
	return nil
}

// RemovePlayer 从房间移除玩家
func (gr *GameRoom) RemovePlayer(playerID uint64) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if _, exists := gr.Players[playerID]; !exists {
		return fmt.Errorf("player not in room")
	}

	delete(gr.Players, playerID)
	logger.Info(fmt.Sprintf("Player %d left room %d", playerID, gr.ID))

	// 如果房间空了，标记结束
	if len(gr.Players) == 0 && gr.State != GameStateEnded {
		gr.State = GameStateEnded
		gr.EndTime = time.Now()
	}

	return nil
}

// GetPlayer 获取玩家
func (gr *GameRoom) GetPlayer(playerID uint64) (*Player, bool) {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	player, exists := gr.Players[playerID]
	return player, exists
}

// SetState 设置房间状态
func (gr *GameRoom) SetState(state GameState) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	oldState := gr.State
	gr.State = state

	logger.Debug(fmt.Sprintf("Room %d state changed: %d -> %d", gr.ID, oldState, state))

	if state == GameStateRunning && gr.StartTime.IsZero() {
		gr.StartTime = time.Now()
	}

	if state == GameStateEnded && gr.EndTime.IsZero() {
		gr.EndTime = time.Now()
	}
}

// AddEvents 添加事件
func (gr *GameRoom) AddEvents(events []GameEvent) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	gr.Events = append(gr.Events, events...)
}

// GetPlayerCount 获取玩家数量
func (gr *GameRoom) GetPlayerCount() int {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	return len(gr.Players)
}

// CardGameModule 卡牌游戏模块（示例）
type CardGameModule struct {
	name    string
	version string
}

// NewCardGameModule 创建卡牌游戏模块
func NewCardGameModule() *CardGameModule {
	return &CardGameModule{
		name:    "card_game",
		version: "1.0.0",
	}
}

// GetName 获取模块名称
func (cgm *CardGameModule) GetName() string {
	return cgm.name
}

// GetVersion 获取模块版本
func (cgm *CardGameModule) GetVersion() string {
	return cgm.version
}

// Initialize 初始化模块
func (cgm *CardGameModule) Initialize() error {
	logger.Info("Card game module initialized")
	return nil
}

// CreateRoom 创建房间
func (cgm *CardGameModule) CreateRoom(config *RoomConfig) (*GameRoom, error) {
	roomID := uint64(time.Now().UnixNano())

	room := &GameRoom{
		ID:       roomID,
		GameType: cgm.name,
		Players:  make(map[uint64]*Player),
		State:    GameStateWaiting,
		Config:   config,
		GameData: &CardGameData{
			Deck:  generateDeck(),
			Hands: make(map[uint64][]Card),
			Board: make([]Card, 0),
		},
		Events: make([]GameEvent, 0),
	}

	return room, nil
}

// ValidateAction 验证操作
func (cgm *CardGameModule) ValidateAction(room *GameRoom, player *Player, action *GameAction) error {
	switch action.Type {
	case "play_card":
		return cgm.validatePlayCard(room, player, action)
	case "draw_card":
		return cgm.validateDrawCard(room, player, action)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// ProcessAction 处理操作
func (cgm *CardGameModule) ProcessAction(room *GameRoom, player *Player, action *GameAction) (*GameResult, error) {
	switch action.Type {
	case "play_card":
		return cgm.processPlayCard(room, player, action)
	case "draw_card":
		return cgm.processDrawCard(room, player, action)
	default:
		return nil, fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// GetRoomState 获取房间状态
func (cgm *CardGameModule) GetRoomState(room *GameRoom) interface{} {
	return room.GameData
}

// Cleanup 清理模块
func (cgm *CardGameModule) Cleanup() error {
	logger.Info("Card game module cleaned up")
	return nil
}

// CardGameData 卡牌游戏数据
type CardGameData struct {
	Deck  []Card
	Hands map[uint64][]Card
	Board []Card
	Turn  uint64
	Round int
}

// Card 卡牌
type Card struct {
	ID    int
	Suit  string
	Value int
	Name  string
}

// validatePlayCard 验证出牌操作
func (cgm *CardGameModule) validatePlayCard(room *GameRoom, player *Player, action *GameAction) error {
	if room.State != GameStateRunning {
		return fmt.Errorf("game is not running")
	}

	if player.Status != PlayerStatusPlaying {
		return fmt.Errorf("player is not in playing state")
	}

	// 更多验证逻辑...
	return nil
}

// validateDrawCard 验证抽牌操作
func (cgm *CardGameModule) validateDrawCard(room *GameRoom, player *Player, action *GameAction) error {
	if room.State != GameStateRunning {
		return fmt.Errorf("game is not running")
	}

	gameData := room.GameData.(*CardGameData)
	if len(gameData.Deck) == 0 {
		return fmt.Errorf("deck is empty")
	}

	return nil
}

// processPlayCard 处理出牌操作
func (cgm *CardGameModule) processPlayCard(room *GameRoom, player *Player, action *GameAction) (*GameResult, error) {
	// 实现出牌逻辑
	events := []GameEvent{
		{
			Type:      "card_played",
			PlayerID:  player.UserID,
			Data:      action.Data,
			Timestamp: time.Now(),
		},
	}

	return &GameResult{
		Success: true,
		Message: "Card played successfully",
		Events:  events,
	}, nil
}

// processDrawCard 处理抽牌操作
func (cgm *CardGameModule) processDrawCard(room *GameRoom, player *Player, action *GameAction) (*GameResult, error) {
	gameData := room.GameData.(*CardGameData)

	// 从牌堆抽一张牌
	if len(gameData.Deck) > 0 {
		card := gameData.Deck[0]
		gameData.Deck = gameData.Deck[1:]

		if gameData.Hands[player.UserID] == nil {
			gameData.Hands[player.UserID] = make([]Card, 0)
		}
		gameData.Hands[player.UserID] = append(gameData.Hands[player.UserID], card)

		events := []GameEvent{
			{
				Type:      "card_drawn",
				PlayerID:  player.UserID,
				Data:      card,
				Timestamp: time.Now(),
			},
		}

		return &GameResult{
			Success: true,
			Message: "Card drawn successfully",
			Data:    card,
			Events:  events,
		}, nil
	}

	return &GameResult{
		Success: false,
		Message: "No cards left in deck",
	}, nil
}

// generateDeck 生成牌堆
func generateDeck() []Card {
	suits := []string{"spades", "hearts", "diamonds", "clubs"}
	deck := make([]Card, 0, 52)

	id := 1
	for _, suit := range suits {
		for value := 1; value <= 13; value++ {
			deck = append(deck, Card{
				ID:    id,
				Suit:  suit,
				Value: value,
				Name:  fmt.Sprintf("%s_%d", suit, value),
			})
			id++
		}
	}

	return deck
}

// GameplayActor 玩法Actor
type GameplayActor struct {
	*actor.BaseActor
	manager *GameplayManager
}

// NewGameplayActor 创建玩法Actor
func NewGameplayActor(manager *GameplayManager) *GameplayActor {
	baseActor := actor.NewBaseActor("gameplay_actor", "gameplay", 1000)

	return &GameplayActor{
		BaseActor: baseActor,
		manager:   manager,
	}
}

// OnReceive 处理消息
func (ga *GameplayActor) OnReceive(ctx context.Context, msg actor.Message) error {
	switch msg.GetType() {
	case "create_room":
		return ga.handleCreateRoom(msg)
	case "join_room":
		return ga.handleJoinRoom(msg)
	case "game_action":
		return ga.handleGameAction(msg)
	default:
		logger.Debug(fmt.Sprintf("Unknown message type: %s", msg.GetType()))
	}

	return nil
}

// OnStart 启动时处理
func (ga *GameplayActor) OnStart(ctx context.Context) error {
	logger.Info("Gameplay actor started")
	return nil
}

// OnStop 停止时处理
func (ga *GameplayActor) OnStop(ctx context.Context) error {
	logger.Info("Gameplay actor stopped")
	return nil
}

// handleCreateRoom 处理创建房间
func (ga *GameplayActor) handleCreateRoom(msg actor.Message) error {
	logger.Debug("Handling create room in gameplay actor")
	// 处理创建房间逻辑
	return nil
}

// handleJoinRoom 处理加入房间
func (ga *GameplayActor) handleJoinRoom(msg actor.Message) error {
	logger.Debug("Handling join room in gameplay actor")
	// 处理加入房间逻辑
	return nil
}

// handleGameAction 处理游戏操作
func (ga *GameplayActor) handleGameAction(msg actor.Message) error {
	logger.Debug("Handling game action in gameplay actor")
	// 处理游戏操作逻辑
	return nil
}
