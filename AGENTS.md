# Repository Guidelines

## 项目结构与模块组织

本仓库是 RayPilot，包含 Go 后端、Vue 前端、部署配置和中文项目文档。

- `cmd/api`、`cmd/worker`、`cmd/seed`、`cmd/node-agent`：可执行程序入口。
- `internal/`：后端核心包，包括 handler、service、repository、model、middleware、scheduler、subscription、config、auth、database、response。
- `migrations/`：SQL 数据库迁移，表结构以 migration 为准，不依赖 GORM 自动迁移。
- `frontend/`：Vue 3 + Vite + Element Plus 前端项目，源码在 `frontend/src`。
- `web/static/`：前端生产构建产物。
- `deploy/nginx/`：Nginx 反向代理配置。
- `文档/`：架构、接口、部署、运维和每日记录。

Go 测试文件与实现文件同目录，命名为 `*_test.go`。前端页面主要位于 `frontend/src/pages/user` 和 `frontend/src/pages/admin`。

## 构建、测试与开发命令

- `make build`：构建 Go 二进制到 `bin/`。
- `make api`：启动 API 服务，即 `go run ./cmd/api`。
- `make worker`：启动后台定时任务。
- `make seed`：运行数据库种子工具。
- `make migrate`：基于 `MIGRATE_DATABASE_URL` 执行 SQL 迁移。
- `make frontend`：启动 Vite 前端开发服务器。
- `make frontend-build`：构建前端产物到 `web/static`。
- `go test ./...`：运行全部后端测试。
- `cd frontend && npm run build`：验证前端生产构建。
- `make up` / `make down`：启动或停止 Docker Compose 服务。
- 当前运行环境使用 `docker-compose` 命令；Makefile 默认 `COMPOSE ?= docker-compose`，如目标环境只支持 Compose v2 子命令，可显式执行 `COMPOSE="docker compose" make up`。

## 编码风格与命名规范

Go 代码必须使用 `gofmt`。包名保持短小、全小写。测试命名优先使用 `Test{功能}_{场景}_{预期}`，例如 `TestAuthService_Register_Success`。

前端使用 Vue 单文件组件、Composition API 和 Element Plus，缩进为 2 个空格；现有 JavaScript 风格不使用分号。接口调用应通过 `frontend/src/api/request.js` 或专门的 API 适配层收口，避免页面内散落原生 `fetch`。

## 测试要求

后端测试使用 Go testing 与 `stretchr/testify`。修改 service、repository、handler、middleware、subscription、scheduler 等核心包时，应补充对应测试。提交前至少运行 `go test ./...`。涉及事务、鉴权、token、流量计费、节点任务处理的改动，需要增加聚焦测试。核心业务路径目标覆盖率不低于 80%。

## Agent 协作注意

修改前先阅读相邻实现和 `文档/代码审查/代码审查报告.md`，按既有分层处理问题：handler 只做参数和响应，service 放业务规则，repository 封装数据库访问。不要覆盖本地 `.env`、构建产物或用户未说明的改动。

排查时优先使用 `rg`、`go test ./...`、`npm run build` 和 `docker-compose config --services`，并在文档中记录无法复现的验证条件。
涉及协议字段、迁移、部署命令或安全策略时，同步更新 `文档/` 中对应说明，保持记录可追踪。
若用户明确允许更新本地配置，修改后必须重新运行 Compose 配置校验。

## 套餐与基础套餐规则

- 系统必须始终存在一个基础套餐，`plans.is_default=true` 表示基础套餐。
- 基础套餐不能删除，只能修改；后端必须强制基础套餐保持启用，前端必须禁用基础套餐删除入口。
- 用户注册或管理员新增用户时，必须同步生成用户唯一订阅 Token，并自动分配基础套餐订阅。
- 删除普通套餐时采用 `plans.is_deleted=true` 逻辑删除，不硬删订单或兑换码历史；仍在使用该套餐的活动订阅必须自动迁移到基础套餐，并触发出口节点用户同步。
- 套餐列表、用户套餐选择、下单和兑换码开通不得使用 `is_deleted=true` 的套餐。

## 节点 Reality 与订阅联调规则

- 涉及 VLESS + Reality 节点、一键部署、订阅生成或节点同步时，必须确认 `nodes.server_name`、`nodes.public_key`、`nodes.short_id` 与节点 `/usr/local/etc/xray/config.json` 中 `realitySettings.serverNames[0]`、`publicKey` 或由 `privateKey` 派生的 PublicKey、`shortIds[0]` 一致。
- 一键部署完成后必须自动读取节点 Xray Reality 参数并写回中心节点记录；如果同步失败，应让部署失败并清理本次创建的节点记录，不能留下可导入但不可用的节点。
- 排查“订阅可导入但节点无信号”时，按顺序检查：订阅 URL 返回 200、节点 443/TCP 可达、Reality `sni/pbk/sid` 是否一致、用户 UUID 是否存在于节点 Xray `clients`、Xray 是否有可用 `outbounds`。
- 修改节点协议字段、Xray 配置模板、node-agent 同步逻辑或订阅输出格式后，除 `go test ./...` 外，还要用真实订阅链接或 Xray 客户端验证节点能出站，并同步更新 `开发方案.md` 与 `文档/`。
- 修改出口节点流量统计、`/api/agent/traffic` 或 node-agent 上报逻辑时，必须保留本地流量队列、`collected_at` 入账和乱序旧批次跳过语义；验证项至少包含 `go test ./...`、node-agent 队列重放测试和真实节点上报状态。
- `nodes.last_traffic_report_at` 表示中心收到流量报告的时间，`nodes.last_traffic_success_at` 表示成功处理到的节点采集时间，不得混用。

## 多出口 IP 与 multi_exit 规则

- 多出口 IP 服务器必须由管理员在一键部署前显式开启“多 IP 服务器”模式；未开启时不得扫描服务器出口 IP，也不得自动创建多个节点。
- 开启多 IP 模式后，必须先通过 SSH 扫描服务器公网 IPv4，并验证 `curl --interface <IP>` 的实际出口等于该 IP；只有管理员手动勾选确认的可用公网 IP 才能创建为逻辑出口节点。
- 多 IP 模式下 `node_hosts` 表示一台物理服务器和唯一 node-agent 身份，`nodes` 表示逻辑出口节点；一个公网出口 IP 对应一条 `nodes` 记录。
- multi_exit 模式只安装一个 `node-agent`，使用 `AGENT_ROLE=multi_exit`、`NODE_HOST_ID`、`NODE_HOST_TOKEN` 和 `MULTI_NODE_CONFIG` 管理同一物理服务器下的多个逻辑节点。
- multi_exit 生成的 Xray 配置必须为每个逻辑节点创建独立 inbound/outbound：`listen` 绑定该节点 IP，`freedom.sendThrough` 也绑定同一 IP，避免双 IP 服务器出站归属漂移。
- multi_exit 对中心仍必须按 `node_id` 分别心跳、领取任务、上报任务结果和用户级流量；中心账本、套餐授权和订阅生成继续以 `nodes.id` 为归属。
- multi_exit 写入 Xray clients 时必须使用节点内部分隔的统计 email，避免同一用户跨多个逻辑节点的 Xray Stats 累计值混在一起；上报中心前再还原为原始 `xray_user_key`。
- 修改多 IP 扫描、`node_hosts`、multi_exit 协议、Xray 多 inbound/outbound 模板或流量归属逻辑时，必须同步更新 `开发方案.md`、节点代理部署指南、管理接口文档和规则文件，并运行 `go test ./...`、前端构建和 Playwright smoke。

## 直连与中转演进规则

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

## 规则文件同步要求

- 本仓库规则文件包括 `CLAUDE.md`、`AGENTS.md`、`QWEN.md`。
- 修改其中任意一个规则文件时，必须同步检查并更新另外两个规则文件，确保关键约束、流程和口径一致。
- 若某条规则只适用于特定工具或 agent，应在三份文件中明确适用范围，避免互相冲突。

## 测试节点信息

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

## 提交与 PR 要求

当前工作区没有可用 Git 历史，因此提交信息建议使用简洁祈使句，例如 `fix subscription token validation` 或 `add redeem code expiry check`。

PR 应包含变更摘要、测试结果、关联任务或问题背景。前端改动需附截图或说明影响页面。涉及数据库迁移、接口契约、部署方式或安全行为的变更，必须在 PR 中明确标注，并同步更新 `文档/`。

## 安全与配置注意事项

不要提交 `.env`、密钥、生成的二进制、覆盖率文件、`frontend/node_modules` 或无意更新的构建产物。新环境从 `.env.example` 开始配置，务必设置强 `JWT_SECRET`。Docker Compose 场景下，`DATABASE_URL` 与 `MIGRATE_DATABASE_URL` 都应指向 Compose 内的 MySQL 服务名，而不是 `127.0.0.1`。
