#!/bin/bash

# 状态查看脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOG_DIR="$PROJECT_ROOT/logs"

# 检查服务状态
check_service_status() {
    local service_type=$1
    local node_id=$2
    local pid_file="$LOG_DIR/${service_type}_${node_id}.pid"
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            # 获取进程信息
            local memory=$(ps -o rss= -p "$pid" 2>/dev/null | awk '{print int($1/1024)}' || echo "N/A")
            local cpu=$(ps -o %cpu= -p "$pid" 2>/dev/null | awk '{print $1}' || echo "N/A")
            local uptime=$(ps -o etime= -p "$pid" 2>/dev/null | awk '{print $1}' || echo "N/A")
            
            printf "%-12s %-10s %-8s %-10s %-8s %-10s\n" "$service_type" "$node_id" "RUNNING" "$pid" "${memory}MB" "$uptime"
        else
            printf "%-12s %-10s %-8s %-10s %-8s %-10s\n" "$service_type" "$node_id" "STOPPED" "-" "-" "-"
        fi
    else
        printf "%-12s %-10s %-8s %-10s %-8s %-10s\n" "$service_type" "$node_id" "UNKNOWN" "-" "-" "-"
    fi
}

# 显示所有服务状态
show_all_status() {
    echo "=== Tribeway游戏服务器状态 ==="
    echo
    
    # 表头
    printf "%-12s %-10s %-8s %-10s %-8s %-10s\n" "SERVICE" "NODE_ID" "STATUS" "PID" "MEMORY" "UPTIME"
    printf "%-12s %-10s %-8s %-10s %-8s %-10s\n" "--------" "-------" "------" "---" "------" "------"
    
    # 检查所有服务
    check_service_status "center" "center1"
    check_service_status "login" "login1"
    check_service_status "gateway" "gateway1"
    check_service_status "gateway" "gateway2"
    check_service_status "lobby" "lobby1"
    check_service_status "friend" "friend1"
    check_service_status "chat" "chat1"
    check_service_status "mail" "mail1"
    check_service_status "game" "game1"
    check_service_status "game" "game2"
    check_service_status "game" "game3"
    check_service_status "gm" "gm1"
    
    echo
    echo "=== 服务统计 ==="
    
    local running_count=0
    local total_count=12
    
    # 统计运行中的服务
    for service in "center_center1" "login_login1" "gateway_gateway1" "gateway_gateway2" "lobby_lobby1" "friend_friend1" "chat_chat1" "mail_mail1" "game_game1" "game_game2" "game_game3" "gm_gm1"; do
        local pid_file="$LOG_DIR/${service}.pid"
        if [ -f "$pid_file" ]; then
            local pid=$(cat "$pid_file")
            if kill -0 "$pid" 2>/dev/null; then
                running_count=$((running_count + 1))
            fi
        fi
    done
    
    echo "运行中: $running_count/$total_count"
    echo "停止: $((total_count - running_count))/$total_count"
    
    # 显示依赖服务状态
    echo
    echo "=== 依赖服务状态 ==="
    
    # Redis
    if redis-cli ping > /dev/null 2>&1; then
        echo "Redis: 运行中"
    else
        echo "Redis: 不可用"
    fi
    
    # MongoDB
    if mongo --eval "db.runCommand('ping')" > /dev/null 2>&1; then
        echo "MongoDB: 运行中"
    else
        echo "MongoDB: 不可用"
    fi
    
    # ETCD
    if curl -s http://localhost:2379/health > /dev/null; then
        echo "ETCD: 运行中"
    else
        echo "ETCD: 不可用"
    fi
    
    # NSQ
    if curl -s http://localhost:4161/ping > /dev/null; then
        echo "NSQ: 运行中"
    else
        echo "NSQ: 不可用"
    fi
}

# 实时监控
watch_status() {
    while true; do
        clear
        show_all_status
        echo
        echo "按 Ctrl+C 退出监控"
        sleep 3
    done
}

# 主函数
if [ "$1" = "watch" ]; then
    watch_status
else
    show_all_status
fi
