# RayPilot — 代理订阅系统

## 项目概述

RayPilot 是一套面向 `xray-core` 节点的订阅分发、用户管理和中转管理系统。用户购买套餐后获得订阅链接，通过 Clash/mihomo、Shadowrocket、Surge 等客户端连接到代理节点。节点控制面通过独立部署的 `node-agent` 管理本机 `xray-core` 的用户权限和流量上报。

**完整开发方案：** 详见 `开发方案.md`

### 核心架构

- **数据面**：`xray-core` 负责实际代理流量转发（VLESS + Reality）
- **控制面**：`node-agent` 负责管理本机 xray-core 用户、上报流量和状态
- **中心服务**：对出口节点只和 node-agent exit 模式通信，不直接跨公网调用 xray-core gRPC；中转阶段对接 node-agent relay 模式
- **订阅分发**：HTTPS 直接下载，支持三种格式（Clash YAML、Base64、纯文本 URI）
- **中转能力**：已新增 node-agent relay 模式和 TCP 透传中转层，保留现有直连出口节点
- **流量计费**：快照差值法，避免重复计费

### 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go + Gin + GORM + MySQL 8.0+ |
| 前端 | Vue 3 + Vite + Element Plus + Alova + Pinia + Vue Router 4 |
| 部署 | Docker Compose + Nginx 反代 |
| 鉴权 | JWT（Access Token + Refresh Token） |
| 任务调度 | robfig/cron/v3 + MySQL 任务表 |

### 项目状态

**v1 功能开发完成（8/8 阶段）**，已进入测试与运维阶段；第一版直连与中转并存能力已落地，包含 `/admin/relays`、node-agent relay 模式和 HAProxy 配置下发。

## 项目结构

```
suiyue/
├─ cmd/
│  ├─ api/main.go              # 后端 API 服务入口
│  ├─ worker/main.go           # 后台 Worker 入口
│  ├─ seed/main.go             # 数据库种子工具
│  └─ node-agent/main.go       # 真实节点代理（支持自动安装 xray-core）
├─ internal/
│  ├─ config/                  # 配置加载
│  ├─ handler/                 # HTTP 路由处理器
│  ├─ service/                 # 业务逻辑层
│  ├─ repository/              # 数据访问层
│  ├─ model/                   # 数据库模型
│  ├─ scheduler/               # 定时任务
│  ├─ middleware/              # 中间件
│  ├─ agent/                   # node-agent 通信层
│  ├─ subscription/            # 订阅生成
│  └─ platform/                # 平台基础模块
├─ frontend/                   # Vue 3 前端项目
├─ migrations/                 # 数据库迁移文件
├─ web/static/                 # 前端构建产物
├─ deploy/nginx/               # Nginx 反代配置
├─ 文档/                       # 项目文档
├─ 开发方案.md                 # 完整开发方案 v3.3
├─ QWEN.md                     # 本项目配置
├─ Makefile
├─ Dockerfile
├─ docker-compose.yml
└─ .env.example
```

## 开发规范

### 代码要求

- Go 工程基线：golang-migrate、log/slog、golang-jwt/jwt/v5、bcrypt
- 每个代码文件前 10 行内必须写文件功能简介
- 复杂业务逻辑、事务处理、并发控制等代码必须逐行注释
- 统一错误处理和响应格式

### 测试要求

- 每个新功能的代码必须伴随对应的测试用例
- 写代码后必须立即编写并运行测试，测试通过才算该功能完成
- Service 层优先写单元测试，Repository 层写集成测试（依赖真实数据库）
- Handler 层通过 HTTP 测试验证完整请求响应链路
- 核心业务逻辑的测试覆盖率不得低于 80%
- 测试命名规则：`Test{功能}_{场景}_{预期结果}`，例如 `TestAuthService_Register_Success`
- 每次提交代码前必须确保所有测试通过（`go test ./...`）

### 文档要求

- 文档是正式交付物，代码、测试、文档三者同步完成才算完成
- 每个自然日必须在 `文档/每日记录/` 下新增当天日报（`YYYY-MM-DD-日报.md`）
- 任何影响接口、数据库、部署方式的变更必须同步更新文档
- 文档目录名、文件名统一使用中文

### 套餐与基础套餐规则

- 系统必须始终存在一个基础套餐，`plans.is_default=true` 表示基础套餐。
- 基础套餐不能删除，只能修改；后端必须强制基础套餐保持启用，前端必须禁用基础套餐删除入口。
- 用户注册或管理员新增用户时，必须同步生成用户唯一订阅 Token，并自动分配基础套餐订阅。
- 删除普通套餐时采用 `plans.is_deleted=true` 逻辑删除，不硬删订单或兑换码历史；仍在使用该套餐的活动订阅必须自动迁移到基础套餐，并触发出口节点用户同步。
- 套餐列表、用户套餐选择、下单和兑换码开通不得使用 `is_deleted=true` 的套餐。

### 规则文件同步要求

- 本仓库规则文件包括 `CLAUDE.md`、`AGENTS.md`、`QWEN.md`。
- 修改其中任意一个规则文件时，必须同步检查并更新另外两个规则文件，确保关键约束、流程和口径一致。
- 若某条规则只适用于特定工具或 agent，应在三份文件中明确适用范围，避免互相冲突。

### 节点 Reality 与订阅联调规则

- 涉及 VLESS + Reality 节点、一键部署、订阅生成或节点同步时，必须确认 `nodes.server_name`、`nodes.public_key`、`nodes.short_id` 与节点 `/usr/local/etc/xray/config.json` 中 `realitySettings.serverNames[0]`、`publicKey` 或由 `privateKey` 派生的 PublicKey、`shortIds[0]` 一致。
- 一键部署完成后必须自动读取节点 Xray Reality 参数并写回中心节点记录；如果同步失败，应让部署失败并清理本次创建的节点记录，不能留下可导入但不可用的节点。
- 排查“订阅可导入但节点无信号”时，按顺序检查：订阅 URL 返回 200、节点 443/TCP 可达、Reality `sni/pbk/sid` 是否一致、用户 UUID 是否存在于节点 Xray `clients`、Xray 是否有可用 `outbounds`。
- 修改节点协议字段、Xray 配置模板、node-agent 同步逻辑或订阅输出格式后，除 `go test ./...` 外，还要用真实订阅链接或 Xray 客户端验证节点能出站，并同步更新 `开发方案.md` 与 `文档/`。
- 修改出口节点流量统计、`/api/agent/traffic` 或 node-agent 上报逻辑时，必须保留本地流量队列、`collected_at` 入账和乱序旧批次跳过语义；验证项至少包含 `go test ./...`、node-agent 队列重放测试和真实节点上报状态。
- `nodes.last_traffic_report_at` 表示中心收到流量报告的时间，`nodes.last_traffic_success_at` 表示成功处理到的节点采集时间，不得混用。

### 多出口 IP 与 multi_exit 规则

- 多出口 IP 服务器必须由管理员在一键部署前显式开启“多 IP 服务器”模式；未开启时不得扫描服务器出口 IP，也不得自动创建多个节点。
- 开启多 IP 模式后，必须先通过 SSH 扫描服务器公网 IPv4，并验证 `curl --interface <IP>` 的实际出口等于该 IP；只有管理员手动勾选确认的可用公网 IP 才能创建为逻辑出口节点。
- 多 IP 模式下 `node_hosts` 表示一台物理服务器和唯一 node-agent 身份，`nodes` 表示逻辑出口节点；一个公网出口 IP 对应一条 `nodes` 记录。
- multi_exit 模式只安装一个 `node-agent`，使用 `AGENT_ROLE=multi_exit`、`NODE_HOST_ID`、`NODE_HOST_TOKEN` 和 `MULTI_NODE_CONFIG` 管理同一物理服务器下的多个逻辑节点。
- multi_exit 生成的 Xray 配置必须为每个逻辑节点创建独立 inbound/outbound：`listen` 绑定该节点 IP，`freedom.sendThrough` 也绑定同一 IP，避免双 IP 服务器出站归属漂移。
- multi_exit 对中心仍必须按 `node_id` 分别心跳、领取任务、上报任务结果和用户级流量；中心账本、套餐授权和订阅生成继续以 `nodes.id` 为归属。
- multi_exit 写入 Xray clients 时必须使用节点内部分隔的统计 email，避免同一用户跨多个逻辑节点的 Xray Stats 累计值混在一起；上报中心前再还原为原始 `xray_user_key`。
- 修改多 IP 扫描、`node_hosts`、multi_exit 协议、Xray 多 inbound/outbound 模板或流量归属逻辑时，必须同步更新 `开发方案.md`、节点代理部署指南、管理接口文档和规则文件，并运行 `go test ./...`、前端构建和 Playwright smoke。

### 直连与中转演进规则

- 第一版中转能力已落地：`006_add_relays`、`/admin/relays`、`/api/agent/relay/*`、node-agent `AGENT_ROLE=relay` 和 HAProxy 配置 reload 均为当前实现，不再只是规划。
- 现有 `nodes` 默认是出口节点；新增中转能力时不得破坏直连订阅、node-agent 用户同步和流量账本。
- 中转节点只做 TCP 透传或端口转发，不写入用户 UUID，不做订阅鉴权和流量计费；用户开通、禁用和流量统计仍落在出口节点的 `node-agent`。
- 中转订阅线路使用 `relays.host:relay_backends.listen_port` 作为 `server/port`，但 `uuid/sni/pbk/sid/fingerprint/flow` 必须沿用对应出口节点，不能把中转节点当 Reality 终点。
- 一个中转监听端口默认只能绑定一个出口节点；除非多个出口节点共享完全一致的 Reality 身份，否则不得做同端口多出口负载均衡。
- 一个用户仍只有一个订阅 Token；直连和中转只是同一订阅内容里的多条线路，不新增中转专用用户 Token。
- 用户连接中转不需要额外 Token，VLESS UUID 会被 TCP 透传到出口节点；出口节点按 UUID 做用户鉴权和用户级流量统计。
- 中转节点可以复用 `node-agent` 二进制，但必须以 `AGENT_ROLE=relay` 运行，使用 `RELAY_ID` + `RELAY_TOKEN`，只访问中转心跳、任务和结果接口。
- `RELAY_TOKEN` 只用于中转节点 agent 鉴权，不能用于下载订阅、访问出口节点任务、上报用户级流量或修改 Xray clients。
- node-agent relay 模式通过 HAProxy stats socket 只能上报中转节点、监听端口、后端绑定维度的线路级指标；用户套餐扣量和封禁仍以出口节点按 UUID 上报的数据为准。
- 中转转发组件第一版固定默认 HAProxy；node-agent relay 模式必须先执行 `haproxy -c` 校验，成功后再 reload，失败时保留上一份可用配置。
- 隐藏出口 IP 时，订阅只下发中转线路，并在出口节点防火墙只放行中转服务器 IP。
- 涉及中转数据表、node-agent relay 模式、订阅输出或部署流程时，必须同步更新 `开发方案.md`、`文档/中转/中转设计.md`、接口文档和架构决策。

### 架构约束

- 服务端对出口节点只和 node-agent exit 模式通信，不直接跨公网调用 xray-core gRPC；中转阶段使用 node-agent relay 模式
- 流量统计必须按"快照差值"计算
- node-agent 采用全量主动推送模式（定时上报状态 + 拉取待执行任务）
- v1 不引入 Redis，不使用 BullMQ
- 配置加载固定环境变量 + Go 配置结构体，不引入配置中心
- 所有回复都用中文回复

### 测试节点信息

已有多台真实节点服务器用于联调测试。三份规则文件必须同步维护本节；SSH 密码、节点 Token、订阅 Token、JWT、数据库连接串和 Reality 私钥不得提交到 GitHub，公开仓库只保留 `[REDACTED]`。

**节点 1**

| 项目 | 值 |
|------|-----|
| 节点 IP | `154.219.97.219` |
| SSH 用户 | `root` |
| SSH 密码 | `[REDACTED]` |
| 系统 | Ubuntu 22.04, 2GB RAM, 30GB 磁盘 |
| xray-core | v26.3.27（已安装） |
| xray 配置 | `/usr/local/etc/xray/config.json` |
| 协议 | VLESS + Reality + TCP，端口 443 |
| Reality SNI | `www.microsoft.com` |
| Reality PublicKey | `Ptge2dO56Lr_sBjn1I05SVhxew3mq6tvGN5JxdG3Plg` |
| node-agent | 已部署为 systemd 服务，10 秒心跳 |
| 中心服务地址 | `[REDACTED]` |
| Node Token | `[REDACTED]` |

**节点 2**

| 项目 | 值 |
|------|-----|
| 节点 IP | `154.219.106.105` |
| 出口 IP | `154.219.106.105`, `154.219.106.53` |
| SSH 用户 | `root` |
| SSH 密码 | `[REDACTED]` |
| xray-core | v26.3.27（已安装） |
| xray 配置 | `/usr/local/etc/xray/config.json` |
| 协议 | VLESS + Reality + TCP，端口 443，每个出口 IP 独立 inbound/outbound |
| Reality SNI | `www.microsoft.com` |
| Reality PublicKey | `zCFojnBF8PNYGYWgHWTynGvPVgp-14G9ttU9rxLD7HE` |
| node-agent | `raypilot-node-agent` Docker 容器，`AGENT_ROLE=multi_exit` |
| 逻辑节点 | 当前联调记录为 `33 -> 154.219.106.105`、`34 -> 154.219.106.53` |
| 旧代理 | systemd `node-agent` 与旧 relay agent 已清理 |
| 中心服务地址 | `[REDACTED]` |
| NodeHost Token | `[REDACTED]` |

部署方式：一台多出口服务器只保留一个 multi_exit node-agent；旧 systemd/relay agent 不得与当前出口角色并存。

## 常用命令（开发阶段参考）

```bash
# 后端开发
go run ./cmd/api              # 启动 API 服务
go run ./cmd/worker           # 启动 Worker
go run ./cmd/seed             # 数据库种子工具

# 数据库迁移
migrate -path migrations -database "$DATABASE_URL" up

# 前端开发
cd frontend && npm run dev    # 启动 Vite 开发服务器

# 构建
docker-compose build          # 构建所有服务镜像
docker-compose up -d          # 启动所有服务
```

当前运行环境使用 `docker-compose` 命令；Makefile 默认 `COMPOSE ?= docker-compose`，如目标环境只支持 Compose v2 子命令，可显式执行 `COMPOSE="docker compose" make up`。

## 开发进度

- [x] **第 1 阶段**：项目初始化与文档骨架
- [x] **第 2 阶段**：认证和用户（注册、登录、Token 刷新、用户首页）
- [x] **第 3 阶段**：套餐、节点、后台基础（CRUD + 管理页面）
- [x] **第 4 阶段**：订阅生成与节点授权（三种格式、NodeAccessTask）
- [x] **第 5 阶段**：兑换码（生成、兑换、记录管理）
- [x] **第 6 阶段**：订单与支付骨架（表结构、接口骨架）
- [x] **第 7 阶段**：流量采集（快照差值、配额控制、节点联动）
- [x] **第 8 阶段**：运维、测试与文档收口

## 其他要求
1. 所有回复全部用中文回复
2. 开发过程中不需要反复询问是否继续，用户需要暂停时会主动说
3. **禁止暂停开发**——不允许在未完成时停下来问"是否继续"，必须一直推进直到项目全部完成，除非用户主动说暂停
