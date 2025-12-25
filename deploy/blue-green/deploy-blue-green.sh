#!/bin/bash
# ═══════════════════════════════════════════════════════════════════════════════
# 蓝绿部署脚本 — LLM Gateway 生产环境
#
# 流程：判断当前颜色 → 启动对端 → 健康检查 → nginx reload → drain → 停旧环境
#
# 用法:
#   ./deploy-blue-green.sh                    # 自动切换到非活跃颜色
#   ./deploy-blue-green.sh --rollback         # 回滚到上一个颜色（立即切换，不启动新容器）
#
# 前置条件：
#   - 环境变量已通过 .env 或 export 设置（DATABASE_URL, REDIS_*, JWT_SECRET 等）
#   - docker compose v2+, nginx 已安装
#   - upstream-active.conf 已被 nginx 主配置 include
# ═══════════════════════════════════════════════════════════════════════════════

set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
STATE_FILE="${DEPLOY_DIR}/.active-color"
UPSTREAM_DIR="${DEPLOY_DIR}"
NGINX_UPSTREAM="/opt/llm-gateway/upstream-active.conf"
DRAIN_TIMEOUT=600
HEALTH_RETRIES=30
HEALTH_INTERVAL=3

# ─── 工具函数 ──────────────────────────────────────────────────────────────────

log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { log "ERROR: $*"; exit 1; }

get_active_color() {
    if [ -f "$STATE_FILE" ]; then
        cat "$STATE_FILE"
    else
        # 首次部署：检测哪个在运行
        if docker ps --format '{{.Names}}' | grep -q "llm-blue-gateway"; then
            echo "blue"
        elif docker ps --format '{{.Names}}' | grep -q "llm-green-gateway"; then
            echo "green"
        else
            echo "none"
        fi
    fi
}

get_target_color() {
    local active="$1"
    case "$active" in
        blue)  echo "green" ;;
        green) echo "blue" ;;
        none)  echo "blue" ;;  # 首次部署默认蓝
        *)     die "Unknown active color: $active" ;;
    esac
}

get_gateway_port() {
    case "$1" in
        blue)  echo "8080" ;;
        green) echo "8082" ;;
    esac
}

get_cp_port() {
    case "$1" in
        blue)  echo "8081" ;;
        green) echo "8083" ;;
    esac
}

health_check() {
    local color="$1"
    local gw_port cp_port
    gw_port=$(get_gateway_port "$color")
    cp_port=$(get_cp_port "$color")

    log "Waiting for ${color} to become healthy..."
    for i in $(seq 1 $HEALTH_RETRIES); do
        if wget --spider -q "http://localhost:${gw_port}/health" 2>/dev/null && \
           wget --spider -q "http://localhost:${cp_port}/healthz" 2>/dev/null; then
            log "${color} is healthy (attempt ${i}/${HEALTH_RETRIES})"
            return 0
        fi
        sleep $HEALTH_INTERVAL
    done
    die "${color} failed health check after $((HEALTH_RETRIES * HEALTH_INTERVAL))s"
}

switch_nginx() {
    local color="$1"
    log "Switching nginx upstream to ${color}..."
    cp "${UPSTREAM_DIR}/upstream-${color}.conf" "$NGINX_UPSTREAM"
    nginx -t || die "nginx config test failed after switching to ${color}"
    nginx -s reload
    log "nginx reloaded → traffic now on ${color}"
}

start_env() {
    local color="$1"
    log "Starting ${color} environment..."
    docker compose -f "${DEPLOY_DIR}/docker-compose.${color}.yml" -p "llm-${color}" up -d --remove-orphans
}

stop_env() {
    local color="$1"
    log "Stopping ${color} environment..."
    docker compose -f "${DEPLOY_DIR}/docker-compose.${color}.yml" -p "llm-${color}" down --timeout 600
}

save_state() {
    echo "$1" > "$STATE_FILE"
    log "State saved: active=${1}"
}

# ─── 主逻辑 ────────────────────────────────────────────────────────────────────

main() {
    local rollback=false
    if [ "${1:-}" = "--rollback" ]; then
        rollback=true
    fi

    local active target
    active=$(get_active_color)
    target=$(get_target_color "$active")

    log "═══════════════════════════════════════════════════════════"
    log "Blue-Green Deploy: active=${active} → target=${target}"
    log "═══════════════════════════════════════════════════════════"

    if [ "$rollback" = true ]; then
        # 回滚：直接切 nginx，不启动新容器（假设对端还在运行）
        if [ "$active" = "none" ]; then
            die "No active environment to rollback from"
        fi
        log "ROLLBACK: switching back to ${target}"
        health_check "$target" || die "${target} is not running, cannot rollback"
        switch_nginx "$target"
        save_state "$target"
        log "Rollback complete. Drain old (${active}) for ${DRAIN_TIMEOUT}s then stop..."
        sleep "$DRAIN_TIMEOUT"
        stop_env "$active"
        log "═══ Rollback finished ═══"
        return 0
    fi

    # 正常部署流程
    # 1. 启动目标环境
    start_env "$target"

    # 2. 等待健康
    health_check "$target"

    # 3. 切换 nginx
    switch_nginx "$target"
    save_state "$target"

    # 4. 等待 drain（让旧环境的存量 SSE 长连接自然结束）
    if [ "$active" != "none" ]; then
        log "Draining old environment (${active}) for ${DRAIN_TIMEOUT}s..."
        log "  (existing SSE streams on ${active} will finish naturally)"
        sleep "$DRAIN_TIMEOUT"
        stop_env "$active"
    fi

    log "═══════════════════════════════════════════════════════════"
    log "Deploy complete. Active: ${target}"
    log "  Gateway:       http://localhost:$(get_gateway_port "$target")/health"
    log "  Control-Plane: http://localhost:$(get_cp_port "$target")/healthz"
    log "═══════════════════════════════════════════════════════════"
}

main "$@"
