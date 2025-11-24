#!/usr/bin/env bash
# watchdog.sh — 自动守护并在崩溃时重启 control-plane 和 gateway
REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

restart_cp() {
  echo "[watchdog] $(date '+%H:%M:%S') starting control-plane..."
  ./control-plane >> /tmp/cp.log 2>&1 &
  CP_PID=$!
  echo "[watchdog] control-plane pid=$CP_PID"
}

restart_gw() {
  echo "[watchdog] $(date '+%H:%M:%S') starting gateway..."
  ./gateway >> /tmp/gateway.log 2>&1 &
  GW_PID=$!
  echo "[watchdog] gateway pid=$GW_PID"
}

restart_cp
restart_gw

while true; do
  sleep 5
  if ! kill -0 "$CP_PID" 2>/dev/null; then
    echo "[watchdog] control-plane died, restarting..."
    restart_cp
  fi
  if ! kill -0 "$GW_PID" 2>/dev/null; then
    echo "[watchdog] gateway died, restarting..."
    restart_gw
  fi
done
