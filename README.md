# xray-core-super

`xray-core-super` 是一套围绕 `xray-core` 节点构建的订阅分发、用户管理和节点运维系统，包含 Go 后端、Vue 管理后台、用户中心、节点 agent、数据库迁移和 Docker Compose 部署配置。

## 功能

- 用户注册、登录、JWT 鉴权和 Refresh Token 刷新
- 套餐、订单、兑换码、订阅 Token 管理
- Clash/mihomo YAML、Base64 聚合、纯文本 URI 订阅输出
- 节点分组、出口节点授权和用户访问同步
- VLESS + Reality 节点参数管理
- 直连线路和中转线路订阅生成
- node-agent 节点侧同步、心跳、任务执行和流量上报
- relay agent 中转节点 HAProxy 配置下发和 reload
- 管理后台和用户中心前端页面
- Docker Compose、Nginx、systemd 部署配置

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 后端 | Go、Gin、GORM、MySQL |
| 前端 | Vue 3、Vite、Element Plus、Alova、Pinia |
| 节点 | xray-core、node-agent、HAProxy |
| 部署 | Docker Compose、Nginx、systemd |
| 测试 | Go testing、testify、Playwright |

## 快速开始

复制环境变量并按实际环境修改：

```bash
cp .env.example .env
```

启动 Compose 服务：

```bash
make up
```

本地分别启动后端和前端：

```bash
make api
make frontend
```

执行数据库迁移：

```bash
make migrate
```

运行后端测试：

```bash
go test ./...
```

构建前端：

```bash
cd frontend && npm run build
```

## 目录结构

```text
cmd/                 后端可执行程序入口
internal/            后端核心业务代码
migrations/          SQL 数据库迁移
frontend/            Vue 3 前端项目
deploy/              Nginx 和 systemd 部署配置
web/static/          前端构建产物目录
```

## 配置说明

项目不会提交本地 `.env`。新环境请从 `.env.example` 复制配置，并务必替换生产环境密钥、数据库密码、JWT secret、节点 token 等敏感信息。

## 联系方式

- QQ：270133383
- 微信：suiyue_creation

## 说明

本仓库当前未声明开源许可证。使用、分发或二次开发前请先联系作者确认授权。
