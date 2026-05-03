# RayPilot — Makefile
#
# 常用命令：
#   make build      — 构建后端二进制
#   make api        — 启动 API 服务
#   make worker     — 启动 Worker 服务
#   make seed       — 运行数据库种子（创建初始管理员）
#   make migrate    — 执行数据库迁移
#   make frontend   — 启动前端开发服务器
#   make frontend:build — 构建前端生产版本
#   make docker     — 构建 Docker 镜像
#   make up         — 启动 Docker Compose 所有服务
#   make down       — 停止 Docker Compose 所有服务

.PHONY: build api worker seed migrate migrate-down frontend frontend-build docker node-agent-image up down node-agent

COMPOSE ?= docker-compose

# 构建后端
build:
	CGO_ENABLED=0 go build -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -o bin/worker ./cmd/worker
	CGO_ENABLED=0 go build -o bin/seed ./cmd/seed
	CGO_ENABLED=0 go build -o bin/node-agent ./cmd/node-agent

# 启动 API 服务
api:
	go run ./cmd/api

# 启动 Worker 服务
worker:
	go run ./cmd/worker

# 运行数据库种子
seed:
	go run ./cmd/seed

# 执行数据库迁移
migrate:
	@echo "执行所有 migration..."
	migrate -path migrations -database "$${MIGRATE_DATABASE_URL}" up

# 回滚最近一次 migration
migrate-down:
	migrate -path migrations -database "$${MIGRATE_DATABASE_URL}" down 1

# 前端开发服务器
frontend:
	cd frontend && npm run dev

# 前端生产构建
frontend-build:
	cd frontend && npm run build

# 构建 Docker 镜像
docker:
	$(COMPOSE) build

# 构建 node-agent 镜像并导出给一键部署使用
node-agent-image:
	docker build -f cmd/node-agent/Dockerfile -t raypilot/node-agent:latest .
	docker save raypilot/node-agent:latest | gzip > /root/node-agent-image.tar.gz

# 启动 Docker Compose
up:
	$(COMPOSE) up -d

# 停止 Docker Compose
down:
	$(COMPOSE) down
