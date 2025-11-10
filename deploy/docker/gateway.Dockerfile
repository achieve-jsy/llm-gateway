# ─── Stage 1: Build ──────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/gateway /usr/local/bin/gateway

# 配置文件
COPY config/prod.yaml /app/config/prod.yaml

# 数据库迁移文件
COPY migrations/ /app/migrations/

WORKDIR /app

ENV APP_ENV=prod
ENV GATEWAY_PORT=8080
EXPOSE 8080

ENTRYPOINT ["gateway"]
