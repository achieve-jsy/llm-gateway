# ═══════════════════════════════════════════════════════════════════════════════
# 预构建 Builder 基础镜像
# 内含: Go module cache + Go build cache（所有依赖包已编译为 .a 文件）
#
# 重建时机: go.mod 或 go.sum 变更时（CI 自动触发）
# 效果: 日常 build 只编译项目源码，依赖包全部命中 cache，2-3min → 10-30s
#
# 构建方式:
#   docker build -f deploy/docker/builder.Dockerfile -t registry/llm-gateway-builder:latest .
# ═══════════════════════════════════════════════════════════════════════════════

FROM 192.0.2.10:5000/golang:1.26-alpine

ENV GOPROXY=https://goproxy.cn,direct
ENV GONOSUMCHECK=*

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Layer 1: 下载依赖（go.mod/go.sum 不变时命中 Docker 层缓存）
COPY go.mod go.sum ./
RUN go mod download

# Layer 2: 预编译所有依赖包到 Go build cache
# 先 COPY 全量源码编译一次，然后删掉二进制，只保留 cache
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev/null ./cmd/gateway && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev/null ./cmd/control-plane && \
    rm -f /dev/null

# 此时 /root/.cache/go-build 和 /go/pkg/mod 已包含所有编译产物
# 后续 unified.Dockerfile FROM 此镜像 + COPY . . + go build 时，只编译变更的包