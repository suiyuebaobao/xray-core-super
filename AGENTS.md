# AGENTS.md

本文件是 RayPilot 仓库唯一规则文件。原其他规则文件的有效项已整合到本文，后续不再维护多份规则，也不再要求跨规则文件同步。

## 项目概述

RayPilot 是一套面向 `xray-core` 节点的订阅分发、用户管理和中转管理系统。用户购买套餐后获得订阅链接，通过 Clash/mihomo、Shadowrocket、Surge 等客户端连接代理节点。节点控制面通过独立部署的 `node-agent` 管理本机 `xray-core` 的用户权限、流量上报、健康状态和配置任务。

完整开发方案详见 `开发方案.md`。当前状态为 v1 功能开发完成，已进入测试与运维阶段；第一版直连与中转并存能力已落地，包含 `/admin/relays`、node-agent relay 模式和 HAProxy 配置下发。

## 核心架构

- 数据面：`xray-core` 负责实际代理流量转发，主要协议为 VLESS + Reality，传输层支持 TCP 与 XHTTP。
- 控制面：`node-agent` 负责管理本机 `xray-core` 用户、Xray/HAProxy 配置、流量上报、状态上报和任务执行。
- 中心服务：对出口节点只和 node-agent exit/multi_exit 模式通信，不直接跨公网调用 xray-core gRPC；中转阶段对接 node-agent relay 模式。
- 订阅分发：HTTPS 下载订阅，唯一入口为 `/sub/{token}`，只下发 Clash/mihomo YAML；`/sub/{token}/clash`、`/sub/{token}/base64` 与 `/sub/{token}/plain` 等带后缀入口全部下线。
- 中转能力：node-agent relay 模式和 TCP 透传中转层已落地，直连出口节点继续保留。
- 流量计费：快照差值法，每次上报存储累计值，与上次快照求差得到增量，避免重复计费。
- 任务管理：MySQL 任务表 + 乐观锁 `lock_token`，v1 不引入 Redis 或 BullMQ。

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go + Gin + GORM + MySQL 8.0+ |
| 前端 | Vue 3 + Vite + Element Plus + Alova + Pinia + Vue Router 4 |
| 部署 | Docker Compose + Nginx 反代 |
| 鉴权 | JWT Access Token + Refresh Token |
| 任务调度 | robfig/cron/v3 + MySQL 任务表 |
| 节点代理 | xray-core + node-agent，relay 模式默认 HAProxy |

## 项目结构与模块组织

本仓库包含 Go 后端、Vue 前端、部署配置和中文项目文档。

- `cmd/api`：Gin HTTP API 服务入口，默认端口 3000。
- `cmd/worker`：后台定时任务 Worker 入口。
- `cmd/seed`：数据库种子工具，创建初始管理员。
- `cmd/node-agent`：真实节点代理，管理本机 xray-core，支持 exit、multi_exit、relay 模式。
- `cmd/node-agent-mock`：开发用节点代理模拟程序。
- `internal/`：后端核心包，包括 handler、service、repository、model、middleware、scheduler、subscription、config、auth、database、response、agent、platform。
- `migrations/`：SQL 数据库迁移，表结构以 migration 为准，不依赖 GORM 自动迁移。
- `frontend/`：Vue 3 + Vite + Element Plus 前端项目，源码在 `frontend/src`。
- `web/static/`：前端生产构建产物。
- `deploy/nginx/`：Nginx 反向代理配置。
- `文档/`：架构、接口、部署、运维、页面清单、测试报告和每日记录。
- `开发方案.md`：完整开发方案。

Go 测试文件与实现文件同目录，命名为 `*_test.go`。前端页面主要位于 `frontend/src/pages/user` 和 `frontend/src/pages/admin`。

## 后端分层

后端遵循 `HTTP Request -> Handler -> Service -> Repository -> MySQL` 的分层。

- Handler 层位于 `internal/handler/`，只负责参数解析、调用 Service 和写响应，不写业务逻辑。
- Service 层位于 `internal/service/`，承载业务规则、事务、权限和跨 Repository 编排，错误返回 AppError。
- Repository 层位于 `internal/repository/`，封装 GORM 数据访问。
- Model 层位于 `internal/model/`，维护 GORM 模型、请求结构和响应结构。
- Middleware 层位于 `internal/middleware/`，处理鉴权、日志、跨域等横切逻辑。
- Subscription 层位于 `internal/subscription/`，负责 Clash/mihomo YAML 订阅输出。

## 前端与路由

前端使用 Vue 3 + Vite + Element Plus + Alova + Pinia + Vue Router 4。

- 用户路由包括 `/login`、`/register`、`/`、`/subscription`、`/orders`、`/plans`、`/redeem`、`/profile`。
- 管理后台路由包括 `/admin/login`、`/admin`、`/admin/plans`、`/admin/node-groups`、`/admin/nodes`、`/admin/relays`、`/admin/users`、`/admin/orders`、`/admin/redeem-codes`、`/admin/subscription-tokens`、`/admin/logs`、`/admin/node-operations`、`/admin/subscription-settings`。
- 路由守卫通过 `requiresAuth`、`requiresAdmin`、`guest` meta 标记和 `beforeEach` 执行。
- 接口调用应通过 `frontend/src/api/request.js` 或专门 API 适配层收口，避免页面内散落原生 `fetch`。

## 鉴权机制

系统使用 JWT 双 Token。

- Access Token 放在 `Authorization: Bearer` 请求头，默认 24 小时过期。
- Refresh Token 放在 HttpOnly Cookie `refresh_token`，默认 7 天过期。
- 刷新和退出必须使旧 Refresh Token 失效，避免长期可复用。
- API 统一返回 `{success, message, code, data}` 格式。

## 核心数据表

当前核心表包括 `users`、`plans`、`node_groups`、`plan_node_groups`、`nodes`、`node_hosts`、`user_subscriptions`、`subscription_tokens`、`refresh_tokens`、`orders`、`payment_records`、`traffic_snapshots`、`node_access_tasks`、`usage_ledgers`、`redeem_codes`、`relays`、`relay_backends`、`relay_config_tasks`、`relay_traffic_snapshots`、`site_settings`、`operation_logs`、`deployment_logs`、`node_runtime_metrics`、`node_health_checks`。

数据库迁移文件位于 `migrations/*.up.sql`，使用 golang-migrate 执行。

## 构建、测试与开发命令

- `cp .env.example .env`：首次配置环境变量。
- `docker-compose up -d mysql`：启动 MySQL。
- `make migrate`：基于 `MIGRATE_DATABASE_URL` 执行 SQL 迁移。
- `make api`：启动 API 服务，即 `go run ./cmd/api`。
- `make worker`：启动后台定时任务。
- `make seed`：运行数据库种子工具。
- `make frontend`：启动 Vite 前端开发服务器。
- `make frontend-build`：构建前端产物到 `web/static`。
- `go test ./...`：运行全部后端测试。
- `go test ./internal/service/... -v`：运行指定 service 包测试。
- `cd frontend && npm run build`：验证前端生产构建。
- `make build`：构建 Go 二进制到 `bin/`。
- `make docker`：执行 `docker-compose build`。
- `make up` / `make down`：启动或停止 Docker Compose 服务。
- `docker-compose config --services`：校验 Compose 服务配置。

当前运行环境使用 `docker-compose` 命令；Makefile 默认 `COMPOSE ?= docker-compose`，如目标环境只支持 Compose v2 子命令，可显式执行 `COMPOSE="docker compose" make up`。

## 编码风格与命名规范

- Go 代码必须使用 `gofmt`。
- 包名保持短小、全小写。
- 每个代码文件前 10 行内写功能简介。
- 复杂业务逻辑、事务处理、并发控制等代码应添加必要注释，但不要添加无意义注释。
- 统一错误处理使用 `platform/response.AppError`。
- Handler 不写业务逻辑，只做参数解析、Service 调用、响应写入。
- Service 层事务操作返回 AppError。
- v1 不引入 Redis。
- 前端使用 Vue 单文件组件、Composition API 和 Element Plus，缩进为 2 个空格；现有 JavaScript 风格不使用分号。
- 测试命名优先使用 `Test{功能}_{场景}_{预期}`，例如 `TestAuthService_Register_Success`。

## 测试要求

- 后端测试使用 Go testing 与 `stretchr/testify`。
- 修改 service、repository、handler、middleware、subscription、scheduler 等核心包时，应补充对应测试。
- Service 层优先写单元测试，Repository 层写集成测试，Handler 层写 HTTP 端到端测试。
- 涉及事务、鉴权、token、流量计费、节点任务处理的改动，需要增加聚焦测试。
- 每完成一项功能开发或 bug 修复，应立即编写对应测试并运行通过，然后再进行下一项工作。
- 提交前至少运行 `go test ./...`，核心业务路径目标覆盖率不低于 80%。
- 涉及前端改动时运行 `cd frontend && npm run build`。
- 涉及关键用户路径、节点部署、订阅输出、管理后台页面时运行 Playwright smoke。

## 文档要求

- 文档是正式交付物，代码、测试、文档三者同步完成才算完成。
- 文档目录名、文件名统一使用中文。
- 每个自然日需要在 `文档/每日记录/` 下新增或更新当天日报，命名为 `YYYY-MM-DD-日报.md`。
- 任何影响接口、数据库、部署方式、安全策略、协议字段、订阅输出或运营流程的变更必须同步更新 `开发方案.md` 与 `文档/` 中对应说明。
- 排查中无法复现或依赖真实环境的验证条件，应记录到文档或测试报告中。

## Agent 协作注意

- 所有回复用中文。
- 不需要反复询问是否继续，用户要暂停时会主动说。
- 禁止在未完成时停下来问“是否继续”，必须持续推进直到任务完成，除非用户主动说暂停或必须等待外部信息。
- 修改前先阅读相邻实现和 `文档/代码审查/代码审查报告.md`，按既有分层处理问题。
- 不要覆盖本地 `.env`、构建产物或用户未说明的改动。
- 排查时优先使用 `rg`、`go test ./...`、`npm run build` 和 `docker-compose config --services`。
- 若用户明确允许更新本地配置，修改后必须重新运行 Compose 配置校验。

## 唯一规则文件

- 本仓库唯一规则文件为 `AGENTS.md`。
- 后续不要重新创建其他规则文件，也不要再添加多规则文件同步要求。
- 修改规则时只更新 `AGENTS.md`，并按影响范围同步更新 `开发方案.md` 和 `文档/` 中对应文档。
- 若某条规则只适用于特定工具或 agent，应在 `AGENTS.md` 中明确适用范围，避免冲突。

## 套餐与基础套餐规则

- 系统必须始终存在一个基础套餐，`plans.is_default=true` 表示基础套餐。
- 基础套餐不能删除，只能修改；后端必须强制基础套餐保持启用，前端必须禁用基础套餐删除入口。
- 用户注册或管理员新增用户时，必须同步生成用户唯一订阅 Token，并自动分配基础套餐订阅。
- 删除普通套餐时采用 `plans.is_deleted=true` 逻辑删除，不硬删订单或兑换码历史；仍在使用该套餐的活动订阅必须自动迁移到基础套餐，并触发出口节点用户同步。
- 套餐列表、用户套餐选择、下单和兑换码开通不得使用 `is_deleted=true` 的套餐。
- 套餐流量采用固定双池：`normal`（普通流量）与 `residential`（家宽流量）。`plans.traffic_limit` / `user_subscriptions.traffic_limit` / `user_subscriptions.used_traffic` 继续表示普通流量兼容字段，家宽流量使用独立字段维护。
- 套餐支持双池扣费倍率：`plans.normal_traffic_multiplier` 作用于普通流量池，`plans.residential_traffic_multiplier` 作用于家宽流量池，默认均为 `1.000`。倍率只影响后续流量入账，不回溯历史账本。
- 订阅超额判断必须按流量池分别处理：普通流量耗尽只影响普通节点，家宽流量耗尽只影响家宽节点，不能把两种流量混扣，也不能把一个池耗尽视为整个订阅失效。
- 兑换码、管理员改订阅、基础套餐迁移和注册自动分配基础套餐时，必须同时维护普通流量和家宽流量字段；未显式设置家宽流量时默认 `0`。兑换码续费已有有效订阅时，必须同时叠加 `traffic_limit` 与 `residential_traffic_limit`，保留 `used_traffic` 与 `residential_used_traffic`，不能只叠加普通流量。

## 双流量池规则

- 出口节点必须声明流量池归属：`nodes.traffic_pool` 取值固定为 `normal` 或 `residential`，默认 `normal`。
- 普通节点按本机出口 IP 建模；家宽节点按上游代理账号建模。`nodes.outbound_type=direct` 表示普通直连出口，`nodes.outbound_type=socks5` 表示家宽上游代理出口。
- `nodes.outbound_ip` 语义按出站方式区分：直连节点表示该逻辑节点的真实本机出口 IP，并写入 Xray `freedom.sendThrough`；SOCKS5 家宽节点表示连接上游代理时使用的本机源 IP，并写入 Xray `socks.sendThrough`，不代表上游家宽最终出口 IP。
- 家宽代理节点一条 `nodes` 记录只允许绑定一个上游 SOCKS5 账号；用户前台看到的是多条独立节点和多条订阅线路，而不是一个节点后面自动轮询多个家宽账号。
- 如果管理员一次导入多条 SOCKS5，上层必须拆成多条逻辑 `nodes`，而不是把多条 SOCKS5 塞进一条节点记录。
- `/api/agent/traffic` 与 `/api/agent/multi/traffic` 处理流量时，必须先读取节点 `traffic_pool`，再按当前套餐对应流量池倍率计算扣费流量，并把扣费流量记入对应订阅流量池。
- `usage_ledgers` 必须同时记录真实流量与扣费流量：`delta_*` 表示节点实际上报增量，`billing_multiplier` 表示本次入账倍率，`billed_*` 表示实际扣费量，便于区分普通流量和家宽流量账本。
- 订阅生成时必须按节点流量池过滤：某个流量池剩余为 0 时，该池节点和对应中转线路不得继续出现在订阅里；另一个池有剩余时仍可继续下发。
- 节点用户同步必须支持按流量池下发。普通流量超额只能对普通池节点下发 `DISABLE_USER`，家宽流量超额只能对家宽池节点下发 `DISABLE_USER`。
- 后台和用户侧展示必须同时展示普通流量与家宽流量；旧字段继续展示普通流量，新增结构用于展示完整双池信息。
- 修改双流量池字段、节点流量池归属、`/api/agent/traffic` 扣量逻辑、订阅过滤或套餐展示时，必须同步更新 `AGENTS.md`、`开发方案.md`、接口文档、节点代理文档和运维手册，并运行后端测试、前端构建和 Playwright smoke。

## 节点 Reality 与订阅联调规则

- 订阅输出配置由后台 `/admin/subscription-settings` 管理，存储在 `site_settings.setting_key=subscription_config`；`SUBSCRIPTION_PROFILE_NAME` 仅作为未配置时的默认订阅名称回退。
- Clash/mihomo 订阅文件名、`profile-title`、自定义 `rules`、`profile-update-interval`、`profile-web-page-url` 和 `subscription-userinfo` 用量头必须由订阅配置统一控制；规则为空时必须回退到 `GEOIP,CN,DIRECT` 与 `MATCH,PROXY`，缺少兜底规则时必须自动追加 `MATCH,PROXY`。
- 节点国家/地区必须使用结构化字段 `nodes.region_code`、`nodes.region_name`、`nodes.region_flag`，后台节点新增、编辑和一键部署都应允许选择内置全球常用地区库；地区库使用 ISO/地区缩写和国旗，订阅节点名前的国旗来自该字段，不得再依赖节点名称关键词猜测。
- 订阅节点命名由订阅配置 `node_name_template` 控制，支持 `{{flag}}`、`{{name}}`、`{{region}}`、`{{code}}`、`{{index}}`、`{{transport}}`、`{{pool}}`；没有明确地区时不得强行追加默认国旗。
- 订阅分组由后台订阅配置 `proxy_groups` 手动维护，管理员可自定义分组名称、类型和节点 ID 列表；订阅生成必须按配置输出 Clash/mihomo `proxy-groups`。自动测速只使用 `url-test`，故障转移 `fallback` 不作为当前功能输出。
- 订阅公开展示和客户端导入链接必须使用 `/sub/{token}`，不得再展示 `/clash`、`/base64` 或 `/plain` 后缀；服务端不得保留带后缀订阅下载入口。
- `subscription-userinfo` 必须使用用户当前有效订阅的普通池与家宽池合计用量和总量生成，只作为客户端展示信息，不参与扣费；扣费仍以 `usage_ledgers` 和订阅双流量池字段为准。
- 修改订阅配置、订阅响应头、Clash 规则输出或订阅配置页面时，必须同步更新 `AGENTS.md`、`开发方案.md`、订阅接口文档、管理接口文档和页面清单，并运行 `go test ./...`、前端构建和 Playwright smoke。
- 涉及 VLESS + Reality 节点、一键部署、订阅生成或节点同步时，必须确认 `nodes.server_name`、`nodes.public_key`、`nodes.short_id` 与节点 `/usr/local/etc/xray/config.json` 中 `realitySettings.serverNames[0]`、`publicKey` 或由 `privateKey` 派生的 PublicKey、`shortIds[0]` 一致。
- 一键部署完成后必须自动读取节点 Xray Reality 参数并写回中心节点记录；如果同步失败，应让部署失败并清理本次创建的节点记录，不能留下可导入但不可用的节点。
- 排查“订阅可导入但节点无信号”时，按顺序检查：订阅 URL 返回 200、节点 443/TCP 可达、Reality `sni/pbk/sid` 是否一致、用户 UUID 是否存在于节点 Xray `clients`、Xray 是否有可用 `outbounds`。
- 节点运营中心显示健康只代表心跳、端口、流量上报和负载等观测项正常，不代表该节点已经包含所有可见活跃订阅用户 UUID；若客户端无信号但运营中心正常，必须检查 `node_access_tasks` 和远端 Xray `clients`，必要时通过 `POST /api/admin/nodes/:id/resync-users` 或后台“同步用户”按钮重下发授权。
- 修改节点协议字段、Xray 配置模板、node-agent 同步逻辑或订阅输出格式后，除 `go test ./...` 外，还要用真实订阅链接或 Xray 客户端验证节点能出站，并同步更新 `开发方案.md` 与 `文档/`。
- 修改出口节点流量统计、`/api/agent/traffic` 或 node-agent 上报逻辑时，必须保留本地流量队列、`collected_at` 入账和乱序旧批次跳过语义；验证项至少包含 `go test ./...`、node-agent 队列重放测试和真实节点上报状态。
- `nodes.last_traffic_report_at` 表示中心收到流量报告的时间，`nodes.last_traffic_success_at` 表示成功处理到的节点采集时间，不得混用。
- `nodes.protocol` 表示 VLESS 等协议，`nodes.transport` 表示传输层；当前默认 `tcp`，可选 `xhttp`。
- XHTTP 节点仍使用 VLESS + Reality，但必须清空 `flow`，不得给 Xray clients 或订阅写入 `xtls-rprx-vision`。
- SOCKS5 家宽节点同样必须清空 `flow`，即使 `transport=tcp` 也不得给 Xray clients、节点同步 payload 或订阅写入 `xtls-rprx-vision`；普通直连 TCP 节点才默认使用 Vision flow。
- `nodes.udp_enabled` 表示订阅侧 UDP 策略：Clash/mihomo YAML 必须按该字段输出 `udp`；SOCKS5 家宽节点默认 `false`，用于减少 QUIC/HTTP3 经二次转发导致的 Cloudflare 卡顿或风控，管理员可在新增、编辑和一键部署时显式开启。
- XHTTP 参数由 `nodes.xhttp_path`、`nodes.xhttp_host`、`nodes.xhttp_mode` 管理；订阅输出必须包含 `network/type=xhttp` 和 XHTTP 参数。
- 节点的 `traffic_pool` 与协议、传输层独立；同一物理服务器可同时部署普通池和家宽池逻辑节点。
- 节点的 `outbound_type` 与 `traffic_pool` 独立；同一物理服务器可同时托管普通 IP 型节点和 SOCKS5 上游型家宽节点。
- 管理后台新增节点和一键部署允许多选传输模式；单选时仍创建一条 `nodes`，多选时按每种传输模式创建一条逻辑 `nodes` 线路。
- 同一 IP 同时选择 TCP 与 XHTTP 时必须使用不同端口；默认 TCP 443、XHTTP 8443，不能在同一个 Xray inbound 上混用两种 network。
- 端口冲突必须按监听端点 `listen_ip:port` 判定，不得只按端口全局去重；多 IP 服务器允许不同公网 IP 使用相同端口，例如 `IP-A:443` 与 `IP-B:443`，但同一 `listen_ip` 下不能有两条逻辑节点占用同一端口。管理 API 新增和编辑节点必须在后端校验同一 `node_host_id` 或同一 `agent_base_url` 下的监听端点，`0.0.0.0` / `::` 视为通配监听，不能只依赖前端提示。
- 修改 XHTTP 字段、订阅格式、Xray `xhttpSettings` 或 node-agent 用户同步时，必须同步更新 `AGENTS.md`、`开发方案.md` 和相关接口/部署文档，并运行后端测试、前端构建和 Playwright smoke。

## 多出口 IP 与 multi_exit 规则

- 多出口 IP 服务器必须由管理员在一键部署前显式开启“多 IP 服务器”模式；未开启时不得扫描服务器出口 IP，也不得自动创建多个节点。
- 开启多 IP 模式后，必须先通过 SSH 扫描服务器公网 IPv4，并验证 `curl --interface <IP>` 的实际出口等于该 IP；只有管理员手动勾选确认的可用公网 IP 才能创建为逻辑出口节点。
- 多 IP 模式下 `node_hosts` 表示一台物理服务器和唯一 node-agent 身份，`nodes` 表示逻辑出口节点；一个公网出口 IP 对应一条 `nodes` 记录。
- 管理后台出口节点列表必须按节点服务器聚合展示：外层一行表示一台物理服务器，显示管理 IP、全部相关 IP、普通/家宽线路数量、SOCKS5 数量、TCP/XHTTP 数量和已用监听端点；展开或编辑服务器时再展示具体 `nodes` 逻辑线路。新增家宽或普通线路时应优先绑定同一 `node_host_id`，而不是创建另一个物理服务器身份。
- multi_exit 模式只安装一个 `node-agent`，使用 `AGENT_ROLE=multi_exit`、`NODE_HOST_ID`、`NODE_HOST_TOKEN` 和 `MULTI_NODE_CONFIG` 管理同一物理服务器下的多个逻辑节点。
- 即使不是多出口 IP，只要一键部署选择了多个传输模式，也必须按多逻辑节点处理，并在目标服务器只运行一个 multi_exit node-agent。
- multi_exit 生成的 Xray 配置必须为每个逻辑节点创建独立 inbound/outbound：普通节点使用 `freedom.sendThrough` 绑定本机出口 IP；家宽代理节点使用 `socks` outbound 指向唯一 `outbound_proxy_url`，如果设置了 `nodes.outbound_ip`，还必须把它作为 `socks.sendThrough`。
- multi_exit 对中心仍必须按 `node_id` 分别心跳、领取任务、上报任务结果和用户级流量；中心账本、套餐授权和订阅生成继续以 `nodes.id` 为归属。
- multi_exit 写入 Xray clients 时必须使用节点内部分隔的统计 email，避免同一用户跨多个逻辑节点的 Xray Stats 累计值混在一起；上报中心前再还原为原始 `xray_user_key`。
- 出口节点一键部署必须是真一键：部署成功后自动绑定管理员选择的节点分组、触发已有活跃订阅用户同步；选择替换旧角色时必须自动停用同服务器旧 relay 和旧出口记录，不能依赖人工再去后台补分组或停旧线路。
- 修改多 IP 扫描、`node_hosts`、multi_exit 协议、Xray 多 inbound/outbound 模板或流量归属逻辑时，必须同步更新 `AGENTS.md`、`开发方案.md`、节点代理部署指南和管理接口文档，并运行 `go test ./...`、前端构建和 Playwright smoke。

## 直连与中转演进规则

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
- 涉及中转数据表、node-agent relay 模式、订阅输出或部署流程时，必须同步更新 `AGENTS.md`、`开发方案.md`、`文档/中转/中转设计.md`、接口文档和架构决策。

## Agent 中心地址容灾规则

- node-agent 必须支持多个中心入口：`CENTER_SERVER_URL` 保留为主入口，`CENTER_SERVER_URLS` 为逗号、空格或换行分隔的完整 http/https 地址列表；新部署必须同时写入两者。
- node-agent 对所有中心请求必须从当前可用入口开始尝试，失败后轮询备用入口；某个入口请求成功后必须记住为当前 active center。
- 一键部署出口节点和中转节点必须提供“备用中心地址”输入，并把主中心和备用中心归一化写入 agent 容器环境变量；生产主/备中心入口通过未提交的环境变量配置，公开仓库不得写真实域名或 IP。后端可根据环境变量中的入口集合自动补齐主备地址。
- 控制平台迁移或域名/IP 不可用时，优先依赖 agent 多中心自动切换；最后兜底才由管理员通过后台“修复中心”发起中心 SSH 到节点机器，重建 Docker agent 或更新 systemd drop-in 并重启 agent。
- SSH 兜底接口为 `POST /api/admin/nodes/repair-center`，可指定 `node_id`、`node_host_id` 或 `relay_id` 等待新心跳确认；该接口只能保存脱敏部署日志，不得记录 SSH 密码、完整 Token 或私钥。
- 修改 `CENTER_SERVER_URLS`、一键部署中心地址、SSH 兜底修复脚本或 agent 中心切换逻辑时，必须同步更新 `AGENTS.md`、`开发方案.md`、节点代理部署指南、管理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建、Compose 配置校验和 Playwright smoke。

## 日志中心与审计规则

- 日志中心 v1 已落地 `/admin/logs`、`/api/admin/logs/runtime`、`/api/admin/logs/deployments`、`/api/admin/logs/operations`，不是规划功能。
- 运行日志读取宿主机按小时切分的 `logs/api/YYYY-MM-DD-HH-00.log` 与 `logs/worker/YYYY-MM-DD-HH-00.log`，后台接口必须限制最大返回行数并只允许管理员访问；旧版 `logs/api/YYYY-MM-DD/HH.log`、`logs/worker/YYYY-MM-DD/HH.log`、`logs/api.log`、`logs/worker.log` 仅作为兼容读取。
- Docker Compose 和 systemd 部署都必须通过 `deploy/scripts/hourly-log.sh` 写入按小时切分的日志文件；Compose 需把宿主机 `./logs` 挂载到 API/Worker 容器 `/app/logs`，systemd 需设置 `RAYPILOT_LOG_DIR=/root/suiyue/logs`。日志文件尚未创建时，运行日志接口应返回空列表，不得 500。
- 操作日志必须记录用户注册、登录、退出、资料修改、密码修改、下单、兑换码兑换、订阅下载，以及管理员新增/删除/禁用用户、重置密码、修改订阅、生成兑换码等关键动作。
- 部署日志必须记录一键部署出口节点和中转节点的结果、耗时、步骤明细、目标服务器 IP、操作者 IP、逻辑节点/中转/后端记录 ID。
- 所有结构化日志必须记录可用 IP 信息：`client_ip` 或 `operator_ip`，并尽量保留 `X-Forwarded-For`、`X-Real-IP`、`User-Agent` 以便排障。
- 日志中不得写入密码、完整 Token、JWT、数据库连接串、SSH 私钥、Reality 私钥；部署请求只能保存脱敏摘要，例如 `node_token_provided` / `relay_token_provided` 布尔值。
- 修改日志表结构、日志记录入口、日志页面或一键部署日志摘要时，必须同步更新 `AGENTS.md`、`开发方案.md`、管理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建和 Playwright smoke。

## 节点运营中心规则

- 节点运营中心 v1 已落地 `/admin/node-operations`、`/api/admin/node-operations/summary`、`/api/admin/node-operations/nodes/:id/checks`，用于节点健康度、延迟、负载、故障原因和流量排行展示。
- 节点运营中心 v1 只做观测和诊断：不得自动停用节点、不得自动隐藏订阅线路、不得直接触发用户禁用；任何自动调度或摘除策略必须作为后续功能单独设计。
- node-agent 心跳运行指标必须保持向后兼容；旧 agent 不上报 `runtime_metric` 时，中心仍要正常处理心跳、任务领取和流量上报。
- 健康检查状态必须综合心跳、TCP 端口、流量上报、Xray 运行状态和负载指标。TCP 端口可达只能代表端口通，不得宣称真实 VLESS/Reality/XHTTP 客户端 100% 可用。
- 健康检查不得把“节点端口可达”误判为“用户授权已同步”；运营中心 v1 不校验远端 Xray `clients` 是否包含每个活跃订阅 UUID，用户授权一致性由节点访问任务和手动重同步接口处理。
- 节点运营中心流量排行必须来自 `usage_ledgers`，同时区分真实流量 `delta_*` 与扣费流量 `billed_*`，不能用订阅已用量反推节点排行。
- 修改 node-agent 心跳指标、`node_runtime_metrics`、`node_health_checks`、健康评分、节点运营页面或运营接口时，必须同步更新 `AGENTS.md`、`开发方案.md`、管理接口文档、节点代理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建和 Playwright smoke。

## 用户级限速规则

- 用户级限速 v1 通过 `user_subscriptions.speed_limit_bps` 配置，单位为 bps，`0` 表示不限速；后台用户订阅编辑必须允许管理员设置 Mbps 并保存为 bps。
- 限速参数必须随 `UPSERT_USER` 节点访问任务下发到 node-agent，单出口和 multi_exit 都必须支持；用户禁用、删除、套餐过期或超额时必须清理对应本地限速策略。
- Xray-core 当前不提供稳定的原生按用户 Mbps 限速能力，node-agent v1 必须基于 Xray Stats 的用户级累计流量做滑动窗口平均限速：超过阈值时临时从对应 VLESS inbound 摘除用户，窗口结束后自动恢复用户。
- 限速只控制出口节点用户连接，不改变订阅生成、不改变套餐流量扣费和 `usage_ledgers` 入账；中转节点不执行用户限速，用户经中转访问时仍由出口节点限速。
- node-agent 必须把限速策略和运行状态持久化到本地状态文件，重启后继续生效；状态文件不得包含中心 Token、订阅 Token、SSH 密码或 Reality 私钥。
- 修改 `speed_limit_bps`、限速任务 payload、node-agent 限速执行器或用户订阅编辑页时，必须同步更新 `AGENTS.md`、`开发方案.md`、管理接口文档、节点代理接口文档、运维手册和页面清单，并运行 `go test ./...`、前端构建和 Playwright smoke。

## 测试节点信息与敏感数据

- 真实节点、控制面入口、备用入口、节点 ID、Reality 公钥、SSH 账号和 Token 属于生产敏感信息，不得提交到 GitHub。
- 公开仓库只允许使用 `[REDACTED]`、`<primary-center-url>`、`<fallback-center-url>`、`<exit-node-ip>` 等占位符描述流程。
- 若需要真实联调信息，应保存在未提交的本地运维密钥文件或密码管理器中；不得写入 `AGENTS.md`、`CHANGELOG.md`、`开发方案.md`、`文档/` 或截图。
- 当前生产库没有启用中的中转节点；`relays` 与 `relay_backends` 均为空。若后续重新启用中转，必须通过后台 API 一键部署，并只在私有运维记录中维护真实服务器信息。

## 一键部署规则

- 一台物理服务器只保留一个当前角色的 node-agent；旧 systemd/relay agent 不得与当前出口角色并存。
- 一键部署镜像包由 `make node-agent-image` 生成到 `deploy/artifacts/node-agent-image.tar.gz`。
- Docker Compose 会把 `deploy/artifacts` 挂载到 API 容器 `/root/raypilot-artifacts`，供部署接口使用。
- 一键部署必须以 Docker 容器闭环成功为准：部署前做镜像包和目标服务器预检，清理旧角色后必须检查本次节点/中转端口没有被 nginx、宝塔或其他进程占用，并自动放行目标机 UFW/firewalld 上的节点或中转 TCP 端口。
- 部署中必须等待 Docker 可用、容器持续运行、Xray/HAProxy 配置生成、agent 心跳回连，并确认节点端口由 `xray` 监听、中转端口由 `haproxy` 监听，不能只用“端口有人监听”当成功。
- 失败时必须采集容器日志和端口诊断，并清理本次创建的容器、节点/中转记录、后端绑定和配置任务。
- 单出口部署必须向容器写入 `NODE_PORT`，node-agent 生成 Xray 配置时按后台端口监听，不能固定 443。
- 部署前必须清理旧 Xray/HAProxy 配置，避免脏服务器复用旧配置。
- 若云厂商安全组另有入站限制，仍需在云侧放行 TCP 443、8443 或自定义监听端口。

## 开发进度记录

- 第 1 阶段：项目初始化与文档骨架，已完成。
- 第 2 阶段：认证和用户，注册、登录、Token 刷新、用户首页，已完成。
- 第 3 阶段：套餐、节点、后台基础，CRUD 与管理页面，已完成。
- 第 4 阶段：订阅生成与节点授权，Clash/mihomo YAML 与 NodeAccessTask，已完成。
- 第 5 阶段：兑换码，生成、兑换、记录管理，已完成。
- 第 6 阶段：订单与支付骨架，表结构与接口骨架，已完成。
- 第 7 阶段：流量采集，快照差值、配额控制、节点联动，已完成。
- 第 8 阶段：运维、测试与文档收口，已完成。

## 已修复审查问题

- 兑换码接口 `/api/redeem` 未挂 JWTAuth 中间件，已修复。
- JWT 默认密钥应改为启动时 panic，已修复。
- `migrations/001_init.up.sql` 缺少 `refresh_tokens` 表创建，已修复。
- 前端 `Plans.vue` 缺少 `ElMessageBox` 导入，已修复。
- Logout/RefreshToken 不轮转、不使旧 token 失效，已修复。
- Worker 过期订阅扫描不创建 `DISABLE_USER` 任务，已修复。
- `ProcessTrafficReport` 和任务创建等错误被静默丢弃，已改为日志记录。
- `docker-compose.yml` 硬编码数据库密码且暴露 3306 端口，已移除默认密码回退，3306 默认不暴露。

## 提交与 PR 要求

当前工作区没有可用 Git 历史时，提交信息建议使用简洁祈使句，例如 `fix subscription token validation` 或 `add redeem code expiry check`。

PR 应包含变更摘要、测试结果、关联任务或问题背景。前端改动需附截图或说明影响页面。涉及数据库迁移、接口契约、部署方式或安全行为的变更，必须在 PR 中明确标注，并同步更新 `开发方案.md` 与 `文档/`。

## 安全与配置注意事项

- 不要提交 `.env`、密钥、生成的二进制、覆盖率文件、`frontend/node_modules` 或无意更新的构建产物。
- 新环境从 `.env.example` 开始配置，务必设置强 `JWT_SECRET`。
- Docker Compose 场景下，`DATABASE_URL` 与 `MIGRATE_DATABASE_URL` 都应指向 Compose 内的 MySQL 服务名，而不是 `127.0.0.1`。
- 仅通过环境变量加载配置，关键变量见 `.env.example`；不要引入配置中心。
