package server

import (
	"fmt"
	"time"

	"tribeway/internal/logger"
)

type componentStopper struct {
	name string
	stop func(*BaseServer) error
}

var componentStoppers = []componentStopper{
	{
		name: "tcp_server",
		stop: func(bs *BaseServer) error {
			if bs.tcpServer == nil {
				return nil
			}
			return bs.tcpServer.Stop()
		},
	},
	{
		name: "rpc_server",
		stop: func(bs *BaseServer) error {
			if bs.rpcServer == nil {
				return nil
			}
			return bs.rpcServer.Stop()
		},
	},
	{
		name: "actor_system",
		stop: func(bs *BaseServer) error {
			if bs.actorSystem == nil {
				return nil
			}
			return bs.actorSystem.Shutdown()
		},
	},
	{
		name: "nsq",
		stop: func(bs *BaseServer) error {
			if bs.nsqManager == nil {
				return nil
			}
			return bs.nsqManager.Close()
		},
	},
	{
		name: "registry",
		stop: func(bs *BaseServer) error {
			if bs.registry == nil {
				return nil
			}
			if err := bs.registry.Unregister(bs.nodeID); err != nil {
				logger.Warnf("Failed to unregister service %s: %v", bs.nodeID, err)
			}
			return bs.registry.Close()
		},
	},
	{
		name: "redis",
		stop: func(bs *BaseServer) error {
			if bs.redisManager == nil {
				return nil
			}
			return bs.redisManager.Close()
		},
	},
	{
		name: "mongodb",
		stop: func(bs *BaseServer) error {
			if bs.mongoManager == nil {
				return nil
			}
			return bs.mongoManager.Close()
		},
	},
}

func (bs *BaseServer) stopComponents() error {
	var firstErr error
	for _, stopper := range componentStoppers {
		start := time.Now()
		if err := stopper.stop(bs); err != nil {
			wrapped := fmt.Errorf("component %s: %v", stopper.name, err)
			logger.Error(wrapped.Error())
			if firstErr == nil {
				firstErr = wrapped
			}
			continue
		}
		logger.Debugf("Component %s stopped in %v", stopper.name, time.Since(start))
	}
	return firstErr
}
