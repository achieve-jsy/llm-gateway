# LLM Gateway · 全量 TDD 测试报告（前端 + 后端 + 联调）

| 项 | 值 |
| :-- | :-- |
| 报告生成时间 | 2026-05-22 11:00 (UTC+8) |
| **P0 修复落地** | 2026-05-22 11:25 (UTC+8) |
| **P1+P3+P4+P5 修复落地** | **2026-05-22 12:05 (UTC+8)** ✅ |
| 组织方式 | 多轮并行 Agent Teams（B-Core / B-Coverage / F / I）+ R 综合 |
| 仓库 | `/Users/jsy/Public/self_code/llm-gateway` |
| 后端栈 | Go 1.26.1（gin · gorm · miniredis · 纯 Go SQLite · testify） |
| 前端栈 | Vue 3.4 · Vite 5 · TypeScript 5.3 · Vitest 1.6 · happy-dom · @vue/test-utils 2 |
| 工作流 | TDD: 基线采集 → 红 → 绿 → 重构 → 补缺 → 复跑 → **P0/P1/P3/P4/P5 修复 + 防回归** |
| 三方互不干涉范围 | B 只动 `internal/`；F 只动 `web/src/`；I 只动 `tests/contract/` 与 `web/src/__contract__/` |
| 当前剩余 | **仅 P2 鉴权治理**（用户显式排除） |

### 🛠 P1 + P3 + P4 + P5 修复落地清单（2026-05-22 12:05 完成）

| ID | 类别 | 修复内容 | 关键文件 | TDD 守护用例 |
| :-- | :-- | :-- | :-- | :-- |
| **S1-1** | FE 类型 | `ApiResponse<T>.code/message` 改为可选字段，`PaginatedResponse<T>` 同步 | `web/src/types/api.ts` + `api.test.ts` | 类型 + 运行时双断言 |
| **S1-2** | FE/BE 分页 | `getProviders/getModels` 增加 `page/page_size` 可选入参，透传到 BE | `web/src/services/control-plane.ts` + `control-plane.test.ts` | 断言 mockHttp 收到 `params:{page,page_size}` |
| **S1-3** | FE 类型 | `TestProviderResult` 补齐 `chat_success/chat_response_time/chat_model/chat_error` 4 字段 | `web/src/services/control-plane.ts` | mock 完整响应后断言全字段透传 |
| **S1-4** | BE 校验 | `SetModelTags`/`BatchSetModelTags` 遇非 UUID 立即 400 + `code:invalid_tag_id`/`invalid_model_id`，不再静默 `continue` | `internal/controlplane/handler/model_tag_handler.go` | `model_tag_handler_test.go` 新增 8 个断言 |
| **S1-5** | BE/FE 错误码 | 引入 13 个机器可读错误码（`provider_name_conflict`/`model_name_conflict`/`provider_has_models`/...），FE 在 `http.ts::ERROR_MESSAGES` 维护文案；新建 `errcodes.go` 单一真实源 | `internal/controlplane/handler/errcodes.go` + `provider_handler.go` + `model_handler.go` + `sync_handler.go` + `model_tag_handler.go` + `definition_handler.go` + `internal_handler.go` + `test_handler.go` + `web/src/services/http.ts` | 各 handler test 显式断言新 code；契约元测试 `TestContracts_MachineCodes_AreCanonical` 锁定 |
| **S1-6** | FE 状态流 | `toggleProviderStatus` 由 `Promise<void>` 改为 `Promise<Provider>`，调用方使用返回对象更新本地状态 | `web/src/services/control-plane.ts` + `views/admin/ProviderListPage.vue` | 契约元测试 `TestContracts_TogglePropagatesProvider` 锁定 |
| **S3-1** | UX | TagDialog 关闭 `show-alpha`（统一输出 `#RRGGBB`）；BE `color` 长度上限同时由 `max=7` 提升至 `max=30` 兼容历史数据 | `web/src/components/admin/TagDialog.vue` + `internal/controlplane/handler/model_tag_handler.go` | TagDialog spec 校验 prop；handler test 校验 rgba 长度可写入 |
| **S3-2** | UX/错误码 | SyncDialog 不支持的同步上游：BE 改为 `type:invalid_request, code:sync_not_supported`；FE 通过 ERROR_MESSAGES 显示「该渠道类型不支持模型自动同步」 | `internal/controlplane/handler/sync_handler.go` + `web/src/services/http.ts` | `sync_handler_test.go` + http.test.ts |
| **P4** | 测试盲区 | 0% 覆盖率包补单测：`controlplane/infra` (10 用例 / 77.4%) · `gateway/server` (11 用例 / 100%) · `gateway/provider/{anthropic,bailian,bedrock,deepseek,litellm}` (84 用例 / 44.5–93.5%) | 7 个新建 `_test.go` 文件 | `go test ./...` 全绿 |
| **P5-1** | 死字段清理 | 彻底删除 `ConfirmSyncInput.DisableRemoved`（历史 no-op）+ FE `confirmSync` 调用同步剔除 | `internal/controlplane/service/sync_service.go` + `tests/contract/contracts.json` | `TestContracts_MachineCodes_AreCanonical` 顺带断言 `disable_removed` 不在契约里 |
| **P5-2** | 字段对齐 | `unsupported` 字段在 200 响应里始终包含（默认 false），FE 类型不再悬挂 | `internal/controlplane/handler/sync_handler.go` | sync_handler_test 直接断言 |
| **跨团队 bug 1** | 生产 bug | `deepseek/adapter.go::Chat/ChatStream` 与 `litellm/adapter.go::Chat/ChatStream` 把 `/chat/completions` 路径预拼后再传给 `OpenAICompatChat`，被内部 `BuildRequestURL` 二次追加，产生 `…/chat/completions/chat/completions` 死链 | `internal/gateway/provider/{deepseek,litellm}/adapter.go` | 各 adapter 新建 `_test.go` 通过 httptest 锁定真实 URL |
| **跨团队 bug 2** | 测试基础设施 | crypto KeyManager 升级为 KEK/DEK + DB 持久化后，依赖旧版 `os.Setenv("ENCRYPTION_KEY")` 的 service/handler TestMain 全数 panic：补 `crypto.InitForTesting()` DB-less 测试桩 | `internal/controlplane/pkg/crypto/manager.go` + 各 TestMain | 各包恢复绿色 |
| **跨团队 bug 3** | 测试隔离 | `setupTestDB` 误用 `file::memory:?cache=shared`，crypto 包内多个 `TestRotate*` 之间互相串数据；改 `:memory:` 实现 per-test 隔离 | `internal/controlplane/pkg/crypto/manager_test.go` | crypto 包整体绿 |

修复后复跑结果：
- Go：**23 个包 100% PASS**（含新增 105 个 0% 覆盖率包用例 + 13 项错误码守护用例 + 4 项契约元测试）
- Vitest：**362 用例 100% PASS**（自 P0 阶段 325 → 360 → 362，新增 37 项断言）
- 契约：13 个机器码、`disable_removed` 死字段、分页参数、toggle 返回 Provider 全部由元测试锁死

### 🛠 P0 修复落地清单（2026-05-22 11:25 完成）

| ID | 修复内容 | 文件 | 验证用例 |
| :-- | :-- | :-- | :-- |
| **S0-1** | `CreateProviderInput`/`UpdateProviderInput` 补齐 `weight/priority/test_model/remark/max_retries/timeout/tag` 共 7 字段，service 透传到 `model.Provider` | `internal/controlplane/service/provider_service.go` | `TestCreateProvider_AdvancedFields*` ×3 + `TestUpdateProvider_AdvancedFields*` ×3 + `TestCreateProvider_AdvancedFieldsRoundTrip` |
| **S0-2** | `CreateModelInput`/`UpdateModelInput` 补齐 `upstream_model_id/description/context_length/max_output_tokens/input_price/output_price/weight` 共 7 字段 | `internal/controlplane/service/model_service.go` | `TestCreateModel_AdvancedFields*` + `TestUpdateModel_AdvancedFields*` + `TestModelHandler_*AdvancedFieldsForwardedToService` + `TestCreateModel_AdvancedFieldsRoundTrip` |
| **S0-3** | 新增独立路由 `GET /api/v1/models/check-name?name=…`，返回 `{data:{available:bool}}`；FE `checkModelNameUnique` 切换到新端点 | `internal/controlplane/handler/model_handler.go` + `router/router.go` + `web/src/services/control-plane.ts` | `TestModelHandler_CheckModelName_*` ×4 + `TestModelHandler_CheckNameDoesNotCollideWithGetByID` + `TestCheckModelName_DoesNotCollideWithIDPath` |
| **契约同步** | `contracts.json` 清空 4 条路由的 `request_fields_ignored_by_backend`、新增 `check-name` 路由、`frontend_clients.checkModelNameUnique` 切换 URL/参数；新增 `TestContracts_NoIgnoredFieldsForFixedRoutes` 元测试防回退 | `tests/contract/contracts.json` + `tests/contract/http_smoke_test.go` | 同上 |

设计要点：
- **指针类型语义**：所有可选数值/可清空字符串字段使用 `*int/*float64/*string`，**区分"未传"与"显式传零/空值"**（如 `Weight=nil` 走 DB 默认 1，`Weight=&0` 则真的写 0）
- **路由顺序契约**：`GET /models/check-name` 必须在 `PUT /models/:id` 与 `DELETE /models/:id` 之前注册，避免被通配吞掉；专门加 `TestModelHandler_CheckNameDoesNotCollideWithGetByID` 守护
- **乐观查重**：`CheckModelName` 仅做实时提示，唯一性最终由 `CreateModel` 事务校验兜底
- **元测试守护**：`tests/contract/http_smoke_test.go::TestContracts_NoIgnoredFieldsForFixedRoutes` 强制 4 条路由的 `request_fields_ignored_by_backend` 必须为空数组——下次有人误把字段加回忽略列表会立刻挂

修复后复跑结果：
- Go：**16 个包 100% PASS**（含 `service` 新增 8 用例 / `handler` 新增 9 用例 / `tests/contract` 新增 4 用例 + 1 个元测试）
- Vitest：**325 用例 100% PASS**（`control-plane.test.ts` 新断言 + `__contract__/control-plane.contract.spec.ts` 切到新 URL）

---

## 0. 一句话结论

**除用户显式排除的 P2 鉴权治理之外，所有自动化测试 PASS**：Go **23 个业务包** 100% 通过 / Vue **362 用例** 100% 通过 / 契约 **8 个元测试 + 56 subtest** 100% 通过。**S0 / S1 / S3 / P4 / P5 共 20 项问题全部修复并由 TDD 守护**：

- ✅ ProviderDialog 7 高级字段端到端透传 **OK**（S0-1）
- ✅ ModelDialog 7 高级字段端到端透传 **OK**（S0-2）
- ✅ `checkModelNameUnique` 走专用 `check-name` 路由 **OK**（S0-3）
- ✅ ApiResponse `code/message` 改可选 + 分页支持 + TestProviderResult 全字段 + Toggle 返回 Provider **OK**（S1-1/2/3/6）
- ✅ `SetModelTags` 非 UUID 立即 400 + `invalid_tag_id` 机器码 **OK**（S1-4）
- ✅ 13 个机器可读错误码体系，FE `ERROR_MESSAGES` 统一文案 **OK**（S1-5）
- ✅ TagDialog 关闭 alpha + BE color max=30；SyncDialog 友好文案 **OK**（S3-1/2）
- ✅ 0% 覆盖率包补 105 个单测：infra · server · 5 个 Provider Adapter **OK**（P4）
- ✅ `disable_removed` 死字段彻底删除 + `unsupported` 始终输出 **OK**（P5-1/2）

> 双层防回归：契约元测试（`TestContracts_NoIgnoredFieldsForFixedRoutes` / `TestContracts_MachineCodes_AreCanonical` / `TestContracts_PaginationQuery_PresentForListEndpoints` / `TestContracts_TogglePropagatesProvider`）+ Go handler test 显式 `assert.Equal` 锁定每条 code 字符串。

| 类别 | 用例总数 | 通过 | 覆盖率（关键） |
| :-- | --: | --: | :-- |
| 后端 Go 单测（**23 业务包**）| 数百 | **100%** | 控平 handler 94.1% / service 72.9% / 中间件 100% / repo 94.1%；网关 handler 90.1% / orchestrator 92.5% / normalizer 100% / middleware 100%；新增 `infra` 77.4% / `server` 100% / `provider/litellm` 93.5% / `provider/anthropic` 70.9% / `provider/deepseek` 64.5% / `provider/bailian` 62.1% / `provider/bedrock` 44.5% |
| 后端 Go 集成（PG/Redis）| —— | 自动 Skip 当 PG 不可达 | 编译 PASS |
| 前端 Vue（19 spec）| **362** | **100%** | components 97.97% / views 96.92% / services 98.16% / utils 100% |
| FE↔BE 契约（vitest + go）| **8 元测试 + 56 subtest** | **100%** | 13 机器码守护 + 分页/Toggle/dead-field 守护 |

| 严重度 | 链路问题数 | 修复状态 | 备注 |
| :-- | --: | :-- | :-- |
| S0 链路功能性破坏 | ~~7~~ → **0** | ✅ **已修复**（11:25） | ProviderDialog ×7 + ModelDialog ×7 字段全部透传；check-name 改走独立路由 |
| S1 功能可见但不影响主流 | ~~6~~ → **0** | ✅ **已修复**（12:05） | ApiResponse 类型 / 分页 / TestProviderResult / SetModelTags 严校验 / 13 个机器码 / Toggle 返回 Provider |
| S2 鉴权/会话治理 | 2 | ⏸️ **用户显式排除本轮** | 401 无登录跳转、InternalAuth 不校验 token |
| S3 UX 小问题 | ~~2~~ → **0** | ✅ **已修复**（12:05） | TagDialog show-alpha 关闭 + BE color max 提到 30；SyncDialog 走 sync_not_supported 友好文案 |
| P4 测试盲区 | ~~7~~ → **0** | ✅ **已修复**（12:05） | infra/server/anthropic/bailian/bedrock/deepseek/litellm 共补 105 用例 |
| P5 残留契约 | ~~3~~ → **0** | ✅ **已修复**（12:05） | disable_removed 删除；unsupported 始终输出；api_version 已由 vendor_options 承载（契约文档化） |

---

## 1. Agent Teams 组织

```
                  ┌──────────────────────┐
                  │  Team R (Synthesis)  │  ← 本报告
                  └─────────┬────────────┘
                            ▲
         ┌──────────────────┼──────────────────┐
         │                  │                  │
┌────────┴────────┐  ┌──────┴───────┐  ┌──────┴────────┐
│ Team B  Backend │  │ Team F  Vue  │  │ Team I  Link  │
│  Go TDD         │  │ Components   │  │ Contract+E2E  │
└─────────────────┘  └──────────────┘  └───────────────┘
```

- **Team B（后端 Go）**：修编译 → 修失败用例 → 17 项 interface 重构 → 补 9 类 _test.go → 真实 miniredis + SQLite 跑通 cache/repo
- **Team F（前端 Vue）**：修 9 个失败用例 + 1 个代码漂移 → 新建 12 个 spec（8 组件 + 4 视图）→ 自研 Element-Plus stub
- **Team I（联调契约）**：抽取路由表与 FE call 表 → 生成 `contracts.json` → vitest 契约 + go 冒烟双向校验 → 字段一致性矩阵
- **Team R（汇总）**：本报告

三支团队**完全独立、并行执行**，靠"互不重叠的修改面"避免冲突；最终合并复跑零冲突。

---

## 2. 后端 TDD 成果（Team B）

### 2.1 修复矩阵

- **17 项编译/接口问题** 全部解决（详见 `tests/reports/backend-test-report.md` §1 表），其中 11 项是测试侧、6 项是为了让测试能注入 mock 而引入"消费侧接口"（生产代码零行为变化）。
- **6 项失败用例** 全部修正：
  - `TestNormalizeResponse_IDFormat` —— 用正则匹配 `^chatcmpl-[0-9a-f]{8}-[0-9a-f]{3}$`
  - `TestErrorHandler_PanicRecovery` —— 按响应码判定而非用例名字符串
  - `TestRegisterModel` —— 先 Register provider 再 RegisterModel 以满足 `IsModelAvailable` 真实约束
  - `TestSyncModels_Success` —— 类型断言由 `float64(2)` 改为 `"model-1"`
  - `TestHandle_InvalidBody/invalid_message_role` —— 同时给 `Messages` binding 加 `,dive`
  - `APIKeyAuth` —— `strings.TrimSpace` 后再判空

### 2.2 关键 interface 重构（生产代码侧仅增加抽象，构造函数签名向后兼容）

| Production File | 新接口 |
| :-- | :-- |
| `internal/gateway/handler/models.go` | `ModelsCacheInterface`, `ModelsRegistryInterface` |
| `internal/gateway/handler/chat.go` | `ChatOrchestratorInterface` |
| `internal/controlplane/handler/sync_handler.go` | `SyncServiceInterface` |
| `internal/controlplane/handler/model_handler.go` | `ModelServiceInterface` |
| `internal/controlplane/handler/internal_handler.go` | `InternalServiceInterface` |
| `internal/controlplane/handler/definition_handler.go` | `DefinitionRepoInterface` |
| `internal/controlplane/handler/model_tag_handler.go` | `ModelTagRepoInterface` |
| `internal/controlplane/handler/provider_handler.go` | `ProviderServiceInterface` |
| `internal/controlplane/handler/test_handler.go` | `TestProviderRepo`, `TestModelRepo` |
| `internal/controlplane/service/provider_service.go` | `ProviderRepoInterface`, `ModelRepoInterface`（被 `model_service.go`、`internal_service.go` 复用） |
| `internal/controlplane/service/sync_service.go` | `SyncProviderRepo`, `SyncModelRepo` |

### 2.3 顺手修复的 3 个真实生产 Bug（不只是测试问题）

1. `internal/gateway/middleware/middleware.go::APIKeyAuth` —— 空白 token（如 `"Bearer  "`）被放行 → 加 `TrimSpace`
2. `internal/gateway/model/chat.go::ChatCompletionRequest.Messages` —— 子项 role 未校验 → binding 改为 `required,min=1,dive`
3. `internal/controlplane/handler/test_handler.go` —— `testModel := testModel` 引用未定义变量 → 改为 `provider.TestModel`

### 2.4 覆盖率（仅展示 ≥60% 的目标包）

| Package | Stmts |
| :-- | --: |
| `internal/controlplane/event` | 87.5% |
| `internal/controlplane/handler` | **94.1%** |
| `internal/controlplane/middleware` | **100%** |
| `internal/controlplane/pkg/crypto` | 88.6% |
| `internal/controlplane/repository` | **94.1%** |
| `internal/controlplane/service` | 72.9% |
| `internal/gateway/config` | **100%** |
| `internal/gateway/errormap` | **100%** |
| `internal/gateway/handler` | 90.1% |
| `internal/gateway/infra` | 83.0% |
| `internal/gateway/middleware` | **100%** |
| `internal/gateway/normalizer` | **100%** |
| `internal/gateway/orchestrator` | 92.5% |
| `internal/gateway/provider` | 90.9% |

详情见 `tests/reports/backend-test-report.md` 与 `backend-coverage.txt`。

---

## 3. 前端 TDD 成果（Team F）

### 3.1 失败修复

| 失败源 | 数量 | 解法 |
| :-- | :-: | :-- |
| `src/utils/mask.test.ts` | 2 | 改实现：长度 9-11 保留前 3+后 3；长度 ≥12 保留前 3+后 4 |
| `src/services/http.test.ts` | 7 | (i) `vi.mocked(axios.create)` 默认值 → 提供合法的 `interceptors` 桩；(ii) 异步 reject 用 `await expect(...).rejects.toBe(...)` |
| `src/services/control-plane.test.ts` | 1（代码漂移） | `testProvider(id)` 已升级为 `testProvider(id, testChat?)`，断言追加 `undefined` 入参 |

### 3.2 新增 8 个组件 spec —— **52 项纯字段交互覆盖 + 25 项通用交互覆盖**

| 组件 | spec 文件 | 用例数 | 字段交互项（按表单字段计） |
| :-- | :-- | --: | :-- |
| `ProviderDialog.vue` | `ProviderDialog.spec.ts` | **28** | name / definition_id / endpoint / credentials_map[k]（动态字段+密码态）/ priority / weight / timeout / max_retries / region（Bedrock）/ api_version（Azure）/ test_model / tag / remark — 共 14 项 |
| `ModelDialog.vue` | `ModelDialog.spec.ts` | **26** | name / upstream_model_id / provider_id / model_type / tags / description / context_length / max_output_tokens / input_price / output_price / weight + 300ms 防抖唯一性检查 — 共 13 项 |
| `TagDialog.vue` | `TagDialog.spec.ts` | **15** | name(1-32) / color（实时驱动 `.color-preview`）/ description / sort_order — 6 项 |
| `SyncModelsDialog.vue` | `SyncModelsDialog.spec.ts` | **17** | 搜索过滤 / 全选反选 / disabled / 三段统计 / confirmSync payload — 7 项 |
| `BatchTagDialog.vue` | `BatchTagDialog.spec.ts` | **8** | modelIds 计数 / multi-select / 空选警告 — 4 项 |
| `ModelCard.vue` | `ModelCard.spec.ts` | **6** | id / owned_by / 徽章 / hover — 5 项 |
| `ModelDiscoverySection.vue` | `ModelDiscoverySection.spec.ts` | **7** | loading / 空 / 错误 / refresh / retry — 5 项 |
| `CapabilitySection.vue` | `CapabilitySection.spec.ts` | **6** | excludes / 描述 / 段落 — 3 项 |

### 3.3 新增 4 个视图 spec —— **49 个用例**

| 视图 | 用例数 | 覆盖 |
| :-- | --: | :-- |
| `ProviderListPage.vue` | 19 | 初始化 / 列表 / tag 筛选 / 新建编辑对话框 / 启停 / 测试连通性（成功+失败）/ 同步（成功 / `sync_not_supported` / 异常）/ 删除（成功 / `409 has_models` / 异常）/ 行内编辑 priority+weight |
| `ModelListPage.vue` | 13 | 三路并行初始化 / providerId+tag 筛选 / 新建编辑对话框 / `el-switch` 切换 / 删除流程 |
| `TagListPage.vue` | 11 | 卡片网格 / 颜色色块 / 空状态 / CRUD |
| `UnifiedEntryPage.vue` | 6 | hero 区域 / 统计 / `<CapabilitySection>` / `<ModelDiscoverySection>` 联动 |

### 3.4 覆盖率（v8）

| 目录 | Stmts | Branch | Funcs | Lines |
| :-- | --: | --: | --: | --: |
| `src/components/access` | **100%** | 100% | 100% | 100% |
| `src/components/admin` | **97.14%** | 88.74% | 81.15% | 97.14% |
| `src/views/access` | **100%** | 100% | 100% | 100% |
| `src/views/admin` | **96.44%** | 87.09% | 76.92% | 96.44% |
| `src/services` | 98.16% | 97.22% | 95.83% | 98.16% |
| `src/utils` | **100%** | 100% | 100% | 100% |
| **全工程** | **93.38%** | 87.81% | 79.81% | 93.38% |

详情见 `tests/reports/frontend-test-report.md`。

### 3.5 关键工程决策

1. **Element Plus 全量自定义 stub**（`src/test-utils/element-stubs.ts`）；用 `setup() + h()` 而非 `template: '...'`，规避 vitest 无运行时模板编译器
2. **mock service 使用 `mockImplementation(async () => clone(sample))`** 而非 `mockResolvedValue(sample)`：避免组件原地写入数据污染下一个用例（这是 ProviderListPage toggle 失败用例的真实根因）
3. **异步 reject 一律 `await expect(...).rejects.toBe(...)`** 消除 unhandledRejection 噪音
4. **仅触碰 1 个产品文件** (`web/src/utils/mask.ts`)，其余漂移问题全部通过测试侧解决

---

## 4. 链路联调（Team I）

### 4.1 契约自动化

- `tests/contract/contracts.json` —— 手工对齐的契约表：control-plane 23 路由 + gateway 4 路由 + FE call 22 条 + envelope 规约
- `web/src/__contract__/control-plane.contract.spec.ts` —— **55 个用例 PASS**
  - A 类（22 用例）：FE call ↔ BE route 静态对齐
  - B 类（6 用例）：每条 POST/PUT 的 FE 提交字段 ⊆ BE `required ∪ optional ∪ ignored`；ignored 集合显式标注，**为字段层 S0 提供机器化跟踪**
  - C 类（22 用例）：mock http 抓取 FE 真实请求并断言 method/url/body
  - D 类（1 用例）：gateway-api.ts 客户端
  - E 类（3 用例）：FE 响应解构与 BE `{data, total, page}` 形态兼容
  - 静态契约自洽循环展开 ~22 个隐式用例
- `tests/contract/http_smoke_test.go` —— **7 函数 / 56 subtest 全 PASS**
  - 包含 23 条 control-plane 路由 + 4 条 gateway 路由的最小合法响应
  - JWTAuth/InternalAuth/APIKeyAuth 中间件 401/403 双断言
  - `TestErrorEnvelope_Shape_TypeCodeMessage` 错误响应形态回归
  - `TestContractsTable_AlignsWithRouter` 路由漂移回归网

### 4.2 字段一致性矩阵（重点）

> 仅列 **❌** 与 **⚠️** 项；✅ 项详见 `tests/reports/integration-test-report.md` §2。

#### ProviderDialog → `POST/PUT /api/v1/providers` —— **7 个 ❌**

| FE 字段 | BE Input | DB 列 | 现状 |
| :-- | :-- | :-- | :-- |
| `weight` | **未声明** | `weight INT DEFAULT 1` | FE 提交后**静默丢弃** |
| `priority` | **未声明** | `priority INT DEFAULT 0` | 同上 |
| `test_model` | **未声明** | `test_model VARCHAR(200)` | 同上 |
| `remark` | **未声明** | `remark VARCHAR(500)` | 同上 |
| `max_retries` | **未声明** | `max_retries INT DEFAULT 3` | 同上 |
| `timeout` | **未声明** | `timeout INT DEFAULT 30` | 同上 |
| `tag` | **未声明** | `tag VARCHAR(100)` | 同上 |
| `api_version` | FE 未发送 | provider_definitions.vendor_options | Azure 永远是默认 |

#### ModelDialog → `POST/PUT /api/v1/models` —— **7 个 ❌**

| FE 字段 | BE Input | DB 列 | 现状 |
| :-- | :-- | :-- | :-- |
| `upstream_model_id` | **未声明** | `upstream_model_id VARCHAR(200)` | FE 提交后**静默丢弃**；网关无法做"上游别名映射" |
| `description` | **未声明** | `description VARCHAR(500)` | 同上 |
| `context_length` | **未声明** | `context_length INT` | 同上 |
| `max_output_tokens` | **未声明** | `max_output_tokens INT` | 同上 |
| `input_price` | **未声明** | `input_price DECIMAL(10,6)` | 同上 |
| `output_price` | **未声明** | `output_price DECIMAL(10,6)` | 同上 |
| `weight` | **未声明** | `weight INT DEFAULT 1` | 同上 |

#### TagDialog → `POST/PUT /api/v1/model-tags`

| 字段 | 状态 |
| :-- | :-- |
| `color` | ⚠️ FE `el-color-picker show-alpha` 可能返回 `rgba(...)` > 7 字符，BE `max=7` 会 422 |

#### SyncModelsDialog → `POST /api/v1/providers/:id/sync-models{,/confirm}`

| 字段 | 状态 |
| :-- | :-- |
| `disable_removed` | ⚠️ FE 从未传入；BE 的 `disable_removed` 分支注释承认未实现，No-op |
| `unsupported` | ⚠️ BE 在 `unsupported=true` 时直接返回 400，永远不会把 `{"unsupported": true}` 嵌在 `data` 里 → FE 类型字段死代码 |

详细矩阵见 `tests/reports/integration-test-report.md` §2。

---

## 5. 关键问题清单（按严重度 + 解决方案）

### S0 链路功能性破坏（必须立刻治理）

#### S0-1 ProviderDialog 高级设置字段静默丢失

- **现象**：用户在 ProviderDialog "高级设置" Tab 内填写的 `weight / priority / test_model / remark / max_retries / timeout / tag` 共 7 个字段，全部被 Gin binding 静默丢弃，DB 保留默认值。
- **影响**：渠道权重、超时、重试、备注、连通性测试模型等运营关键参数**永远无法配置**。
- **解决方案**（无需 FE 改动）：
  ```go
  // internal/controlplane/service/provider_service.go
  type CreateProviderInput struct {
      Name         string `json:"name" binding:"required,max=100"`
      DefinitionID string `json:"definition_id" binding:"required,max=50"`
      Type         string `json:"type" binding:"omitempty,max=50"`
      Endpoint     string `json:"endpoint" binding:"required,max=500"`
      Credentials  string `json:"credentials" binding:"required"`
      Weight       *int   `json:"weight" binding:"omitempty,min=0,max=9999"`
      Priority     *int   `json:"priority" binding:"omitempty,min=0,max=9999"`
      TestModel    string `json:"test_model" binding:"omitempty,max=200"`
      Remark       string `json:"remark" binding:"omitempty,max=500"`
      MaxRetries   *int   `json:"max_retries" binding:"omitempty,min=0,max=10"`
      Timeout      *int   `json:"timeout" binding:"omitempty,min=1,max=300"`
      Tag          string `json:"tag" binding:"omitempty,max=100"`
  }
  ```
  - 指针类型用于区分 "未传" 与 "传零值"
  - service 层把字段透传到 `model.Provider`
  - 修复后把 `tests/contract/contracts.json` 中对应路由的 `request_fields_ignored_by_backend` 清空 → 契约 B 类用例自动收紧 → 再有人引入新的"幽灵字段" CI 立刻红

#### S0-2 ModelDialog 参数配置字段静默丢失

- **现象**：`upstream_model_id / description / context_length / max_output_tokens / input_price / output_price / weight` 共 7 个字段被丢弃。
- **影响**：模型成本核算、上下文窗口约束、上游别名映射**完全失效**。
- **解决方案**：同 S0-1，补全 `CreateModelInput` / `UpdateModelInput`。

#### S0-3 `checkModelNameUnique` 走错路由 + 死断言

- **现象**：FE `control-plane.ts:85` 用 `GET /api/v1/models?check_name=X` 期望 `{data:{available:boolean}}`；BE `ModelHandler.ListModels` **不识别** `check_name` query，返回的是 `{data:Model[],total,page}`。`response.data.data.available` → `undefined` → `available` 永远 `false` → `nameConflict = !false = true` → ModelDialog 永远误报"名字已存在"，最终用户点提交后才被 BE 拒。
- **解决方案**（推荐 B）：
  - 方案 A：BE `ListModels` 加分支 `if name := c.Query("check_name"); name != "" { ... return {"data":{"available": !exists}} }`。
  - 方案 B：BE 新增独立路由 `GET /api/v1/models/check-name?name=…` 返回 `{data:{available}}`；FE 同步切换。

### S1 功能可见但不阻断主流（建议本迭代修）

| ID | 问题 | 解决方案 |
| :-- | :-- | :-- |
| S1-1 | 响应包装类型不诚实：`ApiResponse<T>.code/message` 标必填，BE 从不返回 | 短期：FE 类型改 `code?: number; message?: string`；长期：BE 统一中间件注入 `{code,message,data}` |
| S1-2 | `getProviders/getModels` 不分页，数据量大于 20 直接被 BE 截断 | FE 加 page/page_size 参数或将 BE 默认 page_size 调大 |
| S1-3 | `testProvider` 响应字段缩水：BE 返回 `{success,response_time,status_code,model_count,error}`，FE 类型只有 `{response_time}` | FE 扩展响应类型 |
| S1-4 | `PUT /api/v1/models/:id/tags` 中 BE `uuid.Parse` 失败 silently continue | BE 解析失败返回 400 |
| S1-5 | 错误码全部 `bad_request/internal_error`，无机器细分 | `errormap.Mapper` 引入 machine code (`E_PROVIDER_DUPLICATE_NAME / E_PROVIDER_HAS_MODELS / E_MODEL_DUPLICATE_NAME / E_SYNC_NOT_SUPPORTED / E_UPSTREAM`)，FE `http.ts` 按 code 路由差异化提示 |
| S1-6 | `ToggleProviderStatus` BE 返回完整 ProviderResponse，FE 丢弃 | FE 改返回类型 `Promise<Provider>` 复用 |

### S2 鉴权/会话治理

| ID | 问题 | 解决方案 |
| :-- | :-- | :-- |
| S2-1 | 401 仅 toast，无登录重定向 | FE `http.ts` 401 时清 token 并 `router.push('/login')` 或触发 SSO 重新挑战 |
| S2-2 | `InternalAuth` 只判 `X-Internal-Token` 非空，任意字符串放行 | 引入 `INTERNAL_TOKEN` 环境变量做强等值校验 |

### S3 UX

| ID | 问题 | 解决方案 |
| :-- | :-- | :-- |
| S3-1 | `TagDialog` `el-color-picker show-alpha` 输出 RGBA > 7 字符，BE max=7 | 关闭 `show-alpha` 或 BE 扩展 max=20 接受 rgba |
| S3-2 | `SyncModelsDialog` 上游不支持同步时 BE 直接 400，前端 toast `注册失败`，用户无法理解 | BE 用 machine code `E_SYNC_NOT_SUPPORTED`，FE 显示"上游模型 SDK 不支持目录同步"语义化文案 |

### 前端原代码漂移（已修）

- **mask.ts**：长度 9-11 保留前 3+后 3，长度 ≥12 保留前 3+后 4（之前长度 9 时输出 11 字符几乎无遮蔽）
- **control-plane.test.ts testProvider 用例**：`testProvider` 已升级为双参，断言追加 `undefined`

### 后端真实生产 Bug（已修）

- **APIKeyAuth**：空白 token 被识别为有效 → 加 `TrimSpace`
- **ChatCompletionRequest.Messages**：子项 role 未校验 → binding 加 `,dive`
- **test_handler.go**：未定义变量 `testModel` 导致编译失败 → `provider.TestModel`

---

## 6. CI/CD 接入建议

```yaml
# .github/workflows/test.yml（示例）
jobs:
  backend:
    runs-on: ubuntu-latest
    env: { GOPROXY: 'https://goproxy.cn,direct' }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go test ./... -count=1 -timeout=120s -cover -coverprofile=coverage.out
      - run: go tool cover -func=coverage.out

  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '23' }
      - working-directory: web
        run: |
          npm ci
          npm run test:coverage

  contract:
    runs-on: ubuntu-latest
    needs: [backend, frontend]
    steps:
      - uses: actions/checkout@v4
      - run: go test ./tests/contract/... -count=1
      - working-directory: web
        run: npm run test:run -- src/__contract__
```

### 准入门禁（建议）

| Gate | 阈值 |
| :-- | :-- |
| Go 单测通过率 | 100% |
| Go 受控包覆盖率（14 个目标包均值） | ≥ 85% |
| Vitest 通过率 | 100% |
| Vitest 组件/视图覆盖率（Stmts） | ≥ 90% |
| 契约测试 | 100% |
| `request_fields_ignored_by_backend` | 必须为 `[]`（即所有字段都已声明） |

---

## 7. 待办（按时间顺序）

| 优先级 | 任务 | 负责模块 | 状态 |
| :-: | :-- | :-- | :-: |
| **P0** | 补全 `CreateProviderInput/UpdateProviderInput` 共 7 字段（S0-1） | Team B | ✅ |
| **P0** | 补全 `CreateModelInput/UpdateModelInput` 共 7 字段（S0-2） | Team B | ✅ |
| **P0** | 新增 `GET /api/v1/models/check-name`（S0-3） | Team B | ✅ |
| **P0** | 修复完后清空 `contracts.json` 中两组 `request_fields_ignored_by_backend` | Team I | ✅ |
| **P1** | `ApiResponse<T>` 类型可选化（S1-1） | Team F | ✅ |
| **P1** | FE 加分页参数（S1-2） | Team F | ✅ |
| **P1** | testProvider 响应扩展（S1-3） | Team F | ✅ |
| **P1** | `SetModelTags` BE 解析失败返回 400（S1-4） | Team B-Core | ✅ |
| **P1** | 13 个机器可读 error code 体系（S1-5） | Team B-Core + Team F | ✅ |
| **P1** | `ToggleProviderStatus` FE 返回 Provider（S1-6） | Team F | ✅ |
| **P3** | TagDialog 关闭 alpha + BE color max=30（S3-1） | Team F + Team B-Core | ✅ |
| **P3** | SyncDialog `sync_not_supported` 友好文案（S3-2） | Team F + Team B-Core | ✅ |
| **P4** | `internal/controlplane/infra` 单测 | Team B-Coverage | ✅ |
| **P4** | `internal/gateway/server` 单测 | Team B-Coverage | ✅ |
| **P4** | `provider/{anthropic,bailian,bedrock,deepseek,litellm}` 单测 | Team B-Coverage | ✅ |
| **P4** | `cmd/*` 单测 | —— | ⏸️ 跳过（main 函数无 DI 重构难有意义覆盖） |
| **P5-1** | 删除 `disable_removed` 死字段 | Team B-Core | ✅ |
| **P5-2** | `unsupported` 字段始终输出 | Team B-Core | ✅ |
| **P5-3** | `api_version` 收敛到 `vendor_options`（已是现状，契约文档化） | Team I | ✅ |
| **跨团队 bug 1** | deepseek/litellm Chat URL 双路径 bug | 主合并阶段 | ✅ |
| **跨团队 bug 2** | crypto KeyManager 升级后 TestMain 失败 | 主合并阶段 | ✅ |
| **跨团队 bug 3** | crypto setupTestDB 共享 in-memory DB 互相污染 | 主合并阶段 | ✅ |
| **P2** | 401 登录重定向（S2-1） | Team F | ⏸️ 用户显式排除本轮 |
| **P2** | InternalAuth 强等值校验（S2-2） | Team B | ⏸️ 用户显式排除本轮 |

---

## 8. 产出物清单

| 类别 | 路径 |
| :-- | :-- |
| 基线 | `tests/reports/00-baseline.md` |
| 后端 | `tests/reports/backend-test-report.md` · `backend-coverage.txt` · `backend-coverage.out` · `backend-test-output.txt` |
| 前端 | `tests/reports/frontend-test-report.md` |
| 联调 | `tests/reports/integration-test-report.md` · `tests/contract/contracts.json` · `tests/contract/http_smoke_test.go` · `web/src/__contract__/control-plane.contract.spec.ts` |
| **综合（本报告）** | `tests/reports/FULL-TEST-REPORT.md` |

新增测试文件 **30 + 7 + 多** 个（首轮 30 + Team B-Coverage 7 + Team B-Core/F 在既有文件追加）、Go 接口 17 个、Vue spec 12 个、契约表 1 份。

本次（P1/P3/P4/P5）增量触碰生产代码（含跨团队 bug 修复）：
- BE：`internal/controlplane/handler/{errcodes.go(新), provider_handler.go, model_handler.go, sync_handler.go, model_tag_handler.go, definition_handler.go, internal_handler.go, test_handler.go}` · `internal/controlplane/service/sync_service.go` · `internal/controlplane/pkg/crypto/manager.go` · `internal/gateway/provider/{deepseek,litellm}/adapter.go`
- FE：`web/src/types/api.ts` · `web/src/services/{control-plane.ts, http.ts}` · `web/src/components/admin/{TagDialog.vue, ModelDialog.vue}` · `web/src/views/admin/{ProviderListPage.vue, ModelListPage.vue}`
- 契约：`tests/contract/contracts.json` · `tests/contract/http_smoke_test.go`

---

## 9. 复跑命令

```bash
# 环境前置（仅首次）
export PATH=/Users/jsy/.nvm/versions/node/v23.11.1/bin:$PATH
export GOPROXY=https://goproxy.cn,https://goproxy.io,direct

# 后端全量
cd /Users/jsy/Public/self_code/llm-gateway
go test ./... -count=1 -timeout=120s -cover -coverprofile=tests/reports/backend-coverage.out
go tool cover -func=tests/reports/backend-coverage.out

# 前端全量
cd web
npm run test:coverage

# 契约（前后端同跑）
cd /Users/jsy/Public/self_code/llm-gateway
go test ./tests/contract/... -count=1 -v
cd web && npm run test:run -- src/__contract__
```

---

## 10. 一句话价值

> **测试金字塔已经搭起来了**：底部是 130+ 后端单元测试（含 17 个 interface 重构）、中部是 280+ 前端组件/视图测试（含 8 组件 + 4 视图）、顶部是 111 个 FE↔BE 契约/冒烟测试 —— 但更重要的是，正是这套测试体系**第一次**在自动化层面把"看起来通、实际死透"的 14 个字段、1 个死代码路由、3 个生产 Bug 一次性扒了出来。**先修 P0 三项，立刻把契约门禁打开，往后这类破洞不可能再悄悄合入主干。**
