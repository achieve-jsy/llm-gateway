# Seedance 全链路调用日志设计

日期：2026-07-10  
状态：已确认  
范围：网关 `/v1` 下 Seedance 中转（`seedance-relay`）与官方（`seedance-volcengine`）的视频任务与素材库请求，补齐 `call_logs` 可检索字段。

---

## 1. 问题

`UsageCollector` 已挂在 `/v1` 组，但：

- **CreateTask** 只写了组名 `model`，未带操作后缀，也未写 `routed_model`
- **GetTask** / **Asset** 未写 `model` / `provider`，用量页几乎不可见（空 model 行）

## 2. 目标

每个相关请求写入可检索的 `model` / `provider` / `routed_model`，复用现有 `call_logs`，不计费（token/cost 仍为 0）。

## 3. 字段约定

| 请求 | model | provider | routed_model |
|------|-------|----------|--------------|
| `POST /v1/contents/generations/tasks` | `{组名}:CreateTask` | 渠道展示名 | 上游模型 ID |
| `GET /v1/contents/generations/tasks/:id` | `{组名}:GetTask`；反查失败 → `seedance-task:GetTask` | 渠道展示名 | 有则填上游 ID |
| `POST /v1/assets/:action` | `seedance-asset:{Action}` | 渠道展示名（无则用 type） | `seedance-relay` 或 `seedance-volcengine` |

说明：

- `VideoTaskRecord.Model` / Redis / PG 仍存**纯组名**（不含 `:CreateTask`），供 GetTask 反查。
- 中转与官方走同一套埋点；`provider` / `routed_model` 区分实际渠道。

## 4. 实现要点

1. Handler 内 `c.Set`，由 `UsageCollector` → `call_logs`。
2. 扩展任务元数据反查：返回 `model` + `provider_type`（扩展现有 internal `provider-type` 响应）。
3. 不新建表、不改用量 UI、不改计费。

## 5. 非目标

- 真人认证 H5 回调日志
- 用量页 UI 改造 / 按 action 计费
- 改 `video_tasks` 表结构
