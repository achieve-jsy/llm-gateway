.PHONY: build test lint swagger dev clean

# ─── 变量 ────────────────────────────────────────────────────────────────────────
APP_ENV ?= dev
GATEWAY_BIN := bin/gateway
CP_BIN := bin/control-plane

# ─── 构建 ────────────────────────────────────────────────────────────────────────
build: build-gateway build-cp

build-gateway:
	go build -o $(GATEWAY_BIN) ./cmd/gateway

build-cp:
	go build -o $(CP_BIN) ./cmd/control-plane

# ─── 测试 ────────────────────────────────────────────────────────────────────────
test:
	go test ./... -race -cover

test-coverage:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# ─── Lint ────────────────────────────────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ─── Swagger ─────────────────────────────────────────────────────────────────────
swagger:
	swag init -g api/swagger/doc.go -o api/swagger/docs --parseDependency --parseInternal

# ─── 开发运行 ─────────────────────────────────────────────────────────────────────
dev: dev-infra
	APP_ENV=dev go run ./cmd/gateway

dev-cp:
	APP_ENV=dev go run ./cmd/control-plane

dev-infra:
	docker compose -f docker-compose.dev.yml up -d postgres redis

dev-infra-full:
	docker compose -f docker-compose.dev.yml --profile observability --profile langfuse up -d

dev-down:
	docker compose -f docker-compose.dev.yml --profile observability --profile langfuse down

# ─── 清理 ────────────────────────────────────────────────────────────────────────
clean:
	rm -rf bin/ coverage.out coverage.html api/swagger/docs/
