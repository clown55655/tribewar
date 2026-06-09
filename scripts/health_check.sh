#!/bin/bash

# 健康检查脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOG_DIR="$PROJECT_ROOT/logs"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 健康检查结果
OVERALL_HEALTH="healthy"
FAILED_CHECKS=0
WARN_CHECKS=0

# 打印带颜色的状态
print_status() {
    local status=$1
    local message=$2
    local details=$3
    
    case "$status" in
        "OK")
            echo -e "✅ ${GREEN}${message}${NC}"
            ;;
        "WARN")
            echo -e "⚠️  ${YELLOW}${message}${NC}"
            if [ -n "$details" ]; then
                echo -e "   ${details}"
            fi
            WARN_CHECKS=$((WARN_CHECKS + 1))
            ;;
        "FAIL")
            echo -e "❌ ${RED}${message}${NC}"
            if [ -n "$details" ]; then
                echo -e "   ${details}"
            fi
            FAILED_CHECKS=$((FAILED_CHECKS + 1))
            OVERALL_HEALTH="unhealthy"
            ;;
        "INFO")
            echo -e "ℹ️  ${BLUE}${message}${NC}"
            ;;
    esac
}

# 检查基础设施服务
check_infrastructure() {
    echo "📊 检查基础设施服务..."
    
    # Redis检查
    if redis-cli ping >/dev/null 2>&1; then
        local redis_info=$(redis-cli info server | grep redis_version | cut -d: -f2 | tr -d '\r')
        print_status "OK" "Redis 服务正常" "版本: $redis_info"
        
        # 检查Redis内存使用
        local used_memory=$(redis-cli info memory | grep used_memory_human | cut -d: -f2 | tr -d '\r')
        print_status "INFO" "Redis 内存使用: $used_memory"
    else
        print_status "FAIL" "Redis 服务不可用" "请启动Redis服务"
    fi
    
    # MongoDB检查
    if mongo --eval "db.runCommand('ping')" >/dev/null 2>&1; then
        local mongo_version=$(mongo --eval "db.version()" --quiet 2>/dev/null || echo "unknown")
        print_status "OK" "MongoDB 服务正常" "版本: $mongo_version"
        
        # 检查MongoDB连接数
        local connections=$(mongo --eval "db.runCommand('serverStatus').connections.current" --quiet 2>/dev/null || echo "0")
        print_status "INFO" "MongoDB 当前连接数: $connections"
    else
        print_status "FAIL" "MongoDB 服务不可用" "请启动MongoDB服务"
    fi
    
    # ETCD检查
    if curl -s http://localhost:2379/health >/dev/null 2>&1; then
        local etcd_version=$(curl -s http://localhost:2379/version | jq -r .etcdserver 2>/dev/null || echo "unknown")
        print_status "OK" "ETCD 服务正常" "版本: $etcd_version"
    else
        print_status "FAIL" "ETCD 服务不可用" "请启动ETCD服务"
    fi
    
    # NSQ检查
    if curl -s http://localhost:4161/ping >/dev/null 2>&1; then
        print_status "OK" "NSQ Lookup 服务正常"
    else
        print_status "FAIL" "NSQ Lookup 服务不可用" "请启动nsqlookupd"
    fi
    
    if curl -s http://localhost:4151/ping >/dev/null 2>&1; then
        print_status "OK" "NSQ Daemon 服务正常"
        
        # 检查NSQ统计信息
        local stats=$(curl -s http://localhost:4151/stats | jq -r .topics 2>/dev/null || echo "[]")
        local topic_count=$(echo "$stats" | jq length 2>/dev/null || echo "0")
        print_status "INFO" "NSQ 主题数量: $topic_count"
    else
        print_status "FAIL" "NSQ Daemon 服务不可用" "请启动nsqd"
    fi
}

# 检查Tribeway服务
check_tribeway_services() {
    echo ""
    echo "🎮 检查Tribeway游戏服务..."
    
    local services=(
        "center:7010"
        "gateway1:7001"
        "gateway2:7002" 
        "login:7020"
        "lobby:7030"
        "game1:7100"
        "game2:7101"
        "game3:7102"
        "friend:7040"
        "chat:7050"
        "mail:7060"
        "gm:7200"
    )
    
    local running_services=0
    local total_services=${#services[@]}
    
    for service in "${services[@]}"; do
        local service_name=$(echo "$service" | cut -d: -f1)
        local port=$(echo "$service" | cut -d: -f2)
        
        # 检查HTTP健康端点
        if timeout 3 curl -s "http://localhost:$port/health" >/dev/null 2>&1; then
            # 获取服务详细信息
            local response=$(curl -s "http://localhost:$port/health" 2>/dev/null)
            local status=$(echo "$response" | jq -r .status 2>/dev/null || echo "unknown")
            
            if [ "$status" = "healthy" ]; then
                print_status "OK" "$service_name 服务健康"
                running_services=$((running_services + 1))
                
                # 获取额外信息
                local node_id=$(echo "$response" | jq -r .node_id 2>/dev/null)
                local timestamp=$(echo "$response" | jq -r .timestamp 2>/dev/null)
                if [ "$node_id" != "null" ] && [ "$node_id" != "" ]; then
                    print_status "INFO" "  节点ID: $node_id"
                fi
            else
                print_status "WARN" "$service_name 服务状态异常" "状态: $status"
            fi
        else
            # 检查进程是否存在
            local pid_file="$LOG_DIR/${service_name}_${service_name}1.pid"
            if [ -f "$pid_file" ]; then
                local pid=$(cat "$pid_file")
                if kill -0 "$pid" 2>/dev/null; then
                    print_status "WARN" "$service_name 进程存在但HTTP不可用" "PID: $pid"
                else
                    print_status "FAIL" "$service_name 服务未运行" "PID文件存在但进程已死"
                fi
            else
                print_status "FAIL" "$service_name 服务未启动" "端口: $port"
            fi
        fi
    done
    
    # 计算服务可用率
    local availability=$(( running_services * 100 / total_services ))
    
    if [ $availability -ge 90 ]; then
        print_status "OK" "服务可用率: ${availability}% (${running_services}/${total_services})"
    elif [ $availability -ge 70 ]; then
        print_status "WARN" "服务可用率: ${availability}% (${running_services}/${total_services})" "部分服务不可用"
    else
        print_status "FAIL" "服务可用率: ${availability}% (${running_services}/${total_services})" "大量服务不可用"
    fi
}

# 检查系统资源
check_system_resources() {
    echo ""
    echo "💻 检查系统资源..."
    
    # 检查CPU使用率
    if command -v top >/dev/null 2>&1; then
        local cpu_usage=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | awk -F'%' '{print $1}' 2>/dev/null || echo "0")
        if (( $(echo "$cpu_usage > 80" | bc -l 2>/dev/null || echo "0") )); then
            print_status "WARN" "CPU使用率较高: ${cpu_usage}%" "建议检查高CPU进程"
        elif (( $(echo "$cpu_usage > 60" | bc -l 2>/dev/null || echo "0") )); then
            print_status "WARN" "CPU使用率中等: ${cpu_usage}%" "持续关注"
        else
            print_status "OK" "CPU使用率正常: ${cpu_usage}%"
        fi
    fi
    
    # 检查内存使用
    if command -v free >/dev/null 2>&1; then
        local mem_info=$(free -m | grep ^Mem)
        local total_mem=$(echo $mem_info | awk '{print $2}')
        local used_mem=$(echo $mem_info | awk '{print $3}')
        local available_mem=$(echo $mem_info | awk '{print $7}')
        local mem_percent=$(( used_mem * 100 / total_mem ))
        
        if [ $mem_percent -gt 85 ]; then
            print_status "WARN" "内存使用率较高: ${mem_percent}%" "可用内存: ${available_mem}MB"
        else
            print_status "OK" "内存使用正常: ${mem_percent}%" "可用内存: ${available_mem}MB"
        fi
    fi
    
    # 检查磁盘空间
    if command -v df >/dev/null 2>&1; then
        local disk_usage=$(df -h . | awk 'NR==2{print $5}' | sed 's/%//')
        local available_space=$(df -h . | awk 'NR==2{print $4}')
        
        if [ $disk_usage -gt 90 ]; then
            print_status "WARN" "磁盘使用率较高: ${disk_usage}%" "可用空间: $available_space"
        else
            print_status "OK" "磁盘空间充足: ${disk_usage}%" "可用空间: $available_space"
        fi
    fi
    
    # 检查网络连接
    local established_connections=$(netstat -an 2>/dev/null | grep ESTABLISHED | wc -l)
    if [ $established_connections -gt 5000 ]; then
        print_status "WARN" "网络连接数较高: $established_connections" "可能接近系统限制"
    else
        print_status "OK" "网络连接数正常: $established_connections"
    fi
}

# 检查端口占用
check_ports() {
    echo ""
    echo "🔌 检查端口状态..."
    
    local ports=(
        "6379:Redis"
        "27017:MongoDB" 
        "2379:ETCD"
        "4150:NSQ Daemon"
        "4161:NSQ Lookup"
        "8001:Gateway1 TCP"
        "8002:Gateway2 TCP"
        "9001:Gateway1 RPC"
        "7001:Gateway1 Monitor"
        "9010:Center RPC"
        "7010:Center Monitor"
    )
    
    for port_info in "${ports[@]}"; do
        local port=$(echo "$port_info" | cut -d: -f1)
        local service=$(echo "$port_info" | cut -d: -f2)
        
        if netstat -tuln 2>/dev/null | grep -q ":$port " || ss -tuln 2>/dev/null | grep -q ":$port "; then
            print_status "OK" "端口 $port ($service) 正在使用"
        else
            print_status "INFO" "端口 $port ($service) 空闲"
        fi
    done
}

# 检查日志文件
check_logs() {
    echo ""
    echo "📝 检查日志状态..."
    
    if [ ! -d "$LOG_DIR" ]; then
        print_status "WARN" "日志目录不存在" "路径: $LOG_DIR"
        return
    fi
    
    local log_files=$(find "$LOG_DIR" -name "*.log" -type f 2>/dev/null | wc -l)
    print_status "INFO" "日志文件数量: $log_files"
    
    # 检查最近的错误日志
    local error_count=0
    local recent_errors=""
    
    if [ $log_files -gt 0 ]; then
        # 查找最近5分钟的ERROR日志
        recent_errors=$(find "$LOG_DIR" -name "*.log" -type f -exec grep -l "ERROR\|FATAL" {} \; 2>/dev/null | while read log_file; do
            grep -n "ERROR\|FATAL" "$log_file" | tail -5
        done)
        
        if [ -n "$recent_errors" ]; then
            error_count=$(echo "$recent_errors" | wc -l)
            print_status "WARN" "发现最近错误日志: $error_count 条" "运行 'make logs-error' 查看详情"
        else
            print_status "OK" "没有发现最近的错误日志"
        fi
    fi
    
    # 检查日志文件大小
    local large_logs=$(find "$LOG_DIR" -name "*.log" -size +100M 2>/dev/null)
    if [ -n "$large_logs" ]; then
        print_status "WARN" "发现大型日志文件" "建议清理: $(echo "$large_logs" | wc -l) 个文件"
    fi
}

# 检查配置文件
check_config() {
    echo ""
    echo "⚙️  检查配置文件..."
    
    local config_file="$PROJECT_ROOT/config/config.yaml"
    if [ -f "$config_file" ]; then
        print_status "OK" "主配置文件存在"
        
        # 检查配置文件语法（如果有验证工具）
        if command -v yq >/dev/null 2>&1; then
            if yq eval . "$config_file" >/dev/null 2>&1; then
                print_status "OK" "配置文件语法正确"
            else
                print_status "FAIL" "配置文件语法错误" "请检查YAML格式"
            fi
        fi
    else
        print_status "FAIL" "主配置文件不存在" "路径: $config_file"
    fi
    
    # 检查其他配置文件
    local config_files=(
        "monitoring/prometheus.yml:Prometheus配置"
        "monitoring/tribeway_rules.yml:告警规则"
        "locales/en.json:英文语言包"
        "locales/zh-CN.json:中文语言包"
    )
    
    for config_info in "${config_files[@]}"; do
        local file_path=$(echo "$config_info" | cut -d: -f1)
        local file_desc=$(echo "$config_info" | cut -d: -f2)
        
        if [ -f "$PROJECT_ROOT/$file_path" ]; then
            print_status "OK" "$file_desc 存在"
        else
            print_status "WARN" "$file_desc 不存在" "路径: $file_path"
        fi
    done
}

# 检查数据库数据
check_database_health() {
    echo ""
    echo "🗄️  检查数据库健康状态..."
    
    # Redis健康检查
    if redis-cli ping >/dev/null 2>&1; then
        local redis_memory=$(redis-cli info memory | grep used_memory_peak_human | cut -d: -f2 | tr -d '\r')
        local redis_keyspace=$(redis-cli info keyspace | wc -l)
        
        print_status "INFO" "Redis 内存峰值: $redis_memory"
        
        if [ $redis_keyspace -gt 1 ]; then
            print_status "OK" "Redis 包含数据"
        else
            print_status "INFO" "Redis 暂无数据"
        fi
        
        # 检查Redis慢查询
        local slow_queries=$(redis-cli slowlog len)
        if [ "$slow_queries" -gt 10 ]; then
            print_status "WARN" "Redis 慢查询较多: $slow_queries 条" "建议优化查询"
        fi
    fi
    
    # MongoDB健康检查
    if mongo --eval "db.runCommand('ping')" >/dev/null 2>&1; then
        local db_size=$(mongo tribeway_game --eval "db.stats().dataSize" --quiet 2>/dev/null || echo "0")
        local collections=$(mongo tribeway_game --eval "db.getCollectionNames().length" --quiet 2>/dev/null || echo "0")
        
        print_status "INFO" "MongoDB 数据库大小: $(( db_size / 1024 / 1024 ))MB"
        print_status "INFO" "MongoDB 集合数量: $collections"
        
        if [ "$collections" -gt 0 ]; then
            print_status "OK" "MongoDB 包含数据"
        else
            print_status "INFO" "MongoDB 暂无数据"
        fi
    fi
}

# 检查网络连通性
check_network() {
    echo ""
    echo "🌐 检查网络连通性..."
    
    # 检查本地网络接口
    if command -v ip >/dev/null 2>&1; then
        local interfaces=$(ip addr show | grep "inet " | grep -v "127.0.0.1" | wc -l)
        print_status "INFO" "网络接口数量: $interfaces"
    fi
    
    # 检查DNS解析
    if nslookup google.com >/dev/null 2>&1; then
        print_status "OK" "DNS解析正常"
    else
        print_status "WARN" "DNS解析可能有问题"
    fi
    
    # 检查重要端口的监听状态
    local listening_ports=$(netstat -tuln 2>/dev/null | grep LISTEN | wc -l)
    print_status "INFO" "监听端口总数: $listening_ports"
}

# 检查性能指标
check_performance() {
    echo ""
    echo "📈 检查性能指标..."
    
    # 尝试获取游戏服务器指标
    local gateway_metrics=""
    if curl -s http://localhost:7001/api/metrics >/dev/null 2>&1; then
        gateway_metrics=$(curl -s http://localhost:7001/api/metrics 2>/dev/null)
        
        if [ -n "$gateway_metrics" ]; then
            local cpu_percent=$(echo "$gateway_metrics" | jq -r '.system.cpu_percent[0]' 2>/dev/null || echo "0")
            local memory_percent=$(echo "$gateway_metrics" | jq -r '.system.memory_percent' 2>/dev/null || echo "0")
            local goroutines=$(echo "$gateway_metrics" | jq -r '.runtime.goroutines' 2>/dev/null || echo "0")
            
            print_status "OK" "Gateway性能指标可用"
            print_status "INFO" "  CPU: ${cpu_percent}% | 内存: ${memory_percent}% | Goroutines: $goroutines"
            
            # 性能告警检查
            if (( $(echo "$cpu_percent > 80" | bc -l 2>/dev/null || echo "0") )); then
                print_status "WARN" "Gateway CPU使用率过高" "${cpu_percent}%"
            fi
            
            if (( $(echo "$memory_percent > 85" | bc -l 2>/dev/null || echo "0") )); then
                print_status "WARN" "Gateway 内存使用率过高" "${memory_percent}%"
            fi
        fi
    else
        print_status "WARN" "无法获取Gateway性能指标" "服务可能未启动"
    fi
}

# 检查安全状态
check_security() {
    echo ""
    echo "🔒 检查安全状态..."
    
    # 检查防火墙状态
    if command -v ufw >/dev/null 2>&1; then
        local ufw_status=$(ufw status 2>/dev/null | head -1 | awk '{print $2}')
        if [ "$ufw_status" = "active" ]; then
            print_status "OK" "UFW防火墙已启用"
        else
            print_status "WARN" "UFW防火墙未启用" "生产环境建议启用"
        fi
    fi
    
    # 检查文件权限
    local config_perm=$(stat -c %a "$PROJECT_ROOT/config/config.yaml" 2>/dev/null || echo "000")
    if [ "$config_perm" = "644" ] || [ "$config_perm" = "600" ]; then
        print_status "OK" "配置文件权限正常: $config_perm"
    else
        print_status "WARN" "配置文件权限可能过宽: $config_perm" "建议设置为644或600"
    fi
    
    # 检查敏感文件
    if [ -f "$PROJECT_ROOT/.env" ]; then
        local env_perm=$(stat -c %a "$PROJECT_ROOT/.env" 2>/dev/null || echo "000")
        if [ "$env_perm" != "600" ]; then
            print_status "WARN" ".env文件权限不安全: $env_perm" "建议设置为600"
        fi
    fi
}

# 生成健康报告摘要
generate_summary() {
    echo ""
    echo "📋 健康检查摘要"
    echo "========================================"
    
    case "$OVERALL_HEALTH" in
        "healthy")
            echo -e "总体状态: ${GREEN}健康 ✅${NC}"
            ;;
        "unhealthy")
            echo -e "总体状态: ${RED}不健康 ❌${NC}"
            ;;
    esac
    
    echo "失败检查: $FAILED_CHECKS 项"
    echo "警告检查: $WARN_CHECKS 项"
    echo "检查时间: $(date '+%Y-%m-%d %H:%M:%S')"
    
    if [ $FAILED_CHECKS -gt 0 ]; then
        echo ""
        echo -e "${RED}🚨 需要立即处理的问题:${NC}"
        echo "  1. 启动失败的服务"
        echo "  2. 检查错误日志"
        echo "  3. 验证配置文件"
        echo "  4. 确保依赖服务运行正常"
    fi
    
    if [ $WARN_CHECKS -gt 0 ]; then
        echo ""
        echo -e "${YELLOW}⚠️  建议关注的问题:${NC}"
        echo "  1. 监控资源使用情况"
        echo "  2. 优化性能配置" 
        echo "  3. 清理历史日志"
        echo "  4. 检查安全配置"
    fi
    
    echo ""
    echo "🔧 有用的命令:"
    echo "  ./scripts/status.sh          # 查看详细服务状态"
    echo "  ./scripts/start.sh           # 启动所有服务"
    echo "  ./scripts/stop.sh            # 停止所有服务"
    echo "  make logs                    # 查看聚合日志"
    echo "  go run tools/performance_analyzer.go collect  # 性能分析"
}

# 主函数
main() {
    echo "🏥 Tribeway 游戏服务器健康检查"
    echo "========================================"
    echo "检查时间: $(date '+%Y-%m-%d %H:%M:%S')"
    echo "项目路径: $PROJECT_ROOT"
    echo ""
    
    # 执行各项检查
    check_infrastructure
    check_tribeway_services
    check_system_resources
    check_ports
    check_logs
    check_config
    check_database_health
    check_network
    check_security
    
    # 生成摘要
    generate_summary
    
    # 返回适当的退出码
    if [ "$OVERALL_HEALTH" = "healthy" ]; then
        exit 0
    else
        exit 1
    fi
}

# 如果是watch模式，循环执行
if [ "$1" = "watch" ]; then
    while true; do
        clear
        main
        echo ""
        echo "每30秒刷新一次，按Ctrl+C退出..."
        sleep 30
    done
else
    main
fi