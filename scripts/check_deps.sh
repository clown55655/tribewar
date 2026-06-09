#!/bin/bash

# 依赖检查脚本
set -e

echo "=== 检查Tribeway游戏服务器依赖 ==="
echo ""

# 检查状态标志
ALL_DEPS_OK=true

# 检查单个依赖
check_service() {
    local service_name=$1
    local check_command=$2
    local install_hint=$3
    
    echo -n "检查 $service_name... "
    
    if eval "$check_command" >/dev/null 2>&1; then
        echo "✅ 运行中"
    else
        echo "❌ 不可用"
        echo "  安装提示: $install_hint"
        ALL_DEPS_OK=false
    fi
}

# 检查Go环境
echo "📋 Go环境检查:"
check_service "Go (1.21+)" "go version | grep -E 'go1\.(2[1-9]|[3-9][0-9])'" "请安装Go 1.21或更高版本"

echo ""
echo "📋 基础设施依赖:"

# 检查Redis
check_service "Redis" "redis-cli ping" \
    "sudo apt-get install redis-server 或 docker run -d -p 6379:6379 redis:7-alpine"

# 检查MongoDB
check_service "MongoDB" "mongo --eval 'db.runCommand(\"ping\")'" \
    "sudo apt-get install mongodb-org 或 docker run -d -p 27017:27017 mongo:6.0"

# 检查ETCD
check_service "ETCD" "curl -s http://localhost:2379/health" \
    "下载并安装ETCD 3.5+ 或 docker run -d -p 2379:2379 quay.io/coreos/etcd:v3.5.9"

# 检查NSQ
check_service "NSQ Lookup" "curl -s http://localhost:4161/ping" \
    "下载并安装NSQ 1.2+ 或 docker run -d -p 4161:4161 nsqio/nsq:v1.2.1 /nsqlookupd"

check_service "NSQ Daemon" "curl -s http://localhost:4151/ping" \
    "启动nsqd: docker run -d -p 4150:4150 nsqio/nsq:v1.2.1 /nsqd --lookupd-tcp-address=localhost:4160"

echo ""
echo "📋 可选监控组件:"

# 检查Prometheus (可选)
check_service "Prometheus (可选)" "curl -s http://localhost:9090/metrics" \
    "下载并安装Prometheus 或 docker run -d -p 9090:9090 prom/prometheus"

# 检查Grafana (可选)
check_service "Grafana (可选)" "curl -s http://localhost:3000" \
    "下载并安装Grafana 或 docker run -d -p 3000:3000 grafana/grafana"

# 检查Node Exporter (可选)
check_service "Node Exporter (可选)" "curl -s http://localhost:9100/metrics" \
    "下载并安装Node Exporter 或 docker run -d -p 9100:9100 prom/node-exporter"

echo ""
echo "📋 Go工具依赖:"

# 检查Go模块工具
check_service "protoc" "protoc --version" \
    "安装Protocol Buffers: sudo apt-get install protobuf-compiler"

check_service "protoc-gen-go" "protoc-gen-go --version" \
    "安装Go插件: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"

echo ""
echo "📋 网络端口检查:"

# 检查端口占用
check_port() {
    local port=$1
    local service=$2
    
    echo -n "检查端口 $port ($service)... "
    
    if netstat -tuln 2>/dev/null | grep -q ":$port " || ss -tuln 2>/dev/null | grep -q ":$port "; then
        echo "⚠️  已占用"
    else
        echo "✅ 可用"
    fi
}

check_port "8001" "Gateway TCP"
check_port "8002" "Gateway TCP"
check_port "9001" "Gateway RPC"
check_port "7001" "监控接口"
check_port "6379" "Redis"
check_port "27017" "MongoDB"
check_port "2379" "ETCD"
check_port "4150" "NSQ"

echo ""
echo "📋 系统资源检查:"

# 检查系统资源
check_system_resources() {
    # 检查可用内存
    if command -v free >/dev/null 2>&1; then
        local available_mem=$(free -m | awk '/^Mem:/{print $7}')
        echo -n "可用内存: ${available_mem}MB "
        
        if [ "$available_mem" -lt 1024 ]; then
            echo "⚠️  内存可能不足"
        else
            echo "✅ 充足"
        fi
    fi
    
    # 检查磁盘空间
    if command -v df >/dev/null 2>&1; then
        local available_disk=$(df -BM . | awk 'NR==2{gsub(/M/,"",$4); print $4}')
        echo -n "可用磁盘: ${available_disk}MB "
        
        if [ "$available_disk" -lt 1024 ]; then
            echo "⚠️  磁盘空间可能不足"
        else
            echo "✅ 充足"
        fi
    fi
    
    # 检查CPU核心数
    if command -v nproc >/dev/null 2>&1; then
        local cpu_cores=$(nproc)
        echo "CPU核心数: $cpu_cores"
    fi
}

check_system_resources

echo ""
echo "📋 快速修复建议:"

# 给出快速修复建议
if [ "$ALL_DEPS_OK" = false ]; then
    echo "❌ 部分依赖未满足，以下是快速修复方案："
    echo ""
    echo "🐳 使用Docker快速启动所有依赖:"
    echo "   cd $PROJECT_ROOT"
    echo "   docker-compose up -d redis mongodb etcd nsqlookupd nsqd"
    echo ""
    echo "🚀 或者使用系统包管理器安装:"
    echo "   # Ubuntu/Debian:"
    echo "   sudo apt-get update"
    echo "   sudo apt-get install redis-server mongodb-org"
    echo ""
    echo "   # macOS (Homebrew):"
    echo "   brew install redis mongodb etcd nsq"
    echo ""
    echo "📝 安装完成后，请运行以下命令启动服务:"
    echo "   sudo systemctl start redis-server"
    echo "   sudo systemctl start mongod"
    echo "   etcd &"
    echo "   nsqlookupd &"
    echo "   nsqd --lookupd-tcp-address=127.0.0.1:4160 &"
    
else
    echo "✅ 所有必需依赖都已满足！"
    echo ""
    echo "🚀 现在可以启动Tribeway游戏服务器:"
    echo "   ./scripts/start.sh              # 启动基础版本"
    echo "   ./scripts/start_enhanced.sh     # 启动增强版本"
    echo "   ./scripts/start_enhanced.sh --with-monitoring  # 启动增强版本+监控"
    echo ""
    echo "📊 启动后可以访问:"
    echo "   - 服务状态: ./scripts/status.sh"
    echo "   - 监控面板: http://localhost:7001"
    echo "   - 性能分析: http://localhost:8001/debug/pprof/"
fi

echo ""
echo "=== 依赖检查完成 ==="

# 返回退出码
if [ "$ALL_DEPS_OK" = true ]; then
    exit 0
else
    exit 1
fi
