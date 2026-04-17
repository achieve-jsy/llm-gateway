#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
container="llm-gateway-atlas-budget-upgrade-${RANDOM}-$$"
dsn=""

cleanup() {
  docker rm -f "${container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --rm -d --name "${container}" -e POSTGRES_PASSWORD=postgres -P postgres:16 >/dev/null
port="$(docker port "${container}" 5432/tcp | awk -F: 'NR == 1 {print $NF}')"
for _ in $(seq 1 60); do
  if docker exec "${container}" pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${container}" pg_isready -U postgres >/dev/null
dsn="postgres://postgres:postgres@127.0.0.1:${port}/postgres?sslmode=disable"

atlas migrate apply \
  --dir "file://${repo_root}/atlas-migrations" \
  --url "${dsn}" \
  --to-version 20260710200000

requester_id="10000000-0000-0000-0000-000000000001"
kept_id="20000000-0000-0000-0000-000000000001"
duplicate_id="20000000-0000-0000-0000-000000000002"
precheck_id="20000000-0000-0000-0000-000000000003"
tie_kept_id="30000000-0000-0000-0000-000000000001"
tie_duplicate_id="30000000-0000-0000-0000-000000000002"
concurrent_writer_id="40000000-0000-0000-0000-000000000001"
docker exec "${container}" psql -U postgres -v ON_ERROR_STOP=1 -c "
  INSERT INTO expansion_requests
    (id, scope_type, scope_id, requester_id, current_amount, requested_amount,
     reason, status, current_level, period_type, created_at, updated_at)
  VALUES
    ('${kept_id}', 'member', 'member-1', '${requester_id}', 10, 20,
     'original earliest', 'pending', 'dept', 'monthly', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z'),
    ('${duplicate_id}', 'member', 'member-1', '${requester_id}', 10, 30,
     'duplicate pending', 'pending', 'dept', 'monthly', '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z'),
    ('${precheck_id}', 'member', 'member-1', '${requester_id}', 10, 40,
     'duplicate pending 2', 'pending', 'dept', 'monthly', '2026-07-03T00:00:00Z', '2026-07-03T00:00:00Z'),
    ('${tie_kept_id}', 'member', 'member-2', '${requester_id}', 10, 20,
     'tie lower id', 'pending', 'dept', 'monthly', '2026-07-04T00:00:00Z', '2026-07-04T00:00:00Z'),
    ('${tie_duplicate_id}', 'member', 'member-2', '${requester_id}', 10, 30,
     'tie higher id', 'pending', 'dept', 'monthly', '2026-07-04T00:00:00Z', '2026-07-04T00:00:00Z');
"

atlas migrate apply \
  --dir "file://${repo_root}/atlas-migrations" \
  --url "${dsn}" \
  --to-version 20260711132830

# Simulate an old service holding a write transaction while the migration
# attempts to acquire its bounded table lock. The first attempt must roll back
# cleanly; once the writer commits, retry must reconcile its duplicate too.
docker exec "${container}" psql -U postgres -v ON_ERROR_STOP=1 -c "
  BEGIN;
  INSERT INTO expansion_requests
    (id, scope_type, scope_id, requester_id, current_amount, requested_amount,
     reason, status, current_level, period_type, created_at, updated_at)
  VALUES
    ('${concurrent_writer_id}', 'member', 'member-1', '${requester_id}', 10, 60,
     'concurrent old writer', 'pending', 'dept', 'monthly', '2026-07-05T00:00:00Z', '2026-07-05T00:00:00Z');
  SELECT pg_sleep(4);
  COMMIT;
" >/dev/null &
writer_pid=$!
sleep 0.5

if first_attempt_output="$(
  atlas migrate apply \
    --dir "file://${repo_root}/atlas-migrations" \
    --url "${dsn}" 2>&1
)"; then
  echo "migration unexpectedly bypassed the bounded expansion write lock" >&2
  wait "${writer_pid}"
  exit 1
fi
if [[ "${first_attempt_output}" != *"lock timeout"* ]]; then
  echo "migration failed for an unexpected reason: ${first_attempt_output}" >&2
  wait "${writer_pid}"
  exit 1
fi
wait "${writer_pid}"

invalid_after_retryable_failure="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT count(*)
    FROM pg_index
    WHERE indexrelid = to_regclass('public.uniq_expansion_request_pending')
      AND NOT indisvalid;
  "
)"
test "${invalid_after_retryable_failure}" = "0"

atlas migrate apply \
  --dir "file://${repo_root}/atlas-migrations" \
  --url "${dsn}"

index_state="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT indisvalid::text || ':' || indisready::text
    FROM pg_index
    WHERE indexrelid = to_regclass('public.uniq_expansion_request_pending');
  "
)"
test "${index_state}" = "true:true"

pending="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT id::text || ':' || status
    FROM expansion_requests
    WHERE status IN ('pending', 'precheck_required')
    ORDER BY created_at NULLS LAST, id;
  "
)"
test "${pending}" = "${kept_id}:pending
${tie_kept_id}:pending"

cancelled_count="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT count(*)
    FROM expansion_requests
    WHERE id IN ('${duplicate_id}', '${precheck_id}', '${concurrent_writer_id}')
      AND status = 'cancelled'
      AND reason LIKE '%[P0 migration dedupe]%'
      AND reason LIKE '%retained_id=${kept_id}%'
      AND (
        (id = '${duplicate_id}' AND reason LIKE '%duplicate pending%') OR
        (id = '${precheck_id}' AND reason LIKE '%duplicate pending 2%') OR
        (id = '${concurrent_writer_id}' AND reason LIKE '%concurrent old writer%')
      );
  "
)"
test "${cancelled_count}" = "3"

tie_cancelled="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT count(*)
    FROM expansion_requests
    WHERE id = '${tie_duplicate_id}'
      AND status = 'cancelled'
      AND reason LIKE '%tie higher id%'
      AND reason LIKE '%retained_id=${tie_kept_id}%';
  "
)"
test "${tie_cancelled}" = "1"

if docker exec "${container}" psql -U postgres -v ON_ERROR_STOP=1 -c "
  INSERT INTO expansion_requests
    (id, scope_type, scope_id, requester_id, current_amount, requested_amount,
     status, current_level, period_type, created_at)
  VALUES
    (gen_random_uuid(), 'member', 'member-1', '${requester_id}', 10, 50,
     'pending', 'dept', 'monthly', now());
" >/dev/null 2>&1; then
  echo "pending expansion unique index did not reject a duplicate" >&2
  exit 1
fi
