-- 009_create_encryption_keys.sql
-- 系统配置表 + 加密密钥版本管理表

-- 通用系统配置表
CREATE TABLE IF NOT EXISTS system_configs (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    encrypted BOOLEAN NOT NULL DEFAULT false,
    description VARCHAR(500) DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 加密密钥版本管理表
CREATE TABLE IF NOT EXISTS encryption_keys (
    version INT PRIMARY KEY,
    encrypted_key TEXT NOT NULL,
    algorithm VARCHAR(50) NOT NULL DEFAULT 'AES-256-GCM',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    retired_at TIMESTAMP WITH TIME ZONE
);

COMMENT ON TABLE system_configs IS '系统配置表，存储通用配置项（含 KEK 等敏感配置）';
COMMENT ON TABLE encryption_keys IS '加密密钥版本管理表，DEK 由 KEK 加密后存储';
COMMENT ON COLUMN encryption_keys.encrypted_key IS 'KEK 加密后的 DEK（Base64 编码）';
COMMENT ON COLUMN encryption_keys.status IS '密钥状态：active（当前使用）/ rotating（轮换中，仍可解密）/ retired（已停用）';
