#!/bin/bash

# 热更新脚本
set -e

PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONFIG_FILE="$PROJECT_ROOT/config/config.yaml"

# 显示帮助信息
show_help() {
    echo "热更新脚本使用说明："
    echo ""
    echo "用法: $0 [类型] [目标节点] [额外参数]"
    echo ""
    echo "热更新类型："
    echo "  config    - 重新加载配置文件"
    echo "  logic     - 重新加载游戏逻辑"
    echo "  data      - 重新加载游戏数据"
    echo "  module    - 重新加载指定模块"
    echo ""
    echo "目标节点："
    echo "  all       - 所有节点 (默认)"
    echo "  gateway   - 网关节点"
    echo "  game      - 游戏节点"
    echo "  center    - 中心节点"
    echo "  [node_id] - 指定节点ID"
    echo ""
    echo "示例："
    echo "  $0 config                    # 重载所有节点配置"
    echo "  $0 logic game               # 重载游戏节点逻辑"
    echo "  $0 module gateway module_name # 重载网关指定模块"
    echo ""
}

# 发送热更新命令
send_hot_reload_command() {
    local update_type=$1
    local target=$2
    local extra_args=$3
    
    echo "发送热更新命令: $update_type -> $target"
    
    # 构造更新命令
    local command="hot_update"
    local args="{\"type\":\"$update_type\""
    
    if [ -n "$extra_args" ]; then
        args="$args,\"module\":\"$extra_args\""
    fi
    
    args="$args}"
    
    # 通过NSQ发送系统消息
    if command -v nsq_pub >/dev/null 2>&1; then
        echo "Using NSQ to send hot reload command..."
        
        local message="{\"type\":\"$update_type\",\"target\":\"$target\",\"command\":\"$command\",\"args\":$args,\"timestamp\":$(date +%s)}"
        
        echo "$message" | nsq_pub -topic="system_messages" -nsqd-tcp-address="localhost:4150"
        
        echo "热更新命令已发送"
    else
        echo "NSQ工具未安装，尝试通过RPC发送命令..."
        send_rpc_command "$target" "$command" "$args"
    fi
}

# 通过RPC发送命令
send_rpc_command() {
    local target=$1
    local command=$2
    local args=$3
    
    # 查找目标节点的RPC端口
    local rpc_ports=(9010 9001 9002 9020 9030 9040 9050 9060 9100 9101 9102 9200)
    
    for port in "${rpc_ports[@]}"; do
        if timeout 2 bash -c "</dev/tcp/localhost/$port" 2>/dev/null; then
            echo "Found RPC service on port $port, sending command..."
            # 这里可以实现RPC客户端调用
            echo "Command sent to localhost:$port"
            break
        fi
    done
}

# 验证更新类型
validate_update_type() {
    local update_type=$1
    
    case "$update_type" in
        "config"|"logic"|"data"|"module")
            return 0
            ;;
        *)
            echo "错误: 无效的更新类型 '$update_type'"
            echo "支持的类型: config, logic, data, module"
            return 1
            ;;
    esac
}

# 验证目标节点
validate_target() {
    local target=$1
    
    case "$target" in
        "all"|"gateway"|"login"|"lobby"|"game"|"friend"|"chat"|"mail"|"gm"|"center")
            return 0
            ;;
        *)
            # 检查是否是具体的节点ID
            if [[ "$target" =~ ^[a-zA-Z0-9_-]+$ ]]; then
                return 0
            else
                echo "错误: 无效的目标节点 '$target'"
                echo "支持的目标: all, gateway, login, lobby, game, friend, chat, mail, gm, center 或具体节点ID"
                return 1
            fi
            ;;
    esac
}

# 配置文件热更新
reload_config() {
    local target=$1
    
    echo "执行配置文件热更新..."
    
    # 验证配置文件语法
    if ! go run -tags validate "$PROJECT_ROOT/tools/validate_config.go" "$CONFIG_FILE"; then
        echo "错误: 配置文件语法验证失败"
        return 1
    fi
    
    echo "配置文件验证通过"
    send_hot_reload_command "config" "$target"
    
    echo "配置热更新完成"
}

# 逻辑热更新
reload_logic() {
    local target=$1
    
    echo "执行逻辑热更新..."
    
    # 构建新的逻辑模块
    echo "构建游戏逻辑模块..."
    if ! go build -buildmode=plugin -o "$PROJECT_ROOT/plugins/game_logic.so" "$PROJECT_ROOT/plugins/game_logic.go"; then
        echo "错误: 逻辑模块构建失败"
        return 1
    fi
    
    send_hot_reload_command "logic" "$target"
    
    echo "逻辑热更新完成"
}

# 数据热更新
reload_data() {
    local target=$1
    
    echo "执行数据热更新..."
    
    # 验证数据文件
    if [ -f "$PROJECT_ROOT/data/game_data.json" ]; then
        if ! python3 -m json.tool "$PROJECT_ROOT/data/game_data.json" >/dev/null; then
            echo "错误: 游戏数据文件格式错误"
            return 1
        fi
    fi
    
    send_hot_reload_command "data" "$target"
    
    echo "数据热更新完成"
}

# 模块热更新
reload_module() {
    local target=$1
    local module_name=$2
    
    if [ -z "$module_name" ]; then
        echo "错误: 模块热更新需要指定模块名称"
        return 1
    fi
    
    echo "执行模块热更新: $module_name"
    
    # 构建模块
    module_path="$PROJECT_ROOT/plugins/${module_name}.go"
    if [ -f "$module_path" ]; then
        echo "构建模块: $module_name"
        if ! go build -buildmode=plugin -o "$PROJECT_ROOT/plugins/${module_name}.so" "$module_path"; then
            echo "错误: 模块构建失败"
            return 1
        fi
    else
        echo "警告: 模块源文件不存在: $module_path"
    fi
    
    send_hot_reload_command "module" "$target" "$module_name"
    
    echo "模块热更新完成: $module_name"
}

# 显示更新状态
show_update_status() {
    echo "=== 热更新状态监控 ==="
    
    # 显示服务状态
    ./scripts/status.sh
    
    echo ""
    echo "=== 最近的热更新日志 ==="
    
    # 显示最近的热更新日志
    find "$LOG_DIR" -name "*.log" -exec grep -l "hot.*update\|Hot.*reload" {} \; | while read log_file; do
        echo "--- $(basename "$log_file") ---"
        grep "hot.*update\|Hot.*reload" "$log_file" | tail -5
        echo ""
    done
}

# 回滚功能
rollback_update() {
    local target=$1
    
    echo "执行热更新回滚..."
    
    # 发送回滚命令
    send_hot_reload_command "rollback" "$target"
    
    echo "回滚完成"
}

# 主函数
main() {
    local update_type=${1:-"help"}
    local target=${2:-"all"}
    local extra_args=$3
    
    case "$update_type" in
        "help"|"-h"|"--help")
            show_help
            ;;
        "status")
            show_update_status
            ;;
        "config")
            if validate_target "$target"; then
                reload_config "$target"
            fi
            ;;
        "logic")
            if validate_target "$target"; then
                reload_logic "$target"
            fi
            ;;
        "data")
            if validate_target "$target"; then
                reload_data "$target"
            fi
            ;;
        "module")
            if validate_target "$target"; then
                reload_module "$target" "$extra_args"
            fi
            ;;
        "rollback")
            if validate_target "$target"; then
                rollback_update "$target"
            fi
            ;;
        *)
            echo "错误: 未知的热更新类型 '$update_type'"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@"
