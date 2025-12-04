-- call_logs 增加 request_body / response_body 列，存储完整请求与响应内容
ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS request_body  jsonb,
    ADD COLUMN IF NOT EXISTS response_body jsonb;
