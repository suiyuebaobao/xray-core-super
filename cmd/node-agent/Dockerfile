FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /node-agent ./cmd/node-agent

FROM alpine:3.22

ARG XRAY_VERSION=v26.3.27

RUN apk add --no-cache ca-certificates curl unzip haproxy

WORKDIR /app

COPY --from=builder /node-agent /app/node-agent

# 手动安装 xray-core（alpine 没有 systemd）。固定默认版本，避免 latest API 或 CDN
# 临时异常导致一键部署镜像包构建失败；如需升级可用 --build-arg XRAY_VERSION=...
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64) XRAY_ARCH="linux-64" ;; \
      aarch64) XRAY_ARCH="linux-arm64-v8a" ;; \
      *) XRAY_ARCH="linux-64" ;; \
    esac && \
    test -n "$XRAY_VERSION" && \
    curl -fL --retry 5 --retry-all-errors --retry-delay 2 -o /tmp/xray.zip "https://github.com/XTLS/Xray-core/releases/download/${XRAY_VERSION}/Xray-${XRAY_ARCH}.zip" && \
    unzip -t /tmp/xray.zip >/dev/null && \
    unzip -o /tmp/xray.zip -d /tmp/xray && \
    cp /tmp/xray/xray /usr/local/bin/xray && \
    chmod +x /usr/local/bin/xray && \
    mkdir -p /usr/local/etc/xray && \
    rm -rf /tmp/xray* && \
    echo "xray installed: $(xray version | head -1)"

RUN chmod +x /app/node-agent

ENTRYPOINT ["/app/node-agent"]
