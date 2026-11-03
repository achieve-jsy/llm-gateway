-- 062 down：与 up 对称，整体可回退（参考 docs/trace-archive-final-design.md §16）

-- 1. 删除归档校验清单表
DROP TABLE IF EXISTS trace_archive_manifests;

-- 2. 删除 call_traces 归档列（含 archive_status 上的部分索引；显式 DROP 索引保证对称）
DROP INDEX IF EXISTS idx_call_traces_archstatus_ts;
ALTER TABLE call_traces
    DROP COLUMN IF EXISTS archive_status,
    DROP COLUMN IF EXISTS archive_ref,
    DROP COLUMN IF EXISTS documents_count,
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS archived_at;
