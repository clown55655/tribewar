package database

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"tribeway/internal/logger"
)

// RedisConfig Redis配置
type RedisConfig struct {
	// 单机模式配置
	Addr        string `yaml:"addr"`
	Password    string `yaml:"password"`
	PasswordEnv string `yaml:"password_env"`
	DB          int    `yaml:"db"`

	// 集群模式配置
	ClusterMode  bool     `yaml:"cluster_mode"`
	ClusterAddrs []string `yaml:"cluster_addrs"`

	// 哨兵模式配置
	SentinelMode   bool     `yaml:"sentinel_mode"`
	SentinelAddrs  []string `yaml:"sentinel_addrs"`
	SentinelMaster string   `yaml:"sentinel_master"`

	// 通用配置
	PoolSize     int           `yaml:"pool_size"`
	MaxRetries   int           `yaml:"max_retries"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`

	// 集群特有配置
	MaxRedirects   int  `yaml:"max_redirects"`
	ReadOnly       bool `yaml:"read_only"`
	RouteByLatency bool `yaml:"route_by_latency"`
	RouteRandomly  bool `yaml:"route_randomly"`
}

// RedisManager Redis管理器
type RedisManager struct {
	client         redis.Cmdable // 可以是Client、ClusterClient或SentinelClient
	clusterClient  *redis.ClusterClient
	sentinelClient *redis.Client
	config         *RedisConfig
	ctx            context.Context
	mutex          sync.RWMutex
	mode           string // "single", "cluster", "sentinel"
}

// NewRedisManager 创建Redis管理器
func NewRedisManager(config *RedisConfig) (*RedisManager, error) {
	ctx := context.Background()

	manager := &RedisManager{
		config: config,
		ctx:    ctx,
	}

	var err error

	// 根据配置选择Redis模式
	if config.ClusterMode {
		manager.mode = "cluster"
		err = manager.initClusterMode()
	} else if config.SentinelMode {
		manager.mode = "sentinel"
		err = manager.initSentinelMode()
	} else {
		manager.mode = "single"
		err = manager.initSingleMode()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize redis: %v", err)
	}

	// 测试连接
	if err := manager.client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %v", err)
	}

	logger.Infof("Redis connected in %s mode", manager.mode)
	return manager, nil
}

// initSingleMode 初始化单机模式
func (rm *RedisManager) initSingleMode() error {
	client := redis.NewClient(&redis.Options{
		Addr:         rm.config.Addr,
		Password:     rm.password(),
		DB:           rm.config.DB,
		PoolSize:     rm.config.PoolSize,
		MaxRetries:   rm.config.MaxRetries,
		DialTimeout:  rm.config.DialTimeout,
		ReadTimeout:  rm.config.ReadTimeout,
		WriteTimeout: rm.config.WriteTimeout,
		IdleTimeout:  rm.config.IdleTimeout,
	})

	rm.client = client
	logger.Infof("Redis single mode initialized: %s", rm.config.Addr)
	return nil
}

// initClusterMode 初始化集群模式
func (rm *RedisManager) initClusterMode() error {
	if len(rm.config.ClusterAddrs) == 0 {
		return fmt.Errorf("cluster addresses not configured")
	}

	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        rm.config.ClusterAddrs,
		Password:     rm.password(),
		PoolSize:     rm.config.PoolSize,
		MaxRetries:   rm.config.MaxRetries,
		DialTimeout:  rm.config.DialTimeout,
		ReadTimeout:  rm.config.ReadTimeout,
		WriteTimeout: rm.config.WriteTimeout,
		IdleTimeout:  rm.config.IdleTimeout,

		// 集群特有配置
		MaxRedirects:   rm.config.MaxRedirects,
		ReadOnly:       rm.config.ReadOnly,
		RouteByLatency: rm.config.RouteByLatency,
		RouteRandomly:  rm.config.RouteRandomly,
	})

	rm.client = clusterClient
	rm.clusterClient = clusterClient
	logger.Infof("Redis cluster mode initialized: %s", strings.Join(rm.config.ClusterAddrs, ","))
	return nil
}

// initSentinelMode 初始化哨兵模式
func (rm *RedisManager) initSentinelMode() error {
	if len(rm.config.SentinelAddrs) == 0 {
		return fmt.Errorf("sentinel addresses not configured")
	}

	if rm.config.SentinelMaster == "" {
		return fmt.Errorf("sentinel master name not configured")
	}

	sentinelClient := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    rm.config.SentinelMaster,
		SentinelAddrs: rm.config.SentinelAddrs,
		Password:      rm.password(),
		DB:            rm.config.DB,
		PoolSize:      rm.config.PoolSize,
		MaxRetries:    rm.config.MaxRetries,
		DialTimeout:   rm.config.DialTimeout,
		ReadTimeout:   rm.config.ReadTimeout,
		WriteTimeout:  rm.config.WriteTimeout,
		IdleTimeout:   rm.config.IdleTimeout,
	})

	rm.client = sentinelClient
	rm.sentinelClient = sentinelClient
	logger.Infof("Redis sentinel mode initialized: master=%s, sentinels=%s",
		rm.config.SentinelMaster, strings.Join(rm.config.SentinelAddrs, ","))
	return nil
}

func (rm *RedisManager) password() string {
	if rm.config.PasswordEnv != "" {
		return os.Getenv(rm.config.PasswordEnv)
	}
	return rm.config.Password
}

// GetClient 获取Redis客户端
func (rm *RedisManager) GetClient() redis.Cmdable {
	return rm.client
}

// GetClusterClient 获取集群客户端（仅集群模式）
func (rm *RedisManager) GetClusterClient() *redis.ClusterClient {
	return rm.clusterClient
}

// GetMode 获取Redis模式
func (rm *RedisManager) GetMode() string {
	return rm.mode
}

// Close 关闭Redis连接
func (rm *RedisManager) Ping(ctx context.Context) error {
	if rm == nil || rm.client == nil {
		return fmt.Errorf("redis client not initialized")
	}
	return rm.client.Ping(ctx).Err()
}

func (rm *RedisManager) Close() error {
	switch rm.mode {
	case "cluster":
		if rm.clusterClient != nil {
			return rm.clusterClient.Close()
		}
	case "sentinel":
		if rm.sentinelClient != nil {
			return rm.sentinelClient.Close()
		}
	default:
		if client, ok := rm.client.(*redis.Client); ok {
			return client.Close()
		}
	}
	return nil
}

// GetClusterInfo 获取集群信息（仅集群模式）
func (rm *RedisManager) GetClusterInfo() (map[string]interface{}, error) {
	if rm.mode != "cluster" || rm.clusterClient == nil {
		return nil, fmt.Errorf("not in cluster mode")
	}

	info := make(map[string]interface{})

	// 获取集群节点信息
	clusterSlots := rm.clusterClient.ClusterSlots(rm.ctx)
	if clusterSlots.Err() != nil {
		return nil, clusterSlots.Err()
	}

	slots := clusterSlots.Val()
	info["cluster_slots"] = len(slots)
	info["cluster_nodes"] = rm.config.ClusterAddrs

	// 获取集群状态
	clusterInfo := rm.clusterClient.ClusterInfo(rm.ctx)
	if clusterInfo.Err() == nil {
		info["cluster_state"] = clusterInfo.Val()
	}

	return info, nil
}

// Set 设置键值对
func (rm *RedisManager) Set(key string, value interface{}, expiration time.Duration) error {
	var data []byte
	var err error

	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		data, err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %v", err)
		}
	}

	return rm.client.Set(rm.ctx, key, data, expiration).Err()
}

// Get 获取值
func (rm *RedisManager) Get(key string) ([]byte, error) {
	result, err := rm.client.Get(rm.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("key not found")
		}
		return nil, err
	}
	return []byte(result), nil
}

// GetString 获取字符串值
func (rm *RedisManager) GetString(key string) (string, error) {
	return rm.client.Get(rm.ctx, key).Result()
}

// GetObject 获取对象
func (rm *RedisManager) GetObject(key string, dest interface{}) error {
	data, err := rm.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// Delete 删除键
func (rm *RedisManager) Delete(keys ...string) error {
	return rm.client.Del(rm.ctx, keys...).Err()
}

// Exists 检查键是否存在
func (rm *RedisManager) Exists(key string) (bool, error) {
	count, err := rm.client.Exists(rm.ctx, key).Result()
	return count > 0, err
}

// Expire 设置过期时间
func (rm *RedisManager) Expire(key string, expiration time.Duration) error {
	return rm.client.Expire(rm.ctx, key, expiration).Err()
}

// TTL 获取TTL
func (rm *RedisManager) TTL(key string) (time.Duration, error) {
	return rm.client.TTL(rm.ctx, key).Result()
}

// Incr 递增
func (rm *RedisManager) Incr(key string) (int64, error) {
	return rm.client.Incr(rm.ctx, key).Result()
}

// IncrBy 递增指定值
func (rm *RedisManager) IncrBy(key string, value int64) (int64, error) {
	return rm.client.IncrBy(rm.ctx, key, value).Result()
}

// Hash操作
func (rm *RedisManager) HSet(key, field string, value interface{}) error {
	return rm.client.HSet(rm.ctx, key, field, value).Err()
}

func (rm *RedisManager) HGet(key, field string) (string, error) {
	return rm.client.HGet(rm.ctx, key, field).Result()
}

func (rm *RedisManager) HGetAll(key string) (map[string]string, error) {
	return rm.client.HGetAll(rm.ctx, key).Result()
}

func (rm *RedisManager) HDel(key string, fields ...string) error {
	return rm.client.HDel(rm.ctx, key, fields...).Err()
}

func (rm *RedisManager) HExists(key, field string) (bool, error) {
	return rm.client.HExists(rm.ctx, key, field).Result()
}

// List操作
func (rm *RedisManager) LPush(key string, values ...interface{}) error {
	return rm.client.LPush(rm.ctx, key, values...).Err()
}

func (rm *RedisManager) RPush(key string, values ...interface{}) error {
	return rm.client.RPush(rm.ctx, key, values...).Err()
}

func (rm *RedisManager) LPop(key string) (string, error) {
	return rm.client.LPop(rm.ctx, key).Result()
}

func (rm *RedisManager) RPop(key string) (string, error) {
	return rm.client.RPop(rm.ctx, key).Result()
}

func (rm *RedisManager) LLen(key string) (int64, error) {
	return rm.client.LLen(rm.ctx, key).Result()
}

func (rm *RedisManager) LRange(key string, start, stop int64) ([]string, error) {
	return rm.client.LRange(rm.ctx, key, start, stop).Result()
}

// Set操作
func (rm *RedisManager) SAdd(key string, members ...interface{}) error {
	return rm.client.SAdd(rm.ctx, key, members...).Err()
}

func (rm *RedisManager) SRem(key string, members ...interface{}) error {
	return rm.client.SRem(rm.ctx, key, members...).Err()
}

func (rm *RedisManager) SMembers(key string) ([]string, error) {
	return rm.client.SMembers(rm.ctx, key).Result()
}

func (rm *RedisManager) SIsMember(key string, member interface{}) (bool, error) {
	return rm.client.SIsMember(rm.ctx, key, member).Result()
}

func (rm *RedisManager) SCard(key string) (int64, error) {
	return rm.client.SCard(rm.ctx, key).Result()
}

// ZSet操作
func (rm *RedisManager) ZAdd(key string, members ...*redis.Z) error {
	return rm.client.ZAdd(rm.ctx, key, members...).Err()
}

func (rm *RedisManager) ZRem(key string, members ...interface{}) error {
	return rm.client.ZRem(rm.ctx, key, members...).Err()
}

func (rm *RedisManager) ZRange(key string, start, stop int64) ([]string, error) {
	return rm.client.ZRange(rm.ctx, key, start, stop).Result()
}

func (rm *RedisManager) ZRangeWithScores(key string, start, stop int64) ([]redis.Z, error) {
	return rm.client.ZRangeWithScores(rm.ctx, key, start, stop).Result()
}

func (rm *RedisManager) ZRevRange(key string, start, stop int64) ([]string, error) {
	return rm.client.ZRevRange(rm.ctx, key, start, stop).Result()
}

func (rm *RedisManager) ZScore(key, member string) (float64, error) {
	return rm.client.ZScore(rm.ctx, key, member).Result()
}

func (rm *RedisManager) ZCard(key string) (int64, error) {
	return rm.client.ZCard(rm.ctx, key).Result()
}

// Pipeline操作
func (rm *RedisManager) Pipeline() redis.Pipeliner {
	return rm.client.Pipeline()
}

// Transaction操作
func (rm *RedisManager) TxPipeline() redis.Pipeliner {
	return rm.client.TxPipeline()
}

// Lock 分布式锁
func (rm *RedisManager) Lock(key string, expiration time.Duration) (bool, error) {
	lockKey := fmt.Sprintf("lock:%s", key)
	result := rm.client.SetNX(rm.ctx, lockKey, "1", expiration)
	return result.Result()
}

// Unlock 释放分布式锁
func (rm *RedisManager) Unlock(key string) error {
	lockKey := fmt.Sprintf("lock:%s", key)
	return rm.client.Del(rm.ctx, lockKey).Err()
}

// Pub/Sub操作
func (rm *RedisManager) Publish(channel string, message interface{}) error {
	return rm.client.Publish(rm.ctx, channel, message).Err()
}

func (rm *RedisManager) Subscribe(channels ...string) *redis.PubSub {
	switch rm.mode {
	case "cluster":
		if rm.clusterClient != nil {
			return rm.clusterClient.Subscribe(rm.ctx, channels...)
		}
	case "sentinel":
		if rm.sentinelClient != nil {
			return rm.sentinelClient.Subscribe(rm.ctx, channels...)
		}
	default:
		if client, ok := rm.client.(*redis.Client); ok {
			return client.Subscribe(rm.ctx, channels...)
		}
	}
	return nil
}

func (rm *RedisManager) PSubscribe(patterns ...string) *redis.PubSub {
	switch rm.mode {
	case "cluster":
		if rm.clusterClient != nil {
			return rm.clusterClient.PSubscribe(rm.ctx, patterns...)
		}
	case "sentinel":
		if rm.sentinelClient != nil {
			return rm.sentinelClient.PSubscribe(rm.ctx, patterns...)
		}
	default:
		if client, ok := rm.client.(*redis.Client); ok {
			return client.PSubscribe(rm.ctx, patterns...)
		}
	}
	return nil
}

// UserCache 用户缓存
type UserCache struct {
	redis  *RedisManager
	prefix string
	expiry time.Duration
}

// NewUserCache 创建用户缓存
func NewUserCache(redis *RedisManager) *UserCache {
	return &UserCache{
		redis:  redis,
		prefix: "user:",
		expiry: 24 * time.Hour,
	}
}

// SetUserInfo 设置用户信息
func (uc *UserCache) SetUserInfo(userID uint64, info interface{}) error {
	key := fmt.Sprintf("%s%d", uc.prefix, userID)
	return uc.redis.Set(key, info, uc.expiry)
}

// GetUserInfo 获取用户信息
func (uc *UserCache) GetUserInfo(userID uint64, dest interface{}) error {
	key := fmt.Sprintf("%s%d", uc.prefix, userID)
	return uc.redis.GetObject(key, dest)
}

// DeleteUserInfo 删除用户信息
func (uc *UserCache) DeleteUserInfo(userID uint64) error {
	key := fmt.Sprintf("%s%d", uc.prefix, userID)
	return uc.redis.Delete(key)
}

// SetUserOnline 设置用户在线状态
func (uc *UserCache) SetUserOnline(userID uint64, nodeID string) error {
	key := fmt.Sprintf("online:%d", userID)
	return uc.redis.Set(key, nodeID, 30*time.Minute)
}

// GetUserOnline 获取用户在线节点
func (uc *UserCache) GetUserOnline(userID uint64) (string, error) {
	key := fmt.Sprintf("online:%d", userID)
	return uc.redis.GetString(key)
}

// SetUserOffline 设置用户离线
func (uc *UserCache) SetUserOffline(userID uint64) error {
	key := fmt.Sprintf("online:%d", userID)
	return uc.redis.Delete(key)
}

// GameRoomCache 游戏房间缓存
type GameRoomCache struct {
	redis  *RedisManager
	prefix string
	expiry time.Duration
}

// NewGameRoomCache 创建游戏房间缓存
func NewGameRoomCache(redis *RedisManager) *GameRoomCache {
	return &GameRoomCache{
		redis:  redis,
		prefix: "room:",
		expiry: 2 * time.Hour,
	}
}

// SetRoomInfo 设置房间信息
func (grc *GameRoomCache) SetRoomInfo(roomID uint64, info interface{}) error {
	key := fmt.Sprintf("%s%d", grc.prefix, roomID)
	return grc.redis.Set(key, info, grc.expiry)
}

// GetRoomInfo 获取房间信息
func (grc *GameRoomCache) GetRoomInfo(roomID uint64, dest interface{}) error {
	key := fmt.Sprintf("%s%d", grc.prefix, roomID)
	return grc.redis.GetObject(key, dest)
}

// DeleteRoomInfo 删除房间信息
func (grc *GameRoomCache) DeleteRoomInfo(roomID uint64) error {
	key := fmt.Sprintf("%s%d", grc.prefix, roomID)
	return grc.redis.Delete(key)
}

// AddPlayerToRoom 添加玩家到房间
func (grc *GameRoomCache) AddPlayerToRoom(roomID, userID uint64) error {
	key := fmt.Sprintf("%splayers:%d", grc.prefix, roomID)
	return grc.redis.SAdd(key, userID)
}

// RemovePlayerFromRoom 从房间移除玩家
func (grc *GameRoomCache) RemovePlayerFromRoom(roomID, userID uint64) error {
	key := fmt.Sprintf("%splayers:%d", grc.prefix, roomID)
	return grc.redis.SRem(key, userID)
}

// GetRoomPlayers 获取房间玩家
func (grc *GameRoomCache) GetRoomPlayers(roomID uint64) ([]string, error) {
	key := fmt.Sprintf("%splayers:%d", grc.prefix, roomID)
	return grc.redis.SMembers(key)
}

// SessionCache 会话缓存
type SessionCache struct {
	redis  *RedisManager
	prefix string
	expiry time.Duration
}

// NewSessionCache 创建会话缓存
func NewSessionCache(redis *RedisManager) *SessionCache {
	return &SessionCache{
		redis:  redis,
		prefix: "session:",
		expiry: 2 * time.Hour,
	}
}

// SetSession 设置会话
func (sc *SessionCache) SetSession(sessionID string, userID uint64) error {
	key := fmt.Sprintf("%s%s", sc.prefix, sessionID)
	return sc.redis.Set(key, userID, sc.expiry)
}

// GetSession 获取会话
func (sc *SessionCache) GetSession(sessionID string) (uint64, error) {
	key := fmt.Sprintf("%s%s", sc.prefix, sessionID)
	result, err := sc.redis.GetString(key)
	if err != nil {
		return 0, err
	}

	var userID uint64
	if err := json.Unmarshal([]byte(result), &userID); err != nil {
		return 0, err
	}

	return userID, nil
}

// DeleteSession 删除会话
func (sc *SessionCache) DeleteSession(sessionID string) error {
	key := fmt.Sprintf("%s%s", sc.prefix, sessionID)
	return sc.redis.Delete(key)
}

// RefreshSession 刷新会话
func (sc *SessionCache) RefreshSession(sessionID string) error {
	key := fmt.Sprintf("%s%s", sc.prefix, sessionID)
	return sc.redis.Expire(key, sc.expiry)
}
