-- 040: api_keys 补充缺失字段
-- log_enabled：控制单条 key 是否将调用记录写入 call_logs（默认开启）
-- studio_id：所属事业部 ID
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS log_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS studio_id   VARCHAR(128) NOT NULL DEFAULT '';
