#!/bin/bash

# 增强版启动脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONFIG_FILE="$PROJECT_ROOT/config/config.yaml"
BINARY_PATH="$PROJECT_ROOT/cmd/main.go"
LOG_DIR="$PROJECT_ROOT/logs"
MONITORING_PORT=7001

# 创建日志目录
mkdir -p "$LOG_DIR"

# 启动函数
start_service() {
    local service_type=$1
    local node_id=$2
    local extra_args=$3
    local log_file="$LOG_DIR/${service_type}_${node_id}.log"
    
    echo "Starting $service_type service (node: $node_id)..."
    
    nohup go run "$BINARY_PATH" \
        -config="$CONFIG_FILE" \
        -node="$service_type" \
        -id="$node_id" \
        $extra_args \
        > "$log_file" 2>&1 &
    
    local pid=$!
    echo "$pid" > "$LOG_DIR/${service_type}_${node_id}.pid"
    echo "Started $service_type service with PID $pid"
    sleep 2
}

# 检查扩展依赖服务
check_enhanced_dependencies() {
    echo "Checking enhanced dependencies..."
    
    # 检查基础依赖
    ./scripts/check_deps.sh
    
    # 检查Prometheus是否运行 (可选)
    if curl -s http://localhost:9090/metrics > /dev/null 2>&1; then
        echo "Prometheus: 运行中"
    else
        echo "Prometheus: 未运行 (可选组件)"
    fi
    
    # 检查Grafana是否运行 (可选)
    if curl -s http://localhost:3000 > /dev/null 2>&1; then
        echo "Grafana: 运行中"
    else
        echo "Grafana: 未运行 (可选组件)"
    fi
    
    echo "Dependencies check completed."
}

# 启动增强版监控服务
start_monitoring_stack() {
    echo "=== 启动监控技术栈 ==="
    
    # 启动Prometheus (可选)
    if command -v prometheus >/dev/null 2>&1; then
        echo "Starting Prometheus..."
        nohup prometheus --config.file=monitoring/prometheus.yml \
            --storage.tsdb.path=./data/prometheus \
            --web.console.libraries=./console_libraries \
            --web.console.templates=./consoles \
            --web.enable-lifecycle \
            --web.enable-admin-api \
            > "$LOG_DIR/prometheus.log" 2>&1 &
        echo $! > "$LOG_DIR/prometheus.pid"
    fi
    
    # 启动Grafana (可选)
    if command -v grafana-server >/dev/null 2>&1; then
        echo "Starting Grafana..."
        nohup grafana-server --homepath /usr/share/grafana \
            --config /etc/grafana/grafana.ini \
            > "$LOG_DIR/grafana.log" 2>&1 &
        echo $! > "$LOG_DIR/grafana.pid"
    fi
    
    echo "Monitoring stack startup completed."
}

# 主启动流程
main() {
    echo "=== 启动Tribeway增强版游戏服务器框架 ==="
    
    check_enhanced_dependencies
    
    # 可选：启动监控技术栈
    if [ "$1" = "--with-monitoring" ]; then
        start_monitoring_stack
        sleep 5
    fi
    
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
    
    # 5. 启动增强版游戏服务器（包含所有新功能）
    start_service "enhanced_game" "game1"
    start_service "enhanced_game" "game2"
    start_service "enhanced_game" "game3"
    sleep 2
    
    # 6. 启动GM服务器
    start_service "gm" "gm1"
    
    echo "=== 增强版服务启动完成 ==="
    echo ""
    echo "🚀 服务访问地址："
    echo "  - 游戏网关: tcp://localhost:8001, tcp://localhost:8002"
    echo "  - 监控面板: http://localhost:$MONITORING_PORT"
    echo "  - pprof分析: http://localhost:$((MONITORING_PORT + 1000))"
    echo "  - NSQ管理: http://localhost:4171"
    echo "  - Redis管理: http://localhost:8081"
    echo "  - MongoDB管理: http://localhost:8082"
    
    if [ "$1" = "--with-monitoring" ]; then
        echo "  - Prometheus: http://localhost:9090"
        echo "  - Grafana: http://localhost:3000 (admin/admin)"
    fi
    
    echo ""
    echo "📊 监控功能："
    echo "  - 系统指标: curl http://localhost:$MONITORING_PORT/api/metrics"
    echo "  - 健康检查: curl http://localhost:$MONITORING_PORT/health"
    echo "  - 内存分析: go tool pprof http://localhost:$((MONITORING_PORT + 1000))/debug/pprof/heap"
    echo "  - CPU分析: go tool pprof http://localhost:$((MONITORING_PORT + 1000))/debug/pprof/profile"
    echo ""
    echo "🛠️ 管理命令："
    echo "  - 查看状态: ./scripts/status.sh"
    echo "  - 停止服务: ./scripts/stop.sh"
    echo "  - 热更新: ./scripts/hot_reload.sh [config|logic|data]"
    echo ""
    echo "日志文件位置: $LOG_DIR"
}

# 如果传入了特定服务类型，只启动该服务
if [ $# -ge 2 ]; then
    start_service "$1" "$2" "$3"
else
    main "$@"
fi
