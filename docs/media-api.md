# 媒体接口（图像 / 视频）对外协议

> **文档状态：已定稿（Batch 3 · E4）**。本文是 `/v1/images/*` 与 `/v1/videos/*` 的**对外契约事实源**：路由、字段名、状态词、错误码四项都可以在代码里逐字找到对应实现（证据以 `file:line` 标注在正文与 `tests/contract/contracts.json` 的 `media_channels` 段里）。
>
> 事实源与校验关系：
> - **FE↔BE 契约表**：`tests/contract/contracts.json` 的 `media_channels` 段 —— §6 错误码表、§4.4 响应字段与本文件**逐字一致**（改一处必须同时改另一处）。
> - 设计与上游契约：[media-channels-integration-design.md](media-channels-integration-design.md)（§2.1 对外协议、§3 三家上游契约、§6 网关侧设计、§8 计费）
> - 实现：`internal/gateway/server/routes_media.go`（注册）、`internal/gateway/handler/image_handler.go`（图像）、`internal/gateway/handler/media_task_handler.go`（视频任务三件套）
> - 组件测试：`tests/gateway/handler/image_handler_test.go`、`tests/gateway/handler/media_task_*_test.go`、`tests/gateway/server/media_task_routes_test.go`
>
> 实现状态（截至本文定稿）：
>
> | 链路 | 状态 |
> |---|---|
> | `POST /v1/images/generations`、`POST /v1/images/edits`（OpenAI gpt-image） | 已接通 |
> | `POST /v1/videos`、`GET /v1/videos/{id}`、`GET /v1/videos/{id}/content`（任务平台） | 已接通；**Kling / Vidu 渠道适配器仍是未实现接缝**，这两个渠道会返回 `501 not_implemented`（刻意行为，避免用占位凭据打真实上游产生费用） |
>
> ⚠️ 本文**不是** Seedance（`/v1/contents/generations/tasks`）的文档：那是既有直通链路，与本链路**并存、互不顶替**。

---

## 0. 目录

| 章节 | 内容 |
|---|---|
| §1 | 总则：Base URL、鉴权、模型组语义、错误信封 |
| §2 | 能力与渠道矩阵 |
| §3 | 图像同步接口 `/v1/images/*` |
| §4 | 视频任务接口 `/v1/videos/*`（三件套） |
| §5 | 状态词表与状态机 |
| §6 | 错误码 |
| §7 | 计费口径 |
| §8 | 示例（curl + 响应体） |
| §9 | 相关文档 |

---

## 1. 总则

### 1.1 Base URL 与鉴权

- 对外前缀：`/v1`（与 `/v1/chat/completions` 等一致）。
- 鉴权：`Authorization: Bearer <API Key>`，与既有 `/v1/*` 完全同一条鉴权链（Key 校验 → 组织归集检查 → 预算 → 配额）。
- 内容类型：图像生成与视频创建是 `application/json`；图像编辑是 `multipart/form-data`。
- 中间件链（挂在 `/v1` 组上）：`Auth` → `BodyBudgetLimiter` → **`BillingFactGuard`** → `TraceCapture` 等。因此计费事实存储背压时，`POST /v1/images/*` 与 `POST /v1/videos` 会先被拦成 `503 billing_fact_store_backpressure`（实现：`internal/gateway/server/router.go:276`、`:420-448`）。

### 1.2 模型组语义（重要）

- 三个视频/图像接口的 `model` 字段一律填**模型组名**（对外模型名），由网关完成「组名 → 上游模型 + 渠道」调度。
- **直连上游模型 ID 一律拒绝**：`403 direct_model_not_allowed`（避免绕过计费与能力聚合）。
- 模型组能力是**组内交集**（防护栏 A2）：组内全部成员都支持的能力对外才可见；token 上限取最小正值。
- 渠道参数级校验（时长/分辨率/模式组合）在**适配器内**做（fail-closed）：网关只维护 `POST /v1/videos` 的 7 个冻结参数键，不额外维护渠道白名单。

### 1.3 错误信封

统一为 OpenAI 形状（`error` 对象；`param` 字段恒在，无值时输出 `null`）：

```json
{
  "error": {
    "code": "invalid_request_error",
    "message": "model 字段是必填项",
    "param": "model",
    "type": "invalid_request_error"
  }
}
```

- 少数闸门会额外带上下文键，例如无价拒绝带 `"gate": "media_pricing"`（`internal/gateway/middleware/media_pricing_gate.go:82-95`）。
- 平台级闸门（预算/配额/模型组不可用/背压）复用**既有统一错误体**，字段为 `type` / `code` / `message`（可能没有 `param`）：`internal/gateway/budget/gate.go:87-113`、`internal/gateway/handler/chat.go:474-497`。
- JSON 对象的键顺序不保证（Go 对 `map` 按键名排序），对接方必须按字段名读取。

---

## 2. 能力与渠道矩阵

| 渠道 | 类型 | 对外能力 | 上游状态词表 | 上游产物 |
|---|---|---|---|---|
| OpenAI（gpt-image） | 同步 | 图像生成 / 图像编辑 | —（同步返回） | `data[].b64_json` / `data[].url`（原样透传） |
| Kling 可灵 | 异步 | 文生视频 / 图生视频 | `submitted` / `processing` / `succeed` / `failed` | `video_url`（签名链接） |
| Vidu | 异步 | 文生 / 图生 / 首尾帧 / 参考生视频 | `created` / `queueing` / `processing` / `success` / `failed` | `creations[].url` |

> 上游词表由**适配器**归一为对外词表（§5）。上游出现未知状态词时按**协议失败**处理（返回 error），既不当成 `completed`，也不无限轮询。
>
> 渠道参数矩阵（`duration` / `resolution` / `mode` 的合法组合）在适配器内 fail-closed 校验；网关不代适配器做参数白名单，因此**以各渠道适配器报错为准**（`400 invalid_request_error`）。

---

## 3. 图像同步接口

图像链路是**同步**的：网关把请求翻译后调上游，拿到响应后**字节级原样回写**（`internal/gateway/handler/image_handler.go:669-691`）——不做 Unmarshal/Marshal/sanitize（`b64_json` 可达数十 MB；重新序列化既浪费内存也可能改变字节）。上游超时 120s（`:76`）。

### 3.1 `POST /v1/images/generations`

- 请求：`application/json`，body 上限 10MB（`:57`）。
- 请求字段（白名单，`internal/core/image.go:62-77`）：

  | 字段 | 说明 |
  |---|---|
  | `model` | 必填；模型组名 |
  | `prompt` | 必填 |
  | `n` | 可选；生成张数 |
  | `size` | 可选；缺省由上游决定（gpt-image 常见缺省 `1024x1024`） |
  | `quality` | 可选；缺省 `high` |
  | `background` | 可选 |
  | `moderation` | 可选 |
  | `output_format` | 可选；缺省 `png` |
  | `output_compression` | 可选 |
  | `user` | 可选 |

- **不支持**：`response_format`（改用 `output_format`）、`style`（仅 dall-e-3）、`seed` → `400 invalid_request_error`，并在 message 里给出替代建议，**不做静默丢弃**（`:589-628`、`:644-660`）。
- 响应：`200`，`Content-Type: application/json`，**上游响应字节原样**。

### 3.2 `POST /v1/images/edits`

- 请求：`multipart/form-data`。
- multipart 字段：

  | 字段 | 说明 |
  |---|---|
  | `model` | 必填；模型组名 |
  | `prompt` | 必填 |
  | `image` / `image[]` | 可重复字段，至少 1 个；**顺序语义化**（首尾帧），网关重建时保序并保留真实 filename |
  | `mask` | 可选；缺省文件名 `mask.png` |

- 除 `image` / `mask` 外的文本字段白名单：`model`、`prompt`、`n`、`size`、`quality`、`background`、`moderation`、`output_format`、`output_compression`、`input_fidelity`、`response_format`、`user`（`internal/core/image.go:84-104`）。
- 与 generations 的差异：edits **额外支持** `response_format`；两者都拒绝 `style` / `seed`。
- 上限：单个文件 part 32MB / 整请求 128MB / 单个文本字段 1MB；超限返回 `400 invalid_request_error`（`:54-77`、`:462-521`）。
- 响应：与 generations 相同（上游字节原样回写）。

---

## 4. 视频任务接口（`/v1/videos` 三件套）

创建 → 轮询 → 取产物。创建是同步的（拿到上游任务 id 才返回），后续状态靠**查询**推进（网关不做后台轮询；超时清扫在控制面 cron）。

### 4.1 `POST /v1/videos`（创建）

**请求字段**

| 字段 | 说明 |
|---|---|
| `model` | 必填；**模型组名** |
| `prompt` | 与 `image` 至少给一个（纯图生视频可以没有 prompt） |
| `image` / `images` | 可选；可单值可数组，元素可为 URL 字符串、data URL 或 `{"url": "..."}` 对象。**保序不去重** |
| `duration` | 可选；秒（数字或数字字符串，网关归一为 int） |
| `resolution` | 可选；如 `720p` / `1080p` |
| `mode` | 可选；如 `std` / `pro` |
| `action` | 可选；显式声明动作。**只做一致性校验**：与图片张数推出的动作矛盾时 `400` |
| `aspect_ratio` | 可选；`1:1` / `16:9` / `9:16`。缺省时由 `size` 推导（推导不出则不填，交给适配器 fail-closed） |
| `size` | 可选；形如 `1280x720`。用于推导 `aspect_ratio`，并原样回填到响应的 `size` |
| `kind` | 可选；缺省 `video`；显式 `image` 用于异步图像渠道 |
| `client_request_id` | 可选；幂等键的 body 兜底 |

**动作由图片张数决定**（不是由 `action` 决定）：0 张 → `text_to_video`；1 张 → `image_to_video`；2 张 → `first_tail_to_video`；>2 张 → `reference_to_video`。

**可选请求头**：`Idempotency-Key` —— 命中已知 `(api_key_id, idempotency_key)` 时**直接返回既有任务**（`200`），**不重复打上游、不重复计费**。无幂等键时每次创建都是新任务。

**响应：`200` + OpenAI video 对象**（成功创建与幂等命中都是 200）：

```json
{
  "id": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
  "object": "video",
  "status": "queued",
  "model": "kling-group",
  "created_at": 1760702400,
  "progress": 0,
  "seconds": "5",
  "size": "1920x1080"
}
```

**流程**（顺序是硬约束）：鉴权 → 组调度（组名 → 上游模型 + 渠道类型）→ 准入门禁（预算 → 请求配额 → 模型配额 → **媒体计价**）→ 适配器 `Submit`（**不重试**，至多发出一次）→ **同步**写控制面 `/internal/v1/video/tasks`（写失败必须 5xx，禁止 fire-and-forget）。

### 4.2 `GET /v1/videos/{task_id}`（查询）

- 成功：`200` + 与创建相同的 video 对象（字段见 §4.4）。
- **归属校验**：`task_id` 必须属于调用方 API Key/用户；**任务不存在与不属于调用方都返回 `404 not_found_error`**（不是 403——403 会泄露「该 task_id 存在」，可被用来枚举他人任务）。
- 流程：本地任务查询 → 归属校验 → 终态缓存直返（终态不再打上游、不重复记账）→ 未终态则 `adapter.Query(vendor_task_id)` → 状态归一 → **CAS 只前进** → 首次推进到 `completed` 时记账一次。
- 失败语义（对接方必读）：**上游业务失败**用 `200 + status=failed + error` 表达（不是 5xx、不重试）；**传输/协议失败**返回 `5xx`（`502 upstream_error`），此时任务状态不变，可稍后重试查询。

### 4.3 `GET /v1/videos/{task_id}/content`（取产物）

- 成功：**`302`** 重定向到上游产物地址（credentialless，`Cache-Control: no-store`；只放行 `http`/`https`）。
- 任务未完成或没有可下载产物：`404 not_found_error`。
- Phase 5 计划改为网关代理（届时语义变为字节流回传）。

**产物获取的两种方式**（都要做归属校验，都只能用调用方自己的 `task_id`）：

| 方式 | 用法 | 特点 |
|---|---|---|
| ① 标准方式：`GET /v1/videos/{task_id}/content` | 客户端跟随 302 拿到字节 | 与 OpenAI 一致；上游签名链接的凭据不下发给客户端 |
| ② 本网关扩展：响应的 `artifacts[].url` | 直接用该 URL | **免一次重定向**，前端可直接播放/下载；但它是**上游签名链接**，有效期由上游决定（可能过期，`artifacts[].expires_at` 有值时才可依赖） |

> `artifacts` 是本网关的**扩展字段**（OpenAI video 对象没有它）。保留它是刻意决策：前端需要直连产物，不必为每个产物再发一次请求换 302。**标准/可移植的取产物方式仍是 ①**；只依赖 OpenAI 契约的客户端应忽略 `artifacts`。

### 4.4 响应字段（`video_response_fields` 冻结集合）

字段顺序为实际序列化顺序；类型与是否出现如下（与 `tests/contract/contracts.json` 的 `video_response_fields` / `video_response_field_types` 一致）：

| 字段 | 类型 | 出现条件 | 说明 |
|---|---|---|---|
| `id` | string | 恒有 | 网关生成的对外任务 id（`task_` + 32 位十六进制）；**上游任务 id 永不下发** |
| `object` | string | 恒有 | 恒为 `video` |
| `status` | string | 恒有 | 见 §5；`queued` / `in_progress` / `completed` / `failed` |
| `model` | string | 恒有 | **模型组名**（不是上游模型 id） |
| `created_at` | int64 | 恒有 | Unix 秒 |
| `completed_at` | int64 | 终态时 | Unix 秒；取自控制面在**首次**进入终态时写入的 `finished_at` |
| `progress` | **int** | 恒有 | **0–100 的整数**（不是字符串），恒出现，`0` 也出现；归一规则见下 |
| `seconds` | string | 能取到时 | 秒的十进制字符串（如 `"8"`）；优先上游实际用量，退化请求参数 `duration` |
| `size` | string | 能取到时 | 形如 `1280x720`；只认请求参数里字面给出的 WxH |
| `error` | object | 失败且有原因时 | `{ "message": string, "code": string }`，`code` 目前恒为 `upstream_error` |
| `artifacts` | array | 有产物时 | **本网关扩展字段**；元素字段：`kind`（`image`/`video`）、`url`、`mime_type`（可选）、`expires_at`（可选，RFC3339） |

**`progress` 归一规则（定稿）**——存储层 `media_tasks.progress` 是 varchar，可能是百分比数字也可能是阶段名：

1. 能解析成数字（允许尾随 `%`、前后空白、小数）→ 取该数字并 clamp 到 `0..100`，**小数向下取整**（宁可低估，也不把 99.4% 说成 100%）；
2. 阶段名 → 显式映射表：`queued`/`submitted`/`created`/`pending`/`not_start`/`waiting` = `0`；`queueing` = `10`；`in_progress`/`processing`/`running`/`started`/`generating` = `50`；`completed`/`succeed`/`succeeded`/`success`/`finished`/`done` = `100`；`failed`/`failure`/`error`/`expired`/`cancelled`/`canceled`/`rejected` = `0`；
3. **空值或认不出的词 = `0`**（明确的下界：绝不填 `100`、也不外推未知值）；
4. 归一后状态是 `completed` → **强制 `100`**（避免出现 `status=completed` 与 `progress=0` 自相矛盾的响应）。

> 上述档位是**展示约定**，不是实测百分比：网关只知道粗粒度阶段（上游的百分比口径各不相同，Kling 不报百分比）。另外网关**不保证** `progress` 单调不减——存储层只保留最后一次上报值；若同一渠道先报百分比、后又只报阶段名，对外可能回退。现有渠道口径稳定（Vidu 报百分比、Kling 报阶段名），未观察到该情形。

**`seconds` / `size` 的推导边界**：`seconds` 优先上游实际用量（`usage.vendor.duration` 等），退化请求参数 `duration`；`size` 只认请求参数里**字面**给出的 WxH（`size` 优先，其次 `resolution`）。两者**取不到就省略，绝不编造**——尤其不做 `"720p"` → `"1280x720"` 的推导：短边 720 对应的另一边取决于宽高比、渠道与 `mode` 的输出档位，推导错比不填更糟。

**刻意不实现**的 OpenAI video 字段（宁可不发也不编造）：`task_id`（OpenAI 侧是待废弃的旧 id 别名，本网关对外 id 只有一个）、`remixed_from_video_id`（无 remix 能力）、`metadata`（无自定义元数据）、`expires_at`（未实现；产物签名链接的过期时间只在 `artifacts[].expires_at` 里）。

---

## 5. 状态词表与状态机

- 对外状态（**OpenAI 词表，唯一对外口径**）：`queued` → `in_progress` → `completed` | `failed`。
- 只允许**前进**，终态不可回退（CAS 保证）。
- 存储层词表到对外词表的归一（认不出的存储值当**非终态**，继续走上游轮询）：

  | 存储层值 | 对外状态 |
  |---|---|
  | `queued` / `submitted` / `created` / `pending` / `not_start` / `waiting` | `queued` |
  | `in_progress` / `processing` / `running` / `queueing` / `started` / `generating` | `in_progress` |
  | `completed` / `succeed` / `succeeded` / `success` / `finished` / `done` | `completed` |
  | `failed` / `failure` / `error` / **`expired`** / `cancelled` / `canceled` / `rejected` | `failed` |

- **`expired` 对外归一为 `failed`**：`expired` 是控制面超时清扫（控制面 **cron**，不是拉取式端点）写入的**存储层**状态，对外必须表现为 `failed`、**不记账**，`error.message` 说明超时。控制面内部读端点已先行归一（`internal/controlplane/handler/video_task_handler.go` 的 `internalTaskView`），网关侧再做一次防御性归一。
- 上游使用不同词表（Kling `submitted/processing/succeed/failed`；Vidu `created/queueing/processing/success/failed`），由适配器归一；**上游未知状态词视为协议失败**，不得当成 `completed`，也不得无限轮询。

> **失败语义（对接方必读）**：**业务失败**（上游明确返回失败）用 `status=failed` + `error` 表达——HTTP 仍是 `200`，不要重试；**传输/协议失败**（上游不可达、响应非法、超时）由网关返回 `5xx`，可以稍后重试查询。

---

## 6. 错误码

> 本表与 `tests/contract/contracts.json` 的 `media_channels.error_codes` **逐字一致**（改一处必须同时改另一处）。**表外 code 不得出现**：契约里写了但代码不返回，是最坏的一种文档。

| HTTP | code | 触发条件 |
|---|---|---|
| 400 | `invalid_request_error` | 参数缺失/矛盾/请求体非法；图像链路的不支持参数（generations 的 `response_format` / `style` / `seed`，edits 的 `style` / `seed`），message 给出替代建议 |
| 402 | `media_pricing_missing` | 准入计价开关打开且该模型**没有媒体价**（唯一接线点：组调度之后、调上游之前）。不会静默按 0 元放行 |
| 402 | `budget_exceeded` / `tier_budget_exceeded` | 平台预算闸门（沿用既有 402 语义，`gate: "budget"`） |
| 403 | `direct_model_not_allowed` | `model` 填了直连模型 ID 而非模型组名 |
| 404 | `not_found_error` | 任务不存在，或（有意地）不属于当前 Key/用户；任务无可下载产物 |
| 404 | `group_not_found` | 模型组不存在（平台统一错误体） |
| 429 | 配额类 code | 请求层/模型层配额闸门（沿用平台既有语义） |
| 501 | `not_implemented` | 渠道未实现对应媒体能力（`core.ErrMediaNotSupported`）；Kling/Vidu 适配器尚未接通时也走这里 |
| 502 | `upstream_error` | 上游不可达/响应非法；任务状态或产物写入失败；提交上游时的协议失败 |
| 503 | `service_unavailable` | 任务持久化未接线（控制面内部 API 不可用） |
| 503 | `billing_fact_store_backpressure` | 计费事实存储背压（`BillingFactGuard` 拦在 `POST /v1/images/*`、`POST /v1/videos` 之前） |
| 503 | `model_unavailable` | 模型组暂时无可用模型 |

补充事实：

- `media_pricing_unavailable` 与 `validation_error` 在**媒体链路没有任何代码路径**，因此**不在**本表（媒体准入闸门只有 402）。前端与对接方不得按这两个 code 分支。
- 图像链路会把**上游 4xx 原样透传**（状态码 + 上游错误体/`code`）：因此 400/401/403/404 的 `code` 可能来自上游而不是网关自造（`internal/gateway/handler/image_handler.go:729-737`）。上游 5xx 一律重映射为 `502 upstream_error`。
- 平台级闸门（预算/配额/模型组/背压）的错误体字段是 `type` / `code` / `message`，可能没有 `param`；媒体链路自己的错误体固定为 `type` / `message` / `param` / `code`。

---

## 7. 计费口径

- **事后记账**：仅终态 `completed` 记一次；`failed` / `expired` 不记账；重复查询不重复记账（并发查询只有 CAS 推进成功的那次记账）。
- 图像：优先 image token（`usage.output_tokens` 等）；退化 `len(data) × per_image`。
- 视频：优先上游实际用量（如 Kling `final_unit_deduction`）；退化「时长 × 分辨率档位单价」（Vidu）。
- `n` 与实际产物数不一致时，按**实际产物数**记账。
- 价表查不到时**不写成本**且明确标记用量缺失（绝不静默记 0 元）。
- 对外响应**不含**任何单价/成本字段（§4.4 的字段表就是全集）；成本只落在媒体任务表与管理面用量接口（`/api/v1/usage*`）。

---

## 8. 示例

> 下面示例中的 id、时间戳、URL、签名均为示意值；**字段名与状态码与实现一致**。

### 8.1 图像生成

```bash
curl -sS -X POST "$BASE/v1/images/generations" \
  -H "Authorization: Bearer $GATEWAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-group",
    "prompt": "一只戴墨镜的柴犬，扁平插画风格",
    "size": "1024x1024",
    "quality": "high",
    "output_format": "png"
  }'
```

```json
{
  "created": 1760702400,
  "data": [
    { "b64_json": "iVBORw0KGgoAAAANSUhEUg…(略，可达数十 MB，网关按字节原样回写)" }
  ]
}
```

### 8.2 图像编辑（多图 + mask）

```bash
curl -sS -X POST "$BASE/v1/images/edits" \
  -H "Authorization: Bearer $GATEWAY_API_KEY" \
  -F 'model=gpt-image-group' \
  -F 'prompt=把背景换成雪山' \
  -F 'image[]=@first.png' \
  -F 'image[]=@last.png' \
  -F 'mask=@mask.png' \
  -F 'output_format=png'
```

响应与 8.1 同形（原样回写上游 JSON）。

### 8.3 视频：创建 → 轮询 → 取产物

```bash
# ① 创建（带幂等键；命中时 200 直接返回既有任务，不重复计费）
curl -sS -X POST "$BASE/v1/videos" \
  -H "Authorization: Bearer $GATEWAY_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0" \
  -d '{
    "model": "kling-group",
    "prompt": "一只猫在冲浪",
    "image": "https://cdn.example.com/cat.png",
    "duration": 5,
    "resolution": "1080p",
    "mode": "pro",
    "size": "1920x1080"
  }'
```

```json
{
  "id": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
  "object": "video",
  "status": "queued",
  "model": "kling-group",
  "created_at": 1760702400,
  "progress": 0,
  "seconds": "5",
  "size": "1920x1080"
}
```

```bash
# ② 轮询（建议 2–5s 一次；未终态时才打上游）
curl -sS "$BASE/v1/videos/task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a" \
  -H "Authorization: Bearer $GATEWAY_API_KEY"
```

```json
{
  "id": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
  "object": "video",
  "status": "completed",
  "model": "kling-group",
  "created_at": 1760702400,
  "completed_at": 1760702532,
  "progress": 100,
  "seconds": "5",
  "size": "1920x1080",
  "artifacts": [
    {
      "kind": "video",
      "url": "https://cdn.example.com/out.mp4?sign=…",
      "mime_type": "video/mp4",
      "expires_at": "2026-10-17T12:00:00Z"
    }
  ]
}
```

```bash
# ③ 取产物（标准方式，跟随 302；产物 URL 是上游签名链接）
curl -sSL -o out.mp4 "$BASE/v1/videos/task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a/content" \
  -H "Authorization: Bearer $GATEWAY_API_KEY"
```

### 8.4 失败任务（业务失败：HTTP 200 + status=failed）

```json
{
  "id": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
  "object": "video",
  "status": "failed",
  "model": "kling-group",
  "created_at": 1760702400,
  "completed_at": 1760702532,
  "progress": 0,
  "error": {
    "message": "上游任务失败：prompt 命中安全策略",
    "code": "upstream_error"
  }
}
```

> 注意：这是 **HTTP 200**（业务失败），不是 5xx；`progress` 为 `0` 表示「失败时刻的实际进度不可知」，与 `status=failed` 一起读才是完整语义。

### 8.5 错误示例

```json
// 400：不支持的参数（图像生成）
{
  "error": {
    "code": "invalid_request_error",
    "message": "unsupported parameter(s): response_format (use output_format instead)",
    "param": "response_format",
    "type": "invalid_request_error"
  }
}
```

```json
// 402：该模型没有配置媒体单价（准入闸门拒绝，不静默按 0 元计费）
{
  "error": {
    "code": "media_pricing_missing",
    "gate": "media_pricing",
    "message": "该模型未配置媒体单价（图像/视频），为避免静默 0 元计费已拒绝本次调用；请管理员在模型目录补齐该模型的媒体单价后重试",
    "param": "kling-v1-6",
    "type": "media_pricing_missing"
  }
}
```

```json
// 403：直连模型 ID
{
  "error": {
    "code": "direct_model_not_allowed",
    "message": "model 必须填模型组名，直连上游模型 ID 被拒绝",
    "param": "kling-v1-6",
    "type": "direct_model_not_allowed"
  }
}
```

```json
// 404：任务不存在，或不属于当前 Key（两者同码，避免任务探测）
{
  "error": {
    "code": "not_found_error",
    "message": "任务不存在",
    "param": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
    "type": "not_found_error"
  }
}
```

```json
// 501：渠道未实现该媒体能力（当前 Kling / Vidu 适配器未接通时即此码）
{
  "error": {
    "code": "not_implemented",
    "message": "该渠道未实现视频任务能力（未接线或渠道不支持）",
    "param": "kling",
    "type": "not_implemented"
  }
}
```

```json
// 502：上游不可达 / 响应非法 / 任务状态写入失败
{
  "error": {
    "code": "upstream_error",
    "message": "上游任务查询失败（transient），请稍后重试",
    "param": "task_5f0d1c9b8e7a4d3c2b1a0f9e8d7c6b5a",
    "type": "upstream_error"
  }
}
```

```json
// 503：计费事实存储背压（中间件在进 handler 之前拦截）
{
  "error": {
    "code": "billing_fact_store_backpressure",
    "message": "Billing fact storage is temporarily at capacity"
  }
}
```

---

## 9. 相关文档

| 文档 | 内容 |
|---|---|
| [media-channels-integration-design.md](media-channels-integration-design.md) | 三渠道接入设计：上游契约、状态机、数据模型、计费口径 |
| [media-channels-implementation-plan.md](media-channels-implementation-plan.md) | 实施计划与任务分解（A/B/C/D/E 队） |
| [guardrails.md](guardrails.md) | 防护栏铁律（价格单位、迁移、密钥、内部 API 鉴权） |
| `../../tests/contract/contracts.json` | 契约表 `media_channels` 段（与本文互为校验；错误码与响应字段逐字一致） |

> 注：`docs/seedance-api.md` 只存在于本地未跟踪状态，**不在**版本库中，因而不是本文的事实源，也不应作为对接依据。
