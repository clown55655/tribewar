package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-playground/validator/v10"
	"golang.org/x/crypto/bcrypt"

	"tribeway/internal/logger"
)

// SecurityManager 安全管理器
type SecurityManager struct {
	encryption *EncryptionManager
	auth       *AuthManager
	rateLimit  *RateLimitManager
	validator  *validator.Validate
	blacklist  *IPBlacklist
	antiCheat  *AntiCheatSystem
	jwtSecret  []byte
	mutex      sync.RWMutex
}

// EncryptionManager 加密管理器
type EncryptionManager struct {
	gcm cipher.AEAD
	key []byte
}

// AuthManager 认证管理器
type AuthManager struct {
	sessions    map[string]*Session
	tokenSecret []byte
	tokenExpiry time.Duration
	mutex       sync.RWMutex
}

// RateLimitManager 限流管理器
type RateLimitManager struct {
	limiters map[string]*RateLimiter
	mutex    sync.RWMutex
}

// RateLimiter 限流器
type RateLimiter struct {
	requests    int
	window      time.Duration
	lastReset   time.Time
	maxRequests int
}

// IPBlacklist IP黑名单
type IPBlacklist struct {
	blocked map[string]time.Time
	mutex   sync.RWMutex
}

// AntiCheatSystem 反作弊系统
type AntiCheatSystem struct {
	suspiciousActions map[uint64][]SuspiciousAction
	patterns          []CheatPattern
	mutex             sync.RWMutex
}

// Session 会话信息
type Session struct {
	UserID       uint64
	Token        string
	CreatedAt    time.Time
	LastActivity time.Time
	IP           string
	UserAgent    string
	Permissions  []string
}

// SuspiciousAction 可疑行为
type SuspiciousAction struct {
	Type      string
	Timestamp time.Time
	Data      interface{}
	Score     float64
}

// CheatPattern 作弊模式
type CheatPattern struct {
	Name        string
	Description string
	Detector    func(actions []SuspiciousAction) float64
	Threshold   float64
}

// TokenClaims JWT声明
type TokenClaims struct {
	UserID      uint64   `json:"user_id"`
	Username    string   `json:"username"`
	Permissions []string `json:"permissions"`
	jwt.StandardClaims
}

// NewSecurityManager 创建安全管理器
func NewSecurityManager() (*SecurityManager, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %v", err)
	}

	jwtSecret := make([]byte, 32)
	if _, err := rand.Read(jwtSecret); err != nil {
		return nil, fmt.Errorf("failed to generate JWT secret: %v", err)
	}

	return NewSecurityManagerWithSecrets(key, jwtSecret)
}

func NewSecurityManagerFromEnv(encryptionKeyEnv, jwtSecretEnv string) (*SecurityManager, error) {
	encryptionKey, err := readSecretEnv(encryptionKeyEnv, 32)
	if err != nil {
		return nil, fmt.Errorf("encryption key: %w", err)
	}
	jwtSecret, err := readSecretEnv(jwtSecretEnv, 32)
	if err != nil {
		return nil, fmt.Errorf("jwt secret: %w", err)
	}
	return NewSecurityManagerWithSecrets(encryptionKey, jwtSecret)
}

func NewSecurityManagerWithSecrets(key, jwtSecret []byte) (*SecurityManager, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes")
	}
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("jwt secret must be at least 32 bytes")
	}

	// 创建加密管理器
	encryptionManager, err := NewEncryptionManager(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryption manager: %v", err)
	}

	manager := &SecurityManager{
		encryption: encryptionManager,
		auth:       NewAuthManager(jwtSecret, 24*time.Hour),
		rateLimit:  NewRateLimitManager(),
		validator:  validator.New(),
		blacklist:  NewIPBlacklist(),
		antiCheat:  NewAntiCheatSystem(),
		jwtSecret:  jwtSecret,
	}

	logger.Info("Security manager initialized")
	return manager, nil
}

func readSecretEnv(name string, minLength int) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("environment variable name is empty")
	}
	value := os.Getenv(name)
	if value == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	if len([]byte(value)) < minLength {
		return nil, fmt.Errorf("%s must be at least %d bytes", name, minLength)
	}
	return []byte(value), nil
}

// NewEncryptionManager 创建加密管理器
func NewEncryptionManager(key []byte) (*EncryptionManager, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &EncryptionManager{
		gcm: gcm,
		key: key,
	}, nil
}

// Encrypt 加密数据
func (em *EncryptionManager) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, em.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := em.gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt 解密数据
func (em *EncryptionManager) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := em.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := em.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// EncryptString 加密字符串并返回Base64编码
func (em *EncryptionManager) EncryptString(plaintext string) (string, error) {
	ciphertext, err := em.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString 解密Base64编码的字符串
func (em *EncryptionManager) DecryptString(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	plaintext, err := em.Decrypt(data)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// NewAuthManager 创建认证管理器
func NewAuthManager(tokenSecret []byte, tokenExpiry time.Duration) *AuthManager {
	return &AuthManager{
		sessions:    make(map[string]*Session),
		tokenSecret: tokenSecret,
		tokenExpiry: tokenExpiry,
	}
}

// HashPassword 哈希密码
func (am *AuthManager) HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

// VerifyPassword 验证密码
func (am *AuthManager) VerifyPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GenerateToken 生成JWT令牌
func (am *AuthManager) GenerateToken(userID uint64, username string, permissions []string) (string, error) {
	claims := &TokenClaims{
		UserID:      userID,
		Username:    username,
		Permissions: permissions,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(am.tokenExpiry).Unix(),
			IssuedAt:  time.Now().Unix(),
			Issuer:    "tribeway-game-server",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.tokenSecret)
}

// ValidateToken 验证JWT令牌
func (am *AuthManager) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return am.tokenSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*TokenClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// CreateSession 创建会话
func (am *AuthManager) CreateSession(userID uint64, ip, userAgent string, permissions []string) (*Session, error) {
	sessionToken := generateSessionToken()

	session := &Session{
		UserID:       userID,
		Token:        sessionToken,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		IP:           ip,
		UserAgent:    userAgent,
		Permissions:  permissions,
	}

	am.mutex.Lock()
	am.sessions[sessionToken] = session
	am.mutex.Unlock()

	logger.Info(fmt.Sprintf("Session created for user %d", userID))
	return session, nil
}

// ValidateSession 验证会话
func (am *AuthManager) ValidateSession(token string) (*Session, error) {
	am.mutex.RLock()
	session, exists := am.sessions[token]
	am.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	// 检查会话是否过期
	if time.Since(session.LastActivity) > am.tokenExpiry {
		am.InvalidateSession(token)
		return nil, fmt.Errorf("session expired")
	}

	// 更新最后活动时间
	session.LastActivity = time.Now()
	return session, nil
}

// InvalidateSession 无效化会话
func (am *AuthManager) InvalidateSession(token string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	if session, exists := am.sessions[token]; exists {
		logger.Info(fmt.Sprintf("Session invalidated for user %d", session.UserID))
		delete(am.sessions, token)
	}
}

// generateSessionToken 生成会话令牌
func generateSessionToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// NewRateLimitManager 创建限流管理器
func NewRateLimitManager() *RateLimitManager {
	return &RateLimitManager{
		limiters: make(map[string]*RateLimiter),
	}
}

// CheckLimit 检查限流
func (rlm *RateLimitManager) CheckLimit(key string, maxRequests int, window time.Duration) bool {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	limiter, exists := rlm.limiters[key]
	if !exists {
		limiter = &RateLimiter{
			requests:    0,
			window:      window,
			lastReset:   time.Now(),
			maxRequests: maxRequests,
		}
		rlm.limiters[key] = limiter
	}

	now := time.Now()

	// 重置窗口
	if now.Sub(limiter.lastReset) > limiter.window {
		limiter.requests = 0
		limiter.lastReset = now
	}

	// 检查是否超限
	if limiter.requests >= limiter.maxRequests {
		return false
	}

	limiter.requests++
	return true
}

// NewIPBlacklist 创建IP黑名单
func NewIPBlacklist() *IPBlacklist {
	return &IPBlacklist{
		blocked: make(map[string]time.Time),
	}
}

// IsBlocked 检查IP是否被阻止
func (bl *IPBlacklist) IsBlocked(ip string) bool {
	bl.mutex.RLock()
	defer bl.mutex.RUnlock()

	blockTime, exists := bl.blocked[ip]
	if !exists {
		return false
	}

	// 检查是否已过期
	if time.Since(blockTime) > 24*time.Hour {
		delete(bl.blocked, ip)
		return false
	}

	return true
}

// BlockIP 阻止IP
func (bl *IPBlacklist) BlockIP(ip string, duration time.Duration) {
	bl.mutex.Lock()
	defer bl.mutex.Unlock()

	bl.blocked[ip] = time.Now().Add(duration)
	logger.Warn(fmt.Sprintf("IP blocked: %s for %v", ip, duration))
}

// UnblockIP 解除IP阻止
func (bl *IPBlacklist) UnblockIP(ip string) {
	bl.mutex.Lock()
	defer bl.mutex.Unlock()

	delete(bl.blocked, ip)
	logger.Info(fmt.Sprintf("IP unblocked: %s", ip))
}

// NewAntiCheatSystem 创建反作弊系统
func NewAntiCheatSystem() *AntiCheatSystem {
	acs := &AntiCheatSystem{
		suspiciousActions: make(map[uint64][]SuspiciousAction),
		patterns:          make([]CheatPattern, 0),
	}

	// 添加默认作弊模式
	acs.addDefaultPatterns()

	return acs
}

// addDefaultPatterns 添加默认作弊模式
func (acs *AntiCheatSystem) addDefaultPatterns() {
	// 频率异常模式
	acs.patterns = append(acs.patterns, CheatPattern{
		Name:        "high_frequency",
		Description: "异常高频操作",
		Threshold:   0.8,
		Detector: func(actions []SuspiciousAction) float64 {
			if len(actions) < 10 {
				return 0
			}

			// 计算最近10秒内的操作频率
			recentActions := 0
			now := time.Now()
			for _, action := range actions {
				if now.Sub(action.Timestamp) <= 10*time.Second {
					recentActions++
				}
			}

			if recentActions > 50 { // 10秒内超过50次操作
				return 1.0
			}
			return float64(recentActions) / 50.0
		},
	})

	// 时间异常模式
	acs.patterns = append(acs.patterns, CheatPattern{
		Name:        "timing_anomaly",
		Description: "操作时间异常",
		Threshold:   0.7,
		Detector: func(actions []SuspiciousAction) float64 {
			if len(actions) < 5 {
				return 0
			}

			// 检查操作间隔是否过于规律
			intervals := make([]time.Duration, 0)
			for i := 1; i < len(actions); i++ {
				interval := actions[i].Timestamp.Sub(actions[i-1].Timestamp)
				intervals = append(intervals, interval)
			}

			// 计算间隔的标准差
			if len(intervals) > 0 {
				var sum time.Duration
				for _, interval := range intervals {
					sum += interval
				}
				avg := sum / time.Duration(len(intervals))

				var variance time.Duration
				for _, interval := range intervals {
					diff := interval - avg
					variance += diff * diff / time.Duration(len(intervals))
				}

				// 如果标准差很小，说明操作过于规律
				if variance < time.Millisecond*10 {
					return 0.9
				}
			}

			return 0
		},
	})
}

// RecordAction 记录可疑行为
func (acs *AntiCheatSystem) RecordAction(userID uint64, actionType string, data interface{}, score float64) {
	acs.mutex.Lock()
	defer acs.mutex.Unlock()

	action := SuspiciousAction{
		Type:      actionType,
		Timestamp: time.Now(),
		Data:      data,
		Score:     score,
	}

	acs.suspiciousActions[userID] = append(acs.suspiciousActions[userID], action)

	// 清理过期记录（保留最近1小时的记录）
	cutoff := time.Now().Add(-time.Hour)
	validActions := make([]SuspiciousAction, 0)
	for _, a := range acs.suspiciousActions[userID] {
		if a.Timestamp.After(cutoff) {
			validActions = append(validActions, a)
		}
	}
	acs.suspiciousActions[userID] = validActions
}

// CheckCheat 检查作弊
func (acs *AntiCheatSystem) CheckCheat(userID uint64) (bool, []string) {
	acs.mutex.RLock()
	defer acs.mutex.RUnlock()

	actions, exists := acs.suspiciousActions[userID]
	if !exists || len(actions) == 0 {
		return false, nil
	}

	detectedPatterns := make([]string, 0)

	for _, pattern := range acs.patterns {
		score := pattern.Detector(actions)
		if score >= pattern.Threshold {
			detectedPatterns = append(detectedPatterns, pattern.Name)
			logger.Warn(fmt.Sprintf("Cheat pattern detected for user %d: %s (score: %.2f)",
				userID, pattern.Name, score))
		}
	}

	return len(detectedPatterns) > 0, detectedPatterns
}

// ValidateInput 验证输入数据
func (sm *SecurityManager) ValidateInput(data interface{}) error {
	return sm.validator.Struct(data)
}

// CheckIPSecurity 检查IP安全性
func (sm *SecurityManager) CheckIPSecurity(ip string) error {
	// 检查IP黑名单
	if sm.blacklist.IsBlocked(ip) {
		return fmt.Errorf("IP is blocked")
	}

	// 检查IP格式
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address")
	}

	// 检查是否是私有IP（在生产环境中可能需要阻止）
	if isPrivateIP(ip) {
		logger.Debug(fmt.Sprintf("Private IP detected: %s", ip))
	}

	return nil
}

// isPrivateIP 检查是否是私有IP
func isPrivateIP(ip string) bool {
	privateIPRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}

	ipAddr := net.ParseIP(ip)
	for _, cidr := range privateIPRanges {
		_, ipNet, _ := net.ParseCIDR(cidr)
		if ipNet.Contains(ipAddr) {
			return true
		}
	}
	return false
}

// SanitizeInput 清理输入数据
func (sm *SecurityManager) SanitizeInput(input string) string {
	// 移除潜在的恶意字符
	dangerous := []string{
		"<script", "</script>", "javascript:", "onload=", "onerror=",
		"<iframe", "</iframe>", "eval(", "alert(", "confirm(",
	}

	sanitized := input
	for _, danger := range dangerous {
		sanitized = strings.ReplaceAll(sanitized, danger, "")
		sanitized = strings.ReplaceAll(sanitized, strings.ToUpper(danger), "")
	}

	return strings.TrimSpace(sanitized)
}

// GenerateSignature 生成数据签名
func (sm *SecurityManager) GenerateSignature(data []byte) string {
	mac := hmac.New(sha256.New, sm.jwtSecret)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature 验证数据签名
func (sm *SecurityManager) VerifySignature(data []byte, signature string) bool {
	expectedSignature := sm.GenerateSignature(data)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// GetSecurityMetrics 获取安全指标
func (sm *SecurityManager) GetSecurityMetrics() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	sm.blacklist.mutex.RLock()
	blockedIPs := len(sm.blacklist.blocked)
	sm.blacklist.mutex.RUnlock()

	sm.auth.mutex.RLock()
	activeSessions := len(sm.auth.sessions)
	sm.auth.mutex.RUnlock()

	sm.rateLimit.mutex.RLock()
	rateLimiters := len(sm.rateLimit.limiters)
	sm.rateLimit.mutex.RUnlock()

	return map[string]interface{}{
		"blocked_ips":     blockedIPs,
		"active_sessions": activeSessions,
		"rate_limiters":   rateLimiters,
		"timestamp":       time.Now().Unix(),
	}
}
