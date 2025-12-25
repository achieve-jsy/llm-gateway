# ─── Stage 1: Build ──────────────────────────────────────────────────────────
FROM 192.0.2.10:5000/golang:1.26-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct
ENV GONOSUMCHECK=*

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM 192.0.2.10:5000/alpine:3.21

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
