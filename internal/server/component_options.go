package server

// ComponentOptions 声明当前节点需要初始化的基础组件。
type ComponentOptions struct {
	ActorSystem      bool
	RPCServer        bool
	RPCClient        bool
	Redis            bool
	MongoDB          bool
	NSQ              bool
	MessageBroker    bool
	Registry         bool
	ServiceDiscovery bool
}

// DefaultComponentOptions 保持旧 NewBaseServer 的全量组件初始化行为。
func DefaultComponentOptions() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func GatewayComponents() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func LoginComponents() ComponentOptions {
	return ComponentOptions{
		ActorSystem:      true,
		RPCServer:        true,
		Redis:            true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func MongoServiceComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:        true,
		MongoDB:          true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}

func LobbyComponents() ComponentOptions {
	return MongoServiceComponents()
}

func GameComponents() ComponentOptions {
	return MongoServiceComponents()
}

func FriendComponents() ComponentOptions {
	return MongoServiceComponents()
}

func ChatComponents() ComponentOptions {
	return MongoServiceComponents()
}

func MailComponents() ComponentOptions {
	return MongoServiceComponents()
}

func GMComponents() ComponentOptions {
	return MongoServiceComponents()
}

func CenterComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:     true,
		NSQ:           true,
		MessageBroker: true,
		Registry:      true,
	}
}

func EnhancedGameComponents() ComponentOptions {
	return ComponentOptions{
		RPCServer:        true,
		NSQ:              true,
		MessageBroker:    true,
		Registry:         true,
		ServiceDiscovery: true,
	}
}
