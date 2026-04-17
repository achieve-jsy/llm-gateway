-- 支持等级子预算「下周期移除」：排期记录 set / clear 动作。
ALTER TABLE "budget_limit_schedules"
  ADD COLUMN "schedule_operation" character varying(8) NOT NULL DEFAULT 'set';

ALTER TABLE "budget_limit_schedules"
  ADD CONSTRAINT "chk_budget_limit_schedules_operation"
  CHECK ("schedule_operation" IN ('set', 'clear'));
