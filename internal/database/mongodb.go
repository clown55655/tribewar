package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"tribeway/internal/logger"
)

// MongoConfig MongoDB配置
type MongoConfig struct {
	// 单机模式
	URI      string `yaml:"uri"`
	Database string `yaml:"database"`

	// 副本集模式
	ReplicaSet     bool     `yaml:"replica_set"`
	ReplicaSetName string   `yaml:"replica_set_name"`
	Hosts          []string `yaml:"hosts"`
	AuthSource     string   `yaml:"auth_source"`
	Username       string   `yaml:"username"`
	Password       string   `yaml:"password"`
	PasswordEnv    string   `yaml:"password_env"`

	// 分片模式
	ShardedCluster bool     `yaml:"sharded_cluster"`
	MongosHosts    []string `yaml:"mongos_hosts"`

	// 连接配置
	ConnectTimeout  time.Duration `yaml:"connect_timeout"`
	MaxPoolSize     uint64        `yaml:"max_pool_size"`
	MinPoolSize     uint64        `yaml:"min_pool_size"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`

	// 读写配置
	ReadPreference string `yaml:"read_preference"` // primary, primaryPreferred, secondary, etc.
	WriteConcern   string `yaml:"write_concern"`   // majority, 1, 2, etc.
	ReadConcern    string `yaml:"read_concern"`    // local, available, majority, etc.

	// SSL/TLS配置
	TLSEnabled  bool   `yaml:"tls_enabled"`
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
	TLSCAFile   string `yaml:"tls_ca_file"`
}

// MongoManager MongoDB管理器
type MongoManager struct {
	client   *mongo.Client
	database *mongo.Database
	config   *MongoConfig
	ctx      context.Context
	mode     string // "single", "replica_set", "sharded"
}

// NewMongoManager 创建MongoDB管理器
func NewMongoManager(config *MongoConfig) (*MongoManager, error) {
	ctx := context.Background()

	manager := &MongoManager{
		config: config,
		ctx:    ctx,
	}

	var clientOptions *options.ClientOptions
	var err error

	// 根据配置选择MongoDB模式
	if config.ShardedCluster {
		manager.mode = "sharded"
		clientOptions, err = manager.buildShardedClusterOptions()
	} else if config.ReplicaSet {
		manager.mode = "replica_set"
		clientOptions, err = manager.buildReplicaSetOptions()
	} else {
		manager.mode = "single"
		clientOptions, err = manager.buildSingleOptions()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build client options: %v", err)
	}

	// 连接MongoDB
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mongodb: %v", err)
	}

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping mongodb: %v", err)
	}

	manager.client = client
	manager.database = client.Database(config.Database)

	logger.Infof("MongoDB connected in %s mode", manager.mode)
	return manager, nil
}

// buildSingleOptions 构建单机模式选项
func (mm *MongoManager) buildSingleOptions() (*options.ClientOptions, error) {
	opts := options.Client().
		ApplyURI(mm.config.URI).
		SetConnectTimeout(mm.config.ConnectTimeout).
		SetMaxPoolSize(mm.config.MaxPoolSize).
		SetMinPoolSize(mm.config.MinPoolSize).
		SetMaxConnIdleTime(mm.config.MaxConnIdleTime)

	// 添加认证信息
	password := mm.password()
	if mm.config.Username != "" && password != "" {
		credential := options.Credential{
			Username:   mm.config.Username,
			Password:   password,
			AuthSource: mm.config.AuthSource,
		}
		opts.SetAuth(credential)
	}

	return opts, nil
}

// buildReplicaSetOptions 构建副本集模式选项
func (mm *MongoManager) buildReplicaSetOptions() (*options.ClientOptions, error) {
	if len(mm.config.Hosts) == 0 {
		return nil, fmt.Errorf("replica set hosts not configured")
	}

	if mm.config.ReplicaSetName == "" {
		return nil, fmt.Errorf("replica set name not configured")
	}

	// 构建连接URI
	uri := "mongodb://"
	password := mm.password()
	if mm.config.Username != "" && password != "" {
		uri += fmt.Sprintf("%s:%s@", url.QueryEscape(mm.config.Username), url.QueryEscape(password))
	}
	uri += strings.Join(mm.config.Hosts, ",")
	uri += fmt.Sprintf("/%s?replicaSet=%s", mm.config.Database, mm.config.ReplicaSetName)

	if mm.config.AuthSource != "" {
		uri += fmt.Sprintf("&authSource=%s", mm.config.AuthSource)
	}

	opts := options.Client().
		ApplyURI(uri).
		SetConnectTimeout(mm.config.ConnectTimeout).
		SetMaxPoolSize(mm.config.MaxPoolSize).
		SetMinPoolSize(mm.config.MinPoolSize).
		SetMaxConnIdleTime(mm.config.MaxConnIdleTime).
		SetReplicaSet(mm.config.ReplicaSetName)

	// 设置读偏好
	if mm.config.ReadPreference != "" {
		readPref, err := parseReadPreference(mm.config.ReadPreference)
		if err != nil {
			return nil, fmt.Errorf("invalid read preference: %v", err)
		}
		opts.SetReadPreference(readPref)
	}

	// 设置写关注
	if mm.config.WriteConcern != "" {
		writeConcern, err := parseWriteConcern(mm.config.WriteConcern)
		if err != nil {
			return nil, fmt.Errorf("invalid write concern: %v", err)
		}
		opts.SetWriteConcern(writeConcern)
	}

	return opts, nil
}

// buildShardedClusterOptions 构建分片集群模式选项
func (mm *MongoManager) buildShardedClusterOptions() (*options.ClientOptions, error) {
	if len(mm.config.MongosHosts) == 0 {
		return nil, fmt.Errorf("mongos hosts not configured")
	}

	// 构建连接URI
	uri := "mongodb://"
	password := mm.password()
	if mm.config.Username != "" && password != "" {
		uri += fmt.Sprintf("%s:%s@", url.QueryEscape(mm.config.Username), url.QueryEscape(password))
	}
	uri += strings.Join(mm.config.MongosHosts, ",")
	uri += fmt.Sprintf("/%s", mm.config.Database)

	if mm.config.AuthSource != "" {
		uri += fmt.Sprintf("?authSource=%s", mm.config.AuthSource)
	}

	opts := options.Client().
		ApplyURI(uri).
		SetConnectTimeout(mm.config.ConnectTimeout).
		SetMaxPoolSize(mm.config.MaxPoolSize).
		SetMinPoolSize(mm.config.MinPoolSize).
		SetMaxConnIdleTime(mm.config.MaxConnIdleTime)

	return opts, nil
}

func (mm *MongoManager) password() string {
	if mm.config.PasswordEnv != "" {
		return os.Getenv(mm.config.PasswordEnv)
	}
	return mm.config.Password
}

// parseReadPreference 解析读偏好
func parseReadPreference(pref string) (*readpref.ReadPref, error) {
	switch pref {
	case "primary":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown read preference: %s", pref)
	}
}

// parseWriteConcern 解析写关注
func parseWriteConcern(concern string) (*writeconcern.WriteConcern, error) {
	switch concern {
	case "majority":
		return writeconcern.New(writeconcern.WMajority()), nil
	case "1":
		return writeconcern.New(writeconcern.W(1)), nil
	case "2":
		return writeconcern.New(writeconcern.W(2)), nil
	case "3":
		return writeconcern.New(writeconcern.W(3)), nil
	default:
		// 尝试解析为数字
		if w := parseIntOrDefault(concern, -1); w > 0 {
			return writeconcern.New(writeconcern.W(w)), nil
		}
		return nil, fmt.Errorf("unknown write concern: %s", concern)
	}
}

// parseIntOrDefault 解析整数或返回默认值
func parseIntOrDefault(s string, defaultValue int) int {
	if val, err := strconv.Atoi(s); err == nil {
		return val
	}
	return defaultValue
}

// GetDatabase 获取数据库
func (mm *MongoManager) GetDatabase() *mongo.Database {
	return mm.database
}

// GetCollection 获取集合
func (mm *MongoManager) GetCollection(name string) *mongo.Collection {
	return mm.database.Collection(name)
}

// Close 关闭MongoDB连接
func (mm *MongoManager) Ping(ctx context.Context) error {
	if mm == nil || mm.client == nil {
		return fmt.Errorf("mongodb client not initialized")
	}
	return mm.client.Ping(ctx, nil)
}

func (mm *MongoManager) Close() error {
	return mm.client.Disconnect(mm.ctx)
}

// UserRepository 用户数据仓库
type UserRepository struct {
	collection *mongo.Collection
}

// User 用户模型
type User struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      uint64             `bson:"user_id" json:"user_id"`
	Username    string             `bson:"username" json:"username"`
	Password    string             `bson:"password" json:"password"`
	Nickname    string             `bson:"nickname" json:"nickname"`
	Email       string             `bson:"email,omitempty" json:"email"`
	Phone       string             `bson:"phone,omitempty" json:"phone"`
	Level       int32              `bson:"level" json:"level"`
	Experience  int64              `bson:"experience" json:"experience"`
	Gold        int64              `bson:"gold" json:"gold"`
	Diamond     int64              `bson:"diamond" json:"diamond"`
	Avatar      string             `bson:"avatar,omitempty" json:"avatar"`
	Status      int32              `bson:"status" json:"status"` // 0-正常 1-封禁
	LastLoginIP string             `bson:"last_login_ip" json:"last_login_ip"`
	LastLoginAt time.Time          `bson:"last_login_at" json:"last_login_at"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}

// NewUserRepository 创建用户仓库
func NewUserRepository(mm *MongoManager) *UserRepository {
	collection := mm.GetCollection("users")

	// 创建索引
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "email", Value: 1}},
		},
	}

	collection.Indexes().CreateMany(context.Background(), indexes)

	return &UserRepository{
		collection: collection,
	}
}

// Create 创建用户
func (ur *UserRepository) Create(user *User) error {
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	result, err := ur.collection.InsertOne(context.Background(), user)
	if err != nil {
		return fmt.Errorf("failed to create user: %v", err)
	}

	user.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetByUserID 根据用户ID获取用户
func (ur *UserRepository) GetByUserID(userID uint64) (*User, error) {
	var user User
	err := ur.collection.FindOne(context.Background(), bson.M{"user_id": userID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %v", err)
	}
	return &user, nil
}

// GetByUsername 根据用户名获取用户
func (ur *UserRepository) GetByUsername(username string) (*User, error) {
	var user User
	err := ur.collection.FindOne(context.Background(), bson.M{"username": username}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %v", err)
	}
	return &user, nil
}

// Update 更新用户
func (ur *UserRepository) Update(user *User) error {
	user.UpdatedAt = time.Now()

	filter := bson.M{"user_id": user.UserID}
	update := bson.M{"$set": user}

	_, err := ur.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to update user: %v", err)
	}
	return nil
}

// UpdateFields 更新指定字段
func (ur *UserRepository) UpdateFields(userID uint64, fields bson.M) error {
	fields["updated_at"] = time.Now()

	filter := bson.M{"user_id": userID}
	update := bson.M{"$set": fields}

	_, err := ur.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to update user fields: %v", err)
	}
	return nil
}

// Delete 删除用户
func (ur *UserRepository) Delete(userID uint64) error {
	filter := bson.M{"user_id": userID}
	_, err := ur.collection.DeleteOne(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("failed to delete user: %v", err)
	}
	return nil
}

// List 获取用户列表
func (ur *UserRepository) List(offset, limit int64) ([]*User, error) {
	options := options.Find().
		SetSkip(offset).
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := ur.collection.Find(context.Background(), bson.M{}, options)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}
	defer cursor.Close(context.Background())

	var users []*User
	if err := cursor.All(context.Background(), &users); err != nil {
		return nil, fmt.Errorf("failed to decode users: %v", err)
	}

	return users, nil
}

// FriendRepository 好友关系仓库
type FriendRepository struct {
	collection *mongo.Collection
}

// Friend 好友关系模型
type Friend struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    uint64             `bson:"user_id" json:"user_id"`
	FriendID  uint64             `bson:"friend_id" json:"friend_id"`
	Status    int32              `bson:"status" json:"status"` // 0-待确认 1-已确认 2-已拒绝
	Message   string             `bson:"message,omitempty" json:"message"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// NewFriendRepository 创建好友仓库
func NewFriendRepository(mm *MongoManager) *FriendRepository {
	collection := mm.GetCollection("friends")

	// 创建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "friend_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "friend_id", Value: 1}},
		},
	}

	collection.Indexes().CreateMany(context.Background(), indexes)

	return &FriendRepository{
		collection: collection,
	}
}

// AddFriend 添加好友请求
func (fr *FriendRepository) AddFriend(userID, friendID uint64, message string) error {
	friend := &Friend{
		UserID:    userID,
		FriendID:  friendID,
		Status:    0, // 待确认
		Message:   message,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := fr.collection.InsertOne(context.Background(), friend)
	if err != nil {
		return fmt.Errorf("failed to add friend: %v", err)
	}
	return nil
}

// AcceptFriend 接受好友请求
func (fr *FriendRepository) AcceptFriend(userID, friendID uint64) error {
	// 更新请求状态
	filter := bson.M{"user_id": friendID, "friend_id": userID, "status": 0}
	update := bson.M{"$set": bson.M{"status": 1, "updated_at": time.Now()}}

	_, err := fr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to accept friend request: %v", err)
	}

	// 添加反向关系
	friend := &Friend{
		UserID:    userID,
		FriendID:  friendID,
		Status:    1, // 已确认
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = fr.collection.InsertOne(context.Background(), friend)
	if err != nil {
		return fmt.Errorf("failed to add reverse friend relation: %v", err)
	}

	return nil
}

// GetFriends 获取好友列表
func (fr *FriendRepository) GetFriends(userID uint64) ([]*Friend, error) {
	filter := bson.M{"user_id": userID, "status": 1}
	cursor, err := fr.collection.Find(context.Background(), filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %v", err)
	}
	defer cursor.Close(context.Background())

	var friends []*Friend
	if err := cursor.All(context.Background(), &friends); err != nil {
		return nil, fmt.Errorf("failed to decode friends: %v", err)
	}

	return friends, nil
}

// MailRepository 邮件仓库
type MailRepository struct {
	collection *mongo.Collection
}

// Mail 邮件模型
type Mail struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MailID     uint64             `bson:"mail_id" json:"mail_id"`
	ToUserID   uint64             `bson:"to_user_id" json:"to_user_id"`
	FromUserID uint64             `bson:"from_user_id,omitempty" json:"from_user_id"`
	Title      string             `bson:"title" json:"title"`
	Content    string             `bson:"content" json:"content"`
	Rewards    []MailReward       `bson:"rewards,omitempty" json:"rewards"`
	IsRead     bool               `bson:"is_read" json:"is_read"`
	IsClaimed  bool               `bson:"is_claimed" json:"is_claimed"`
	ExpireAt   time.Time          `bson:"expire_at" json:"expire_at"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time          `bson:"updated_at" json:"updated_at"`
}

// MailReward 邮件奖励
type MailReward struct {
	Type   int32  `bson:"type" json:"type"`
	ItemID int32  `bson:"item_id" json:"item_id"`
	Count  int64  `bson:"count" json:"count"`
	Name   string `bson:"name,omitempty" json:"name"`
}

// NewMailRepository 创建邮件仓库
func NewMailRepository(mm *MongoManager) *MailRepository {
	collection := mm.GetCollection("mails")

	// 创建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "mail_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "to_user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "expire_at", Value: 1}},
		},
	}

	collection.Indexes().CreateMany(context.Background(), indexes)

	return &MailRepository{
		collection: collection,
	}
}

// SendMail 发送邮件
func (mr *MailRepository) SendMail(mail *Mail) error {
	mail.CreatedAt = time.Now()
	mail.UpdatedAt = time.Now()

	result, err := mr.collection.InsertOne(context.Background(), mail)
	if err != nil {
		return fmt.Errorf("failed to send mail: %v", err)
	}

	mail.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetUserMails 获取用户邮件列表
func (mr *MailRepository) GetUserMails(userID uint64, limit int64) ([]*Mail, error) {
	filter := bson.M{
		"to_user_id": userID,
		"expire_at":  bson.M{"$gt": time.Now()},
	}

	options := options.Find().
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := mr.collection.Find(context.Background(), filter, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get user mails: %v", err)
	}
	defer cursor.Close(context.Background())

	var mails []*Mail
	if err := cursor.All(context.Background(), &mails); err != nil {
		return nil, fmt.Errorf("failed to decode mails: %v", err)
	}

	return mails, nil
}

// MarkAsRead 标记邮件为已读
func (mr *MailRepository) MarkAsRead(mailID uint64) error {
	filter := bson.M{"mail_id": mailID}
	update := bson.M{"$set": bson.M{"is_read": true, "updated_at": time.Now()}}

	_, err := mr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to mark mail as read: %v", err)
	}
	return nil
}

// ClaimRewards 领取邮件奖励
func (mr *MailRepository) ClaimRewards(mailID uint64) error {
	filter := bson.M{"mail_id": mailID}
	update := bson.M{"$set": bson.M{"is_claimed": true, "updated_at": time.Now()}}

	_, err := mr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to claim rewards: %v", err)
	}
	return nil
}

// GameRecordRepository 游戏记录仓库
type GameRecordRepository struct {
	collection *mongo.Collection
}

// GameRecord 游戏记录模型
type GameRecord struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	GameID    uint64             `bson:"game_id" json:"game_id"`
	RoomID    uint64             `bson:"room_id" json:"room_id"`
	GameType  int32              `bson:"game_type" json:"game_type"`
	Players   []GamePlayer       `bson:"players" json:"players"`
	Winner    uint64             `bson:"winner,omitempty" json:"winner"`
	Duration  int32              `bson:"duration" json:"duration"` // 游戏时长（秒）
	Status    int32              `bson:"status" json:"status"`     // 0-进行中 1-已结束 2-异常结束
	GameData  bson.M             `bson:"game_data,omitempty" json:"game_data"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// GamePlayer 游戏玩家信息
type GamePlayer struct {
	UserID   uint64 `bson:"user_id" json:"user_id"`
	Nickname string `bson:"nickname" json:"nickname"`
	Level    int32  `bson:"level" json:"level"`
	Score    int64  `bson:"score" json:"score"`
	Rank     int32  `bson:"rank" json:"rank"`
}

// NewGameRecordRepository 创建游戏记录仓库
func NewGameRecordRepository(mm *MongoManager) *GameRecordRepository {
	collection := mm.GetCollection("game_records")

	// 创建索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "game_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "room_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "players.user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	}

	collection.Indexes().CreateMany(context.Background(), indexes)

	return &GameRecordRepository{
		collection: collection,
	}
}

// CreateRecord 创建游戏记录
func (grr *GameRecordRepository) CreateRecord(record *GameRecord) error {
	record.CreatedAt = time.Now()
	record.UpdatedAt = time.Now()

	result, err := grr.collection.InsertOne(context.Background(), record)
	if err != nil {
		return fmt.Errorf("failed to create game record: %v", err)
	}

	record.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// UpdateRecord 更新游戏记录
func (grr *GameRecordRepository) UpdateRecord(record *GameRecord) error {
	record.UpdatedAt = time.Now()

	filter := bson.M{"game_id": record.GameID}
	update := bson.M{"$set": record}

	_, err := grr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to update game record: %v", err)
	}
	return nil
}

// GetUserGameRecords 获取用户游戏记录
func (grr *GameRecordRepository) GetUserGameRecords(userID uint64, limit int64) ([]*GameRecord, error) {
	filter := bson.M{"players.user_id": userID}
	options := options.Find().
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := grr.collection.Find(context.Background(), filter, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get user game records: %v", err)
	}
	defer cursor.Close(context.Background())

	var records []*GameRecord
	if err := cursor.All(context.Background(), &records); err != nil {
		return nil, fmt.Errorf("failed to decode game records: %v", err)
	}

	return records, nil
}

// DeleteFriend 删除好友关系
func (fr *FriendRepository) DeleteFriend(userID, friendID uint64) error {
	// 删除用户A到用户B的关系
	filter1 := bson.M{"user_id": userID, "friend_id": friendID}
	_, err := fr.collection.DeleteOne(context.Background(), filter1)
	if err != nil {
		return fmt.Errorf("failed to delete friend relation (user->friend): %v", err)
	}

	// 删除用户B到用户A的关系
	filter2 := bson.M{"user_id": friendID, "friend_id": userID}
	_, err = fr.collection.DeleteOne(context.Background(), filter2)
	if err != nil {
		return fmt.Errorf("failed to delete friend relation (friend->user): %v", err)
	}

	return nil
}

// GetPendingFriendRequests 获取待处理的好友请求
func (fr *FriendRepository) GetPendingFriendRequests(userID uint64) ([]*Friend, error) {
	filter := bson.M{"friend_id": userID, "status": 0} // 待确认状态
	cursor, err := fr.collection.Find(context.Background(), filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending friend requests: %v", err)
	}
	defer cursor.Close(context.Background())

	var requests []*Friend
	if err := cursor.All(context.Background(), &requests); err != nil {
		return nil, fmt.Errorf("failed to decode friend requests: %v", err)
	}

	return requests, nil
}

// RejectFriend 拒绝好友请求
func (fr *FriendRepository) RejectFriend(userID, friendID uint64) error {
	filter := bson.M{"user_id": friendID, "friend_id": userID, "status": 0}
	update := bson.M{"$set": bson.M{"status": 2, "updated_at": time.Now()}} // 状态2表示已拒绝

	_, err := fr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to reject friend request: %v", err)
	}

	return nil
}

// RoomRepository 房间数据仓库
type RoomRepository struct {
	collection *mongo.Collection
}

// Room 房间模型
type Room struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	RoomID         uint64             `bson:"room_id" json:"room_id"`
	RoomName       string             `bson:"room_name" json:"room_name"`
	GameType       int32              `bson:"game_type" json:"game_type"`
	MaxPlayers     int32              `bson:"max_players" json:"max_players"`
	CurrentPlayers int32              `bson:"current_players" json:"current_players"`
	Status         int32              `bson:"status" json:"status"` // 0-等待中 1-游戏中 2-已结束
	IsPrivate      bool               `bson:"is_private" json:"is_private"`
	Password       string             `bson:"password,omitempty" json:"password,omitempty"`
	OwnerID        uint64             `bson:"owner_id" json:"owner_id"`
	Players        []RoomPlayer       `bson:"players" json:"players"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}

// RoomPlayer 房间玩家信息
type RoomPlayer struct {
	UserID   uint64 `bson:"user_id" json:"user_id"`
	Nickname string `bson:"nickname" json:"nickname"`
	Level    int32  `bson:"level" json:"level"`
	Status   int32  `bson:"status" json:"status"` // 0-等待 1-准备 2-游戏中
	JoinTime int64  `bson:"join_time" json:"join_time"`
}

// ChatMessage 聊天消息数据模型
type ChatMessage struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MessageID   uint64             `bson:"message_id" json:"message_id"`
	FromUserID  uint64             `bson:"from_user_id" json:"from_user_id"`
	ToUserID    uint64             `bson:"to_user_id" json:"to_user_id"`
	ChannelType int32              `bson:"channel_type" json:"channel_type"`
	ChannelID   uint64             `bson:"channel_id" json:"channel_id"`
	MessageType int32              `bson:"message_type" json:"message_type"`
	Content     string             `bson:"content" json:"content"`
	SendTime    uint32             `bson:"send_time" json:"send_time"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
}

// BlockedUser 屏蔽用户数据模型
type BlockedUser struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    uint64             `bson:"user_id" json:"user_id"`
	TargetID  uint64             `bson:"target_id" json:"target_id"`
	BlockedAt time.Time          `bson:"blocked_at" json:"blocked_at"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

// ChatRepository 聊天数据访问层
type ChatRepository struct {
	messageCollection *mongo.Collection
	blockedCollection *mongo.Collection
}

// NewChatRepository 创建聊天Repository
func NewChatRepository(mm *MongoManager) *ChatRepository {
	return &ChatRepository{
		messageCollection: mm.GetCollection("chat_messages"),
		blockedCollection: mm.GetCollection("blocked_users"),
	}
}

// BanRecord 封禁记录数据模型
type BanRecord struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    uint64             `bson:"user_id" json:"user_id"`
	GMUserID  uint64             `bson:"gm_user_id" json:"gm_user_id"`
	Reason    string             `bson:"reason" json:"reason"`
	BanTime   time.Time          `bson:"ban_time" json:"ban_time"`
	UnbanTime time.Time          `bson:"unban_time" json:"unban_time"`
	IsActive  bool               `bson:"is_active" json:"is_active"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// GMLog GM操作日志数据模型
type GMLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	GMUserID  uint64             `bson:"gm_user_id" json:"gm_user_id"`
	Action    string             `bson:"action" json:"action"`
	TargetID  uint64             `bson:"target_id" json:"target_id"`
	Details   string             `bson:"details" json:"details"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

// GMRepository GM数据访问层
type GMRepository struct {
	banCollection *mongo.Collection
	logCollection *mongo.Collection
}

// NewGMRepository 创建GM Repository
func NewGMRepository(mm *MongoManager) *GMRepository {
	return &GMRepository{
		banCollection: mm.GetCollection("ban_records"),
		logCollection: mm.GetCollection("gm_logs"),
	}
}

// BanUser 封禁用户
func (r *GMRepository) BanUser(userID, gmUserID uint64, reason string, duration uint32) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 检查用户是否已被封禁
	filter := bson.M{
		"user_id":   userID,
		"is_active": true,
	}

	var existing BanRecord
	err := r.banCollection.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return fmt.Errorf("用户已被封禁")
	}
	if err != mongo.ErrNoDocuments {
		return err
	}

	// 创建封禁记录
	banTime := time.Now()
	unbanTime := banTime.Add(time.Duration(duration) * time.Second)
	if duration == 0 {
		// 永久封禁
		unbanTime = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
	}

	banRecord := &BanRecord{
		UserID:    userID,
		GMUserID:  gmUserID,
		Reason:    reason,
		BanTime:   banTime,
		UnbanTime: unbanTime,
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = r.banCollection.InsertOne(ctx, banRecord)
	return err
}

// UnbanUser 解封用户
func (r *GMRepository) UnbanUser(userID, gmUserID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":   userID,
		"is_active": true,
	}

	update := bson.M{
		"$set": bson.M{
			"is_active":  false,
			"updated_at": time.Now(),
		},
	}

	result, err := r.banCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.ModifiedCount == 0 {
		return fmt.Errorf("用户未被封禁或封禁记录不存在")
	}

	return nil
}

// IsUserBanned 检查用户是否被封禁
func (r *GMRepository) IsUserBanned(userID uint64) (bool, *BanRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":    userID,
		"is_active":  true,
		"unban_time": bson.M{"$gt": time.Now()},
	}

	var banRecord BanRecord
	err := r.banCollection.FindOne(ctx, filter).Decode(&banRecord)
	if err == mongo.ErrNoDocuments {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}

	return true, &banRecord, nil
}

// LogGMAction 记录GM操作日志
func (r *GMRepository) LogGMAction(gmUserID uint64, action string, targetID uint64, details string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gmLog := &GMLog{
		GMUserID:  gmUserID,
		Action:    action,
		TargetID:  targetID,
		Details:   details,
		CreatedAt: time.Now(),
	}

	_, err := r.logCollection.InsertOne(ctx, gmLog)
	return err
}

// CleanExpiredBans 清理过期的封禁记录
func (r *GMRepository) CleanExpiredBans() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"is_active":  true,
		"unban_time": bson.M{"$lt": time.Now()},
	}

	update := bson.M{
		"$set": bson.M{
			"is_active":  false,
			"updated_at": time.Now(),
		},
	}

	_, err := r.banCollection.UpdateMany(ctx, filter, update)
	return err
}

// CreateMail 创建邮件
func (r *MailRepository) CreateMail(mail *Mail) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mail.CreatedAt = time.Now()
	mail.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, mail)
	return err
}

// GetMailsByUserID 根据用户ID获取邮件列表
func (r *MailRepository) GetMailsByUserID(userID uint64, mailType int32, limit, offset int32) ([]*Mail, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"to_user_id": userID,
	}

	// 如果指定了邮件类型
	if mailType > 0 {
		filter["mail_type"] = mailType
	}

	// 过滤未过期的邮件
	currentTime := uint32(time.Now().Unix())
	filter["$or"] = []bson.M{
		{"expire_time": bson.M{"$eq": 0}},           // 永不过期
		{"expire_time": bson.M{"$gt": currentTime}}, // 未过期
	}

	// 获取总数
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 获取邮件列表
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "send_time", Value: -1}}) // 按发送时间倒序
	opts.SetLimit(int64(limit))
	opts.SetSkip(int64(offset))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var mails []*Mail
	for cursor.Next(ctx) {
		var mail Mail
		if err := cursor.Decode(&mail); err != nil {
			continue
		}
		mails = append(mails, &mail)
	}

	return mails, total, nil
}

// GetMailByID 根据邮件ID获取邮件
func (r *MailRepository) GetMailByID(mailID uint64) (*Mail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"mail_id": mailID}

	var mail Mail
	err := r.collection.FindOne(ctx, filter).Decode(&mail)
	if err != nil {
		return nil, err
	}

	return &mail, nil
}

// UpdateMailReadStatus 更新邮件已读状态
func (r *MailRepository) UpdateMailReadStatus(mailID uint64, isRead bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"mail_id": mailID}
	update := bson.M{
		"$set": bson.M{
			"is_read":    isRead,
			"updated_at": time.Now(),
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

// UpdateMailClaimStatus 更新邮件奖励领取状态
func (r *MailRepository) UpdateMailClaimStatus(mailID uint64, isClaimed bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"mail_id": mailID}
	update := bson.M{
		"$set": bson.M{
			"is_claimed": isClaimed,
			"updated_at": time.Now(),
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

// DeleteMail 删除邮件
func (r *MailRepository) DeleteMail(mailID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"mail_id": mailID}
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("邮件不存在")
	}

	return nil
}

// DeleteExpiredMails 删除过期邮件
func (r *MailRepository) DeleteExpiredMails() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	currentTime := uint32(time.Now().Unix())
	filter := bson.M{
		"expire_time": bson.M{
			"$gt": 0,
			"$lt": currentTime,
		},
	}

	_, err := r.collection.DeleteMany(ctx, filter)
	return err
}

// SaveMessage 保存聊天消息
func (r *ChatRepository) SaveMessage(message *ChatMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	message.CreatedAt = time.Now()
	_, err := r.messageCollection.InsertOne(ctx, message)
	return err
}

// GetChatHistory 获取聊天历史
func (r *ChatRepository) GetChatHistory(channelType int32, channelID uint64, limit, offset int32) ([]*ChatMessage, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"channel_type": channelType,
		"channel_id":   channelID,
	}

	// 获取总数
	total, err := r.messageCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 获取消息列表
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "send_time", Value: -1}}) // 按发送时间倒序
	opts.SetLimit(int64(limit))
	opts.SetSkip(int64(offset))

	cursor, err := r.messageCollection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var messages []*ChatMessage
	for cursor.Next(ctx) {
		var message ChatMessage
		if err := cursor.Decode(&message); err != nil {
			continue
		}
		messages = append(messages, &message)
	}

	return messages, total, nil
}

// GetPrivateMessages 获取私聊消息
func (r *ChatRepository) GetPrivateMessages(userID1, userID2 uint64, limit, offset int32) ([]*ChatMessage, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"$or": []bson.M{
			{
				"from_user_id": userID1,
				"to_user_id":   userID2,
			},
			{
				"from_user_id": userID2,
				"to_user_id":   userID1,
			},
		},
		"channel_type": 1, // 私聊类型
	}

	// 获取总数
	total, err := r.messageCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 获取消息列表
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "send_time", Value: -1}})
	opts.SetLimit(int64(limit))
	opts.SetSkip(int64(offset))

	cursor, err := r.messageCollection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var messages []*ChatMessage
	for cursor.Next(ctx) {
		var message ChatMessage
		if err := cursor.Decode(&message); err != nil {
			continue
		}
		messages = append(messages, &message)
	}

	return messages, total, nil
}

// BlockUser 屏蔽用户
func (r *ChatRepository) BlockUser(userID, targetID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 检查是否已经屏蔽
	filter := bson.M{
		"user_id":   userID,
		"target_id": targetID,
	}

	var existing BlockedUser
	err := r.blockedCollection.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return fmt.Errorf("用户已被屏蔽")
	}
	if err != mongo.ErrNoDocuments {
		return err
	}

	// 添加屏蔽记录
	blockedUser := &BlockedUser{
		UserID:    userID,
		TargetID:  targetID,
		BlockedAt: time.Now(),
		CreatedAt: time.Now(),
	}

	_, err = r.blockedCollection.InsertOne(ctx, blockedUser)
	return err
}

// UnblockUser 取消屏蔽用户
func (r *ChatRepository) UnblockUser(userID, targetID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":   userID,
		"target_id": targetID,
	}

	result, err := r.blockedCollection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("未找到屏蔽记录")
	}

	return nil
}

// IsUserBlocked 检查用户是否被屏蔽
func (r *ChatRepository) IsUserBlocked(userID, targetID uint64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":   userID,
		"target_id": targetID,
	}

	var blocked BlockedUser
	err := r.blockedCollection.FindOne(ctx, filter).Decode(&blocked)
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// NewRoomRepository 创建房间仓库
func NewRoomRepository(mm *MongoManager) *RoomRepository {
	collection := mm.GetCollection("rooms")

	// 创建索引
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "room_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "game_type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "owner_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	}

	collection.Indexes().CreateMany(context.Background(), indexes)

	return &RoomRepository{
		collection: collection,
	}
}

// CreateRoom 创建房间
func (rr *RoomRepository) CreateRoom(room *Room) error {
	room.CreatedAt = time.Now()
	room.UpdatedAt = time.Now()

	result, err := rr.collection.InsertOne(context.Background(), room)
	if err != nil {
		return fmt.Errorf("failed to create room: %v", err)
	}

	room.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// GetRoomByID 根据房间ID获取房间
func (rr *RoomRepository) GetRoomByID(roomID uint64) (*Room, error) {
	var room Room
	err := rr.collection.FindOne(context.Background(), bson.M{"room_id": roomID}).Decode(&room)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("room not found")
		}
		return nil, fmt.Errorf("failed to get room: %v", err)
	}
	return &room, nil
}

// GetRoomList 获取房间列表
func (rr *RoomRepository) GetRoomList(gameType int32, limit int64, offset int64) ([]*Room, error) {
	filter := bson.M{}
	if gameType > 0 {
		filter["game_type"] = gameType
	}
	// 只显示等待中的房间
	filter["status"] = 0

	options := options.Find().
		SetLimit(limit).
		SetSkip(offset).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := rr.collection.Find(context.Background(), filter, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get room list: %v", err)
	}
	defer cursor.Close(context.Background())

	var rooms []*Room
	if err := cursor.All(context.Background(), &rooms); err != nil {
		return nil, fmt.Errorf("failed to decode rooms: %v", err)
	}

	return rooms, nil
}

// UpdateRoom 更新房间信息
func (rr *RoomRepository) UpdateRoom(room *Room) error {
	room.UpdatedAt = time.Now()

	filter := bson.M{"room_id": room.RoomID}
	update := bson.M{"$set": room}

	_, err := rr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to update room: %v", err)
	}
	return nil
}

// AddPlayerToRoom 添加玩家到房间
func (rr *RoomRepository) AddPlayerToRoom(roomID uint64, player RoomPlayer) error {
	filter := bson.M{"room_id": roomID}
	update := bson.M{
		"$push": bson.M{"players": player},
		"$inc":  bson.M{"current_players": 1},
		"$set":  bson.M{"updated_at": time.Now()},
	}

	_, err := rr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to add player to room: %v", err)
	}
	return nil
}

// RemovePlayerFromRoom 从房间移除玩家
func (rr *RoomRepository) RemovePlayerFromRoom(roomID uint64, userID uint64) error {
	filter := bson.M{"room_id": roomID}
	update := bson.M{
		"$pull": bson.M{"players": bson.M{"user_id": userID}},
		"$inc":  bson.M{"current_players": -1},
		"$set":  bson.M{"updated_at": time.Now()},
	}

	_, err := rr.collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return fmt.Errorf("failed to remove player from room: %v", err)
	}
	return nil
}

// DeleteRoom 删除房间
func (rr *RoomRepository) DeleteRoom(roomID uint64) error {
	filter := bson.M{"room_id": roomID}
	_, err := rr.collection.DeleteOne(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("failed to delete room: %v", err)
	}
	return nil
}

// CountRooms 统计房间数量
func (rr *RoomRepository) CountRooms(gameType int32) (int64, error) {
	filter := bson.M{}
	if gameType > 0 {
		filter["game_type"] = gameType
	}
	filter["status"] = 0 // 只统计等待中的房间

	count, err := rr.collection.CountDocuments(context.Background(), filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count rooms: %v", err)
	}
	return count, nil
}
