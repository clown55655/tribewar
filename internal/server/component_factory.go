package server

import (
	"fmt"
	"time"

	"tribeway/internal/actor"
	"tribeway/internal/database"
	"tribeway/internal/discovery"
	"tribeway/internal/mq"
	"tribeway/internal/network"
	"tribeway/internal/rpc"
)

// ComponentFactory 负责创建 BaseServer 需要的基础组件。
type ComponentFactory interface {
	NewActorSystem(nodeType, nodeID string) *actor.ActorSystem
	NewRedisManager(config *database.RedisConfig) (*database.RedisManager, error)
	NewMongoManager(config *database.MongoConfig) (*database.MongoManager, error)
	NewNSQManager(config *mq.NSQConfig) (*mq.NSQManager, error)
	NewMessageBroker(nsqManager *mq.NSQManager, nodeID string) (*mq.MessageBroker, error)
	NewRegistry(config *discovery.ETCDConfig) (*discovery.ETCDRegistry, error)
	NewServiceDiscovery(registry *discovery.ETCDRegistry, nodeType string) (*discovery.ServiceDiscovery, error)
	NewRPCServer(config *ServerConfig) *rpc.RPCServer
}

var _ ComponentFactory = (*DefaultComponentFactory)(nil)

// DefaultComponentFactory 是生产环境默认组件工厂。
type DefaultComponentFactory struct{}

func NewDefaultComponentFactory() *DefaultComponentFactory {
	return &DefaultComponentFactory{}
}

func (f *DefaultComponentFactory) NewActorSystem(nodeType, nodeID string) *actor.ActorSystem {
	return actor.NewActorSystem(fmt.Sprintf("%s-%s", nodeType, nodeID))
}

func (f *DefaultComponentFactory) NewRedisManager(config *database.RedisConfig) (*database.RedisManager, error) {
	return database.NewRedisManager(config)
}

func (f *DefaultComponentFactory) NewMongoManager(config *database.MongoConfig) (*database.MongoManager, error) {
	return database.NewMongoManager(config)
}

func (f *DefaultComponentFactory) NewNSQManager(config *mq.NSQConfig) (*mq.NSQManager, error) {
	return mq.NewNSQManager(config)
}

func (f *DefaultComponentFactory) NewMessageBroker(nsqManager *mq.NSQManager, nodeID string) (*mq.MessageBroker, error) {
	if nsqManager == nil {
		return nil, fmt.Errorf("message broker requires nsq manager")
	}
	return mq.NewMessageBroker(nsqManager, nodeID), nil
}

func (f *DefaultComponentFactory) NewRegistry(config *discovery.ETCDConfig) (*discovery.ETCDRegistry, error) {
	return discovery.NewETCDRegistry(config)
}

func (f *DefaultComponentFactory) NewServiceDiscovery(registry *discovery.ETCDRegistry, nodeType string) (*discovery.ServiceDiscovery, error) {
	if registry == nil {
		return nil, fmt.Errorf("service discovery requires registry")
	}
	return discovery.NewServiceDiscovery(
		registry,
		nodeType,
		discovery.NewWeightedLoadBalancer(),
	), nil
}

func (f *DefaultComponentFactory) NewRPCServer(config *ServerConfig) *rpc.RPCServer {
	rpcServer := rpc.NewRPCServer("0.0.0.0", config.Network.RPCPort)
	rpcServer.SetFrameOptions(
		time.Duration(config.Network.ReadTimeout)*time.Second,
		time.Duration(config.Network.WriteTimeout)*time.Second,
		maxPacketSize(config),
	)
	return rpcServer
}

func maxPacketSize(config *ServerConfig) uint32 {
	if config.Network.MaxPacketSize <= 0 {
		return network.DefaultMaxFrame
	}
	return uint32(config.Network.MaxPacketSize)
}
