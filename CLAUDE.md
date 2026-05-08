# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

RayPilot 是一套面向 xray-core 节点的订阅分发、用户管理和中转管理系统。用户购买套餐后获得订阅链接，通过 Clash/mihomo、Shadowrocket、Surge 等客户端连接代理节点。节点控制面通过独立部署的 node-agent 管理本机 xray-core 的用户权限和流量上报。

**当前进度：v1 功能开发完成（8/8 阶段）**，已进入测试与运维阶段；第一版直连与中转并存能力已落地，包含 `/admin/relays`、node-agent relay 模式和 HAProxy 配置下发。

## 常用命令

### 开发

```bash
cp .env.example .env                    # 首次配置
docker-compose up -d mysql              # 启动 MySQL
make migrate                            # 执行数据库迁移
make api                                # 启动 API 服务（端口 3000）
make worker                             # 启动定时任务 Worker
make seed                               # 创建初始管理员（admin/admin123456）
make frontend                           # 启动 Vue 3 开发服务器
```

### 测试

```bash
go test ./...                           # 运行所有测试
go test ./internal/service/... -v       # 运行指定包测试
```

**要求：每完成一项工作（无论功能开发还是 bug 修复），必须立即编写对应测试并运行通过，然后才能进行下一项工作。最终提交前 `go test ./...` 必须全部通过，测试覆盖率不得低于 80%。**

### 构建与部署

```bash
make build                              # 构建二进制到 bin/
make docker                             # docker-compose build
make up                                 # docker-compose up -d
make down                               # docker-compose down
```

本仓库当前运行环境使用 `docker-compose` 命令；Makefile 默认 `COMPOSE ?= docker-compose`，如目标环境只支持 Compose v2 子命令，可显式执行 `COMPOSE="docker compose" make up`。

## 架构概览

### 后端分层（Clean Architecture）

```
HTTP Request → Handler → Service → Repository → MySQL
```

- **Handler 层**（`internal/handler/`）：解析请求参数，调用 Service，写响应。零业务逻辑。
- **Service 层**（`internal/service/`）：业务逻辑，通过 Repository 访问数据，返回 AppError。
- **Repository 层**（`internal/repository/repository.go`）：全部 12 个 Repository 类型在单个文件中，封装 GORM 操作。
- **Model 层**（`internal/model/model.go`）：GORM 模型定义 + 请求/响应结构体。

### 五个可执行程序入口

| 入口 | 路径 | 用途 |
|------|------|------|
| API | `cmd/api/main.go` | Gin HTTP 服务，端口 3000 |
| Worker | `cmd/worker/main.go` | robfig/cron 定时任务 |
| Seed | `cmd/seed/main.go` | 数据库种子（创建管理员） |
| Node Agent | `cmd/node-agent/main.go` | 真实 node-agent，管理本机 xray-core（支持自动检测和安装） |
| Node Agent Mock | `cmd/node-agent-mock/main.go` | 开发用节点代理模拟 |

### 前端

Vue 3 + Vite + Element Plus + Alova（HTTP）+ Pinia（状态）+ Vue Router 4。

- 用户路由：`/login`, `/register`, `/`, `/subscription`, `/orders`, `/plans`, `/redeem`, `/profile`
- 管理后台路由：`/admin/login`, `/admin`, `/admin/plans`, `/admin/node-groups`, `/admin/nodes`, `/admin/relays`, `/admin/users`, `/admin/orders`, `/admin/redeem-codes`, `/admin/subscription-tokens`
- 路由守卫：`requiresAuth`, `requiresAdmin`, `guest` meta 标记通过 `beforeEach` 执行

### 鉴权机制

JWT 双 Token：
- **Access Token**：放在 `Authorization: Bearer` 请求头，默认 24h 过期
- **Refresh Token**：放在 HttpOnly Cookie（`refresh_token`），默认 7d 过期

### 数据库（当前核心表）

`users` → `plans` → `node_groups` → `plan_node_groups` → `nodes` → `user_subscriptions` → `subscription_tokens` → `refresh_tokens` → `orders` → `payment_records` → `traffic_snapshots` → `node_access_tasks` → `usage_ledgers` → `redeem_codes`

迁移文件：`migrations/*.up.sql`（golang-migrate 工具，当前包含基础表、订阅 Token、节点分组多对多、中转和基础套餐迁移）

中转能力新增 `relays`、`relay_backends`、`relay_config_tasks`、`relay_traffic_snapshots`，并给 `nodes` 增加 `line_mode`。

### 核心机制

- **订阅生成**：Clash YAML 为主格式，Base64 和纯文本 URI 由其派生
- **流量计费**：快照差值法 — 每次上报存储累计值，与上次快照求差得到增量
- **节点通信**：拉取模式 — 节点 agent 轮询心跳，服务器返回待执行任务
- **任务管理**：MySQL 任务表 + 乐观锁（lock_token），无 Redis
- **中转能力**：已新增 node-agent relay 模式和 TCP 透传中转层，直连出口节点保持不变
- **基础套餐**：系统必须保留一个 `plans.is_default=true` 的基础套餐，注册/管理员新增用户自动分配，基础套餐不能删除

## 配置

仅通过环境变量加载（`internal/config/config.go`），无配置文件或配置中心。关键变量见 `.env.example`。

## 开发规范

### 代码

- 每个代码文件前 10 行内写功能简介
- 统一错误处理：使用 `platform/response.AppError`，所有 API 返回 `{success, message, code, data}` 格式
- Handler 不写业务逻辑，只做参数解析、Service 调用、响应写入
- Service 层事务操作返回 AppError
- v1 不引入 Redis

### 测试

- Service 优先写单元测试，Repository 写集成测试（真实数据库），Handler 写 HTTP 端到端测试
- 测试命名：`Test{功能}_{场景}_{预期结果}`
- 写代码后立即编写并运行测试，通过后才算完成
- 每次提交前确保 `go test ./...` 全部通过

### 文档

- 代码、测试、文档三者同步
- 任何影响接口、数据库、部署方式的变更必须更新文档
- 文档目录/文件名使用中文

### 套餐与基础套餐规则

- 系统必须始终存在一个基础套餐，`plans.is_default=true` 表示基础套餐。
- 基础套餐不能删除，只能修改；后端必须强制基础套餐保持启用，前端必须禁用基础套餐删除入口。
- 用户注册或管理员新增用户时，必须同步生成用户唯一订阅 Token，并自动分配基础套餐订阅。
- 删除普通套餐时采用 `plans.is_deleted=true` 逻辑删除，不硬删订单或兑换码历史；仍在使用该套餐的活动订阅必须自动迁移到基础套餐，并触发出口节点用户同步。
- 套餐列表、用户套餐选择、下单和兑换码开通不得使用 `is_deleted=true` 的套餐。
- 套餐流量采用固定双池：`normal`（普通流量）与 `residential`（家宽流量）。`plans.traffic_limit` / `user_subscriptions.traffic_limit` / `user_subscriptions.used_traffic` 继续表示普通流量兼容字段，家宽流量使用独立字段维护。
- 订阅超额判断必须按流量池分别处理：普通流量耗尽只影响普通节点，家宽流量耗尽只影响家宽节点，不能混扣，也不能把单池耗尽视为整个订阅不可用。
- 兑换码、管理员改订阅、基础套餐迁移和注册自动分配基础套餐时，必须同时维护普通流量和家宽流量字段；未显式设置家宽流量时默认 `0`。

### 双流量池规则

- 出口节点必须声明流量池归属：`nodes.traffic_pool` 取值固定为 `normal` 或 `residential`，默认 `normal`。
- 普通节点按本机出口 IP 建模；家宽节点按上游代理账号建模。`nodes.outbound_type=direct` 表示普通直连出口，`nodes.outbound_type=socks5` 表示家宽上游代理出口。
- `nodes.outbound_ip` 语义按出站方式区分：直连节点表示该逻辑节点的真实本机出口 IP，并写入 Xray `freedom.sendThrough`；SOCKS5 家宽节点表示连接上游代理时使用的本机源 IP，并写入 Xray `socks.sendThrough`，不代表上游家宽最终出口 IP。
- 家宽代理节点一条 `nodes` 记录只允许绑定一个上游 SOCKS5 账号；用户前台看到的是多条独立节点和多条订阅线路，而不是一个节点后面自动轮询多个家宽账号。
- 如果管理员一次导入多条 SOCKS5，上层必须拆成多条逻辑 `nodes`，而不是把多条 SOCKS5 塞进一条节点记录。
- `/api/agent/traffic` 与 `/api/agent/multi/traffic` 处理流量时，必须先读取节点 `traffic_pool`，再把增量流量记入对应订阅流量池。
- `usage_ledgers` 必须记录流量池归属，便于区分普通流量和家宽流量账本。
- 订阅生成时必须按节点流量池过滤：某个流量池剩余为 0 时，该池节点和对应中转线路不得继续出现在订阅里；另一个池有剩余时仍可继续下发。
- 节点用户同步必须支持按流量池下发。普通流量超额只能对普通池节点下发 `DISABLE_USER`，家宽流量超额只能对家宽池节点下发 `DISABLE_USER`。
- 后台和用户侧展示必须同时展示普通流量与家宽流量；旧字段继续展示普通流量，新增结构用于展示完整双池信息。

### 规则文件同步要求

- 本仓库规则文件包括 `CLAUDE.md`、`AGENTS.md`、`QWEN.md`。
- 修改其中任意一个规则文件时，必须同步检查并更新另外两个规则文件，确保关键约束、流程和口径一致。
- 若某条规则只适用于特定工具或 agent，应在三份文件中明确适用范围，避免互相冲突。

### 日志中心与审计规则

- 日志中心 v1 已落地 `/admin/logs`、`/api/admin/logs/runtime`、`/api/admin/logs/deployments`、`/api/admin/logs/operations`，不是规划功能。
- 运行日志只读取宿主机 `logs/api.log` 与 `logs/worker.log`，后台接口必须限制最大返回行数并只允许管理员访问。
- Docker Compose 部署必须把宿主机 `./logs` 挂载到 API/Worker 容器 `/app/logs`，并确保 API 写入 `api.log`、Worker 写入 `worker.log`；日志文件尚未创建时，运行日志接口应返回空列表，不得 500。
- 操作日志必须记录用户注册、登录、退出、资料修改、密码修改、下单、兑换码兑换、订阅下载，以及管理员新增/删除/禁用用户、重置密码、修改订阅、生成兑换码等关键动作。
- 部署日志必须记录一键部署出口节点和中转节点的结果、耗时、步骤明细、目标服务器 IP、操作者 IP、逻辑节点/中转/后端记录 ID。
- 所有结构化日志必须记录可用 IP 信息：`client_ip` 或 `operator_ip`，并尽量保留 `X-Forwarded-For`、`X-Real-IP`、`User-Agent` 以便排障。
- 日志中不得写入密码、完整 Token、JWT、数据库连接串、SSH 私钥、Reality 私钥；部署请求只能保存脱敏摘要，例如 `node_token_provided` / `relay_token_provided` 布尔值。
- 修改日志表结构、日志记录入口、日志页面或一键部署日志摘要时，必须同步更新 `开发方案.md`、管理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建和 Playwright smoke。

### 节点 Reality 与订阅联调规则

- 涉及 VLESS + Reality 节点、一键部署、订阅生成或节点同步时，必须确认 `nodes.server_name`、`nodes.public_key`、`nodes.short_id` 与节点 `/usr/local/etc/xray/config.json` 中 `realitySettings.serverNames[0]`、`publicKey` 或由 `privateKey` 派生的 PublicKey、`shortIds[0]` 一致。
- 一键部署完成后必须自动读取节点 Xray Reality 参数并写回中心节点记录；如果同步失败，应让部署失败并清理本次创建的节点记录，不能留下可导入但不可用的节点。
- 排查“订阅可导入但节点无信号”时，按顺序检查：订阅 URL 返回 200、节点 443/TCP 可达、Reality `sni/pbk/sid` 是否一致、用户 UUID 是否存在于节点 Xray `clients`、Xray 是否有可用 `outbounds`。
- 修改节点协议字段、Xray 配置模板、node-agent 同步逻辑或订阅输出格式后，除 `go test ./...` 外，还要用真实订阅链接或 Xray 客户端验证节点能出站，并同步更新 `开发方案.md` 与 `文档/`。
- 修改出口节点流量统计、`/api/agent/traffic` 或 node-agent 上报逻辑时，必须保留本地流量队列、`collected_at` 入账和乱序旧批次跳过语义；验证项至少包含 `go test ./...`、node-agent 队列重放测试和真实节点上报状态。
- `nodes.last_traffic_report_at` 表示中心收到流量报告的时间，`nodes.last_traffic_success_at` 表示成功处理到的节点采集时间，不得混用。
- `nodes.protocol` 表示 VLESS 等协议，`nodes.transport` 表示传输层；当前默认 `tcp`，可选 `xhttp`。
- XHTTP 节点仍使用 VLESS + Reality，但必须清空 `flow`，不得给 Xray clients 或订阅写入 `xtls-rprx-vision`。
- XHTTP 参数由 `nodes.xhttp_path`、`nodes.xhttp_host`、`nodes.xhttp_mode` 管理；订阅输出必须包含 `network/type=xhttp` 和 XHTTP 参数。
- 节点的 `traffic_pool` 与协议、传输层独立；同一物理服务器可同时部署普通池和家宽池逻辑节点。
- 节点的 `outbound_type` 与 `traffic_pool` 独立；同一物理服务器可同时托管普通 IP 型节点和 SOCKS5 上游型家宽节点。
- 管理后台新增节点和一键部署允许多选传输模式；单选时仍创建一条 `nodes`，多选时按每种传输模式创建一条逻辑 `nodes` 线路。
- 同一 IP 同时选择 TCP 与 XHTTP 时必须使用不同端口；默认 TCP 443、XHTTP 8443，不能在同一个 Xray inbound 上混用两种 network。
- 修改 XHTTP 字段、订阅格式、Xray `xhttpSettings` 或 node-agent 用户同步时，必须同步更新三份规则文件、`开发方案.md` 和相关接口/部署文档，并运行后端测试、前端构建和 Playwright smoke。
- 修改双流量池字段、节点流量池归属、`/api/agent/traffic` 扣量逻辑、订阅过滤或套餐展示时，必须同步更新三份规则文件、`开发方案.md`、接口文档、节点代理文档和运维手册，并运行后端测试、前端构建和 Playwright smoke。

### 多出口 IP 与 multi_exit 规则

- 多出口 IP 服务器必须由管理员在一键部署前显式开启“多 IP 服务器”模式；未开启时不得扫描服务器出口 IP，也不得自动创建多个节点。
- 开启多 IP 模式后，必须先通过 SSH 扫描服务器公网 IPv4，并验证 `curl --interface <IP>` 的实际出口等于该 IP；只有管理员手动勾选确认的可用公网 IP 才能创建为逻辑出口节点。
- 多 IP 模式下 `node_hosts` 表示一台物理服务器和唯一 node-agent 身份，`nodes` 表示逻辑出口节点；一个公网出口 IP 对应一条 `nodes` 记录。
- 管理后台出口节点列表必须按节点服务器聚合展示：外层一行表示一台物理服务器，显示管理 IP、全部相关 IP、普通/家宽线路数量、SOCKS5 数量、TCP/XHTTP 数量；展开或编辑服务器时再展示具体 `nodes` 逻辑线路。新增家宽或普通线路时应优先绑定同一 `node_host_id`，而不是创建另一个物理服务器身份。
- multi_exit 模式只安装一个 `node-agent`，使用 `AGENT_ROLE=multi_exit`、`NODE_HOST_ID`、`NODE_HOST_TOKEN` 和 `MULTI_NODE_CONFIG` 管理同一物理服务器下的多个逻辑节点。
- 即使不是多出口 IP，只要一键部署选择了多个传输模式，也必须按多逻辑节点处理，并在目标服务器只运行一个 multi_exit node-agent。
- multi_exit 生成的 Xray 配置必须为每个逻辑节点创建独立 inbound/outbound：普通节点使用 `freedom.sendThrough` 绑定本机出口 IP；家宽代理节点使用 `socks` outbound 指向唯一 `outbound_proxy_url`，如果设置了 `nodes.outbound_ip`，还必须把它作为 `socks.sendThrough`。
- multi_exit 对中心仍必须按 `node_id` 分别心跳、领取任务、上报任务结果和用户级流量；中心账本、套餐授权和订阅生成继续以 `nodes.id` 为归属。
- multi_exit 写入 Xray clients 时必须使用节点内部分隔的统计 email，避免同一用户跨多个逻辑节点的 Xray Stats 累计值混在一起；上报中心前再还原为原始 `xray_user_key`。
- 出口节点一键部署必须是真一键：部署成功后自动绑定管理员选择的节点分组、触发已有活跃订阅用户同步；选择替换旧角色时必须自动停用同服务器旧 relay 和旧出口记录，不能依赖人工再去后台补分组或停旧线路。
- 修改多 IP 扫描、`node_hosts`、multi_exit 协议、Xray 多 inbound/outbound 模板或流量归属逻辑时，必须同步更新 `开发方案.md`、节点代理部署指南、管理接口文档和规则文件，并运行 `go test ./...`、前端构建和 Playwright smoke。

### 直连与中转演进规则

- 第一版中转能力已落地：`006_add_relays`、`/admin/relays`、`/api/agent/relay/*`、node-agent `AGENT_ROLE=relay` 和 HAProxy 配置 reload 均为当前实现，不再只是规划。
- 现有 `nodes` 默认是出口节点；新增中转能力时不得破坏直连订阅、node-agent 用户同步和流量账本。
- 中转节点只做 TCP 透传或端口转发，不写入用户 UUID，不做订阅鉴权和流量计费；用户开通、禁用和流量统计仍落在出口节点的 `node-agent`。
- 中转订阅线路使用 `relays.host:relay_backends.listen_port` 作为 `server/port`，但 `uuid/sni/pbk/sid/fingerprint/flow` 必须沿用对应出口节点，不能把中转节点当 Reality 终点。
- 中转后端只能绑定启用中的出口节点；保存 `relay_backends` 时必须拒绝停用出口，生成 `RELOAD_CONFIG` 时也必须再次过滤停用或不存在的出口节点，避免 HAProxy 继续转发到已改作管理系统或已下线的服务器。
- 一个中转监听端口默认只能绑定一个出口节点；除非多个出口节点共享完全一致的 Reality 身份，否则不得做同端口多出口负载均衡。
- 一个用户仍只有一个订阅 Token；直连和中转只是同一订阅内容里的多条线路，不新增中转专用用户 Token。
- 用户连接中转不需要额外 Token，VLESS UUID 会被 TCP 透传到出口节点；出口节点按 UUID 做用户鉴权和用户级流量统计。
- 中转节点可以复用 `node-agent` 二进制，但必须以 `AGENT_ROLE=relay` 运行，使用 `RELAY_ID` + `RELAY_TOKEN`，只访问中转心跳、任务和结果接口。
- `RELAY_TOKEN` 只用于中转节点 agent 鉴权，不能用于下载订阅、访问出口节点任务、上报用户级流量或修改 Xray clients。
- node-agent relay 模式通过 HAProxy stats socket 只能上报中转节点、监听端口、后端绑定维度的线路级指标；用户套餐扣量和封禁仍以出口节点按 UUID 上报的数据为准。
- 中转转发组件第一版固定默认 HAProxy；node-agent relay 模式必须先执行 `haproxy -c` 校验，成功后再 reload，失败时保留上一份可用配置。
- 中转节点一键部署必须是真一键：管理员选择出口节点和监听端口后，部署流程必须自动创建 `relay_backends`、下发并等待 HAProxy reload 成功；选择替换旧角色时必须自动停用同服务器旧出口记录，不能把“部署 agent 成功但未绑定后端”视为成功。
- 隐藏出口 IP 时，订阅只下发中转线路，并在出口节点防火墙只放行中转服务器 IP。
- 涉及中转数据表、node-agent relay 模式、订阅输出或部署流程时，必须同步更新 `开发方案.md`、`文档/中转/中转设计.md`、接口文档和架构决策。

### Agent 中心地址容灾规则

- node-agent 必须支持多个中心入口：`CENTER_SERVER_URL` 保留为主入口，`CENTER_SERVER_URLS` 为逗号、空格或换行分隔的完整 http/https 地址列表；新部署必须同时写入两者。
- node-agent 对所有中心请求必须从当前可用入口开始尝试，失败后轮询备用入口；某个入口请求成功后必须记住为当前 active center。
- 一键部署出口节点和中转节点必须提供“备用中心地址”输入，并把主中心和备用中心归一化写入 agent 容器环境变量；当前控制面主域名为 `leiyunai.fun`，备用 IP 为 `154.219.106.105`、`154.219.106.53`，后端在任一入口出现时自动补齐另外两个入口。
- 控制平台迁移或域名/IP 不可用时，优先依赖 agent 多中心自动切换；最后兜底才由管理员通过后台“修复中心”发起中心 SSH 到节点机器，重建 Docker agent 或更新 systemd drop-in 并重启 agent。
- SSH 兜底接口为 `POST /api/admin/nodes/repair-center`，可指定 `node_id`、`node_host_id` 或 `relay_id` 等待新心跳确认；该接口只能保存脱敏部署日志，不得记录 SSH 密码、完整 Token 或私钥。
- 修改 `CENTER_SERVER_URLS`、一键部署中心地址、SSH 兜底修复脚本或 agent 中心切换逻辑时，必须同步更新三份规则文件、`开发方案.md`、节点代理部署指南、管理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建、Compose 配置校验和 Playwright smoke。

### 协作

- 所有回复用中文
- 不需要反复询问是否继续，用户要暂停时会主动说
- 禁止在未完成时停下来问"是否继续"，必须一直推进直到项目全部完成

## 已知待修复问题（审查发现）

以下问题在代码审查中被发现，全部已修复：

1. ~~兑换码接口 `/api/redeem` 未挂 JWTAuth 中间件~~ — **已修复**
2. ~~JWT 默认密钥应改为启动时 panic~~ — **已修复**
3. ~~`migrations/001_init.up.sql` 缺少 `refresh_tokens` 表创建~~ — **已修复**
4. ~~前端 `Plans.vue` 缺少 `ElMessageBox` 导入~~ — **已修复**
5. ~~Logout/RefreshToken 不轮转、不使旧 token 失效~~ — **已修复**
6. ~~Worker 过期订阅扫描不创建 DISABLE_USER 任务~~ — **已修复**
7. ~~`ProcessTrafficReport` 和任务创建等错误被静默丢弃~~ — **已修复**：所有 `_ =` 改为 `log.Printf` 记录
8. ~~docker-compose.yml 硬编码数据库密码且暴露 3306 端口~~ — **已修复**：移除默认密码回退，3306 默认不暴露

### 测试节点信息

已有多台真实节点服务器用于联调测试。三份规则文件必须同步维护本节；SSH 密码、节点 Token、订阅 Token、JWT、数据库连接串和 Reality 私钥不得提交到 GitHub，公开仓库只保留 `[REDACTED]`。

`154.219.106.105` 与 `154.219.106.53` 当前作为 RayPilot 管理系统入口和备用入口使用，不再作为测试节点服务器；不得对这台管理系统服务器执行节点清理、node-agent 部署或 Xray 改动，除非用户明确要求维护管理系统本身。

**中转节点：154.219.97.219**

| 项目 | 值 |
|------|-----|
| 节点 IP | `154.219.97.219` |
| SSH 用户 | `root` |
| SSH 密码 | `[REDACTED]` |
| 系统 | Ubuntu 22.04, 2GB RAM, 30GB 磁盘 |
| xray-core | v26.3.27（已安装） |
| xray 配置 | `/usr/local/etc/xray/config.json` |
| 当前角色 | 中转节点物理机（原出口已停用） |
| 协议 | VLESS + Reality + TCP，原端口 443 |
| Reality SNI | `www.microsoft.com` |
| Reality PublicKey | `Ptge2dO56Lr_sBjn1I05SVhxew3mq6tvGN5JxdG3Plg` |
| node-agent | `raypilot-relay-agent` Docker 容器，`AGENT_ROLE=relay` |
| relay 记录 | `55` |
| 监听端口 | `24443 -> 156.238.231.16:443` |
| 中心服务地址 | `[REDACTED]` |
| Relay Token | `[REDACTED]` |

**出口节点：156.238.231.16**

| 项目 | 值 |
|------|-----|
| 节点 IP | `156.238.231.16` |
| SSH 用户 | `root` |
| SSH 密码 | `[REDACTED]` |
| 系统 | Ubuntu 22.04 |
| 角色 | 出口节点 |
| 节点记录 | `105` |
| node-agent | `raypilot-node-agent` Docker 容器，单出口模式 |
| 转发组件 | Xray 26.3.27 |
| 监听端口 | `443/TCP` |
| Reality SNI | `www.microsoft.com` |
| Reality PublicKey | `ZyjLrHt4dl3mig1vqxvFT6un5UL12gwZQhQbguIUm08` |
| 中心服务地址 | `[REDACTED]` |
| Node Token | `[REDACTED]` |

部署方式：一台多出口服务器只保留一个 multi_exit node-agent；旧 systemd/relay agent 不得与当前出口角色并存。
