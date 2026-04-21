#!/bin/bash
# ═══════════════════════════════════════════════════════════════════════════════
# 蓝绿部署脚本 — LLM Gateway（生产 / 测试通用）
#
# 流程（始终「先起新、再停旧」，保证零宕机）：
#   判断当前颜色 → 启动对端(target) → 健康检查 → nginx 切换 → drain → 停旧环境
#
# 用法:
#   ./deploy-blue-green.sh                    # 部署到非活跃颜色
#   ./deploy-blue-green.sh --rollback         # 回滚到上一个颜色（要求对端仍在运行）
#   ./deploy-blue-green.sh --skip-start       # 跳过启动步骤，直接健康检查+切换+停旧（容器已由外部启动）
#
# 可用环境变量（带默认值，生产默认行为不变）：
#   NGINX_UPSTREAM   nginx 活跃 upstream 文件路径   (默认 /opt/llm-gateway/upstream-active.conf)
#   DRAIN_TIMEOUT    切换后等待旧环境 SSE 自然结束的秒数 (默认 600)
#   STOP_TIMEOUT     drain 后停止旧环境容器的超时秒数   (默认 600；旧容器若不响应 SIGTERM 会等满)
#   HEALTH_RETRIES   健康检查重试次数               (默认 30)
#   HEALTH_INTERVAL  健康检查间隔秒数               (默认 3)
#   SAFE_SWITCH_ENABLED 启用部署锁和可恢复的原子切流 (默认 false)
#   KEEP_OLD_ENV     切流后保留旧颜色，供快速回滚     (默认 false)
#   NGINX_SMOKE_URL  切流后经 nginx 验证的 URL        (默认空，不执行)
#   SMOKE_RETRIES    nginx 冒烟重试次数               (默认 5)
#   SMOKE_INTERVAL   nginx 冒烟重试间隔秒数           (默认 2)
#   DEPLOY_LOCK_FILE 主机级部署锁文件                 (默认同脚本目录)
#   LEGACY_CONTAINERS 首次迁移时要下线的旧容器名     (默认 "llm-gw-gateway llm-gw-control-plane")
#   IMAGE_TAG / REGISTRY / APP_ENV 及各密钥由 compose 透传
#
# 前置条件：
#   - 环境变量已通过 .env 或 export 设置（DATABASE_URL, REDIS_*, JWT_SECRET 等）
#   - docker compose v2+, nginx 已安装；nginx 主配置已 include $NGINX_UPSTREAM
# ═══════════════════════════════════════════════════════════════════════════════

set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"
STATE_FILE="${DEPLOY_DIR}/.active-color"
UPSTREAM_DIR="${DEPLOY_DIR}"
NGINX_UPSTREAM="${NGINX_UPSTREAM:-/opt/llm-gateway/upstream-active.conf}"
DRAIN_TIMEOUT="${DRAIN_TIMEOUT:-600}"
STOP_TIMEOUT="${STOP_TIMEOUT:-600}"
HEALTH_RETRIES="${HEALTH_RETRIES:-15}"
HEALTH_INTERVAL="${HEALTH_INTERVAL:-2}"
SAFE_SWITCH_ENABLED="${SAFE_SWITCH_ENABLED:-false}"
KEEP_OLD_ENV="${KEEP_OLD_ENV:-false}"
NGINX_SMOKE_URL="${NGINX_SMOKE_URL:-}"
SMOKE_RETRIES="${SMOKE_RETRIES:-5}"
SMOKE_INTERVAL="${SMOKE_INTERVAL:-2}"
DEPLOY_LOCK_FILE="${DEPLOY_LOCK_FILE:-${DEPLOY_DIR}/.deploy.lock}"
LEGACY_CONTAINERS="${LEGACY_CONTAINERS:-llm-gw-gateway llm-gw-control-plane}"
PRE_SWITCH_UPSTREAM=""

# ─── 工具函数 ──────────────────────────────────────────────────────────────────

log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { log "ERROR: $*"; exit 1; }

acquire_deploy_lock() {
    [ "$SAFE_SWITCH_ENABLED" = "true" ] || return 0
    command -v flock >/dev/null 2>&1 || die "flock is required for serialized deployment"
    exec 9>"$DEPLOY_LOCK_FILE"
    flock -n 9 || die "Another deployment is already running (lock: ${DEPLOY_LOCK_FILE})"
}

get_active_color() {
    if [ -f "$STATE_FILE" ]; then
        cat "$STATE_FILE"
    else
        # 首次部署：检测哪个蓝绿容器在运行（旧的非蓝绿容器不算）
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
        # 首次迁移：旧容器占用 blue 端口(8080/8081)，因此新版部署到 green，
        # 旧容器继续服务，待 green 健康并切流后再下线 —— 零宕机。
        none)  echo "green" ;;
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

    log "Waiting for ${color} to become healthy (gw:${gw_port} cp:${cp_port})..."
    for i in $(seq 1 "$HEALTH_RETRIES"); do
        # 用 curl 发 GET 探活：宿主机上的 GNU wget --spider 会发 HEAD，
        # 而 gin 的 /health、/healthz 仅注册 GET，HEAD 会 404 导致误判不健康。
        if curl -fsS -o /dev/null --max-time 5 "http://localhost:${gw_port}/ready" && \
           curl -fsS -o /dev/null --max-time 5 "http://localhost:${cp_port}/healthz"; then
            log "${color} is healthy (attempt ${i}/${HEALTH_RETRIES})"
            return 0
        fi
        sleep "$HEALTH_INTERVAL"
    done
    return 1
}

supports_budget_v2() {
    local color="$1" port payload
    port=$(get_gateway_port "$color")
    payload=$(curl -fsS --max-time 5 "http://localhost:${port}/ready" 2>/dev/null) || return 1
    # jq 优先；生产机未必装 jq，回退 python3，避免预检因缺工具直接中断部署。
    if command -v jq >/dev/null 2>&1; then
        echo "$payload" | jq -e \
            '.capabilities.tier_snapshot_schema == 1 and .capabilities.redis_v2 == true' \
            >/dev/null 2>&1
        return $?
    fi
    if command -v python3 >/dev/null 2>&1; then
        printf '%s' "$payload" | python3 -c '
import json, sys
d = json.load(sys.stdin)
c = d.get("capabilities") or {}
sys.exit(0 if c.get("tier_snapshot_schema") == 1 and c.get("redis_v2") is True else 1)
'
        return $?
    fi
    log "WARN: neither jq nor python3 available; treating active as budget-v2 unsupported"
    return 1
}

force_safe_budget_mode() {
    export BUDGET_TIER_MASTER_ENABLED=false
    export BUDGET_TIER_ACCOUNTING_ENABLED=false
    export BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED=false
    export BUDGET_TIER_ZERO_BLOCK_ENABLED=false
    export BUDGET_TIER_SHADOW_ENABLED=false
    export BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED=false
    export BUDGET_LEGACY_TOTAL_WRITE_ENABLED=true
    log "Budget mode forced safe: all tier/v2 enforcement disabled; legacy total write enabled"
}

log_budget_mode() {
    log "Budget mode: master=${BUDGET_TIER_MASTER_ENABLED:-false} accounting=${BUDGET_TIER_ACCOUNTING_ENABLED:-false} positive=${BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED:-false} zero=${BUDGET_TIER_ZERO_BLOCK_ENABLED:-false} shadow=${BUDGET_TIER_SHADOW_ENABLED:-false} v2_total=${BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED:-false} legacy_write=${BUDGET_LEGACY_TOTAL_WRITE_ENABLED:-true}"
}

switch_nginx() {
    local color="$1"
    local candidate backup

    if [ "$SAFE_SWITCH_ENABLED" != "true" ]; then
        log "Switching nginx upstream to ${color} (${NGINX_UPSTREAM})..."
        cp "${UPSTREAM_DIR}/upstream-${color}.conf" "$NGINX_UPSTREAM"
        nginx -t || die "nginx config test failed after switching to ${color}"
        nginx -s reload
        log "nginx reloaded → traffic now on ${color}"
        return 0
    fi

    candidate="${NGINX_UPSTREAM}.candidate.$$"
    backup="${NGINX_UPSTREAM}.backup.$$"

    log "Switching nginx upstream to ${color} (${NGINX_UPSTREAM})..."
    cp "${UPSTREAM_DIR}/upstream-${color}.conf" "$candidate"
    if [ -e "$NGINX_UPSTREAM" ]; then
        cp -p "$NGINX_UPSTREAM" "$backup"
    fi
    mv -f "$candidate" "$NGINX_UPSTREAM"

    if ! nginx -t; then
        if [ -e "$backup" ]; then mv -f "$backup" "$NGINX_UPSTREAM"; else rm -f "$NGINX_UPSTREAM"; fi
        die "nginx config test failed after switching to ${color}; upstream restored"
    fi
    if ! nginx -s reload; then
        if [ -e "$backup" ]; then
            mv -f "$backup" "$NGINX_UPSTREAM"
            nginx -t && nginx -s reload || true
        else
            rm -f "$NGINX_UPSTREAM"
        fi
        die "nginx reload failed after switching to ${color}; upstream restored"
    fi
    rm -f "$backup"
    log "nginx reloaded → traffic now on ${color}"
}

capture_current_upstream() {
    PRE_SWITCH_UPSTREAM="${NGINX_UPSTREAM}.pre-switch.$$"
    if [ -e "$NGINX_UPSTREAM" ]; then
        cp -p "$NGINX_UPSTREAM" "$PRE_SWITCH_UPSTREAM"
    else
        : > "$PRE_SWITCH_UPSTREAM"
    fi
}

discard_captured_upstream() {
    [ -z "$PRE_SWITCH_UPSTREAM" ] || rm -f "$PRE_SWITCH_UPSTREAM"
    PRE_SWITCH_UPSTREAM=""
}

restore_captured_upstream() {
    [ -n "$PRE_SWITCH_UPSTREAM" ] || return 1
    if [ -s "$PRE_SWITCH_UPSTREAM" ]; then
        mv -f "$PRE_SWITCH_UPSTREAM" "$NGINX_UPSTREAM"
    else
        rm -f "$PRE_SWITCH_UPSTREAM" "$NGINX_UPSTREAM"
    fi
    PRE_SWITCH_UPSTREAM=""
    nginx -t && nginx -s reload
}

smoke_check_nginx() {
    local i
    [ -z "$NGINX_SMOKE_URL" ] && return 0

    log "Verifying switched traffic through nginx (${NGINX_SMOKE_URL})..."
    for i in $(seq 1 "$SMOKE_RETRIES"); do
        if curl -fsS -o /dev/null --max-time 5 "$NGINX_SMOKE_URL"; then
            log "nginx smoke check passed (attempt ${i}/${SMOKE_RETRIES})"
            return 0
        fi
        sleep "$SMOKE_INTERVAL"
    done
    return 1
}

start_env() {
    local color="$1"
    log "Starting ${color} environment..."
    docker compose -f "${DEPLOY_DIR}/docker-compose.${color}.yml" -p "llm-${color}" up -d --remove-orphans
}

stop_env() {
    local color="$1"
    local timeout="${2:-600}"
    log "Stopping ${color} environment (timeout ${timeout}s)..."
    docker compose -f "${DEPLOY_DIR}/docker-compose.${color}.yml" -p "llm-${color}" down --timeout "$timeout"
}

# 下线首次迁移前的旧（非蓝绿）容器，按名字停止并删除
stop_legacy() {
    local present=false c
    for c in $LEGACY_CONTAINERS; do
        if docker ps --format '{{.Names}}' | grep -qx "$c"; then present=true; fi
    done
    if [ "$present" = false ]; then
        log "No legacy containers running, skip."
        return 0
    fi
    log "Draining legacy containers (${LEGACY_CONTAINERS}) for ${DRAIN_TIMEOUT}s..."
    log "  (existing SSE streams on legacy will finish naturally)"
    sleep "$DRAIN_TIMEOUT"
    # shellcheck disable=SC2086
    docker stop $LEGACY_CONTAINERS 2>/dev/null || true
    # shellcheck disable=SC2086
    docker rm $LEGACY_CONTAINERS 2>/dev/null || true
    log "Legacy containers stopped & removed. Ports freed."
}

save_state() {
    local state_tmp="${STATE_FILE}.tmp.$$"
    printf '%s\n' "$1" > "$state_tmp"
    mv -f "$state_tmp" "$STATE_FILE"
    log "State saved: active=${1}"
}

# ─── 主逻辑 ────────────────────────────────────────────────────────────────────

main() {
    acquire_deploy_lock

    local rollback=false
    local skip_start=false
    for arg in "$@"; do
        case "$arg" in
            --rollback) rollback=true ;;
            --skip-start) skip_start=true ;;
        esac
    done

    local active target
    active=$(get_active_color)
    target=$(get_target_color "$active")

    log "═══════════════════════════════════════════════════════════"
    log "Blue-Green Deploy: active=${active} → target=${target} (rollback=${rollback})"
    log "  NGINX_UPSTREAM=${NGINX_UPSTREAM}  DRAIN_TIMEOUT=${DRAIN_TIMEOUT}s  SAFE_SWITCH_ENABLED=${SAFE_SWITCH_ENABLED}  KEEP_OLD_ENV=${KEEP_OLD_ENV}"
    log "═══════════════════════════════════════════════════════════"

    if [ "$active" != "none" ] && ! supports_budget_v2 "$active"; then
        log "Active ${active} does not advertise tier snapshot schema 1 / Redis v2."
        force_safe_budget_mode
    fi
    log_budget_mode

    if [ "$rollback" = true ]; then
        # 回滚：直接切回对端（要求对端仍在运行，未被上次 drain 删除）
        [ "$active" = "none" ] && die "No active environment to rollback from"
        log "ROLLBACK: switching back to ${target}"
        health_check "$target" || die "${target} is not running/healthy, cannot rollback (请改用指定版本重新部署)"
        capture_current_upstream
        switch_nginx "$target"
        if ! smoke_check_nginx; then
            log "Rollback smoke check failed; restoring traffic to ${active}"
            restore_captured_upstream || die "Rollback smoke check failed and previous nginx upstream could not be restored; both environments were kept running"
            die "Rollback aborted because nginx smoke check failed; traffic restored to ${active}"
        fi
        discard_captured_upstream
        save_state "$target"
        if [ "$KEEP_OLD_ENV" = "true" ]; then
            log "Rollback complete. Keeping ${active} running for fast recovery."
            return 0
        fi
        log "Rollback complete. Draining old (${active}) for ${DRAIN_TIMEOUT}s then stop..."
        sleep "$DRAIN_TIMEOUT"
        stop_env "$active" "$STOP_TIMEOUT"
        log "═══ Rollback finished, active=${target} ═══"
        return 0
    fi

    # ─── 正常部署：始终「先起新、再停旧」 ───────────────────────────────────────
    if [ "$skip_start" = true ]; then
        log "SKIP-START: containers already started externally, skipping start_env for ${target}"
    else
        # 1. 启动目标环境（新版本）
        start_env "$target"
    fi

    # 2. 等待健康；失败则不动当前在跑的环境（旧栈/active 仍在服务），直接退出
    if ! health_check "$target"; then
        log "Health check failed for ${target}. Current traffic stays on previous env (no downtime)."
        log "Tearing down failed ${target} to free ports..."
        stop_env "$target" 30 || true
        die "${target} failed health check after $((HEALTH_RETRIES * HEALTH_INTERVAL))s; deploy aborted, nothing switched."
    fi
    supports_budget_v2 "$target" || {
        stop_env "$target" 30 || true
        die "${target} does not advertise tier_snapshot_schema=1 and redis_v2=true"
    }

    # 3. 切换 nginx 到新环境
    capture_current_upstream
    switch_nginx "$target"
    if ! smoke_check_nginx; then
        log "Post-switch smoke check failed; restoring previous nginx upstream"
        restore_captured_upstream || die "nginx smoke check failed and previous upstream could not be restored; both environments were kept running"
        stop_env "$target" 30 || true
        die "nginx smoke check failed; deploy rolled back"
    fi
    discard_captured_upstream
    save_state "$target"

    # 4. 下线旧环境（流量已切走）
    if [ "$active" = "none" ]; then
        # 首次迁移：下线旧（非蓝绿）容器
        stop_legacy
    elif [ "$KEEP_OLD_ENV" = "true" ]; then
        log "Keeping old environment (${active}) running for fast rollback."
    else
        log "Draining old environment (${active}) for ${DRAIN_TIMEOUT}s..."
        sleep "$DRAIN_TIMEOUT"
        stop_env "$active" "$STOP_TIMEOUT"
    fi

    log "═══════════════════════════════════════════════════════════"
    log "Deploy complete. Active: ${target}"
    log "  Gateway:       http://localhost:$(get_gateway_port "$target")/health"
    log "  Control-Plane: http://localhost:$(get_cp_port "$target")/healthz"
    log "═══════════════════════════════════════════════════════════"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
