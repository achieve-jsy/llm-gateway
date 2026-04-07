ALTER TABLE "budget_alert_records"
  VALIDATE CONSTRAINT "chk_budget_alert_dimension",
  VALIDATE CONSTRAINT "fk_budget_alert_records_model_tier";

ALTER TABLE "expansion_requests"
  VALIDATE CONSTRAINT "chk_expansion_dimension",
  VALIDATE CONSTRAINT "chk_expansion_dimension_key",
  VALIDATE CONSTRAINT "chk_expansion_requests_duration_type",
  VALIDATE CONSTRAINT "chk_expansion_requests_period_type",
  VALIDATE CONSTRAINT "chk_expansion_requests_priority",
  VALIDATE CONSTRAINT "chk_expansion_requests_sync_status",
  VALIDATE CONSTRAINT "fk_expansion_requests_model_tier";

ALTER TABLE "model_groups"
  VALIDATE CONSTRAINT "fk_model_groups_budget_tier";
