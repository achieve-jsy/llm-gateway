-- 010_create_health_check.sql
-- 健康检测配置与记录

-- 健康检测策略配置表
CREATE TABLE IF NOT EXISTS health_check_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enabled BOOLEAN NOT NULL DEFAULT false,
    interval_seconds INT NOT NULL DEFAULT 300,
    failure_threshold INT NOT NULL DEFAULT 3,
    failure_action VARCHAR(20) NOT NULL DEFAULT 'auto_disable',
    check_providers BOOLEAN NOT NULL DEFAULT true,
    check_models BOOLEAN NOT NULL DEFAULT true,
    timeout_seconds INT NOT NULL DEFAULT 15,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 健康检测记录表
CREATE TABLE IF NOT EXISTS health_check_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type VARCHAR(20) NOT NULL,
    target_id UUID NOT NULL,
    target_name VARCHAR(200) NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL,
    response_time_ms INT NOT NULL DEFAULT 0,
    error_message TEXT DEFAULT '',
    checked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_health_check_logs_target ON health_check_logs(target_type, target_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_health_check_logs_checked_at ON health_check_logs(checked_at DESC);

-- 插入默认配置（单例）
INSERT INTO health_check_configs (enabled, interval_seconds, failure_threshold, failure_action)
SELECT false, 300, 3, 'auto_disable'
WHERE NOT EXISTS (SELECT 1 FROM health_check_configs LIMIT 1);

COMMENT ON TABLE health_check_configs IS '健康检测策略配置（单例，只有一条记录）';
COMMENT ON TABLE health_check_logs IS '健康检测执行记录';
COMMENT ON COLUMN health_check_configs.failure_action IS '失败处理策略：auto_disable（自动禁用）/ alert_only（仅记录）';
COMMENT ON COLUMN health_check_logs.target_type IS '检测目标类型：provider / model';
