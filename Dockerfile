# suiyue（岁月）— Dockerfile
#
# 多阶段构建：
# 阶段 1: 构建 Go 后端二进制
# 阶段 2: 构建前端静态文件

# ============================================================
# 阶段 1: 构建 Go 后端
# ============================================================
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/seed ./cmd/seed

# ============================================================
# 阶段 2: 构建前端
# ============================================================
FROM node:22-alpine AS frontend-builder

WORKDIR /frontend

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install

COPY frontend/ .
RUN npm run build

# ============================================================
# 阶段 3: API 最终镜像
# ============================================================
FROM alpine:3.22 AS api

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=backend-builder /bin/api /app/api
COPY --from=backend-builder /bin/worker /app/worker
COPY --from=backend-builder /bin/seed /app/seed
COPY --from=frontend-builder /web/static /app/web/static
COPY migrations /app/migrations

EXPOSE 3000

CMD ["/app/api"]

# ============================================================
# 阶段 4: Worker 最终镜像
# ============================================================
FROM api AS worker
CMD ["/app/worker"]

# ============================================================
# 阶段 5: Seed 最终镜像
# ============================================================
FROM api AS seed
CMD ["/app/seed"]
