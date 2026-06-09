package main

import (
	"flag"
	"fmt"
	"os"

	"tribeway/internal/server"
)

func main() {
	var (
		configFile = flag.String("config", "config/config.yaml", "配置文件路径")
		nodeType   = flag.String("node", "gateway", "节点类型")
		nodeID     = flag.String("id", "node1", "节点ID")
	)
	flag.Parse()

	if *configFile == "" || *nodeType == "" || *nodeID == "" {
		fmt.Println("使用方法: -config=config/config.yaml -node=gateway -id=node1")
		os.Exit(1)
	}

	// 启动服务器节点
	srv := server.NewServer(*configFile, *nodeType, *nodeID)
	if err := srv.Start(); err != nil {
		fmt.Printf("启动服务器失败: %v\n", err)
		os.Exit(1)
	}
}
