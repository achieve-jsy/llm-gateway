# Integration Test Report — Team I (FE↔BE Contract / E2E)

**生成时间**：2026-05-22
**负责人**：Team I（前端↔后端 集成 / 契约 / E2E）
**仓库**：`/Users/jsy/Public/self_code/llm-gateway`
**范围声明**：本报告只针对"链路通畅性"，不包含 Team B（后端单测/编译）与 Team F（前端单测）的修复内容。
**严禁清单（已遵守）**：未改 `internal/controlplane/{handler,service}/*.go`，未改 `web/src/services/{control-plane,gateway-api,http}.ts`，未 commit，未触碰 `00-baseline.md / backend-test-report.md / frontend-test-report.md`。

---

## 0. 最终 PASS/FAIL 汇总

| 测试类别 | 通过 | 失败 | 结论 |
| :-- | --: | --: | :-- |
| FE 契约测试 (vitest, `web/src/__contract__`) | 55 | 0 | **PASS** |
| BE 路由冒烟测试 (go test, `tests/contract`) | 56 | 0 | **PASS** |
| 字段一致性矩阵 — 红项 (❌) | — | **7** | **FAIL — 链路有破洞** |
| 字段一致性矩阵 — 黄项 (⚠️) | — | 11 | 需治理 |

> 结论：路由 / 鉴权 / JSON 包装层面**完全通**；但 **请求 body 字段**有 7 处"FE 发送的字段 BE 静默丢弃"导致功能性数据丢失（最严重在 ProviderDialog / ModelDialog / SyncModelsDialog 三个表单上）。

---

## 1. 路由清单与契约（method + path + status codes）

### 1.1 control-plane（端口 8081）

| # | Method | Path | Auth | Handler | 成功 | 客户端常见错误 |
| --: | :-- | :-- | :-- | :-- | --: | :-- |
| 1 | GET | `/healthz` | none | inline | 200 | — |
| 2 | GET | `/api/v1/provider-definitions` | JWT | `DefinitionHandler.ListDefinitions` | 200 | 401 |
| 3 | GET | `/api/v1/provider-definitions/:id` | JWT | `DefinitionHandler.GetDefinition` | 200 | 401, 404 |
| 4 | POST | `/api/v1/providers` | JWT | `ProviderHandler.CreateProvider` | 201 | 400, 401, 409, 500 |
| 5 | GET | `/api/v1/providers` | JWT | `ProviderHandler.ListProviders` | 200 | 401, 500 |
| 6 | GET | `/api/v1/providers/:id` | JWT | `ProviderHandler.GetProvider` | 200 | 400, 401, 404, 500 |
| 7 | PUT | `/api/v1/providers/:id` | JWT | `ProviderHandler.UpdateProvider` | 200 | 400, 401, 404, 409, 500 |
| 8 | PATCH | `/api/v1/providers/:id/status` | JWT | `ProviderHandler.ToggleProviderStatus` | 200 | 400, 401, 404, 500 |
| 9 | DELETE | `/api/v1/providers/:id` | JWT | `ProviderHandler.DeleteProvider` | 200 | 400, 401, 404, 409, 500 |
| 10 | POST | `/api/v1/providers/:id/test` | JWT | `TestHandler.TestProvider` | 200 | 400, 401, 404, 500 |
| 11 | POST | `/api/v1/providers/:id/sync-models` | JWT | `SyncHandler.SyncModels` | 200 | 400, 401, 404, 500, 502 |
| 12 | POST | `/api/v1/providers/:id/sync-models/confirm` | JWT | `SyncHandler.ConfirmSync` | 200 | 400, 401, 500 |
| 13 | POST | `/api/v1/models` | JWT | `ModelHandler.CreateModel` | 201 | 400, 401, 409, 500 |
| 14 | GET | `/api/v1/models` | JWT | `ModelHandler.ListModels` | 200 | 400, 401, 500 |
| 15 | PUT | `/api/v1/models/:id` | JWT | `ModelHandler.UpdateModel` | 200 | 400, 401, 404, 409, 500 |
| 16 | DELETE | `/api/v1/models/:id` | JWT | `ModelHandler.DeleteModel` | 200 | 400, 401, 404, 500 |
| 17 | GET | `/api/v1/model-tags` | JWT | `ModelTagHandler.ListTags` | 200 | 401, 500 |
| 18 | POST | `/api/v1/model-tags` | JWT | `ModelTagHandler.CreateTag` | 201 | 400, 401, 409, 500 |
| 19 | PUT | `/api/v1/model-tags/:id` | JWT | `ModelTagHandler.UpdateTag` | 200 | 400, 401, 404, 500 |
| 20 | DELETE | `/api/v1/model-tags/:id` | JWT | `ModelTagHandler.DeleteTag` | 200 | 400, 401, 500 |
| 21 | GET | `/api/v1/models/:id/tags` | JWT | `ModelTagHandler.GetModelTags` | 200 | 400, 401, 500 |
| 22 | PUT | `/api/v1/models/:id/tags` | JWT | `ModelTagHandler.SetModelTags` | 200 | 400, 401, 500 |
| 23 | GET | `/internal/v1/models/available` | Internal | `InternalHandler.ListAvailableModels` | 200 | 403, 500 |

### 1.2 gateway（端口 8080）

| # | Method | Path | Auth | Handler |
| --: | :-- | :-- | :-- | :-- |
| 1 | GET | `/health` | none | inline |
| 2 | GET | `/ready` | none | inline |
| 3 | POST | `/v1/chat/completions` | API Key | `ChatHandler.Handle` |
| 4 | GET | `/v1/models` | API Key | `ModelsHandler.Handle` |

### 1.3 统一响应包装（Go handler 现状）

| 形态 | 形如 | 实际使用场景 |
| :-- | :-- | :-- |
| `success_single` | `{ "data": <T> }` | 单资源 GET/POST/PUT |
| `success_list` | `{ "data": <T[]>, "total": N, "page": N }` | 列表 GET |
| `success_message` | `{ "message": "<verb> deleted" }` | DELETE |
| `error` | `{ "error": { "type": "...", "code": "...", "message": "..." } }` | 4xx/5xx |

> ⚠️ **不一致点**：BE 既无 `code` 也无 `message` 字段，但前端 `web/src/types/api.ts` 中的 `ApiResponse<T>` 与 `PaginatedResponse<T>` 都把 `code`/`message` 标注为**必填**（前端 lying types）。详见 §5-S2。

---

## 2. 字段一致性矩阵（按表单 × 端点 × DB）

图例：✅ 三方一致 │ ⚠️ FE/BE/DB 命名一致但**BE 静默丢字段**或可空 │ ❌ 链路功能性破坏

### 2.1 `ProviderDialog.vue` → `POST/PUT /api/v1/providers`

| FE form/payload | BE `CreateProviderInput` 接受？ | BE `UpdateProviderInput` 接受？ | DB column (`providers`) | 状态 |
| :-- | :-- | :-- | :-- | :-: |
| `name` (required) | ✅ required,max=100 | ✅ omitempty,max=100 | `name VARCHAR(100) NOT NULL UNIQUE` | ✅ |
| `definition_id` (required) | ✅ required,max=50 | ✅ omitempty,max=50 | `definition_id VARCHAR(50)` | ✅ |
| `type` (FE optional) | ✅ omitempty | ✅ omitempty | `type VARCHAR(50) NOT NULL` | ⚠️ FE 几乎从不传 `type`，BE allows empty 而 DB NOT NULL → 实测插入会用 zero-value `""` 占位 |
| `endpoint` (required) | ✅ required,max=500 | ✅ omitempty,max=500 | `endpoint VARCHAR(500) NOT NULL` | ✅ |
| `credentials` (FE 始终发，编辑可空) | ✅ required | ✅ omitempty | `encrypted_credentials TEXT NOT NULL` | ✅ |
| `weight` | ❌ **未声明** | ❌ **未声明** | `weight INT NOT NULL DEFAULT 1` | ❌ **FE 发出后被 Gin 静默丢弃**，DB 永远保留默认 1 |
| `priority` | ❌ **未声明** | ❌ **未声明** | `priority INT NOT NULL DEFAULT 0` | ❌ |
| `test_model` | ❌ **未声明** | ❌ **未声明** | `test_model VARCHAR(200) DEFAULT ''` | ❌ |
| `remark` | ❌ **未声明** | ❌ **未声明** | `remark VARCHAR(500) DEFAULT ''` | ❌ |
| `max_retries` | ❌ **未声明** | ❌ **未声明** | `max_retries INT NOT NULL DEFAULT 3` | ❌ |
| `timeout` | ❌ **未声明** | ❌ **未声明** | `timeout INT NOT NULL DEFAULT 30` | ❌ |
| `tag` | ❌ **未声明** | ❌ **未声明** | `tag VARCHAR(100) DEFAULT ''` | ❌ |
| `region` (Bedrock 表单内字段) | 由 FE 拼到 `endpoint` 中 | 同左 | — | ⚠️ 仅前端语义，不直接落库 |
| `api_version` (Azure 表单内字段) | 当前 FE **未发送给后端** | 当前 FE **未发送给后端** | provider_definitions.vendor_options | ❌ Azure API Version 永远是定义层默认值 |

> 单表合计 ❌ × **7**（weight/priority/test_model/remark/max_retries/timeout/tag），用户在"高级设置"页填的内容**全部丢失**。

### 2.2 `ModelDialog.vue` → `POST/PUT /api/v1/models`

| FE form/payload | BE `CreateModelInput` 接受？ | BE `UpdateModelInput` 接受？ | DB column (`models`) | 状态 |
| :-- | :-- | :-- | :-- | :-: |
| `name` (required) | ✅ required,max=200 | ✅ omitempty,max=200 | `name VARCHAR(200) NOT NULL UNIQUE` | ✅ |
| `provider_id` (required, uuid) | ✅ required,uuid | ✅ omitempty,uuid | `provider_id UUID NOT NULL` | ✅ |
| `model_type` (chat/embedding) | ✅ required, oneof | ✅ omitempty, oneof | `model_type VARCHAR(50) NOT NULL DEFAULT 'chat'` | ✅ |
| `enabled` | n/a (Create 时强制 true) | ✅ `*bool` | `enabled BOOLEAN NOT NULL DEFAULT true` | ⚠️ Create 强制 true，FE 创建表单也无开关，OK |
| `upstream_model_id` | ❌ **未声明** | ❌ **未声明** | `upstream_model_id VARCHAR(200) DEFAULT ''` | ❌ **FE 提交后丢失** |
| `description` | ❌ **未声明** | ❌ **未声明** | `description VARCHAR(500) DEFAULT ''` | ❌ |
| `context_length` | ❌ **未声明** | ❌ **未声明** | `context_length INT DEFAULT 0` | ❌ |
| `max_output_tokens` | ❌ **未声明** | ❌ **未声明** | `max_output_tokens INT DEFAULT 0` | ❌ |
| `input_price` | ❌ **未声明** | ❌ **未声明** | `input_price DECIMAL(10,6) DEFAULT 0` | ❌ |
| `output_price` | ❌ **未声明** | ❌ **未声明** | `output_price DECIMAL(10,6) DEFAULT 0` | ❌ |
| `weight` | ❌ **未声明** | ❌ **未声明** | `weight INT NOT NULL DEFAULT 1` | ❌ |
| FE form 的 `tag_ids` | 通过 `PUT /api/v1/models/:id/tags` 二段提交 | — | `model_tag_relations` | ✅ 走专门端点，OK |

> 单表合计 ❌ × **7**，模型注册表单的"参数配置"区域**完全无效**。

### 2.3 `TagDialog.vue` → `POST/PUT /api/v1/model-tags`

| FE form/payload | BE 接受？ | DB column (`model_tags`) | 状态 |
| :-- | :-- | :-- | :-: |
| `name` (required) | ✅ required,max=50 | `name VARCHAR(50) NOT NULL UNIQUE` | ✅ |
| `color` (required) | ✅ required,max=7 | `color VARCHAR(7) NOT NULL` | ⚠️ FE 用 `el-color-picker show-alpha` 可能产出 `rgba(...)` > 7 字符，BE max=7 会 422 失败 |
| `description` | ✅ max=200 | `description VARCHAR(200)` | ✅ |
| `sort_order` | ✅ (create=`int`, update=`*int`) | `sort_order INT NOT NULL DEFAULT 0` | ✅ |

### 2.4 `SyncModelsDialog.vue` → `POST /api/v1/providers/:id/sync-models{,/confirm}`

| FE field | BE 接受？ | 状态 |
| :-- | :-- | :-: |
| `syncModels(providerId)` 无 body | ✅ | ✅ |
| `confirmSync.models_to_add: string[]` | ✅ `binding:"required"` | ✅ |
| `confirmSync.disable_removed?: boolean` | ✅ `*bool` | ⚠️ FE 当前从未传入；BE 的 `disable_removed` 分支实际为空（注释承认未实现）→ 功能 No-op |
| `syncResult.unsupported: boolean` | ⚠️ BE 在 `unsupported=true` 时直接返回 400，**永远不会**把 `{"unsupported": true}` 嵌在 `data` 里；FE `SyncResult.unsupported` 字段无效 | ⚠️ |

### 2.5 分页参数大小写一致性

| 调用 | FE query | BE 解析 | 状态 |
| :-- | :-- | :-- | :-: |
| `getProviders` | FE 当前**未发**任何 page 参数（一次性拉全部） | BE `DefaultQuery("page","1")`,`DefaultQuery("page_size","20")` | ⚠️ FE 一旦数据量上来必须改为分页；命名风格已对齐 `snake_case` |
| `getModels` | FE 只传 `provider_id` | BE 默认 page=1, page_size=20 | ⚠️ 同上 |

### 2.6 错误码处理覆盖（FE `http.ts` 拦截器 + 各 dialog catch）

| 状态码 | BE 触发场景 | FE `http.ts` 行为 | FE 业务层处理 | 状态 |
| :-- | :-- | :-- | :-- | :-: |
| 401 | JWT 缺失/无效 | `ElMessage.error("[401] ...")` | 无跳转登录 / 无 token 刷新 | ⚠️ 缺乏鉴权失败重定向 |
| 403 | InternalAuth 缺失 | `ElMessage.error("[403] ...")` | 仅内部接口；前端不会撞到 | ✅ |
| 404 | 资源不存在 / 路径不匹配 | `ElMessage.error("[404] ...")` | dialog 普通 catch，无差异化 | ⚠️ |
| 409 | name 冲突 / has_models | **不弹全局**，留给业务层 | dialog 只显示通用 `创建失败` | ⚠️ 缺重名/有关联模型的语义提示 |
| 422 | 无（BE 全部用 400） | 不会触达 | — | ✅ |
| 500 | 内部错 | `ElMessage.error("[500] ...")` | 业务层 catch 通用提示 | ✅ |
| 502 | upstream_error（同步模型） | `ElMessage.error("[502] ...")` | 同步对话框只 toast | ⚠️ 用户不知是上游问题 |

---

## 3. FE 契约测试结果（vitest）

```bash
cd web && npm run test:run -- src/__contract__
```

```
 ✓ src/__contract__/control-plane.contract.spec.ts  (55 tests) 18ms

 Test Files  1 passed (1)
      Tests  55 passed (55)
   Duration  592ms
```

**55/55 PASS**。覆盖：
- A. 静态契约自洽（FE 客户端 22 条 call ↔ BE 22 条 route 路径匹配） — 22 个用例。
- B. 请求字段允许集（每条 POST/PUT 的 body 字段集合 ⊆ BE required ∪ optional ∪ **ignored**） — 6 个用例（其中 `createProvider` / `updateProvider` / `createModel` / `updateModel` 的"被 BE 忽略字段"在契约里**显式标注** `request_fields_ignored_by_backend`，否则会直接红）。
- C. 运行时调用（每个 FE 函数发出的真实 URL/method/body 用 mock http 抓取并断言） — 22 个用例。
- D. gateway-api.ts 客户端 — 1 个用例。
- E. 响应解构兼容（FE 用 `.data.data` 解 BE 风格响应） — 3 个用例。
- 静态契约自洽循环展开 ≈ 22 个隐式用例。

> 关键：B 类用例**靠 `request_fields_ignored_by_backend` 兜底通过**，这正是矩阵中 ❌ 的可机器化跟踪机制 —— 一旦 BE 加上对应字段，把列表元素删掉，CI 不会假阴。

## 4. BE 路由冒烟测试结果（go test）

```bash
GOPROXY=https://goproxy.cn,direct go test ./tests/contract/... -count=1 -v
```

```
=== RUN   TestControlPlane_RoutesSmoke       (23 subtests PASS)
=== RUN   TestControlPlane_AuthEnforced      (22 subtests PASS — admin→401, internal→403)
=== RUN   TestControlPlane_InvalidPath       PASS
=== RUN   TestGateway_RoutesSmoke            (4 subtests PASS)
=== RUN   TestGateway_AuthEnforced           PASS
=== RUN   TestErrorEnvelope_Shape_TypeCodeMessage PASS
=== RUN   TestContractsTable_AlignsWithRouter PASS

ok  	github.com/example/llm-gateway/tests/contract	0.023s
```

**7 个顶层函数 / 56 个 subtest 全部 PASS**。注意：
- 桩 handler 用 `gin.New() + 真实中间件（JWTAuth/InternalAuth/APIKeyAuth）`，保证认证链路、路由注册、JSON 包装、Content-Type 都被严格断言。
- **不使用** `internal/controlplane/repository`/`service` 等需要 PG/Redis 的依赖，避免在本机 docker 缺失场景下被卡。
- `TestContractsTable_AlignsWithRouter` 把"路由是否注册"做成单测断言，作为对 `router.go` 的回归网。
- 真实 handler 的 200/400/409 行为由 Team B 的 `internal/controlplane/handler/*_test.go` 覆盖；本测试**专注链路通畅性**。

---

## 5. 发现的不一致 / 错误码缺漏 / 字段命名分歧（按严重度）

### S0（**链路功能性破坏**，必须立刻修）

#### S0-1：`POST /api/v1/providers` 与 `PUT /api/v1/providers/:id` 接受不到高级字段
- **现象**：前端 `ProviderDialog` 高级设置页的 `weight / priority / test_model / remark / max_retries / timeout / tag` 共 7 个字段在 `Create/Update` 请求体里都被 Gin 静默丢弃（input struct 未声明）。
- **影响**：用户填什么都没用；后端永远是默认值；列表页拉出来的也是默认值；DB 列闲置。
- **路径**：`internal/controlplane/service/provider_service.go` 的 `CreateProviderInput` 与 `UpdateProviderInput`。
- **建议**：参见 §6-R1。

#### S0-2：`POST /api/v1/models` 与 `PUT /api/v1/models/:id` 接受不到模型参数字段
- **现象**：前端 `ModelDialog` 的"参数配置"区（`upstream_model_id / description / context_length / max_output_tokens / input_price / output_price / weight`）共 7 个字段被 Gin 静默丢弃。
- **影响**：模型管理表单 50% 区域**形同摆设**；网关路由时无法用 `upstream_model_id` 做"上游别名映射"（最关键功能之一）。
- **路径**：`internal/controlplane/service/model_service.go` 的 `CreateModelInput` / `UpdateModelInput`。
- **建议**：参见 §6-R1。

#### S0-3：`checkModelNameUnique` 完全是死代码
- **现象**：FE `control-plane.ts:85` 调用 `GET /api/v1/models?check_name=X` 期望 `{data:{available:boolean}}`；BE `ModelHandler.ListModels` **不识别** `check_name` query，返回的是 `{data:Model[],total,page}`。FE 后续 `response.data.data.available` 等价于读取数组的 `.available` 属性 → `undefined` → 函数永远返回 `false`（不可用）。
- **影响**：`ModelDialog` 的"模型名重复"提示永远**误报** "已存在"，但代码里却没真正阻断提交（因为 `nameConflict.value = !available = !false = true`），最终用户点提交后才被 BE 拒。前端"实时唯一性校验"完全是骗自己的。
- **建议**：参见 §6-R3。

### S1（**功能可见，但不影响主流程**）

#### S1-1：响应包装类型不诚实
- `web/src/types/api.ts` 的 `ApiResponse<T>` 与 `PaginatedResponse<T>` 都把 `code: number` 和 `message: string` 标为必填，但 BE 从来不返回这两个字段。读到 `response.data.data` 时不爆，但任何尝试读 `response.data.code` / `.message` 的代码都会获得 `undefined`，且类型系统不会告警。
- 建议：要么 BE 加上统一 `code/message`，要么 FE 类型改为 `code?: number; message?: string`（兼容现有 BE）。

#### S1-2：`getProviders` / `getModels` 不分页
- FE 不传 page/page_size，全量拉。一旦渠道数 > 20，BE 默认 `page_size=20` 会截断，FE 直接显示不全且无翻页 UI。
- 建议：FE 加分页查询参数或将默认 page_size 调大。

#### S1-3：`testProvider` 响应字段缩水
- BE 返回 `{success,response_time,status_code?,model_count?,error?}`，FE 类型只承认 `{response_time}`。`success=false` 的失败原因前端读不到。
- 建议：FE 类型扩展为 `{success: boolean; response_time: number; status_code?: number; model_count?: number; error?: string}`。

#### S1-4：`PUT /api/v1/models/:id/tags` 用 `string[]` 但 BE 内部 `uuid.Parse(idStr)` 失败 silently `continue`
- 若 FE 误传非 UUID 字符串，BE 会"成功"，但实际写入的关系比预期少。无任何报错给 FE。
- 建议：BE 解析失败时返回 400 而非静默跳过。

#### S1-5：错误码全部走 `code:bad_request` / `code:internal`
- 400 永远是 `invalid_request/bad_request`，500 永远是 `internal_error/internal`，无机器可读细分。FE `http.ts` 的拦截器无法做差异化处理（如 401 跳登录、422 字段级提示）。
- 建议：参见 §6-R4。

#### S1-6：`ToggleProviderStatus` 返回完整 ProviderResponse，FE 只 await void 丢弃
- BE 200 返回 `{data: ProviderResponse}`，FE `toggleProviderStatus` 标记返回 `Promise<void>` 直接扔掉。错过了状态翻转后立刻刷新本地缓存的机会，列表页只能整页 reload。
- 建议：FE 改返回类型为 `Promise<Provider>` 并复用。

### S2（**鉴权/会话治理缺口**）

#### S2-1：401 无登录重定向
- FE `http.ts` 收到 401 仅 `ElMessage.error`，不会跳转 SSO/登录页。当前用 hardcoded `dev-token` 跑得通，但接入真实 SSO 后用户会迷失。
- 建议：401 时清 token 并 router.push('/login') 或触发 SSO 重新挑战。

#### S2-2：`InternalAuth` 不校验 token 值
- `middleware.go:InternalAuth` 只检查 `X-Internal-Token` 非空，**任何字符串都放行**。当前只用于网关内部，但若误暴露到公网即裸奔。
- 建议：环境变量配置 `INTERNAL_TOKEN_SECRET`，强等值校验。

### S3（**前端文案/UX 小问题**）

- S3-1：`TagDialog` 颜色框允许 RGBA，BE max=7 校验会失败（仅 #RRGGBB 才合法）。建议关闭 `show-alpha` 或 BE 扩展为 max=20 接受 rgba。
- S3-2：`SyncModelsDialog` 永远不会展示"上游不支持同步"的语义提示，BE 直接 400，前端 toast `注册失败`。

---

## 6. 解决方案 / 改造建议

### R1（首要，对应 S0-1 / S0-2）

补全 `CreateProviderInput` / `UpdateProviderInput` / `CreateModelInput` / `UpdateModelInput` 的字段。示例：

```go
type CreateProviderInput struct {
    Name         string `json:"name" binding:"required,max=100"`
    DefinitionID string `json:"definition_id" binding:"required,max=50"`
    Type         string `json:"type" binding:"omitempty,max=50"`
    Endpoint     string `json:"endpoint" binding:"required,max=500"`
    Credentials  string `json:"credentials" binding:"required"`
    Weight       *int    `json:"weight" binding:"omitempty,min=0,max=9999"`
    Priority     *int    `json:"priority" binding:"omitempty,min=0,max=9999"`
    TestModel    string `json:"test_model" binding:"omitempty,max=200"`
    Remark       string `json:"remark" binding:"omitempty,max=500"`
    MaxRetries   *int    `json:"max_retries" binding:"omitempty,min=0,max=10"`
    Timeout      *int    `json:"timeout" binding:"omitempty,min=1,max=300"`
    Tag          string `json:"tag" binding:"omitempty,max=100"`
}
```

并在 service 层把字段透传到 `model.Provider`。`UpdateProviderInput` 同样用指针字段以区分"未传"与"传零值"。

`CreateModelInput` 把 `UpstreamModelID/Description/ContextLength/MaxOutputTokens/InputPrice/OutputPrice/Weight` 加上。同理 `UpdateModelInput`。

**契约测试随之收紧**：把 `tests/contract/contracts.json` 中对应路由的 `request_fields_ignored_by_backend` 清空，FE 的 spec.B.* 用例如果有任何 FE 字段不在 BE 接受集就会立刻 RED。

### R2（对应 S0-3）

二选一：
- 方案 A：BE 在 `ListModels` 头部加分支：`if name := c.Query("check_name"); name != "" { ... return {"data":{"available": !exists}} }`。
- 方案 B：FE 改 `checkModelNameUnique` 走真正的 `GET /api/v1/models?check_name=…`，BE 加一个 `GET /api/v1/models/check-name?name=…` 的独立路由（语义更清晰）。

推荐 B，避免一个端点服务两种语义。

### R3（对应 S1-1）

统一响应包装：
- 短期：FE 把 `code?`/`message?` 改为可选。
- 长期：BE 统一中间件，所有 2xx 响应注入 `{"code":0,"message":"ok","data":...}` 形态，所有 4xx/5xx 注入 `{"code":<machine_code>,"message":"...","error":{...}}`，前端做差异化处理。

### R4（对应 S1-5）

后端 `errormap.Mapper` 引入 machine code（`E_PROVIDER_DUPLICATE_NAME / E_PROVIDER_HAS_MODELS / E_MODEL_DUPLICATE_NAME / E_SYNC_NOT_SUPPORTED / E_UPSTREAM / ...`），前端 `http.ts` 按 code 决定 toast 文案，dialog 业务层按 code 决定行为（重名 → 高亮 `name` 输入框；有模型 → 提示先删模型）。

### R5（鉴权增强，对应 S2-*）

- `cpmw.JWTAuth()` 接 SSO，校验失败 → 401 with `code:E_AUTH_REQUIRED`，FE 拦截后跳转。
- `cpmw.InternalAuth()` 加 `os.Getenv("INTERNAL_TOKEN")` 强等值校验。

### R6（流程治理）

- 把 `tests/contract/contracts.json` 纳入 CI，FE 改 `control-plane.ts` 必须同步更新 contracts。
- BE 改 `router.go` 必须同步更新 contracts。
- `tests/contract/http_smoke_test.go` 的 `TestContractsTable_AlignsWithRouter` 当作回归网，发现 route 漂移立刻失败。

---

## 7. 复跑命令

```bash
# 前端契约
export PATH=/Users/jsy/.nvm/versions/node/v23.11.1/bin:$PATH
cd web && npm run test:run -- src/__contract__

# 后端冒烟
export GOPROXY=https://goproxy.cn,https://goproxy.io,direct
cd /Users/jsy/Public/self_code/llm-gateway
go test ./tests/contract/... -count=1 -v
```

---

## 8. 产出物清单

| 文件 | 说明 |
| :-- | :-- |
| `tests/contract/contracts.json` | 手工对齐的 FE↔BE 契约表（路由 23 条 + FE call 22 条 + envelope 规约） |
| `tests/contract/http_smoke_test.go` | Go 路由 + 鉴权 + JSON 包装冒烟测试（7 函数 / 56 subtest） |
| `web/src/__contract__/control-plane.contract.spec.ts` | Vitest 契约 spec（55 cases） |
| `tests/reports/integration-test-report.md` | 本报告 |

未触碰：`tests/reports/00-baseline.md`、`backend-test-report.md`、`frontend-test-report.md`，未修改任何 production handler/service 与 FE service 文件。
