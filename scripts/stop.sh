#!/bin/bash

# 停止脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOG_DIR="$PROJECT_ROOT/logs"

# 停止服务函数
stop_service() {
    local service_type=$1
    local node_id=$2
    local pid_file="$LOG_DIR/${service_type}_${node_id}.pid"
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            echo "Stopping $service_type service (node: $node_id, PID: $pid)..."
            kill "$pid"
            
            # 等待进程停止
            local count=0
            while kill -0 "$pid" 2>/dev/null && [ $count -lt 10 ]; do
                sleep 1
                count=$((count + 1))
            done
            
            if kill -0 "$pid" 2>/dev/null; then
                echo "Force killing $service_type service..."
                kill -9 "$pid"
            fi
            
            echo "Stopped $service_type service"
        else
            echo "$service_type service (node: $node_id) is not running"
        fi
        rm -f "$pid_file"
    else
        echo "PID file not found for $service_type service (node: $node_id)"
    fi
}

# 停止所有服务
stop_all_services() {
    echo "=== 停止所有Tribeway游戏服务器 ==="
    
    # 按相反顺序停止服务
    stop_service "gm" "gm1"
    
    stop_service "game" "game1"
    stop_service "game" "game2"
    stop_service "game" "game3"
    
    stop_service "mail" "mail1"
    stop_service "chat" "chat1"
    stop_service "friend" "friend1"
    stop_service "lobby" "lobby1"
    
    stop_service "gateway" "gateway1"
    stop_service "gateway" "gateway2"
    
    stop_service "login" "login1"
    
    stop_service "center" "center1"
    
    echo "=== 所有服务已停止 ==="
}

# 如果传入了特定服务，只停止该服务
if [ $# -eq 2 ]; then
    stop_service "$1" "$2"
else
    stop_all_services
fi
