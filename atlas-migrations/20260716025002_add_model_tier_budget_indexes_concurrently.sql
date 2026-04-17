-- atlas:txmode none

CREATE INDEX CONCURRENTLY "idx_call_logs_budget_block_code"
  ON "call_logs" ("budget_block_code");
CREATE INDEX CONCURRENTLY "idx_call_logs_budget_tier_ts"
  ON "call_logs" ("budget_tier_id", "ts");

-- Keep legacy idx_budget_alert through Expand. P3 pre-enable Contract removes it.
CREATE INDEX CONCURRENTLY "idx_budget_alert_records_dimension_type"
  ON "budget_alert_records" ("dimension_type");
CREATE INDEX CONCURRENTLY "idx_budget_alert_records_model_tier_id"
  ON "budget_alert_records" ("model_tier_id");
CREATE UNIQUE INDEX CONCURRENTLY "uniq_budget_alert_dimension"
  ON "budget_alert_records"
  ("scope_type", "scope_id", "period_type", "period", "threshold", "dimension_key");

CREATE INDEX CONCURRENTLY "idx_expansion_requests_dimension_key"
  ON "expansion_requests" ("dimension_key");
CREATE INDEX CONCURRENTLY "idx_expansion_requests_dimension_type"
  ON "expansion_requests" ("dimension_type");
CREATE INDEX CONCURRENTLY "idx_expansion_requests_duration_type"
  ON "expansion_requests" ("duration_type");
CREATE INDEX CONCURRENTLY "idx_expansion_requests_model_tier_id"
  ON "expansion_requests" ("model_tier_id");

CREATE INDEX CONCURRENTLY "idx_model_groups_budget_tier"
  ON "model_groups" ("budget_tier_id")
  WHERE "budget_tier_id" IS NOT NULL AND "deleted_at" IS NULL;
