# Backend Go TDD 测试改造报告

> 由 Team B 完成。本次任务在保持生产代码语义不变的前提下，引入接口抽象、修复编译/失败用例、补齐缺口单测，并产出覆盖率与运行报告。

- 测试命令：`go test ./... -count=1 -timeout=120s -cover -coverprofile=tests/reports/backend-coverage.out`
- 函数级覆盖：`tests/reports/backend-coverage.txt`
- 运行日志：`tests/reports/backend-test-output.txt`
- 总体结果：**全部 PASS**（`go test ./...` 退出码 0），受覆盖包总语句覆盖率 **54.3%**（含未列入需求的 `cmd/`、`adapter/*`、`server`、`model` 等纯 wiring/类型包，这些包合计零覆盖；**所需 14 个包全部 ≥ 60%，多数 ≥ 90%**）。

---

## 1. 编译错误修复清单

> 所有修复严格遵循"生产代码最小变更 + 测试通过接口注入"原则；测试中所有 `unsafe.Pointer` 强转一律移除。

| # | 文件 | 根因 | 修复方式 | 影响面 |
|---|------|------|----------|--------|
| 1 | `internal/gateway/handler/models.go` | `*MockRegistry` / `*infra.Cache` 测试时无法注入到具体结构体字段 | 引入 `ModelsCacheInterface` / `ModelsRegistryInterface`，`NewModelsHandler` 改为接受接口 | 路由装配处的具体类型仍实现接口，零调用方调整 |
| 2 | `internal/gateway/handler/chat.go` | 测试用 `unsafe.Pointer` 注入 `MockOrchestrator` | 新增 `ChatOrchestratorInterface`，`NewChatHandler` 接收接口；具体 `*orchestrator.ChatOrchestrator` 同时实现 | 同上 |
| 3 | `internal/gateway/handler/health.go` 等使用 `*provider.Registry` | 缺乏测试钩子 | `Registry` 保留具体类型（含状态），仅在 handler/service 入口处通过接口注入；测试侧使用 `provider.NewRegistry()` 真实实例 | 无（仅测试代码受益） |
| 4 | `internal/controlplane/handler/sync_handler.go` | `*MockSyncService` 无法转 `*service.SyncService` | 引入 `SyncServiceInterface`，`NewSyncHandler` 接收接口 | 装配处调用兼容 |
| 5 | `internal/controlplane/service/sync_service.go` | `*MockProviderRepo` / `*MockModelRepo` 不能转具体 repo | 引入 `SyncProviderRepo` / `SyncModelRepo` 接口（仅暴露所需方法子集） | 兼容 `repository.*Repo` |
| 6 | `internal/controlplane/handler/{model,internal,definition,test,model_tag,provider}_handler.go` | 同 1/4 | 各自新增 `ModelServiceInterface` / `InternalServiceInterface` / `DefinitionRepoInterface` / `TestProviderRepo`+`TestModelRepo` / `ModelTagRepoInterface` / `ProviderServiceInterface` 并改造构造函数 | 装配处兼容 |
| 7 | `internal/controlplane/service/{provider,model}_service.go` | repository 强类型阻碍 mock | 新增 `ProviderRepoInterface` / `ModelRepoInterface`（位于 `provider_service.go`，被 `model_service.go`、`internal_service.go` 复用） | 装配处兼容 |
| 8 | `internal/controlplane/handler/test_handler.go` | `testModel := testModel` 引用未定义变量，编译失败 | 改为 `testModel := provider.TestModel` | 复现生产意图 |
| 9 | `internal/gateway/middleware/middleware_test.go` | 未使用 `log/slog` 导入；`io.Reader.Read` 误写为单返回值；`nextCalled` 未使用 | 清理导入；改用 `io.ReadAll`；重写 `TestRequestIDMiddlewareNext` 使用 `gin.New()` 路由完成链路测试 | 仅测试 |
| 10 | `internal/controlplane/middleware/middleware_test.go` | 未使用 `strings` 导入 | 删除 | 仅测试 |
| 11 | `internal/gateway/infra/cache_test.go` | `cache` 变量声明未使用 | 直接删除并清理无用 mock 类型 | 仅测试 |
| 12 | `internal/controlplane/handler/model_tag_handler_test.go` | `mockRepo` 未使用；mock 与具体 repo 类型冲突 | 整体重写：新增 `MockModelTagRepo` 实现接口，统一 `newTagRouter` 装配 | 仅测试 |
| 13 | `internal/gateway/provider/registry_test.go` | `p, ok := registry.GetProvider("openai")` 未使用 `p` | `if _, ok := registry.GetProvider(...)` | 仅测试 |
| 14 | `internal/gateway/orchestrator/chat_test.go` | `for chunk := range ch` 未使用 `chunk` | 改为 `for range ch` | 仅测试 |
| 15 | `internal/gateway/provider/mock_provider_test.go` | `MockProvider` 仅测试包可见，跨包测试无法引用 | 重命名为 `mock_provider.go`（普通文件），使其对其他包可用 | 测试包之间可复用 |
| 16 | `internal/controlplane/handler/sync_handler_test.go` | `MockSyncService` 默认返回 `nil, nil`，handler 解引用 panic | 默认返回非 nil 零值结构体 | 仅测试 |
| 17 | `internal/gateway/handler/chat_stream_test.go` | `TestHandleStreamResponse_NilData` channel 缓冲 2 但写 3 条，发送阻塞导致 60s 超时；`TestHandleStreamResponse_ContextCancellation` 请求 nil 导致 panic；`TestHandleStreamResponse_MarshalError` 缓冲不足 | 调整缓冲；改用 `httptest.NewRequest` + `context.WithCancel`，cancel 后再读 channel | 仅测试 |

---

## 2. 失败用例修复清单（断言变更原因）

| 用例 | 旧断言 | 新断言 | 理由 |
|------|--------|--------|------|
| `internal/gateway/normalizer/TestNormalizeResponse_IDFormat` | `strings.Split` 后强校验 `[chatcmpl, XXXX]` 共 2 段 | `regexp.MustCompile(`^chatcmpl-[0-9a-f]{8}-[0-9a-f]{3}$`)` | 实现生成 `chatcmpl-<uuid 前 12 字符>`，含有原生 `-`，简单分段不可行；正则准确匹配真实格式 |
| `internal/gateway/middleware/TestErrorHandler_PanicRecovery/normal_handler...` | `if strings.Contains(tt.name, "panic")` 进入分支断言 `internal_error` 出现在 body | 改为 `if tt.expectStatusCode == http.StatusInternalServerError` | 用例名含 "panic" 但实际不应触发恢复分支；按响应码判定避免误触 |
| `internal/gateway/provider/TestRegisterModel` | 仅注册模型未注册 Provider，期望 `IsModelAvailable=true` | 先 `Register` provider 再 `RegisterModel`，并显式覆盖"无 provider 时 false" | `IsModelAvailable` 语义要求 provider 实例存在；新断言更贴近 `Resolve` 的真实约束 |
| `internal/controlplane/handler/TestSyncModels_Success` | 误以为 `data["added"][0]["id"]` 为 `float64(2)` | 实际为 string `"model-1"` | 字段类型回归至产物本意 |
| `internal/gateway/handler/TestHandle_InvalidBody/invalid_message_role` | `oneof=system user assistant` 校验通不过 | 给 `Messages` 增加 `dive` binding tag 让验证下钻到元素 | 这是生产 bug：消息数组未校验子项 role；新增 `dive` 与测试断言一致 |
| `internal/gateway/middleware/middleware.go::APIKeyAuth` | `"Bearer  "` 解析后 apiKey="" 但被识别为空字符串通过 | `strings.TrimSpace` 之后再判空 | 生产 bug：空白 token 不应放行 |

---

## 3. 新增测试清单（按 package）

| Package | 新增/重写文件 | 用例数 | 主要覆盖目标 |
|---------|---------------|--------|--------------|
| `internal/controlplane/pkg/crypto` | `aes_test.go` | 9 | `Encrypt/Decrypt` 对称、Unicode/Emoji、空串、错误 base64、密文过短、错误密钥、Mask 边界 |
| `internal/controlplane/event` | `event_test.go` | 4 | `NewChangeEvent` 字段填充、所有事件常量值、`Publisher.Publish` 经 miniredis 收发、关闭客户端返回错误 |
| `internal/controlplane/handler` | `definition_handler_test.go` / `internal_handler_test.go` / `model_handler_test.go` / `test_handler_test.go` | 4 + 2 + 13 + 12 | 全部 handler 分支：成功/无效 ID/未找到/冲突/内部错误/连通性/转发测试 |
| `internal/controlplane/repository` | `repo_test.go` | 4 个表驱动用例覆盖 4 个 repo | `Provider/Model/ModelTag/ProviderDefinition` 全部 CRUD + 唯一性/级联/Builtin 删除约束 |
| `internal/controlplane/service` | `model_service_test.go` / 扩展 `sync_service_test.go` / `provider_service_test.go` 微调 | 16 + 12 + 已有 | `ModelService`、`InternalService`、`SyncService` 全部分支；含 `httptest` 上游服务模拟 |
| `internal/gateway/handler` | `health_test.go` 等 | 3 + 既有补丁 | `Health` / `Ready` / Redis 不可达 |
| `internal/gateway/infra` | `cache_real_test.go` / `infra_test.go` | 8 + 9 | `Cache` 的全部分支（命中/miss/json 错误/Redis 错误/fail-open）；`InitRedis`、`InitTracer(disabled)`、`LoadFromControlPlane`、`Subscriber.Start`/`handleMessage` |
| `internal/gateway/provider` | `helper_test.go`（含 `MockProvider` 方法覆盖） | 12 | `BuildRequestURL` 各 URL 模式、`SetAuthHeader` 各种 auth/extra headers、`OpenAICompatChat`/`OpenAICompatStream` 成功/错误/空 choices/网络故障 |
| `tests/integration` | `setup_test.go` 补丁 | - | 在缺少 PG 时自动 `t.Skip`，保证 `go test ./...` 干净通过 |

---

## 4. 最终 `go test ./...` 输出摘要

```
ok  	github.com/example/llm-gateway/internal/controlplane/event        0.020s coverage: 87.5%
ok  	github.com/example/llm-gateway/internal/controlplane/handler      0.078s coverage: 94.1%
ok  	github.com/example/llm-gateway/internal/controlplane/middleware   0.030s coverage: 100.0%
ok  	github.com/example/llm-gateway/internal/controlplane/pkg/crypto   0.020s coverage: 88.6%
ok  	github.com/example/llm-gateway/internal/controlplane/repository   0.073s coverage: 94.1%
ok  	github.com/example/llm-gateway/internal/controlplane/service      0.047s coverage: 72.9%
ok  	github.com/example/llm-gateway/internal/gateway/config            0.016s coverage: 100.0%
ok  	github.com/example/llm-gateway/internal/gateway/errormap          0.015s coverage: 100.0%
ok  	github.com/example/llm-gateway/internal/gateway/handler           1.733s coverage: 90.1%
ok  	github.com/example/llm-gateway/internal/gateway/infra             5.284s coverage: 83.0%
ok  	github.com/example/llm-gateway/internal/gateway/middleware        0.034s coverage: 100.0%
ok  	github.com/example/llm-gateway/internal/gateway/normalizer        0.022s coverage: 100.0%
ok  	github.com/example/llm-gateway/internal/gateway/orchestrator      0.043s coverage: 92.5%
ok  	github.com/example/llm-gateway/internal/gateway/provider          0.025s coverage: 90.9%
ok  	github.com/example/llm-gateway/tests/contract                     0.046s
ok  	github.com/example/llm-gateway/tests/integration                  0.015s (Skip when DB unreachable)
```

总（含 0% 覆盖的 wiring/类型包）：**54.3%**；所需测试范围内的 14 个 package 全部 ≥ 60%（最低 controlplane/service = 72.9%），多数 ≥ 90%。

---

## 5. 风险与遗留项

1. **未触及的 0% 覆盖包**（不在需求范围内，但建议后续补齐）：
   - `cmd/control-plane`、`cmd/gateway`：纯 wiring，建议未来用 `go-cleanenv` + httptest 端到端 smoke。
   - `internal/controlplane/{infra,model,router}`、`internal/gateway/{model,server}`、`internal/gateway/provider/{anthropic,bailian,bedrock,deepseek,litellm}`：多为类型定义、router 编织或厂商 adapter；adapter 单测需要大量 fixture/录制 HTTP 响应，建议作为独立小任务推进。
2. **集成测试**：`tests/integration/*` 编译通过，但默认 `t.Skip` 直到设置 `RUN_INTEGRATION_TESTS=1` 且能连到 `TEST_DB_*` 指向的 PG。CI/本地无 Docker 时不再报错。
3. **生产代码改动**：
   - `internal/gateway/middleware/middleware.go::APIKeyAuth` 对 token 增加 `TrimSpace`（已修真实空白 token 通过 bug）。
   - `internal/gateway/model/chat.go::ChatCompletionRequest.Messages` binding 标签由 `required,min=1` 改为 `required,min=1,dive`，让消息子项校验生效（消除 invalid role 被放行的隐患）。
   - `internal/controlplane/handler/test_handler.go` 修复未定义变量 `testModel` → `provider.TestModel`。
4. **接口抽象决策**：所有新接口位于"被使用方"的同包内，仅暴露调用所需方法子集（避免 fat-interface）。生产路由仍传具体类型实例（实现接口），零行为变化。
5. **依赖新增**：
   - `github.com/alicebob/miniredis/v2`：用于 event/infra/handler 的 Redis pub/sub & cache 集成测试。
   - `github.com/glebarez/sqlite`（纯 Go SQLite）：用于 repository 层 in-memory 测试，无需 CGO。

---

## 6. 关键决策（接口重构清单）

| Production File | 新接口 | 影响测试 |
|-----------------|--------|----------|
| `internal/gateway/handler/models.go` | `ModelsCacheInterface`, `ModelsRegistryInterface` | `models_test.go` 重写 |
| `internal/gateway/handler/chat.go` | `ChatOrchestratorInterface` | `chat_test.go` 移除 `unsafe.Pointer` |
| `internal/controlplane/handler/sync_handler.go` | `SyncServiceInterface` | `sync_handler_test.go` |
| `internal/controlplane/handler/model_tag_handler.go` | `ModelTagRepoInterface` | `model_tag_handler_test.go` |
| `internal/controlplane/handler/definition_handler.go` | `DefinitionRepoInterface` | 新增 `definition_handler_test.go` |
| `internal/controlplane/handler/internal_handler.go` | `InternalServiceInterface` | 新增 `internal_handler_test.go` |
| `internal/controlplane/handler/model_handler.go` | `ModelServiceInterface` | 新增 `model_handler_test.go` |
| `internal/controlplane/handler/test_handler.go` | `TestProviderRepo`, `TestModelRepo` | 新增 `test_handler_test.go` |
| `internal/controlplane/handler/provider_handler.go` | `ProviderServiceInterface` | `provider_handler_test.go` 改造 |
| `internal/controlplane/service/sync_service.go` | `SyncProviderRepo`, `SyncModelRepo` | `sync_service_test.go` 扩展 |
| `internal/controlplane/service/provider_service.go` | `ProviderRepoInterface`, `ModelRepoInterface`（被 `model_service.go` / `internal_service.go` 复用） | 三个服务测试不再依赖具体 repo |

> 全部生产代码的 `NewXxx` 构造签名保持向后兼容（具体类型实现接口）。
