#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
container="llm-gateway-atlas-budget-${RANDOM}-$$"

if rg -i 'DROP[[:space:]]+INDEX[^;]*idx_budget_alert' \
  "${repo_root}"/atlas-migrations/*model_tier_budget*.sql; then
  echo "P0 Expand migration must retain idx_budget_alert" >&2
  exit 1
fi

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

atlas migrate apply \
  --dir "file://${repo_root}/atlas-migrations" \
  --url "postgres://postgres:postgres@127.0.0.1:${port}/postgres?sslmode=disable"

invalid="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT count(*)
    FROM pg_constraint
    WHERE conname IN (
      'chk_budget_alert_dimension',
      'fk_budget_alert_records_model_tier',
      'chk_expansion_dimension',
      'chk_expansion_dimension_key',
      'chk_expansion_requests_duration_type',
      'chk_expansion_requests_period_type',
      'chk_expansion_requests_priority',
      'chk_expansion_requests_sync_status',
      'fk_expansion_requests_model_tier',
      'fk_model_groups_budget_tier'
    ) AND NOT convalidated;
  "
)"
test "${invalid}" = "0"

legacy_typmods="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT string_agg(column_name || ':' || numeric_precision || ',' || numeric_scale, ';' ORDER BY column_name)
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'expansion_requests'
      AND column_name IN ('current_amount', 'requested_amount');
  "
)"
test "${legacy_typmods}" = "current_amount:12,4;requested_amount:12,4"

legacy_alert_index="$(
  docker exec "${container}" psql -U postgres -Atqc "
    SELECT to_regclass('public.idx_budget_alert') IS NOT NULL;
  "
)"
test "${legacy_alert_index}" = "t"
