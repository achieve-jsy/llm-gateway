#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE_DIR="${ROOT_DIR}/deploy/blue-green"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_contains() {
    local file="$1" expected="$2"
    grep -Fq "$expected" "$file" || fail "${file} does not contain: ${expected}"
}

assert_not_contains() {
    local file="$1" unexpected="$2"
    if grep -Fq "$unexpected" "$file"; then
        fail "${file} unexpectedly contains: ${unexpected}"
    fi
}

new_fixture() {
    local name="$1"
    local fixture="${TEST_ROOT}/${name}"
    mkdir -p "${fixture}/bin" "${fixture}/deploy"
    cp "${SOURCE_DIR}/deploy-blue-green.sh" "${fixture}/deploy/"
    cp "${SOURCE_DIR}"/docker-compose.*.yml "${fixture}/deploy/"
    cp "${SOURCE_DIR}"/upstream-*.conf "${fixture}/deploy/"
    printf 'blue\n' > "${fixture}/deploy/.active-color"
    cp "${SOURCE_DIR}/upstream-blue.conf" "${fixture}/upstream-active.conf"
    : > "${fixture}/calls.log"

    cat > "${fixture}/bin/docker" <<'EOF'
#!/bin/bash
echo "docker $*" >> "$CALLS_LOG"
if [ "${1:-}" = "ps" ]; then
    printf '%s\n' "${DOCKER_PS_OUTPUT:-llm-blue-gateway}"
fi
EOF

    cat > "${fixture}/bin/nginx" <<'EOF'
#!/bin/bash
echo "nginx $*" >> "$CALLS_LOG"
exit 0
EOF

    cat > "${fixture}/bin/curl" <<'EOF'
#!/bin/bash
url="${*: -1}"
echo "curl $url" >> "$CALLS_LOG"
case "$url" in
    http://localhost:8080/ready|http://localhost:8082/ready)
        count_file="${CALLS_LOG}.ready-count"
        count=0
        [ -f "$count_file" ] && count=$(cat "$count_file")
        count=$((count + 1))
        printf '%s' "$count" > "$count_file"
        version=7
        model_count=12
        if [ "${FLAP_READY_VERSION:-false}" = "true" ]; then
            version=$count
        fi
        if [ "${EMPTY_READY_MODELS:-false}" = "true" ]; then
            model_count=0
        fi
        # checks.group_cache 可控：READY_GROUP_CACHE=missing 时整体省略该字段（模拟不含该字段的
        # 旧网关镜像），其余取值按字面写入（默认 ok）。其余字段保持不变。
        group_cache_field=',"group_cache":"ok"'
        case "${READY_GROUP_CACHE:-ok}" in
            missing) group_cache_field="" ;;
            *)       group_cache_field=",\"group_cache\":\"${READY_GROUP_CACHE:-ok}\"" ;;
        esac
        printf '{"status":"ready","checks":{"redis":"ok","registry":"ok","scheduler":"ok"%s},"details":{"version":%s,"model_count":%s,"source":"redis-snapshot","loaded_at":"2026-07-17T00:00:00Z"},"capabilities":{"tier_snapshot_schema":1,"redis_v2":true,"snapshot_revision":4}}\n' "$group_cache_field" "$version" "$model_count"
        ;;
    http://localhost:8081/healthz|http://localhost:8083/healthz)
        exit 0
        ;;
    http://nginx.test/health)
        [ "${FAIL_SMOKE:-false}" != "true" ]
        ;;
    *)
        exit 0
        ;;
esac
EOF

    cat > "${fixture}/bin/flock" <<'EOF'
#!/bin/bash
echo "flock $*" >> "$CALLS_LOG"
[ "${LOCK_BUSY:-false}" != "true" ]
EOF

    cat > "${fixture}/bin/sleep" <<'EOF'
#!/bin/bash
exit 0
EOF

    chmod +x "${fixture}/bin/"*
    printf '%s\n' "$fixture"
}

run_deploy() {
    local fixture="$1"
    shift
    env \
        PATH="${fixture}/bin:${PATH}" \
        CALLS_LOG="${fixture}/calls.log" \
        NGINX_UPSTREAM="${fixture}/upstream-active.conf" \
        DEPLOY_LOCK_FILE="${fixture}/deploy.lock" \
        NGINX_SMOKE_URL="http://nginx.test/health" \
        SAFE_SWITCH_ENABLED=true \
        KEEP_OLD_ENV=true \
        HEALTH_RETRIES=3 \
        HEALTH_INTERVAL=0 \
        READY_STABLE_CHECKS=2 \
        READY_STABLE_INTERVAL=0 \
        SMOKE_RETRIES=1 \
        SMOKE_INTERVAL=0 \
        "$@" \
        bash "${fixture}/deploy/deploy-blue-green.sh"
}

test_success_keeps_old_environment() {
    local fixture
    fixture=$(new_fixture success)

    run_deploy "$fixture"

    [ "$(cat "${fixture}/deploy/.active-color")" = "green" ] || fail "active color was not switched to green"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8082"
    assert_not_contains "${fixture}/calls.log" "compose -f ${fixture}/deploy/docker-compose.blue.yml -p llm-blue down"
}

test_failed_smoke_restores_previous_upstream() {
    local fixture
    fixture=$(new_fixture smoke-failure)

    if run_deploy "$fixture" FAIL_SMOKE=true; then
        fail "deployment should fail when nginx smoke check fails"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed after failed smoke check"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8080"
    assert_contains "${fixture}/calls.log" "compose -f ${fixture}/deploy/docker-compose.green.yml -p llm-green down --timeout 30"
}

test_flapping_ready_version_blocks_cutover() {
    local fixture
    fixture=$(new_fixture ready-flap)

    if run_deploy "$fixture" HEALTH_RETRIES=3 FLAP_READY_VERSION=true >"${fixture}/deploy.log" 2>&1; then
        fail "deployment should fail when gateway ready version does not stabilize"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed after unstable ready payload"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8080"
    assert_not_contains "${fixture}/calls.log" "nginx -s reload"
    # 失败原因必须是「指纹取到了但版本反复变化」，而不是别的门禁（否则判别力会被静默削弱）。
    assert_contains "${fixture}/deploy.log" "green gateway ready fingerprint stable=1/2"
    assert_not_contains "${fixture}/deploy.log" "green gateway /ready payload not cutover-ready yet"
}

test_empty_ready_models_blocks_cutover() {
    local fixture
    fixture=$(new_fixture ready-empty)

    if run_deploy "$fixture" HEALTH_RETRIES=2 EMPTY_READY_MODELS=true >"${fixture}/deploy.log" 2>&1; then
        fail "deployment should fail when gateway ready model_count is empty"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed with empty ready models"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8080"
    assert_not_contains "${fixture}/calls.log" "nginx -s reload"
    # 失败原因必须是「model_count=0 取不到切流指纹」，而不是版本不稳定或健康检查失败。
    assert_contains "${fixture}/deploy.log" "green gateway /ready payload not cutover-ready yet"
    assert_not_contains "${fixture}/deploy.log" "green gateway ready fingerprint stable="
    assert_not_contains "${fixture}/deploy.log" "Health check failed for green"
}

# 冷启动组名缓存未就绪（checks.group_cache=not_initialized）必须阻断切流 ——
# 这是 2026-10-16 线上蓝绿事故（绿网关组名缓存为空仍被切流、合法组名请求被误判直连）的门禁回归。
test_group_cache_not_initialized_blocks_cutover() {
    local fixture
    fixture=$(new_fixture cache-not-initialized)

    if run_deploy "$fixture" READY_GROUP_CACHE=not_initialized HEALTH_RETRIES=2 >"${fixture}/deploy.log" 2>&1; then
        fail "deployment should fail when /ready reports group_cache=not_initialized"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed with group_cache=not_initialized"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8080"
    assert_not_contains "${fixture}/calls.log" "nginx -s reload"
    # 目标环境必须先起来并通过 health_check，否则失败原因就不是切流门禁。
    assert_contains "${fixture}/calls.log" "compose -f ${fixture}/deploy/docker-compose.green.yml -p llm-green up -d"
    # 失败原因必须是 cutover-ready 门禁本身，而不是健康检查 / capabilities / 锁竞争等其它原因。
    assert_contains "${fixture}/deploy.log" "green gateway /ready payload not cutover-ready yet"
    assert_contains "${fixture}/deploy.log" "did not become cutover-ready; deploy aborted"
    assert_not_contains "${fixture}/deploy.log" "Health check failed for green"
    assert_not_contains "${fixture}/deploy.log" "does not advertise tier_snapshot_schema=1"
    assert_not_contains "${fixture}/deploy.log" "Timed out waiting for another deployment"
}

# 旧网关镜像的 /ready 不含 checks.group_cache 字段，同样必须阻断切流
# （回退到这类旧版本要走 deploy-blue-green.sh --rollback，见 deploy/blue-green/README.md）。
test_missing_group_cache_field_blocks_cutover() {
    local fixture
    fixture=$(new_fixture cache-missing)

    if run_deploy "$fixture" READY_GROUP_CACHE=missing HEALTH_RETRIES=2 >"${fixture}/deploy.log" 2>&1; then
        fail "deployment should fail when /ready payload has no group_cache field"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed without group_cache field"
    assert_contains "${fixture}/upstream-active.conf" "127.0.0.1:8080"
    assert_not_contains "${fixture}/calls.log" "nginx -s reload"
    assert_contains "${fixture}/calls.log" "compose -f ${fixture}/deploy/docker-compose.green.yml -p llm-green up -d"
    assert_contains "${fixture}/deploy.log" "green gateway /ready payload not cutover-ready yet"
    assert_contains "${fixture}/deploy.log" "did not become cutover-ready; deploy aborted"
    assert_not_contains "${fixture}/deploy.log" "Health check failed for green"
    assert_not_contains "${fixture}/deploy.log" "does not advertise tier_snapshot_schema=1"
}

test_concurrent_deploy_waits_then_times_out() {
    local fixture
    fixture=$(new_fixture lock-busy)

    if run_deploy "$fixture" LOCK_BUSY=true; then
        fail "deployment should fail when another deployment owns the lock"
    fi

    [ "$(cat "${fixture}/deploy/.active-color")" = "blue" ] || fail "active color changed while lock was busy"
    assert_contains "${fixture}/calls.log" "flock -w 600 9"
    assert_not_contains "${fixture}/calls.log" "compose"
}

test_retained_target_is_removed_before_replacement() {
    local fixture down_line up_line
    fixture=$(new_fixture retained-target)
    printf 'green\n' > "${fixture}/deploy/.active-color"
    cp "${SOURCE_DIR}/upstream-green.conf" "${fixture}/upstream-active.conf"

    run_deploy "$fixture" DOCKER_PS_OUTPUT=$'llm-green-gateway\nllm-blue-gateway'

    down_line=$(grep -n "compose -f ${fixture}/deploy/docker-compose.blue.yml -p llm-blue down --timeout 30" "${fixture}/calls.log" | cut -d: -f1)
    up_line=$(grep -n "compose -f ${fixture}/deploy/docker-compose.blue.yml -p llm-blue up -d --remove-orphans" "${fixture}/calls.log" | cut -d: -f1)
    [ -n "$down_line" ] || fail "retained blue environment was not removed"
    [ -n "$up_line" ] || fail "replacement blue environment was not started"
    [ "$down_line" -lt "$up_line" ] || fail "retained blue environment was removed after replacement start"
}

test_success_keeps_old_environment
test_failed_smoke_restores_previous_upstream
test_flapping_ready_version_blocks_cutover
test_empty_ready_models_blocks_cutover
test_group_cache_not_initialized_blocks_cutover
test_missing_group_cache_field_blocks_cutover
test_concurrent_deploy_waits_then_times_out
test_retained_target_is_removed_before_replacement
echo "PASS: blue-green deployment tests"
