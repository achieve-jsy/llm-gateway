-- AI 应用聚合页本地表：工作室主数据 + EHR 组织镜像 + 应用 + 访问日志
--
-- 生成方式：atlas migrate diff --env local（GORM provider 直出），随后**只保留本次新增的 4 张表**，
-- 剔除了 diff 中与本改动无关的存量漂移（alert_events / api_keys / audit_logs / models / users
-- 等 20+ 张表的 integer→bigint、DROP DEFAULT、索引重建）。原因见
-- 20261014063200_add_call_logs_call_type.sql 头部说明：本工作区的 diff 基线长期漂移，
-- 混入会让无关表被改写。下方语句与 diff 输出逐字一致，只去掉了 "public". 前缀。
--
-- 为什么工作室主数据用新表 ai_app_studios，而不是给 legacy 的 studios 加字段：
-- studios / dept_studio_map / user_roles 只存在于 legacy migrations/043，没有纳入 Atlas
-- 迁移目录；一旦写进 GORM model，diff 会产出 CREATE TABLE "studios"，无法在已有库重放。
--
-- 表关系：
--   ai_app_studios        工作室（= EHR departmentType=2 的事业部，可人工覆盖）
--   ai_app_departments    EHR 组织节点镜像（99 条），用于「用户部门 → 事业部」上溯
--   ai_apps               AI 应用，按 studio_id 归属
--   ai_app_hub_login_logs H5 访问日志，异步写入

-- Create "ai_app_departments" table
CREATE TABLE "ai_app_departments" (
  "department_id" character varying(64) NOT NULL,
  "name" character varying(128) NOT NULL DEFAULT '',
  "parent_id" character varying(64) NOT NULL DEFAULT '',
  "department_type" integer NOT NULL DEFAULT 0,
  "department_path" character varying(512) NOT NULL DEFAULT '',
  "studio_id" uuid NULL,
  "leader_name" character varying(128) NOT NULL DEFAULT '',
  "leader_passport" character varying(128) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("department_id")
);
-- Create index "idx_ai_app_departments_parent" to table: "ai_app_departments"
CREATE INDEX "idx_ai_app_departments_parent" ON "ai_app_departments" ("parent_id");
-- Create index "idx_ai_app_departments_studio" to table: "ai_app_departments"
CREATE INDEX "idx_ai_app_departments_studio" ON "ai_app_departments" ("studio_id");
-- Create "ai_app_hub_login_logs" table
CREATE TABLE "ai_app_hub_login_logs" (
  "id" bigserial NOT NULL,
  "user_id" character varying(64) NOT NULL DEFAULT '',
  "passport" character varying(128) NOT NULL DEFAULT '',
  "user_name" character varying(128) NOT NULL DEFAULT '',
  "dept_id" character varying(64) NOT NULL DEFAULT '',
  "studio_id" uuid NULL,
  "scope" character varying(16) NOT NULL DEFAULT '',
  "ip" character varying(64) NOT NULL DEFAULT '',
  "user_agent" character varying(255) NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "idx_ai_app_hub_login_logs_created_at" to table: "ai_app_hub_login_logs"
CREATE INDEX "idx_ai_app_hub_login_logs_created_at" ON "ai_app_hub_login_logs" ("created_at");
-- Create "ai_app_studios" table
CREATE TABLE "ai_app_studios" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "department_id" character varying(64) NULL,
  "name" character varying(128) NOT NULL DEFAULT '',
  "leader_name" character varying(128) NOT NULL DEFAULT '',
  "leader_passport" character varying(128) NOT NULL DEFAULT '',
  "leader_employee_id" character varying(64) NOT NULL DEFAULT '',
  "source" character varying(16) NOT NULL DEFAULT 'ehr',
  "visible" boolean NOT NULL DEFAULT true,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "idx_ai_app_studios_leader" to table: "ai_app_studios"
CREATE INDEX "idx_ai_app_studios_leader" ON "ai_app_studios" ("leader_passport");
-- Create index "uniq_ai_app_studios_department_id" to table: "ai_app_studios"
CREATE UNIQUE INDEX "uniq_ai_app_studios_department_id" ON "ai_app_studios" ("department_id");
-- Create "ai_apps" table
CREATE TABLE "ai_apps" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "studio_id" uuid NOT NULL,
  "name" character varying(128) NOT NULL,
  "description" character varying(200) NOT NULL DEFAULT '',
  "icon_url" character varying(500) NOT NULL DEFAULT '',
  "url" character varying(1000) NOT NULL DEFAULT '',
  "heat" smallint NOT NULL DEFAULT 0,
  "is_new" boolean NOT NULL DEFAULT false,
  "status" character varying(16) NOT NULL DEFAULT 'published',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_by" character varying(128) NOT NULL DEFAULT '',
  "updated_by" character varying(128) NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "heat_between_0_and_3" CHECK ((heat >= 0) AND (heat <= 3))
);
-- Create index "idx_ai_apps_studio_status" to table: "ai_apps"
CREATE INDEX "idx_ai_apps_studio_status" ON "ai_apps" ("studio_id", "status");
-- Create index "uniq_ai_apps_studio_name" to table: "ai_apps"
CREATE UNIQUE INDEX "uniq_ai_apps_studio_name" ON "ai_apps" ("studio_id", "name");
