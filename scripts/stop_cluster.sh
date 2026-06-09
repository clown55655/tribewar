#!/bin/bash

# Tribeway 集群停止脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

# 优雅停止服务
graceful_stop() {
    print_status "INFO" "执行优雅停止..."
    
    # 首先停止应用服务，给它们时间处理完现有请求
    print_status "INFO" "停止Tribeway应用服务..."
    docker-compose -f docker-compose.cluster.yml stop \
        tribeway-center-cluster \
        tribeway-gateway-cluster-1 \
        tribeway-gateway-cluster-2 \
        nginx-lb
    
    sleep 5
    
    # 停止监控服务
    print_status "INFO" "停止监控服务..."
    docker-compose -f docker-compose.cluster.yml stop \
        prometheus-cluster \
        grafana-cluster
    
    sleep 2
    
    # 停止NSQ集群
    print_status "INFO" "停止NSQ集群..."
    docker-compose -f docker-compose.cluster.yml stop \
        nsqd-1 nsqd-2 nsqd-3 \
        nsqlookupd-1 nsqlookupd-2
    
    sleep 2
    
    # 停止数据库集群（需要更多时间）
    print_status "INFO" "停止MongoDB副本集..."
    docker-compose -f docker-compose.cluster.yml stop \
        mongodb-rs-1 mongodb-rs-2 mongodb-rs-3
    
    print_status "INFO" "停止Redis集群..."
    docker-compose -f docker-compose.cluster.yml stop \
        redis-cluster-1 redis-cluster-2 redis-cluster-3 \
        redis-cluster-4 redis-cluster-5 redis-cluster-6
    
    sleep 3
    
    # 最后停止ETCD集群
    print_status "INFO" "停止ETCD集群..."
    docker-compose -f docker-compose.cluster.yml stop \
        etcd-1 etcd-2 etcd-3
    
    print_status "SUCCESS" "所有服务已优雅停止"
}

# 强制停止服务
force_stop() {
    print_status "WARNING" "执行强制停止..."
    
    # 强制停止所有服务
    docker-compose -f docker-compose.cluster.yml kill
    docker-compose -f docker-compose.cluster.yml down --remove-orphans
    
    # 清理悬空容器
    local orphan_containers=$(docker ps -aq --filter "name=tribeway-")
    if [ -n "$orphan_containers" ]; then
        print_status "INFO" "清理悬空容器..."
        echo "$orphan_containers" | xargs docker rm -f
    fi
    
    print_status "SUCCESS" "强制停止完成"
}

# 清理集群数据
clean_cluster_data() {
    print_status "WARNING" "清理集群数据..."
    
    echo "⚠️  警告: 此操作将删除所有集群数据，包括:"
    echo "  - Redis 集群数据"
    echo "  - MongoDB 副本集数据"
    echo "  - ETCD 集群数据"
    echo "  - 应用程序日志"
    echo ""
    
    if [ "$1" != "--force" ]; then
        read -p "确认删除所有数据？输入 'DELETE' 确认: " confirm
        if [ "$confirm" != "DELETE" ]; then
            print_status "INFO" "操作已取消"
            return 0
        fi
    fi
    
    # 停止所有服务
    docker-compose -f docker-compose.cluster.yml down --remove-orphans --volumes
    
    # 删除Docker volumes
    print_status "INFO" "删除数据卷..."
    docker volume rm -f $(docker volume ls -q --filter "name=tribeway_*") 2>/dev/null || true
    
    # 清理网络
    print_status "INFO" "清理网络..."
    docker network prune -f
    
    # 清理日志文件
    if [ -d "$LOG_DIR" ]; then
        print_status "INFO" "清理日志文件..."
        rm -rf "$LOG_DIR"/*
    fi
    
    print_status "SUCCESS" "集群数据清理完成"
}

# 部分停止（停止指定服务类型）
partial_stop() {
    local service_type=$1
    
    case "$service_type" in
        "app"|"application")
            print_status "INFO" "停止应用服务..."
            docker-compose -f docker-compose.cluster.yml stop \
                tribeway-center-cluster \
                tribeway-gateway-cluster-1 \
                tribeway-gateway-cluster-2 \
                nginx-lb
            ;;
        "db"|"database")
            print_status "INFO" "停止数据库服务..."
            docker-compose -f docker-compose.cluster.yml stop \
                redis-cluster-1 redis-cluster-2 redis-cluster-3 \
                redis-cluster-4 redis-cluster-5 redis-cluster-6 \
                mongodb-rs-1 mongodb-rs-2 mongodb-rs-3
            ;;
        "mq"|"messagequeue")
            print_status "INFO" "停止消息队列..."
            docker-compose -f docker-compose.cluster.yml stop \
                nsqd-1 nsqd-2 nsqd-3 \
                nsqlookupd-1 nsqlookupd-2
            ;;
        "etcd")
            print_status "INFO" "停止ETCD集群..."
            docker-compose -f docker-compose.cluster.yml stop \
                etcd-1 etcd-2 etcd-3
            ;;
        "monitor"|"monitoring")
            print_status "INFO" "停止监控服务..."
            docker-compose -f docker-compose.cluster.yml stop \
                prometheus-cluster \
                grafana-cluster
            ;;
        *)
            print_status "ERROR" "未知服务类型: $service_type"
            echo "支持的类型: app, db, mq, etcd, monitor"
            exit 1
            ;;
    esac
    
    print_status "SUCCESS" "$service_type 服务已停止"
}

# 显示帮助信息
show_help() {
    echo "Tribeway 集群停止脚本"
    echo ""
    echo "用法: $0 [模式] [选项]"
    echo ""
    echo "停止模式:"
    echo "  graceful    优雅停止（默认）"
    echo "  force       强制停止"
    echo "  clean       清理所有数据"
    echo "  partial     部分停止"
    echo ""
    echo "部分停止类型:"
    echo "  app         仅停止应用服务"
    echo "  db          仅停止数据库"
    echo "  mq          仅停止消息队列"
    echo "  etcd        仅停止ETCD"
    echo "  monitor     仅停止监控服务"
    echo ""
    echo "选项:"
    echo "  --force     跳过确认提示"
    echo "  --backup    停止前备份数据"
    echo ""
    echo "示例:"
    echo "  $0                      # 优雅停止所有服务"
    echo "  $0 force --force        # 强制停止"
    echo "  $0 clean --force        # 清理所有数据"
    echo "  $0 partial app          # 仅停止应用服务"
}

# 备份数据
backup_before_stop() {
    if [ "$1" = "--backup" ]; then
        print_status "INFO" "停止前备份数据..."
        
        if [ -f "./scripts/cluster_backup.sh" ]; then
            ./scripts/cluster_backup.sh quick
        else
            print_status "WARNING" "备份脚本不存在，跳过备份"
        fi
    fi
}

# 主执行流程
case "${1:-graceful}" in
    "graceful")
        backup_before_stop "$2"
        graceful_stop
        ;;
    "force")
        backup_before_stop "$2"
        force_stop
        ;;
    "clean")
        force_stop
        clean_cluster_data "$2"
        ;;
    "partial")
        if [ -z "$2" ]; then
            echo "错误: partial模式需要指定服务类型"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
        fi
        partial_stop "$2"
        ;;
    "help")
        show_help
        ;;
    *)
        echo "未知模式: $1"
        echo "使用 '$0 help' 查看帮助信息"
        exit 1
        ;;
esac

print_status "SUCCESS" "集群停止操作完成"
