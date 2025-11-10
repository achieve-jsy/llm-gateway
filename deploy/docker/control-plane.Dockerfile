# ─── Stage 1: Build Go backend ───────────────────────────────────────────────
FROM 192.0.2.10:5000/golang:1.26-alpine AS go-builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=go-builder /out/control-plane /usr/local/bin/control-plane

# 配置文件（通过 APP_ENV 查找 ./config/{env}.yaml）
RUN mkdir -p /app/config
COPY config/prod.yaml /app/config/prod.yaml

WORKDIR /app

ENV APP_ENV=prod
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8081

ENTRYPOINT ["control-plane"]
