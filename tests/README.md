# 测试原则与方案

## 核心理念

**只测业务逻辑，不测框架代码。** 测试的目的是验证"我们的代码做对了该做的事"，不是验证 Go/Vue/Gin/Element Plus 能正常工作。

## 什么要测

```
请求链路: 鉴权 → 模型组调度 → 健康过滤 → 策略路由 → 厂商适配 → HTTP 调用
管理链路: 渠道 CRUD → 模型 CRUD → 模型组 CRUD → Catalog 同步 → 配置下发
```

| 优先级 | 模块 | 理由 |
|--------|------|------|
| P0 | `gateway/middleware/auth.go` | 安全边界，API Key 验证错了全站瘫痪 |
| P0 | `scheduler/` (调度 + 健康 + 故障转移) | 路由决策核心，选错模型直接影响用户 |
| P0 | `llmclient/` (重试 + 熔断) | 上游调用的韧性层，重试/熔断逻辑错了会雪崩 |
| P0 | `providers/<vendor>/` (3-4 个高频厂商) | 请求/响应格式转换，格式错了上游不认 |
| P0 | `controlplane/service/` (核心 service) | 管理面业务逻辑，CRUD 校验/聚合/同步 |
| P1 | `gateway/handler/chat.go` | 请求入口，编排全链路 |
| P1 | `orchestrator/group_dispatcher.go` | 模型组解析与绑定校验 |
| P1 | `loader/provider_loader.go` | 配置热加载，原子交换逻辑 |
| P1 | `fallback/resolver.go` | 回退链计算（9 条件排序） |
| P2 | `auth/` (JWT/OIDC/Key 验证库) | 鉴权库，被 gateway 内嵌使用 |
| P2 | `errormap/` | 厂商错误码标准化 |
| P2 | `configsync/` | Redis 快照读写 |

## 什么不测

| 不测 | 理由 |
|------|------|
| 前端组件渲染 | TypeScript 编译 + Vite 构建已足够 |
| 配置文件加载 (config/) | 启动即发现，无业务逻辑 |
| 类型定义 (model/) | 纯数据结构，编译器覆盖 |
| 路由注册 (router/) | 框架线程，无业务逻辑 |
| main 函数 (cmd/) | 进程启动代码，集成测试覆盖 |
| 数据库 Migration | 一次性脚本，上线即验证 |

## 测试分层

### 单元测试 (95% 的测试量)

```go
// 模式：接口注入 + 手写 fake + 表驱动
type fakeScheduler struct {
    selectResult string
    selectErr    error
}

func (f *fakeScheduler) SelectModel(groupID string, healthFilter bool) (string, error) {
    return f.selectResult, f.selectErr
}

func TestGroupDispatcher_Dispatch(t *testing.T) {
    tests := []struct {
        name    string
        fake    *fakeScheduler
        want    string
        wantErr bool
    }{
        {"正常调度", &fakeScheduler{selectResult: "model-1"}, "model-1", false},
        {"无可用模型", &fakeScheduler{selectErr: ErrNoAvailableModel}, "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

**原则**：
- 依赖通过接口注入，不依赖具体类型
- 手写 fake（struct + 函数字段），不用 mock 框架
- 表驱动测试覆盖多场景
- 外部测试包（`package xxx_test`），仅访问公开 API

**适用于**：service 层、调度器、熔断器、回退链、适配器、所有纯逻辑模块

### 组件测试 (Handler + 中间件)

```go
// 模式：httptest + Gin TestMode + fake service
func TestChatHandler_Handle(t *testing.T) {
    gin.SetMode(gin.TestMode)
    fakeOrch := &fakeOrchestrator{result: &core.ChatResponse{...}}
    handler := NewChatHandler(fakeOrch)
    
    router := gin.New()
    router.POST("/v1/chat/completions", handler.Handle)
    
    w := httptest.NewRecorder()
    req, _ := http.NewRequest("POST", "/v1/chat/completions", body)
    router.ServeHTTP(w, req)
    
    assert.Equal(t, 200, w.Code)
}
```

**原则**：
- 用 `httptest.ResponseRecorder` + 真实 Gin 路由
- Service 层全部 fake，不连数据库/Redis
- 只断言 HTTP 状态码 + 响应体关键字段
- 需要 mock Redis 时用 `miniredis`

**适用于**：handler 层、middleware 层

### 集成测试 (关键链路)

```go
// 模式：Docker Compose 启动 PG + Redis，走真实 HTTP
// +build integration

func TestChatCompletionFullFlow(t *testing.T) {
    db := connectTestDB(t)
    seedTestData(t, db)  // 预先插入 provider/model/group/key
    
    cp := startControlPlane(t, db)
    gw := startGateway(t, cp.URL)
    
    resp := callChatCompletion(t, gw.URL, "test-api-key", "gpt-4o-group")
    assert.Equal(t, 200, resp.StatusCode)
}
```

**原则**：
- 仅覆盖最核心的 2-3 条链路
- 需要 `go:build integration` tag，`go test ./...` 默认不跑
- CI 中用 Docker Compose 启动依赖

**适用于**：完整请求链路、配置热加载、故障转移

## 测试文件组织

测试代码与业务代码**物理分离**，统一放在 `tests/` 下，按模块镜像 `internal/` 结构：

```
tests/
├── README.md                           ← 本文档
├── contract/
│   └── contracts.json                 ← FE↔BE 接口契约规范（非测试代码）
│
├── gateway/                            ← 镜像 internal/gateway/
│   ├── middleware/
│   │   └── auth_test.go               ← package middleware_test
│   ├── handler/
│   │   └── chat_test.go
│   ├── orchestrator/
│   │   └── group_dispatcher_test.go
│   └── scheduler/
│       ├── scheduler_test.go
│       └── failover_test.go
│
├── controlplane/                       ← 镜像 internal/controlplane/
│   ├── service/
│   │   ├── provider_service_test.go
│   │   ├── model_service_test.go
│   │   └── sync_service_test.go
│   └── handler/
│       └── internal_handler_test.go
│
├── providers/                          ← 镜像 internal/providers/
│   ├── openai/
│   │   └── adapter_test.go
│   ├── anthropic/
│   │   └── adapter_test.go
│   └── deepseek/
│       └── adapter_test.go
│
├── llmclient/                          ← 镜像 internal/llmclient/
│   ├── client_test.go
│   └── circuit_breaker_test.go
│
├── fallback/
│   └── resolver_test.go
│
├── auth/                               ← 镜像 internal/auth/
│   └── key_auth_test.go
│
└── integration/                        ← 集成测试（go:build integration tag）
    ├── chat_flow_test.go
    └── hot_reload_test.go
```

**注意**：测试文件在独立包中（如 `package middleware_test`），只能访问被测包的**公开 API**。被测代码需要提供接口用于依赖注入。

## 命名规范

| Go 测试 | 格式 |
|---------|------|
| 包名 | `package <被测模块>_test`，如 `package scheduler_test` |
| 测试函数 | `Test<函数名>_<场景>`，如 `TestSelectModel_NoHealthyModel` |
| 表驱动子测试 | `t.Run("无健康模型", ...)`，中文描述 |
| Fake 类型 | `fakeXxx` / `stubXxx`，如 `fakeScheduler`, `stubKeyCache` |
| 测试文件 | `<模块>_test.go`，按被测模块命名 |

## 运行命令

```bash
go test ./tests/... -count=1                    # 全部单元测试 + 组件测试
go test ./tests/... -tags=integration -count=1  # 含集成测试（需 Docker）
go test ./tests/gateway/scheduler/... -v        # 只跑指定模块
```

## 注释规范

每个测试用例**必须**带中文注释，说明「测什么、怎么测、期望什么」，方便开发人员快速理解意图。注释放在表驱动测试的 `description` 字段或子测试名中。

```go
tests := []struct {
    name        string
    description string // 中文说明：测什么、关键条件、期望结果
    input       string
    want        string
    wantErr     bool
}{
    {
        name:        "正常调度到健康模型",
        description: "组内有 3 个模型，其中 1 个不健康，调度器应选出健康的且权重最高的模型",
        input:       "group-1",
        want:        "model-a",
        wantErr:     false,
    },
    {
        name:        "全部模型不健康",
        description: "组内所有模型都处于不健康状态，调度器应返回 ErrNoAvailableModel",
        input:       "group-2",
        want:        "",
        wantErr:     true,
    },
    {
        name:        "空模型组",
        description: "模型组 ID 存在但组内没有注册任何模型，应返回 ErrModelGroupUnavailable",
        input:       "group-empty",
        want:        "",
        wantErr:     true,
    },
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // tt.description 已在表头注释，此处只做断言
        got, err := scheduler.SelectModel(tt.input, true)
        if tt.wantErr {
            assert.Error(t, err)
            return
        }
        assert.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

**原则**：
- 表驱动测试必须包含 `description` 字段，用中文写清楚测试意图
- 非表驱动测试在函数体开头用 `// 测试目的：xxx` 注释
- Fake/Mock 类型用 `// fakeXxx 模拟 xxx 用于测试 yyy 场景` 注释
- 复杂断言前加一行注释解释断言逻辑

## 禁止事项

- ❌ 不要测试前端（TypeScript 编译 + Vite 构建足够）
- ❌ 不要用 `gomock`/`mockgen`（手写 fake 更直观）
- ❌ 不要测私有方法（通过公共接口覆盖）
- ❌ 不要测框架行为（Gin 路由匹配、GORM SQL 生成）
- ❌ 不要一个测试函数测多个不相关场景（用表驱动替代）
- ❌ 不要写端到端测试（Playwright/Cypher）
- ❌ 不要写无注释的测试用例（后人看不懂为什么测）