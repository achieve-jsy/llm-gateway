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
        printf '%s\n' '{"capabilities":{"tier_snapshot_schema":1,"redis_v2":true}}'
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
        HEALTH_RETRIES=1 \
        HEALTH_INTERVAL=0 \
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
test_concurrent_deploy_waits_then_times_out
test_retained_target_is_removed_before_replacement
echo "PASS: blue-green deployment tests"
