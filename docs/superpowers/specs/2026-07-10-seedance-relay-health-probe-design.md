# Seedance 中转（景汐）健康探测设计

日期：2026-07-10  
状态：已确认并实现（含组级 video skip + 前端测试按钮）  
范围：`seedance-relay`（景汐中转）的**唯一**连通+鉴权探针，供定时健康检查与管理面手动检测共用。

---

## 1. 问题

控制面定时健康检查对 OpenAI 兼容渠道执行：

```text
GET {endpoint}/models
```

景汐中转协议是 Seedance 路径（`/seedance/api/v3/...`），**没有** `/models`。  
返回 FastAPI 风格 `404 {"detail":"Not Found"}` → 连续失败 → `enabled=false` → gateway 视频适配器回退 placeholder。

同时存在**第二套**入口：管理面「测试连接」（`TestHandler`）也独立实现 `GET /models`，与定时探活**不是同一逻辑**。  
模型级「检测」又通过 `IsNonOpenAICompatible` 误绑到 Bedrock Converse。  
结果：同一渠道在不同按钮下可能得到不同结论，且改一处漏一处。

定义里虽有 `vendor_options.skip_forward_check: true`，但连通性探活路径**未读取**该字段，也无法替代「该用哪条协议路径」。

---

## 2. 核心原则：检测 = 探测（同一套逻辑）

**定时健康检查（探测）与管理面手动测试（检测）必须调用同一探针实现，不得各写一份 HTTP 判定。**

| 入口 | 现状 | 目标 |
|------|------|------|
| 定时 `HealthCheckService.CheckOne` | `GET /models` | 调共享探针 |
| 管理面「测试连接」`TestHandler` | 独立 `GET /models` + 独立 nonOpenAI map | **同一共享探针** |
| 模型「检测」`model_check` / `doModelForwardCheck` | 非 OpenAI → 一律 Bedrock | 按 `definition_id` 分发；景汐 → **同一共享探针**或明确 skip（语义见 §3） |
| 同步上游模型 `sync_service` | `GET /models` | 本期不改协议；UI/错误提示「景汐不支持 /models 同步」即可 |

共享探针是**唯一真相源**（single source of truth）：改判定规则只改一处，所有入口行为一致。

包装层只负责：

- 定时任务：写 health log、累加失败、AutoDisable
- 手动测试：返回 API JSON 给前端
- 模型检测：更新模型 check 结果 / 失败计数（不另造 HTTP）

---

## 3. 目标与非目标

### 目标

- 抽出 **一个** 景汐连通+鉴权探针，定时探活与手动检测共用。
- 探针只验证：**网络可达 + Bearer Key 被上游接受**。
- **不创建**视频任务，不消耗生成额度。
- 代码、日志、接口返回中**明确标注**测的是什么 / 不测什么。
- 非 OpenAI 按 `definition_id` **分发**，禁止 `seedance-relay` 落入 Bedrock 分支。

### 非目标（已部分放开）

- ~~不验证「能否成功创建/完成视频任务」。~~（仍不验证）
- ~~不做 `seedance-volcengine` 专属探针。~~ → **已做**：HMAC `GET .../tasks/__gateway_health_probe__`
- ~~不改变渠道自动禁用阈值；不新增渠道级自动恢复。~~ → **已做**：`status=3` 渠道继续参与 CheckAll，检测成功即 `AutoEnable` + 刷 snapshot
- 不引入通用 `health_check_path` 配置面（可后续扩展）。
- 不改造「同步上游模型」为 Seedance 协议（仅提示不支持）。

---

## 4. 探针语义（必须写清）

### 4.1 检测的是什么（In Scope）

| 检测项 | 含义 | 如何判定 |
|--------|------|----------|
| 网络可达 | control-plane 能连上渠道 `endpoint` | 请求未因 dial/timeout 失败 |
| 鉴权有效 | 渠道配置的 API Key 被上游接受 | HTTP **不是** 401 / 403 |
| 上游未整体 5xx | 协议面可响应 | HTTP **不是** 5xx（见 §4.3） |

探针请求（所有入口相同）：

```http
GET {endpoint}/seedance/api/v3/contents/generations/tasks/__gateway_health_probe__
Authorization: Bearer {api_key}
```

固定假任务 ID `__gateway_health_probe__`：

- Key 正确时预期为**业务层「任务不存在」**（典型 HTTP 404）。
- 证明：路径在 Seedance 协议面、Key 已过鉴权。
- **不**表示假任务存在，**不**表示视频生成可用。

### 4.2 明确不检测什么（Out of Scope）

| 不检测 | 说明 |
|--------|------|
| 视频创建能力 | 不 `POST .../tasks` |
| 任务轮询 / 素材 API | 不测 `/seedance-assets/*` |
| 模型名是否正确 | 不带 `model`，不校验 catalog |
| 额度 / 配额 | 无计费调用 |
| OpenAI 兼容性 | 不访问 `/models`、`/chat/completions` |

**运维解读**：成功 =「景汐中转连通且 Key 可用」；**不等于**「Seedance 视频业务全绿」。

### 4.3 成功 / 失败判定（统一）

```text
失败:
  - 传输层错误（连接拒绝、DNS、超时等）
  - HTTP 401 / 403（鉴权失败）
  - HTTP 5xx（上游不可用；避免「服务挂了仍显示绿」）

成功:
  - 请求完成，且状态码 ∉ {401, 403} ∪ [500, 599]
  - 典型期望：404（任务不存在）

跳过（不计失败，文案标明原因）:
  - 渠道 checkMode 含 model_forward：景汐「转发」段 skip
    （本探针本身已是连通+鉴权；不做视频创建）
  - 模型类型 video / video_generation：不走 chat ProbeModelForward；
    改为调用同一共享探针（渠道级语义），或 skip 并提示
    「视频模型无独立转发探针，请用渠道连通+鉴权检测」
```

> **实现前实测**：对真实景汐 `GET .../tasks/__gateway_health_probe__` 记录 Key 正确/错误时的状态码，写入探针注释。若实测与上表冲突，以实测为准并更新本表。

### 4.4 与 Bedrock 对照

| | Bedrock | 景汐 `seedance-relay` |
|--|---------|----------------------|
| 协议 | AWS SDK | HTTP Seedance |
| 连通性 | `ListFoundationModels` | `GET .../tasks/{假ID}` |
| 测什么 | 网络 + AWS 凭证 | 网络 + Bearer Key（+ 非 5xx） |
| 转发检测 | `Converse(max_tokens=1)` | **不做**（不耗视频额度） |
| 入口 | 定时 / 手动 / 模型检测应共用 Bedrock 实现 | **定时 / 手动 / 模型检测共用本探针** |

---

## 5. 代码落点

### 5.1 共享探针（新建，唯一实现）

建议位置（二选一，实现时取更贴现有结构者）：

- `internal/controlplane/service/seedance_relay_probe.go`，或
- 与 `model_probe.go` 并列的 `ProbeSeedanceRelayConnectivity(ctx, endpoint, apiKey)`

返回值建议：

```go
type ProbeResult struct {
    Success      bool
    ResponseTime int
    StatusCode   int
    // Message 必须可读，且标明语义，例如：
    // "ok: connectivity+auth only (GET tasks/__gateway_health_probe__; not video create)"
    // "auth failed: HTTP 401 ..."
    Message      string
}
```

函数注释硬性要求写清 §4.1 / §4.2。

### 5.2 调用方（只包装，不重写判定）

1. **`HealthCheckService`**
   - `isNonOpenAICompatible` 增加 `seedance-relay`
   - `checkNonOpenAIProvider`：`case "seedance-relay"` → 调共享探针；**禁止** fallthrough 到 Bedrock
   - `model_forward` / `both`：转发段 skip，日志用统一文案

2. **`TestHandler`（管理面测试连接）**
   - `nonOpenAICompatibleAdapters` 增加 `seedance-relay`，或改为调用 `IsNonOpenAICompatible`（避免双 map 漂移）
   - `testNonOpenAIProvider`：`case "seedance-relay"` → **同一** `ProbeSeedanceRelayConnectivity`
   - 响应里带 `message` / `check_scope` 字段说明「仅连通+鉴权」

3. **模型检测路径**
   - `doModelForwardCheck` / `model_check_handler`：`IsNonOpenAICompatible` 后按 definition 分发
     - `bedrock` → 现有 Converse
     - `seedance-relay` → 共享探针（或 skip + 明确文案）
     - **禁止** seedance → `DoBedrockModelCheckPublic`
   - `ProbeModelForward`：`video` / `video_generation` → skip，避免误打 chat

4. **同步模型**
   - `isUnsupportedAdapter` 增加 `seedance-relay`（及可选 `seedance-volcengine`），避免无意义打 `/models`

### 5.3 日志 / 前端可读性

- health log 成功 message 固定带 scope 说明（见 §5.1）
- 健康检查设置文案可保持「仅连通性」；景汐结果详情用探针 Message 区分「连通+鉴权、非视频创建」

### 5.4 测试

- 共享探针单测（mock HTTP）：404 成功、401 失败、503 失败、超时失败、200 成功
- 断言 `HealthCheckService` 与 `TestHandler` 对景汐都调用同一函数（或同包导出函数），且不再请求 `/models`
- 断言模型检测对 `seedance-relay` **不会**调用 Bedrock

---

## 6. 已识别缺口（纳入本期约束）

### 6.1 非 OpenAI ≠ Bedrock

`IsNonOpenAICompatible` 只表示「不要走 OpenAI 通用路径」，**必须再按 definition_id switch**。  
遗漏则景汐模型检测会打 Converse → 必失败或行为诡异。

### 6.2 双入口必须统一

定时探活与「测试连接」共用 §5.1；禁止只修 HealthCheck 不修 TestHandler。

### 6.3 渠道禁用后无自动恢复

渠道 `AutoDisable` 后，`CheckAll` 通常只扫 enabled，**不会自动重新启用**（与模型 auto-recovery 不同）。  
探针上线后：已误禁用的景汐需**人工重新启用**。运维说明与验收步骤写明。

### 6.4 5xx 记失败

避免上游宕机仍显示健康；与「鉴权通过即可」相比，更利于发现真实故障。

### 6.5 假 ID 响应以实测为准

落地前对景汐打一枪；若 Key 正确仍 401，改探针路径（另开变更），不硬猜。

### 6.6 `seedance-volcengine`

本期不做专属探针；若加入 non-OpenAI 集合，只能 skip 或独立 case，**禁止**进 Bedrock。

---

## 7. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 假 ID 返回 401 | 实测；必要时换只读 Action |
| 运维误读「健康=可生成」 | Message / 注释强制写 scope |
| 只改一处入口 | §2 / §5.2 强制双入口 + 单测 |
| 已禁用渠道仍不可用 | 文档要求手动 re-enable |
| 双 map（TestHandler vs service）漂移 | 尽量共用 `IsNonOpenAICompatible` |

---

## 8. 验收

1. 定时检查：景汐**不再**因 `/models` 404 被禁用。
2. 管理面「测试连接」与定时检查对同一渠道、同一 Key **结论一致**（同源探针）。
3. 模型「检测」不对景汐调用 Bedrock。
4. 错误 Key / 断网 / 5xx → 失败；正确 Key + 可达 + 非 5xx（如 404）→ 成功，且文案含「connectivity+auth only」。
5. 全程无探活用的 `POST .../contents/generations/tasks`。
6. 验收清单含：手动重新启用曾被误禁用的景汐渠道。

---

## 9. 待确认

请确认：

1. **检测 = 探测**（§2 统一探针）  
2. **§4 语义与 5xx 失败**  
3. **§6 缺口均纳入本期**

确认后进入实现计划与编码。
