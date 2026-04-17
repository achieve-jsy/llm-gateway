-- atlas:txmode none

-- Block legacy INSERT/UPDATE/DELETE only for the bounded reconciliation and
-- unique-index build. Reads remain available. A timeout rolls back DML and DDL
-- together, leaving the migration cleanly retryable.
BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';
LOCK TABLE "expansion_requests" IN SHARE ROW EXCLUSIVE MODE;

WITH ranked_pending AS (
  SELECT
    "id",
    "status",
    first_value("id") OVER (
      PARTITION BY
        "requester_id", "scope_type", "scope_id", "period_type",
        "dimension_key", "duration_type"
      ORDER BY "created_at" ASC NULLS LAST, "id" ASC
    ) AS "retained_id",
    row_number() OVER (
      PARTITION BY
        "requester_id", "scope_type", "scope_id", "period_type",
        "dimension_key", "duration_type"
      ORDER BY "created_at" ASC NULLS LAST, "id" ASC
    ) AS "position"
  FROM "expansion_requests"
  WHERE "status" IN ('pending', 'precheck_required')
)
UPDATE "expansion_requests" AS request
SET
  "status" = 'cancelled',
  "reason" = concat_ws(
    E'\n',
    nullif(request."reason", ''),
    format(
      '[P0 migration dedupe] previous_status=%s; retained_id=%s',
      ranked_pending."status",
      ranked_pending."retained_id"
    )
  ),
  "updated_at" = clock_timestamp()
FROM ranked_pending
WHERE request."id" = ranked_pending."id"
  AND ranked_pending."position" > 1;

CREATE UNIQUE INDEX "uniq_expansion_request_pending"
  ON "expansion_requests"
  ("requester_id", "scope_type", "scope_id", "period_type", "dimension_key", "duration_type")
  WHERE "status" IN ('pending', 'precheck_required');
COMMIT;
