-- 039: 新增 stage_ttfb_ms 字段（上游 TTFB，仅流式请求记录）
ALTER TABLE call_logs  ADD COLUMN IF NOT EXISTS stage_ttfb_ms INT;
ALTER TABLE call_traces ADD COLUMN IF NOT EXISTS stage_ttfb_ms INT;
