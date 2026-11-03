#!/bin/bash
# 测试环境启动脚本

set -e

echo "🧪 Bedrock upstream_model_id 修复测试"
echo "======================================"

# 1. 检查是否在 worktree 分支
BRANCH=$(git branch --show-current)
if [ "$BRANCH" != "worktree-fix+bedrock-upstream-model-id" ]; then
    echo "❌ 错误：当前不在测试分支"
    exit 1
fi

echo "✅ 分支: $BRANCH"

# 2. 编译测试版本
echo ""
echo "📦 编译测试版本..."
go build -o bin/gateway-test ./cmd/gateway
go build -o bin/control-plane-test ./cmd/control-plane

echo "✅ 编译完成"

# 3. 准备测试环境变量
cat > .env.test <<EOF
# 测试环境配置（使用临时端口避免冲突）
APP_ENV=test
GATEWAY_PORT=18080
CONTROL_PLANE_PORT=18081

# 连接生产数据库（只读查询，不写入）
DATABASE_URL=host=192.0.2.10 port=5433 user=llm_gateway password=CHANGE_ME_PASSWORD dbname=llm_gateway sslmode=disable

# Redis
REDIS_ADDR=192.0.2.10:6379
REDIS_PASSWORD=CHANGE_ME_REDIS_PASSWORD
REDIS_DB=0

# 认证（使用测试 token）
JWT_SECRET=test-jwt-secret-$(date +%s)
INTERNAL_AUTH_TOKEN=test-internal-token-$(date +%s)
ENCRYPTION_KEY=test-encryption-key-32bytes!!

# OAuth（测试用）
OAUTH_ISSUER_URL=https://oauth.example.net
OAUTH_CLIENT_ID=test-client
OAUTH_CLIENT_SECRET=test-secret
OAUTH_REDIRECT_URI=http://localhost:18081/auth/callback
OAUTH_SCOPES=openid profile email
FRONTEND_URL=http://localhost:18081

# Control Plane URL
CONTROL_PLANE_URL=http://localhost:18081
AUTH_URL=http://localhost:18081
EOF

echo "✅ 测试配置已生成: .env.test"

# 4. 先更新数据库配置（只更新测试用的模型）
echo ""
echo "📝 准备数据库测试数据..."
cat > /tmp/test-upstream-model-id.sql <<'SQL'
-- 查询当前 Bedrock 模型配置
SELECT
    m.id,
    m.name,
    m.upstream_model_id,
    p.name as provider_name
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.definition_id = 'bedrock'
  AND m.name LIKE '%claude-sonnet-4%'
  AND m.enabled = true
LIMIT 5;

-- 备份当前配置
CREATE TEMP TABLE IF NOT EXISTS models_backup AS
SELECT id, name, upstream_model_id
FROM models
WHERE provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND name LIKE '%claude-sonnet-4%';

-- 更新测试数据（只更新一个模型用于测试）
UPDATE models
SET upstream_model_id = 'us.anthropic.claude-sonnet-4-6-20250514-v1:0'
WHERE name = 'global.anthropic.claude-sonnet-4-6'
  AND provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND enabled = true;

-- 验证更新
SELECT
    m.name,
    m.upstream_model_id,
    CASE
        WHEN m.upstream_model_id = '' THEN '❌ 未配置'
        ELSE '✅ 已配置'
    END as status
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.definition_id = 'bedrock'
  AND m.name LIKE '%claude-sonnet-4%'
  AND m.enabled = true;
SQL

echo "✅ SQL 脚本已生成: /tmp/test-upstream-model-id.sql"

echo ""
echo "======================================"
echo "🚀 测试环境准备完成"
echo ""
echo "下一步："
echo "1. 执行数据库配置: PGPASSWORD=CHANGE_ME_PASSWORD psql -h 192.0.2.10 -p 5433 -U llm_gateway -d llm_gateway -f /tmp/test-upstream-model-id.sql"
echo "2. 启动服务: ./scripts/start-test-services.sh"
echo "3. 运行测试: ./scripts/run-bedrock-test.sh"
echo "4. 清理: ./scripts/cleanup-test.sh"
