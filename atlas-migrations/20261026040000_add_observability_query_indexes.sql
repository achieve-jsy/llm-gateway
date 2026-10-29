-- atlas:txmode none
-- ⚠️ 上面这行必须留在文件第 1 行：Atlas 只从文件开头的指令块读取 txmode，
--    一旦挪到中间的注释之后就会失效，CONCURRENTLY 被包进事务，
--    报 "CREATE INDEX CONCURRENTLY cannot run inside a transaction block (25001)"。
--    2026-10-26 首次上线即因此失败，勿再移动。
-- atlas-migrations/20261026040000_add_observability_query_indexes.sql
-- Feature: 补齐用量/运维查询缺的两个索引（call_logs.provider、usage_daily.stat_date）
--
-- 背景（2026-10-26 对生产库 llm_gateway_prod 的只读实测）：
--
--   ① GET /api/v1/usage/options（前端筛选下拉框，rollup_service.go 的 QueryOptions）
--        SELECT DISTINCT provider FROM call_logs
--         WHERE provider <> '' AND ts >= NOW() - INTERVAL '90 days'
--      实测 seq scan 917k buffer / 2.2~4.0 s，只为拿到 42 个 provider 取值。
--      补 (provider, ts) 后走 index-only scan；配合把窗口收窄到 7 天，
--      实测由 2.2~4.0 s 降到约 276ms。
--      ⚠️ 该列基数很低（约 42 个取值），索引的主要价值是"把窗口内的行按 provider
--         聚在一起"避免回表，而不是点查选择性。
--
--   ② usage_daily(stat_date) — 「用量与成本」30 天窗口、MyPercentiles 的
--        WHERE stat_date >= CURRENT_DATE - INTERVAL '30 days'
--      目前靠 idx_usage_daily_dept(dept_id, stat_date) 的**列前缀**在扫，
--      能用但属于碰巧：优化器一旦改走别的路径就退化成整表扫。
--      单列索引仅约 208 kB，直接补上更稳，也让 usage_daily 作为默认读路径有据可依。
--
-- 与既有 20260821120000 的关系：该迁移已建 idx_call_logs_studio_ts (studio_id, ts)，
-- 本迁移不重复建。
--
-- 代价（生产实测）：provider 索引 547 MB，usage_daily 索引 208 kB。
-- 两句都是 CONCURRENTLY，建索引全程只拿 ShareUpdateExclusiveLock，
-- 期间读写正常（已实测：建 547 MB 索引时前台 SELECT 正常返回）。
--
-- 回滚（本项目不使用 Atlas down 机制，回滚 SQL 内联备查；
-- ⚠️ 目录内放 *.down.sql 会被 Atlas 当作独立前向迁移执行，不要落盘 down 文件）：
--   DROP INDEX CONCURRENTLY IF EXISTS idx_call_logs_provider_ts;
--   DROP INDEX CONCURRENTLY IF EXISTS idx_usage_daily_stat_date;

CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_call_logs_provider_ts"
  ON "call_logs" ("provider", "ts");

CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_usage_daily_stat_date"
  ON "usage_daily" ("stat_date");
