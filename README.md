# Tribeway

Tribeway 是一个使用 Go 编写的分布式在线游戏服务器框架雏形。它包含网关、登录、大厅、游戏、好友、聊天、邮件、GM、中心管理、Actor、RPC、TCP 帧协议、服务发现、Redis、MongoDB、NSQ、监控、安全配置、迁移工具、压测工具和 Docker Compose 部署配置。

这个项目目前更适合作为“可以继续演进的中小型在线游戏后端框架”和学习型工程。它已经补齐了多项 P0/P1 基础能力，但还没有达到完整企业级游戏服务器框架的终态：关键业务闭环、完整链路追踪、完整 Actor 监督树、业务级幂等事务接入、真实业务压测基线仍需要继续完善。

## 当前状态

最近验证结果：

```powershell
go test ./...
go vet ./...
go build -o .\bin\tribeway.exe .\cmd
go build -o .\bin\migrate.exe .\tools\migrate
go build -o .\bin\loadtest.exe .\tools\loadtest
```

以上命令当前均已通过。

已完成的基础改造：

- TCP/RPC 帧读取使用 `io.ReadFull`，避免半包读取错误。
- 网络读写加入超时、最大包长和慢连接处理。
- 登录密码从 MD5 改为 bcrypt，并支持旧 MD5 密码登录后迁移。
- token secret、数据库密码、ETCD 密码、Grafana 密码等改为从环境变量或 Secret 注入。
- 移除默认密码和默认密钥，生产环境不再依赖硬编码敏感信息。
- 监控管理接口和 pprof 支持绑定地址、来源网段和 admin token 访问控制。
- GM 操作加入管理员白名单校验，未配置管理员时默认拒绝 GM 操作。
- 服务注册地址不再注册 `0.0.0.0`，支持显式 `advertise_address`。
- Actor 增加 mailbox 背压拒绝统计和运行指标汇总。
- RPC 增加基础重试、熔断、连接池健康检查和统一错误码基础。
- 健康检查区分 `live`、`ready`、`dependency`。
- 新增审计/幂等 repository、MongoDB migration runner、TCP 帧级压测工具和 SLO 模板。
- CI 已覆盖测试、vet、主程序构建和工具构建。

仍需继续完善的边界：

- 聊天真实转发、邮件奖励发放、热更新真实执行等业务 TODO 仍需补齐。
- ActorSystem 还缺完整监督树、失败重启、死信队列和跨节点 location。
- RPC 还需要全量指标、取消传播、请求链路追踪，并逐步减少反射调用约定。
- 数据写入需要逐个业务接入事务、幂等流水和审计闭环。
- OpenTelemetry trace 还没有贯穿完整请求链路。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 语言 | Go |
| 网络协议 | TCP Frame、RPC、Protobuf |
| 服务发现 | ETCD |
| 缓存 | Redis |
| 持久化 | MongoDB |
| 消息队列 | NSQ |
| 监控 | Prometheus 风格指标、HTTP 管理 API、pprof |
| 部署 | Docker Compose、集群脚本 |
| 安全 | bcrypt、环境变量密钥、GM 白名单、监控 token |

## 项目结构

```text
cmd/                    程序入口，按 node 类型启动不同服务
config/                 单机和集群配置
examples/               示例客户端
internal/actor/         Actor 系统
internal/database/      Redis、MongoDB 管理器和 Repository
internal/discovery/     ETCD 注册中心和服务发现
internal/gameplay/      游戏玩法相关基础结构
internal/hotreload/     热更新相关能力
internal/i18n/          国际化基础能力
internal/logger/        日志封装
internal/monitoring/    指标、pprof、健康检查和管理接口
internal/mq/            NSQ 和内部消息代理
internal/network/       TCP 连接、帧读写、超时和最大包长
internal/pool/          通用池化能力
internal/protocol/      协议错误码和协议版本基础
internal/rpc/           RPC Server、Client、连接池、重试和熔断
internal/security/      token、密码、安全校验、密钥读取
internal/server/        Gateway、Login、Lobby、Game、GM 等服务
pkg/proto/              Protobuf 生成代码
proto/                  Protobuf 源文件
scripts/                启动和集群脚本
tools/migrate/          MongoDB 迁移工具
tools/loadtest/         TCP 帧级压测工具
.github/workflows/      CI 配置
```

## 服务节点

启动入口是 `cmd/main.go`，通过 `-node` 指定节点类型。

| node | 说明 | 主要依赖 |
| --- | --- | --- |
| `gateway` | 客户端接入、TCP 消息处理、消息路由 | Redis、NSQ、ETCD、Actor、RPC |
| `login` | 注册、登录、token、会话 | Redis、MongoDB、NSQ、ETCD、Actor、RPC |
| `lobby` | 大厅和房间相关逻辑 | MongoDB、NSQ、ETCD、RPC |
| `game` | 游戏业务逻辑 | MongoDB、NSQ、ETCD、RPC |
| `enhanced_game` | 增强游戏服务，包含安全、监控、热更新组件 | NSQ、ETCD、RPC、监控安全配置 |
| `friend` | 好友系统 | MongoDB、NSQ、ETCD、RPC |
| `chat` | 聊天系统 | MongoDB、NSQ、ETCD、RPC |
| `mail` | 邮件系统 | MongoDB、NSQ、ETCD、RPC |
| `gm` | GM 操作、封禁、公告、配置重载 | MongoDB、NSQ、ETCD、RPC、GM 白名单 |
| `center` | 中心管理和服务统计 | NSQ、ETCD、RPC |

## 快速学习路线

建议按下面顺序阅读。每一步都先看结构，再看一条请求如何流动。

1. `cmd/main.go`
   先理解命令行参数、配置文件、节点类型和不同 server 的创建入口。

2. `internal/server/server.go`
   重点看 `BaseServer`、组件选项、配置加载、组件初始化、RPC 注册、服务注册和启动/停止流程。这里是整个框架的骨架。

3. `internal/server/*_server.go`
   选择一个具体服务读，例如 `gateway_server.go` 或 `login_server.go`。对照 `BaseServer` 看每个服务真正需要哪些组件。

4. `internal/network/frame.go`、`internal/network/tcp_server.go`
   学 TCP 帧协议、包头、最大包长、`io.ReadFull`、读写超时、慢连接处理和连接生命周期。

5. `internal/rpc/rpc.go`
   学 RPC 请求/响应封装、方法注册、客户端调用、连接池、重试、熔断和错误码。

6. `internal/actor/actor.go`
   学 ActorSystem、ActorRef、mailbox、串行处理、背压、统计和当前缺失的监督能力。

7. `internal/database/*.go`
   学 Repository 写法、MongoDB/Redis 管理器、用户/邮件/GM/审计/幂等数据结构。

8. `internal/mq/nsq.go`、`internal/discovery/etcd.go`
   学服务之间如何通过 MQ 和服务发现协作。

9. `internal/monitoring/monitor.go`、`internal/security/security.go`
   学监控、pprof、健康检查、安全配置、密码和 token 处理。

10. `proto/common.proto`、`pkg/proto/common.pb.go`
    学协议定义和 Go 代码之间的关系。修改 proto 后需要重新生成并验证兼容性。

## 建议断点

如果你想用 GoLand 从 0 跟一遍完整启动流程，可以从这些位置打断点：

- `cmd/main.go`：程序入口和节点选择。
- `NewBaseServerWithOptions`：创建基础服务。
- `BaseServer.initComponents`：按组件选项初始化 Redis、MongoDB、NSQ、RPC、Actor 等。
- `BaseServer.Start`：启动网络监听、服务注册和后台任务。
- `network.ReadFrameWithOptions`：读取 TCP/RPC 帧。
- `network.WriteFrameWithOptions`：写回 TCP/RPC 帧。
- `RPCClient.CallWithOptions`：发起 RPC 调用。
- `RPCServer.handleRequest`：服务端处理 RPC 请求。
- `ActorSystem.Tell`：投递 Actor 消息。
- `BaseActor.run`：Actor mailbox 串行消费。
- `LoginService.Register`、`LoginService.Login`：注册和登录主流程。
- `GatewayMessageHandler.HandleMessage`：客户端消息进入网关后的处理入口。

## 模块学习目标

### BaseServer

你需要理解三个问题：

- 一个 server 节点应该依赖哪些组件。
- `BaseServer` 如何通过组件选项避免所有服务都初始化所有依赖。
- 服务启动时配置、网络、RPC、MQ、服务发现、监控之间的顺序关系。

学习重点是“框架骨架”和“组件解耦”。例如 gateway 不需要 MongoDB 时，就不应该强制初始化 `mongoManager`。

### Network

你需要理解：

- TCP 是字节流，不天然保留消息边界。
- 为什么必须用固定包头或长度字段恢复消息边界。
- 为什么帧读取必须使用 `io.ReadFull`。
- 为什么必须限制最大包长，避免恶意大包或内存放大。
- 为什么读写超时能处理慢连接和半开连接。

### RPC

你需要理解：

- RPC 本质上是“请求 envelope + 方法名 + 参数 + 响应 envelope”。
- 连接池减少频繁建连开销。
- 重试只能用于可重试、幂等或明确安全的调用。
- 熔断用于保护调用方和被调用方。
- 反射注册方便，但企业级框架通常需要更强的接口约束、指标和追踪。

### Actor

你需要理解：

- Actor 用 mailbox 保证单 Actor 内部串行处理。
- Actor 之间通过消息交互，减少共享内存锁竞争。
- mailbox 满时必须有背压策略。
- 企业级 Actor 系统还需要监督树、重启策略、死信、location、迁移和可观测性。

### Database

你需要理解：

- Manager 负责连接生命周期。
- Repository 负责某类业务数据访问。
- 业务写入需要事务、幂等、审计和失败补偿。
- 当前项目已有部分基础设施，但还没有把所有业务写入完整接入。

### Security

你需要理解：

- 密码不应该用 MD5 存储，bcrypt/argon2id 更适合密码哈希。
- token secret 不应该硬编码在源码中。
- 默认密码和默认密钥会让测试配置泄漏到生产。
- GM、监控、pprof 都属于高危入口，必须有访问控制。

### Operations

你需要理解：

- `live` 用来判断进程是否活着。
- `ready` 用来判断服务是否准备好接流量。
- `dependency` 用来判断 Redis、MongoDB、ETCD、NSQ 等依赖是否健康。
- migration、loadtest、SLO、CI 是工程可运营能力的一部分。

## 环境要求

本地开发建议：

- Go 1.21+
- Redis
- MongoDB
- ETCD
- NSQ
- Docker 和 Docker Compose，可选

如果只执行编译、单元测试和静态检查，不需要启动 Redis、MongoDB、ETCD、NSQ。

## 必需环境变量

项目已经移除默认密码和默认密钥。启动涉及认证或外部依赖的节点前，请按实际环境设置变量。

### 认证和安全

```powershell
$env:TRIBEWAY_TOKEN_SECRET="replace-with-a-long-random-token-secret"
$env:TRIBEWAY_ENCRYPTION_KEY="replace-with-32-byte-encryption-key"
$env:TRIBEWAY_JWT_SECRET="replace-with-a-long-random-jwt-secret"
```

### 监控管理接口

```powershell
$env:TRIBEWAY_MONITORING_ADMIN_TOKEN="replace-with-a-long-random-admin-token"
```

访问受保护接口时需要携带：

```bash
curl -H "X-Admin-Token: $TRIBEWAY_MONITORING_ADMIN_TOKEN" http://127.0.0.1:7001/api/system
```

### GM 管理员

```powershell
$env:TRIBEWAY_GM_ADMIN_USER_IDS="1001,1002"
```

未配置 `TRIBEWAY_GM_ADMIN_USER_IDS` 或 `security.gm.admin_user_ids` 时，GM 操作默认拒绝。

### 数据库和基础设施密码

按实际启用的依赖配置：

```powershell
$env:TRIBEWAY_REDIS_PASSWORD="redis-password"
$env:TRIBEWAY_MONGODB_PASSWORD="mongodb-password"
$env:TRIBEWAY_MONGODB_APP_PASSWORD="mongodb-app-password"
$env:TRIBEWAY_ETCD_PASSWORD="etcd-password"
$env:TRIBEWAY_GRAFANA_ADMIN_PASSWORD="grafana-password"
```

如果对应服务没有启用密码，可以不设置相应变量，但生产环境不建议无密码运行。

## 核心配置

主配置文件：

- `config/config.yaml`：单机和本地开发配置。
- `config/cluster.yaml`：集群配置。

### 网络配置

```yaml
network:
  tcp_port: 8001
  rpc_port: 9001
  http_port: 7001
  advertise_address: ""
  advertise_address_env: "TRIBEWAY_ADVERTISE_ADDRESS"
  max_connections: 10000
  read_timeout: 30
  write_timeout: 30
  max_packet_size: 1048576
```

说明：

- `0.0.0.0` 可以作为监听地址，但不能作为服务发现注册地址。
- 服务注册地址选择顺序是环境变量、配置项、自动选择非 loopback IPv4、回退到 `127.0.0.1`。
- `max_packet_size` 用于限制 TCP/RPC 最大帧大小。

### 监控安全配置

```yaml
security:
  monitoring:
    bind_address: "127.0.0.1"
    admin_token_env: "TRIBEWAY_MONITORING_ADMIN_TOKEN"
    allowed_cidrs:
      - "127.0.0.1/32"
      - "::1/128"
    protect_metrics_endpoint: false
```

受保护接口：

- `/debug/pprof/*`
- `/api/metrics`
- `/api/alerts`
- `/api/system`
- 独立 pprof 端口：`http_port + 1000`

### GM 权限配置

```yaml
security:
  gm:
    admin_user_ids: []
    admin_user_ids_env: "TRIBEWAY_GM_ADMIN_USER_IDS"
```

管理员 ID 可以写在配置中，也可以通过环境变量注入。生产和容器环境更建议使用环境变量或 Secret。

## 构建和测试

格式化：

```powershell
gofmt -w internal cmd pkg tools
```

单元测试：

```powershell
go test ./...
```

静态检查：

```powershell
go vet ./...
```

构建主程序：

```powershell
go build -o .\bin\tribeway.exe .\cmd
```

构建迁移工具和压测工具：

```powershell
go build -o .\bin\migrate.exe .\tools\migrate
go build -o .\bin\loadtest.exe .\tools\loadtest
```

Linux/macOS：

```bash
go build -o ./bin/tribeway ./cmd
```

## 本地启动示例

先确保 Redis、MongoDB、ETCD、NSQ 已经按 `config/config.yaml` 配置启动。

启动网关：

```powershell
go run .\cmd -config=config\config.yaml -node=gateway -id=gateway-1
```

启动登录服：

```powershell
go run .\cmd -config=config\config.yaml -node=login -id=login-1
```

启动游戏服：

```powershell
go run .\cmd -config=config\config.yaml -node=game -id=game-1
```

启动 GM 服务：

```powershell
$env:TRIBEWAY_GM_ADMIN_USER_IDS="1001"
go run .\cmd -config=config\config.yaml -node=gm -id=gm-1
```

启动增强游戏服前，建议设置监控和安全变量：

```powershell
$env:TRIBEWAY_ENCRYPTION_KEY="replace-with-32-byte-encryption-key"
$env:TRIBEWAY_JWT_SECRET="replace-with-a-long-random-jwt-secret"
$env:TRIBEWAY_MONITORING_ADMIN_TOKEN="replace-with-a-long-random-admin-token"
go run .\cmd -config=config\config.yaml -node=enhanced_game -id=game-enhanced-1
```

## Docker Compose

项目包含：

- `docker-compose.yml`
- `docker-compose.cluster.yml`

这些配置已经移除默认密码，启动前必须提供相关环境变量。示例：

```powershell
$env:TRIBEWAY_MONGODB_ROOT_PASSWORD="mongodb-root-password"
$env:TRIBEWAY_MONGODB_APP_PASSWORD="mongodb-app-password"
$env:TRIBEWAY_GRAFANA_ADMIN_PASSWORD="grafana-password"
$env:TRIBEWAY_TOKEN_SECRET="replace-with-token-secret"
$env:TRIBEWAY_MONITORING_ADMIN_TOKEN="replace-with-monitoring-token"
docker compose up -d
```

如果 compose 中使用了 `${VAR:?required}`，变量缺失时 Docker 会拒绝启动，这是预期行为。

## 监控和 pprof

默认监控绑定到：

```text
127.0.0.1:http_port
```

常用接口：

- `GET /health/live`
- `GET /health/ready`
- `GET /health/dependency`
- `GET /api/metrics`
- `GET /api/system`
- `GET /debug/pprof/`

生产环境建议：

- 只绑定内网或本机地址。
- 为管理接口配置 `TRIBEWAY_MONITORING_ADMIN_TOKEN`。
- 使用反向代理、VPN、堡垒机或安全网关限制访问来源。
- 对 pprof 保持默认关闭公网访问。

## 迁移工具

迁移入口：

```powershell
go run .\tools\migrate -config=config\config.yaml
```

或构建后运行：

```powershell
.\bin\migrate.exe -config=config\config.yaml
```

迁移逻辑位于 `tools/migrate/`，用于管理 MongoDB 结构和索引演进。

## 压测工具

TCP 帧级压测入口：

```powershell
go run .\tools\loadtest -addr=127.0.0.1:8001 -connections=100 -requests=1000
```

压测工具主要用于验证网络层帧读写、最大包长、超时和连接处理，不等价于完整业务压测。完整业务压测还需要构造真实登录、匹配、游戏、聊天、邮件等业务链路。

## 阅读建议

学习这个项目时，不要一开始就试图记住所有文件。更有效的方式是选一条链路反复跟：

- 启动链路：`cmd/main.go` -> `BaseServer` -> 组件初始化 -> 服务注册 -> 网络监听。
- 登录链路：客户端请求 -> gateway -> login RPC -> 用户 repository -> token 返回。
- RPC 链路：client call -> frame write -> server read -> method dispatch -> response write。
- Actor 链路：`Tell` 投递 -> mailbox 入队 -> actor goroutine 串行消费。
- 运维链路：启动 -> 注册 -> 健康检查 -> 指标 -> pprof -> CI。

读完这些链路，再回头看模块边界、错误处理、配置注入和测试覆盖，会更容易判断这个框架距离企业级还差什么。
