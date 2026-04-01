# VL Rerank 协议扩展设计

> **版本**: v1.0 | **日期**: 2026-07-10 | **状态**: 待评审  
> **前置**: [embedding-rerank-protocol-design](./2026-07-10-embedding-rerank-protocol-design.md)  
> **决策摘要**: 对外对齐硅基流动式扁平协议；对内 Bailian 映射百炼原生；新增 `model_type=rerank_vl`。

---

## 1. 背景与目标

### 1.1 问题

当前网关 `RerankRequest` 将 `query` / `documents` 固定为 `string` / `[]string`。  
`qwen3-vl-rerank` 的图文调用（`query: {"image":...}`）在 JSON 绑定阶段即失败：

```text
json: cannot unmarshal object into Go struct field .query of type string
```

百炼官方与硅基流动均支持多模态 object；模型本身可用，是**网关协议层**挡住了 VL 路径。

### 1.2 目标

1. 扩展 `/v1/rerank`，使 `query` / `documents` 支持 `string | object`。
2. **纯文本调用完全兼容**现网 / LiteLLM / NewAPI / Cohere。
3. 图文仅允许 `model_type=rerank_vl`；非 VL → 400。
4. 对外一套协议；Bailian adapter 负责转百炼原生 `input/parameters`。
5. 为后续硅基流动等渠道预留同一对外形态（近乎透传）。

### 1.3 非目标

- 不对外同时接受百炼原生 `{input, parameters}` 作为主协议。
- 不追求与硅基流动 OpenAPI 字节级一致（扩展参数按渠道透传/忽略）。
- 一期不做：单 object 内 text+image 混装、复杂 document 回传、非百炼 VL 真机。
- 不做跨模型 rerank failover。

---

## 2. 业界对照与选型

| 来源 | 文本 rerank | VL 图文 | 形态 |
|------|-------------|---------|------|
| Cohere / LiteLLM / NewAPI | ✅ | ❌ | 扁平，`query: string` |
| 硅基流动 | ✅ | ✅ | 扁平 `/v1/rerank`，`string \| object` |
| 百炼 DashScope | ✅ | ✅ | 原生 `input` + `parameters` |
| vLLM 等 | ✅ | ✅（另一套 content 结构） | 自建推理 |

**结论**: VL rerank **无**业界统一 OpenAPI。  
**对外协议**: 硅基流动式扁平（客户端友好 + 文本兼容）。  
**上游映射**: 百炼原生（当前真实供给）。

---

## 3. 对外协议（硅基流动式）

### 3.1 端点

`POST /v1/rerank`（不变）

### 3.2 请求

| 字段 | 类型 | 说明 |
|------|------|------|
| `model` | string | 模型组名或模型名 |
| `query` | `string \| MultimodalPart` | 查询 |
| `documents` | `(string \| MultimodalPart)[]` | 候选文档，1–1000（VL 上游另有更严上限，由 adapter/上游约束） |
| `top_n` | int? | 返回前 N |
| `return_documents` | bool? | 是否回传文档 |
| `max_chunks_per_doc` | int? | Cohere 扩展；不支持的渠道忽略 |
| `provider` | string? | 网关路由提示，转发前剥离 |

**MultimodalPart（一期：单键）**

```json
{ "text": "非空字符串" }
{ "image": "URL 或 data:image/...;base64,..." }
{ "video": "公开可访问 URL" }
```

规则：

1. object **有且仅有一个** 模态键：`text` | `image` | `video`。
2. 值必须为非空 string。
3. `query` 允许：`string` | `{text}` | `{image}`；**不允许** `{video}`（对齐百炼）。
4. `documents[]` 允许：`string` | `{text}` | `{image}` | `{video}`。
5. 一期**不支持**同一 object 内多键混装（如同时 `text`+`image`）。
6. 纯 `string` 与现网完全兼容。

**文本示例（兼容）**

```json
{
  "model": "bailian-rerank-qwen3",
  "query": "法国首都是哪?",
  "documents": ["巴黎是法国首都", "伦敦是英国首都"],
  "top_n": 2
}
```

**VL 示例（扩展）**

```json
{
  "model": "bailian-rerank-vl",
  "query": { "image": "https://example.com/q.png" },
  "documents": [
    { "text": "一只红色的猫" },
    { "image": "https://example.com/d1.png" },
    { "video": "https://example.com/d1.mp4" }
  ],
  "top_n": 2,
  "return_documents": true
}
```

### 3.3 响应（一期不变）

保持 Cohere 风格：

```json
{
  "id": "...",
  "model": "...",
  "provider": "dashscope",
  "results": [
    { "index": 0, "relevance_score": 0.95 }
  ],
  "usage": { "total_tokens": 123, "search_units": 0 }
}
```

`return_documents=true` 时：一期优先回传可表示为文本的内容；图文原文完整回传可作为二期。

### 3.4 兼容性声明

| 项 | 状态 |
|----|------|
| 纯文本 `query`/`documents` | ✅ 与现网 / LiteLLM / Cohere 兼容 |
| 图文 object | ⚠️ 网关扩展（对齐硅基流动/百炼语义，非 Cohere 标准） |
| 百炼原生 `input/parameters` 作主协议 | ❌ 不采用；需要时走 passthrough |
| 路径 | `/v1/rerank`（与现网一致） |

---

## 4. 能力模型：`rerank_vl`

### 4.1 新增类型

| model_type | 含义 | 典型模型 |
|------------|------|----------|
| `rerank` | 纯文本重排序 | `qwen3-rerank`, `gte-rerank-v2`, Cohere |
| `rerank_vl` | 多模态重排序 | `qwen3-vl-rerank` |

与 `embedding` / `embedding_vl` 对称。

### 4.2 校验规则

定义「多模态 rerank 输入」：`query` 或任一 `documents` 元素为含 `image`/`video` 的 object（或非纯文本形态）。

| 输入 | model_type | 行为 |
|------|------------|------|
| 全纯文本（string 或仅 `{text}`） | `rerank` 或 `rerank_vl` | 通过 |
| 含 image/video | **仅** `rerank_vl` | 通过 |
| 含 image/video + 非 `rerank_vl` | — | **400** `model_capability_mismatch` |
| `query` 为 `{video}` | — | **400** `request_body_invalid` |
| object 多键 / 空键 / 未知键 | — | **400** `request_body_invalid` |

### 4.3 控制面

- Create/Update `oneof` 增加 `rerank_vl`。
- 组内成员 `model_type` 必须一致（沿用现有规则）。
- 渠道同步推断：名含 `rerank` 且含 `vl` / `multimodal` / `vision` → `rerank_vl`；仅 `rerank` → `rerank`。
- 存量 `qwen3-vl-rerank`：迁移或同步纠正为 `rerank_vl`。
- 探测：`rerank_vl` 使用最小**文本**探测体即可（避免探测依赖外网图片）；可选后续加 VL 探测。

---

## 5. 分层与 Adapter

```text
Client
  │  硅基流动式扁平协议
  ▼
RerankHandler
  │  绑定 any → 校验形态 → 组调度填 ModelType → 能力校验
  ▼
providers.Router
  ▼
Bailian Adapter
  │  扁平 → 百炼原生 input/parameters
  ▼
DashScope text-rerank API
```

### 5.1 核心类型变更

```go
// 概念示意
type RerankRequest struct {
    Model           string
    Provider        string
    Query           any       // string | map
    Documents       []any     // string | map
    TopN            *int
    ReturnDocuments *bool
    MaxChunksPerDoc *int
    // ...
}
```

Handler 内提供归一化/检测辅助：`isMultimodalRerankInput`、`validateRerankParts`。

### 5.2 Bailian 映射

上游端点不变：`POST /api/v1/services/rerank/text-rerank/text-rerank`

```json
{
  "model": "<req.Model>",
  "input": {
    "query": <透传 string 或 object>,
    "documents": <透传 string/object 数组>
  },
  "parameters": {
    "top_n": ...,
    "return_documents": ...,
    "instruct": <ExtraFields 可选>,
    "fps": <ExtraFields 可选，VL>
  }
}
```

文本模型（`rerank`）：

- 若收到 `{text: s}`，可剥成 `s` 再发给仅支持 string 的上游路径（若需要）。
- 若仍含 image/video，不应到达此处（Handler 已拦）。

### 5.3 其它渠道

- Compatible / Cohere：继续文本；收到多模态 → 400/501。
- 未来 SiliconFlow 渠道：扁平 body 近乎透传。

### 5.4 百炼原生兼容（非主路径）

已有系统若必须打 `input/parameters`，使用现有 passthrough（`/v1/p/:provider/*`），**不**抬升为主协议。

---

## 6. 错误契约

| 场景 | HTTP | code |
|------|------|------|
| JSON 形态非法 | 400 | `request_body_invalid` |
| 图文 + 非 `rerank_vl` | 400 | `model_capability_mismatch` |
| 渠道不支持 VL | 501 | `unsupported_operation` |
| 上游失败 | 映射/透传 | 既有错误映射 |

---

## 7. 实现范围

### 7.1 一期（P0）

1. `RerankRequest` 类型放宽 + 校验。
2. Handler 能力校验（对齐 embeddings）。
3. `model_type=rerank_vl`（控制面 binding、推断、组一致性、前端枚举若有）。
4. Bailian adapter 支持 object 透传映射。
5. 单测 + 百炼 VL 图文真机（aimeter 或本地）。
6. 存量 `qwen3-vl-rerank` → `rerank_vl`。

### 7.2 二期（可选）

1. 硅基流动 provider 注册与透传。
2. `return_documents` 图文回传。
3. 单 part 多模态混装（若上游确认支持）。
4. VL 专用健康探测。

---

## 8. 测试计划

| 用例 | 期望 |
|------|------|
| 文本 rerank 各模型（含 vl 模型打纯文本） | 200，与现网一致 |
| VL：image query + 混合 documents | 200（`rerank_vl`） |
| 文本组 + image query | 400 `model_capability_mismatch` |
| 非法 object（多键/空） | 400 |
| 大批量文本 documents（≤1000） | 行为与现网一致 |
| 回归：gte / qwen3-rerank 纯文本 | 通过 |

---

## 9. 决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 对外协议 | 硅基流动式扁平 | 文本兼容、客户端友好、多上游可复用 |
| 是否对外收百炼原生 | 否（passthrough 兜底） | 避免双协议 |
| `rerank_vl` | **新增** | 与 `embedding_vl` 对称，能力校验不靠名字启发式 |
| 一期上游 | 仅深做百炼 | 现网供给 |

---

## 10. 开放问题

无（关键选型已确认）。实现阶段若硅基流动字段与百炼冲突，以「对外文档 + ExtraFields」消解，不阻塞 P0。
