package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tribeway/internal/server"
)

func main() {
	var (
		configFile = flag.String("config", "config/config.yaml", "config file path")
		nodeType   = flag.String("node", "gateway", "node type")
		nodeID     = flag.String("id", "node1", "node id")
	)
	flag.Parse()

	if *configFile == "" || *nodeType == "" || *nodeID == "" {
		fmt.Println("usage: -config=config/config.yaml -node=gateway -id=node1")
		os.Exit(1)
	}

	srv, err := server.NewServerWithError(*configFile, *nodeType, *nodeID)
	if err != nil {
		fmt.Printf("failed to create server: %v\n", err)
		os.Exit(1)
	}
	if err := srv.Start(); err != nil {
		fmt.Printf("failed to start server: %v\n", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	if err := srv.Stop(); err != nil {
		fmt.Printf("failed to stop server: %v\n", err)
		os.Exit(1)
	}
}
