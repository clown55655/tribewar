package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"tribeway/internal/database"
	"tribeway/internal/logger"
	"tribeway/pkg/proto"
)

// GameServer 游戏服务器
type GameServer struct {
	*BaseServer
	gameRecordRepo *database.GameRecordRepository
	games          map[uint64]*GameInstance // 游戏实例映射
	gamesMutex     sync.RWMutex             // 游戏实例锁
	nextGameID     uint64                   // 下一个游戏ID
	idMutex        sync.Mutex               // ID生成锁
}

// GameInstance 游戏实例
type GameInstance struct {
	GameID        uint64                     `json:"game_id"`
	RoomID        uint64                     `json:"room_id"`
	GameType      int32                      `json:"game_type"`
	Status        int32                      `json:"status"` // 0-等待开始 1-进行中 2-已结束
	Players       map[uint64]*GamePlayerData `json:"players"`
	CurrentPlayer uint64                     `json:"current_player"`
	StartTime     time.Time                  `json:"start_time"`
	EndTime       time.Time                  `json:"end_time"`
	Winner        uint64                     `json:"winner"`
	GameData      map[string]interface{}     `json:"game_data"`
	mutex         sync.RWMutex               `json:"-"`
}

// GamePlayerData 游戏玩家数据
type GamePlayerData struct {
	UserID   uint64                 `json:"user_id"`
	Nickname string                 `json:"nickname"`
	Level    int32                  `json:"level"`
	Score    int64                  `json:"score"`
	Status   int32                  `json:"status"` // 0-等待 1-准备 2-游戏中 3-已离开
	Data     map[string]interface{} `json:"data"`
}

// NewGameServer 创建游戏服务器
func NewGameServer(configFile, nodeID string) *GameServer {
	gameServer, err := NewGameServerWithError(configFile, nodeID)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Failed to create game server: %v", err))
	}
	return gameServer
}

func NewGameServerWithError(configFile, nodeID string) (*GameServer, error) {
	baseServer, err := NewBaseServerWithOptions(configFile, "game", nodeID, GameComponents())
	if err != nil {
		return nil, fmt.Errorf("failed to create base server: %v", err)
	}
	constructed := false
	defer cleanupBaseServerUnlessConstructed(baseServer, &constructed)

	gameServer := &GameServer{
		BaseServer:     baseServer,
		gameRecordRepo: database.NewGameRecordRepository(baseServer.mongoManager),
		games:          make(map[uint64]*GameInstance),
		nextGameID:     1,
	}

	if err := RegisterCommonServices(baseServer); err != nil {
		return nil, fmt.Errorf("failed to register common services: %v", err)
	}

	gameService := NewGameService(gameServer)
	if err := baseServer.rpcServer.RegisterService(gameService); err != nil {
		return nil, fmt.Errorf("failed to register game service: %v", err)
	}
	constructed = true
	return gameServer, nil
}

// generateGameID 生成游戏ID
func (gs *GameServer) generateGameID() uint64 {
	gs.idMutex.Lock()
	defer gs.idMutex.Unlock()
	id := gs.nextGameID
	gs.nextGameID++
	return id
}

// getGame 获取游戏实例
func (gs *GameServer) getGame(gameID uint64) (*GameInstance, bool) {
	gs.gamesMutex.RLock()
	defer gs.gamesMutex.RUnlock()
	game, exists := gs.games[gameID]
	return game, exists
}

// addGame 添加游戏实例
func (gs *GameServer) addGame(game *GameInstance) {
	gs.gamesMutex.Lock()
	defer gs.gamesMutex.Unlock()
	gs.games[game.GameID] = game
}

// removeGame 移除游戏实例
func (gs *GameServer) removeGame(gameID uint64) {
	gs.gamesMutex.Lock()
	defer gs.gamesMutex.Unlock()
	delete(gs.games, gameID)
}

// GameService 游戏RPC服务
type GameService struct {
	server *GameServer
}

// NewGameService 创建游戏服务
func NewGameService(server *GameServer) *GameService {
	return &GameService{
		server: server,
	}
}

// GetName 获取服务名称
func (gs *GameService) GetName() string {
	return "GameService"
}

// RegisterMethods 注册方法
func (gs *GameService) RegisterMethods() map[string]reflect.Value {
	methods := make(map[string]reflect.Value)

	methods["StartGame"] = reflect.ValueOf(gs.StartGame)
	methods["EndGame"] = reflect.ValueOf(gs.EndGame)
	methods["PlayerAction"] = reflect.ValueOf(gs.PlayerAction)
	methods["GetGameState"] = reflect.ValueOf(gs.GetGameState)

	return methods
}

// StartGame 开始游戏
func (gs *GameService) StartGame(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("StartGame: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var startGameReq proto.StartGameRequest
	if err := proto.Unmarshal(req.Data, &startGameReq); err != nil {
		logger.Error(fmt.Sprintf("StartGame: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	roomID := startGameReq.GetRoomId()
	gameType := startGameReq.GetGameType()

	// 验证房间ID
	if roomID == 0 {
		logger.Error("StartGame: invalid room id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid room id",
		}, nil
	}

	// 获取用户信息
	userRepo := database.NewUserRepository(gs.server.mongoManager)
	user, err := userRepo.GetByUserID(userID)
	if err != nil {
		logger.Error(fmt.Sprintf("StartGame: failed to get user %d: %v", userID, err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "user not found",
		}, nil
	}

	// 生成游戏ID
	gameID := gs.server.generateGameID()

	// 创建游戏实例
	game := &GameInstance{
		GameID:        gameID,
		RoomID:        roomID,
		GameType:      gameType,
		Status:        0, // 等待开始
		Players:       make(map[uint64]*GamePlayerData),
		CurrentPlayer: userID,
		StartTime:     time.Now(),
		GameData:      make(map[string]interface{}),
	}

	// 添加创建者为玩家
	playerData := &GamePlayerData{
		UserID:   userID,
		Nickname: user.Nickname,
		Level:    user.Level,
		Score:    0,
		Status:   1, // 准备状态
		Data:     make(map[string]interface{}),
	}
	game.Players[userID] = playerData

	// 添加到游戏服务器
	gs.server.addGame(game)

	// 创建游戏记录
	gameRecord := &database.GameRecord{
		GameID:   gameID,
		RoomID:   roomID,
		GameType: gameType,
		Players: []database.GamePlayer{
			{
				UserID:   userID,
				Nickname: user.Nickname,
				Level:    user.Level,
				Score:    0,
				Rank:     0,
			},
		},
		Status: 0, // 进行中
	}

	if err := gs.server.gameRecordRepo.CreateRecord(gameRecord); err != nil {
		logger.Error(fmt.Sprintf("StartGame: failed to create game record: %v", err))
		// 不返回错误，继续游戏
	}

	logger.Info(fmt.Sprintf("User %s (ID: %d) started game %d in room %d", user.Nickname, userID, gameID, roomID))

	// 构造响应数据
	responseData := map[string]interface{}{
		"game_id":   gameID,
		"room_id":   roomID,
		"game_type": gameType,
		"status":    game.Status,
	}

	responseBytes, err := json.Marshal(responseData)
	if err != nil {
		logger.Error(fmt.Sprintf("StartGame: failed to marshal response: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "failed to create response",
		}, nil
	}

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "game started successfully",
		Data:   responseBytes,
	}, nil
}

// EndGame 结束游戏
func (gs *GameService) EndGame(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("EndGame: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var endGameReq proto.EndGameRequest
	if err := proto.Unmarshal(req.Data, &endGameReq); err != nil {
		logger.Error(fmt.Sprintf("EndGame: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	gameID := endGameReq.GetGameId()
	winner := endGameReq.GetWinner()

	// 验证游戏ID
	if gameID == 0 {
		logger.Error("EndGame: invalid game id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid game id",
		}, nil
	}

	// 获取游戏实例
	game, exists := gs.server.getGame(gameID)
	if !exists {
		logger.Error(fmt.Sprintf("EndGame: game %d not found", gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "game not found",
		}, nil
	}

	// 检查用户是否在游戏中
	game.mutex.Lock()
	defer game.mutex.Unlock()

	if _, exists := game.Players[userID]; !exists {
		logger.Error(fmt.Sprintf("EndGame: user %d not in game %d", userID, gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "user not in game",
		}, nil
	}

	// 检查游戏状态
	if game.Status == 2 {
		logger.Warn(fmt.Sprintf("EndGame: game %d already ended", gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -6,
			Msg:    "game already ended",
		}, nil
	}

	// 结束游戏
	game.Status = 2 // 已结束
	game.EndTime = time.Now()
	game.Winner = winner

	// 计算游戏时长
	duration := int32(game.EndTime.Sub(game.StartTime).Seconds())

	// 更新游戏记录
	gameRecord := &database.GameRecord{
		GameID:   gameID,
		RoomID:   game.RoomID,
		GameType: game.GameType,
		Winner:   winner,
		Duration: duration,
		Status:   1, // 已结束
	}

	// 添加玩家信息到记录
	for _, player := range game.Players {
		gamePlayer := database.GamePlayer{
			UserID:   player.UserID,
			Nickname: player.Nickname,
			Level:    player.Level,
			Score:    player.Score,
			Rank:     1, // 简化处理，实际应该根据分数排名
		}
		if player.UserID == winner {
			gamePlayer.Rank = 1
		} else {
			gamePlayer.Rank = 2
		}
		gameRecord.Players = append(gameRecord.Players, gamePlayer)
	}

	if err := gs.server.gameRecordRepo.UpdateRecord(gameRecord); err != nil {
		logger.Error(fmt.Sprintf("EndGame: failed to update game record: %v", err))
		// 不返回错误，继续处理
	}

	// 从内存中移除游戏实例（延迟移除，给客户端时间获取最终状态）
	go func() {
		time.Sleep(5 * time.Minute)
		gs.server.removeGame(gameID)
		logger.Info(fmt.Sprintf("Game %d removed from memory", gameID))
	}()

	logger.Info(fmt.Sprintf("Game %d ended, winner: %d, duration: %d seconds", gameID, winner, duration))

	// 构造响应数据
	responseData := map[string]interface{}{
		"game_id":  gameID,
		"winner":   winner,
		"duration": duration,
		"end_time": game.EndTime.Unix(),
	}

	responseBytes, err := json.Marshal(responseData)
	if err != nil {
		logger.Error(fmt.Sprintf("EndGame: failed to marshal response: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -7,
			Msg:    "failed to create response",
		}, nil
	}

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "game ended successfully",
		Data:   responseBytes,
	}, nil
}

// PlayerAction 玩家操作
func (gs *GameService) PlayerAction(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("PlayerAction: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var actionReq proto.PlayerActionRequest
	if err := proto.Unmarshal(req.Data, &actionReq); err != nil {
		logger.Error(fmt.Sprintf("PlayerAction: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	gameID := actionReq.GetGameId()
	actionType := actionReq.GetActionType()
	actionData := actionReq.GetActionData()

	// 验证游戏ID
	if gameID == 0 {
		logger.Error("PlayerAction: invalid game id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid game id",
		}, nil
	}

	// 获取游戏实例
	game, exists := gs.server.getGame(gameID)
	if !exists {
		logger.Error(fmt.Sprintf("PlayerAction: game %d not found", gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "game not found",
		}, nil
	}

	// 检查用户是否在游戏中
	game.mutex.Lock()
	defer game.mutex.Unlock()

	player, exists := game.Players[userID]
	if !exists {
		logger.Error(fmt.Sprintf("PlayerAction: user %d not in game %d", userID, gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "user not in game",
		}, nil
	}

	// 检查游戏状态
	if game.Status != 1 {
		logger.Error(fmt.Sprintf("PlayerAction: game %d not in progress (status: %d)", gameID, game.Status))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -6,
			Msg:    "game not in progress",
		}, nil
	}

	// 检查是否轮到该玩家
	if game.CurrentPlayer != userID {
		logger.Error(fmt.Sprintf("PlayerAction: not player %d's turn in game %d (current: %d)", userID, gameID, game.CurrentPlayer))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -7,
			Msg:    "not your turn",
		}, nil
	}

	// 处理不同类型的操作
	var actionResult map[string]interface{}
	var err error

	switch actionType {
	case 1: // 出牌
		actionResult, err = gs.handlePlayCard(game, player, actionData)
	case 2: // 使用技能
		actionResult, err = gs.handleUseSkill(game, player, actionData)
	case 3: // 结束回合
		actionResult, err = gs.handleEndTurn(game, player)
	case 4: // 投降
		actionResult, err = gs.handleSurrender(game, player)
	default:
		logger.Error(fmt.Sprintf("PlayerAction: unknown action type %d", actionType))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -8,
			Msg:    "unknown action type",
		}, nil
	}

	if err != nil {
		logger.Error(fmt.Sprintf("PlayerAction: failed to process action: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -9,
			Msg:    fmt.Sprintf("action failed: %v", err),
		}, nil
	}

	logger.Info(fmt.Sprintf("Player %d performed action %d in game %d", userID, actionType, gameID))

	// 构造响应数据
	responseData := map[string]interface{}{
		"game_id":        gameID,
		"action_type":    actionType,
		"action_result":  actionResult,
		"current_player": game.CurrentPlayer,
		"game_status":    game.Status,
	}

	responseBytes, err := json.Marshal(responseData)
	if err != nil {
		logger.Error(fmt.Sprintf("PlayerAction: failed to marshal response: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -10,
			Msg:    "failed to create response",
		}, nil
	}

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "action processed successfully",
		Data:   responseBytes,
	}, nil
}

// GetGameState 获取游戏状态
func (gs *GameService) GetGameState(ctx context.Context, req *proto.BaseRequest) (*proto.BaseResponse, error) {
	// 验证用户ID
	userID := req.Header.GetUserId()
	if userID == 0 {
		logger.Error("GetGameState: invalid user id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -1,
			Msg:    "invalid user id",
		}, nil
	}

	// 解析请求数据
	var stateReq proto.GameStateRequest
	if err := proto.Unmarshal(req.Data, &stateReq); err != nil {
		logger.Error(fmt.Sprintf("GetGameState: failed to unmarshal request: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -2,
			Msg:    "invalid request data",
		}, nil
	}

	gameID := stateReq.GetGameId()

	// 验证游戏ID
	if gameID == 0 {
		logger.Error("GetGameState: invalid game id")
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -3,
			Msg:    "invalid game id",
		}, nil
	}

	// 获取游戏实例
	game, exists := gs.server.getGame(gameID)
	if !exists {
		logger.Error(fmt.Sprintf("GetGameState: game %d not found", gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -4,
			Msg:    "game not found",
		}, nil
	}

	// 检查用户是否在游戏中
	game.mutex.RLock()
	defer game.mutex.RUnlock()

	if _, exists := game.Players[userID]; !exists {
		logger.Error(fmt.Sprintf("GetGameState: user %d not in game %d", userID, gameID))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -5,
			Msg:    "user not in game",
		}, nil
	}

	// 构造玩家信息列表
	var players []*proto.GamePlayerInfo
	for _, player := range game.Players {
		playerInfo := &proto.GamePlayerInfo{
			UserId:   player.UserID,
			Nickname: player.Nickname,
			Level:    player.Level,
			Score:    player.Score,
			Status:   player.Status,
		}
		players = append(players, playerInfo)
	}

	// 序列化游戏数据
	gameDataBytes, err := json.Marshal(game.GameData)
	if err != nil {
		logger.Error(fmt.Sprintf("GetGameState: failed to marshal game data: %v", err))
		gameDataBytes = []byte("{}")
	}

	// 构造游戏状态响应
	gameStateResp := &proto.GameStateResponse{
		GameId:        gameID,
		Status:        game.Status,
		CurrentPlayer: game.CurrentPlayer,
		Players:       players,
		GameData:      gameDataBytes,
	}

	responseData, err := proto.Marshal(gameStateResp)
	if err != nil {
		logger.Error(fmt.Sprintf("GetGameState: failed to marshal response: %v", err))
		return &proto.BaseResponse{
			Header: req.Header,
			Code:   -6,
			Msg:    "failed to create response",
		}, nil
	}

	logger.Debug(fmt.Sprintf("User %d retrieved game state for game %d", userID, gameID))

	return &proto.BaseResponse{
		Header: req.Header,
		Code:   0,
		Msg:    "success",
		Data:   responseData,
	}, nil
}

// handlePlayCard 处理出牌操作
func (gs *GameService) handlePlayCard(game *GameInstance, player *GamePlayerData, actionData []byte) (map[string]interface{}, error) {
	// 简化实现：解析卡牌数据并处理
	var cardData map[string]interface{}
	if err := json.Unmarshal(actionData, &cardData); err != nil {
		return nil, fmt.Errorf("invalid card data: %v", err)
	}

	// 这里应该实现具体的卡牌逻辑
	// 简化处理：增加玩家分数
	player.Score += 10

	// 切换到下一个玩家
	gs.switchToNextPlayer(game)

	return map[string]interface{}{
		"action": "play_card",
		"card":   cardData,
		"score":  player.Score,
	}, nil
}

// handleUseSkill 处理使用技能操作
func (gs *GameService) handleUseSkill(game *GameInstance, player *GamePlayerData, actionData []byte) (map[string]interface{}, error) {
	// 简化实现：解析技能数据并处理
	var skillData map[string]interface{}
	if err := json.Unmarshal(actionData, &skillData); err != nil {
		return nil, fmt.Errorf("invalid skill data: %v", err)
	}

	// 这里应该实现具体的技能逻辑
	// 简化处理：增加玩家分数
	player.Score += 20

	return map[string]interface{}{
		"action": "use_skill",
		"skill":  skillData,
		"score":  player.Score,
	}, nil
}

// handleEndTurn 处理结束回合操作
func (gs *GameService) handleEndTurn(game *GameInstance, player *GamePlayerData) (map[string]interface{}, error) {
	// 切换到下一个玩家
	gs.switchToNextPlayer(game)

	return map[string]interface{}{
		"action":         "end_turn",
		"current_player": game.CurrentPlayer,
	}, nil
}

// handleSurrender 处理投降操作
func (gs *GameService) handleSurrender(game *GameInstance, player *GamePlayerData) (map[string]interface{}, error) {
	// 设置玩家状态为已离开
	player.Status = 3

	// 如果只剩一个玩家，结束游戏
	activePlayerCount := 0
	var lastActivePlayer uint64
	for _, p := range game.Players {
		if p.Status != 3 {
			activePlayerCount++
			lastActivePlayer = p.UserID
		}
	}

	if activePlayerCount <= 1 {
		game.Status = 2 // 游戏结束
		game.Winner = lastActivePlayer
		game.EndTime = time.Now()
	}

	return map[string]interface{}{
		"action":      "surrender",
		"game_status": game.Status,
		"winner":      game.Winner,
	}, nil
}

// switchToNextPlayer 切换到下一个玩家
func (gs *GameService) switchToNextPlayer(game *GameInstance) {
	// 简化实现：在活跃玩家中轮换
	var playerIDs []uint64
	for _, player := range game.Players {
		if player.Status != 3 { // 不是已离开状态
			playerIDs = append(playerIDs, player.UserID)
		}
	}

	if len(playerIDs) <= 1 {
		return // 只有一个或没有活跃玩家
	}

	// 找到当前玩家的索引
	currentIndex := -1
	for i, playerID := range playerIDs {
		if playerID == game.CurrentPlayer {
			currentIndex = i
			break
		}
	}

	// 切换到下一个玩家
	if currentIndex >= 0 {
		nextIndex := (currentIndex + 1) % len(playerIDs)
		game.CurrentPlayer = playerIDs[nextIndex]
	}
}
