# Bedrock upstream_model_id 修复 - 数据库配置指南

## 问题概述

Bedrock provider 返回 400 错误，根因是代码未使用数据库中的 `upstream_model_id` 字段，导致将网关内部模型名（如 `global.anthropic.claude-sonnet-4-6`）直接传给 AWS Bedrock API，而 Bedrock 需要的是推理配置文件 ID（如 `us.anthropic.claude-sonnet-4-6-20250514-v1:0`）。

## 代码修复内容

已完成以下修改：

1. ✅ 添加 `ModelEntry` 结构体，包含 `upstream_model_id` 字段
2. ✅ 修改 `ProviderEntry` 添加 `ModelsV2` 字段
3. ✅ 更新 `buildProviderEntries` 从数据库读取 `upstream_model_id`
4. ✅ 在 `ProviderConfig` 添加 `ModelUpstreamIDs` 映射
5. ✅ 更新 `ProviderLoader` 构建并传递映射
6. ✅ 修改 Bedrock Provider 优先使用 `upstream_model_id`
7. ✅ 添加单元测试验证功能

## 数据库配置步骤

### 1. 查询当前 Bedrock 模型配置

```sql
-- 连接生产数据库
psql -h 192.0.2.10 -p 5433 -U llm_gateway -d llm_gateway

-- 查看所有 Bedrock 模型的 upstream_model_id 状态
SELECT 
    m.name AS model_name,
    m.upstream_model_id,
    m.enabled,
    p.name AS provider_name,
    p.definition_id
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.definition_id = 'bedrock'
  AND m.enabled = true
ORDER BY m.name;
```

**预期结果**：所有 Bedrock 模型的 `upstream_model_id` 字段为空。

### 2. 填充 upstream_model_id

根据 AWS Bedrock 的模型命名规范填充：

```sql
-- Claude 模型系列
UPDATE models
SET upstream_model_id = CASE name
    -- Claude Sonnet 4 系列
    WHEN 'global.anthropic.claude-sonnet-4-6' THEN 'us.anthropic.claude-sonnet-4-6-20250514-v1:0'
    WHEN 'anthropic.claude-sonnet-4-6' THEN 'us.anthropic.claude-sonnet-4-6-20250514-v1:0'
    
    -- Claude Opus 4 系列
    WHEN 'global.anthropic.claude-opus-4-8' THEN 'us.anthropic.claude-opus-4-8-20250514-v1:0'
    WHEN 'anthropic.claude-opus-4-8' THEN 'us.anthropic.claude-opus-4-8-20250514-v1:0'
    
    -- Claude Haiku 4 系列
    WHEN 'global.anthropic.claude-haiku-4-5' THEN 'us.anthropic.claude-haiku-4-5-20250514-v1:0'
    WHEN 'anthropic.claude-haiku-4-5' THEN 'us.anthropic.claude-haiku-4-5-20250514-v1:0'
    
    -- 其他 Claude 模型（根据实际情况补充）
    ELSE upstream_model_id
END
WHERE provider_id IN (
    SELECT id FROM providers WHERE definition_id = 'bedrock'
)
AND name LIKE '%anthropic%'
AND enabled = true;

-- 验证更新结果
SELECT name, upstream_model_id, enabled
FROM models
WHERE provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND name LIKE '%anthropic%'
  AND enabled = true;
```

### 3. 查找正确的 Bedrock 推理配置文件 ID

如果不确定正确的推理配置文件 ID，可以：

#### 方法 1：通过 AWS CLI 查询

```bash
# 列出所有可用的推理配置文件
aws bedrock list-inference-profiles \
    --region us-east-1 \
    --query 'inferenceProfileSummaries[?contains(inferenceProfileName, `claude`)].{Name:inferenceProfileName, ID:inferenceProfileId}' \
    --output table

# 查询特定模型的详细信息
aws bedrock get-inference-profile \
    --inference-profile-identifier "us.anthropic.claude-sonnet-4-6-20250514-v1:0" \
    --region us-east-1
```

#### 方法 2：查看 AWS Bedrock 控制台

1. 登录 AWS Console → Bedrock
2. 导航到 "Cross-region inference" 或 "Inference profiles"
3. 查找对应模型的推理配置文件 ARN 或 ID

#### 方法 3：参考 AWS 文档

最新的推理配置文件 ID 格式通常为：
- 跨区域：`us.anthropic.claude-{model}-{version}:0`
- 区域内：`anthropic.claude-{model}-{version}`

### 4. 触发配置热重载

```bash
# 方式 1: 通过 Redis 发布配置变更事件
redis-cli -h 192.0.2.10 -p 6379 -a CHANGE_ME_REDIS_PASSWORD PUBLISH config:change reload

# 方式 2: 重启 gateway 服务
kubectl rollout restart deployment/llm-gateway -n production
```

### 5. 验证修复

```bash
# 发送测试请求
curl -X POST http://gateway-url/v1/messages \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100
  }'

# 检查日志确认使用了正确的 upstream_model_id
# 应该看到类似: ModelId: us.anthropic.claude-sonnet-4-6-20250514-v1:0
```

## 注意事项

### 兼容性

- **向后兼容**：当 `upstream_model_id` 为空时，自动使用 gateway model name
- **旧版本**：如果 `ModelsV2` 字段不存在，会回退到 `Models` 字段
- **无侵入**：不影响其他 provider（Anthropic 直连、OpenAI 等）

### 其他 Provider

以下 provider 可能也需要 upstream_model_id 映射：

1. **Oracle OCI Generative AI**：可能有类似的模型 ID 映射需求
2. **Google Vertex AI**：可能需要映射到特定的 Vertex 模型 ID
3. **Azure OpenAI**：可能需要映射到 deployment ID

可以用相同的方式为这些 provider 配置 `upstream_model_id`。

### 批量操作模板

```sql
-- 批量查询需要配置的模型
SELECT 
    p.name AS provider,
    p.definition_id,
    COUNT(*) AS model_count,
    STRING_AGG(m.name, ', ' ORDER BY m.name) AS model_names
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE m.enabled = true
  AND (m.upstream_model_id IS NULL OR m.upstream_model_id = '')
GROUP BY p.name, p.definition_id
ORDER BY model_count DESC;

-- 模板：批量更新同一系列模型
UPDATE models
SET upstream_model_id = 'us.anthropic.{model_family}-{version}:0'
WHERE provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND name LIKE 'global.anthropic.{model_family}%'
  AND enabled = true;
```

## 回滚方案

如果修复后出现问题：

```bash
# 1. 回滚代码（回到修复前的 commit）
git revert cf396932

# 2. 或者清空 upstream_model_id（恢复原有行为）
UPDATE models
SET upstream_model_id = ''
WHERE provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock');

# 3. 触发热重载
redis-cli -h 192.0.2.10 -a CHANGE_ME_REDIS_PASSWORD PUBLISH config:change reload
```

## 监控建议

部署后监控以下指标：

1. **错误率**：Bedrock 的 400 错误应该消失
2. **调用日志**：检查 `call_traces` 表的 `upstream_status` 不再为 0
3. **响应时间**：确保没有引入性能回归
4. **成功率**：Bedrock 模型的成功率应该恢复正常

```sql
-- 监控查询：最近 1 小时 Bedrock 调用情况
SELECT 
    provider,
    status,
    COUNT(*) AS count,
    AVG(duration_ms) AS avg_duration_ms
FROM call_traces
WHERE provider LIKE 'aws-%'
  AND created_at > NOW() - INTERVAL '1 hour'
GROUP BY provider, status
ORDER BY provider, status;
```

## 参考文档

- [根因分析文档](./incident-20260713-bedrock-400.md)
- [AWS Bedrock 推理配置文件文档](https://docs.aws.amazon.com/bedrock/latest/userguide/cross-region-inference.html)
- [Model.UpstreamName() 方法](../../internal/controlplane/model/model.go#L75)
