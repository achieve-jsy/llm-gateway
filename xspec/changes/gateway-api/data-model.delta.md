# gateway-api 数据模型变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-api/data-model.md (v1.3)

## 变更摘要

gateway-api 无状态，不新增持久化表。仅新增内存数据结构 UsageEvent 和 Collector 配置。

## 新增内存结构

### UsageEvent

```go
// internal/gateway/observability/event.go

type UsageEvent struct {
    RequestID string
    Timestamp time.Time

    // 身份维度
    KeyID    string
    UserID   string
    DeptID   string
    StudioID string

    // 路由/模型
    Model       string
    RoutedModel string
    GroupName   string
    Provider    string

    // Token 用量
    PromptTokens     int
    CachedTokens     int
    CacheWriteTokens int
    CompletionTokens int
    ReasoningTokens  int
    TotalTokens      int
    UsageMissing     bool

    // 状态
    Status         int
    UpstreamStatus int
    DurationMs     int64
    Stream         bool
    FailoverUsed   bool

    // 成本（采集时锁价）
    InputPriceSnapshot  float64
    OutputPriceSnapshot float64
    EstCostUSD          float64
}
```

### Collector 配置

```go
type CollectorConfig struct {
    Enabled       bool          // 默认 true
    BufferSize    int           // 默认 10000
    FlushN        int           // 默认 200
    FlushInterval time.Duration // 默认 1s
}
```

## 配置项

| 变量 | 默认 | 说明 |
| :--- | :--- | :--- |
| USAGE_COLLECT_ENABLED | true | 埋点总开关 |
| USAGE_BUFFER_SIZE | 10000 | channel 缓冲容量 |
| USAGE_FLUSH_N | 200 | 批量写条数阈值 |
| USAGE_FLUSH_INTERVAL | 1s | 批量写时间阈值 |