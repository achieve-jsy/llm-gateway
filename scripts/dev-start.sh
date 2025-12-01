#!/usr/bin/env bash
# dev-start.sh — 一键启动本地开发环境（gateway / control-plane / nginx / vite）
# 用法: bash scripts/dev-start.sh
set -e

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

stop_all() {
  echo "[dev] stopping..."
  kill "$GW_PID" "$CP_PID" "$VITE_PID" 2>/dev/null || true
  nginx -s stop 2>/dev/null || true
}
trap stop_all EXIT INT TERM

echo "[dev] building binaries..."
go build -o gateway ./cmd/gateway
go build -o control-plane ./cmd/control-plane

echo "[dev] starting control-plane..."
./control-plane > /tmp/cp.log 2>&1 &
CP_PID=$!

echo "[dev] starting gateway..."
./gateway > /tmp/gateway.log 2>&1 &
GW_PID=$!

echo "[dev] starting nginx..."
nginx -c "$REPO/deploy/nginx-dev.conf"

echo "[dev] starting vite..."
(cd web && npm run dev) > /tmp/vite.log 2>&1 &
VITE_PID=$!

echo ""
echo "  ✓ control-plane  :8081   (pid $CP_PID)"
echo "  ✓ gateway        :8080   (pid $GW_PID)"
echo "  ✓ nginx          :443"
echo "  ✓ vite           :5173   (pid $VITE_PID)"
echo ""
echo "  访问 https://localhost:5173  或  https://aimeter.xk-devops.com"
echo "  按 Ctrl+C 停止所有服务"
echo ""

wait
