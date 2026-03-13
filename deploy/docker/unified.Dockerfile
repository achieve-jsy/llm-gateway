# ═══════════════════════════════════════════════════════════════════════════════
# 统一构建 Dockerfile — LLM Gateway + Control Plane
# 一个 builder stage 编译两个二进制，利用 Go build cache 避免重复编译
#
# 构建方式:
#   docker build --target gateway-runtime -t llm-gateway:xxx -f deploy/docker/unified.Dockerfile .
#   docker build --target cp-runtime -t llm-control-plane:xxx -f deploy/docker/unified.Dockerfile .
# ═══════════════════════════════════════════════════════════════════════════════

# ─── Stage 1: 统一 Builder（编译 gateway + control-plane + gateway-all）──────────
# 使用预构建的 builder 镜像，内含 Go module cache + build cache
# 依赖包已预编译为 .a 文件，日常构建只编译项目源码，秒级完成
FROM ${BUILDER_IMAGE:-192.0.2.10:5000/llm-gateway-builder:latest} AS builder

ENV GOPROXY=https://goproxy.cn,direct
ENV GONOSUMCHECK=*

WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/gateway-all ./cmd/gateway-all

# ─── Stage 2a: Gateway Runtime ────────────────────────────────────────────────
FROM 192.0.2.10:5000/alpine:3.21 AS gateway-runtime

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /out/gateway /usr/local/bin/gateway
COPY config/ /app/config/
COPY atlas-migrations/ /app/atlas-migrations/

WORKDIR /app

ARG APP_ENV=prod
ENV APP_ENV=${APP_ENV}
ENV TZ=Asia/Shanghai
ENV GATEWAY_PORT=8080
EXPOSE 8080

ENTRYPOINT ["gateway"]

# ─── Stage 2b: Control-Plane Runtime ──────────────────────────────────────────
FROM 192.0.2.10:5000/alpine:3.21 AS cp-runtime

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /out/control-plane /usr/local/bin/control-plane
COPY config/ /app/config/
COPY atlas-migrations/ /app/atlas-migrations/

WORKDIR /app

ARG APP_ENV=prod
ENV APP_ENV=${APP_ENV}
ENV TZ=Asia/Shanghai
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8081

ENTRYPOINT ["control-plane"]

# ─── Stage 2c: Gateway-All Runtime（gateway + control-plane 合并进程）──────────
FROM 192.0.2.10:5000/alpine:3.21 AS all-runtime

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /out/gateway-all /usr/local/bin/gateway-all
COPY config/ /app/config/
COPY atlas-migrations/ /app/atlas-migrations/

WORKDIR /app

ARG APP_ENV=prod
ENV APP_ENV=${APP_ENV}
ENV TZ=Asia/Shanghai
ENV GATEWAY_PORT=8080
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8080 8081

ENTRYPOINT ["gateway-all"]