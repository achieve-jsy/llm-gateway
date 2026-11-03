#!/usr/bin/env bash
# 真实双进程脱敏 E2E：Postgres + Redis + control-plane + gateway + 假上游。
#
# 与 tests/integration/privacy_redaction_e2e_test.go 的分工：
#   那边是进程内（handler→orchestrator→providers 都是真的，但没有真实鉴权/配置链路）；
#   这边起两个真实进程，走真实 API Key 鉴权、控制面下发 provider、Redis 调度配置，
#   断言「上游真实收到的字节」和「客户端真实收到的字节」。
#
# 覆盖：mask 非流式 / 跨请求占位符稳定 / 流式跨 chunk 还原 / detect 放行 / block fail-closed。
#
# 依赖：psql、go、python3、atlas（建库）、redis-server（本地没有会用外部 Redis）。
# 用法：bash tests/e2e/privacy/run.sh
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
WORK=${E2E_WORK_DIR:-/tmp/privacy-e2e}
DB_URL=${E2E_DATABASE_URL:-postgres://jsy@127.0.0.1:5432/llm_gateway_priv_e2e?sslmode=disable}
ADMIN_URL=${E2E_ADMIN_URL:-postgres://jsy@127.0.0.1:5432/postgres}
DB_NAME=${E2E_DB_NAME:-llm_gateway_priv_e2e}
REDIS_PORT=${E2E_REDIS_PORT:-6390}
GW_PORT=${E2E_GATEWAY_PORT:-18080}
UP_PORT=${E2E_UPSTREAM_PORT:-19090}
CP_PORT=8081
API_KEY=sk-e2e-privacy-local-0000000000000000
SECRET='glpat-AbCdEfGhIjKlMnOpQrSt'
KEY_PATH=$WORK/hmac.key
GW_LOG=''

cleanup() {
  for pid in $(cat "$WORK/pids" 2>/dev/null); do kill "$pid" 2>/dev/null || true; done
  if [ -n "${REDIS_OWNED:-}" ]; then redis-cli -p "$REDIS_PORT" shutdown nosave >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

log() { printf '\n=== %s ===\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

mkdir -p "$WORK/config"
rm -rf "$WORK/upstream"; mkdir -p "$WORK/upstream"
: > "$WORK/pids"

cd "$ROOT"
# .env 是 dotenv 格式（OAUTH_SCOPES 之类的值带空格），bash 直接 source 会当成命令执行，
# 这里按 KEY=VALUE 逐行读，值原样保留。
# 只挑脚本真正需要的几个键，避免 .env 里的 DB_NAME/DATABASE_URL 覆盖本脚本的库名
# （上一版就这么把本地开发库当成 E2E 库重建了）。
while IFS= read -r line; do
  case "$line" in ''|'#'*) continue ;; esac
  key=${line%%=*}
  case "$key" in
    INTERNAL_TOKEN|ENCRYPTION_KEY|JWT_SECRET) export "$key=${line#*=}" ;;
  esac
done < .env
: "${INTERNAL_TOKEN:?需要 .env 里的 INTERNAL_TOKEN}"

# ── Redis ────────────────────────────────────────────────────────────────────
if ! redis-cli -p "$REDIS_PORT" ping >/dev/null 2>&1; then
  redis-server --port "$REDIS_PORT" --daemonize yes --save '' --appendonly no
  REDIS_OWNED=1
  until redis-cli -p "$REDIS_PORT" ping >/dev/null 2>&1; do sleep 0.2; done
fi
redis-cli -p "$REDIS_PORT" flushdb >/dev/null

# ── Postgres：重建 + Atlas 迁移 ──────────────────────────────────────────────
log "重建数据库 $DB_NAME"
psql "$ADMIN_URL" -tAc "select pg_terminate_backend(pid) from pg_stat_activity where datname='$DB_NAME'" >/dev/null
psql "$ADMIN_URL" -c "drop database if exists $DB_NAME" >/dev/null
psql "$ADMIN_URL" -c "create database $DB_NAME" >/dev/null
command -v atlas >/dev/null || fail "需要 atlas CLI"
atlas migrate apply --dir "file://atlas-migrations" --url "$DB_URL&search_path=public" >/dev/null

# ── 假上游 ──────────────────────────────────────────────────────────────────
UPSTREAM_PORT=$UP_PORT UPSTREAM_DUMP_DIR=$WORK/upstream \
  python3 tests/e2e/privacy/upstream.py > "$WORK/upstream.log" 2>&1 &
echo $! >> "$WORK/pids"

# ── 构建 ────────────────────────────────────────────────────────────────────
log "构建二进制"
go build -o "$WORK/gateway" ./cmd/gateway
go build -o "$WORK/control-plane" ./cmd/control-plane

# ── 最小数据集 ──────────────────────────────────────────────────────────────
log "写入最小数据集"
psql "$DB_URL" -q -v ON_ERROR_STOP=1 <<SQL
insert into model_groups (id, name, description, strategy, enabled, max_switches,
  same_model_retries, failover_cooldown_seconds, timeout_seconds, open_mode, model_type,
  sticky_enabled, sticky_ttl_seconds)
values (1, 'privacy-e2e-group', 'E2E', 'priority', true, 3, 1, 30, 120, 'public', 'chat', false, 300)
on conflict (id) do nothing;
SQL
E2E_DATABASE_URL="$DB_URL" E2E_UPSTREAM_BASE_URL="http://127.0.0.1:$UP_PORT/v1" \
  go run ./tests/e2e/privacy/seed
psql "$DB_URL" -q -v ON_ERROR_STOP=1 <<SQL
update providers set definition_id = null where name = 'e2e-openai';
update api_keys set bypass_org_check = true where name = 'e2e-privacy';
insert into group_models (group_id, model_id, model_name, priority, weight)
values (1, '44444444-4444-4444-4444-444444444444', 'bf-privacy-e2e', 0, 1)
on conflict do nothing;
SQL

# control-plane 只在启动时把「可用模型快照」写一次 Redis（router.Setup 里的
# RefreshAvailableSnapshot），之后要等 provider 变更才会再写。所以必须先灌数据再起 CP：
# 反过来的话网关拿到的是一份空快照，请求会 404 unknown_model。
# 之前能过只是因为共用一个长期存活的 Redis，里面残留着上一轮的快照。
log "启动 control-plane"
APP_ENV=dev CONTROL_PLANE_PORT=$CP_PORT DATABASE_URL="$DB_URL" \
  REDIS_ADDR="127.0.0.1:$REDIS_PORT" REDIS_PASSWORD='' \
  "$WORK/control-plane" > "$WORK/cp.log" 2>&1 &
echo $! >> "$WORK/pids"
for _ in $(seq 1 40); do
  [ "$(curl -s -m 2 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$CP_PORT/api/v1/settings/public")" = 200 ] && break
  sleep 1
done

# ── 网关配置与启动 ──────────────────────────────────────────────────────────
# 只改 privacy 段：mode / key_path / actions，其余沿用 config/dev.yaml。
write_config() {
  awk -v mode="$1" -v key="$KEY_PATH" -v act="$2" '
    /^privacy:/ { print; inb=1; next }
    inb && /^[^ \t]/ { inb=0 }
    inb && /^  mode:/ { print "  mode: \"" mode "\""; next }
    inb && /^  key_path:/ {
      print "  key_path: \"" key "\""
      if (act != "") { print "  actions:"; print act }
      next
    }
    inb && /^  # actions:/ { next }
    inb && /^  #   / { next }
    { print }
  ' "$ROOT/config/dev.yaml" > "$WORK/config/dev.yaml"
}

start_gw() {
  write_config "$1" "${2:-}"
  lsof -ti:$GW_PORT 2>/dev/null | xargs -r kill -9 2>/dev/null || true
  GW_LOG=$WORK/gw_$1.log
  ( cd "$WORK" && APP_ENV=dev GATEWAY_PORT=$GW_PORT REDIS_ADDR="127.0.0.1:$REDIS_PORT" \
      REDIS_PASSWORD='' REDIS_DB=0 DATABASE_URL="$DB_URL" \
      AUTH_INTERNAL_TOKEN="$INTERNAL_TOKEN" PRIVACY_MODE="$1" \
      exec "$WORK/gateway" > "$GW_LOG" 2>&1 ) &
  echo $! >> "$WORK/pids"
  disown || true
  for _ in $(seq 1 40); do
    [ "$(curl -s -m 2 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$GW_PORT/health")" = 200 ] && break
    sleep 1
  done
  # 调度配置每 30s 对账一次，等模型组进 router 再发请求（否则 503 model_unavailable）
  for _ in $(seq 1 45); do
    grep -q '"groups":1' "$GW_LOG" 2>/dev/null && break
    sleep 1
  done
}

req() {
  local body=$1
  if [ "${2:-}" = stream ]; then
    curl -sN -m 20 -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
      -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' -d "$body"
  else
    curl -s -m 20 -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
      -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
      -d "$body" -w '\n%{http_code}'
  fi
}

body() { printf '{"model":"privacy-e2e-group","messages":[{"role":"user","content":"%s"}]}' "$1"; }
last_up() { ls -1t "$WORK"/upstream/req_*.json 2>/dev/null | head -1; }
up_count() { ls -1 "$WORK"/upstream/req_*.json 2>/dev/null | wc -l | tr -d ' '; }

# ── 用例 ────────────────────────────────────────────────────────────────────
log "用例 1/5 mask 非流式：上游只见占位符，客户端见原文"
start_gw mask
resp=$(req "$(body "第一次 $SECRET")")
[ "$(echo "$resp" | tail -1)" = 200 ] || fail "mask 非流式失败: $resp"
up=$(cat "$(last_up)")
echo "$up" | grep -q '{{[A-Z_]*_[0-9a-z]\{8\}}}' || fail "上游没收到占位符: $up"
echo "$up" | grep -q "$SECRET" && fail "上游收到明文: $up"
echo "$resp" | grep -q "$SECRET" || fail "客户端没拿回原文: $resp"
echo "上游: $up"

log "用例 2/5 跨请求占位符稳定（key_path 生效）"
first=$(echo "$up" | grep -o '{{[A-Z_]*_[0-9a-z]\{8\}}}' | head -1)
req "$(body "第二次 $SECRET")" > /dev/null
second=$(cat "$(last_up)" | grep -o '{{[A-Z_]*_[0-9a-z]\{8\}}}' | head -1)
[ -n "$first" ] && [ "$first" = "$second" ] || fail "占位符不稳定: $first vs $second"
echo "占位符: $first"

log "用例 2b/5 trace 落库：call_traces 里不能留明文"
# 时序：TraceCapture 在 handler 之前就抓走了原始字节，trace 又是异步批量落库，
# 所以要轮询等落库完成；mask 模式的 trace 正文必须与上游一致（占位符，无明文）。
traced=0
for _ in $(seq 1 20); do
  masked=$(psql "$DB_URL" -tAc "select count(*) from call_traces where request_body::text like '%{{%'" 2>/dev/null || echo 0)
  if [ "${masked:-0}" -ge 1 ]; then traced=1; break; fi
  sleep 1
done
[ "$traced" = 1 ] || fail "call_traces 里没有脱敏后的请求正文"
plain=$(psql "$DB_URL" -tAc "select count(*) from call_traces where request_body::text like '%glpat-%'" 2>/dev/null || echo 0)
[ "${plain:-0}" = 0 ] || fail "call_traces 里仍有明文凭证（$plain 条）"
echo "call_traces: 含占位符 $masked 条，含明文 $plain 条"

log "用例 3/5 流式：占位符被上游切成两半，客户端仍拿到完整原文"
stream=$(req "$(printf '{"model":"privacy-e2e-group","stream":true,"messages":[{"role":"user","content":"流式 %s 结束"}]}' "$SECRET")" stream)
echo "$stream" | grep -q "$SECRET" || fail "流式没还原: $stream"
echo "$stream" | grep -q '{{' && fail "流式把占位符漏给客户端: $stream"
echo "客户端 SSE 条数: $(echo "$stream" | grep -c '^data: ')"

log "用例 4/5 detect：放行明文，只记 privacy_hits"
start_gw detect
resp=$(req "$(body "$SECRET")")
[ "$(echo "$resp" | tail -1)" = 200 ] || fail "detect 请求失败: $resp"
up=$(cat "$(last_up)")
echo "$up" | grep -q "$SECRET" || fail "detect 不该改写，但上游没收到明文: $up"
grep -q 'privacy_hits' "$GW_LOG" || fail "detect 没记录 privacy_hits"
echo "上游(明文): $up"

log "用例 5/5 block：fail-closed，请求不发上游"
start_gw mask '    GITLAB_TOKEN: block'
before=$(up_count)
resp=$(req "$(body "$SECRET")")
[ "$(echo "$resp" | tail -1)" = 500 ] || fail "block 期望 500，实际: $resp"
echo "$resp" | grep -q 'redaction_failed' || fail "block 错误码不对: $resp"
[ "$(up_count)" = "$before" ] || fail "block 后上游仍被调用"
echo "HTTP 500 redaction_failed，上游调用 $before → $(up_count)"

log "全部通过"