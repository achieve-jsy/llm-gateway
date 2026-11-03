-- studios 承载 EHR 身份：同步按 ehr_department_id 匹配，改名只改 name，id 终身不变。
-- visible 单向：同步只会置 false（EHR 里消失），永不自处置回 true。
ALTER TABLE "studios"
  ADD COLUMN "ehr_department_id"  character varying(64) NULL,
  ADD COLUMN "visible"            boolean NOT NULL DEFAULT true,
  ADD COLUMN "source"             character varying(16) NOT NULL DEFAULT 'manual',
  ADD COLUMN "leader_name"        character varying(128) NOT NULL DEFAULT '',
  ADD COLUMN "leader_passport"    character varying(64) NOT NULL DEFAULT '',
  ADD COLUMN "leader_employee_id" character varying(64) NOT NULL DEFAULT '';

-- Postgres 唯一索引允许多个 NULL，手工工作室（无 EHR 身份）不受约束。
CREATE UNIQUE INDEX "uniq_studios_ehr_dept" ON "studios" ("ehr_department_id");
