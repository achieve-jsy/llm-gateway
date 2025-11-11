# scheduler Web 后端接口契约

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260525-001 |
| **模块名称** | scheduler |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-25 |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.0 | 2026-05-25 | 初稿创建 | OpenCode |

---

## 1. 概览

本文档定义 scheduler 嵌入 gateway-api 进程内的 Go 接口契约。scheduler 不暴露 HTTP 端点，通过 Go interface 提供服务。

| 属性 | 内容 |
| :--- | :--- |
| **生产方（Provider）** | scheduler（Go package，嵌入 gateway-api 进程内运行） |
| **消费方（Consumer）** | gateway-api（上层调用方） |
| **当前版本** | `v1.0.0` |
| **最后更新** | 2026-05-25 |

---

## 2. 公共约定

> 适用于本契约所有接口的全局规则。

| 约定 | 内容 |
| :--- | :--- |
| **调用方式** | Go 函数调用（同步） |
| **错误返回** | Go error interface |
| **并发安全** | 所有接口 goroutine-safe |
| **数据格式** | Go struct（进程内无需序列化） |
| **认证方式** | 无（进程内调用，无认证需求） |

**幂等性定义**：相同请求多次执行，服务端状态与执行一次相同。

| 接口 | 幂等 |
| :--- | :---: |
| Route | ❌（轮询和加权策略每次可能返回不同结果） |
| Failover | ❌（可能返回不同备选模型） |
| GetGroupStatus | ✅（纯查询，无副作用） |
| NotifyFailure | ❌（每次调用更新健康状态） |

---

## 3. 端点索引

| 端点 | 调用方式 | 用途 | 并发安全 | 幂等 |
| :--- | :---: | :--- | :---: | :---: |
| Route(groupName) | Go func → *RouteDecision | 路由决策，返回选中模型 | 是 | 否 |
| Failover(groupName, failedModelID) | Go func → *RouteDecision | 故障切换，排除失败模型后重新路由 | 是 | 否 |
| GetGroupStatus(groupName) | Go func → *GroupStatus | 查询组内所有模型健康状态 | 是 | 是 |
| NotifyFailure(modelID) | Go func（异步） | 被动上报调用失败 | 是 | 否 |
| Shutdown() | Go func | 优雅关闭 | 是 | — |

---

## 4. 端点详细设计

### 4.1 接口契约概述

scheduler 作为 Go package 嵌入 gateway-api 进程内运行，不暴露 HTTP 端点。所有接口均为 **Go interface**，通过函数调用方式使用。本章节以合约风格记录所有内部接口的方法签名、参数、返回值、错误语义及调用约束。

**调用关系**：
- **gateway-api → scheduler**：调用 `Scheduler.Route()` / `Scheduler.Failover()` / `Scheduler.NotifyFailure()`
- **scheduler → control-plane**：HTTP GET 拉取全量配置
- **scheduler → 厂商渠道**：HTTP GET 执行健康探测

---

### 4.2 Scheduler 顶层接口

gateway-api 通过 `Scheduler` 结构体使用调度能力。

```go
type Scheduler struct {
    // 内部字段，gateway-api 不直接访问
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error)

func (s *Scheduler) Route(groupName string) (*RouteDecision, error)

func (s *Scheduler) Failover(groupName string, failedModelID string) (*RouteDecision, error)

func (s *Scheduler) NotifyFailure(modelID string)

func (s *Scheduler) Shutdown()
```

---

### 4.3 Router 接口

#### 4.3.1 接口定义

```go
type Router interface {
    // Route 根据组名执行路由决策，返回选中的目标模型信息。
    //
    // 参数：
    //   groupName: 模型组名称，由 gateway-api 传入。
    //
    // 返回值：
    //   *RouteDecision: 路由决策结果，包含选中模型、渠道信息等。
    //   error: 路由失败时返回具体错误类型。
    //
    // 错误类型：
    //   ErrGroupNotFound    — 组名不存在于路由表中
    //   ErrNoAvailableModel — 组内所有模型均不健康
    //
    // 性能约束：
    //   必须在 10ms 内完成，不含网络 I/O。
    Route(groupName string) (*RouteDecision, error)
}
```

#### 4.3.2 RouteDecision 结构体

```go
type RouteDecision struct {
    // GroupName 路由所在模型组名称
    GroupName string

    // Strategy 使用的路由策略
    Strategy string

    // SelectedModelID 选中的模型唯一标识
    SelectedModelID string

    // SelectedModelName 选中的模型展示名称
    SelectedModelName string

    // ChannelID 选中模型对应的渠道 ID
    ChannelID string

    // ChannelEndpoint 渠道端点 URL，gateway-api 直接用此地址发起模型调用
    ChannelEndpoint string

    // ChannelCredential 渠道认证凭证
    ChannelCredential string

    // FallbackAvailable 同组是否还有可用备选模型（用于 gateway-api 判断是否需要后续切换）
    FallbackAvailable bool

    // DecisionTimeUs 路由决策耗时（微秒），用于性能观测
    DecisionTimeUs int64
}
```

#### 4.3.3 调用语义

| 维度 | 约束 |
| :--- | :--- |
| 并发安全 | 是。Router 内部使用 sync.RWMutex 保护 RouteTable 读操作，多 goroutine 可安全并发调用 Route |
| 幂等性 | 否。轮询和加权策略每次调用可能返回不同结果 |
| 无副作用 | Route 方法不修改任何持久状态（除轮询计数器使用 atomic 递增） |
| 超时 | 不设置。纯内存操作，应在 10ms 内完成 |

---

### 4.4 FailoverHandler 接口

#### 4.4.1 接口定义

```go
type FailoverHandler interface {
    // Failover 排除已失败模型后重新执行路由决策。
    //
    // 参数：
    //   groupName:     模型组名称。
    //   failedModelID: 已调用失败的模型 ID，本次路由将排除此模型。
    //
    // 返回值：
    //   *RouteDecision: 新选中的目标模型信息。
    //   error: 切换失败时返回具体错误类型。
    //
    // 错误类型：
    //   ErrGroupNotFound — 组名不存在于路由表中
    //   ErrNoOtherModel  — 同组内排除已失败模型后无其他可用模型
    //
    // 副作用：
    //   内部会调用 HealthProber.NotifyFailure(failedModelID) 记录失败事件。
    //
    // 性能约束：
    //   必须在 10ms 内完成，不含网络 I/O。
    Failover(groupName string, failedModelID string) (*RouteDecision, error)
}
```

#### 4.4.2 调用语义

| 维度 | 约束 |
| :--- | :--- |
| 并发安全 | 是 |
| 幂等性 | 否。可能返回不同备选模型 |
| 限制 | 单次调用只排除一个失败模型。若备选模型也失败，gateway-api 需再次调用 Failover |
| 降级 | 返回 `ErrNoOtherModel` 时 gateway-api 应向业务方返回 503，不可无限重试 |

---

### 4.5 ConfigProvider 接口

#### 4.5.1 接口定义

```go
type ConfigProvider interface {
    // FetchConfig 从 control-plane HTTP 拉取全量路由配置。
    //
    // 返回值：
    //   *ControlPlaneConfig: 全量模型组配置。
    //   error: 网络错误、解析错误等。
    //
    // 错误处理：
    //   调用方（ConfigSyncer）在返回 error 时不更新路由表，保留上一次成功配置。
    FetchConfig() (*ControlPlaneConfig, error)
}
```

#### 4.5.2 ControlPlaneConfig 结构体

```go
type ControlPlaneConfig struct {
    Version string        `json:"version"`
    Groups  []GroupConfig `json:"groups"`
}

type GroupConfig struct {
    GroupName string          `json:"group_name"`
    GroupID   string          `json:"group_id"`
    Strategy  RoutingStrategy `json:"strategy"`
    Enabled   bool            `json:"enabled"`
    Models    []GroupModel    `json:"models"`
}

type GroupModel struct {
    ModelID     string          `json:"model_id"`
    ModelName   string          `json:"model_name"`
    GroupID     string          `json:"group_id"`
    Priority    int             `json:"priority"`
    Weight      int             `json:"weight"`
    ChannelInfo ChannelEndpoint `json:"channel_info"`
}

type ChannelEndpoint struct {
    ChannelID  string `json:"channel_id"`
    Endpoint   string `json:"endpoint"`
    Credential string `json:"credential"`
    TimeoutMs  int    `json:"timeout_ms"`
}
```

#### 4.5.3 HTTP 拉取约定

| 维度 | 约定 |
| :--- | :--- |
| 请求方法 | GET |
| 端点 | `{control_plane_base_url}/api/v1/config/routing` |
| 请求头 | `Accept: application/json`，`X-Service: scheduler` |
| 响应状态码 | 200 成功，4xx/5xx 视为错误 |
| 超时 | 连接超时 2s，读取超时 5s |
| 重试 | 不重试，等待下一个定时周期（5s）自动拉取 |
| 失败处理 | 保留上一次成功配置，记录 warn 日志 |

---

### 4.6 HealthProber 接口

#### 4.6.1 接口定义

```go
type HealthProber interface {
    // NotifyFailure 被动检测入口，gateway-api 调用失败时通知。
    //
    // 参数：
    //   modelID: 调用失败的模型 ID。
    //
    // 副作用：
    //   立即更新该模型的健康评分，可能触发状态变更。
    //   该方法异步执行，不阻塞调用方。
    //
    // 调用约束：
    //   同一 modelID 可并发调用，HealthStore 内部加锁保护。
    NotifyFailure(modelID string)

    // Run 启动主动探测循环，后台 goroutine 调用。
    //
    // 行为：
    //   每 probeInterval（默认 30s）遍历所有模型，并发执行 HTTP 健康探测。
    //   并发上限由 worker pool 控制（默认 50）。
    //
    // 退出：
    //   通过 stopCh channel 通知退出。
    Run()
}
```

#### 4.6.2 健康探测 HTTP 约定

| 维度 | 约定 |
| :--- | :--- |
| 请求方法 | GET |
| 端点 | `{channel_endpoint}/health` |
| 超时 | 由 ChannelEndpoint.TimeoutMs 指定，默认 5000ms |
| 成功判定 | HTTP 状态码 2xx |
| 临时错误 | 429 Too Many Requests 不计入失败，保持当前状态 |
| 失败判定 | 连接超时、连接拒绝、DNS 解析失败、状态码 5xx |
| 并发控制 | worker pool 上限 50 goroutine |

---

### 4.7 HealthEvent — 健康状态变更事件

```go
type HealthEvent struct {
    // ModelID 状态变更的模型 ID
    ModelID string

    // GroupID 模型所属组 ID
    GroupID string

    // ModelName 模型展示名称
    ModelName string

    // OldStatus 变更前状态
    OldStatus HealthStatus

    // NewStatus 变更后状态
    NewStatus HealthStatus

    // Reason 变更原因：consecutive_failures / consecutive_successes / quick_fail / probe_fail / probe_success
    Reason string

    // Timestamp 变更发生时间
    Timestamp time.Time
}
```

**事件类型与原因映射**：

| 旧状态 | 新状态 | Reason |
| :--- | :--- | :--- |
| Healthy | Unhealthy | `consecutive_failures` — 5s 窗口内失败 ≥3 次 |
| Healthy | Unhealthy | `quick_fail` — 60s 内快速失败 ≥5 次 |
| Unhealthy | Healthy | `consecutive_successes` — 连续成功 ≥3 次 |

事件通过 EventPublisher 异步发布至 observability-hub，不阻塞健康探测主流程。

---

### 4.8 SchedulerConfig — 初始化配置

```go
type SchedulerConfig struct {
    // ControlPlaneURL control-plane 地址
    // 示例: "http://control-plane:8080"
    ControlPlaneURL string

    // ConfigSyncInterval 配置同步间隔，默认 5s
    ConfigSyncInterval time.Duration

    // HealthProbeInterval 健康探测间隔，默认 30s
    HealthProbeInterval time.Duration

    // MaxProbeConcurrency 探测并发上限，默认 50
    MaxProbeConcurrency int

    // FailureThreshold 5s 滑动窗口内失败次数阈值，默认 3
    FailureThreshold int

    // RecoverySuccessThreshold 连续成功次数阈值，默认 3
    RecoverySuccessThreshold int

    // QuickFailWindow 快速失败统计窗口，默认 60s
    QuickFailWindow time.Duration

    // QuickFailThreshold 快速失败次数阈值，默认 5
    QuickFailThreshold int

    // ChannelProbeTimeout 单次渠道探测超时，默认 5s（当 ChannelEndpoint.TimeoutMs 未配置时使用）
    ChannelProbeTimeout time.Duration
}

// DefaultConfig 返回默认配置
func DefaultConfig(controlPlaneURL string) SchedulerConfig {
    return SchedulerConfig{
        ControlPlaneURL:         controlPlaneURL,
        ConfigSyncInterval:      5 * time.Second,
        HealthProbeInterval:     30 * time.Second,
        MaxProbeConcurrency:     50,
        FailureThreshold:        3,
        RecoverySuccessThreshold: 3,
        QuickFailWindow:         60 * time.Second,
        QuickFailThreshold:      5,
        ChannelProbeTimeout:     5 * time.Second,
    }
}
```

---

## 5. 错误码

### 5.1 错误类型定义

```go
// ErrGroupNotFound 组名不存在于路由表中。
// gateway-api 应确保在调用前校验组名有效性，此错误表示配置同步异常或组名拼写错误。
var ErrGroupNotFound = errors.New("scheduler: group not found")

// ErrNoAvailableModel 组内所有模型均不健康，无法选出可路由模型。
// gateway-api 应向业务方返回 503 Service Unavailable，提示"该模型组当前无可用模型"。
var ErrNoAvailableModel = errors.New("scheduler: no available model in group")

// ErrNoOtherModel 故障切换时，排除已失败模型后无其他可用模型。
// gateway-api 应向业务方返回调用失败，不可进一步重试。
var ErrNoOtherModel = errors.New("scheduler: no other available model for failover")

// ErrConfigFetchFailed 从 control-plane 拉取配置失败。
// 内部使用，不对外暴露。ConfigSyncer 在上层捕获后记录日志并保留上次配置。
var ErrConfigFetchFailed = errors.New("scheduler: failed to fetch config from control-plane")

// ErrInvalidConfig 从 control-plane 拉取的配置格式无效。
// 内部使用，不对外暴露。
var ErrInvalidConfig = errors.New("scheduler: invalid config from control-plane")
```

### 5.2 错误处理语义

| 错误 | 调用方 | 处理方式 | 是否需要重试 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `ErrGroupNotFound` | gateway-api | 返回 400 Bad Request | 否 | 组名在调用前已校验，出现此错误说明配置不一致 |
| `ErrNoAvailableModel` | gateway-api | 返回 503 Service Unavailable | 否（可启用降级模式直连首个模型） | 所有模型不健康，降级策略由 gateway-api 配置决定 |
| `ErrNoOtherModel` | gateway-api | 返回 502 Bad Gateway（上游调用失败 + 无备选） | 否 | 首次调用失败且无备选模型 |
| `ErrConfigFetchFailed` | ConfigSyncer（内部） | 保留上一次配置，Warn 日志 | 自动（下个周期重试） | 对路由决策无影响 |
| `ErrInvalidConfig` | ConfigSyncer（内部） | 跳过本轮同步，Error 日志 | 自动（下个周期正常拉取可能修复） | 对路由决策无影响 |

---

## 6. 版本策略与 Breaking Change 规则

scheduler 作为内部 package 随 gateway-api 同版本发布，接口变更遵循语义化版本。Breaking change 需在 gateway-api changelog 中标注。

**当前版本**：`v1.0.0`

**Breaking Change（必须升大版本）：**
- 删除已有接口方法
- 修改已有方法的签名（参数或返回值类型）
- 修改已有错误类型的语义
- 新增接口必填字段

**非 Breaking Change（可在当前版本直接发布）：**
- 新增接口方法
- 新增可选字段
- 新增错误类型（不修改已有）
- 新增 SchedulerConfig 配置项（有默认值）

**废弃流程**：
1. 废弃方法加 `// Deprecated:` 注释，标注替代方案
2. 保留至少 2 个月后方可移除
3. 移除前在 gateway-api changelog 中通知

---

## 7. 非功能约束（适用时填）

| 约束类型 | 值 | 超出时的行为 |
| :--- | :--- | :--- |
| Route() 延迟 | <10ms P99 | 记录 warn 日志，不阻塞调用 |
| Failover() 延迟 | <10ms P99 | 记录 warn 日志，不阻塞调用 |
| GetGroupStatus() 延迟 | <5ms P99 | 记录 warn 日志 |
| 并发安全 | 支持多 goroutine 同时调用所有接口 | — |
| 配置同步间隔 | 5s（可配置） | — |
| 健康探测间隔 | 30s（可配置） | — |
| 探测并发上限 | 50 goroutine（可配置） | 排队等待 |

---

## 8. 接口安全

不适用 — scheduler 仅通过进程内函数调用提供服务，无网络暴露面。

---

## 9. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 | 变更人 |
| :--- | :--- | :---: | :--- | :--- |
| v1.0.0 | 2026-05-25 | — | 初始版本 | OpenCode |