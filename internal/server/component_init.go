package server

import "fmt"

func (bs *BaseServer) initComponents(opts ComponentOptions, factory ComponentFactory) error {
	if opts.ActorSystem {
		bs.actorSystem = factory.NewActorSystem(bs.nodeType, bs.nodeID)
	}

	if opts.Redis {
		redisManager, err := factory.NewRedisManager(&bs.config.Database.Redis)
		if err != nil {
			return fmt.Errorf("failed to init redis: %v", err)
		}
		bs.redisManager = redisManager
	}

	if opts.MongoDB {
		mongoManager, err := factory.NewMongoManager(&bs.config.Database.MongoDB)
		if err != nil {
			return fmt.Errorf("failed to init mongodb: %v", err)
		}
		bs.mongoManager = mongoManager
	}

	if opts.NSQ {
		nsqManager, err := factory.NewNSQManager(&bs.config.NSQ)
		if err != nil {
			return fmt.Errorf("failed to init nsq: %v", err)
		}
		bs.nsqManager = nsqManager
	}

	if opts.MessageBroker {
		messageBroker, err := factory.NewMessageBroker(bs.nsqManager, bs.nodeID)
		if err != nil {
			return err
		}
		bs.messageBroker = messageBroker
	}

	if opts.Registry {
		registry, err := factory.NewRegistry(&bs.config.ETCD)
		if err != nil {
			return fmt.Errorf("failed to init etcd registry: %v", err)
		}
		bs.registry = registry
	}

	if opts.ServiceDiscovery {
		serviceDiscovery, err := factory.NewServiceDiscovery(bs.registry, bs.nodeType)
		if err != nil {
			return err
		}
		bs.discovery = serviceDiscovery
	}

	if opts.RPCServer {
		bs.rpcServer = factory.NewRPCServer(bs.config)
	}

	return nil
}

func (bs *BaseServer) maxPacketSize() uint32 {
	return maxPacketSize(bs.config)
}
