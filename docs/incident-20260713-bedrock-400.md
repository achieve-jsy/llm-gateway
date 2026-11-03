# Bedrock 400 错误根因分析

**日期**: 2026-07-13  
**请求 ID**: 76cda259-d28a-4cb1-a9d4-e5d376be7fda  
**状态**: 已定位根因  

---

## 问题现象

### UI 显示信息

| 字段 | 值 |
|------|-----|
| 模型 | claude-sonnet-4-6 |
| 状态码 | 400 |
| 时间戳 | 2026-07-13 14:04:26 |
| 总耗时 | 2.7s |
| 鉴权耗时 | 0ms |
| 路由耗时 | 38ms |
| 上游响应耗时 | 2680ms |
| 错误来源 | HTTP 400 192.0.2.10 |

---

## 证据链

### 1. 数据库 call_traces 表记录

```sql
-- 生产库查询
SELECT 
    trace_id,
    provider,
    routed_model,
    status,
    upstream_status,
    stage_route_ms,
    stage_upstream_ms,
    duration_ms,
    inbound_endpoint
FROM call_traces 
WHERE trace_id = '76cda259-d28a-4cb1-a9d4-e5d376be7fda';
```

**查询结果**：

| 字段 | 值 | 说明 |
|------|-----|------|
| trace_id | 76cda259-d28a-4cb1-a9d4-e5d376be7fda | 请求唯一标识 |
| provider | aws-0799 | Bedrock provider |
| routed_model | global.anthropic.claude-sonnet-4-6 | 网关内部模型名 |
| status | 400 | **网关返回客户端的状态码** |
| upstream_status | **0** | **上游从未返回 HTTP 状态码** |
| stage_route_ms | 38 | 路由完成时刻 |
| stage_upstream_ms | 38 | 上游调用启动时刻 |
| duration_ms | 2718 | 总耗时 = 上游实际耗时约 2680ms |
| inbound_endpoint | /v1/messages | Anthropic 原生格式 |

**关键发现**：
- `upstream_status = 0` 说明这不是 HTTP 调用（AWS SDK 内部调用）
- UI 显示的"上游响应 2680ms"是推算值（duration - stage_upstream）
- 400 错误是**网关自己生成的**，来自 AWS SDK 的异常映射

### 2. 模型配置表记录

```sql
-- 查询 Bedrock 模型配置
SELECT 
    m.name,
    m.upstream_model_id,
    p.name as provider,
    p.definition_id,
    p.type
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.name = 'aws-0799'
  AND m.name LIKE '%claude-sonnet-4%';
```

**查询结果**：

| name | upstream_model_id | provider | definition_id | type |
|------|-------------------|----------|---------------|------|
| global.anthropic.claude-sonnet-4-6 | **""** (空) | aws-0799 | bedrock | "" |

**关键发现**：
- **所有 Bedrock 模型的 `upstream_model_id` 字段全部为空**
- `type` 字段也为空（不影响此次问题，但不规范）

### 3. 代码验证

```bash
# 搜索 upstream_model_id 使用情况
grep -r "upstream_model_id" internal/gateway --include="*.go"
# 结果：无任何匹配
```

**结论**：数据库有该字段，但代码**完全未使用**。

---

## 根因分析

### 执行路径重建

```
1. 客户端请求
   POST /v1/messages
   {"model": "claude-sonnet-4-6", ...}
   
2. 网关路由
   ├─ 鉴权通过（0ms）
   ├─ 路由查找（38ms）
   └─ 匹配到：global.anthropic.claude-sonnet-4-6 @ aws-0799 (Bedrock)
   
3. Bedrock Provider 处理
   internal/providers/bedrock/chat.go:62-68
   
   model := strings.TrimSpace(req.Model)  
   // req.Model = "global.anthropic.claude-sonnet-4-6"
   
   model = stripProviderPrefix(model, "bedrock")  
   // stripProviderPrefix 只处理 "bedrock/xxx" 格式
   // "global.anthropic.claude-sonnet-4-6" 不匹配，原样返回
   
   parts := converseParts{
       modelID: awssdk.String(model),  
       // ← 传给 AWS 的是 "global.anthropic.claude-sonnet-4-6"
       ...
   }
   
4. AWS Bedrock API 调用
   AWS SDK → Bedrock Converse API
   ModelId: "global.anthropic.claude-sonnet-4-6"
   
   ↓ Bedrock 不认识此 ID（需要推理配置文件 ID）
   ↓ 等待 ~2.6 秒后返回异常
   
   ValidationException: The provided model identifier is invalid.
   
5. 错误映射
   internal/providers/bedrock/chat.go:mapAWSError()
   
   ValidationException → NewInvalidRequestErrorWithStatus(400, msg)
   
6. 网关响应客户端
   HTTP 400
   upstream_status = 0（AWS SDK 调用，非 HTTP）
```

### 根本原因

**设计层缺陷**：网关内部 model name 和上游 provider model ID 没有做隔离转换

1. **数据模型有字段但未使用**
   - `models.upstream_model_id` 字段存在于数据库
   - ProviderLoader 不读取该字段
   - Router 不返回该字段
   - Bedrock Provider 收不到真实的上游 model ID

2. **兼容性假设错误**
   - 代码假设：网关 model name = 上游 API model ID
   - 对 Anthropic 直连成立：claude-sonnet-4-6 就是其 API 认识的 ID
   - 对 Bedrock **不成立**：需要的是推理配置文件 ID
     - 正确示例：`us.anthropic.claude-sonnet-4-6-20250514-v1:0`
     - 或完整 ARN：`arn:aws:bedrock:us-east-1::foundation-model/...`

3. **Bedrock Provider 的错误处理**
   - `stripProviderPrefix(model, "bedrock")` 只能处理 `bedrock/xxx` 格式
   - 对路由传来的 `global.anthropic.claude-sonnet-4-6` 无效
   - 直接将网关内部名称发给 AWS

---

## 影响范围

### 受影响的模型

```sql
-- 查询所有 Bedrock 模型配置
SELECT 
    m.name,
    m.upstream_model_id,
    m.enabled,
    p.name as provider
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.definition_id = 'bedrock'
  AND m.enabled = true
ORDER BY m.name;
```

**预期**：所有通过 Bedrock 提供的 Claude 模型都存在此问题。

### 受影响的场景

- ✅ Anthropic 官方 API：不受影响（model name = upstream ID）
- ❌ AWS Bedrock：**全部受影响**
- ❓ Oracle OCI：需验证（可能有相同问题）
- ❓ Google Vertex AI：需验证（可能有相同问题）

---

## 修复方案

### 方案 1：临时修复（数据库层绕过）

**不推荐**，会破坏语义一致性。

```sql
-- 将 model.name 改成 Bedrock 认识的 ID
UPDATE models 
SET name = 'us.anthropic.claude-sonnet-4-6-20250514-v1:0'
WHERE name = 'global.anthropic.claude-sonnet-4-6'
  AND provider_id = (SELECT id FROM providers WHERE name = 'aws-0799');
```

**问题**：
- 客户端需要知道每个 provider 的 model ID 格式
- 多租户场景下不同 provider 无法共享统一的 model name

### 方案 2：正确修复（代码改造）

#### 2.1 ProviderLoader 读取 upstream_model_id

```go
// internal/gateway/provider_loader.go

type ModelConfig struct {
    Name             string
    UpstreamModelID  string  // 新增字段
    InputPrice       float64
    OutputPrice      float64
    // ...
}

func (l *ProviderLoader) loadModelsForProvider(providerID int64) ([]ModelConfig, error) {
    rows, err := l.db.Query(`
        SELECT name, upstream_model_id, input_price_per_million, output_price_per_million, ...
        FROM models
        WHERE provider_id = $1 AND enabled = true
    `, providerID)
    
    for rows.Next() {
        var m ModelConfig
        err := rows.Scan(&m.Name, &m.UpstreamModelID, &m.InputPrice, ...)
        // ...
        models = append(models, m)
    }
    return models, nil
}
```

#### 2.2 Router 优先返回 upstream_model_id

```go
// internal/gateway/router.go

type RoutedModel struct {
    Name            string
    UpstreamModelID string  // 新增字段
    Provider        providers.Provider
    // ...
}

func (r *Router) Route(modelName string) (*RoutedModel, error) {
    // ... 查找逻辑
    
    return &RoutedModel{
        Name:            model.Name,
        UpstreamModelID: model.UpstreamModelID,  // 传递给 provider
        Provider:        provider,
    }, nil
}
```

#### 2.3 Bedrock Provider 使用 UpstreamModelID

```go
// internal/providers/bedrock/chat.go

func (p *BedrockProvider) CreateChatCompletion(ctx context.Context, req *providers.ChatRequest) (*providers.ChatResponse, error) {
    // 优先使用 UpstreamModelID，回退到 Model
    modelID := req.UpstreamModelID
    if modelID == "" {
        modelID = req.Model
    }
    
    // 移除 provider 前缀（兼容性保留）
    modelID = stripProviderPrefix(modelID, "bedrock")
    
    parts := p.buildConverseParts(req)
    parts.modelID = awssdk.String(modelID)  // 使用正确的 ID
    
    // ...
}
```

#### 2.4 数据库填充 upstream_model_id

```sql
-- Bedrock Claude 模型映射
UPDATE models
SET upstream_model_id = CASE name
    WHEN 'global.anthropic.claude-sonnet-4-6' THEN 'us.anthropic.claude-sonnet-4-6-20250514-v1:0'
    WHEN 'global.anthropic.claude-opus-4-8' THEN 'us.anthropic.claude-opus-4-8-20250514-v1:0'
    WHEN 'global.anthropic.claude-haiku-4-5' THEN 'us.anthropic.claude-haiku-4-5-20250514-v1:0'
    -- ... 其他模型
END
WHERE provider_id IN (SELECT id FROM providers WHERE definition_id = 'bedrock')
  AND name LIKE 'global.anthropic.%';

-- 触发配置热重载
-- 方式1: Redis pub/sub
-- redis-cli -h 192.0.2.10 -a CHANGE_ME_REDIS_PASSWORD PUBLISH config:change reload

-- 方式2: 重启网关服务
-- kubectl rollout restart deployment/llm-gateway -n production
```

---

## 验证步骤

### 1. 代码改造后的验证

```bash
# 1. 单元测试
go test ./internal/gateway/provider_loader_test.go -v
go test ./internal/providers/bedrock/chat_test.go -v

# 2. 集成测试
curl -X POST http://localhost:8080/v1/messages \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100
  }'

# 3. 检查日志
# 应该看到正确的 model ID 传给 Bedrock
# upstream_model_id: us.anthropic.claude-sonnet-4-6-20250514-v1:0
```

### 2. 数据库验证

```sql
-- 确认所有 Bedrock 模型都有 upstream_model_id
SELECT 
    m.name,
    m.upstream_model_id,
    CASE 
        WHEN m.upstream_model_id IS NULL OR m.upstream_model_id = '' 
        THEN '❌ 缺失' 
        ELSE '✅ 已配置' 
    END as status
FROM models m
JOIN providers p ON m.provider_id = p.id
WHERE p.definition_id = 'bedrock'
  AND m.enabled = true;
```

---

## 附加问题

### 1. Provider type 字段为空

```sql
-- 当前状态
SELECT name, definition_id, type FROM providers WHERE definition_id = 'bedrock';
-- aws-0799 | bedrock | ""

-- 建议修复
UPDATE providers SET type = 'bedrock' WHERE definition_id = 'bedrock';
```

**影响**：目前不影响运行（工厂通过 definition_id 查找），但建议补齐保持一致性。

### 2. 其他 provider 的映射需求

需要验证以下 provider 是否也需要 upstream_model_id 映射：
- Oracle OCI Generative AI
- Google Vertex AI
- Azure OpenAI（可能有 deployment_id 映射需求）

---

## Timeline

| 时间 | 事件 |
|------|------|
| 2026-07-13 14:04:26 | 用户请求 claude-sonnet-4-6 返回 400 |
| 2026-07-13 17:00 | 开始调查，查询生产库 call_traces |
| 2026-07-13 17:15 | 发现 upstream_model_id 为空 |
| 2026-07-13 17:30 | 确认代码未使用该字段 |
| 2026-07-13 17:45 | 定位根因：设计层缺陷 |
| 2026-07-13 18:00 | 提出修复方案 |

---

## 总结

这是一个**设计债务暴露**导致的问题：

1. **历史原因**：最初可能只对接了 Anthropic 官方 API，model name 和 upstream ID 一致
2. **扩展问题**：加入 Bedrock 后，其 model ID 格式（推理配置文件）与网关内部 name 不一致
3. **配置缺失**：虽然数据库有 upstream_model_id 字段，但代码从未使用
4. **错误传播**：Bedrock Provider 直接将网关 model name 传给 AWS，导致 ValidationException

**修复优先级**：P0（影响所有 Bedrock 模型调用）

**修复工作量**：
- 代码改造：2-3 小时（涉及 ProviderLoader、Router、Bedrock Provider）
- 数据库配置：1 小时（查询所有 Bedrock 模型的正确 ID 并填充）
- 测试验证：1-2 小时
- **总计：4-6 小时**
