-- 062: call_traces 归档列 + 归档校验清单表 trace_archive_manifests
-- 参考 docs/trace-archive-final-design.md §2.1（①c 最简版）

-- 1. call_traces 增加归档相关列（置空后 jsonb 抽取失效，列表页仍需 task_id/documents_count）
ALTER TABLE call_traces
    ADD COLUMN IF NOT EXISTS task_id         TEXT,
    ADD COLUMN IF NOT EXISTS documents_count INT,
    ADD COLUMN IF NOT EXISTS archive_ref     TEXT,
    ADD COLUMN IF NOT EXISTS archive_status  SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS archived_at     TIMESTAMPTZ;

COMMENT ON COLUMN call_traces.archive_status IS
  '0=未归档 1=已归档校验(正文仍热存PG) 2=已置空(正文离场,仅元数据+指针)';

-- 2. 已归档校验(archive_status=1)的扫描索引
CREATE INDEX IF NOT EXISTS idx_call_traces_archstatus_ts
    ON call_traces (archive_status, ts) WHERE archive_status = 1;

-- 3. 归档校验清单表
CREATE TABLE IF NOT EXISTS trace_archive_manifests (
    dt            DATE PRIMARY KEY,
    state         VARCHAR(16) NOT NULL DEFAULT 'archiving',   -- archiving / verified / cleaned(后续)
    state_rank    SMALLINT    NOT NULL DEFAULT 0,
    files         JSONB       NOT NULL DEFAULT '[]',
    row_count     BIGINT      NOT NULL DEFAULT 0,
    pg_row_count  BIGINT      NOT NULL DEFAULT 0,
    sha256        TEXT,
    min_ts        TIMESTAMPTZ,
    max_ts        TIMESTAMPTZ,
    verified_at   TIMESTAMPTZ,
    cleaned_at    TIMESTAMPTZ,
    attempt       INT         NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_manifest_state ON trace_archive_manifests (state);
