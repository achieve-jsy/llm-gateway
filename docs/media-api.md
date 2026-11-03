# 媒体接口（图像 / 视频）对外协议

> **文档状态：骨架（A10 占位）**。本文件只登记**接口清单与契约要点**，正文（完整参数表、逐项错误码、可运行示例、渠道差异说明）由 **E 队（E4）** 补齐。
>
> 事实源：
> - 设计与上游契约：[media-channels-integration-design.md](media-channels-integration-design.md)（§2.1 对外协议、§3 三家上游契约、§6 网关侧设计、§8 计费）
> - FE↔BE 契约表：`tests/contract/contracts.json` 的 `media_channels` 段
> - 实施方案：[media-channels-implementation-plan.md](media-channels-implementation-plan.md)
>
> 实现状态（截至骨架落地）：路由接缝（`internal/gateway/server/routes_media.go`）与接口/类型接缝（`internal/core/image.go`、`internal/core/media_task.go`）已就位；图像链路（B 队）与任务平台（C 队）尚在实现中。

---

## 0. 目录

| 章节 | 内容 | 状态 |
|---|---|---|
| §1 | 总则：Base URL、鉴权、模型组语义、错误信封 | 骨架 |
| §2 | 能力与渠道矩阵 | 骨架（待 D 队回填渠道细节） |
| §3 | 图像同步接口 `/v1/images/*` | 接口清单已定，示例待补 |
| §4 | 视频任务接口 `/v1/videos/*` | 接口清单已定，示例待补 |
| §5 | 状态词表与状态机 | 已定 |
| §6 | 错误码 | 骨架（部分 code 待定稿） |
| §7 | 计费口径 | 骨架（待 A1/E3 回填字段名） |
| §8 | 示例 | **TODO(E)** |
| §9 | 相关文档 | 已定 |

---

## 1. 总则

### 1.1 Base URL 与鉴权

- 对外前缀：`/v1`（与现有 `/v1/chat/completions` 等一致）。
- 鉴权：`Authorization: Bearer <API Key>`，与既有 `/v1/*` 完全相同的鉴权链（Key 校验 → 组织归集检查 → 配额 → 预算）。
- 内容类型：图像生成、视频创建为 `application/json`；图像编辑为 `multipart/form-data`。

### 1.2 模型组语义（重要）

- 三个接口的 `model` 字段一律填**模型组名**（对外模型名），由网关完成「组名 → 上游模型 + 渠道」调度。
- **直连上游模型 ID 一律拒绝**：`403 direct_model_not_allowed`（避免绕过计费与能力聚合）。
- 模型组能力是**组内交集**（防护栏 A2）：组内全部成员都支持的能力对外才可见。

---

## 2. 能力与渠道矩阵

> TODO(E4/D)：按上线渠道逐行回填。下表只登记字段含义。

| 渠道 | 类型 | 对外能力 | 说明 |
|---|---|---|---|
| OpenAI（gpt-image） | 同步 | 图像生成 / 图像编辑 | 走 `/v1/images/generations`、`/v1/images/edits` |
| Kling 可灵 | 异步 | 文生视频 / 图生视频 | 走 `/v1/videos`，上游词表 `submitted/processing/succeed/failed` |
| Vidu | 异步 | 文生 / 图生 / 首尾帧 / 参考生视频 | 走 `/v1/videos`，上游词表 `created/queueing/processing/success/failed` |

---

## 3. 图像同步接口

### 3.1 `POST /v1/images/generations`

- 请求：`application/json`。
- 请求字段（**白名单**）：

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

- **不支持**：`response_format`（gpt-image 用 `output_format` 代替）、`style`（仅 dall-e-3）、`seed` → 返回 `400`，不做静默丢弃。
- 响应：原样回写上游响应字节（`data[].b64_json` 可达数十 MB），网关不重新序列化。

### 3.2 `POST /v1/images/edits`

- 请求：`multipart/form-data`。
- multipart 字段：

  | 字段 | 说明 |
  |---|---|
  | `model` | 必填；模型组名 |
  | `prompt` | 必填 |
  | `image` | 可重复字段（`image[]`），至少 1 个；**顺序语义化**，网关重建时保序 |
  | `mask` | 可选；缺省文件名 `mask.png` |

- 额外支持（相对 generations）：`input_fidelity`、`response_format`。
- 响应：与 generations 相同（原样回写）。

### 3.3 示例

> TODO(E4)：补 curl + 返回体示例（含 multipart 的 `-F 'image[]=@a.png'` 写法与 b64 截断展示）。

---

## 4. 视频任务接口

### 4.1 `POST /v1/videos`（创建）

- 请求字段：`model`（模型组名，必填）、`prompt`、`image`、`duration`、`resolution`、`mode`、`action`、`aspect_ratio`。
- ⚠️ **`image` 支持单值或数组**：0 张→文生、1 张→图生、2 张→首尾帧、>2 张→参考生视频（Vidu；Kling 仅支持 0/1 张）。网关统一归一为内部 `images []string`，适配器只认数组（见设计文档 §6.1.1）。
- 可选请求头：`Idempotency-Key` —— 命中已知 key 时**直接返回既有任务**，不重复打上游、不重复计费。
- 响应（OpenAI 形状）：

```json
{
  "id": "<网关生成的对外 task id>",
  "object": "video",
  "status": "queued",
  "model": "<模型组名>",
  "created_at": 0,
  "progress": "0"
}
```

> TODO(E4)：补完整字段说明与示例；`created_at` 为 Unix 秒。

### 4.2 `GET /v1/videos/{task_id}`（查询）

- 归属校验：`task_id` 必须属于调用方 API Key/用户；**跨 Key 读取返回 `404`**（不是 403，避免任务探测）。
- 返回终态缓存（未超 TTL 时不再打上游）；未终态则查上游并 CAS 推进状态。
- 响应字段：`id`、`object`、`status`、`model`、`created_at`、`progress`、`error`、`artifacts`。

> TODO(E4)：补 artifacts 精确形状与示例（C5 定稿后回填）。

### 4.3 `GET /v1/videos/{task_id}/content`（产物）

- `302` 直跳上游产物 URL（credentialless）；Phase 5 起可能改为网关代理（届时本接口语义变为字节流回传）。

---

## 5. 状态词表与状态机

- 对外状态（**OpenAI 词表，唯一对外口径**）：`queued` → `in_progress` → `completed` | `failed`。
- 只允许**前进**，终态不可回退（CAS 保证）。
- 控制面内部的 `expired`（超时 sweeper）对外归一为 `failed`，不记账。
- 上游使用不同词表（Kling `submitted/processing/succeed/failed`；Vidu `created/queueing/processing/success/failed`），由适配器归一；**上游未知状态词视为协议失败**，不得当成 `completed`，也不得无限轮询。

> 失败语义（对接方必读）：**业务失败**用 `status=failed` + `error` 表达（不是 5xx、不重试）；**传输/协议失败**由网关返回 `5xx`。

---

## 6. 错误码

> TODO(E4)：与实现逐项对齐后定稿；下表为骨架。

| HTTP | code | 触发条件 |
|---|---|---|
| 400 | `invalid_request_error` | 不支持的参数（如 generations 的 `response_format` / `style` / `seed`） |
| 400 | `validation_error` | 渠道参数前置校验失败（时长/分辨率/模式组合等） |
| 402 | `media_pricing_missing` | 该模型未配置媒体价且准入开关要求必须计价 |
| 403 | `direct_model_not_allowed` | `model` 填了直连模型 ID 而非模型组名 |
| 404 | `not_found_error` | 任务不存在，或（有意地）不属于当前 Key/用户 |
| 501 | `not_implemented` | 渠道未实现对应媒体能力 |
| 502 | `upstream_error` | 上游不可达 / 响应非法 |
| 503 | `media_pricing_unavailable`、`billing_fact_store_backpressure` | 计价不可用 / 记账链路背压 |

错误信封沿用 OpenAI 形状：

```json
{ "error": { "type": "...", "message": "...", "param": null, "code": "..." } }
```

---

## 7. 计费口径

- **事后记账**：仅终态 `completed` 记一次；`failed` / `expired` 不记账；重复查询不重复记账。
- 图像：优先 image token（`usage.output_tokens` 等），退化 `len(data) × per_image`。
- 视频：优先上游实际用量（如 Kling `final_unit_deduction`），退化「时长 × 分辨率档位单价」（Vidu）。
- `n` 与实际产物数不一致时，按**实际产物数**记账。

> TODO(E4)：补媒体单价的对外展示字段名（待 A1/E3 定稿）。

---

## 8. 示例

> **TODO(E4) 补齐**：
> 1. 图像生成（含 `b64_json` 截断展示）
> 2. 图像编辑（多图 `image[]` + `mask`）
> 3. 视频创建 → 轮询 → 取产物（含 `Idempotency-Key` 命中示例）
> 4. 错误示例（不支持的参数、归属校验 404、无媒体价 402）

---

## 9. 相关文档

| 文档 | 内容 |
|---|---|
| [media-channels-integration-design.md](media-channels-integration-design.md) | 三渠道接入设计：上游契约、状态机、数据模型、计费口径 |
| [media-channels-implementation-plan.md](media-channels-implementation-plan.md) | 实施计划与任务分解（A/B/C/D/E 队） |
| [guardrails.md](guardrails.md) | 防护栏铁律（价格单位、迁移、密钥、内部 API 鉴权） |
| `../../tests/contract/contracts.json` | 契约表 `media_channels` 段（与本文互为校验） |
