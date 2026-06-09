package main

import (
	"fmt"
	"time"
)

// 卡牌游戏逻辑插件
// 这是一个可热更新的游戏逻辑模块示例

// CardGameLogic 卡牌游戏逻辑
type CardGameLogic struct {
	Version string
	Config  map[string]interface{}
}

// Card 卡牌定义
type Card struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Cost     int    `json:"cost"`
	Attack   int    `json:"attack"`
	Health   int    `json:"health"`
	CardType string `json:"card_type"`
	Rarity   string `json:"rarity"`
	Effect   string `json:"effect"`
}

// GameState 游戏状态
type GameState struct {
	CurrentPlayer int           `json:"current_player"`
	Turn          int           `json:"turn"`
	Phase         string        `json:"phase"`
	PlayerStates  []PlayerState `json:"player_states"`
	Board         []Card        `json:"board"`
	GameLog       []string      `json:"game_log"`
}

// PlayerState 玩家状态
type PlayerState struct {
	PlayerID  uint64 `json:"player_id"`
	Health    int    `json:"health"`
	Mana      int    `json:"mana"`
	MaxMana   int    `json:"max_mana"`
	HandCards []Card `json:"hand_cards"`
	DeckSize  int    `json:"deck_size"`
}

// PlayCardAction 出牌动作
type PlayCardAction struct {
	CardID   int `json:"card_id"`
	Position int `json:"position"`
	Target   int `json:"target"`
}

// AttackAction 攻击动作
type AttackAction struct {
	AttackerID int `json:"attacker_id"`
	TargetID   int `json:"target_id"`
}

// 插件导出函数

// GetVersion 获取插件版本
func GetVersion() string {
	return "1.0.0"
}

// Initialize 初始化游戏逻辑
func Initialize(config map[string]interface{}) error {
	fmt.Println("Card game logic plugin initialized")
	return nil
}

// ValidatePlayCard 验证出牌动作
func ValidatePlayCard(gameState *GameState, playerID uint64, action *PlayCardAction) error {
	// 检查是否轮到该玩家
	currentPlayerIndex := gameState.CurrentPlayer
	if gameState.PlayerStates[currentPlayerIndex].PlayerID != playerID {
		return fmt.Errorf("not your turn")
	}

	// 检查游戏阶段
	if gameState.Phase != "main" {
		return fmt.Errorf("cannot play cards in %s phase", gameState.Phase)
	}

	// 检查玩家是否拥有该卡牌
	playerState := &gameState.PlayerStates[currentPlayerIndex]
	cardFound := false
	cardCost := 0

	for _, card := range playerState.HandCards {
		if card.ID == action.CardID {
			cardFound = true
			cardCost = card.Cost
			break
		}
	}

	if !cardFound {
		return fmt.Errorf("card not found in hand")
	}

	// 检查法力值是否足够
	if playerState.Mana < cardCost {
		return fmt.Errorf("insufficient mana: have %d, need %d", playerState.Mana, cardCost)
	}

	// 检查场上位置是否有效
	if action.Position < 0 || action.Position > 7 {
		return fmt.Errorf("invalid board position: %d", action.Position)
	}

	return nil
}

// ProcessPlayCard 处理出牌动作
func ProcessPlayCard(gameState *GameState, playerID uint64, action *PlayCardAction) (*GameState, []string, error) {
	currentPlayerIndex := gameState.CurrentPlayer
	playerState := &gameState.PlayerStates[currentPlayerIndex]

	// 找到并移除手牌
	var playedCard Card
	newHand := make([]Card, 0, len(playerState.HandCards)-1)

	for _, card := range playerState.HandCards {
		if card.ID == action.CardID {
			playedCard = card
		} else {
			newHand = append(newHand, card)
		}
	}

	playerState.HandCards = newHand
	playerState.Mana -= playedCard.Cost

	// 将卡牌放到场上
	gameState.Board = append(gameState.Board, playedCard)

	// 生成游戏日志
	events := []string{
		fmt.Sprintf("Player %d played card %s (Cost: %d)", playerID, playedCard.Name, playedCard.Cost),
	}

	// 触发卡牌效果
	if effectEvents, err := processCardEffect(gameState, &playedCard, playerID); err == nil {
		events = append(events, effectEvents...)
	}

	gameState.GameLog = append(gameState.GameLog, events...)

	return gameState, events, nil
}

// ValidateAttack 验证攻击动作
func ValidateAttack(gameState *GameState, playerID uint64, action *AttackAction) error {
	// 检查是否轮到该玩家
	currentPlayerIndex := gameState.CurrentPlayer
	if gameState.PlayerStates[currentPlayerIndex].PlayerID != playerID {
		return fmt.Errorf("not your turn")
	}

	// 检查游戏阶段
	if gameState.Phase != "combat" {
		return fmt.Errorf("cannot attack in %s phase", gameState.Phase)
	}

	// 检查攻击者是否存在且属于当前玩家
	attackerFound := false
	for _, card := range gameState.Board {
		if card.ID == action.AttackerID {
			attackerFound = true
			break
		}
	}

	if !attackerFound {
		return fmt.Errorf("attacker not found on board")
	}

	return nil
}

// ProcessAttack 处理攻击动作
func ProcessAttack(gameState *GameState, playerID uint64, action *AttackAction) (*GameState, []string, error) {
	var attacker, target *Card

	// 找到攻击者和目标
	for i := range gameState.Board {
		if gameState.Board[i].ID == action.AttackerID {
			attacker = &gameState.Board[i]
		}
		if gameState.Board[i].ID == action.TargetID {
			target = &gameState.Board[i]
		}
	}

	if attacker == nil {
		return gameState, nil, fmt.Errorf("attacker not found")
	}

	events := make([]string, 0)

	if target != nil {
		// 随从攻击随从
		target.Health -= attacker.Attack
		attacker.Health -= target.Attack

		events = append(events, fmt.Sprintf("%s attacks %s (%d damage)",
			attacker.Name, target.Name, attacker.Attack))

		// 检查随从是否死亡
		newBoard := make([]Card, 0)
		for _, card := range gameState.Board {
			if card.Health > 0 {
				newBoard = append(newBoard, card)
			} else {
				events = append(events, fmt.Sprintf("%s destroyed", card.Name))
			}
		}
		gameState.Board = newBoard
	} else {
		// 攻击对手英雄
		opponentIndex := 1 - gameState.CurrentPlayer
		gameState.PlayerStates[opponentIndex].Health -= attacker.Attack

		events = append(events, fmt.Sprintf("%s attacks enemy hero (%d damage)",
			attacker.Name, attacker.Attack))

		// 检查游戏是否结束
		if gameState.PlayerStates[opponentIndex].Health <= 0 {
			events = append(events, fmt.Sprintf("Player %d wins!",
				gameState.PlayerStates[gameState.CurrentPlayer].PlayerID))
			gameState.Phase = "ended"
		}
	}

	gameState.GameLog = append(gameState.GameLog, events...)
	return gameState, events, nil
}

// processCardEffect 处理卡牌效果
func processCardEffect(gameState *GameState, card *Card, playerID uint64) ([]string, error) {
	events := make([]string, 0)

	switch card.Effect {
	case "draw_card":
		// 抽一张牌
		currentPlayerIndex := gameState.CurrentPlayer
		playerState := &gameState.PlayerStates[currentPlayerIndex]

		if playerState.DeckSize > 0 {
			// 这里应该从牌堆抽牌，简化为增加手牌数
			playerState.DeckSize--
			events = append(events, fmt.Sprintf("Player %d draws a card", playerID))
		}

	case "heal":
		// 治疗效果
		currentPlayerIndex := gameState.CurrentPlayer
		healAmount := 3 // 可以从卡牌数据中读取

		oldHealth := gameState.PlayerStates[currentPlayerIndex].Health
		gameState.PlayerStates[currentPlayerIndex].Health += healAmount

		// 不能超过最大生命值
		maxHealth := 30 // 默认最大生命值
		if gameState.PlayerStates[currentPlayerIndex].Health > maxHealth {
			gameState.PlayerStates[currentPlayerIndex].Health = maxHealth
		}

		actualHeal := gameState.PlayerStates[currentPlayerIndex].Health - oldHealth
		if actualHeal > 0 {
			events = append(events, fmt.Sprintf("Player %d healed for %d", playerID, actualHeal))
		}

	case "damage_all":
		// 对所有敌方随从造成伤害
		damageAmount := 1

		for i := range gameState.Board {
			// 简化：假设所有场上随从都是敌方的
			gameState.Board[i].Health -= damageAmount
			events = append(events, fmt.Sprintf("%s takes %d damage",
				gameState.Board[i].Name, damageAmount))
		}
	}

	return events, nil
}

// EndTurn 结束回合
func EndTurn(gameState *GameState, playerID uint64) (*GameState, []string, error) {
	currentPlayerIndex := gameState.CurrentPlayer

	if gameState.PlayerStates[currentPlayerIndex].PlayerID != playerID {
		return gameState, nil, fmt.Errorf("not your turn")
	}

	// 切换到下一个玩家
	gameState.CurrentPlayer = 1 - gameState.CurrentPlayer
	gameState.Turn++

	// 新回合开始处理
	newPlayerIndex := gameState.CurrentPlayer
	newPlayerState := &gameState.PlayerStates[newPlayerIndex]

	// 增加法力值
	if newPlayerState.MaxMana < 10 {
		newPlayerState.MaxMana++
	}
	newPlayerState.Mana = newPlayerState.MaxMana

	events := []string{
		fmt.Sprintf("Player %d's turn ended", playerID),
		fmt.Sprintf("Player %d's turn begins (Turn %d)",
			newPlayerState.PlayerID, gameState.Turn),
	}

	// 抽牌
	if newPlayerState.DeckSize > 0 {
		newPlayerState.DeckSize--
		events = append(events, fmt.Sprintf("Player %d draws a card", newPlayerState.PlayerID))
	}

	gameState.GameLog = append(gameState.GameLog, events...)
	return gameState, events, nil
}

// CalculateDamage 计算伤害
func CalculateDamage(attacker, target *Card, bonusDamage int) int {
	baseDamage := attacker.Attack + bonusDamage

	// 这里可以添加各种伤害计算规则
	// 比如护甲减免、伤害加成等

	return baseDamage
}

// CheckWinCondition 检查胜利条件
func CheckWinCondition(gameState *GameState) (bool, uint64) {
	for i, playerState := range gameState.PlayerStates {
		if playerState.Health <= 0 {
			winnerIndex := 1 - i
			return true, gameState.PlayerStates[winnerIndex].PlayerID
		}
	}

	// 检查其他胜利条件...

	return false, 0
}

// GetValidActions 获取有效动作列表
func GetValidActions(gameState *GameState, playerID uint64) []string {
	actions := make([]string, 0)

	currentPlayerIndex := gameState.CurrentPlayer
	if gameState.PlayerStates[currentPlayerIndex].PlayerID != playerID {
		return actions // 不是该玩家的回合
	}

	switch gameState.Phase {
	case "main":
		// 可以出牌
		playerState := gameState.PlayerStates[currentPlayerIndex]
		for _, card := range playerState.HandCards {
			if card.Cost <= playerState.Mana {
				actions = append(actions, fmt.Sprintf("play_card_%d", card.ID))
			}
		}

		// 可以结束回合
		actions = append(actions, "end_turn")

	case "combat":
		// 可以攻击
		for _, card := range gameState.Board {
			actions = append(actions, fmt.Sprintf("attack_with_%d", card.ID))
		}

		// 可以结束战斗阶段
		actions = append(actions, "end_combat")
	}

	return actions
}

// ApplyBuffs 应用增益效果
func ApplyBuffs(card *Card, buffs []string) *Card {
	buffedCard := *card // 复制卡牌

	for _, buff := range buffs {
		switch buff {
		case "charge":
			// 冲锋效果：可以立即攻击
		case "taunt":
			// 嘲讽效果：必须优先攻击
		case "divine_shield":
			// 圣盾效果：免疫一次伤害
		case "+1_attack":
			buffedCard.Attack++
		case "+1_health":
			buffedCard.Health++
		case "windfury":
			// 风怒效果：可以攻击两次
		}
	}

	return &buffedCard
}

// GetCardDatabase 获取卡牌数据库
func GetCardDatabase() []Card {
	return []Card{
		{ID: 1, Name: "Wisp", Cost: 0, Attack: 1, Health: 1, CardType: "minion", Rarity: "basic"},
		{ID: 2, Name: "Murloc Raider", Cost: 1, Attack: 2, Health: 1, CardType: "minion", Rarity: "basic"},
		{ID: 3, Name: "River Crocolisk", Cost: 2, Attack: 2, Health: 3, CardType: "minion", Rarity: "basic"},
		{ID: 4, Name: "Magma Rager", Cost: 3, Attack: 5, Health: 1, CardType: "minion", Rarity: "basic"},
		{ID: 5, Name: "Chillwind Yeti", Cost: 4, Attack: 4, Health: 5, CardType: "minion", Rarity: "basic"},
		{ID: 6, Name: "Boulderfist Ogre", Cost: 6, Attack: 6, Health: 7, CardType: "minion", Rarity: "basic"},
		{ID: 7, Name: "Core Hound", Cost: 7, Attack: 9, Health: 5, CardType: "minion", Rarity: "basic"},

		{ID: 11, Name: "Fireball", Cost: 4, Attack: 6, CardType: "spell", Rarity: "basic", Effect: "damage"},
		{ID: 12, Name: "Healing Potion", Cost: 1, CardType: "spell", Rarity: "basic", Effect: "heal"},
		{ID: 13, Name: "Card Draw", Cost: 2, CardType: "spell", Rarity: "basic", Effect: "draw_card"},
		{ID: 14, Name: "Lightning Bolt", Cost: 1, Attack: 3, CardType: "spell", Rarity: "basic", Effect: "damage"},
		{ID: 15, Name: "Holy Light", Cost: 2, CardType: "spell", Rarity: "basic", Effect: "heal"},
	}
}

// BuildDeck 构建牌组
func BuildDeck(cardIDs []int) ([]Card, error) {
	cardDB := GetCardDatabase()
	cardMap := make(map[int]Card)

	for _, card := range cardDB {
		cardMap[card.ID] = card
	}

	deck := make([]Card, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		if card, exists := cardMap[cardID]; exists {
			deck = append(deck, card)
		} else {
			return nil, fmt.Errorf("card not found: %d", cardID)
		}
	}

	return deck, nil
}

// ShuffleDeck 洗牌
func ShuffleDeck(deck []Card) []Card {
	shuffled := make([]Card, len(deck))
	copy(shuffled, deck)

	// 简单的洗牌算法
	for i := len(shuffled) - 1; i > 0; i-- {
		j := time.Now().UnixNano() % int64(i+1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled
}

// CalculateGameScore 计算游戏积分
func CalculateGameScore(gameState *GameState, playerID uint64) int {
	score := 0

	// 基础分数
	if hasWinner, winnerID := CheckWinCondition(gameState); hasWinner && winnerID == playerID {
		score += 100 // 胜利奖励
	}

	// 回合数奖励（快速获胜有额外奖励）
	if gameState.Turn <= 10 {
		score += 50 - gameState.Turn*2
	}

	// 剩余生命值奖励
	for _, playerState := range gameState.PlayerStates {
		if playerState.PlayerID == playerID {
			score += playerState.Health
			break
		}
	}

	return score
}

// GetAIAction AI动作决策（简单AI示例）
func GetAIAction(gameState *GameState, playerID uint64) string {
	validActions := GetValidActions(gameState, playerID)

	if len(validActions) == 0 {
		return ""
	}

	// 简单AI：优先出牌，然后攻击
	for _, action := range validActions {
		if action != "end_turn" {
			return action
		}
	}

	return "end_turn"
}

// main 插件主函数（必需）
func main() {
	// 插件编译时的入口点
	fmt.Println("Card game logic plugin compiled successfully")
}
