# gateway-api 接口契约

## 1. 概览

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | gateway-api |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

本契约定义了 gateway-api 模块对外暴露的 OpenAI 兼容接口规范。gateway-api 作为统一大模型网关的接入层，对外提供标准的 OpenAI Chat Completions API 格式，调用方无需修改现有代码即可通过本网关访问多家大模型厂商的服务。

---

## 2. 公共约定

### 2.1 基础信息

| 项目 | 约定 |
| :--- | :--- |
| Base URL | `/v1` |
| 数据格式 | JSON，UTF-8 编码 |
| 认证方式 | Bearer Token（HTTP Header：`Authorization: Bearer {api_key}`） |
| 字符集 | UTF-8 |
| 时间格式 | Unix 时间戳（秒） |

### 2.2 请求头约定

| Header | 必填 | 说明 |
| :--- | :--- | :--- |
| `Authorization` | 是（业务接口） | `Bearer {api_key}` |
| `Content-Type` | 是（POST 请求） | `application/json` |
| `X-Request-ID` | 否 | 调用方自定义请求追踪 ID，缺失时网关自动生成 UUID v4 |

### 2.3 响应头约定

| Header | 说明 | 适用场景 |
| :--- | :--- | :--- |
| `Content-Type` | `application/json` 或 `text/event-stream` | 所有响应 |
| `X-Request-ID` | 请求唯一标识 | 所有响应 |
| `Cache-Control` | `no-cache` | 流式响应 |
| `Connection` | `keep-alive` | 流式响应 |

### 2.4 统一错误响应格式

所有错误统一使用以下 JSON 结构：

```json
{
  "error": {
    "type": "error_type",
    "message": "人类可读的错误描述",
    "code": "machine_readable_code"
  }
}
```

### 2.5 认证规则

| 规则 | 说明 |
| :--- | :--- |
| Header 格式 | `Authorization: Bearer {api_key}` |
| Key 格式 | 字符串，长度 32-128 字符，允许字母/数字/连字符 |
| 适用范围 | `/v1/*` 路径下所有接口 |
| 豁免路径 | `/health`、`/ready` 无需认证 |
| 失败响应 | 401 + `authentication_error` |

---

## 3. 端点索引

| 路径 | 方法 | 说明 | 认证 | 幂等性 |
| :--- | :--- | :--- | :--- | :--- |
| `/v1/chat/completions` | POST | 对话补全（支持普通/流式） | Bearer Token | 非幂等 |
| `/v1/models` | GET | 获取可用模型列表 | Bearer Token | 幂等 |
| `/health` | GET | 存活探针 | 无 | 幂等 |
| `/ready` | GET | 就绪探针 | 无 | 幂等 |

---

## 4. 端点详细设计

### 4.1 POST /v1/chat/completions

对话补全接口，兼容 OpenAI Chat Completions API 格式。根据 `stream` 参数决定返回普通 JSON 响应或 SSE 流式响应。

#### 请求参数

**Headers**：

| Header | 必填 | 说明 |
| :--- | :--- | :--- |
| `Authorization` | 是 | `Bearer {api_key}` |
| `Content-Type` | 是 | `application/json` |
| `X-Request-ID` | 否 | 调用方自定义请求 ID |

**Body（JSON）**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `model` | string | 是 | - | 模型标识，如 `qwen-plus`、`deepseek-chat`、`anthropic.claude-3-5-sonnet-20241022-v2:0` |
| `messages` | array | 是 | - | 对话消息列表，长度 1~256 |
| `messages[].role` | string | 是 | - | 角色：`system`、`user`、`assistant` |
| `messages[].content` | string | 是 | - | 消息内容 |
| `stream` | boolean | 否 | `false` | 是否启用流式返回 |
| `temperature` | number | 否 | 模型默认值 | 采样温度，范围 [0, 2] |
| `top_p` | number | 否 | 模型默认值 | 核采样概率，范围 (0, 1] |
| `max_tokens` | integer | 否 | 模型默认值 | 生成的最大 token 数 |
| `stop` | string/array | 否 | - | 停止生成的标记词 |
| `presence_penalty` | number | 否 | 0 | 存在惩罚，范围 [-2, 2] |
| `frequency_penalty` | number | 否 | 0 | 频率惩罚，范围 [-2, 2] |

**请求示例**：

```json
{
  "model": "qwen-plus",
  "messages": [
    {"role": "system", "content": "你是一个有帮助的助手。"},
    {"role": "user", "content": "你好"}
  ],
  "stream": false,
  "temperature": 0.7,
  "max_tokens": 1024
}
```

#### 成功响应 — 普通模式（stream=false）

**HTTP 状态码**：200

**Headers**：`Content-Type: application/json`、`X-Request-ID: {uuid}`

**Body 示例**：

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1716249600,
  "model": "qwen-plus",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "你好！有什么我可以帮助你的吗？"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 20,
    "completion_tokens": 12,
    "total_tokens": 32
  }
}
```

**响应字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `id` | string | 本次补全唯一 ID |
| `object` | string | 固定值 `"chat.completion"` |
| `created` | integer | Unix 时间戳（秒） |
| `model` | string | 实际使用的模型名称 |
| `choices[].index` | integer | 选项索引 |
| `choices[].message.role` | string | 固定值 `"assistant"` |
| `choices[].message.content` | string | 生成的回复内容 |
| `choices[].finish_reason` | string | 结束原因：`stop`（正常）、`length`（达到 max_tokens） |
| `usage.prompt_tokens` | integer | 输入 token 数 |
| `usage.completion_tokens` | integer | 输出 token 数 |
| `usage.total_tokens` | integer | 总 token 数 |

#### 成功响应 — 流式模式（stream=true）

**HTTP 状态码**：200

**Headers**：

| Header | 值 |
| :--- | :--- |
| `Content-Type` | `text/event-stream` |
| `Cache-Control` | `no-cache` |
| `Connection` | `keep-alive` |
| `X-Request-ID` | `{uuid}` |

**SSE 事件流格式**：

每个事件格式为 `data: {JSON}\n\n`，流结束以 `data: [DONE]\n\n` 标记。

**完整流式响应示例**：

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1716249600,"model":"qwen-plus","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1716249600,"model":"qwen-plus","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1716249600,"model":"qwen-plus","choices":[{"index":0,"delta":{"content":"！有什么"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1716249600,"model":"qwen-plus","choices":[{"index":0,"delta":{"content":"可以帮助你的吗？"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1716249600,"model":"qwen-plus","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":12,"total_tokens":32}}

data: [DONE]
```

**chunk 字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `id` | string | 同一流内所有 chunk 共享相同 ID |
| `object` | string | 固定值 `"chat.completion.chunk"` |
| `created` | integer | Unix 时间戳 |
| `model` | string | 模型名称 |
| `choices[].delta.role` | string | 仅第一个 chunk 包含，值为 `"assistant"` |
| `choices[].delta.content` | string | 增量文本内容 |
| `choices[].finish_reason` | string/null | 仅最后一个内容 chunk 有值 |
| `usage` | object | 仅最后一个 chunk 包含 |

#### 错误响应

**普通模式错误**：

```json
{
  "error": {
    "type": "invalid_request_error",
    "message": "参数 'model' 为必填字段",
    "code": "missing_required_field"
  }
}
```

**流式模式中途错误**（以 SSE error 事件发送）：

```
event: error
data: {"error":{"type":"upstream_error","message":"upstream connection lost","code":"stream_interrupted"}}
```

---

### 4.2 GET /v1/models

获取当前可用的模型列表。

#### 请求参数

**Headers**：

| Header | 必填 | 说明 |
| :--- | :--- | :--- |
| `Authorization` | 是 | `Bearer {api_key}` |

无查询参数。

#### 成功响应

**HTTP 状态码**：200

**Body 示例**：

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen-plus",
      "object": "model",
      "created": 1716249600,
      "owned_by": "bailian"
    },
    {
      "id": "qwen-turbo-latest",
      "object": "model",
      "created": 1716249600,
      "owned_by": "bailian"
    },
    {
      "id": "deepseek-chat",
      "object": "model",
      "created": 1716249600,
      "owned_by": "deepseek"
    },
    {
      "id": "deepseek-reasoner",
      "object": "model",
      "created": 1716249600,
      "owned_by": "deepseek"
    },
    {
      "id": "anthropic.claude-3-5-sonnet-20241022-v2:0",
      "object": "model",
      "created": 1716249600,
      "owned_by": "bedrock"
    }
  ]
}
```

**响应字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `object` | string | 固定值 `"list"` |
| `data` | array | 模型列表 |
| `data[].id` | string | 模型标识 |
| `data[].object` | string | 固定值 `"model"` |
| `data[].created` | integer | 模型注册时间（Unix 时间戳） |
| `data[].owned_by` | string | 所属厂商标识 |

#### 错误响应

| 状态码 | 错误类型 | 场景 |
| :--- | :--- | :--- |
| 401 | `authentication_error` | 未认证或 Key 无效 |
| 500 | `internal_error` | 内部异常 |

---

### 4.3 GET /health

存活探针，用于 Kubernetes liveness probe。仅检查服务进程存活状态，不检查外部依赖。

#### 请求参数

无需 Header 和参数。

#### 成功响应

**HTTP 状态码**：200

```json
{
  "status": "ok"
}
```

#### 错误响应

服务进程不可达时无响应（由 Kubernetes 判定为不健康并重启 Pod）。

---

### 4.4 GET /ready

就绪探针，用于 Kubernetes readiness probe。检查服务是否可以接收流量，包括外部依赖连通性。

#### 请求参数

无需 Header 和参数。

#### 成功响应（HTTP 200）

```json
{
  "status": "ready",
  "checks": {
    "redis": "ok",
    "providers": "ok"
  }
}
```

#### 未就绪响应（HTTP 503）

```json
{
  "status": "not_ready",
  "checks": {
    "redis": "failed: connection refused",
    "providers": "ok"
  }
}
```

**检查项说明**：

| 检查项 | 检查内容 | 失败影响 |
| :--- | :--- | :--- |
| `redis` | Redis PING 是否成功 | 返回 503，从负载均衡摘除 |
| `providers` | 至少一个 Provider 可达 | 返回 503，从负载均衡摘除 |

---

## 5. 错误码（后端含 §5）

### 5.1 统一错误体格式

```json
{
  "error": {
    "type": "error_type_identifier",
    "message": "描述性错误信息",
    "code": "specific_error_code"
  }
}
```

### 5.2 完整错误码表

#### 输入错误类（调用方应修正请求）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 400 | `invalid_request_error` | `invalid_json` | 请求体 JSON 格式错误 | 检查 JSON 语法 |
| 400 | `invalid_request_error` | `missing_required_field` | 缺少必填字段 | 补充 model/messages 等必填字段 |
| 400 | `invalid_request_error` | `invalid_field_value` | 字段值不合法 | 修正参数值（如 temperature 超范围） |
| 400 | `invalid_request_error` | `empty_messages` | messages 数组为空 | 至少提供一条消息 |
| 400 | `invalid_request_error` | `too_many_messages` | messages 超过 256 条 | 减少对话历史长度 |
| 400 | `invalid_request_error` | `request_too_large` | 请求体超过 1MB | 缩减请求体大小 |

#### 模型不可用类（调用方应换模型或稍后重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `missing_api_key` | 未提供 API Key | 添加 Authorization Header |
| 401 | `authentication_error` | `invalid_api_key` | API Key 无效 | 检查 Key 是否正确 |
| 404 | `model_not_found` | `unknown_model` | 请求模型未注册 | 调用 GET /v1/models 确认可用模型 |
| 503 | `model_unavailable` | `model_disabled` | 模型已被管理员禁用 | 更换模型或稍后重试 |
| 503 | `model_unavailable` | `model_offline` | 模型临时下线维护 | 稍后重试 |

#### 临时失败类（调用方应重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 502 | `upstream_error` | `provider_error` | 厂商 API 返回错误 | 稍后重试 |
| 502 | `upstream_error` | `provider_rate_limited` | 厂商限流 | 降低调用频率后重试 |
| 502 | `upstream_error` | `stream_interrupted` | 流式传输中途中断 | 重新发起请求 |
| 504 | `upstream_timeout` | `provider_timeout` | 厂商 API 响应超时 | 稍后重试，可考虑减少 max_tokens |
| 500 | `internal_error` | `unexpected_error` | 网关内部非预期异常 | 联系网关管理员 |

---

## 6. 版本策略与 Breaking Change 规则

### 6.1 版本策略

| 规则 | 说明 |
| :--- | :--- |
| 版本标识 | URL 路径中包含版本号，当前为 `/v1` |
| 版本共存 | 新大版本通过 `/v2` 路径独立部署，旧版本保持兼容运行 |
| 小版本迭代 | 同一大版本内仅做向后兼容变更 |

### 6.2 兼容性变更（不需要升级版本）

- 响应体新增可选字段
- 新增可选请求参数
- 新增新的 error.code 值
- 新增新的模型支持

### 6.3 Breaking Change（需要升级大版本）

- 删除或重命名现有字段
- 修改字段类型
- 修改错误响应结构
- 修改认证方式
- 删除端点

### 6.4 废弃流程

1. 提前 30 天通过文档和响应头 `Deprecation` 通知调用方
2. 旧版本至少维持 90 天可用
3. 下线前通过监控确认无流量后执行

---

## 7. 非功能约束（适用时填）

### 7.1 超时约束

| 场景 | 超时时间 | 超限处理 |
| :--- | :--- | :--- |
| 请求体读取 | 5s | 返回 400 |
| 普通调用总超时 | 30s | 返回 504 upstream_timeout |
| 流式连接总时长 | 300s | 关闭连接 |
| 流式 chunk 间隔 | 60s | 发送 SSE error 事件后关闭 |
| 就绪探针检查 | 5s | 视为不就绪 |

### 7.2 请求体大小限制

| 限制项 | 值 | 超限处理 |
| :--- | :--- | :--- |
| 请求体最大字节数 | 1MB | 返回 400 `request_too_large` |
| messages 数组最大长度 | 256 条 | 返回 400 `too_many_messages` |

### 7.3 并发与吞吐

| 指标 | 目标值 |
| :--- | :--- |
| 同时在线连接数 | ≥ 1000 |
| 网关自身处理延迟（P99） | < 10ms |
| 流式首 chunk 额外延迟 | < 50ms |

### 7.4 可用性

| 指标 | 目标值 |
| :--- | :--- |
| 服务可用率 | ≥ 99.9% |
| 优雅关停时间 | 30s（等待存量请求完成） |

---

## 8. 接口安全

### 8.1 认证流程

```mermaid
flowchart TD
    A[收到请求] --> B{路径是否豁免?}
    B -->|是 /health /ready| C[跳过认证，继续处理]
    B -->|否 /v1/*| D{Authorization Header 存在?}
    D -->|否| E[返回 401 missing_api_key]
    D -->|是| F{格式为 Bearer xxx?}
    F -->|否| G[返回 401 invalid_api_key]
    F -->|是| H{Key 在合法集合中?}
    H -->|否| I[返回 401 invalid_api_key]
    H -->|是| J[认证通过，继续处理]
```

### 8.2 安全措施

| 措施 | 说明 |
| :--- | :--- |
| 传输加密 | 生产环境强制 HTTPS（TLS 1.2+） |
| Key 脱敏 | 日志中 API Key 仅记录前 8 位 + 后 4 位 |
| 请求体限制 | 最大 1MB，防止大 payload 攻击 |
| 请求 ID 追踪 | 每个请求分配唯一 ID，便于安全审计 |
| 无敏感数据缓存 | 网关不缓存对话内容，仅转发 |
| 限流预留 | 当前不实现 429，后续迭代引入基于 Key 的限流 |

### 8.3 CORS 策略

本网关为后端服务间调用设计，默认不启用 CORS。若需浏览器端直接调用，需在反向代理层（如 Nginx/Envoy）配置 CORS 头。

---

## 9. 变更历史

| 版本 | 日期 | 修改人 | 变更内容 |
| :--- | :--- | :--- | :--- |
| v1.0.0 | 2026-05-21 | OpenCode | 初始版本，定义 OpenAI 兼容接口契约 |
| v1.1.0 | 2026-05-25 | 新增 GET /v1/me/models 端点 — CHG-20260523-001 | OpenCode |
| v1.2.0 | 2026-05-26 | 新增组名调用 + 故障切换错误码 — CHG-20260525-001 | OpenCode |

---

## 10. 变更记录 — CHG-20260523-001

## 变更摘要

API Key 认证方式从简单 Token 校验升级为 Bearer `sk-{32+ chars}` 格式身份解析链路；新增 `GET /v1/me/models` 基于身份返回可用模型列表；现有 `/v1/models` 改为基于认证身份过滤；新增鉴权相关错误码。

---

## 1. 认证约定变更

### 1.1 Authorization Header 规范

| 维度 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| 格式 | `Authorization: Bearer {api_key}` | `Authorization: Bearer sk-{32+ chars}` |
| Key 格式 | 32~128 字符，字母/数字/连字符 | `sk-` 前缀 + 32 位以上随机字符串（字母/数字） |
| 校验方式 | Redis SISMEMBER 简单校验 | 调用 auth 服务 `/internal/v1/auth/verify-key` 完整身份解析 |
| 校验结果 | Key 有效/无效 | Key 有效 → 注入 `{user_id, key_id, department_id}` 到请求上下文 |
| 豁免路径 | `/health`、`/ready` | 不变——`/health`、`/ready` 无需认证 |

> 内部管理调用使用 JWT Token（如 ops-console 员工自助页面调用 `/v1/me/models`），与外部 API Key 通过 Token 前缀区分：`sk-` 前缀为 API Key，`eyJ` 前缀为 JWT。

### 1.2 认证流程

```mermaid
flowchart TD
    A[收到请求] --> B{路径是否豁免?}
    B -->|是 /health /ready| C[跳过认证，继续处理]
    B -->|否 /v1/*| D{Authorization Header 存在?}
    D -->|否| E[返回 401 missing_api_key]
    D -->|是| F{格式为 Bearer xxx?}
    F -->|否| G[返回 401 invalid_api_key]
    F -->|是| H{提取 Token}
    H --> I{Token 前缀?}
    I -->|sk- 前缀| J[调用 auth 服务 /internal/v1/auth/verify-key]
    I -->|eyJ 前缀| K[验证 JWT 签名与过期时间]
    J --> L{Key 有效且启用?}
    L -->|是| M[注入 UserIdentity 到 Context]
    L -->|Key 不存在| N[返回 401 invalid_api_key]
    L -->|Key 已禁用| O[返回 401 key_disabled]
    L -->|auth 服务不可达| P[返回 503 auth_service_unavailable]
    K --> Q{JWT 有效?}
    Q -->|是| M
    Q -->|否| R[返回 401 invalid_token]
    M --> S[认证通过，继续处理]
```

---

## 2. 端点变更

### 2.1 端点索引（变更后）

| 路径 | 方法 | 说明 | 认证 | 幂等性 | 变更类型 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `/v1/chat/completions` | POST | 对话补全（普通/流式） | Bearer `sk-{key}` | 非幂等 | **变更** |
| `/v1/models` | GET | 获取可用模型列表（基于身份过滤） | Bearer `sk-{key}` | 幂等 | **变更** |
| **`/v1/me/models`** | **GET** | **获取当前登录员工可用模型列表** | **Bearer JWT** | **幂等** | **新增** |
| `/health` | GET | 存活探针 | 无 | 幂等 | 不变 |
| `/ready` | GET | 就绪探针 | 无 | 幂等 | 不变 |

---

## 3. 变更端点详细设计

### 3.1 POST /v1/chat/completions — 变更

**变更要点**：Authorization Header 必须携带 `sk-` 前缀 API Key，JWT Token 不再用于模型调用接口。

**请求变更**：

| Header | 变更前 | 变更后 |
| :--- | :--- | :--- |
| `Authorization` | `Bearer {api_key}`（32~128 字符） | `Bearer sk-{32+ chars}`（`sk-` 前缀 + 随机字符串） |
| `Content-Type` | 不变 | 不变 |
| `X-Request-ID` | 不变 | 不变 |

**鉴权行为变更**：

| 场景 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| 缺少 Authorization | 401 `missing_api_key` | 不变 |
| Key 格式不合法 | 401 `invalid_api_key` | 401 `invalid_api_key`（新增 `sk-` 前缀校验） |
| Key 不存在 | 401 `invalid_api_key` | 401 `invalid_api_key` |
| Key 已禁用 | 401 `invalid_api_key`（无区分） | 401 `key_disabled`（新增独立错误码） |
| 鉴权服务不可用 | 无此场景 | 503 `auth_service_unavailable` |

**Body 和响应格式不变**。

---

### 3.2 GET /v1/models — 变更

**变更要点**：返回的模型列表从全量列表改为基于认证身份过滤的可用模型列表。

| 维度 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| 返回内容 | 全量注册模型列表 | 基于 `dept_id` 过滤后的模型列表（与 `/v1/me/models` 结果等价） |
| 认证 | Bearer Token 简单校验 | Bearer `sk-{key}` 完整身份解析 |
| 无可用模型时 | 不适用（全量返回总有模型） | 返回空列表 `{"object":"list","data":[]}`，HTTP 200 |

**响应变更**：

响应格式不变（`object`、`data[]`），但 `data` 数组内容基于认证用户身份过滤：

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen-plus",
      "object": "model",
      "created": 1716249600,
      "owned_by": "bailian"
    }
  ]
}
```

**错误响应扩充**：

| 状态码 | error.type | error.code | 场景 |
| :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `missing_api_key` | 未携带 API Key |
| 401 | `authentication_error` | `invalid_api_key` | Key 无效或不存在 |
| 401 | `authentication_error` | `key_disabled` | Key 已被禁用 |
| 503 | `service_unavailable` | `auth_service_unavailable` | auth 服务不可达 |
| 500 | `internal_error` | `unexpected_error` | 内部异常 |

---

## 4. 新增端点

### 4.1 GET /v1/me/models

获取当前认证用户可用的模型列表。与 `/v1/models` 入口不同：本端点从路由命名语义明确表达"我的模型"，返回结果与 `/v1/models`（变更后）等价。

#### 请求参数

**Headers**：

| Header | 必填 | 说明 |
| :--- | :--- | :--- |
| `Authorization` | 是 | `Bearer {jwt_access_token}` |
| `X-Request-ID` | 否 | 调用方自定义请求 ID |

无查询参数，无 Body。

**请求示例**：

```bash
curl -s https://gateway.example.com/v1/me/models \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiIs..." \
  -H "X-Request-ID: req-20260523-001"
```

#### 成功响应

**HTTP 状态码**：200

**Headers**：`Content-Type: application/json`、`X-Request-ID: {uuid}`

**Body 示例 — 有可用模型**：

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen-plus",
      "object": "model",
      "owned_by": "bailian",
      "type": "chat",
      "enabled": true
    },
    {
      "id": "qwen-turbo-latest",
      "object": "model",
      "owned_by": "bailian",
      "type": "chat",
      "enabled": true
    },
    {
      "id": "deepseek-chat",
      "object": "model",
      "owned_by": "deepseek",
      "type": "chat",
      "enabled": true
    }
  ]
}
```

**Body 示例 — 无可用模型**：

```json
{
  "object": "list",
  "data": []
}
```

> 无可用模型时返回 HTTP 200 + 空列表，不返回错误。调用方可通过空 `data` 数组判断当前身份无可用模型。

**响应字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `object` | string | 固定值 `"list"` |
| `data` | array | 当前用户可用模型列表 |
| `data[].id` | string | 模型标识，可传入 `POST /v1/chat/completions` 的 `model` 字段 |
| `data[].object` | string | 固定值 `model` |
| `data[].owned_by` | string | 所属厂商标识 |
| `data[].type` | string | 模型类型：`chat`、`embedding`、`rerank` |
| `data[].enabled` | bool | 当前用户是否有权限调用（返回列表中始终为 `true`） |

#### 错误响应

| 状态码 | error.type | error.code | 场景 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `missing_token` | 未携带 Authorization Header | 添加 `Authorization: Bearer {jwt}` |
| 401 | `authentication_error` | `invalid_token` | JWT 无效、过期或签名错误 | 重新登录获取新 JWT |
| 503 | `service_unavailable` | `auth_service_unavailable` | 鉴权服务临时不可用 | 稍后重试 |
| 500 | `internal_error` | `unexpected_error` | 网关内部非预期异常 | 联系网关管理员 |

**错误响应示例**：

```json
{
  "error": {
    "type": "authentication_error",
    "message": "API Key 已被禁用，请联系管理员",
    "code": "key_disabled"
  }
}
```

---

## 5. 错误码扩充

### 5.1 新增错误码

在原有错误码表基础上新增以下错误码，归入**模型不可用类（调用方应修正认证或稍后重试）**：

| HTTP 码 | error.type | error.code | 含义 | 触发条件 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `invalid_api_key` | API Key 无效（格式不合法或不存在） | Key 格式非 `sk-` 前缀、长度不足 32 字符、auth 服务返回不存在 | 检查 Key 格式与有效性 |
| 401 | `authentication_error` | `key_disabled` | API Key 已被禁用 | auth 服务返回 Key 状态为 disabled | 联系管理员申请新 Key |
| 503 | `service_unavailable` | `auth_service_unavailable` | 鉴权服务不可达 | auth 服务超时（500ms）或连接拒绝 | 稍后重试，网关正在恢复中 |

### 5.2 变更后完整错误码表

#### 输入错误类（调用方应修正请求）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 400 | `invalid_request_error` | `invalid_json` | 请求体 JSON 格式错误 | 检查 JSON 语法 |
| 400 | `invalid_request_error` | `missing_required_field` | 缺少必填字段 | 补充 model/messages 等必填字段 |
| 400 | `invalid_request_error` | `invalid_field_value` | 字段值不合法 | 修正参数值 |
| 400 | `invalid_request_error` | `empty_messages` | messages 数组为空 | 至少提供一条消息 |
| 400 | `invalid_request_error` | `too_many_messages` | messages 超过 256 条 | 减少对话历史长度 |
| 400 | `invalid_request_error` | `request_too_large` | 请求体超过 1MB | 缩减请求体大小 |

#### 认证不可用类（调用方应修正认证或稍后重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `missing_api_key` | 未提供 API Key | 添加 Authorization Header |
| **401** | **`authentication_error`** | **`invalid_api_key`** | **API Key 格式非法或不存在** | **确认 Key 格式：`sk-{32+ chars}`** |
| **401** | **`authentication_error`** | **`key_disabled`** | **API Key 已被禁用** | **联系管理员** |
| **503** | **`service_unavailable`** | **`auth_service_unavailable`** | **鉴权服务暂时不可用** | **稍后重试** |

#### 模型不可用类（调用方应换模型或稍后重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 404 | `model_not_found` | `unknown_model` | 请求模型未注册 | 调用 GET /v1/me/models 确认可用模型 |
| 503 | `model_unavailable` | `model_disabled` | 模型已被管理员禁用 | 更换模型或稍后重试 |
| 503 | `model_unavailable` | `model_offline` | 模型临时下线维护 | 稍后重试 |

#### 临时失败类（调用方应重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 502 | `upstream_error` | `provider_error` | 厂商 API 返回错误 | 稍后重试 |
| 502 | `upstream_error` | `provider_rate_limited` | 厂商限流 | 降低调用频率后重试 |
| 502 | `upstream_error` | `stream_interrupted` | 流式传输中途中断 | 重新发起请求 |
| 504 | `upstream_timeout` | `provider_timeout` | 厂商 API 响应超时 | 稍后重试 |
| 500 | `internal_error` | `unexpected_error` | 网关内部非预期异常 | 联系网关管理员 |

> 新增/变更错误码以粗体标注。

---

## 6. 兼容性说明

### 6.1 Breaking Change

| 变更项 | Breaking? | 说明 |
| :--- | :--- | :--- |
| Authorization Header 必须 `sk-` 前缀 | **是** | 旧版 Bearer Token（无 `sk-` 前缀）将返回 401 `invalid_api_key` |
| GET /v1/models 返回过滤后列表 | **是** | 旧版客户端收到的全量列表将缩减为当前身份可用子集 |
| 鉴权失败错误码细分 | 否 | 新增 `key_disabled`、`auth_service_unavailable`，旧 error.code 仍支持 |
| 新增 GET /v1/me/models | 否 | 纯增量端点，不影响现有调用 |

### 6.2 迁移指引

1. **API Key 格式**：调用方需将现有 API Key 前添加 `sk-` 前缀（如 `a1b2c3...` → `sk-a1b2c3...`），由管理员统一发放符合新格式的 Key
2. **模型列表**：依赖 `/v1/models` 返回全量列表的调用方，应改为调用 `/v1/me/models`（语义更明确），或接受过滤后的子集
3. **错误处理**：调用方应在重试逻辑中新增对 `auth_service_unavailable`（HTTP 503）的处理——退避重试而非直接失败

### 6.3 废弃时间线

| 阶段 | 时间 | 说明 |
| :--- | :--- | :--- |
| 旧格式 Key | 立即废弃 | 不再接受无 `sk-` 前缀的 API Key |
| `/v1/models` 全量返回 | 立即变更 | 改为基于身份过滤，无过渡期 |
| `/v1/me/models` | 立即可用 | 与变更同时上线 |

---

## 11. 变更记录 — CHG-20260525-001

## 变更摘要

`POST /v1/chat/completions` 端点的 `model` 字段新增语义：除接受直接模型名外，现也可接受模型组名。组名经 scheduler 路由后解析为目标模型，调用方无感知。流式响应行为、错误响应格式、认证方式均不变。新增三个与模型组相关的错误码。

---

## 1. 行为变更 — 现有接口

### 1.1 POST /v1/chat/completions — model 字段语义变更

**变更要点**：`model` 字段从仅接受模型名扩展为可接受模型组名或模型名。Gateway 内部自动判定并路由，调用方无需修改请求体结构。

#### 请求变更

| 维度 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| `model` 字段语义 | 仅接受具体模型标识（如 `qwen-plus`、`deepseek-chat`） | 接受模型标识或模型组名（如 `日常推理`、`代码生成`） |
| 组名定义 | 无概念 | 由 control-plane 管理的模型组，组内包含多个可路由模型 |
| 路由逻辑 | 前缀匹配 Provider→直连调用 | 先判定组名/模型名；组名→scheduler 路由→Provider 调用 |
| 调用失败处理 | 立即返回错误 | 组名调用时自动 failover 一次（切换组内其他模型）；模型名调用行为不变 |

**Body 格式无变化**，调用方无需新增任何字段。组名作为 `model` 字段的值传入即可：

```json
// 组名调用 — 与模型名调用使用完全相同的请求格式
{
  "model": "日常推理",
  "messages": [
    {"role": "user", "content": "介绍一下大语言模型"}
  ],
  "stream": false
}
```

**Headers 无变化**。Authorization、Content-Type、X-Request-ID 均沿用现有约定。

#### 响应变更

**成功响应无变化**。组名调用成功返回的 `model` 字段值为 scheduler 实际选中的目标模型 ID（如 `qwen-plus`），与直接传模型名的响应语义一致：

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1716249600,
  "model": "qwen-plus",
  "choices": [...],
  "usage": {...}
}
```

> 调用方可通过日志或监控获取组名与选中模型的映射关系。`model` 字段返回的是实际调用的模型而非请求中的组名，这一行为对应 AC-GINV-01："返回与直接传模型名语义一致的结果"。

#### 鉴权行为

组名调用经过与模型名调用完全相同的鉴权中间件（API Key → User → Department），鉴权失败的行为无变化。

#### 错误响应变更

组名调用新增以下错误场景，原有错误码保持不变：

| HTTP 码 | error.type | error.code | 场景 | 触发条件 |
| :--- | :--- | :--- | :--- | :--- |
| 404 | `group_not_found` | `group_not_found` | 请求的模型组不存在 | GroupNameCache 未命中 且 scheduler 查询确认不存在 |
| 503 | `model_unavailable` | `no_available_model` | 模型组内无可用模型 | scheduler.Route 返回无可用模型 |
| 503 | `model_unavailable` | `no_other_model_available` | 首次调用失败且切换后也无可用模型 | scheduler.Failover 返回无其他可用模型 |

**完整错误响应示例**：

```json
// 模型组不存在
{
  "error": {
    "type": "group_not_found",
    "message": "模型组 '不存在的组' 不存在，请检查组名是否正确",
    "code": "group_not_found"
  }
}
```

```json
// 模型组无可用模型
{
  "error": {
    "type": "model_unavailable",
    "message": "模型组 '日常推理' 当前无可用模型，请稍后重试",
    "code": "no_available_model"
  }
}
```

```json
// 模型组无其他可用模型（切换失败）
{
  "error": {
    "type": "model_unavailable",
    "message": "模型组 '日常推理' 内无其他可用模型可供切换",
    "code": "no_other_model_available"
  }
}
```

---

### 1.2 GET /v1/models — 行为不变

**无变更**。`GET /v1/models` 仍返回当前认证用户可用的**个体模型列表**，不返回模型组信息。

| 维度 | 说明 |
| :--- | :--- |
| 返回内容 | 基于 `dept_id` 过滤后的可用模型列表（与 v1.1 行为一致） |
| 模型组信息 | **不返回**。模型组列表通过 `GET /v1/me/models`（已有）查询，其响应将在后续变更中由 control-plane/ops-console 扩展 |
| 向后兼容 | 完全保持 v1.1 行为，已有调用方无需改动 |

### 1.3 GET /v1/me/models — 不变

**无变更**。`GET /v1/me/models` 端点继续返回基于当前身份过滤后的模型列表。模型组维度的查询由 control-plane 管理端提供，不在此端点中暴露。

---

## 2. 新增错误响应

### 2.1 错误码扩充

在原有错误码表基础上新增以下错误码，归入**模型组相关（新增分类）**：

| HTTP 码 | error.type | error.code | 含义 | 触发条件 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **404** | **`group_not_found`** | **`group_not_found`** | **请求的模型组不存在** | **GroupNameCache 未命中 + scheduler 查询也未找到该组** | **确认组名是否正确；调用 GET /v1/me/models 确认可用模型组** |
| **503** | **`model_unavailable`** | **`no_available_model`** | **模型组内当前无可用模型** | **scheduler.Route 返回该组所有模型均不健康/离线** | **稍后重试；若持续出现，联系管理员检查组配置** |
| **503** | **`model_unavailable`** | **`no_other_model_available`** | **调用失败且组内无其他模型可切换** | **首次调用失败 + scheduler.Failover 返回无其他健康模型** | **稍后重试；与 no_available_model 处理方式相同** |

### 2.2 变更后完整错误码表

#### 输入错误类（调用方应修正请求）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 400 | `invalid_request_error` | `invalid_json` | 请求体 JSON 格式错误 | 检查 JSON 语法 |
| 400 | `invalid_request_error` | `missing_required_field` | 缺少必填字段 | 补充 model/messages 等必填字段 |
| 400 | `invalid_request_error` | `invalid_field_value` | 字段值不合法 | 修正参数值 |
| 400 | `invalid_request_error` | `empty_messages` | messages 数组为空 | 至少提供一条消息 |
| 400 | `invalid_request_error` | `too_many_messages` | messages 超过 256 条 | 减少对话历史长度 |
| 400 | `invalid_request_error` | `request_too_large` | 请求体超过 1MB | 缩减请求体大小 |

#### 认证不可用类（调用方应修正认证或稍后重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 401 | `authentication_error` | `missing_api_key` | 未提供 API Key | 添加 Authorization Header |
| 401 | `authentication_error` | `invalid_api_key` | API Key 格式非法或不存在 | 确认 Key 格式：`sk-{32+ chars}` |
| 401 | `authentication_error` | `key_disabled` | API Key 已被禁用 | 联系管理员 |
| 503 | `service_unavailable` | `auth_service_unavailable` | 鉴权服务暂时不可用 | 稍后重试 |

#### 模型组相关（新增分类）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| **404** | **`group_not_found`** | **`group_not_found`** | **模型组不存在** | **确认组名是否正确** |
| **503** | **`model_unavailable`** | **`no_available_model`** | **模型组内无可用模型** | **稍后重试** |
| **503** | **`model_unavailable`** | **`no_other_model_available`** | **调用失败后切换也无可用模型** | **稍后重试** |

#### 模型不可用类（调用方应换模型或稍后重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 404 | `model_not_found` | `unknown_model` | 请求模型未注册 | 调用 GET /v1/me/models 确认可用模型 |
| 503 | `model_unavailable` | `model_disabled` | 模型已被管理员禁用 | 更换模型或稍后重试 |
| 503 | `model_unavailable` | `model_offline` | 模型临时下线维护 | 稍后重试 |

#### 临时失败类（调用方应重试）

| HTTP 码 | error.type | error.code | 含义 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- |
| 502 | `upstream_error` | `provider_error` | 厂商 API 返回错误 | 稍后重试 |
| 502 | `upstream_error` | `provider_rate_limited` | 厂商限流 | 降低调用频率后重试 |
| 502 | `upstream_error` | `stream_interrupted` | 流式传输中途中断 | 重新发起请求 |
| 504 | `upstream_timeout` | `provider_timeout` | 厂商 API 响应超时 | 稍后重试 |
| 500 | `internal_error` | `unexpected_error` | 网关内部非预期异常 | 联系网关管理员 |

> 新增/变更错误码以粗体标注。其中 `group_not_found` 为新增 error.type，其余两个为 `model_unavailable` 下的新增 error.code。

---

## 3. 行为不变保证

### 3.1 模型名直连行为完全不变

当请求中的 `model` 字段为直接模型名（在 GroupNameCache 中未命中时），gateway-api 的处理行为与变更前**完全一致**：

| 维度 | 保证内容 |
| :--- | :--- |
| 路由逻辑 | 走原有 Orchestrator → Provider Registry 前缀匹配 → Provider 调用链路 |
| 模型可用性校验 | 走原有 Redis GET `model:available:{id}` 校验逻辑 |
| 错误码 | 模型不存在返回 `unknown_model`；模型不可用返回 `model_offline` 或 `model_disabled` |
| 调用失败处理 | 不触发 failover，立即返回错误 |
| 响应格式 | 成功/错误响应的 JSON 结构完全不变 |

### 3.2 错误响应格式兼容性

新增的错误响应沿用现有统一错误体格式：

```json
{
  "error": {
    "type": "...",
    "message": "...",
    "code": "..."
  }
}
```

`error.type` 新增 `group_not_found`；`error.code` 新增三个 code 值。均为**兼容性增量扩展**，不破坏现有 error.code 的解析逻辑。调用方若只处理已知 error.code 集，新增 code 不会导致解析异常。

### 3.3 流式响应行为不变

| 维度 | 保证内容 |
| :--- | :--- |
| SSE 格式 | `data: {JSON}\n\n` 格式不变；`data: [DONE]\n\n` 结束标记不变 |
| chunk 结构 | `ChatCompletionChunk` 字段不变 |
| 流式错误 | SSE `event: error` 事件格式不变 |
| failover 行为 | 流式调用中途失败**不触发切换重试**（数据已部分发送给客户端，无法透明重试） |
| 连接管理 | 连接超时、chunk 间隔超时行为不变 |

### 3.4 认证方式不变

| 维度 | 保证内容 |
| :--- | :--- |
| API Key 格式 | `sk-{32+ chars}` 不变 |
| JWT 支持 | `/v1/me/models` 继续支持 JWT 认证 |
| 鉴权链路 | AuthMiddleware → auth 服务路径不变；`UserIdentity` 注入不变 |
| 豁免路径 | `/health`、`/ready` 无需认证 |

### 3.5 其他端点无变更

以下端点行为完全不变：

| 端点 | 方法 | 说明 |
| :--- | :--- | :--- |
| `/v1/models` | GET | 返回基于身份的模型列表（与 v1.1 行为一致） |
| `/v1/me/models` | GET | 返回当前用户可用的模型列表（与 v1.1 行为一致） |
| `/health` | GET | 存活探针，检查项新增 `scheduler` |
| `/ready` | GET | 就绪探针，检查项新增 `scheduler` |

---

## 4. 兼容性说明

### 4.1 向后兼容

| 变更项 | Breaking? | 说明 |
| :--- | :--- | :--- |
| `model` 字段接受组名 | **否** | 纯增量语义扩张。模型名直连行为完全不动，组名调用为新增能力 |
| 新增错误码 | **否** | 增量错误码。不删除/重命名现有 error.code |
| 新增 `group_not_found` error.type | **否** | 增量 error.type。客户端未知 type 可 fallback 为通用错误处理 |
| GET /v1/models 不变 | **否** | 行为与 v1.1 完全一致 |

### 4.2 调用方适配指引

1. **现有调用方无需改动**。直接传模型名的调用行为完全不变。
2. **希望使用组名调用的业务方**：将 `model` 字段的值改为组名即可（如 `"日常推理"`），其余请求参数和错误处理逻辑不变。
3. **错误处理建议**：调用方应在重试逻辑中识别以下新增错误码并合理处理：
   - `group_not_found` — 不重试，确认组名是否正确
   - `no_available_model` — 退避重试（组内模型可能正在恢复）
   - `no_other_model_available` — 退避重试或降级为指定具体模型名调用

### 4.3 兼容性测试矩阵

| 场景 | 预期行为 | 兼容性 |
| :--- | :--- | :--- |
| 传已有模型名（如 `qwen-plus`） | 与变更前行为完全一致 | 完全兼容 |
| 传新增组名（如 `日常推理`） | 路由到组内模型，返回成功 | 新增能力 |
| 传不存在的组名 | 返回 404 `group_not_found` | 新增错误 |
| 组内模型全部不可用 | 返回 503 `no_available_model` | 新增错误 |
| 传已有错误模型名 | 返回 404 `unknown_model`（与变更前一致） | 完全兼容 |
| 流式调用 | 行为与变更前一致 | 完全兼容 |
