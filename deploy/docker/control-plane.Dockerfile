# ─── Stage 1: Build Go backend ───────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS go-builder

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
    go build -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=go-builder /out/control-plane /usr/local/bin/control-plane

COPY config/prod.yaml /app/config/prod.yaml
COPY migrations/ /app/migrations/

WORKDIR /app

ENV APP_ENV=prod
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8081

ENTRYPOINT ["control-plane"]
