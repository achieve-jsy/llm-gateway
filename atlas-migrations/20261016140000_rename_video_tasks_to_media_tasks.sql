-- C1（决策 D）：video_tasks → media_tasks —— 扩列（14 列）+ 表改名（单表承载 image/video）
--
-- 🔴 本迁移**手写**，禁止用 `atlas migrate diff` 生成。原因（已实证，见 docs/media-channels-implementation-plan.md §0.2）：
--    本仓库 atlas-migrations/ 与 GORM 期望 schema 已有 72 条既有漂移（含破坏性 DROP CONSTRAINT/DROP INDEX），
--    任何一次 diff 都会把它们一起产出并连带删掉 50+ 个既有约束/索引；而且 rename 会被 diff 理解为
--    「旧表消失 + 新表出现」= DROP TABLE + CREATE TABLE → **丢数据**。
--
-- 🔴 只 RENAME，绝不 DROP + CREATE：生产 video_tasks 有 **112 行**存量（2026-07-10 → 2026-10-15）、
--    video_task_events **327 行**，必须原样保住（本迁移已在本机带数据的演练库上验证行数不变）。
--
-- 具名对象命名策略（C1-补充的强制选择；依据 = `cd tools/atlas-loader && go run main.go` 实测 + 漂移对拍）：
--   * 二级索引：**保持历史名**（idx_video_tasks_* / idx_video_task_events_*）。Atlas 按「名 + 定义」比对索引，
--     两侧同名同定义 ⇒ 零漂移；GORM 侧 index: tag 同步保持不动（唯一改名触点是 TableName()）。
--   * 主键：**显式 RENAME**。⚠️ 与「Atlas 不看主键名」的直觉相反 —— 漂移对拍**实测证明 Atlas 会比对
--     主键名**：RENAME TABLE 后 Postgres 侧仍是 video_tasks_pkey / video_task_events_pkey，而 Atlas 的
--     期望名由表名派生（media_tasks_pkey / media_task_events_pkey），于是产生 2 条漂移语句
--     （`ALTER TABLE "media_tasks" RENAME CONSTRAINT "video_tasks_pkey" TO "media_tasks_pkey";` 等）。
--     期望旧名来自生产库实测（方案 §0.2 的对象清单）并在本机 llm_gateway 库复核一致；若某环境主键名
--     不同，本迁移会因 DDL 同事务而**整体回滚**（响亮失败，不留半改名状态）。
--   * NOT NULL 约束：**接受 Postgres 侧旧名**（video_tasks_*_not_null、video_task_events_*_not_null）。
--     实测证明 **Atlas 不比对** NOT NULL 约束名（漂移输出里零处提及）⇒ 显式 RENAME 是纯 churn、零收益。
--   * 序列：不比对、不改名（video_task_events_id_seq 保留旧名；实测漂移集合对序列名零提及）。
--   * 外键：**显式 RENAME**。Atlas **按名比对**外键（基线漂移第 225-226 行正是「DROP 旧名
--     fk_video_task_events_task + ADD 期望名 fk_video_tasks_events」）。改名后期望名变为
--     fk_media_tasks_events（GORM 侧同步声明 OnDelete:CASCADE，ON DELETE 语义与 DB 现状一致）
--     ⇒ 一次性对齐后漂移集合**减少 1 条**（72 → 71），且 CASCADE 语义不变。
--   * 同一行的另一半漂移顺手修掉：video_task_events.id 实际是 **integer(int4)**（迁移写 serial），
--     而 GORM 声明 uint → int8，Atlas 要求 bigint。

-- ── 1) 表改名 ─────────────────────────────────────────────────────────────────────
-- 外键随 ALTER TABLE ... RENAME 自动跟随（引用的是表，不写死名字）；
-- 二级索引与 NOT NULL 约束名不变，主键名见下一步。
ALTER TABLE "video_tasks" RENAME TO "media_tasks";
ALTER TABLE "video_task_events" RENAME TO "media_task_events";

-- ── 1b) 主键更名到 Atlas 期望名（由表名派生） ──────────────────────────────────────
ALTER TABLE "media_tasks" RENAME CONSTRAINT "video_tasks_pkey" TO "media_tasks_pkey";
ALTER TABLE "media_task_events" RENAME CONSTRAINT "video_task_events_pkey" TO "media_task_events_pkey";

-- ── 2) 修既有漂移 A：外键更名到 Atlas/GORM 期望名 ──────────────────────────────────
ALTER TABLE "media_task_events" RENAME CONSTRAINT "fk_video_task_events_task" TO "fk_media_tasks_events";

-- ── 3) 修既有漂移 B：event id 由 int4 提升为 bigint（对齐 GORM 的 uint） ────────────
ALTER TABLE "media_task_events" ALTER COLUMN "id" TYPE bigint;

-- ── 4) 扩列（C1 delta 14 列） ─────────────────────────────────────────────────────
-- 列定义逐项对齐 GORM tag（tools/atlas-loader 的期望 schema），否则会引入新漂移。
ALTER TABLE "media_tasks"
  ADD COLUMN "kind" character varying(16) NOT NULL DEFAULT '',
  ADD COLUMN "action" character varying(40) NOT NULL DEFAULT '',
  ADD COLUMN "vendor_task_id" character varying(191) NOT NULL DEFAULT '',
  ADD COLUMN "progress" character varying(20) NOT NULL DEFAULT '',
  ADD COLUMN "usage" jsonb NULL,
  ADD COLUMN "cost_micro_usd" bigint NOT NULL DEFAULT 0,
  ADD COLUMN "billed_at" timestamptz NULL,
  ADD COLUMN "started_at" timestamptz NULL,
  ADD COLUMN "finished_at" timestamptz NULL,
  ADD COLUMN "poll_failures" bigint NOT NULL DEFAULT 0,
  ADD COLUMN "idempotency_key" character varying(191) NULL,
  ADD COLUMN "private_data" jsonb NULL,
  ADD COLUMN "dept_id" character varying(64) NOT NULL DEFAULT '',
  ADD COLUMN "studio_id" character varying(64) NOT NULL DEFAULT '';

-- ── 5) 幂等唯一索引：(api_key_id, idempotency_key) ────────────────────────────────
-- PG 的唯一索引里 NULL 互不相等 ⇒ 存量 112 行（idempotency_key 全为 NULL）与「不带幂等键」的新行
-- 都不会冲突；只有显式携幂等键的重复提交才命中。唯一索引是幂等的**最终防线**
-- （应用层先查后插，并发下靠它兜底：撞唯一键 → 回读既有任务返回，不新建、不打上游）。
CREATE UNIQUE INDEX "uniq_media_tasks_idem" ON "media_tasks" ("api_key_id", "idempotency_key");
