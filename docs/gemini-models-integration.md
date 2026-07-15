# Gemini 5 模型接入运行手册

> 目标：在 LLM Gateway 中接入 Gemini 并注册以下 5 个模型。
>
> - `gemini-3.1-pro-preview`（真实）
> - `gemini-3-flash-preview`（真实）
> - `gemini-3.5-flash`（真实）
> - `gemini-3.6-flash`（**占位**，Google 当前未发布）
> - `gemini-3.7-flash`（**占位**，Google 当前未发布）
>
> 本分支 `feat/gemini-models` 已从 `origin/main`（`22af6d06`）拉出，`go build ./cmd/gateway ./cmd/control-plane` 通过。

## 0. 协议结论（已核验，无需改 Go 代码）

Gateway 的 Gemini/Vertex 接入是**模型名无关**的，任何 `gemini-3.x-*` ID 都能原样进出：

| 能力 | 代码位置 | 说明 |
|------|----------|------|
| 原生 generateContent | `internal/providers/gemini/native.go:862` → `/models/{id}:generateContent` | `nativeChatCompletion` 用 `req.Model` 动态拼 URL |
| 原生流式 | `native.go:866` → `/models/{id}:streamGenerateContent?alt=sse` | 支持 SSE 流 |
| OpenAI 兼容 | `internal/providers/gemini/gemini.go:433-462` → `/chat/completions` | 已注册 `gemini`(main.go:342) / `vertex_ai`(main.go:364) |
| /models 发现 | `native.go:898` `isGeminiExposedModel` | 所有 `gemini-*` 均放行 |
| 思考参数 | `native.go:596-616` `thinkingConfigForEffort` | 只对 `gemini-2.5` 特判 `thinkingBudget`；3.x 走 `thinkingLevel` ✅ |

**注意事项（协议语义，非阻塞）：**

- **后端不同公开 ID 不同**：AI Studio 后端用裸名 `gemini-3.x-*`；Vertex 后端用 `google/gemini-3.x-*`（`native.go:887-893` `displayModelIDFromGemini`）。注册时 `upstream_model_id` 必须跟所在 channel 的后端一致。
- **思考**：`effort:"none"` 在 3.x 会变成 `thinkingLevel:"minimal"`（无法真正关闭思考）。若要完全关闭，用 `extra_body.google.thinking_config`（`native.go:565-594`）。
- **能力聚合是保守的**（`model_info_service.go:151-217`：组内**全员**支持才算支持）。所以每个模型要么绑定到声明了 vision/FC/tool_choice 的 catalog 条目，要么在组上设 `IsCapabilityOverride`，否则组能力会被拖成 false。

## 1. 前置：确认 provider / channel 已就绪

模型要能用，先得有指向 Gemini 的 channel：

- channel 类型：`gemini`（AI Studio，走 API Key）或 `vertex_ai`（Vertex，走 GCP 凭据）。
- 若用 `gemini`：填 `GEMINI_API_KEY`（或渠道 `api_key`）；走 `USE_GOOGLE_GEMINI_NATIVE_API` 决定用原生还是 OpenAI 兼容协议。
- 若用 `vertex_ai`：填 `base_url` 或 `vertex_project` + `vertex_location` + ADC/服务账号。
- 确认渠道 `enabled`、`status=1`。记录该渠道的 `provider_id`（models.provider_id 引用它）。

## 2. 五个模型的注册清单

| 模型 | catalog 状态 | 后端公开 ID（AI Studio） | 后端公开 ID（Vertex） | 需新增 catalog 行 |
|------|-------------|--------------------------|------------------------|-------------------|
| gemini-3.1-pro-preview | ✅ 已有（bare `litellm_catalog.json:15443` / `vertex_ai/:15739`） | `gemini-3.1-pro-preview` | `google/gemini-3.1-pro-preview` | 否 |
| gemini-3-flash-preview | ✅ 已有（bare `:17305` / `vertex_ai/:15626`） | `gemini-3-flash-preview` | `google/gemini-3-flash-preview` | 否 |
| gemini-3.5-flash | ✅ 已有（bare `:17362` / `vertex_ai/:15681`） | `gemini-3.5-flash` | `google/gemini-3.5-flash` | 否 |
| gemini-3.6-flash | ❌ 缺失 | `gemini-3.6-flash` | `google/gemini-3.6-flash` | **是**（第 4 节 SQL） |
| gemini-3.7-flash | ❌ 缺失 | `gemini-3.7-flash` | `google/gemini-3.7-flash` | **是**（第 4 节 SQL） |

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
   - `gemini-3.6-flash`/`gemini-3.7-flash` 在 Google 发布前会得到上游 404（透明透传），属预期；**不会真正被服务**，直到 Google 上线对应模型。
6. 前端管理页「模型 / 模型组 / Catalog」应展示 5 个模型；OpenCode 动态发现（`/v1/models` + `/v1/model/info`）自动列出，无需改插件。

## 6. 回归/测试

```bash
cd <worktree>   # .worktrees/gemini-models
go build ./cmd/gateway ./cmd/control-plane   # 已验证通过
# go test ./tests/...   # tests/ 下有历史遗留的 bedrock_model_id_check.go 编译问题（与本次无关）
```
