.PHONY: build test lint swagger dev clean remove-bg

# 使用 bash 以支持 trap / kill 0 等特性
SHELL := /bin/bash

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

build-all:
	go build -mod=vendor -ldflags="-s -w" -o bin/gateway-all ./cmd/gateway-all

# ─── 测试 ────────────────────────────────────────────────────────────────────────
test:
	@if find tests -name '*_test.go' | grep -q .; then \
		go test ./tests/... -count=1; \
	else \
		echo "No test files found in tests/"; \
	fi

# ─── Lint ────────────────────────────────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ─── Swagger ─────────────────────────────────────────────────────────────────────
swagger:
	swag init -g api/swagger/doc.go -o api/swagger/docs --parseDependency --parseInternal

# ─── 开发运行 ─────────────────────────────────────────────────────────────────────
dev:
	APP_ENV=dev go run ./cmd/gateway

dev-cp:
	APP_ENV=dev go run ./cmd/control-plane

dev-web:
	cd web && npm run dev

# 一键启动开发环境：control-plane + gateway + web 前端
# Ctrl-C 会同时停止三个服务
.PHONY: dev-all
dev-all:
	@echo "🚀 启动开发环境：control-plane(:8081) + gateway(:8080) + web 前端 ..."
	@trap 'echo; echo "🛑 正在停止所有服务..."; kill 0' INT TERM EXIT; \
	APP_ENV=$(APP_ENV) go run ./cmd/control-plane & \
	APP_ENV=$(APP_ENV) go run ./cmd/gateway & \
	( cd web && npm run dev ) & \
	wait

# ─── 清理 ────────────────────────────────────────────────────────────────────────
clean:
	rm -rf bin/ api/swagger/docs/

# ─── 图片处理 ─────────────────────────────────────────────────────────────────────
# 去除 web/public/images/mascot/ 下PNG图片的白色背景，生成带透明通道的RGBA PNG
remove-bg:
	@echo "🖼  去除吉祥物图片白色背景..."
	@python3 scripts/remove_bg.py || python3.11 scripts/remove_bg.py || \
		( echo "❌ 需要 Python3 + Pillow，请先运行: pip install Pillow" && exit 1 )
	@echo "✅ 完成"

# ─── Atlas 迁移 ───────────────────────────────────────────────────────────────────
.PHONY: atlas-diff atlas-apply atlas-status

atlas-diff:
	atlas migrate diff $(name) --env local

atlas-apply:
	atlas migrate apply --env local

atlas-status:
	atlas migrate status --env local
