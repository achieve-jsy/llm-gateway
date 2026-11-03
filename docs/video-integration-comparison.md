# 网关视频接入对比分析：本网关 vs LiteLLM vs New API

> 分析对象：`llm-gateway`（本仓库）当前的「视频生成 + 素材库」接入方式，与 LiteLLM、New API 的视频方案对比。
> 分析方法：**全部基于源码的一手证据**（每条结论带 `文件:行号`），不使用二手文档转述。
>
> | 对比对象 | 证据来源 | 版本基线 |
> |----------|----------|----------|
> | 本网关 | 本仓库源码 | 当前工作区 |
> | LiteLLM | `/Users/jsy/Public/self_code/litellm` | `v1.84.0-rc.1-1180-g9cac0471ae`（commit `9cac0471ae`，2026-05-27） |
> | New API | `/Users/jsy/Public/self_code/new-api` | `v0.11.0-alpha.9-1-g982dc5c5`（commit `982dc5c5`，2026-02-25） |
> | one-api | `/Users/jsy/Public/self_code/one-api` | `v0.6.11-alpha.22-1-g8df4a26`（commit `8df4a26`，2025-02-21） |
>
> 说明：本次调研由 4 个并行分析智能体完成（本网关全链路 / LiteLLM / New API+one-api / 交叉核验）。本机联网检索通道不可用，故改用本机源码克隆做离线一手分析，这比文档转述更可靠；关键结论已由主分析者抽样复核，其中**修正了 New API 创建响应 `status` 字段的一处误判**（见 §3.1）。

---

## 0. 结论速览（TL;DR）

| 维度 | 本网关 | LiteLLM（v1.84.0-rc.1） | New API（v0.11.0-alpha.9） |
|------|--------|------------------------|---------------------------|
| 对外协议 | **火山方舟 v3 原生**：`/v1/contents/generations/tasks` | **OpenAI Sora 兼容**：`/v1/videos` 全家桶 | **双协议**：自研 `/v1/video/generations` + OpenAI `/v1/videos` |
| 请求体归一化 | 无（原样透传，仅改写 `model`） | 统一 `VideoCreateOptionalRequestParams` → 各 provider transformation 转换 | 统一 `TaskSubmitReq` → 各 adaptor 转换，`metadata` 透传厂商私有参数 |
| 创建响应 | `{"id": "..."}`（丢弃上游其余字段） | `VideoObject`（id/status/progress/…） | `OpenAIVideo`（**`status` 实际为空串**，见 §3.1 修正） |
| 任务 id 归属 | 直接沿用**上游 id** | `video_<base64(provider;model;id)>` 自编码 id | **对外 `task_<rand>` 与上游 id 分离**（存 `PrivateData.UpstreamTaskID`） |
| 异步任务推进 | 客户端轮询，**每次回源上游** | 客户端轮询，每次回源上游 | **服务端 task 表 + 15s 后台轮询**（查询读库，仅 Gemini/Vertex 回源） |
| 任务持久化 | Redis(30d) + PG `video_tasks`/`video_task_events` | **无**（无表、无 worker、无 webhook） | `tasks` 表（+ `PrivateData`/`BillingContext` JSON 列） |
| 超时与退款 | 无 | 无 | 24h 超时置失败 + **自动退款**；CAS 防竞态 |
| 计费 | **0 成本**（token 计价路径不适用） | **按秒**：`output_cost_per_video_per_second` × duration | **按次**：`model_price` × groupRatio（+ 可选 OtherRatios 倍率、终态差额结算） |
| 预算/配额 | ❌ 不接入（不预扣、不受配额） | ✅ 复用 key/team/org budget + rpm/tpm | ✅ 复用 token 鉴权 + `PreConsumeBilling` 预扣 |
| 视频厂商覆盖 | **1 类**（Seedance：景汐中转 + 火山官方，双槽位） | **5 家**：OpenAI Sora、Azure、Gemini、Vertex Veo、RunwayML | **10 个适配器**：Doubao/VolcEngine、Kling、即梦、Vidu、Hailuo、万相、Sora/OpenAI、Gemini、Vertex |
| 素材库/真人认证 | ✅ **独有**（10 个 Action） | ❌ | ❌ |
| 内容下载代理 | ❌（客户端直连上游 `video_url`） | ✅ `GET /v1/videos/{id}/content` | ✅ `GET /v1/videos/{id}/content` |
| 任务列表 | ❌ | ✅ `GET /v1/videos` | 管理端 `/api/task/*`（无对外列表） |
| 扩展机制 | **硬编码双槽位** | provider `transformation.py`，新增 provider 加目录 | 按 channelType switch（新版为 JS 插件） |

### 五条核心结论

1. **厂商覆盖才是最大差距，不是协议形状**：三家的支持集合几乎不重叠——LiteLLM 覆盖海外（Sora/Veo/Runway），New API 覆盖国内（Seedance/Kling/即梦/Vidu/Hailuo/万相），本网关只有 Seedance 一系。协议用原生还是 OpenAI 兼容是战略选择；**能不能接上你要用的那家厂商才是硬差距**。
2. **本网关是"半原生"**：路径被收编进 `/v1`、上游错误统一归一成 502，但请求/响应体仍暴露火山原生语义——既拿不到原生协议的全部能力（`callback_url`、`draft`、`tools` 等虽可透传但文档未承诺），又拿不到 OpenAI 兼容的生态红利（SDK 直接用不了）。
3. **架构上最根本的差别是"任务生命周期归谁"**：只有 New API 把服务端做成唯一事实源（task 表 + 15s 轮询 + 超时退款 + 终态结算）。本网关与 LiteLLM 都把生命周期交给客户端——代价是：客户端停止轮询则任务永远停在 `submitted`、无法做终态计费结算、上游查询 QPS 与客户端轮询频率成正比。
4. **本网关在三家里"控制面完备度"最高**：`video_tasks` 台账 + `video_task_events` 时间线 + 统计/趋势/分布接口 + 管理面页面 + 素材库 + 配置热加载 + 视频能力元数据进 catalog。LiteLLM 连任务表都没有；New API 的对外任务视图只有管理端 API。
5. **当前最该修的是风控而非功能**：视频与 chat 共用同一条 `/v1` 中间件链，却**绕过预算预扣、四层配额、`BillingFactGuard` 背压、`TraceCapture` 正文采集**，且 `GetTask` **不校验任务归属**（任意有效 Key 知道 task_id 即可读到他人任务的原始上游响应）。这不是"少个功能"，是可被滥用的口子。

---

## 1. 当前网关的视频接入（已逐行核验）

### 1.1 对外接口形状：上游原生协议，不是 OpenAI 兼容

视频路由注册在 `internal/gateway/server/router.go:363-364`、素材库在 `internal/gateway/server/router.go:391`：

| 方法 | 路径 | 说明 | 证据 |
|------|------|------|------|
| `POST` | `/v1/contents/generations/tasks` | 创建视频生成任务 | `internal/gateway/server/router.go:363` |
| `GET` | `/v1/contents/generations/tasks/:task_id` | 查询任务状态与结果 | `internal/gateway/server/router.go:364` |
| `POST` | `/v1/assets/:action` | 素材库统一入口（10 个 Action） | `internal/gateway/server/router.go:391` |

**关键结论：路径直接照搬火山方舟 v3 原生协议**（`/api/v3/contents/generations/tasks`），只是在前面挂到 `/v1` 下。这与 OpenAI 的 Sora 风格 `/v1/videos`、`/v1/videos/{id}`、`/v1/videos/{id}/content` **不兼容**——调用方 SDK（OpenAI Python/JS 的 `client.videos`）无法直接使用。

对比之下，LiteLLM 选择「对齐 OpenAI `/v1/videos`」作为统一对外形状，New API 选择「自研统一 task 端点」。本网关是三家中唯一把**上游厂商协议直接暴露为对外协议**的方案。

#### 请求体：直通，只改写 `model`

`internal/gateway/handler/video_handler.go:401-485`：

1. 读原始 body（上限 64 KiB，`video_handler.go:19` `maxVideoTaskBodyBytes`）；
2. **只解析 `model` 一个字段**（`video_handler.go:408-414`），其余字段完全不动；
3. 若 `model` 是模型组名，则经 group dispatcher 换成上游模型 ID 并**重写 body 的 `model` 字段**（`video_handler.go:439`，实现见 `rewriteVideoBodyModel` `video_handler.go:488-499`，用 `map[string]json.RawMessage` 只换 model，保留其余字段原文）；
4. 原始 body（除 model 外）原样 POST 给上游（`video_handler.go:456`）。

```jsonc
// 调用方发（POST /v1/contents/generations/tasks）
{
  "model": "video-group",              // 对外模型组名
  "content": [{"type": "text", "text": "test"}],
  "resolution": "720p",
  "duration": 5
}
// 网关改写后发给上游
{ "model": "doubao-seedance-2-0-260128", ... }   // 其余字段逐字保留
```

#### 响应体：创建只回 `{"id": ...}`，查询原样透传

- 创建：`video_handler.go:484` 只返回 `{"id": taskID}`，**上游返回的完整任务对象被丢弃**（上游 body 仅用于解析 `id`，见 `internal/providers/seedance/relay/adapter.go:66-73`）。LiteLLM/New API 创建接口通常会回完整的任务对象（含 status）。
- 查询：`video_handler.go:557` `c.Data(http.StatusOK, "application/json", result)` 直接把上游 JSON 透传给调用方，网关不改写形状。

### 1.2 鉴权与横切能力：共享鉴权/埋点，但**被排除在预算与配额之外**

`/v1` 路由组的中间件链（`internal/gateway/server/router.go:275-298`）：

| 中间件 | 是否作用于视频 | 证据 |
|--------|----------------|------|
| `AuthMiddleware`（Key 校验） | ✅ | `router.go:276` |
| `BillingFactGuard` | ✅ | `router.go:277` |
| `TraceCapture`（正文捕获/trace） | ✅ | `router.go:279` |
| `UsageCollectorWithRedis`（call_logs + 预算核销 hook） | ✅ | `router.go:298` |
| `ProviderTracker`（并发/限流观测） | ✅ | `router.go:302` |
| `OrgCheckMiddleware` | ✅ | `router.go:304` |
| **预算预扣 / 预算 Gate** | ❌ **未挂** | 只挂在 chat/embeddings/rerank/messages/responses：`router.go:186-190`、`router.go:209-213` |
| **四层配额（RequestQuotaGate）** | ❌ **未挂** | `router.go:237-241` |
| **模型层配额（ModelQuotaGate）** | ❌ **未挂** | `router.go:251-255` |

也就是说：**视频任务要过鉴权、要进 call_logs、要进 Trace、要进组织校验，但既不预扣预算、也不受请求/模型层配额限制。** 对视频这种高单价、长耗时、易滥用的能力来说，这是一个明显的风控缺口。

#### 计费：成本恒为 0

- `UsageCollector` 只跳过 `/health`、`/ready`、`/v1/models`、`/v1/model/info`、`/v1/health/models`（`internal/gateway/middleware/usage_collector.go:37-41`），视频路径**会**产生 call_logs；
- 视频在 context 里写的 model 是带后缀的：创建 `组名:CreateTask`（`video_handler.go:448`）、查询 `组名:GetTask`（`video_handler.go:528`）、素材 `seedance-asset:<Action>`（`asset_handler.go:133`）——即**用量可观测，但用于"按次计费"的语义不完整**（同一任务创建+多次轮询会记多条调用明细）；
- 成本计算 `EstimateCost`（`internal/gateway/observability/collector.go:623-665`）只有两条路径：**token 计价**与 **rerank 的 `SearchUnits × RerankPerSearchUnit`**。视频请求没有 token usage，也没有命中任何单价字段，因此 `EstCostUSD = 0` 且大概率 `UsageMissing = true`。

**一个很讽刺的细节**：定价类型 `core.ModelPricing` **已经预留了按次/按秒字段**——`PerRequest`、`PerSecondInput`、`PerSecondOutput`、`PerImage`（`internal/core/types.go:255-261`），但 `EstimateCost` 从未使用 `PerSecond*`/`PerRequest`。同时：

- 内嵌的 LiteLLM catalog（`internal/controlplane/seed/litellm_catalog.json`）**已经包含 25 个 `mode: video_generation` 条目**，且**带视频价格字段** `output_cost_per_video_per_second`（如 `sora-2` = 0.1 USD/s、`sora-2-pro` = 0.3 USD/s，源 `https://platform.openai.com/docs/api-reference/videos`）；
- 但 catalog 落库时，解析器**只映射 token 类价格**（`internal/controlplane/service/catalog_parser.go:104-118` 只读 `input_cost_per_token`/`output_cost_per_token`/cache 类；`model_catalog.go:33-39` 也只有这些列），`output_cost_per_video_per_second` **没有结构化列**——它只作为原始 JSON 保留在 `RawPayload` 里（`catalog_parser.go:89`），**没有任何代码消费它**；
- `models` 表也只有 token 价格列（`internal/controlplane/model/model.go:38-41`），没有视频价格列。

**结论：视频能力目前是"免费 + 不可计费"的。**（下一章的 LiteLLM 正是把 `output_cost_per_video_per_second` 用于计费的。）

### 1.3 模型组调度：复用 chat 的 dispatcher，但无 fallback

```
对外 model
  └─ VideoHandler.CreateTask
       ├─ resolveProviderType(model) → providers.Router.GetProviderType(model)   video_handler.go:377-397
       └─ groupDispatch.Dispatch(ctx, model, nil)                                video_handler.go:423-445
            → decision{ModelName(上游模型ID), ChannelType(providerType), ProviderName}
       └─ rewriteVideoBodyModel(body, upstreamModel)                             video_handler.go:439
  └─ adapterReg.Get(providerType) → video.VideoProvider                          video_handler.go:454
```

- group dispatcher 与 chat 共用同一个 `GroupAwareDispatcher`（`internal/gateway/server/router.go:123-127`），所以**模型组、渠道权重、渠道绑定这套基建视频是复用的**——这是当前设计做得对的地方；
- 但视频**只取一个 decision 就直接转发**，没有 chat 链路里的 fallback 候选链、重试、熔断（`internal/fallback/`、`internal/llmclient/circuit_breaker.go` 均未参与视频路径）。上游 5xx 直接 `502` 给调用方（`video_handler.go:457-460`）。

### 1.4 适配器层：两个硬编码槽位 + 直通式接口

接口极简（`internal/core/video/interfaces.go`）：

```go
type VideoProvider interface {
    CreateTask(ctx, req json.RawMessage) (taskID string, err error)  // 请求直通，只取 id
    GetTask(ctx, taskID string) (json.RawMessage, error)             // 响应直通
}
type AssetProvider interface {
    DoAction(ctx, action string, req json.RawMessage) (json.RawMessage, error)
}
```

| 实现 | 路径前缀 | 鉴权 | 证据 |
|------|----------|------|------|
| `seedance-relay`（景汐中转） | `{base}/seedance/api/v3/contents/generations/tasks`；素材 `{base}/seedance-assets/{Action}` | `Authorization: Bearer <api_key>` | `internal/providers/seedance/relay/adapter.go:42,48,78,106` |
| `seedance-volcengine`（火山方舟官方） | `{base}/api/v3/contents/generations/tasks` | `api_key` → Bearer；`aksk` → HMAC 签名（`Signer`） | `internal/providers/seedance/volcengine/adapter.go:86-98,106-107,145-146`；`creds.go`、`signer.go` |

**扩展性问题（第 4 章会展开）**：适配器注册表是**两个具名槽位**而不是按 providerType 的 map——

```go
type VideoAdapterRegistry struct { relay, volcengine video.VideoProvider; relayName, volcName string }
func (r *VideoAdapterRegistry) Get(providerType string) video.VideoProvider {
    switch providerType {
    case "seedance-volcengine": return r.volcengine   // 其余一切 → relay
    }
    ...
}
```

见 `internal/gateway/handler/video_handler.go:79-160`。任何新增视频厂商（Sora/Veo/Kling/Runway）都必须改这个 struct、`Swap`、`Get`、`buildSeedanceAdapters`（`internal/gateway/server/seedance_adapters.go:20-48`）以及 `GetTask` 里的默认值 `providerType := "seedance-relay"`（`video_handler.go:505`）。

### 1.5 异步任务状态管理：**无服务端推进，纯客户端轮询**

这是与 New API 差异最大的一环。

**task_id 来源**：网关**不生成** task_id，直接以上游返回的 `id` 作为主键——`internal/controlplane/model/video_task.go:11-12` 注释写得很明确：「视频任务记录（用量统计用，非 task_id 映射表）主键为上游原始 task_id」。

**双层存储**：

| 层 | 实现 | 职责 | 证据 |
|----|------|------|------|
| 热缓存 | `RedisVideoTaskStore` | Redis hash `video_task:{task_id} → {provider_type, model, upstream_model, ...}`，TTL 30 天 | `video_handler.go:166-253`；`router.go:333` |
| 权威存储 | 控制面 PG `video_tasks` + `video_task_events` | 用量/详情/时间线 | `internal/gateway/handler/video_task_cp_adapter.go`；`internal/gateway/infra/video_task_client.go:59-85` |
| 组合 | `CompositeVideoTaskStore` | 写时双写，读时"先 Redis 后 CP" | `video_handler.go:264-322`；`router.go:336-342` |

**写入路径**：

- 创建成功后**异步 goroutine** 落库（`video_handler.go:477-481`，`go func(){ taskStore.Record(...) }()`）——即**落库失败不影响返回给调用方**（只有一条 Warn 日志）；
- 查询时，每次把上游返回的 `status`/`video_url`/`error` **异步回写** PG（`video_handler.go:540-555`，字段提取见 `extractUpstreamTaskDetails` `video_handler.go:565-593`）。

**读取路径（关键）**：`GET /contents/generations/tasks/:task_id` 的每次调用都**实时回源上游**：

```
task_id → taskStore.LookupTaskMeta()（先 Redis，miss 再控制面 HTTP）→ providerType
        → adapterReg.Get(providerType).GetTask(ctx, taskID)   ← 每次都真的打上游
        → 原样返回 + 异步回写 PG
```
证据：`video_handler.go:502-558`。

**因此**：

- ❌ 没有服务端轮询 worker（对比 New API 的统一 task 推进）；
- ❌ 没有上游 callback/webhook 接收端点（grep 全仓 gateway 无相关路由）；
- ❌ 没有任务列表 / 取消 / 删除接口，没有视频内容代理下载（`video_url` 指向上游或 CDN，由调用方直连取回）；
- ⚠️ Redis 或控制面不可用时 `LookupTaskMeta` 失败会**静默降级到默认 `seedance-relay`**（`video_handler.go:505-526`）——如果任务是 volcengine 渠道创建的，此时会拿着 relay 的凭证去查一个不存在的任务 id。

### 1.6 素材库：有，但只有一个渠道、无法按模型路由

- 单一入口 `POST /v1/assets/:action`，10 个 Action **大小写敏感、原样透传**给上游（`asset_handler.go:114-144`）——`CreateVisualValidateSession`、`GetVisualValidateResult`、`CreateAssetGroup`、`CreateAsset`、`GetAsset`、`ListAssets`、`UpdateAsset`、`GetAssetGroup`、`UpdateAssetGroup`、`DeleteAsset`（`docs/seedance-api.md` 第 2 章 / 第 6 章有全量字段说明）。
- **默认且实际只路由到 `seedance-relay`**（`asset_handler.go:95` 默认值；`SetRouting` 在热加载时也只被 relay 名调用——`router.go:380`）；volcengine 的 `DoAction` 实现存在（`volcengine/adapter.go:176`）但默认不参与路由。
- 素材请求体里**没有 `model` 字段**，因此无法复用模型组做渠道选择——这与视频生成路径（走 group dispatcher）不一致。

> 「素材库 + 真人认证 + 素材组」这一整套是 **LiteLLM 与 New API 都没有的能力**，属于本网关相对同类的差异化优势，但它被绑死在单一中转渠道上。

### 1.7 配置热加载

- 启动时从 Redis provider snapshot 构建适配器（`router.go:255-264`，`buildSeedanceAdapters` `seedance_adapters.go:20-48`）；凭证/endpoint 缺失时**用占位凭证兜底而不是启动失败**（`seedance_adapters.go:35,41-46`）；
- `config:change` → `ProviderLoader` reconcile → `reloadSeedance` → `AdapterRegistry.Swap` + `AssetAdapterRegistry.Swap`（`router.go:374-386`），`RWMutex` 保护（`video_handler.go:98-116`）。也就是说改渠道配置**不需要重启网关**。

### 1.8 目录与能力元数据：有元数据，但没有价格

- 本网关的 catalog 同步自 LiteLLM（`internal/controlplane/service/catalog_sync_service.go:67` 默认拉 `raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`），内嵌快照 `internal/controlplane/seed/litellm_catalog.json` **已包含 25 个 `mode: video_generation` 条目**：`openai/sora-2`、`sora-2-pro`、`sora-2-pro-high-res`、`azure/sora-2*`、`gemini/veo-2.0/3.1-*`、`vertex_ai/veo-*`、`runwayml/gen4_turbo|gen4_aleph|gen3a_turbo`，每条含 `supported_resolutions` 与 `output_cost_per_video_per_second`。
- 模型注册表能识别视频类别：`modeToCategory["video_generation"] = CategoryVideo`（`internal/core/types.go:205`），`CategoryVideo` 参与分类缓存与展示名（`internal/providers/registry.go:650,729`）。
- `models` 表补充了视频规格列：`video_supported_resolutions`、`video_supported_aspect_ratios`、`video_min/max_duration_seconds`、`video_supported_actions`（`atlas-migrations/20260710170000_add_models_video_params.sql`）。
- **但没有任何视频价格列**（catalog 表与 models 表都只有 token 价）。

**配置面与执行面脱节的风险（已修正，勿按"既成事实"理解）**：早先版本的本报告曾断言"`/v1/models?category=video` 会把这些视频模型列出来"——**该断言不成立**，已核验并更正如下：

- `/v1/models` 的模型来自 **provider registry**（由渠道/模型配置构建），**不是** `model_catalog`；catalog 条目不会自动变成可调用模型，需要管理员手动建 `model` 行并关联 catalog（`AGENTS.md` 的 `model_catalog → model → group` 链路；`CatalogSyncService` 只有"把已有 model 绑定到匹配 catalog 条目"的 `ListAutoBindCandidates`/`ApplyAutoUpdates`，**没有** catalog→model 的自动创建）。
- 因此准确表述是：**catalog 里躺着 25 条跑不通的视频模型与 0 价格的记录**；一旦管理员照着 catalog 建 `model` + 组，就会对外暴露一个"可列出但不可执行"的视频模型——`POST /v1/contents/generations/tasks` 会因 `GetProviderType` 无匹配而 fallback 到 `seedance-relay`（`video_handler.go:140-160`、`:454`），拿着 Seedance 协议体去打一个不支持它的上游。**这是"配置面可引导管理员犯错"的风险，而非当前已对外暴露的事实。**
- 另需注意：价格并非"完全没进来"——`RawPayload` 保留了 LiteLLM 条目的完整 JSON（`catalog_parser.go:89`），所以 `output_cost_per_video_per_second` 在 DB 里是可得的，只是没有映射到结构化列、也没有任何代码消费它。

### 1.9 关键文件清单

| 路径 | 职责 |
|------|------|
| `internal/core/video/interfaces.go` | `VideoProvider` / `AssetProvider` 接口（直通式） |
| `internal/gateway/handler/video_handler.go` | 视频创建/查询、适配器注册表、双层任务存储（631 行） |
| `internal/gateway/handler/asset_handler.go` | 素材库统一入口与分发（145 行） |
| `internal/gateway/handler/video_task_cp_adapter.go` | `VideoTaskClient` → 存储接口适配（72 行） |
| `internal/gateway/server/seedance_adapters.go` | 从 provider snapshot 构建两个适配器 |
| `internal/gateway/infra/video_task_client.go` | 网关 → 控制面内部 API（不直连 DB） |
| `internal/providers/seedance/relay/adapter.go` | 景汐中转适配器（视频 + 素材） |
| `internal/providers/seedance/volcengine/{adapter,creds,signer}.go` | 火山方舟官方适配器（Bearer / HMAC） |
| `internal/controlplane/model/video_task.go` | `video_tasks` + `video_task_events` 模型 |
| `internal/controlplane/handler/video_task_handler.go` | 管理面 + 内部 API（列表/统计/趋势/分布） |
| `atlas-migrations/20260710150000_create_video_tasks_table.sql` 等 | 建表与详情字段、视频规格字段 |
| `docs/seedance-api.md` | 对外对接文档（903 行，12 个接口全量） |

---

## 2. LiteLLM 的视频方案

> 证据基线：本机克隆 `/Users/jsy/Public/self_code/litellm`，`v1.84.0-rc.1-1180-g9cac0471ae`（commit `9cac0471ae`，2026-05-27）。以下 `文件:行号` 均指该 clone。
> 抽样核验：`litellm/llms/*/videos` 目录、`litellm/proxy/schema.prisma` 无视频表、`litellm/llms/volcengine/` 仅 `chat/embedding/responses`、按秒成本函数 —— 由本文作者独立复核通过。

### 2.1 起点与定位

视频支持起始于 commit `0f9996a4d0`（2025-10-24），首个 stable tag **v1.79.0-stable**（2025-11-01）；此后 Gemini/Vertex Veo（v1.79.3）、RunwayML（v1.80.5）、characters/edits/extensions（v1.83.3）逐步补齐。**它把"OpenAI Sora 的 `/v1/videos` 契约"当作统一对外协议**，与 New API 的"自研格式为主"形成对照。

### 2.2 端点矩阵（9 组）

| 方法 | 路径 | handler | 证据 |
|------|------|---------|------|
| POST | `/v1/videos` | `video_generation` | `litellm/proxy/video_endpoints/endpoints.py:32-44` |
| GET | `/v1/videos` | `video_list` | `endpoints.py:118-130` |
| GET | `/v1/videos/{video_id}` | `video_status` | `endpoints.py:203-215` |
| GET | `/v1/videos/{video_id}/content` | `video_content` | `endpoints.py:303-315` |
| POST | `/v1/videos/{video_id}/remix` | `video_remix` | `endpoints.py:412-424` |
| POST | `/v1/videos/characters` | `video_create_character`（multipart） | `endpoints.py:517-534` |
| GET | `/v1/videos/characters/{character_id}` | `video_get_character` | `endpoints.py:626-638` |
| POST | `/v1/videos/edits` | `video_edit` | `endpoints.py:734-746` |
| POST | `/v1/videos/extensions` | `video_extension` | `endpoints.py:836-848` |

**没有** `/v1/video/generations` 别名，**没有** `DELETE /v1/videos/{id}` 路由（虽然基类有 `transform_video_delete_request`，`litellm/llms/base_llm/videos/transformation.py:223`）。

请求体不做 pydantic 强校验，按 `VideoCreateOptionalRequestParams` 白名单过滤（`litellm/types/videos/main.py:68-89`）：`prompt`(必填) / `model` / `input_reference` / `image`(gcsUri·bytesBase64Encoded) / `parameters`(厂商直传) / `seconds` / `size` / `characters` / `user` / `extra_headers` / `extra_body`。注意它**保留了 `parameters` 这个厂商私有参数直传口**——与本网关"整体直通"不同，是"统一字段 + 厂商逃生舱"的折中。

响应体 `VideoObject`（`litellm/types/videos/main.py:8-23`）：`id` / `object="video"` / `status` / `created_at` / `completed_at` / `expires_at` / `error` / `progress` / `remixed_from_video_id` / `seconds` / `size` / `model` / `usage`。`status` **没有枚举类型**（就是 `str`），跨 provider 取值不完全一致（公共集合 `queued`/`processing`/`completed`/`failed`）。

### 2.3 无状态的任务 id 编码（很值得借鉴的一招）

LiteLLM 不落库，却要在查询时知道"这个任务属于哪个 provider"，解法是把 provider 信息**编码进 id**：

```
video_<base64("litellm:custom_llm_provider:{provider};model_id:{model};video_id:{id}")>
```
`litellm/types/videos/utils.py:15,42-64`；解码用于 status/content 路由（`litellm/videos/main.py:1046-1049`）。

**对照本网关**：本网关用 Redis `video_task:{id}` + PG `provider_type` 反查（`internal/gateway/handler/video_handler.go:509-526`）。两者都解决了同一问题，但 LiteLLM 的方案**完全无状态、无 TTL 失效风险**；本网关的方案多了一次 Redis/CP 依赖，且 Redis key 过期后 `routed_model` 就丢了。代价是 LiteLLM 的 id 不可读、且必须由网关自己生成（放弃"直接沿用上游 id"的便利）。

### 2.4 provider 覆盖：只有 5 家，且**没有火山/Seedance**

真相来源 `litellm/utils.py:9194-9218`（只有 5 个分支，其余 `return None`）：

| Provider | 代表模型 | 证据 |
|----------|----------|------|
| `openai` | `openai/sora-2`、`sora-2-pro`、`sora-2-pro-high-res` | `utils.py:9198`；`litellm/llms/openai/videos/transformation.py` |
| `azure` | `azure/sora-2*` | `utils.py:9202`；`litellm/llms/azure/videos/transformation.py:78-86` |
| `gemini` | `gemini/veo-2.0-generate-001`、`veo-3.1-*` | `utils.py:9206`；`.../v1beta/models/{m}:predictLongRunning` |
| `vertex_ai` | `vertex_ai/veo-2.0/3.0/3.1-*` | `utils.py:9210`；`vertex_ai/videos/transformation.py:217-223,395-409` |
| `runwayml` | `runwayml/gen3a_turbo`、`gen4_turbo`、`gen4_aleph` | `utils.py:9214` |

**不支持**：Bedrock Nova Reel（`litellm/llms/bedrock/` 无 `videos/`）、**火山方舟/Volcengine/Doubao/Seedance**（`litellm/llms/volcengine/` 只有 `chat/embedding/responses`）、Kling/Vidu/Luma/MiniMax。

> 旁证：LiteLLM 的 `model_prices_and_context_window.json` 里 `mode == "video_generation"` 共 25 条，全部属于这 5 家 —— **而这份文件正是本网关 catalog 的同步源**（`internal/controlplane/service/catalog_sync_service.go:67`）。也就是说：本网关的 catalog 里**已经同步进来了 Sora/Veo/Runway 的视频模型与按秒单价**，但执行侧一个都接不了、价格也全部丢弃（见 §1.2 / §1.8）。

### 2.5 异步模型：单次 POST + 客户端轮询，**无任何服务端状态**

- `video_generation` 只发一次 POST 就返回（`litellm/llms/custom_httpx/llm_http_handler.py:6116`），视频代码里**没有** `while True` / `asyncio.sleep` 轮询循环；Runway 的实现注释直白写着 "Client must poll or wait for task completion"（`runwayml/videos/transformation.py:41`）。
- **无任务持久化**：`grep -ni video litellm/proxy/schema.prisma` → 0 命中；`litellm/proxy/video_endpoints/` 内无 DB 调用。`LiteLLM_ManagedObjectTable` 的注释明确只服务 batch/fine-tune。
- **无后台 worker、无 webhook**（`grep -rni "webhook|callback" litellm/proxy/video_endpoints/ litellm/videos/` → 0）。
- 有 `GET /v1/videos` 列表接口，但它是**直接向上游列表接口转发**，不是查本地表。

**这是本网关与 LiteLLM 最像的一点**：两者都是"转发 + 客户端轮询"。差别在于本网关至少还落了一张 `video_tasks` 台账（尽管落库是异步 fire-and-forget），LiteLLM 什么都不留。

### 2.6 计费：按秒，且已处理"分辨率分级"

```python
_VIDEO_CALL_TYPES = {create_video, acreate_video, video_remix, avideo_remix}   # cost_calculator.py:132-139
→ video_generation_cost(...)                                                   # cost_calculator.py:1400-1447
→ litellm/llms/openai/cost_calculation.py:181-227
```

- 单价优先取 `output_cost_per_video_per_second`，否则 `output_cost_per_second_<resolution>` → `output_cost_per_second`（`_video_output_cost_per_second`，`cost_calculation.py:156-178`）——**分辨率分级的按秒定价**。
- duration 取自 `usage.duration_seconds`；**取不到就传 0.0，静默记 0 成本**（`cost_calculator.py:1419-1448`）。
- ModelInfo 里的视频字段：`input/output_cost_per_video_per_second`、`input/output_cost_per_video_token`，**没有"按次"字段**（`litellm/types/utils.py:351-407`）；要按次只能用透传端点的 `cost_per_request`（`litellm/proxy/_types.py:2255-2258`）。
- 预算/限流**复用通用链路**：每个视频端点都 `Depends(user_api_key_auth)`（`endpoints.py:34,120,205,305,414,519,628,736,838`）→ key/team/org/user 预算检查（`proxy/auth/auth_checks.py:522,625-700`）→ 走 `llm_router` 统一分派，享 deployment 选择与 rpm/tpm 限制。**没有视频专属限流**，但至少**没有豁免**。

**对照本网关**：本网关的视频**完全不在计费与配额链路上**（§1.2）。LiteLLM 至少做到了"进预算、按秒计价"，只是不落任务。

### 2.7 未原生支持的厂商怎么办：passthrough

`general_settings.pass_through_endpoints`（`PassThroughGenericEndpoint{path,target,headers,include_subpath,cost_per_request,auth,guardrails}`，`litellm/proxy/_types.py:2234-2264`）可把任意厂商挂进来，另有内置的 `/bedrock/{path}`、`/gemini/{path}`、`/vertex_ai/{path}`。好处是**能接一切**；代价是**没有 `VideoObject` 归一化、没有按秒成本**，只能按次 `cost_per_request`，且 Gemini/Vertex 的 passthrough 仍有专门的日志钩子补上视频成本（`vertex_passthrough_logging_handler.py:55-78`）。

### 2.8 LiteLLM 的限制清单（源码级）

仅 5 家 provider；无 DELETE；无 `/v1/video/generations` 别名；`characters`/`edits`/`extensions` **仅 OpenAI 可用**（gemini/vertex/runway 的实现均抛 `NotImplementedError`）；无 streaming；**无任务持久化/无 webhook**；status 跨 provider 不一致；成本依赖 duration 缺失即 0；仅 create/remix 计费；无按次价格；无视频专属限流。

---

## 3. New API / one-api 的视频方案

> 证据基线：本机克隆 `/Users/jsy/Public/self_code/new-api`（`v0.11.0-alpha.9-1-g982dc5c5`，2026-02-25）与 `/Users/jsy/Public/self_code/one-api`（`v0.6.11-alpha.22-1-g8df4a26`，2025-02-21）。
> 抽样核验（作者独立复核通过）：视频路由表、`service/task_polling.go` 的 15s 循环与超时退款、`model/task.go` 状态枚举与 `PerCallBilling` 字段、`dto/openai_video.go`、`relay/helper/price.go` 按次定价；并**修正**了下文 §3.1 的一处结论。

### 3.1 对外端点：双协议并存 + 厂商原生路径

全部集中在 `router/video-router.go`：

| 方法 | 路径 | 说明 | 证据 |
|------|------|------|------|
| POST | `/v1/video/generations` | **自研统一入口**（主推） | `router/video-router.go:23` |
| GET | `/v1/video/generations/:task_id` | 自研查询 | `:24` |
| POST | `/v1/videos` | **OpenAI Sora 兼容**（注释直引 OpenAI 文档） | `:27-30` |
| GET | `/v1/videos/:task_id` | OpenAI 兼容查询 | `:31` |
| POST | `/v1/videos/:video_id/remix` | remix | `:25` |
| GET | `/v1/videos/:task_id/content` | **内容反向代理直出字节流** | `:16`、`controller/video_proxy.go:29` |
| POST | `/kling/v1/videos/text2video`、`/image2video`（+GET） | **厂商原生路径** + `KlingRequestConvert()` | `:38-41` |
| POST | `/jimeng/` | 即梦原生路径 + `JimengRequestConvert()` | `:45-50` |

**与本网关的关键差别**：New API 用「统一 `TaskSubmitReq` + 每个 adaptor 做协议转换」的方式，把**同一份 task 记录投影成三种对外形状**（自研 / OpenAI / 厂商原生）。

请求体 `TaskSubmitReq`（`relay/common/relay_info.go:643-654`）：`prompt`(必填) / `model` / `mode` / `image` / `images` / `size` / `duration`(int) / `seconds`(string) / `input_reference` / `metadata`(自由 map)。**`metadata` 是厂商私有参数的唯一通道**，adaptor 直接把它反序列化进自己的强类型结构（`relay/channel/task/taskcommon/helpers.go:16-28`）。

**⚠️ 修正（作者核验）**：调研草稿称创建响应 `status` 由 `NewOpenAIVideo()` 兜底为 `queued`。实际源码 `dto/openai_video.go:41-46` 的 `NewOpenAIVideo()` **只设置 `Object: "video"`，不设置 `Status`**；而 doubao adaptor 的 `DoResponse` 只写 `ID/TaskID/CreatedAt/Model`（`relay/channel/task/doubao/adaptor.go:172-178`）——**因此创建响应的 `status` 是空字符串 `""`，不是 `queued`**。下游若按 `status` 分支需自行兜底。（另外 `dto/video.go:3-47` 里的 `VideoResponse{task_id,status}` 等结构在该仓库视频链路中**无任何读写方**，仅 swagger 用，别被它误导。）

### 3.2 统一 task 平台

```
relay/channel/task/
├── ali  doubao  gemini  hailuo  jimeng  kling  sora  suno  vertex  vidu   ← 10 个视频 + 1 个音频(suno)
└── taskcommon (metadata 反序列化、BaseBilling 空实现、进度常量)
```

`Task` 表（`model/task.go:44-66`）关键字段：`TaskID`(对外 `task_<32位随机>`)、`Platform`(渠道类型数字的字符串)、`UserId`、`Group`(按提交时分组定价)、`ChannelId`、`Quota`、`Action`、`Status`、`Progress`、`Properties`(含 `upstream_model_name`)、`PrivateData`(含 `UpstreamTaskID` / `ResultURL` / `Key` / `TokenId` / `BillingContext`)、`Data`(上游原始响应快照)。

**状态机**：

| 内部（`model/task.go:34-42`） | 对外（`ToVideoStatus()`，`:17-32`） |
|---|---|
| `QUEUED` / `SUBMITTED` | `queued` |
| `IN_PROGRESS` | `in_progress` |
| `SUCCESS` | `completed` |
| `FAILURE` | `failed` |
| `NOT_START` / `UNKNOWN` | `unknown` |

**双 ID 设计是它最值得抄的一点**：对外 id 由网关生成、上游 id 藏在 `PrivateData.UpstreamTaskID`（`model/task.go:122-127`），因此换渠道/换厂商对调用方透明，且不泄漏上游 id。

### 3.3 状态推进：服务端 15s 轮询（三家里唯一）

| 参数 | 值 | 证据 |
|------|-----|------|
| 启动条件 | `common.IsMasterNode && constant.UpdateTask`（`UPDATE_TASK` 默认 true） | `main.go:124-130`、`common/init.go:138` |
| **轮询间隔** | **15 秒**（固定 `time.Sleep`） | `service/task_polling.go:93` |
| 批量 | `TASK_QUERY_LIMIT` 默认 **1000** | `common/init.go:147`；消费 `service/task_polling.go:97` |
| 超时清扫 | **100 条/轮** + **CAS** 置失败并**退款** | `service/task_polling.go:41-88` |
| 任务超时 | `TASK_TIMEOUT_MINUTES` 默认 **1440（24h）**，`<=0` 关闭 | `common/init.go:149` |
| 并发度 | **1**（每渠道内串行 `for`，未用 gopool） | `service/task_polling.go:334-338` |
| 幂等/防竞态 | `UpdateWithStatus(fromStatus)` CAS（`WHERE status = ?`）；批量更新显式警告"**NO CAS guard … DO NOT use in billing/quota lifecycle flows**" | `model/task.go:410-416`、`:418-430` |

**客户端查询默认不回源上游**（与 LiteLLM/本网关相反）：`videoFetchByIDRespBodyBuilder` 先读 DB（`relay/relay_task.go:363`），只有 Gemini/Vertex 会实时回源（`tryRealtimeFetch` 首行类型闸门 `:420-422`）。

**代价（New API 自己的短板）**：状态最多滞后 15s；单协程串行轮询在任务量大时成为吞吐瓶颈；doubao adaptor 的 `default` 分支把未知状态当 `IN_PROGRESS`（`relay/channel/task/doubao/adaptor.go:278-281`），一旦上游改状态字面量，任务会**卡到 24h 超时才失败退款**。

### 3.4 计费：按次为主 + 预扣 + 终态差额结算

- **按次定价**：`ModelPriceHelperPerCall` → `quota = modelPrice × QuotaPerUnit × groupRatio`（`relay/helper/price.go:143-174`），取不到配置时**兜底 0.1 USD**。
- **按次 vs 按倍率**：环境变量 `TASK_PRICE_PATCH` 列出的模型走**纯按次**（`OtherRatios` 不参与），未列出的则**固定价 × 各 OtherRatios 连乘**（`relay/relay_task.go:190-197`、`common/init.go:151-158`）。
- **哪些适配器真的会因分辨率/时长变价**：只有 3 个实现了 `EstimateBilling` —— **sora**（`seconds` + `size`，`1792x1024` 系数 1.666667）、**ali**（`seconds` + 分辨率系数）、**vertex**（`sampleCount`）；**doubao/Seedance 继承空实现，`480p/720p/1080p`、`5s/10s` 不影响价格**（`relay/channel/task/taskcommon/helpers.go:80-95`）。要差异化定价只能拆模型名或走 `TASK_PRICE_PATCH`。
- **流程**：提交时 `ForcePreConsume=true` → `PreConsumeBilling` 预扣（`relay/relay_task.go:199-205`）→ 提交成功即写消费日志并记全额 quota（`controller/relay.go:557-575`，**任务还没跑完**）→ 轮询到终态做差额结算（`service/task_polling.go:522-539`）：`PerCallBilling` 跳过 / adaptor 额度 / `TotalTokens>0` 按 token 重算（仅当模型配了 `model_ratio`，`service/task_billing.go:244-285`）/ 否则保持预扣 → **失败或超时全额退款**（`RefundTaskQuota`，`service/task_billing.go:145-175`，退款覆盖钱包/订阅/令牌额度）。

### 3.5 火山方舟 Doubao / Seedance 的接法（与本网关最可直接对照）

| 维度 | New API | 本网关 |
|------|---------|--------|
| 上游路径 | `{base}/api/v3/contents/generations/tasks[/{id}]`（`relay/channel/task/doubao/adaptor.go:111-113`、`:189`） | relay: `{base}/seedance/api/v3/...`；官方: `{base}/api/v3/...` |
| 鉴权 | **仅 Bearer**（`:116-121`） | relay Bearer；官方 **Bearer 或 AK/SK HMAC 签名**（`volcengine/signer.go`） |
| 请求转换 | 统一 `TaskSubmitReq` → `content[]`，`metadata` 整体反序列化覆盖 | **不转换**，原样透传，仅改写 `model` |
| 模型清单 | 硬编码 4 个（1.0 pro/lite、1.5 pro），**不含 Seedance 2.0** | 不硬编码，由 catalog/渠道配置决定（已支持 `doubao-seedance-2-0-260128`） |
| 状态映射 | pending/queued→10%、processing/running→50%、succeeded→100%、failed→100%、**其他→30% 且视为进行中** | 网关不映射，原样返回上游 status；仅落库时按 `status\|state\|task_status` 抽取 |
| 失败原因 | **硬编码 `"task failed"`，丢弃上游 `error.message`**（`:277`） | 从 `error_message\|error.message\|error` 抽取并落库 |
| 调用方看到的形状 | 统一 `OpenAIVideo`（status 空串，见 §3.1 修正） | 创建 `{"id"}`、查询原样透传上游 JSON |

有意思的对照：**在 Seedance 这一家上，本网关的适配质量更高**（支持 2.0、支持 AK/SK 签名、失败原因透传、请求体不做有损转换），New API 则胜在"同一套 task 骨架接十家厂商"。

### 3.6 one-api：官方不支持视频

`grep -rn "video" router/*.go` **零命中**；relay mode 枚举无视频分支（`relay/relaymode/define.go:4-11`）；渠道类型仅 56 项且无视频渠道（`relay/channeltype/define.go:4-56`）；`model/` 下**没有 `task.go`**，无任何异步任务框架。

**修正一处常见误解**：one-api 也**没有**内置 Midjourney 支持（`grep -rn "midjourney" --include=*.go --include=*.md` 零命中）；`/mj/*` 整套是 **New API 的增量能力**（`router/relay-router.go:168-217`）。所以"New API = one-api + MJ + 视频 task 平台"这个理解是对的。

### 3.7 版本注记（实施前必须确认）

以上描述的是 **v0.11（Go adaptor）架构**。New API 上游已把任务适配器迁移为 **JS 任务插件**体系（`plugins/tasks/<key>/plugin.js`、`/v1/tasks/{pluginKey}`、`system_task` 表 + 租约），迁移点在 **v1.0.0-rc.27** 附近（v1.0.0-rc.26 仍有 `relay/channel/task/kling/adaptor.go`）。新旧差异：适配器语言（Go→JS）、计费表达（`model_price`→插件 `usageSchema` + `tier()`）、轮询（单协程→`system_task` 租约）、Seedance 模型（1.x→2.0/2.5 含分辨率/有声/参考视频差异化单价）、失败原因（硬编码→透传 `error.message`）。**若按新版实施，上表若干行需重新核对。**

---

## 4. 差异对比：三条本质分歧轴

把三家的实现细节压缩后，真正的分歧只有三条：

### 轴 1：协议归一化发生在哪里

| 方案 | 归一化位置 | 收益 | 代价 |
|------|-----------|------|------|
| **本网关** | 不做归一化（只改 `model`） | 适配成本最低；新增厂商只要拼路径；上游新能力自动可用 | 调用方要按厂商语义写代码；OpenAI SDK 用不了；能力/校验/计费都无处挂载 |
| **New API** | 统一 `TaskSubmitReq` + adaptor 转换 | 一套骨架接 10 家；同一 task 投影成 3 种对外形状；可挂校验与计费 | 每个厂商要写转换 + 状态映射；字段映射有损（如 `size`/`seconds` 被忽略） |
| **LiteLLM** | 对齐 OpenAI `/v1/videos` + provider transformation | 直接吃 OpenAI 生态与 SDK；跨厂商字段统一 | 只能接"能被 OpenAI 契约表达"的厂商；对国内厂商无适配（实测 0 家） |

**对本网关的含义**：现在是"半原生"——既没有原生协议的完整承诺（文档只承诺了 12 个接口的一小部分字段），也没有兼容协议的红利。**要么补齐原生厂商的完整能力（参考视频/首尾帧/draft/回调），要么加一层 OpenAI 兼容投影**，二选一，别停在中间。

### 轴 2：任务生命周期归谁

| | 本网关 | LiteLLM | New API |
|---|--------|---------|---------|
| 状态推进 | 客户端每次查询回源 | 客户端每次查询回源 | **服务端 15s 轮询** |
| 客户端断连 | 台账永久停 `submitted` | 无台账，无影响也无记录 | 照常推进 + 结算/退款 |
| 上游查询量 | 与客户端轮询频率成正比 | 同 | **与客户端数量解耦**（未完成任务数/15s） |
| 超时兜底 | 无 | 无 | 24h 置失败 + 退款 |
| 终态计费 | 不可能（看不到终态） | 不可能 | 可能（差额结算/退款） |

**对本网关的含义**：`video_tasks` 表看起来像 New API 的 `tasks`，但**它只是"客户端查询的副作用记录"**：调用方不轮询 → 表里的 `status`/`video_url` 永远停在初始值，统计口径（平均耗时、成功率）因此系统性失真。要么承认它只是"路由反查缓存 + 审计台账"，要么引入服务端推进（控制面已有 cron 基础设施与 `video_tasks` 表，是成本最低的落点）。

### 轴 3：计费模型

| | 本网关 | LiteLLM | New API |
|---|--------|---------|---------|
| 计价维度 | 无 | 按秒（`output_cost_per_video_per_second`，支持分辨率分级） | 按次（`model_price`×groupRatio）+ 可选倍率 |
| 预扣 | 无 | 走通用 budget | `PreConsumeBilling` |
| 终态结算 | 无 | 无（代理侧只有成本日志） | 差额结算 + 失败/超时退款 |
| 数据基础 | **catalog 里已有单价数据**（同步自 LiteLLM，含 `sora-2` 0.1 USD/s） | 定价文件自带 | 管理面配置 |

**对本网关的含义**：这是三家里"投入产出比最高"的一块——价格数据**已经在库里**（`litellm_catalog.json` 的 25 条视频模型），类型系统**已经预留字段**（`PerRequest`/`PerSecondInput`/`PerSecondOutput`），唯一缺的是 `EstimateCost` 的一条分支 + catalog 同步映射两个字段。**现在是"有数据、有字段、没算法"，而且视频还绕过了预算与配额。**

### 4.4 反直觉结论

1. **本网关在 Seedance 上的适配质量高于 New API**（支持 2.0/2.5、AK/SK 签名、失败原因透传、无损直通），但**在厂商广度上被打成 1 : 10**。
2. **LiteLLM 的视频实现比想象中薄**：没有任务表、没有 worker、没有 webhook，只有 5 家 provider、9 个端点、一个按秒成本函数。它真正的价值是"**契约选择**"（占住 OpenAI `/v1/videos`）与"**id 内嵌 provider 实现无状态反解**"，而不是工程完备度。
3. **本网关唯一独有的能力是"素材库 + 素材组 + 真人认证"**（LiteLLM/New API 都没有）。这是差异化资产，但它被钉死在 seedance-relay 一个渠道上，且 `Action` 无白名单校验（可被路径拼接）。

---

## 5. 对本网关的演进建议

按"投入产出比 × 风险"排序。

### P0 —— 正确性与风控（建议优先做）

| # | 事项 | 依据 | 建议做法 |
|---|------|------|----------|
| 1 | **修 `GetTask` 越权读** | `video_handler.go:502-537` 不校验 `api_key_id`/`user_id`；管理面有 scope 隔离而对外接口没有 | 落库时已写入 `api_key_id`/`user_id`，查询时比对 context 中的 Key/用户，不匹配返回 404（勿返回 403，避免探测） |
| 2 | **消除"创建后立刻查询"的竞态** | 路由缓存是 `go func(){ Record(...) }()` 异步写（`video_handler.go:477-482`），客户端拿到 `id` 可能早于落库 → 反查落空 → **回落到 relay 默认适配器**（`:505`） | 至少把 Redis 路由缓存改成**同步写**（CP PG 可继续异步）；或参照 LiteLLM 把 provider 编码进对外 id |
| 3 | **给视频接上限流与配额** | 视频未挂 `SetRequestQuotaGate`/`SetModelQuotaGate`/`SetBudgetGate`（`router.go:186-255` 只注入 5 类 handler） | 最低限度：给 `POST /contents/generations/tasks` 挂请求层配额（按次），并纳入 `BillingFactGuard` 的计费路径白名单（`router.go:420-430`），避免"chat 被背压拒流、视频全量放行" |
| 4 | **素材 `Action` 白名单 + 转义 `task_id`** | `asset_handler.go:138` 直接拼接 action；`video_handler.go:503` 拼接 `task_id` | 用 10 个 Action 的常量集合做白名单；路径段用 `url.PathEscape` |

### P1 —— 计费（数据与字段已就绪，工程量最小）

| # | 事项 | 依据 |
|---|------|------|
| 5 | catalog 同步映射视频单价 | `catalog_parser.go:104-118` 只解析 token 类价格，`output_cost_per_video_per_second` 无结构化列（仅存于 `RawPayload`，`catalog_parser.go:89`）；数据源已含 25 条视频模型 |
| 6 | `EstimateCost` 增加按次/按秒分支 | `collector.go:623-665` 只有 token 与 rerank 两条路径；`core/types.go:255-261` 已有 `PerRequest`/`PerSecondInput`/`PerSecondOutput` |
| 7 | 视频记"按次成本"而非 0 | 操作类型已由 `call_logs.call_type` 承载（`video.create`/`video.get`/`video.asset.{Action}`），视频行可直接按 `call_type` 定位；只需在这条路径上挂一个 per-request 价格 |
| 8 | （若上服务端轮询）终态补结算 | 现无终态感知，无法做 New API 那种差额结算/退款 |

### P2 —— 架构与覆盖面

| # | 事项 | 参考 |
|---|------|------|
| 9 | 适配器注册表 `map[providerType]VideoProvider` 化 | 现在 `VideoAdapterRegistry{relay, volcengine}` + `default:` 兜底 relay（`video_handler.go:79-160`），是"新增 Sora/Veo/Kling"的最大结构阻碍（需同时改 `asset_handler.go`、`seedance_adapters.go`、`router.go` 解构） |
| 10 | 增加 OpenAI 兼容投影 `POST/GET /v1/videos` | 复用现有 `video_tasks` 做协议投影；New API 已经证明"一套 task、多种视图"可行 |
| 11 | 服务端推进任务（可选） | 控制面已有 cron 与 `video_tasks`：未终态任务定期回源上游 → 写回 + 触发计费。注意多副本需单例/租约（New API 用 `IsMasterNode`，新版用 `system_task` 租约） |
| 12 | 内容代理 `GET /v1/videos/{id}/content` | 现在直接把上游 `video_url` 给调用方（24h 有效），既泄漏直链也依赖上游有效期；LiteLLM/New API 都提供反代 |
| 13 | 素材库泛化 | 素材能力目前只在 relay 可达（`asset_handler.go:95`、`router.go:369,380`），且不支持按模型/渠道路由 |
| 14 | 统一"异步任务"抽象 | 视频用 `video_tasks`、批处理用 `NativeBatchProvider*`（`core/interfaces.go:36-75`）各自独立；New API 的统一 task 骨架更利于复用（超时扫描、CAS、退款、日志） |

### 5.4 三家各自"值得抄"的一条

- **抄 New API**：**对外 id 与上游 id 分离**（`PrivateData.UpstreamTaskID`）—— 换渠道/换厂商对调用方透明，且不泄漏上游 id。本网关现在直接沿用上游 id 作主键，一旦要迁移厂商就无法保持幂等。
- **抄 LiteLLM**：**把 provider 信息编码进 id**（`video_<base64>`）或无状态反解—— 可以彻底删掉"Redis 反查 + 默认回落"这段脆弱逻辑。
- **抄自己的**：**素材库/素材组/真人认证这一整套**—— 这是三家唯一无二的资产，应把它从"绑死 relay"里解出来，而不是继续当作旁路。

### 5.5 一个需要产品决策的问题

本网关视频接口的 `model` **必须是模型组名**（上游模型名直接 403 `direct_model_not_allowed`，`docs/seedance-api.md:108-118`）。这与 LiteLLM/New API「直接传真实模型名」的习惯相反。好处是强制走模型组授权与调度；代价是对接方必须先在管理面建组，**上手成本显著高于"填个 model 就能调"**。如果目标是吸引外部对接，这一点值得重新评估（例如：允许直连但按 Key 的 `access_all_groups` 白名单放行，或提供"自动建组"的引导流程）。

## 6. 已实施记录：视频日志补全（4 项）

对照第 2/3 节的三家做法，先把"视频日志是否记得全"这一档补齐（计费明确不做）。
**结论：台账层（`video_tasks`/`video_task_events`）本来就全，链路追踪层此前基本空白，本次补齐。**

| # | 事项 | 改动 | 落库位置 |
|---|------|------|----------|
| ① | 请求/响应正文 | `video_handler.go` 的 `CreateTask`/`GetTask`、`asset_handler.go` 的 `Handle` 主动登记：`middleware.CaptureTraceRequestBody`（新增，登记**改写前**的原始体，尊重 `log_enabled`）+ 复用既有的 `middleware.SetTraceResponseObject`（`trace_context.go`，同时置 `stream=false`） | `call_traces.request_body` / `response_body` |
| ② | 失败专项 | adapter 由字符串错误改为 `*core.GatewayError`（`core.ParseProviderError`：按状态码分类 + 提取上游 message + 保留原始 body）；handler 统一 `respondUpstreamFailure` 写 `error_message`。**对外仍是 502，不改对接方契约** | `call_traces.error_message` / `error_code` |
| ③ | 上游状态码 | `VideoProvider`/`AssetProvider` 接口增加上游 HTTP 状态码返回值（2 个 adapter + handler + 测试）；成功与失败都写 | `call_logs.upstream_status` |
| ④ | 操作类型标识 | 新增 `call_logs.call_type`（迁移 `20261014063200_add_call_logs_call_type.sql`，含视频历史行回填）+ `internal/gateway/observability/call_type.go` 按端点归一化；`model` 去掉 `:CreateTask`/`:GetTask` 后缀与 `seedance-asset:` 伪模型名；看板/前端筛选优先按 `call_type`，保留旧约定兜底 | `call_logs.call_type` |

顺带修掉两处日志失真：

- `truncateVideoTaskBody` 原按**字节**硬切，会把中文/emoji 切成非法 UTF-8，改为按 rune 边界截断并追加 `...(truncated)`；
- 创建任务的响应体登记后，`call_traces.task_id` 可通过既有的 `extractTaskFields(resp.id)` 抽出，**创建日志终于能关联到任务**（此前只有 GET 日志能关联）。

验证方式（本机 PG 18 + Atlas v1.2.3）：

- `call_logs` 与 `cd tools/atlas-loader && go run main.go` 导出的期望 schema **逐列比对一致**（56 列 == 56 列，`call_type = varchar(64)`）；
- 剔除他人未提交 WIP `*.down.sql` 的副本上全量 apply：58 个迁移 / 307 条语句干净通过；
- 回填 SQL 用 13 行历史样本实测：端点优先、`model` 后缀兜底、超长/多段 Action 退回 `video.asset`、**已写 `call_type` 的行不被覆盖**、chat 行不受影响，全部符合预期。

端到端验证（真 gateway 进程 + 真 PG/Redis + 本地 stub 上游，2026-10-14）：

以 `seedance-relay` 渠道指向本地 stub，跑通「创建成功 / 查询 / 素材动作 / 上游 429 失败」四场景，落库实测（`request_id` 逐条比对）：

| 场景 | `call_type` | `call_logs.model` | `status` | `upstream_status` | `call_traces` |
|---|---|---|---|---|---|
| 创建成功 | `video.create` | `seedance-video-e2e`（无 `:CreateTask`） | 200 | **200** | `request_body.model`=组名（改写前）、`response_body.id`=tasks id、**`task_id` 已关联** |
| 查询任务 | `video.get` | 组名（无 `:GetTask`） | 200 | **200** | `request_body` 为 NULL（GET 无正文）、`task_id` 已关联 |
| 素材动作 | `video.asset.CreateAssetGroup` | 空串（无 `seedance-asset:`） | 200 | **200** | 请求/响应正文齐全 |
| 上游 429 | `video.create` | 组名 | **502** | **429** | `error_code=upstream_error`、`error_message=rate limit exceeded` |

同时得到两个交叉印证：`video_tasks.request_body` 里是**改写后**的上游模型名、而 `call_traces.request_body` 里是**改写前**的组名，证明正文采集确实发生在改写之前；stub 侧收到 `POST /seedance/api/v3/contents/generations/tasks` 且 `Authorization: Bearer sk-stub-e2e`，证明凭据解密→快照→出站整条链路真实生效。

同一环境还补了两块覆盖：

**① 火山官方渠道（`seedance-volcengine`）**：新增指向同一 stub 的 volcengine 渠道后，`api_key` 与 `aksk` 两种凭据模式均端到端跑通；出站为官方路径 `POST /api/v3/contents/generations/tasks`（对比 relay 的 `/seedance/api/v3/...`）；`aksk` 模式下 stub 收到完整签名头 `Authorization: HMAC-SHA256 Credential=AKLTstub/<date>/cn-beijing/ark/request`、`X-Date`、`X-Content-Sha256`。落库 `provider` 列正确归属到官方渠道。

**② 失败注入矩阵**：`upstream_status` 能区分"上游错"与"我们自己错"，这正是旧版恒为 0 时拿不到的信息：

| 注入 | 网关对外 | `status` | `upstream_status` | `error_message` |
|---|---|---|---|---|
| 上游 429 | 502 | 502 | **429** | `rate limit exceeded`（上游 message） |
| 上游 503 | 502 | 502 | **503** | `upstream overloaded`（上游 message） |
| 上游 200 但响应体非 JSON | 502 | 502 | **200** | `parse response: invalid character '<' …` |
| 上游挂起（超 30s 客户端超时） | 502 | 502 | **0** | `do request: … context deadline exceeded` |
| 上游不可达（连接被拒） | 502 | 502 | **0** | `do request: … connection refused` |
| 路径不存在 | 502 | 502 | **404** | `stub: unmatched path …` |

即：`upstream_status=0` 表示**根本没拿到上游响应**（超时/连接失败），`200` 表示**上游正常但我们处理失败**，二者在排障时指向完全不同的责任方。

仍未做（本报告 P0/P1 的其余条目）：视频限流/配额、`GetTask` 越权读、素材 `Action` 白名单、视频计费（按次/按秒）、服务端任务推进。
