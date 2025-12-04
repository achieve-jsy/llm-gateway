# ─── Stage 1: Build Go backend ───────────────────────────────────────────────
FROM 192.0.2.10:5000/golang:1.26-alpine AS go-builder

ENV GOPROXY=https://goproxy.cn,direct
ENV GONOSUMCHECK=*

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM 192.0.2.10:5000/alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY --from=go-builder /out/control-plane /usr/local/bin/control-plane

COPY config/ /app/config/
COPY atlas-migrations/ /app/atlas-migrations/

WORKDIR /app

ARG APP_ENV=prod
ENV APP_ENV=${APP_ENV}
ENV TZ=Asia/Shanghai
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8081

ENTRYPOINT ["control-plane"]
