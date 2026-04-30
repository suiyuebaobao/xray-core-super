FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /node-agent ./cmd/node-agent

FROM alpine:3.22

RUN apk add --no-cache ca-certificates curl unzip haproxy

WORKDIR /app

COPY --from=builder /node-agent /app/node-agent

# 手动安装 xray-core（alpine 没有 systemd）
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64) XRAY_ARCH="linux-64" ;; \
      aarch64) XRAY_ARCH="linux-arm64-v8a" ;; \
      *) XRAY_ARCH="linux-64" ;; \
    esac && \
    LATEST=$(curl -sL https://api.github.com/repos/XTLS/Xray-core/releases/latest | grep tag_name | cut -d'"' -f4) && \
    curl -L -o /tmp/xray.zip "https://github.com/XTLS/Xray-core/releases/download/${LATEST}/Xray-${XRAY_ARCH}.zip" && \
    unzip -o /tmp/xray.zip -d /tmp/xray && \
    cp /tmp/xray/xray /usr/local/bin/xray && \
    chmod +x /usr/local/bin/xray && \
    mkdir -p /usr/local/etc/xray && \
    rm -rf /tmp/xray* && \
    echo "xray installed: $(xray version | head -1)"

RUN chmod +x /app/node-agent

ENTRYPOINT ["/app/node-agent"]
