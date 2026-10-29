#!/usr/bin/env bash
# 视频输入端到端验收（E2E）
#
# 与 tests/ 下的组件测试不同，这个脚本跑的是**真进程**：
#   真 control-plane(:18081) + 真 gateway(:18080) + 真 Postgres/Redis + 桩上游(:19099)
# 覆盖单测覆盖不到的那一段：控制面 payload → 网关 config syncer → 路由期能力过滤
# → 上游实际收到的片段字节。
#
# 前置条件：
#   1. Postgres 与 Redis 容器可用（默认复用 funny_mendel:32771 / jolly_gauss:32769，
#      可用 PG_CONTAINER/PG_PORT/REDIS_CONTAINER/REDIS_PORT 覆盖）；
#   2. 两个二进制可构建（若 internal/controlplane/router 因他人未提交改动而编译失败，
#      本脚本会在构建阶段失败，这是环境问题不是功能问题）。
#
# 用法：bash scripts/e2e-video-input.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PG_CONTAINER=${PG_CONTAINER:-funny_mendel}
PG_PORT=${PG_PORT:-32771}
REDIS_CONTAINER=${REDIS_CONTAINER:-jolly_gauss}
REDIS_PORT=${REDIS_PORT:-32769}
DB=${DB:-gateway_e2e}
CP_PORT=${CP_PORT:-18081}
GW_PORT=${GW_PORT:-18080}
STUB_PORT=${STUB_PORT:-19099}
INTERNAL_TOKEN="e2e-token-9d2f"
API_KEY="sk-e2e-video-test"
API_KEY_HASH=$(printf '%s' "$API_KEY" | shasum -a 256 | awk '{print $1}')
WORK=$(mktemp -d)
PASS=0
FAIL=0

cleanup() {
  [[ -n "${GW_PID:-}" ]] && kill "$GW_PID" 2>/dev/null
  [[ -n "${CP_PID:-}" ]] && kill "$CP_PID" 2>/dev/null
  [[ -n "${STUB_PID:-}" ]] && kill "$STUB_PID" 2>/dev/null
  wait 2>/dev/null
  echo "日志留在 ${WORK}（cp.log / gw.log / stub.log / upstream_last.json）"
}
trap cleanup EXIT

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
check() {
  if [[ "$2" == "$3" ]]; then
    echo "  PASS  $1（$3）"; PASS=$((PASS + 1))
  else
    echo "  FAIL  $1：期望 $2，实际 $3"; FAIL=$((FAIL + 1))
  fi
}

say "0/8 构建二进制"
(cd "$ROOT" && go build -o "$WORK/gateway" ./cmd/gateway && go build -o "$WORK/control-plane" ./cmd/control-plane) || {
  echo "构建失败：确认 cmd/control-plane 可编译（HEAD 上若受他人未提交改动影响会失败）"; exit 1;
}

say "1/8 重建数据库 schema 并顺序执行迁移"
docker exec "$PG_CONTAINER" psql -q -U gateway -d postgres -c "DROP DATABASE IF EXISTS $DB;" >/dev/null
docker exec "$PG_CONTAINER" psql -q -U gateway -d postgres -c "CREATE DATABASE $DB;" >/dev/null
for f in $(ls "$ROOT"/atlas-migrations/*.sql | grep -v '\.down\.sql$'); do
  docker exec -i "$PG_CONTAINER" psql -q -U gateway -d "$DB" -v ON_ERROR_STOP=1 < "$f" >/dev/null || {
    echo "迁移失败：$f"; exit 1; }
done
echo "  迁移执行完成"

say "2/8 种入数据（provider/catalog/models/组/用户/API Key）"
docker exec -i "$PG_CONTAINER" psql -q -U gateway -d "$DB" -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO providers (id, name, type, endpoint, encrypted_credentials, enabled, weight, priority, status, max_retries, timeout, consecutive_failures, created_at, updated_at)
VALUES ('aaaaaaaa-0000-0000-0000-000000000001', 'e2e-stub', 'openai', 'http://127.0.0.1:$STUB_PORT/v1', '', true, 1, 0, 1, 0, 30, 0, now(), now());

INSERT INTO model_catalog (id, source, source_model_id, display_name, provider, mode, context_length, max_output_tokens, supports_vision, supports_video_input, input_price_per_million, output_price_per_million, synced_at, created_at, updated_at)
VALUES
 ('cccccccc-0000-0000-0000-000000000001', 'e2e', 'e2e-video-model', 'video', 'openai', 'chat', 100000, 8192, true, true, 0.1, 0.4, now(), now(), now()),
 ('cccccccc-0000-0000-0000-000000000002', 'e2e', 'e2e-text-model', 'text', 'openai', 'chat', 100000, 8192, true, false, 0.1, 0.4, now(), now(), now());

INSERT INTO models (id, name, provider_id, catalog_id, model_type, enabled, price_mode, weight, ctx_overridden, max_output_overridden, consecutive_failures, consecutive_recovery_successes, video_min_duration_seconds, video_max_duration_seconds, video_supported_resolutions, video_supported_aspect_ratios, video_supported_actions, created_at, updated_at, status, upstream_model_id)
VALUES
 ('bbbbbbbb-0000-0000-0000-000000000001', 'video-model', 'aaaaaaaa-0000-0000-0000-000000000001', 'cccccccc-0000-0000-0000-000000000001', 'chat', true, 'catalog', 1, false, false, 0, 0, 0, 0, '[]', '[]', '[]', now(), now(), 1, 'video-model'),
 ('bbbbbbbb-0000-0000-0000-000000000002', 'text-model', 'aaaaaaaa-0000-0000-0000-000000000001', 'cccccccc-0000-0000-0000-000000000002', 'chat', true, 'catalog', 1, false, false, 0, 0, 0, 0, '[]', '[]', '[]', now(), now(), 1, 'text-model');

INSERT INTO model_groups (id, name, description, strategy, enabled, max_switches, same_model_retries, failover_cooldown_seconds, timeout_seconds, open_mode, is_capability_override, override_supports_vision, override_supports_function_calling, override_supports_tool_choice, model_type, sticky_enabled, sticky_ttl_seconds, created_at, updated_at)
VALUES
 (9001, 'e2e-mixed-group', 'video+text', 'priority', true, 1, 0, 1, 30, 'public', false, false, false, false, 'chat', false, 300, now(), now()),
 (9002, 'e2e-text-group', 'text only', 'priority', true, 1, 0, 1, 30, 'public', false, false, false, false, 'chat', false, 300, now(), now());

INSERT INTO group_models (id, group_id, model_id, model_name, priority, weight, complexity_tier, created_at, updated_at)
VALUES
 (1, 9001, 'bbbbbbbb-0000-0000-0000-000000000001', 'video-model', 0, 1, 'MEDIUM', now(), now()),
 (2, 9001, 'bbbbbbbb-0000-0000-0000-000000000002', 'text-model', 1, 1, 'MEDIUM', now(), now()),
 (3, 9002, 'bbbbbbbb-0000-0000-0000-000000000002', 'text-model', 0, 1, 'MEDIUM', now(), now());

INSERT INTO users (id, sub, name, email, source, role, created_at, updated_at, login_status, dept_source)
VALUES ('dddddddd-0000-0000-0000-000000000001', 'e2e-user', 'e2e', 'e2e@example.com', 'local', 'admin', now(), now(), 'active', 'local');

INSERT INTO api_keys (id, user_id, name, key_hash, encrypted_key, key_prefix, key_hint, enabled, log_enabled, access_all_groups, bypass_org_check, dept_id, dept_name, studio_id, studio_name, created_at, updated_at)
VALUES ('eeeeeeee-0000-0000-0000-000000000001', 'dddddddd-0000-0000-0000-000000000001', 'e2e', '$API_KEY_HASH', 'x', 'sk-', 'e2e', true, true, true, true, 'e2e-dept', 'e2e', 'e2e-studio', 'e2e', now(), now());
SQL
echo "  数据就绪（API Key: ${API_KEY}）"

say "3/8 写 provider snapshot 到 Redis"
cat > "$WORK/snapshot.json" <<JSON
{"version":1,"updated_at":"$(date -u +%Y-%m-%dT%H:%M:%SZ)","entries":[{"id":"aaaaaaaa-0000-0000-0000-000000000001","name":"e2e-stub","type":"openai","endpoint":"http://127.0.0.1:$STUB_PORT/v1","credentials":"","enabled":true,"models":["video-model","text-model"],"models_v2":[{"name":"video-model","upstream_model_id":"video-model"},{"name":"text-model","upstream_model_id":"text-model"}]}]}
JSON
docker exec "$REDIS_CONTAINER" redis-cli SET gateway:providers:snapshot:v1 "$(cat "$WORK/snapshot.json")" >/dev/null
echo "  snapshot 已写入"

say "4/8 启动桩上游（记录收到的请求体）"
cat > "$WORK/stub.py" <<PY
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
LOG = "$WORK/upstream_last.json"
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        open(LOG, "wb").write(self.rfile.read(n))
        data = json.dumps({"id":"chatcmpl-e2e","object":"chat.completion","created":0,"model":"stub",
            "choices":[{"index":0,"message":{"role":"assistant","content":"ok-from-stub"},"finish_reason":"stop"}],
            "usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}).encode()
        self.send_response(200); self.send_header("Content-Type","application/json")
        self.send_header("Content-Length", str(len(data))); self.end_headers(); self.wfile.write(data)
    def log_message(self, *a): pass
HTTPServer(("127.0.0.1", $STUB_PORT), H).serve_forever()
PY
python3 "$WORK/stub.py" > "$WORK/stub.log" 2>&1 &
STUB_PID=$!

say "5/8 启动 control-plane 与 gateway"
export DATABASE_URL="postgres://gateway:gateway@127.0.0.1:$PG_PORT/$DB?sslmode=disable"
export REDIS_ADDR="127.0.0.1:$REDIS_PORT"
export INTERNAL_TOKEN JWT_SECRET="e2e-jwt-secret"
export CONTROL_PLANE_PORT=$CP_PORT GATEWAY_PORT=$GW_PORT
export CONTROL_PLANE_URL="http://127.0.0.1:$CP_PORT"
export AUTH_URL="http://127.0.0.1:$CP_PORT" AUTH_INTERNAL_TOKEN="$INTERNAL_TOKEN"
export GATEWAY_URL="http://127.0.0.1:$GW_PORT" APP_ENV=dev
"$WORK/control-plane" > "$WORK/cp.log" 2>&1 &
CP_PID=$!
for _ in $(seq 1 60); do
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$CP_PORT/api/v1/internal/groups" -H "X-Internal-Token: $INTERNAL_TOKEN")" == "200" ]] && break
  sleep 0.5
done
"$WORK/gateway" > "$WORK/gw.log" 2>&1 &
GW_PID=$!
for _ in $(seq 1 60); do
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$GW_PORT/v1/models" -H "Authorization: Bearer $API_KEY")" == "200" ]] && break
  sleep 0.5
done
sleep 6

# 就绪探针必须真的检查依赖（PG/Redis）：蓝绿切流门禁与 compose healthcheck 依赖它。
check "CP /readyz → 200" "200" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${CP_PORT}/readyz")"
check "CP /readyz 报告 postgres=ok" "ok:ok" \
  "$(curl -s "http://127.0.0.1:${CP_PORT}/readyz" | python3 -c 'import json,sys;c=json.load(sys.stdin)["checks"];print(c["postgres"]+":"+c["redis"])')"
check "CP /readyz?deep=1（库里有可用 Key）-> 200" "200" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${CP_PORT}/readyz?deep=1")"
check "合成 Key -> 401 invalid_api_key（verify 链路可达）" "401:invalid_api_key" \
  "$(c=$(curl -s -o "$WORK/smoke.json" -w '%{http_code}' -H 'Authorization: Bearer sk-smoke-e2e' "http://127.0.0.1:${GW_PORT}/v1/models"); echo "$c:$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["error"]["code"])' "$WORK/smoke.json")")"

say "6/8 控制面 payload 能力位"
payload=$(curl -s "http://127.0.0.1:$CP_PORT/api/v1/internal/groups" -H "X-Internal-Token: $INTERNAL_TOKEN")
check "video-model 下发 supports_video_input=true" "True" \
  "$(printf '%s' "$payload" | python3 -c 'import json,sys;d=json.load(sys.stdin);print([m["supports_video_input"] for g in d["data"] for m in g["models"] if m["name"]=="video-model"][0])')"
check "text-model 下发 supports_video_input=false" "False" \
  "$(printf '%s' "$payload" | python3 -c 'import json,sys;d=json.load(sys.stdin);print([m["supports_video_input"] for g in d["data"] for m in g["models"] if m["name"]=="text-model"][0])')"

say "7/8 正向与反向请求"
VID='{"role":"user","content":[{"type":"text","text":"这段视频讲了什么"},{"type":"video_url","video_url":{"url":"https://example.com/a.mp4","fps":2}}]}'
rm -f "$WORK/upstream_last.json"
code=$(curl -s -o "$WORK/r1.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d "{\"model\":\"e2e-mixed-group\",\"messages\":[$VID]}")
check "混合组 + 视频 → 200" "200" "$code"
check "上游原样收到 video_url 与 fps" "True" \
  "$(python3 - "$WORK/upstream_last.json" <<'PY'
import json, sys
try:
    body = json.load(open(sys.argv[1]))
    part = body["messages"][0]["content"][1]
    print(part == {"type": "video_url", "video_url": {"url": "https://example.com/a.mp4", "fps": 2}})
except Exception:
    print(False)
PY
)"

code=$(curl -s -o "$WORK/r2.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d "{\"model\":\"e2e-text-group\",\"messages\":[$VID]}")
check "纯文本组 + 视频 → 400 video_input_not_supported" "400:video_input_not_supported" \
  "$code:$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["error"]["code"])' "$WORK/r2.json")"

code=$(curl -s -o "$WORK/r3.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/messages" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d "{\"model\":\"e2e-mixed-group\",\"max_tokens\":16,\"messages\":[$VID]}")
check "/v1/messages + 视频 → 400 unsupported_content_part" "400:unsupported_content_part" \
  "$code:$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["error"]["code"])' "$WORK/r3.json")"

code=$(curl -s -o "$WORK/r4.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"e2e-mixed-group","messages":[{"role":"user","content":[{"type":"pdf","pdf":{"url":"https://e/a.pdf"}}]}]}')
check "未知片段 → 400 unsupported_content_part + param" "400:unsupported_content_part:content[0].type" \
  "$code:$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))["error"];print(d["code"]+":"+d.get("param",""))' "$WORK/r4.json")"

say "8/8 模型级 override 三态（catalog=false → override=true → 清回 NULL）"
docker exec "$PG_CONTAINER" psql -q -U gateway -d "$DB" -c "UPDATE models SET override_supports_video_input = true WHERE name='text-model';" >/dev/null
sleep 8
code=$(curl -s -o "$WORK/r5.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d "{\"model\":\"e2e-text-group\",\"messages\":[$VID]}")
check "override=true 后纯文本组 + 视频 → 200" "200" "$code"
check "上游收到的是 text-model" "text-model" \
  "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["model"])' "$WORK/upstream_last.json" 2>/dev/null || echo unknown)"

docker exec "$PG_CONTAINER" psql -q -U gateway -d "$DB" -c "UPDATE models SET override_supports_video_input = NULL WHERE name='text-model';" >/dev/null
sleep 8
code=$(curl -s -o "$WORK/r6.json" -w '%{http_code}' -X POST "http://127.0.0.1:$GW_PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $API_KEY" \
  -d "{\"model\":\"e2e-text-group\",\"messages\":[$VID]}")
check "清回 NULL 后（跟随 catalog）→ 又变 400" "400" "$code"

say "结果：PASS=$PASS FAIL=$FAIL"
[[ $FAIL -eq 0 ]] || exit 1
