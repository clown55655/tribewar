package server

import (
	"fmt"
	"strings"
)

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

func (opts ComponentOptions) Validate() error {
	var problems []string
	if opts.MessageBroker && !opts.NSQ {
		problems = append(problems, "message broker requires nsq")
	}
	if opts.ServiceDiscovery && !opts.Registry {
		problems = append(problems, "service discovery requires registry")
	}
	if opts.RPCClient {
		problems = append(problems, "rpc client component is not implemented")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid component options: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (opts ComponentOptions) EnabledNames() []string {
	names := make([]string, 0, 9)
	if opts.ActorSystem {
		names = append(names, "actor_system")
	}
	if opts.RPCServer {
		names = append(names, "rpc_server")
	}
	if opts.RPCClient {
		names = append(names, "rpc_client")
	}
	if opts.Redis {
		names = append(names, "redis")
	}
	if opts.MongoDB {
		names = append(names, "mongodb")
	}
	if opts.NSQ {
		names = append(names, "nsq")
	}
	if opts.MessageBroker {
		names = append(names, "message_broker")
	}
	if opts.Registry {
		names = append(names, "registry")
	}
	if opts.ServiceDiscovery {
		names = append(names, "service_discovery")
	}
	return names
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
