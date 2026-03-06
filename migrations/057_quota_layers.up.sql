-- 四层配额体系：用户/模型组/模型/Service Key 配额字段
-- NULL = 继承全局默认；0 = 显式不限

-- 用户配额（NULL = 继承 default_user）
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS quota_rpm INT NULL,
  ADD COLUMN IF NOT EXISTS quota_max_concurrency INT NULL;

-- 模型组配额（NULL = 不限，运行时按主体独立计数）
ALTER TABLE model_groups
  ADD COLUMN IF NOT EXISTS quota_rpm INT NULL,
  ADD COLUMN IF NOT EXISTS quota_max_concurrency INT NULL;

-- 模型配额（NULL = 不限，运行时按主体独立计数）
ALTER TABLE models
  ADD COLUMN IF NOT EXISTS quota_rpm INT NULL,
  ADD COLUMN IF NOT EXISTS quota_max_concurrency INT NULL;

-- Service Key 主体配额（key_type=service；NULL = 继承 default_service_key）
ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS quota_rpm INT NULL,
  ADD COLUMN IF NOT EXISTS quota_max_concurrency INT NULL;
