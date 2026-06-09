#!/bin/bash

# 启动脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONFIG_FILE="$PROJECT_ROOT/config/config.yaml"
BINARY_PATH="$PROJECT_ROOT/cmd/main.go"
LOG_DIR="$PROJECT_ROOT/logs"

# 创建日志目录
mkdir -p "$LOG_DIR"

# 启动函数
start_service() {
    local service_type=$1
    local node_id=$2
    local log_file="$LOG_DIR/${service_type}_${node_id}.log"
    
    echo "Starting $service_type service (node: $node_id)..."
    
    nohup go run "$BINARY_PATH" \
        -config="$CONFIG_FILE" \
        -node="$service_type" \
        -id="$node_id" \
        > "$log_file" 2>&1 &
    
    local pid=$!
    echo "$pid" > "$LOG_DIR/${service_type}_${node_id}.pid"
    echo "Started $service_type service with PID $pid"
    sleep 2
}

# 检查依赖服务
check_dependencies() {
    echo "Checking dependencies..."
    
    # 检查Redis
    if ! redis-cli ping > /dev/null 2>&1; then
        echo "Warning: Redis is not running"
    fi
    
    # 检查MongoDB
    if ! mongo --eval "db.runCommand('ping')" > /dev/null 2>&1; then
        echo "Warning: MongoDB is not running"
    fi
    
    # 检查ETCD
    if ! curl -s http://localhost:2379/health > /dev/null; then
        echo "Warning: ETCD is not running"
    fi
    
    # 检查NSQ
    if ! curl -s http://localhost:4161/ping > /dev/null; then
        echo "Warning: NSQ is not running"
    fi
}

# 主启动流程
main() {
    echo "=== 启动Tribeway游戏服务器框架 ==="
    
    check_dependencies
    
    # 启动顺序很重要
    # 1. 先启动中心服务器
    start_service "center" "center1"
    sleep 3
    
    # 2. 启动核心服务
    start_service "login" "login1"
    sleep 2
    
    # 3. 启动网关服务器
    start_service "gateway" "gateway1"
    start_service "gateway" "gateway2"
    sleep 2
    
    # 4. 启动业务服务
    start_service "lobby" "lobby1"
    start_service "friend" "friend1"
    start_service "chat" "chat1"
    start_service "mail" "mail1"
    sleep 2
    
    # 5. 启动游戏服务器
    start_service "game" "game1"
    start_service "game" "game2"
    start_service "game" "game3"
    sleep 2
    
    # 6. 启动GM服务器
    start_service "gm" "gm1"
    
    echo "=== 所有服务启动完成 ==="
    echo "日志文件位置: $LOG_DIR"
    echo "使用 './scripts/stop.sh' 停止所有服务"
    echo "使用 './scripts/status.sh' 查看服务状态"
}

# 如果传入了特定服务类型，只启动该服务
if [ $# -eq 2 ]; then
    start_service "$1" "$2"
else
    main
fi
