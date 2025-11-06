# ==================== Build Stage ====================
FROM golang:1.22-alpine AS builder

# 镜像加速（可选，国内网络用）
# RUN go env -w GOPROXY=https://goproxy.cn,direct

WORKDIR /app

# 先复制依赖文件，利用 Docker 缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/gateway ./cmd/gateway/main.go

# ==================== Runtime Stage ====================
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 从 builder 复制二进制
COPY --from=builder /app/gateway .

# 暴露端口
EXPOSE 8080

# 启动
ENTRYPOINT ["./gateway"]
