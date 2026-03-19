-- 为 call_logs 表添加 inbound_endpoint 列，记录用户调用的 API 端点路径
-- (如 /v1/chat/completions, /v1/embeddings, /v1/rerank)
-- 使用 DO 块判断列是否存在，避免重复执行报错
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'call_logs' AND column_name = 'inbound_endpoint'
    ) THEN
        ALTER TABLE call_logs ADD COLUMN inbound_endpoint VARCHAR(128);
    END IF;
END $$;