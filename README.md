# Tribeway

Tribeway 是一个使用 Go 编写的分布式在线游戏服务器框架。它包含网关、登录、大厅、游戏、好友、聊天、邮件、GM、中心管理、Actor、TCP 帧协议、RPC、服务发现、Redis、MongoDB、NSQ、监控、安全配置、迁移工具、压测工具和 Docker Compose 部署配置。

这个项目更适合被理解为“中小型在线游戏后端框架骨架”和“学习型工程”。它已经具备不少企业级框架需要的基础设施设计，例如组件按需初始化、网络帧安全读取、连接超时、最大包长、密码哈希、密钥外部注入、服务注册发现、健康检查和基础监控；但它还不是一个开箱即用的完整商业游戏后端，业务闭环、完整 Actor 监督树、链路追踪、真实业务压测和跨服治理还需要继续完善。

## 适合的游戏类型

Tribeway 当前最适合“连接长在线、业务强后端、实时性要求中等、房间或大厅驱动”的游戏。

| 类型 | 示例 | 适配原因 |
| --- | --- | --- |
| 回合制、棋牌、桌游 | 斗地主、麻将、德州扑克、象棋、狼人杀、剧本杀房间 | 这类游戏通常以房间为单位推进状态，消息频率可控，Actor 模型适合承载单房间串行状态机。 |
| 轻量房间制竞技 | 1v1 小局、3v3 休闲竞技、台球、飞行棋、大富翁、小游戏大厅 | Gateway 负责接入，Lobby/Game 负责匹配、房间和对局逻辑，比较符合当前服务拆分。 |
| 社交休闲游戏 | 派对游戏、语音房互动、好友聊天、邮件奖励、轻公会玩法 | 项目已经有好友、聊天、邮件、GM、消息队列等模块雏形，适合继续扩展社交链路。 |
| 卡牌、放置、SLG 异步玩法 | 卡牌养成、挂机放置、异步战斗结算、城建收菜、排行榜活动 | 后端重点在账号、存档、邮件、任务、活动、战报和异步结算，对毫秒级同步要求不高。 |
| 中小型私服或教学项目 | 自研小游戏服务器、课程项目、框架学习、原型验证 | 模块边界清晰，便于学习从单进程到多服务的演进方式。 |

需要较大扩展后才适合的游戏：

- MMO / MMORPG：需要 AOI、场景服、世界分片、地图状态同步、跨服迁移、对象可见性管理和更复杂的实体生命周期。
- MOBA / FPS / 动作竞技：通常需要 UDP/KCP、快照同步、状态插值、帧同步或回滚、反作弊、录像回放和更严格的延迟控制。
- 大规模开放世界：需要场景切片、动态负载均衡、跨节点实体迁移、热区治理和更强的状态一致性方案。

不建议直接用当前版本承载高实时物理同步、超大规模 MMO 或强竞技对抗游戏。它可以作为基础设施起点，但核心同步模型和场景架构需要重新设计。

## 架构概览

```text
Client
  |
  | TCP frame
  v
Gateway
  |-- login/register/heartbeat -> Login RPC
  |-- route planned messages    -> Lobby/Game/Friend/Chat/Mail RPC
  |
  +--> Redis        session/cache/rate state
  +--> ETCD         service registry/discovery
  +--> NSQ          async event/system message

Business Services
  |
  | RPC + protobuf
  v
Login / Lobby / Game / Friend / Chat / Mail / GM / Center
  |
  +--> MongoDB      user/room/game/mail/friend/gm persistent data
  +--> ActorSystem  room/game/node local serial state
  +--> Monitoring   metrics/health/pprof/admin api
```

核心思路是把游戏服务器拆成几个层次：

- 接入层：`gateway` 面向客户端 TCP 长连接，负责帧读取、基础消息解析、连接管理、登录转发和后续路由入口。
- 业务层：`login`、`lobby`、`game`、`friend`、`chat`、`mail`、`gm` 等服务通过 RPC 暴露能力。
- 基础设施层：Redis 做缓存和会话，MongoDB 做持久化，ETCD 做注册发现，NSQ 做异步消息和系统广播。
- 状态执行层：Actor 用于承载局部串行状态，例如房间、对局、玩家会话等，减少共享内存锁竞争。
- 运维安全层：监控、健康检查、pprof、GM 白名单、token secret、密码哈希和环境变量密钥注入。

## 目录结构

```text
cmd/                    程序入口，按 -node 启动不同服务
config/                 本地和集群配置
docs/                   架构学习、改造记录和设计说明
examples/               示例客户端
internal/actor/         Actor 系统
internal/database/      Redis、MongoDB 管理器和 Repository
internal/discovery/     ETCD 注册中心和服务发现
internal/gameplay/      游戏玩法基础结构
internal/hotreload/     热更新相关能力
internal/i18n/          国际化基础能力
internal/logger/        日志封装
internal/monitoring/    指标、pprof、健康检查和管理接口
internal/mq/            NSQ 和内部消息代理
internal/network/       TCP 连接、帧读写、超时和最大包长
internal/pool/          通用池化能力
internal/protocol/      协议错误码和协议版本基础
internal/rpc/           RPC Server、Client、连接池、重试和熔断
internal/security/      token、密码、安全校验和密钥读取
internal/server/        Gateway、Login、Lobby、Game、GM 等服务
pkg/proto/              Protobuf 生成代码
proto/                  Protobuf 源文件
scripts/                启动和集群脚本
tools/migrate/          MongoDB 迁移工具
tools/loadtest/         TCP 帧级压测工具
.github/workflows/      CI 配置
```

## 服务节点

统一入口是 `cmd/main.go`，通过 `-node` 指定节点类型。

| node | 说明 | 主要依赖 |
| --- | --- | --- |
| `gateway` | 客户端接入、TCP 消息处理、登录转发、消息路由入口 | Redis、NSQ、ETCD、Actor、RPC |
| `login` | 注册、登录、token、会话 | Redis、MongoDB、NSQ、ETCD、Actor、RPC |
| `lobby` | 大厅和房间相关逻辑 | MongoDB、NSQ、ETCD、RPC |
| `game` | 游戏业务逻辑 | MongoDB、NSQ、ETCD、RPC |
| `enhanced_game` | 增强游戏服务，包含监控、安全、热更新等扩展组件 | NSQ、ETCD、RPC、监控安全配置 |
| `friend` | 好友系统 | MongoDB、NSQ、ETCD、RPC |
| `chat` | 聊天系统 | MongoDB、NSQ、ETCD、RPC |
| `mail` | 邮件系统 | MongoDB、NSQ、ETCD、RPC |
| `gm` | GM 操作、封禁、公告、配置重载 | MongoDB、NSQ、ETCD、RPC、GM 白名单 |
| `center` | 中心管理和服务统计 | NSQ、ETCD、RPC |

## 环境要求

本地开发建议：

- Go 1.21+
- Docker Desktop / Docker Compose
- Redis
- MongoDB
- ETCD
- NSQ

如果只执行 `go test`、`go vet`、`go build`，通常不需要启动 Redis、MongoDB、ETCD、NSQ。需要运行服务时，建议用 Docker Compose 启动依赖。

## 必需环境变量

项目已经移除默认密钥。启动 Docker Compose 或登录相关服务前，至少准备这些变量：

```powershell
$env:TRIBEWAY_MONGODB_PASSWORD="dev-root-password-123456"
$env:TRIBEWAY_MONGODB_APP_PASSWORD="dev-app-password-123456"
$env:TRIBEWAY_TOKEN_SECRET="dev-token-secret-1234567890"
```

如果要访问监控管理接口、GM 接口或集群监控，再补充：

```powershell
$env:TRIBEWAY_MONITORING_ADMIN_TOKEN="dev-monitoring-admin-token"
$env:TRIBEWAY_GM_ADMIN_USER_IDS="1001,1002"
$env:TRIBEWAY_GRAFANA_ADMIN_PASSWORD="dev-grafana-password"
```

可选依赖密码：

```powershell
$env:TRIBEWAY_REDIS_PASSWORD="redis-password"
$env:TRIBEWAY_ETCD_PASSWORD="etcd-password"
```

生产环境请使用随机强密钥，并通过环境变量、容器 Secret、CI/CD Secret 或密钥管理系统注入，不要提交真实 `.env`、数据库密码、token secret、证书私钥或数据目录。

## 本地启动

### 1. 启动基础依赖

只启动本地开发最常用的依赖：

```powershell
docker compose up -d redis mongodb etcd nsqlookupd nsqd
```

查看容器状态：

```powershell
docker compose ps
```

停止依赖：

```powershell
docker compose down
```

### 2. 启动服务进程

可以在多个终端里分别启动不同节点。最小链路通常先启动 `login` 和 `gateway`：

```powershell
go run .\cmd -config=config\config.yaml -node=login -id=login1
```

```powershell
go run .\cmd -config=config\config.yaml -node=gateway -id=gateway1
```

继续启动大厅和游戏服务：

```powershell
go run .\cmd -config=config\config.yaml -node=lobby -id=lobby1
go run .\cmd -config=config\config.yaml -node=game -id=game1
```

启动其他业务服务：

```powershell
go run .\cmd -config=config\config.yaml -node=friend -id=friend1
go run .\cmd -config=config\config.yaml -node=chat -id=chat1
go run .\cmd -config=config\config.yaml -node=mail -id=mail1
go run .\cmd -config=config\config.yaml -node=center -id=center1
```

启动 GM 服务前需要配置管理员用户 ID：

```powershell
$env:TRIBEWAY_GM_ADMIN_USER_IDS="1001"
go run .\cmd -config=config\config.yaml -node=gm -id=gm1
```

### 3. Docker Compose 启动完整本地环境

如果希望直接启动 compose 文件里定义的服务：

```powershell
docker compose up -d
```

查看日志：

```powershell
docker compose logs -f
```

查看某个服务日志：

```powershell
docker compose logs -f tribeway-gateway1
```

## 构建和测试

运行单元测试：

```powershell
go test ./...
```

运行静态检查：

```powershell
go vet ./...
```

构建主程序：

```powershell
go build -o .\bin\tribeway.exe .\cmd
```

构建迁移工具：

```powershell
go build -o .\bin\migrate.exe .\tools\migrate
```

构建压测工具：

```powershell
go build -o .\bin\loadtest.exe .\tools\loadtest
```

运行数据库迁移：

```powershell
go run .\tools\migrate -config=config\config.yaml
```

或：

```powershell
.\bin\migrate.exe -config=config\config.yaml
```

## 冒烟测试方式

### 1. 代码级验证

提交前建议至少执行：

```powershell
go test ./...
go vet ./...
go build -o .\bin\tribeway.exe .\cmd
```

如果修改了迁移或压测工具，再补充：

```powershell
go build -o .\bin\migrate.exe .\tools\migrate
go build -o .\bin\loadtest.exe .\tools\loadtest
```

### 2. 依赖级验证

```powershell
docker compose up -d redis mongodb etcd nsqlookupd nsqd
docker compose ps
```

确认 Redis、MongoDB、ETCD、NSQ 都是 `running` 或 `healthy` 状态。

### 3. 服务启动验证

分别启动 `login` 和 `gateway` 后，确认日志里没有 `FATAL`、`panic`、`failed to init`、`invalid ReadTimeout`、`service register failed` 等错误。

如果启动了 enhanced game 或监控服务，可以访问健康检查：

```powershell
curl http://127.0.0.1:7001/health
```

Prometheus 指标：

```powershell
curl http://127.0.0.1:7001/metrics
```

受保护的管理接口需要 admin token：

```powershell
curl -H "X-Admin-Token: $env:TRIBEWAY_MONITORING_ADMIN_TOKEN" http://127.0.0.1:7001/api/system
curl -H "X-Admin-Token: $env:TRIBEWAY_MONITORING_ADMIN_TOKEN" http://127.0.0.1:7001/api/metrics
curl -H "X-Admin-Token: $env:TRIBEWAY_MONITORING_ADMIN_TOKEN" http://127.0.0.1:7001/debug/pprof/
```

### 4. TCP 帧级压测

压测工具主要验证 TCP 帧写入、连接并发、最大包长和超时处理，不等同于完整业务压测。

```powershell
go run .\tools\loadtest -addr=127.0.0.1:8001 -connections=10 -requests=100 -payload=128 -timeout=5s
```

构建后运行：

```powershell
.\bin\loadtest.exe -addr=127.0.0.1:8001 -connections=10 -requests=100 -payload=128 -timeout=5s
```

完整业务压测还需要构造真实链路，例如注册、登录、进入大厅、创建房间、加入房间、游戏操作、聊天、邮件领取、断线重连等。

## 关键配置

主配置文件：

- `config/config.yaml`：本地和单机开发配置。
- `config/cluster.yaml`：集群配置。

网络配置重点：

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

- `tcp_port` 是客户端 TCP 接入端口。
- `rpc_port` 是服务间 RPC 端口。
- `http_port` 是 HTTP 监控或管理端口。
- `max_packet_size` 限制 TCP/RPC 最大帧大小。
- `read_timeout` 和 `write_timeout` 用于处理慢连接和异常连接。
- `0.0.0.0` 可以作为监听地址，但不应该作为 ETCD 服务发现的注册地址。需要跨机器访问时设置 `TRIBEWAY_ADVERTISE_ADDRESS`。

监控安全配置：

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

GM 权限配置：

```yaml
security:
  gm:
    admin_user_ids: []
    admin_user_ids_env: "TRIBEWAY_GM_ADMIN_USER_IDS"
```

## 推荐阅读顺序

如果你想从零学习这个项目，建议按一条请求链路来读，而不是一开始就记所有文件。

1. `cmd/main.go`：理解命令行参数、配置文件、节点类型和启动入口。
2. `internal/server/server.go`：理解 `BaseServer`、生命周期、服务注册、启动和停止。
3. `internal/server/component_options.go`：理解不同服务需要哪些基础组件。
4. `internal/server/component_factory.go`：理解组件工厂如何创建 Redis、MongoDB、NSQ、ETCD、RPC、Actor。
5. `internal/server/gateway_server.go`：理解客户端消息如何进入网关。
6. `internal/server/login_server.go`：理解注册、登录、密码哈希和 token 生成。
7. `internal/network/frame.go`：理解 TCP 帧协议、`io.ReadFull`、最大包长和超时。
8. `internal/rpc/rpc.go`：理解服务间 RPC、连接池、重试和熔断。
9. `internal/actor/actor.go`：理解 Actor、mailbox、串行处理和背压。
10. `internal/database/*.go`：理解 Repository、Redis/MongoDB 管理器和数据访问方式。
11. `internal/mq/nsq.go`、`internal/discovery/etcd.go`：理解异步消息和服务发现。
12. `internal/monitoring/monitor.go`、`internal/security/security.go`：理解监控、安全和运维入口。

## 当前限制和后续优化方向

当前版本仍然建议继续完善这些点：

- Gateway 到 Lobby/Game/Friend/Chat/Mail 的完整业务路由还需要继续补齐。
- Actor 系统还缺完整监督树、失败重启策略、死信队列、跨节点定位和迁移能力。
- RPC 还可以继续增强 tracing、context cancel 传播、接口生成、幂等重试约束和更细粒度指标。
- 数据写入需要逐步接入事务、幂等流水、审计闭环和失败补偿。
- 业务压测需要从帧级压测升级为真实玩家行为模型。
- 监控可以进一步接入 OpenTelemetry、Prometheus/Grafana 仪表盘和告警规则。
- 配置热更新需要明确哪些配置可热更、哪些必须重启，避免运行时状态不一致。
