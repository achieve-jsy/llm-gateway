# Gemini 5 模型接入运行手册

> 目标：在 LLM Gateway 中接入 Gemini 并注册以下 5 个模型。
>
> - `gemini-3.5-flash`（**真实可用**，仅在 `asia-southeast1` 等亚洲区域）
> - `gemini-3-flash-preview`（**当前不可用**：Vertex 所有区域 404，需申请模型访问或换 OAuth）
> - `gemini-3.1-pro-preview`（**当前不可用**：同上）
> - `gemini-3.6-flash`（**占位**，Google 当前未发布）
> - `gemini-3.7-flash`（**占位**，Google 当前未发布）
>
> 本分支 `feat/gemini-models` 已从 `origin/main`（`22af6d06`）拉出，`go build ./cmd/gateway ./cmd/control-plane` 通过。

## 0. 实测结论（2026-09 用 4 个 Key 在 Vertex AI 上核验）

**接入协议：`vertex` 渠道 + `auth_type=api_key`（第三方 `google-genai` SDK 的 `genai.Client(api_key=..., vertexai=True)` 等价路径）。** 已对 4 个 Key（项目 `793586848164`/`791838655877`/`251441475008`/`439364517691`）全量核验，结果一致：

| 模型 | asia-southeast1 | us-central1 | 结论 |
|------|-----------------|-------------|------|
| `gemini-2.5-flash`（对照） | ✅ | ✅ | 真实模型 |
| `gemini-3.5-flash` | ✅ | ❌ 404 | **真实可用，区域锁定**（绑 `asia-southeast1`） |
| `gemini-3.5-pro-preview` | ❌ | ❌ | 不可用 |
| `gemini-3-pro-preview` / `3.1-pro-preview` | ❌ | ❌ | **所有测试区域 404**（需 Model Garden 申请访问，或换 OAuth/服务账号） |
| `gemini-3-flash-preview` | ❌ | ❌ | 同上 |
| `gemini-3.6-flash` / `3.7-flash` | ❌ | ❌ | **ID 不存在**（占位） |

三个结论：
1. **模型名是"区域锁定"的**：`gemini-3.5-flash` 只在 `asia-southeast1`/`asia-northeast1`/`asia-south1`/`europe-west2`/`northamerica-northeast1` 有效，`us-central1` 反而 404。**渠道必须配 `vertex_location=asia-southeast1`。**
2. **`3.x preview/pro` 模型对当前 Key/项目不可见**：连 `gemini-2.5-pro` 都能在你项目的 us 区用，但这些 3.x preview 全 404 —— 不是 ID 拼错，是**未获项目访问**。
3. **能真实服务的只有 `gemini-3.5-flash`**；其余 4 个建议按占位（catalog 补行）登记，等 Google 上线或申请到访问后再转真实。

## 0.1 本分支已做的 Go 改动：允许 Vertex 用 `auth_type=api_key`

原代码把 Vertex 后端**强制为 GCP 凭据**（`gcp_adc`/`gcp_service_account`），拒绝 API Key（所以才会把上面的 API-Key 用法挡掉）。本次改动让 Vertex 渠道支持 `api_key`，行为与 `google-genai` SDK 的 `vertexai=True + api_key` 一致（原生 `generateContent` 走 `x-goog-api-key`）：

| 文件 | 改动 |
|------|------|
| `internal/providers/gemini/gemini.go` | `validateConfig`：允许 vertex 后端 + `api_key`（原先直接报"requires gcp_adc/gcp_service_account"） |
| `internal/providers/vertex/vertex.go` | 支持 `api_key`：`validAuthType`/`normalizeAuthType`/`validateConfig`/`authHTTPClient`/`setHeaders`（设 `x-goog-api-key`） |
| `internal/providers/vertex/vertex.go` | `normalizeAuthType` 对**空 auth_type + 已配 API Key + 无服务账号**自动判定为 `api_key`；`newProvider` 把解析出的 authType **传给 gemini 子 provider**（否则 gemini 默认 `gcp_adc`，不设 `x-goog-api-key`，被 Google 拒绝） |
| `internal/providers/config.go` | `validVertexProviderConfig`：`auth_type=api_key` 时要求 `api_key` 非空 |
| `internal/providers/config_vertex_api_key_test.go` | 新增单元测试锁定行为 |
| `internal/providers/vertex/vertex_api_key_integration_test.go` | 新增 live 集成测试（显式 api_key + 空 auth_type 网关场景）**

> **关键点**：控制面 snapshot 只下发 `type/api_key/base_url`（不含 `auth_type`/`vertex_project`/`vertex_location`）。所以网关侧必须：① 空 auth_type + api_key 时自动判定为 `api_key`；② 把 project/location 放进 provider 的 `endpoint`（native base_url）。否则 gemini 子 provider 会默认走 `gcp_adc`，请求不带 key → Google 返回 `missing required authentication credential`。

> 走 OpenAI 兼容面（`/chat/completions`）时 gemini 的 `setHeaders` 用 `Authorization: Bearer`；走原生 generateContent（默认）时用 `x-goog-api-key`。**聊天默认走原生，因此 `api_key` 即可。**

**协议要点（沿用既有事实）：**
- Gateway 的 Gemini/Vertex 接入是**模型名无关**的：原生走 `internal/providers/gemini/native.go:862` `/models/{id}:generateContent`（流式 `:866` `streamGenerateContent?alt=sse`），`/models` 发现 `:898` `isGeminiExposedModel` 放行所有 `gemini-*`。
- **后端不同公开 ID 不同**：AI Studio 后端用裸名；Vertex 后端用 `google/gemini-3.x-*`（`native.go:887-893`）。注册 model 时 `upstream_model_id` 必须跟 channel 后端一致。
- **思考**：`effort:"none"` 在 3.x 会变成 `thinkingLevel:"minimal"`；要完全关闭用 `extra_body.google.thinking_config`（`native.go:565-594`）。
- **能力聚合保守**（`model_info_service.go:151-217`：组内全员支持才算支持）：每个模型要么绑到声明了 vision/FC/tool_choice 的 catalog 条目，要么在组上设 `IsCapabilityOverride`。


## 1. 前置：确认 provider / channel 已就绪

模型要能用，先得有指向 Gemini 的 channel：

- **推荐（本分支实测可用）——`vertex` + `auth_type=api_key`**（等价 SDK 的 `vertexai=True`）：
  - `type`：`vertex`（或 `gemini` + `backend=vertex`，二者都会被归一为 `vertex`，见 `config.go:683-689`）。
  - `auth_type`：`api_key`；`api_key`：填 Key（本次实测 4 个 Key 行为一致）。
  - `vertex_project`：填 Key 对应项目（例如 `793586848164`）；`vertex_location`：**必须 `asia-southeast1`**（`gemini-3.5-flash` 只在亚洲区有效）。
- 若用 `gemini`（AI Studio，走 `generativelanguage.googleapis.com` + API Key）：填 `api_key`，`auth_type` 留空即默认 api_key；但本次实测该 Developer API 对这些项目是 `SERVICE_DISABLED`（未启用）。
- 若用 `vertex_ai`（Vertex，走 GCP 凭据 `gcp_adc`/`gcp_service_account`）：填 `base_url` 或 `vertex_project` + `vertex_location` + ADC/服务账号。（本分支另已支持 `vertex`+`api_key`，无需服务账号。）
- 确认渠道 `enabled`、`status=1`。记录该渠道的 `provider_id`（models.provider_id 引用它）。

## 2. 五个模型的注册清单

| 模型 | 实际状态 | catalog 状态 | 后端公开 ID（AI Studio） | 后端公开 ID（Vertex） | 需新增 catalog 行 |
|------|----------|-------------|--------------------------|------------------------|-------------------|
| gemini-3.1-pro-preview | ⚠️ 当前不可用（所有区域 404，需申请访问） | ✅ 已有（bare `litellm_catalog.json:15443` / `vertex_ai/:15739`） | `gemini-3.1-pro-preview` | `google/gemini-3.1-pro-preview` | 否 |
| gemini-3-flash-preview | ⚠️ 当前不可用（同上） | ✅ 已有（bare `:17305` / `vertex_ai/:15626`） | `gemini-3-flash-preview` | `google/gemini-3-flash-preview` | 否 |
| gemini-3.5-flash | ✅ **真实可用**（`vertex_location=asia-southeast1`） | ✅ 已有（bare `:17362` / `vertex_ai/:15681`） | `gemini-3.5-flash` | `google/gemini-3.5-flash` | 否 |
| gemini-3.6-flash | ❌ 占位（ID 不存在） | ❌ 缺失 | `gemini-3.6-flash` | `google/gemini-3.6-flash` | **是**（第 4 节 SQL） |
| gemini-3.7-flash | ❌ 占位（ID 不存在） | ❌ 缺失 | `gemini-3.7-flash` | `google/gemini-3.7-flash` | **是**（第 4 节 SQL） |

## 3. 每个模型：注册 model 行 + 建组 + 加组成员

走管理后台（`/api/v1/models` 相关，`internal/controlplane/router/router.go`），权限为 `rbac.PermModelManage` / `rbac.PermGroupManage`。

1. **建 model**：`POST /api/v1/models`（或 `POST /api/v1/models/from-catalog`）
   - `name`：对外暴露的模型名（建议 = 组名，例如 `gemini-3.5-flash`）
   - `provider_id`：第 1 节的 Gemini/Vertex 渠道 id
   - `model_type`：`chat`
   - `upstream_model_id`：**按后端填**（裸名 或 `google/` 前缀）
   - `price_mode`：`catalog`（价格取自 model_catalog）
   - `catalog_id`：绑定到对应 catalog 条目（若 catalog 有该项；3.6/3.7 需先插入 catalog 行）
   - `enabled=true`, `status=1`
   - 依次为 5 个模型各建一条。
2. **建组**：`POST /api/v1/groups`
   - `name`：`gemini-3.5-flash`（= `/v1/models` 对外暴露的名字，`router.go:699`）
   - `model_type`：`chat`，`strategy`：`round_robin`，`enabled=true`，`open_mode` 按需 `public`/`controlled`
   - 若绑定不到声明了能力的 catalog，设 `is_capability_override=true` + `override_supports_vision/function_calling/tool_choice=true`
3. **加成员**：`POST /api/v1/groups/:id/models`（`groupHandler.AddModels`）
   - 把该模型的 `model_id` 加入组 → 写入 `group_models`。
4. **验证**：`GET /api/v1/groups/capabilities` 确认组聚合能力正确。

> 也可走批量：`POST /api/v1/models/batch-from-catalog` + `POST /api/v1/models/batch-bind-catalog`，一次注册多个。

## 4. 两个占位模型的 catalog 行（数据-only，无 schema 变更）

`3.6-flash` / `3.7-flash` 上游 LiteLLM 没有，`POST /api/v1/model-catalog/sync` 也不会带来（且无手动建 catalog 的 API）。因此**直接向 `model_catalog` 表插入**（能力字段对齐 `gemini-3.5-flash`），随后把它们绑定到 model 行。

> 注意：`model_catalog` 唯一键是 `(source, source_model_id)`，`source` 用 `litellm`（与现有条目一致），`source_model_id` 用裸模型名。`synced_at` 必填。若部署用 `SyncFromBundled` 且你希望它们随快照出现，也可把以下两条加到 `internal/controlplane/seed/litellm_catalog.json`（会被下次远程 sync 覆盖，因此更推荐直接插库）。

```sql
-- 占位：gemini-3.6-flash（待 Google 发布后替换为真实定价/能力）
INSERT INTO model_catalog
  (id, source, source_model_id, display_name, provider, mode,
   context_length, max_output_tokens,
   input_price_per_million, output_price_per_million,
   supports_function_calling, supports_vision, supports_prompt_caching,
   supports_response_schema, supports_tool_choice, supports_parallel_function_calling,
   synced_at, created_at, updated_at)
VALUES
  (gen_random_uuid(), 'litellm', 'gemini-3.6-flash', 'gemini-3.6-flash', 'vertex_ai-language-models', 'chat',
   1048576, 65535, 1.5, 9.0, -- 占位价（USD/1M tokens），待真实数据替换
   true, true, true, true, true, true, now(), now(), now()),
  (gen_random_uuid(), 'litellm', 'gemini-3.7-flash', 'gemini-3.7-flash', 'vertex_ai-language-models', 'chat',
   1048576, 65535, 1.5, 9.0,
   true, true, true, true, true, true, now(), now(), now());
```

然后在后台 `POST /api/v1/models/:id/bind-catalog` 把 `gemini-3.6-flash`/`gemini-3.7-flash` 的 model 行 `catalog_id` 指向这两条。

## 5. 验证（端到端）

1. `GET /api/v1/model-catalog/search/by-source/:id` → 5 条都能查到（3.6/3.7 来自第 4 节插入）。
2. `GET /api/v1/models` → 5 条 model 行，`catalog_id` 绑定正确。
3. `GET /api/v1/groups/capabilities` → 组聚合能力正确（vision/FC/tool_choice 为 true）。
4. 控制面保存 provider snapshot 后（`SaveProviderSnapshot`），gateway `ProviderLoader` 热重建；`GET /models` 读出 5 个外部组名。
5. Smoke：每个模型 `/v1/chat/completions`（AI Studio 用裸名、Vertex 用 `google/` 前缀）、`/v1/embeddings`、流式。
   - `gemini-3.5-flash`：唯一能真实服务的模型（渠道须配 `vertex_location=asia-southeast1`）。
   - `gemini-3-flash-preview` / `gemini-3.1-pro-preview`：当前所有区域 404（未获项目访问），**不会真正被服务**，直到你在 Model Garden 申请到访问或改 OAuth。
   - `gemini-3.6-flash` / `gemini-3.7-flash`：Google 未发布，404 透传属预期，**不会真正被服务**。
6. 前端管理页「模型 / 模型组 / Catalog」应展示 5 个模型；OpenCode 动态发现（`/v1/models` + `/v1/model/info`）自动列出，无需改插件。

## 6. 回归/测试

```bash
cd <worktree>   # .worktrees/gemini-models
go build ./cmd/gateway ./cmd/control-plane   # 已验证通过
# go test ./tests/...   # tests/ 下有历史遗留的 bedrock_model_id_check.go 编译问题（与本次无关）
```
