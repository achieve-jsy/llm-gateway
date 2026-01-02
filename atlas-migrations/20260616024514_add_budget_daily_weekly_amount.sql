-- Add daily and weekly budget limits to budget_allocations
-- Each scope can now optionally have daily/weekly caps in addition to the monthly cap.
-- -1 means "no limit" (same semantics as existing amount_usd = -1 for unconfigured).

ALTER TABLE "budget_allocations"
  ADD COLUMN "daily_amount_usd" numeric(12,4) NOT NULL DEFAULT -1,
  ADD COLUMN "weekly_amount_usd" numeric(12,4) NOT NULL DEFAULT -1,
  ADD COLUMN "updated_by" uuid;

COMMENT ON COLUMN "budget_allocations"."daily_amount_usd" IS '日限额（USD），-1表示不限';
COMMENT ON COLUMN "budget_allocations"."weekly_amount_usd" IS '周限额（USD），-1表示不限';
COMMENT ON COLUMN "budget_allocations"."updated_by" IS '最后修改人 user UUID';
