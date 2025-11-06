# gateway-api 数据模型

## 1. 文档概述

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | gateway-api |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

本模块为无状态接入层，不持有业务数据表，仅消费外部数据。所有请求处理均为实时计算，模块自身无数据库连接、无事务、无持久化写入操作。

数据消费关系概述：

| 数据源 | 消费方式 | 用途 |
| :--- | :--- | :--- |
| 模型注册配置文件 | 启动时加载 + SIGHUP 热重载 | 构建 Provider Registry |
| API Key 集合 | 请求时只读查询 Redis | 认证校验 |
| 模型可用性缓存 | 请求时只读查询 Redis | 模型可用性校验 |
| 模型列表缓存 | 请求时只读查询 Redis | /v1/models 接口返回 |

---

## 2. 实体关系总览

本模块消费的外部数据实体及运行时数据结构之间的关系：

```mermaid
erDiagram
    GATEWAY_API ||--o{ CONFIG_FILE : "启动加载"
    GATEWAY_API ||--o{ REDIS : "只读查询"
    
    CONFIG_FILE {
        list providers "厂商配置列表"
        list models "各厂商模型注册列表"
    }
    
    REDIS {
        string model_available "model:available:{modelID} → 0/1"
        string models_list "gateway:models:list → JSON Array"
        set api_keys "gateway:apikeys → Key Set"
    }
    
    GATEWAY_API {
        struct ProviderRegistry "运行时 Provider 注册表"
        struct RequestContext "每次请求的上下文"
        channel StreamChunk "流式数据管道"
        struct UnifiedError "统一错误结构"
    }
    
    GATEWAY_API ||--|{ PROVIDER_API : "HTTP/SDK 调用"
    
    PROVIDER_API {
        string bailian "百炼 DashScope API"
        string deepseek "DeepSeek API"
        string bedrock "AWS Bedrock API"
        string litellm "voocel/litellm 库"
    }
```

### 外部数据实体说明

| 实体 | 管理方 | 存储位置 | gateway-api 操作 |
| :--- | :--- | :--- | :--- |
| 模型配置 | control-plane / 运维 | config/models.yaml | 只读加载 |
| API Key 信息 | control-plane | Redis SET `gateway:apikeys` | SISMEMBER 只读校验 |
| 模型可用性状态 | control-plane | Redis STRING `model:available:{id}` | GET 只读查询 |
| 模型列表缓存 | control-plane / gateway-api | Redis STRING `gateway:models:list` | GET 只读查询（缓存未命中时从配置生成并写入） |

---

## 3. 实体定义

### 3.1 RequestContext — 请求处理上下文

每次请求在处理过程中携带的运行时上下文信息：

```go
// RequestContext 请求处理上下文
type RequestContext struct {
    RequestID  string    // 唯一请求标识（UUID v4），由中间件注入
    APIKey     string    // 调用方 API Key（脱敏：仅保留前 8 位 + 后 4 位）
    Model      string    // 请求指定的模型名称
    CallMode   CallMode  // 调用模式：Normal / Stream
    Provider   string    // 解析后匹配的 Provider 名称
    StartTime  time.Time // 请求开始时间（用于计算耗时）
}

// CallMode 调用模式枚举
type CallMode int

const (
    CallModeNormal CallMode = iota // 普通调用
    CallModeStream                  // 流式调用
)
```

### 3.2 ProviderEntry — Provider 注册条目

Provider 注册表中的每个条目：

```go
// ProviderEntry Provider 注册条目
type ProviderEntry struct {
    Prefix   string   // 模型名称匹配前缀，如 "qwen"、"deepseek"、"anthropic."
    Name     string   // Provider 名称标识，如 "bailian"、"deepseek"、"bedrock"
    Provider Provider // Provider 接口实例
    Models   []string // 该 Provider 支持的模型列表
}

// ProviderRegistry Provider 注册与分发
type ProviderRegistry struct {
    mu        sync.RWMutex
    entries   []ProviderEntry // 按前缀长度降序排列（最长匹配优先）
    providers map[string]Provider
    fallback  Provider // 兜底 Provider（voocel/litellm）
}
```

### 3.3 StreamChunk — 流式数据块

在 Go channel 中传递的流式响应数据块：

```go
// ChatCompletionChunk 流式响应数据块
type ChatCompletionChunk struct {
    ID      string        `json:"id"`       // 本次对话唯一 ID（同一流内所有 chunk 相同）
    Object  string        `json:"object"`   // 固定值 "chat.completion.chunk"
    Created int64         `json:"created"`  // Unix 时间戳（秒）
    Model   string        `json:"model"`    // 实际使用的模型名称
    Choices []ChunkChoice `json:"choices"`  // 选项列表
    Usage   *Usage        `json:"usage,omitempty"` // 仅最后一个 chunk 包含
}

// ChunkChoice 流式选项
type ChunkChoice struct {
    Index        int     `json:"index"`                   // 选项索引
    Delta        Delta   `json:"delta"`                   // 增量内容
    FinishReason *string `json:"finish_reason,omitempty"` // 结束原因
}

// Delta 增量消息
type Delta struct {
    Role    string `json:"role,omitempty"`    // 角色（仅第一个 chunk）
    Content string `json:"content,omitempty"` // 增量文本
}

// Usage token 用量统计
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### 3.4 UnifiedError — 统一错误结构

所有错误在网关内部以此结构传递，最终序列化为统一错误响应体：

```go
// GatewayError 网关统一错误（内部传递）
type GatewayError struct {
    HTTPStatus int    // HTTP 状态码（400/401/404/500/502/503/504）
    Type       string // 错误类型标识（如 invalid_request_error）
    Message    string // 人类可读的错误描述
    Code       string // 机器可读的细分错误码（如 missing_required_field）
}

// Error 实现 error 接口
func (e *GatewayError) Error() string {
    return fmt.Sprintf("[%d] %s: %s", e.HTTPStatus, e.Type, e.Message)
}

// ErrorResponse HTTP 错误响应体（序列化输出给调用方）
type ErrorResponse struct {
    Error ErrorBody `json:"error"`
}

// ErrorBody 错误详情
type ErrorBody struct {
    Type    string `json:"type"`
    Message string `json:"message"`
    Code    string `json:"code,omitempty"`
}
```

### 3.5 辅助运行时结构

```go
// ChatCompletionRequest 对话补全请求（从调用方 JSON 反序列化）
type ChatCompletionRequest struct {
    Model            string    `json:"model" binding:"required"`
    Messages         []Message `json:"messages" binding:"required,min=1"`
    Stream           *bool     `json:"stream,omitempty"`
    Temperature      *float64  `json:"temperature,omitempty"`
    TopP             *float64  `json:"top_p,omitempty"`
    MaxTokens        *int      `json:"max_tokens,omitempty"`
    Stop             any       `json:"stop,omitempty"`
    PresencePenalty  *float64  `json:"presence_penalty,omitempty"`
    FrequencyPenalty *float64  `json:"frequency_penalty,omitempty"`
}

// Message 对话消息
type Message struct {
    Role    string `json:"role" binding:"required,oneof=system user assistant"`
    Content string `json:"content" binding:"required"`
}

// ChatCompletionResponse 对话补全响应（普通模式）
type ChatCompletionResponse struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`  // "chat.completion"
    Created int64    `json:"created"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   *Usage   `json:"usage,omitempty"`
}

// Choice 响应选项
type Choice struct {
    Index        int     `json:"index"`
    Message      Message `json:"message"`
    FinishReason string  `json:"finish_reason"`
}

// ProviderConfig 厂商配置（从 YAML 文件反序列化）
type ProviderConfig struct {
    Name       string        `yaml:"name"`
    Prefix     string        `yaml:"prefix"`
    Endpoint   string        `yaml:"endpoint"`
    AuthEnv    string        `yaml:"auth_env"`
    AuthType   string        `yaml:"auth_type"`
    Region     string        `yaml:"region"`
    Timeout    time.Duration `yaml:"timeout"`
    IsFallback bool          `yaml:"is_fallback"`
    Models     []string      `yaml:"models"`
}
```

---

## 4. 关系定义

### 4.1 运行时结构关系

| 源结构 | 关系 | 目标结构 | 说明 |
| :--- | :--- | :--- | :--- |
| RequestContext | 1:1 创建 | HTTP 请求 | 每个请求创建一个 RequestContext |
| RequestContext | N:1 引用 | ProviderEntry | 多个请求可路由到同一 Provider |
| ProviderRegistry | 1:N 包含 | ProviderEntry | 注册表持有多个 Provider 条目 |
| ProviderEntry | 1:1 实现 | Provider Interface | 每个条目绑定一个接口实例 |
| ChatCompletionRequest | 1:1 产出 | ChatCompletionResponse | 普通模式：一个请求对应一个完整响应 |
| ChatCompletionRequest | 1:N 产出 | ChatCompletionChunk | 流式模式：一个请求产出多个 chunk |
| GatewayError | 1:1 序列化为 | ErrorResponse | 错误发生时转换为响应体输出 |

### 4.2 数据流向关系

```mermaid
graph LR
    Config[ProviderConfig YAML] -->|启动加载| Registry[ProviderRegistry]
    Registry -->|Resolve| Entry[ProviderEntry]
    
    Req[ChatCompletionRequest] -->|创建| Ctx[RequestContext]
    Ctx -->|路由| Entry
    
    Entry -->|普通调用| Resp[ChatCompletionResponse]
    Entry -->|流式调用| Chunk[ChatCompletionChunk channel]
    
    Entry -->|错误| Err[GatewayError]
    Err -->|序列化| ErrResp[ErrorResponse]
```

---

## 5. 索引设计

不适用——本模块不管理数据库表。

---

## 6. 事务边界

不适用——本模块无数据库写入操作。

---

## 7. 迁移策略

不适用——本模块无自有数据表。

---

## 8. 数据约束与规则

### 8.1 请求数据校验规则

| 字段 | 约束 | 失败错误码 |
| :--- | :--- | :--- |
| `model` | 必填，非空字符串 | `missing_required_field` |
| `messages` | 必填，数组长度 ≥ 1 且 ≤ 256 | `empty_messages` / `too_many_messages` |
| `messages[].role` | 必填，枚举值：system / user / assistant | `invalid_field_value` |
| `messages[].content` | 必填，非空字符串 | `invalid_field_value` |
| `temperature` | 可选，范围 [0, 2] | `invalid_field_value` |
| `top_p` | 可选，范围 (0, 1] | `invalid_field_value` |
| `max_tokens` | 可选，正整数 | `invalid_field_value` |
| `presence_penalty` | 可选，范围 [-2, 2] | `invalid_field_value` |
| `frequency_penalty` | 可选，范围 [-2, 2] | `invalid_field_value` |
| 请求体大小 | ≤ 1MB | `request_too_large` |
| JSON 格式 | 合法 JSON | `invalid_json` |

### 8.2 API Key 格式约束

| 约束 | 值 |
| :--- | :--- |
| 长度范围 | 32 ~ 128 字符 |
| 允许字符 | 字母（a-z A-Z）、数字（0-9）、连字符（-） |
| 传递方式 | HTTP Header `Authorization: Bearer {key}` |

### 8.3 运行时数据不变式

| 不变式 | 说明 |
| :--- | :--- |
| RequestContext.RequestID 非空 | 中间件保证注入，缺失时自动生成 UUID v4 |
| ProviderRegistry.fallback 非 nil | 初始化时必须设置兜底 Provider |
| ProviderRegistry.entries 按前缀长度降序 | 保证最长前缀优先匹配 |
| ChatCompletionChunk.ID 同流一致 | 同一次流式调用产出的所有 chunk 共享相同 ID |
| chunk channel 关闭表示流结束 | 消费方通过 channel 关闭判断流结束 |
| GatewayError.HTTPStatus 范围有效 | 仅允许 400/401/404/500/502/503/504 |

### 8.4 Redis 数据格式约束

| Key Pattern | 类型 | Value 格式 | 说明 |
| :--- | :--- | :--- | :--- |
| `model:available:{modelID}` | STRING | `"1"` 或 `"0"` | 1=可用，0=不可用 |
| `gateway:models:list` | STRING | JSON Array（Model 对象数组） | 模型列表缓存 |
| `gateway:apikeys` | SET | 每个成员为一个合法 API Key 字符串 | Key 校验集合 |

### 8.5 缓存未命中处理规则

| 场景 | 处理策略 | 理由 |
| :--- | :--- | :--- |
| `model:available:{id}` 未命中 | 视为可用（fail-open） | 避免缓存故障导致全部请求被拒 |
| `gateway:models:list` 未命中 | 从配置文件生成模型列表 | 保证接口可用 |
| `gateway:apikeys` 查询失败 | 降级为本地配置校验 | 保证认证链路可用 |
| Redis 完全不可达 | 所有校验降级放行 | 网关核心链路优先保可用性 |
