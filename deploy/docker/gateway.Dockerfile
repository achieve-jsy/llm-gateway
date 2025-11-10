# ─── Stage 1: Build ──────────────────────────────────────────────────────────
FROM 192.0.2.10:5000/golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/gateway /usr/local/bin/gateway

# 配置文件（Load() 通过 APP_ENV 查找 ./config/{env}.yaml）
RUN mkdir -p /app/config
COPY config/prod.yaml /app/config/prod.yaml

WORKDIR /app

ENV APP_ENV=prod
ENV GATEWAY_PORT=8080
EXPOSE 8080

ENTRYPOINT ["gateway"]
