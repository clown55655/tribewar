package server

import (
	"fmt"
	"time"

	"tribeway/internal/logger"
)

type componentInitializer struct {
	name    string
	enabled func(ComponentOptions) bool
	init    func(*BaseServer, ComponentFactory) error
}

var componentInitializers = []componentInitializer{
	{
		name:    "actor_system",
		enabled: func(opts ComponentOptions) bool { return opts.ActorSystem },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			bs.actorSystem = factory.NewActorSystem(bs.nodeType, bs.nodeID)
			return nil
		},
	},
	{
		name:    "redis",
		enabled: func(opts ComponentOptions) bool { return opts.Redis },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			redisManager, err := factory.NewRedisManager(&bs.config.Database.Redis)
			if err != nil {
				return fmt.Errorf("failed to init redis: %v", err)
			}
			bs.redisManager = redisManager
			return nil
		},
	},
	{
		name:    "mongodb",
		enabled: func(opts ComponentOptions) bool { return opts.MongoDB },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			mongoManager, err := factory.NewMongoManager(&bs.config.Database.MongoDB)
			if err != nil {
				return fmt.Errorf("failed to init mongodb: %v", err)
			}
			bs.mongoManager = mongoManager
			return nil
		},
	},
	{
		name:    "nsq",
		enabled: func(opts ComponentOptions) bool { return opts.NSQ },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			nsqManager, err := factory.NewNSQManager(&bs.config.NSQ)
			if err != nil {
				return fmt.Errorf("failed to init nsq: %v", err)
			}
			bs.nsqManager = nsqManager
			return nil
		},
	},
	{
		name:    "message_broker",
		enabled: func(opts ComponentOptions) bool { return opts.MessageBroker },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			messageBroker, err := factory.NewMessageBroker(bs.nsqManager, bs.nodeID)
			if err != nil {
				return err
			}
			bs.messageBroker = messageBroker
			return nil
		},
	},
	{
		name:    "registry",
		enabled: func(opts ComponentOptions) bool { return opts.Registry },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			registry, err := factory.NewRegistry(&bs.config.ETCD)
			if err != nil {
				return fmt.Errorf("failed to init etcd registry: %v", err)
			}
			bs.registry = registry
			return nil
		},
	},
	{
		name:    "service_discovery",
		enabled: func(opts ComponentOptions) bool { return opts.ServiceDiscovery },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			serviceDiscovery, err := factory.NewServiceDiscovery(bs.registry, bs.nodeType)
			if err != nil {
				return err
			}
			bs.discovery = serviceDiscovery
			return nil
		},
	},
	{
		name:    "rpc_server",
		enabled: func(opts ComponentOptions) bool { return opts.RPCServer },
		init: func(bs *BaseServer, factory ComponentFactory) error {
			bs.rpcServer = factory.NewRPCServer(bs.config)
			return nil
		},
	},
}

func (bs *BaseServer) initComponents(opts ComponentOptions, factory ComponentFactory) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	logger.Infof("Initializing components for %s/%s: %v", bs.nodeType, bs.nodeID, opts.EnabledNames())
	for _, initializer := range componentInitializers {
		if !initializer.enabled(opts) {
			continue
		}
		start := time.Now()
		if err := initializer.init(bs, factory); err != nil {
			return fmt.Errorf("component %s: %v", initializer.name, err)
		}
		logger.Debugf("Component %s initialized in %v", initializer.name, time.Since(start))
	}

	return nil
}

func (bs *BaseServer) maxPacketSize() uint32 {
	return maxPacketSize(bs.config)
}
