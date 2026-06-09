# Tribeway 游戏服务器框架 Makefile

.PHONY: help build run test clean docker dev-deps proto format lint

# 变量定义
BINARY_NAME=tribeway
BINARY_PATH=./bin/$(BINARY_NAME)
MAIN_PATH=./cmd/main.go
DOCKER_IMAGE=tribeway-game-server
VERSION?=latest

# 默认目标
help: ## 显示帮助信息
	@echo "Tribeway 游戏服务器框架"
	@echo ""
	@echo "可用命令:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# 构建相关
build: ## 构建二进制文件
	@echo "构建 $(BINARY_NAME)..."
	@go build -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "构建完成: $(BINARY_PATH)"

build-linux: ## 构建 Linux 二进制文件
	@echo "构建 Linux 版本..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_PATH)-linux $(MAIN_PATH)
	@echo "Linux 构建完成: $(BINARY_PATH)-linux"

build-windows: ## 构建 Windows 二进制文件
	@echo "构建 Windows 版本..."
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(BINARY_PATH)-windows.exe $(MAIN_PATH)
	@echo "Windows 构建完成: $(BINARY_PATH)-windows.exe"

build-mac: ## 构建 macOS 二进制文件
	@echo "构建 macOS 版本..."
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $(BINARY_PATH)-darwin $(MAIN_PATH)
	@echo "macOS 构建完成: $(BINARY_PATH)-darwin"

build-all: build-linux build-windows build-mac ## 构建所有平台二进制文件

# 运行相关
run: ## 运行网关服务器
	@go run $(MAIN_PATH) -config=config/config.yaml -node=gateway -id=gateway1

run-center: ## 运行中心服务器
	@go run $(MAIN_PATH) -config=config/config.yaml -node=center -id=center1

run-login: ## 运行登录服务器
	@go run $(MAIN_PATH) -config=config/config.yaml -node=login -id=login1

# 集群管理
start: ## 启动完整服务器集群
	@echo "启动服务器集群..."
	@chmod +x scripts/*.sh
	@./scripts/start.sh

stop: ## 停止服务器集群
	@echo "停止服务器集群..."
	@./scripts/stop.sh

status: ## 查看服务器状态
	@./scripts/status.sh

restart: stop start ## 重启服务器集群

# 开发工具
dev-deps: ## 安装开发依赖
	@echo "安装开发工具..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

proto: ## 生成 Protobuf 文件
	@echo "生成 Protobuf 代码..."
	@mkdir -p pkg/proto
	@protoc -I proto --go_out=pkg/proto --go_opt=paths=source_relative proto/*.proto
	@echo "Protobuf 代码生成完成"

format: ## 格式化代码
	@echo "格式化代码..."
	@go fmt ./...
	@goimports -w .
	@echo "代码格式化完成"

lint: ## 运行代码检查
	@echo "运行代码检查..."
	@golangci-lint run

# 测试相关
test: ## 运行测试
	@echo "运行测试..."
	@go test -v ./...

test-cover: ## 运行测试并生成覆盖率报告
	@echo "运行测试并生成覆盖率报告..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告生成: coverage.html"

benchmark: ## 运行性能测试
	@echo "运行性能测试..."
	@go test -bench=. -benchmem ./...

# Docker 相关
docker-build: ## 构建 Docker 镜像
	@echo "构建 Docker 镜像..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Docker 镜像构建完成: $(DOCKER_IMAGE):$(VERSION)"

docker-run: ## 运行 Docker 容器
	@echo "运行 Docker 容器..."
	@docker run -d --name tribeway-server -p 8001:8001 -p 9001:9001 $(DOCKER_IMAGE):$(VERSION)

docker-compose-up: ## 启动单机 Docker Compose 集群
	@echo "启动单机 Docker Compose 集群..."
	@docker-compose up -d
	@echo "单机集群启动完成"

docker-compose-down: ## 停止单机 Docker Compose 集群
	@echo "停止单机 Docker Compose 集群..."
	@docker-compose down
	@echo "单机集群已停止"

docker-compose-logs: ## 查看单机集群日志
	@docker-compose logs -f

# 集群相关
cluster-build: ## 构建集群镜像
	@echo "构建集群镜像..."
	@docker build -t tribeway-cluster:latest .
	@docker build -f Dockerfile.cluster-init -t tribeway-cluster-init:latest .
	@echo "集群镜像构建完成"

cluster-up: ## 启动完整集群
	@echo "启动Tribeway集群..."
	@chmod +x scripts/*.sh
	@./scripts/start_cluster.sh full --with-monitoring

cluster-down: ## 停止集群
	@echo "停止Tribeway集群..."
	@./scripts/stop_cluster.sh graceful

cluster-status: ## 查看集群状态
	@./scripts/cluster_status.sh

cluster-watch: ## 实时监控集群
	@./scripts/cluster_status.sh watch

cluster-quick: ## 快速启动集群（开发用）
	@echo "快速启动集群..."
	@./scripts/start_cluster.sh quick

cluster-clean: ## 清理集群数据
	@echo "清理集群数据..."
	@./scripts/stop_cluster.sh clean --force

cluster-restart: cluster-down cluster-up ## 重启集群

cluster-logs: ## 查看集群日志
	@docker-compose -f docker-compose.cluster.yml logs -f

cluster-backup: ## 备份集群数据
	@echo "备份集群数据..."
	@./scripts/cluster_backup.sh

cluster-restore: ## 恢复集群数据
	@echo "恢复集群数据..."
	@./scripts/cluster_restore.sh

# 清理相关
clean: ## 清理构建文件
	@echo "清理构建文件..."
	@rm -f $(BINARY_PATH)*
	@rm -f coverage.out coverage.html
	@rm -rf logs/*.log
	@rm -rf logs/*.pid
	@echo "清理完成"

clean-docker: ## 清理 Docker 资源
	@echo "清理 Docker 资源..."
	@docker system prune -f
	@docker volume prune -f

# 数据库相关
db-init: ## 初始化数据库
	@echo "初始化数据库..."
	@echo "创建 MongoDB 索引..."
	@mongo tribeway_game --eval "db.users.createIndex({user_id: 1}, {unique: true})"
	@mongo tribeway_game --eval "db.users.createIndex({username: 1}, {unique: true})"
	@echo "数据库初始化完成"

db-migrate: ## 运行数据库迁移
	@echo "运行数据库迁移..."
	# TODO: 实现数据库迁移逻辑

db-backup: ## 备份数据库
	@echo "备份数据库..."
	@mkdir -p backup
	@mongodump --db tribeway_game --out ./backup/mongodb_$(shell date +%Y%m%d_%H%M%S)
	@redis-cli --rdb ./backup/redis_$(shell date +%Y%m%d_%H%M%S).rdb
	@echo "数据库备份完成"

# 监控相关
monitor: ## 启动监控面板
	@echo "启动监控服务..."
	@echo "Redis Commander: http://localhost:8081"
	@echo "Mongo Express: http://localhost:8082"
	@echo "NSQ Admin: http://localhost:4171"

# 压力测试
load-test: ## 运行负载测试
	@echo "运行负载测试..."
	# TODO: 实现负载测试脚本

# 部署相关
deploy-dev: ## 部署到开发环境
	@echo "部署到开发环境..."
	@$(MAKE) build-linux
	@scp $(BINARY_PATH)-linux dev-server:/opt/tribeway/
	@scp config/config.yaml dev-server:/opt/tribeway/config/
	@ssh dev-server "systemctl restart tribeway-server"

deploy-prod: ## 部署到生产环境
	@echo "部署到生产环境..."
	@echo "请确认是否要部署到生产环境 [y/N]:"
	@read confirm && [ "$$confirm" = "y" ] || exit 1
	@$(MAKE) build-linux
	# TODO: 实现生产环境部署逻辑

# 工具相关
install: build ## 安装到系统
	@echo "安装 $(BINARY_NAME) 到系统..."
	@sudo cp $(BINARY_PATH) /usr/local/bin/
	@echo "安装完成"

uninstall: ## 从系统卸载
	@echo "从系统卸载 $(BINARY_NAME)..."
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "卸载完成"

# 版本信息
version: ## 显示版本信息
	@echo "Tribeway 游戏服务器框架 $(VERSION)"
	@echo "Go 版本: $(shell go version)"
	@echo "构建时间: $(shell date)"

# 依赖管理
deps: ## 下载依赖
	@echo "下载依赖..."
	@go mod download
	@go mod tidy

deps-update: ## 更新依赖
	@echo "更新依赖..."
	@go get -u ./...
	@go mod tidy

# 代码生成
generate: proto ## 运行代码生成
	@echo "运行代码生成..."
	@go generate ./...

# 安全检查
security: ## 运行安全检查
	@echo "运行安全检查..."
	@go mod verify
	@gosec ./...

# 文档生成
docs: ## 生成文档
	@echo "生成文档..."
	@godoc -http=:6060 &
	@echo "文档服务启动: http://localhost:6060"
