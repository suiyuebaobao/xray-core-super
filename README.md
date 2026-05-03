# RayPilot - Xray / VLESS Reality VPN & Proxy Subscription Panel

**Self-hosted Xray panel for VPN/proxy services, VLESS Reality nodes, Clash/mihomo subscriptions, relay nodes, traffic accounting, and one-click node deployment.**

RayPilot is a self-hosted control panel for Xray-core proxy and VPN-like services. It provides a Go API, Vue admin console, user portal, subscription delivery, exit-node agent, relay-node management, traffic accounting, database migrations, and Docker/systemd deployment assets.

中文关键词：Xray 面板、VLESS Reality 面板、VPN 面板、代理面板、梯子面板、机场面板、订阅面板、Clash 订阅、mihomo 订阅、Shadowrocket 订阅、中转节点、流量统计、一键部署节点。

English keywords: xray panel, xray-core panel, vless reality panel, vpn panel, proxy panel, subscription panel, clash subscription, mihomo subscription, shadowrocket subscription, relay node, traffic accounting, node-agent, self-hosted proxy.

## Why RayPilot

- Xray-core and VLESS Reality node management for proxy/VPN subscription services
- Clash/mihomo, Shadowrocket-compatible, Base64, and plain URI subscription output
- Direct and relay line generation in one subscription token
- Multi-group node management for plans and user access control
- node-agent driven user sync, heartbeat, task execution, and traffic reporting
- HAProxy TCP relay management with safe config validation and reload
- One-click exit node and relay node deployment
- Per-user traffic ledger, daily/weekly/monthly summaries, and quota enforcement
- Admin console and user portal built with Vue 3 and Element Plus
- MySQL migrations, Docker Compose, Nginx, and systemd deployment examples

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go, Gin, GORM, MySQL |
| Frontend | Vue 3, Vite, Element Plus, Alova, Pinia |
| Proxy Node | Xray-core, VLESS Reality, node-agent |
| Relay | HAProxy TCP passthrough, node-agent relay mode |
| Deployment | Docker Compose, Nginx, systemd |
| Tests | Go testing, testify, Playwright |

## Quick Start

Copy environment variables and edit them for your environment:

```bash
cp .env.example .env
```

Start the Compose stack:

```bash
make up
```

Run backend and frontend separately for local development:

```bash
make api
make frontend
```

Run database migrations:

```bash
make migrate
```

Run backend tests:

```bash
go test ./...
```

Build frontend assets:

```bash
cd frontend && npm run build
```

Build the node-agent image used by one-click deployment:

```bash
make node-agent-image
```

## Repository Layout

```text
cmd/                 Go command entrypoints: api, worker, seed, node-agent
internal/            Backend handlers, services, repositories, models, platform code
migrations/          SQL migrations managed by golang-migrate
frontend/            Vue 3 admin console and user portal
web/static/          Frontend production build output
deploy/              Nginx and systemd deployment examples
文档/                Chinese architecture, API, deployment, operation, and daily notes
```

## GitHub Search Topics

Recommended repository topics for discovery:

```text
xray
xray-core
vless
reality
vpn
proxy
proxy-panel
subscription
subscription-panel
clash
mihomo
shadowrocket
relay
node-agent
traffic-accounting
self-hosted
golang
vue
```

Recommended GitHub repository description:

```text
Xray/VLESS Reality VPN & proxy subscription panel with Clash/mihomo subscriptions, relay nodes, traffic accounting, node-agent, and one-click deployment.
```

## Notes

- The Go module path is currently kept as `suiyue` for compatibility with existing imports and deployments.
- Local `.env` files, generated binaries, frontend build output, logs, and node modules are intentionally not committed.
- Production environments must use strong `JWT_SECRET`, database passwords, node tokens, and relay tokens.

## Contact

- QQ: 270133383
- WeChat: suiyue_creation

## License

This repository has not declared an open-source license yet. Contact the author before using, distributing, or building commercial derivatives.
