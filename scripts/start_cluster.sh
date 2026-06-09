#!/bin/bash

# Tribeway 游戏服务器集群启动脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
CLUSTER_CONFIG_FILE="$PROJECT_ROOT/config/cluster.yaml"
LOG_DIR="$PROJECT_ROOT/logs"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 创建日志目录
mkdir -p "$LOG_DIR"

# 打印带颜色的状态
print_status() {
    local status=$1
    local message=$2
    
    case "$status" in
        "INFO")
            echo -e "ℹ️  ${BLUE}${message}${NC}"
            ;;
        "SUCCESS")
            echo -e "✅ ${GREEN}${message}${NC}"
            ;;
        "WARNING")
            echo -e "⚠️  ${YELLOW}${message}${NC}"
            ;;
        "ERROR")
            echo -e "❌ ${RED}${message}${NC}"
            ;;
    esac
}

# 检查Docker环境
check_docker() {
    print_status "INFO" "检查Docker环境..."
    
    if ! command -v docker >/dev/null 2>&1; then
        print_status "ERROR" "Docker未安装，请先安装Docker"
        exit 1
    fi
    
    if ! command -v docker-compose >/dev/null 2>&1; then
        print_status "ERROR" "Docker Compose未安装，请先安装Docker Compose"
        exit 1
    fi
    
    # 检查Docker是否运行
    if ! docker info >/dev/null 2>&1; then
        print_status "ERROR" "Docker服务未运行，请启动Docker服务"
        exit 1
    fi
    
    print_status "SUCCESS" "Docker环境检查完成"
}

check_required_secrets() {
    print_status "INFO" "检查必需密钥环境变量..."

    local missing=0
    for var_name in TRIBEWAY_TOKEN_SECRET TRIBEWAY_MONGODB_PASSWORD TRIBEWAY_MONGODB_APP_PASSWORD TRIBEWAY_GRAFANA_ADMIN_PASSWORD; do
        if [ -z "${!var_name}" ]; then
            print_status "ERROR" "缺少环境变量: $var_name"
            missing=1
        fi
    done

    if [ "$missing" -ne 0 ]; then
        echo ""
        echo "请先设置必需密钥，例如："
        echo "  export TRIBEWAY_TOKEN_SECRET='<strong random secret>'"
        echo "  export TRIBEWAY_MONGODB_PASSWORD='<mongodb root password>'"
        echo "  export TRIBEWAY_MONGODB_APP_PASSWORD='<mongodb app password>'"
        echo "  export TRIBEWAY_GRAFANA_ADMIN_PASSWORD='<grafana admin password>'"
        exit 1
    fi
}

# 构建集群镜像
build_cluster_images() {
    print_status "INFO" "构建集群镜像..."
    
    # 构建主应用镜像
    docker build -t tribeway-cluster:latest .
    
    # 构建集群初始化镜像
    docker build -f Dockerfile.cluster-init -t tribeway-cluster-init:latest .
    
    print_status "SUCCESS" "集群镜像构建完成"
}

# 启动基础设施集群
start_infrastructure() {
    print_status "INFO" "启动基础设施集群..."
    
    # 停止现有服务
    docker-compose -f docker-compose.cluster.yml down --remove-orphans || true
    
    # 清理网络
    docker network prune -f || true
    
    # 分阶段启动基础设施
    print_status "INFO" "启动ETCD集群..."
    docker-compose -f docker-compose.cluster.yml up -d etcd-1 etcd-2 etcd-3
    sleep 10
    
    print_status "INFO" "启动Redis集群..."
    docker-compose -f docker-compose.cluster.yml up -d redis-cluster-1 redis-cluster-2 redis-cluster-3 redis-cluster-4 redis-cluster-5 redis-cluster-6
    sleep 15
    
    print_status "INFO" "初始化Redis集群..."
    docker-compose -f docker-compose.cluster.yml up --no-deps redis-cluster-init
    sleep 5
    
    print_status "INFO" "启动MongoDB副本集..."
    docker-compose -f docker-compose.cluster.yml up -d mongodb-rs-1 mongodb-rs-2 mongodb-rs-3
    sleep 20
    
    print_status "INFO" "初始化MongoDB副本集..."
    docker-compose -f docker-compose.cluster.yml up --no-deps mongodb-rs-init
    sleep 10
    
    print_status "INFO" "启动NSQ集群..."
    docker-compose -f docker-compose.cluster.yml up -d nsqlookupd-1 nsqlookupd-2 nsqd-1 nsqd-2 nsqd-3
    sleep 10
    
    print_status "SUCCESS" "基础设施集群启动完成"
}

# 启动应用服务集群
start_application_services() {
    print_status "INFO" "启动Tribeway应用服务集群..."
    
    # 启动中心服务
    print_status "INFO" "启动中心服务..."
    docker-compose -f docker-compose.cluster.yml up -d tribeway-center-cluster
    sleep 10
    
    # 启动网关集群
    print_status "INFO" "启动网关集群..."
    docker-compose -f docker-compose.cluster.yml up -d tribeway-gateway-cluster-1 tribeway-gateway-cluster-2
    sleep 5
    
    # 启动负载均衡器
    print_status "INFO" "启动负载均衡器..."
    docker-compose -f docker-compose.cluster.yml up -d nginx-lb
    
    print_status "SUCCESS" "应用服务集群启动完成"
}

# 启动监控服务
start_monitoring() {
    print_status "INFO" "启动监控服务..."
    
    docker-compose -f docker-compose.cluster.yml up -d prometheus-cluster grafana-cluster
    
    print_status "SUCCESS" "监控服务启动完成"
}

# 验证集群健康状态
verify_cluster_health() {
    print_status "INFO" "验证集群健康状态..."
    
    local health_score=0
    local total_checks=0
    
    # 检查Redis集群
    total_checks=$((total_checks + 1))
    if docker exec tribeway-redis-cluster-1 redis-cli cluster info | grep -q "cluster_state:ok"; then
        print_status "SUCCESS" "Redis集群状态正常"
        health_score=$((health_score + 1))
    else
        print_status "WARNING" "Redis集群状态异常"
    fi
    
    # 检查MongoDB副本集
    total_checks=$((total_checks + 1))
    if docker exec tribeway-mongodb-rs-1 mongosh --eval "rs.status().ok" --quiet | grep -q "1"; then
        print_status "SUCCESS" "MongoDB副本集状态正常"
        health_score=$((health_score + 1))
    else
        print_status "WARNING" "MongoDB副本集状态异常"
    fi
    
    # 检查ETCD集群
    total_checks=$((total_checks + 1))
    if docker exec tribeway-etcd-1 etcdctl endpoint health --endpoints=http://172.20.3.1:2379,http://172.20.3.2:2379,http://172.20.3.3:2379 | grep -q "is healthy"; then
        print_status "SUCCESS" "ETCD集群状态正常"
        health_score=$((health_score + 1))
    else
        print_status "WARNING" "ETCD集群状态异常"
    fi
    
    # 检查NSQ集群
    total_checks=$((total_checks + 1))
    if curl -s http://localhost:4161/ping >/dev/null && curl -s http://localhost:4163/ping >/dev/null; then
        print_status "SUCCESS" "NSQ集群状态正常"
        health_score=$((health_score + 1))
    else
        print_status "WARNING" "NSQ集群状态异常"
    fi
    
    # 计算健康分数
    health_percentage=$((health_score * 100 / total_checks))
    
    echo ""
    print_status "INFO" "集群健康评分: $health_percentage% ($health_score/$total_checks)"
    
    if [ $health_percentage -ge 75 ]; then
        print_status "SUCCESS" "集群整体状态良好"
        return 0
    else
        print_status "WARNING" "集群状态需要关注"
        return 1
    fi
}

# 显示集群访问信息
show_cluster_info() {
    echo ""
    print_status "INFO" "=== Tribeway 集群访问信息 ==="
    echo ""
    
    echo "🌐 负载均衡器:"
    echo "  - HTTP: http://localhost"
    echo "  - HTTPS: https://localhost (如果配置了SSL)"
    echo ""
    
    echo "🎮 游戏服务端点:"
    echo "  - Gateway 1: tcp://localhost:8001"
    echo "  - Gateway 2: tcp://localhost:8002"
    echo "  - Gateway LB: tcp://localhost:80"
    echo ""
    
    echo "📊 监控面板:"
    echo "  - Prometheus: http://localhost:9090"
    echo "  - Grafana: http://localhost:3000 (admin / \$TRIBEWAY_GRAFANA_ADMIN_PASSWORD)"
    echo "  - NSQ Admin: http://localhost:4171"
    echo ""
    
    echo "🗄️ 数据库集群:"
    echo "  - Redis Cluster:"
    for i in {0..5}; do
        port=$((7000 + i))
        echo "    * 节点$((i+1)): localhost:$port"
    done
    echo ""
    echo "  - MongoDB 副本集:"
    echo "    * Primary: localhost:27017"
    echo "    * Secondary: localhost:27018"
    echo "    * Secondary: localhost:27019"
    echo ""
    echo "    连接字符串请使用 TRIBEWAY_MONGODB_APP_PASSWORD 注入，不要在脚本中打印真实密码。"
    echo ""
    
    echo "📡 消息队列集群:"
    echo "  - NSQ Lookup:"
    echo "    * Lookup-1: http://localhost:4161"
    echo "    * Lookup-2: http://localhost:4163"
    echo "  - NSQ Daemon:"
    echo "    * NSQD-1: http://localhost:4150"
    echo "    * NSQD-2: http://localhost:4152"
    echo "    * NSQD-3: http://localhost:4154"
    echo ""
    
    echo "🔧 管理工具:"
    echo "  - 集群状态: ./scripts/cluster_status.sh"
    echo "  - 集群扩缩容: ./scripts/cluster_scale.sh"
    echo "  - 集群备份: ./scripts/cluster_backup.sh"
    echo "  - 故障恢复: ./scripts/cluster_recovery.sh"
}

# 等待用户确认
confirm_start() {
    echo ""
    print_status "WARNING" "即将启动Tribeway游戏服务器集群"
    echo ""
    echo "集群包含以下组件:"
    echo "  - Redis 集群 (6节点)"
    echo "  - MongoDB 副本集 (3节点)"
    echo "  - ETCD 集群 (3节点)"
    echo "  - NSQ 集群 (3 NSQD + 2 NSQLookupd)"
    echo "  - Tribeway 应用服务"
    echo "  - 监控和负载均衡"
    echo ""
    echo "预计资源占用:"
    echo "  - 内存: ~8GB"
    echo "  - CPU: ~4核"
    echo "  - 磁盘: ~10GB"
    echo ""
    
    if [ "$1" != "--force" ]; then
        read -p "确认启动？(y/N): " confirm
        if [[ ! $confirm =~ ^[Yy]$ ]]; then
            echo "操作已取消"
            exit 0
        fi
    fi
}

# 主启动流程
main() {
    echo "🚀 Tribeway 游戏服务器集群启动器"
    echo "========================================"
    
    # 解析命令行参数
    MONITORING=false
    FORCE=false
    SKIP_BUILD=false
    
    for arg in "$@"; do
        case $arg in
            --with-monitoring)
                MONITORING=true
                ;;
            --force)
                FORCE=true
                ;;
            --skip-build)
                SKIP_BUILD=true
                ;;
            --help)
                echo "用法: $0 [选项]"
                echo "选项:"
                echo "  --with-monitoring  启动监控服务"
                echo "  --force           跳过确认提示"
                echo "  --skip-build      跳过镜像构建"
                echo "  --help            显示帮助信息"
                exit 0
                ;;
        esac
    done
    
    # 检查环境
    check_docker
    check_required_secrets
    
    # 确认启动
    if [ "$FORCE" = true ]; then
        confirm_start --force
    else
        confirm_start
    fi
    
    # 构建镜像
    if [ "$SKIP_BUILD" = false ]; then
        build_cluster_images
    fi
    
    # 启动基础设施
    start_infrastructure
    
    # 验证基础设施健康状态
    if verify_cluster_health; then
        print_status "SUCCESS" "基础设施集群启动成功"
    else
        print_status "WARNING" "基础设施集群部分异常，继续启动应用服务..."
    fi
    
    # 启动应用服务
    start_application_services
    
    # 启动监控（可选）
    if [ "$MONITORING" = true ]; then
        start_monitoring
    fi
    
    # 最终健康检查
    echo ""
    print_status "INFO" "执行最终健康检查..."
    sleep 10
    
    if verify_cluster_health; then
        print_status "SUCCESS" "🎉 Tribeway集群启动成功！"
        show_cluster_info
        
        echo ""
        print_status "INFO" "集群已就绪，开始游戏开发之旅！"
        
        # 提示后续操作
        echo ""
        echo "📋 下一步操作建议:"
        echo "  1. 运行客户端测试: go run examples/client/main.go"
        echo "  2. 查看集群状态: ./scripts/cluster_status.sh"
        echo "  3. 监控集群指标: go run tools/performance_analyzer.go watch"
        echo "  4. 查看实时日志: docker-compose -f docker-compose.cluster.yml logs -f"
        
    else
        print_status "ERROR" "集群启动不完整，请检查日志"
        echo ""
        echo "🔧 故障排查建议:"
        echo "  1. 查看容器状态: docker-compose -f docker-compose.cluster.yml ps"
        echo "  2. 查看容器日志: docker-compose -f docker-compose.cluster.yml logs [service_name]"
        echo "  3. 重新启动: ./scripts/stop_cluster.sh && ./scripts/start_cluster.sh"
        echo "  4. 清理重启: ./scripts/clean_cluster.sh && ./scripts/start_cluster.sh"
        
        exit 1
    fi
}

# 快速启动模式
quick_start() {
    print_status "INFO" "快速启动模式（适合开发环境）"
    
    # 启动基础设施
    docker-compose -f docker-compose.cluster.yml up -d \
        etcd-1 redis-cluster-1 redis-cluster-2 redis-cluster-3 \
        mongodb-rs-1 nsqlookupd-1 nsqd-1
    
    sleep 20
    
    # 简化集群初始化
    docker exec tribeway-redis-cluster-1 redis-cli --cluster create \
        172.20.1.1:6379 172.20.1.2:6379 172.20.1.3:6379 \
        --cluster-yes || true
    
    # 启动应用服务
    docker-compose -f docker-compose.cluster.yml up -d tribeway-center-cluster tribeway-gateway-cluster-1
    
    print_status "SUCCESS" "快速启动完成！访问: http://localhost:8001"
}

# 检查现有集群
check_existing_cluster() {
    if docker-compose -f docker-compose.cluster.yml ps | grep -q "Up"; then
        print_status "WARNING" "检测到已运行的集群服务"
        
        echo "当前运行的服务:"
        docker-compose -f docker-compose.cluster.yml ps
        echo ""
        
        read -p "是否停止现有服务并重新启动？(y/N): " restart_confirm
        if [[ $restart_confirm =~ ^[Yy]$ ]]; then
            print_status "INFO" "停止现有集群..."
            docker-compose -f docker-compose.cluster.yml down
            echo ""
        else
            print_status "INFO" "取消启动，保持现有集群运行"
            exit 0
        fi
    fi
}

# 执行启动
case "${1:-full}" in
    "full")
        check_existing_cluster
        main "${@:2}"
        ;;
    "quick")
        check_existing_cluster
        quick_start
        ;;
    "infra")
        check_existing_cluster
        check_docker
        check_required_secrets
        start_infrastructure
        ;;
    "app")
        start_application_services
        ;;
    "monitor")
        start_monitoring
        ;;
    "help")
        echo "Tribeway 集群启动脚本"
        echo ""
        echo "用法: $0 [模式] [选项]"
        echo ""
        echo "启动模式:"
        echo "  full     完整集群启动（默认）"
        echo "  quick    快速启动（开发用）"
        echo "  infra    仅启动基础设施"
        echo "  app      仅启动应用服务"
        echo "  monitor  仅启动监控服务"
        echo ""
        echo "选项:"
        echo "  --with-monitoring  启动完整监控"
        echo "  --force           跳过确认"
        echo "  --skip-build      跳过构建"
        echo ""
        echo "示例:"
        echo "  $0 full --with-monitoring --force"
        echo "  $0 quick"
        echo "  $0 infra"
        ;;
    *)
        print_status "ERROR" "未知启动模式: $1"
        echo "使用 '$0 help' 查看帮助信息"
        exit 1
        ;;
esac
