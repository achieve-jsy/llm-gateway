---
name: aws-bedrock-ops
description: AWS Bedrock 运维排障指南。用于诊断和修复 Bedrock 模型调用失败、API Key 认证问题、模型 ID 格式错误、provider 自动禁用等生产问题。当用户遇到 Bedrock 相关报错时使用此 skill。
---

# AWS Bedrock 运维排障 (aws-bedrock-ops)

本 skill 沉淀了 llm_gateway 项目中 AWS Bedrock 集成的运维经验，覆盖认证、模型 ID、路由、健康检查等关键环节。

## 1. 认证机制

### 三种认证模式（bedrock provider `New()` 自动检测）

| 模式 | APIKey 格式 | SDK 配置 |
|------|-------------|----------|
| Bedrock API Key (Bearer) | 非空、非 JSON 字符串 | `AnonymousCredentials` + `BearerAuthTokenProvider` + `AuthSchemePreference: ["smithy.api#httpBearerAuth"]` |
| IAM 静态凭证 | `{"access_key_id":"...","secret_access_key":"..."}` | `StaticCredentialsProvider` |
| 默认凭证链 | 空字符串 | `LoadDefaultConfig` 无额外配置 |

### Bedrock API Key 格式

生产环境使用的 API Key 存储格式：
- DB `providers.encrypted_credentials` → 解密后为 JSON `{"api_key":"<base64_value>"}`
- `extractAPIKey()` 提取出 base64 编码值，如 `ABSKQmVkcm9ja0FQSUtleS14cGg4LWF0LTEyMzQ1Njc4OTAxMjp...`
- 该 base64 值**直接**作为 Bearer token 发送，**不要解码**
- 解码后内容为二进制前缀 + `BedrockAPIKey-<prefix>-at-<account_id>:<secret>`，但 SDK 只需要原始 base64 字符串

### 验证 API Key 连通性

```bash
# 在生产服务器上直接用 HTTP 测试（绕过 SDK）
curl -s -X POST "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-sonnet-4-20250514-v1:0/converse" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <base64_api_key>" \
  -d '{"messages":[{"role":"user","content":[{"text":"hi"}]}],"inferenceConfig":{"maxTokens":5}}'
```

## 2. 模型 ID 格式

### 核心规则

- DB `models.name` 中直接存 Bedrock inference profile ID，**不加** `bedrock/` 前缀
- 使用 `global.` 区域前缀（cross-region inference profile），支持全球路由
- 裸模型 ID（不带区域前缀如 `anthropic.claude-*`）不可用于 Converse API

```
global.anthropic.claude-sonnet-4-20250514-v1:0  ✅ 正确（DB 中存这个）
us.anthropic.claude-sonnet-4-20250514-v1:0      ✅ 也可用（限 us 区域）
anthropic.claude-sonnet-4-20250514-v1:0         ❌ 报 400: "Retry with inference profile ID"
bedrock/global.anthropic.claude-sonnet-4-6      ❌ 报 400（不要加 provider 前缀！）
```

### 可用模型 ID 参考（2026-05 验证）

| 模型 | Bedrock Model ID (存入 DB) | 状态 |
|------|-------------------|------|
| Claude Sonnet 4 | `global.anthropic.claude-sonnet-4-20250514-v1:0` | ✅ |
| Claude Sonnet 4.5 | `global.anthropic.claude-sonnet-4-5-20250929-v1:0` | ✅ |
| Claude Opus 4.5 | `global.anthropic.claude-opus-4-5-20251101-v1:0` | ✅ |
| Claude Opus 4.6 | `global.anthropic.claude-opus-4-6-v1` | ✅ |

### 常见错误与含义

| 错误信息 | 含义 |
|----------|------|
| `ValidationException: The provided model identifier is invalid` | model ID 格式不对或 model 不存在 |
| `Invocation with on-demand throughput isn't supported. Retry with inference profile ID` | 需要加 `us.` 前缀 |
| `This model version has reached the end of its life` | 模型版本已停服 |
| `Access denied. Model is marked as Legacy` | 需要联系 AWS 升级 |

## 3. 模型名在系统中的流转

### DB → Redis Snapshot → Gateway Registry → Bedrock API

```
DB models.name = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"  (无前缀)
        │
        ▼ provider_snapshot_service.go (直接使用 m.Name)
Redis snapshot models = ["global.anthropic.claude-sonnet-4-5-20250929-v1:0"]
        │
        ▼ provider_loader.go → registry (type="bedrock" 来自 provider 配置)
Registry model ID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
        │
        ▼ resolveUnqualifiedSelector → GetProviderName → flat map 命中
        ▼ forwardChatRequest(selector.Model)
Bedrock Converse API ModelId = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"  ✅
```

### DB → Groups API → Scheduler → Handler → Registry

```
DB models.name = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
        │
        ▼ group_handler.go ListEnabledGroups
Groups API model name = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
  + provider_type = "bedrock" (来自 providers.definition_id)
        │
        ▼ config_syncer.go → route table
RouteDecision.ModelName = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
        │
        ▼ handler/chat.go: req.Model = decision.ModelName
req.Model = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
        │
        ▼ ParseModelSelector (无 "/" → 不分割 provider)
selector = {Provider: "", Model: "global.anthropic.claude-sonnet-4-5-20250929-v1:0"}
        │
        ▼ resolveUnqualifiedSelector → registry flat map 命中 → 找到 provider "aws"
        ▼ forwardChatRequest
forwardReq.Model = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"  ✅
```

### 关键设计决策

- DB 中 `models.name` **不加** provider 前缀（用户注册什么就存什么）
- Provider 类型通过 `providers.definition_id` 字段识别，不靠模型名前缀
- `ParseModelSelector` 对不含 `/` 的模型名不做 provider 分割，走 registry flat map 查找
- Registry 通过 provider_loader 注册时建立 model → provider 映射关系

## 4. 健康检查问题

### Provider 自动禁用

健康检查服务 (`health_check_service.go`) 对 Bedrock provider 使用 `ListFoundationModels` API。

**问题**：`ListFoundationModels` 走的是 control client，如果使用 Bearer token 模式，control client 也配了 Bearer auth，但 `ListFoundationModels` 可能不支持 Bearer token（它需要 SigV4）。结果是健康检查持续失败 → 达到 `failure_threshold` 后自动禁用 provider。

**临时解决**：设置 `health_check_configs.failure_threshold = 999`

```sql
UPDATE health_check_configs SET failure_threshold = 999;
UPDATE providers SET enabled = true, status = 1, consecutive_failures = 0 WHERE name = 'aws';
```

**根本解决**：修改 `checkBedrockProvider` 跳过 `ListFoundationModels`，改用 `Converse` API 做轻量探测。

### Group Check

`group_check_service.go` 的 `probeGroup` 通过 Gateway API 发真实请求（model = group name），不直接接触 Bedrock model ID，依赖 Gateway 路由层正确解析。

## 5. 排障清单

当 Bedrock 模型调用失败时，按顺序检查：

1. **provider 是否启用？**
   ```sql
   SELECT name, enabled, status, consecutive_failures FROM providers WHERE name = 'aws';
   ```
   如果 `enabled=false` 或 `status=3`，重新启用。

2. **model 是否启用？**
   ```sql
   SELECT id, name, enabled FROM models WHERE provider_id = '<aws_provider_id>';
   ```

3. **Redis snapshot 是否最新？**
   ```bash
   # 检查 snapshot 中 aws provider 的 models 列表
   redis-cli -h <host> -a <pass> -n <db> GET 'gateway:providers:snapshot:v1'
   ```
   如果过时，重启 control-plane 刷新。

4. **model ID 格式正确？**
   - 必须是 `us.anthropic.claude-*` 格式（cross-region inference profile）
   - 不能带 `bedrock/` 前缀
   - 不能用 `global.` 前缀
   - 不能用裸 `anthropic.claude-*`（无区域前缀）

5. **API Key 有效？**
   用独立 test binary 或 curl 验证：
   ```bash
   /tmp/bedrock_test '<base64_api_key>' 'us.anthropic.claude-sonnet-4-20250514-v1:0'
   ```

6. **Gateway registry 是否注册了该模型？**
   错误 `"model not found: bedrock/..."` = registry 没注册（provider 可能被禁用或 snapshot 过时）
   错误 `"ValidationException: ..."` = 已到达 AWS，但 model ID 无效

7. **重启顺序**
   ```bash
   docker stop llm-gw-control-plane -t 3 && docker start llm-gw-control-plane
   sleep 5
   docker stop llm-gw-gateway -t 3 && docker start llm-gw-gateway
   ```
   control-plane 先启动（写 Redis snapshot），gateway 后启动（读 snapshot）。

## 6. 关键文件索引

| 文件 | 职责 |
|------|------|
| `internal/providers/bedrock/bedrock.go` | Bedrock provider 初始化、认证配置 |
| `internal/providers/bedrock/chat.go` | Converse API 调用，model ID 传递 |
| `internal/controlplane/service/provider_snapshot_service.go` | 构建 Redis 快照，**去 provider 前缀** |
| `internal/controlplane/service/internal_service.go` | `/internal/v1/models/available` API |
| `internal/controlplane/handler/group_handler.go` | `/api/v1/internal/groups` API（保留前缀） |
| `internal/controlplane/service/health_check_service.go` | 健康检查 + 自动禁用逻辑 |
| `internal/gateway/scheduler/config_syncer.go` | 从 groups API 同步路由表 |
| `internal/gateway/loader/provider_loader.go` | 从 Redis snapshot 构建 providers.Router |
| `internal/providers/router.go` | 模型解析 (`ParseModelSelector`) + 请求转发 (`forwardChatRequest`) |
| `internal/core/model_selector.go` | `ParseModelSelector`：按 `/` 分割 provider 和 model |
