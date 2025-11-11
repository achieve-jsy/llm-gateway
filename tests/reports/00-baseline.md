# 测试基线快照（2026-05-22 10:20）

## 环境

- Go: `go1.26.1 darwin/arm64`
- Node: `v23.11.1`（v24.12.0 的 node 二进制被某 oh-my-openagent shim 递归覆盖，需要把 `/Users/jsy/.nvm/versions/node/v23.11.1/bin` 前置到 PATH）
- 已设置 `GOPROXY=https://goproxy.cn,https://goproxy.io,direct`

## 后端 Go 基线

`go test ./internal/... -count=1` 结果：

| Package | 状态 | 说明 |
| --- | --- | --- |
| `internal/gateway/config` | PASS | |
| `internal/gateway/errormap` | PASS | |
| `internal/gateway/normalizer` | FAIL (1 case) | `TestNormalizeResponse_IDFormat`：断言 `chatcmpl-XXXX`，实际 `chatcmpl-313df7fd-200`（assertion 过严） |
| `internal/gateway/handler` | BUILD FAIL | `models_test.go` `*MockRegistry` 无法强转 `*provider.Registry`（Registry 是具体结构体而非 interface）；`chat_test.go` import 未使用 |
| `internal/gateway/infra` | BUILD FAIL | `cache_test.go:75` 变量 `cache` 声明未使用 |
| `internal/gateway/middleware` | BUILD FAIL | 编译失败（待详查） |
| `internal/gateway/orchestrator` | BUILD FAIL | 编译失败（待详查） |
| `internal/gateway/provider` | BUILD FAIL | 编译失败（待详查） |
| `internal/controlplane/handler` | BUILD FAIL | `sync_handler_test.go`：`*MockSyncService` 无法传 `*service.SyncService`；`model_tag_handler_test.go:111` 未用 mockRepo |
| `internal/controlplane/service` | BUILD FAIL | `sync_service_test.go`：`*MockProviderRepo/*MockModelRepo` 无法传 `*repository.ProviderRepo/*ModelRepo` |
| `internal/controlplane/middleware` | BUILD FAIL | 编译失败 |
| `internal/controlplane/{model,repository,router,infra,pkg/crypto}` | NO TESTS | 完全无测试 |

**根因**：handler/service 的构造函数接收"具体类型指针"，测试用 `*MockXxx`，但 Go 不支持隐式接口转换。需要将构造函数改为接受 interface（或定义接口 + 让 mock 实现）。

## 前端 Vue 基线

`npm run test:run` 结果：

| 测试文件 | 通过/总数 | 备注 |
| --- | --- | --- |
| `src/types/api.test.ts` | 28/28 | |
| `src/composables/useTheme.test.ts` | 22/22 | |
| `src/services/gateway-api.test.ts` | 8/8 | |
| `src/services/control-plane.test.ts` | 27/27 | |
| `src/utils/mask.test.ts` | 9/11 | `should mask string with length 9/10` 失败 |
| `src/services/http.test.ts` | 5/12 | 7 个用例失败（axios instance / 拦截器 / 错误响应） |
| 组件测试 `src/components/**` | 0/0 | **完全缺失** |
| 视图测试 `src/views/**` | 0/0 | **完全缺失** |

总计 **108 cases / 9 failing**；**字段交互层（表单/弹窗）零覆盖**。

## 前后端联调基线

- Go `tests/integration/*` 现存 5 个文件（provider/model/model_tag/sync/internal + setup），但因 controlplane 包编译失败，整体跑不起来
- 前端 → 后端契约**没有**任何自动化断言；变量名/字段是否一致全靠人工对照
- 未发现任何 e2e / msw / supertest 工程

## 缺口总览

1. 后端构建期错误必须先修（mocks vs 具体类型）
2. 后端 ID 格式断言修正
3. 前端 9 个失败用例修正
4. 前端 0 个组件/视图测试 → 全量补 (Provider/Model/Tag/Sync Dialog + 4 个 ListPage + ModelCard/CapabilitySection/ModelDiscoverySection)
5. FE↔BE 契约自动化（控件字段⇄ Go handler 字段一致性、URL/method、错误码）
6. Go 集成测试要随后端构建一起恢复
