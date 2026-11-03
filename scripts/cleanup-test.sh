#!/bin/bash
# 清理测试环境

echo "🧹 清理测试环境"
echo "======================================"

# 1. 停止服务
./scripts/stop-test-services.sh

# 2. 恢复数据库配置（可选）
echo ""
read -p "是否恢复数据库配置到测试前状态？(y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "恢复数据库配置..."
    PGPASSWORD=CHANGE_ME_PASSWORD psql -h 192.0.2.10 -p 5433 -U llm_gateway -d llm_gateway <<SQL
UPDATE models
SET upstream_model_id = ''
WHERE name = 'global.anthropic.claude-sonnet-4-6'
  AND provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND enabled = true;

SELECT 'Restored: ' || name FROM models
WHERE name = 'global.anthropic.claude-sonnet-4-6'
  AND provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock');
SQL
    echo "✅ 数据库已恢复"
else
    echo "⚠️  数据库配置保持不变（upstream_model_id 仍然有效）"
fi

# 3. 清理文件
echo ""
echo "清理测试文件..."
rm -f .env.test
rm -f /tmp/test-upstream-model-id.sql
rm -rf logs/test/
rm -f bin/gateway-test bin/control-plane-test

echo ""
echo "======================================"
echo "✅ 清理完成"
EOF
