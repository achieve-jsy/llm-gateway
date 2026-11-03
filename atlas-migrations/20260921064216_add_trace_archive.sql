-- atlas-migrations/20260921064216_add_trace_archive.sql
-- Feature: 调用链路正文归档 v3（call_traces 归档列 + trace_archive_manifests 校验清单）
--
-- 背景：call_traces 正文（jsonb）占用 TOAST 265.84 GiB，是 /data 盘写满的主因。
-- 方案见 docs/trace-archive-solution-v3.md：正文按天分块 zstd 归档到 /datalog，
-- 校验通过后置空 PG 正文；archive_status 记录三态（0=未归档 1=已归档校验 2=已置空）。
--
-- v3 相对 v2 的关键修正（评审 BLK-02）：trace_archive_manifests 增加
-- expected_total / expected_body_count / expected_body_rids_sha256 三列，
-- 构成【独立于导出谓词】的冻结期望集，消除「校验分母与导出谓词同源」的自证循环；
-- 另加 verify_state / last_inspected_at / inspect_error 支撑定期巡检。
--
-- 生成方式说明：本项目 main 的迁移目录与 GORM model 已存在既有漂移
-- （channel_monitors、quota_rpm 等 model 无对应迁移），且 tools/atlas-loader
-- 输出非确定性（约束顺序随 map 迭代变化）。因此本文件由
-- `atlas schema diff`（main model schema → 本分支 model schema，语义比较）
-- 生成，仅含本特性增量，未混入无关漂移。已执行 `atlas migrate hash` 登记校验和。
--
-- 说明：archive_status 的 DEFAULT 0 为 PG 11+ 快速默认值，5.29M 行表不触发重写。
-- Modify "call_traces" table
ALTER TABLE "public"."call_traces" ADD COLUMN "task_id" text NULL, ADD COLUMN "documents_count" integer NULL, ADD COLUMN "archive_ref" text NULL, ADD COLUMN "archive_status" smallint NOT NULL DEFAULT 0, ADD COLUMN "archived_at" timestamptz NULL;
-- Create index "idx_call_traces_archstatus_ts" to table: "call_traces"
CREATE INDEX "idx_call_traces_archstatus_ts" ON "public"."call_traces" ("archive_status", "ts") WHERE (archive_status = 1);
-- Set comment to column: "archive_status" on table: "call_traces"
COMMENT ON COLUMN "public"."call_traces"."archive_status" IS '0=未归档 1=已归档校验(正文仍热存PG) 2=已置空(正文离场,仅元数据+指针)';
-- Create "trace_archive_manifests" table
CREATE TABLE "public"."trace_archive_manifests" (
  "dt" date NOT NULL,
  "state" character varying(16) NOT NULL DEFAULT 'archiving',
  "state_rank" smallint NOT NULL DEFAULT 0,
  "expected_total" bigint NOT NULL DEFAULT 0,
  "expected_body_count" bigint NOT NULL DEFAULT 0,
  "expected_body_rids_sha256" text NULL,
  "files" jsonb NOT NULL DEFAULT '[]',
  "row_count" bigint NOT NULL DEFAULT 0,
  "uncompressed_bytes" bigint NOT NULL DEFAULT 0,
  "compressed_bytes" bigint NOT NULL DEFAULT 0,
  "sha256" text NULL,
  "min_ts" timestamptz NULL,
  "max_ts" timestamptz NULL,
  "verified_at" timestamptz NULL,
  "cleaned_at" timestamptz NULL,
  "verify_state" character varying(16) NOT NULL DEFAULT 'unknown',
  "last_inspected_at" timestamptz NULL,
  "inspect_error" text NULL,
  "attempt" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("dt")
);
-- Create index "idx_manifest_state" to table: "trace_archive_manifests"
CREATE INDEX "idx_manifest_state" ON "public"."trace_archive_manifests" ("state");
