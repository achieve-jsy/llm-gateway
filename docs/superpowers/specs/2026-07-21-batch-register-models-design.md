# 注册模型时多选渠道批量创建

## 目标

注册模型时渠道支持多选：同一模型配置一次挂到多个渠道。部分成功（能建则建，已存在跳过），最后汇总。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/models/batch` | 手填字段批量创建 |
| POST | `/api/v1/models/batch-from-catalog` | 从一条 Catalog 批量创建 |

请求：在现有单条字段基础上，用 `provider_ids: string[]`（必填，≥1）替代 `provider_id`。服务端去重。

响应（HTTP 200，除非整请求非法）：

```json
{
  "data": {
    "success_count": 2,
    "skip_count": 1,
    "fail_count": 0,
    "results": [
      { "provider_id": "...", "status": "created", "model_id": "..." },
      { "provider_id": "...", "status": "skipped", "reason": "model already exists" },
      { "provider_id": "...", "status": "failed", "reason": "..." }
    ]
  }
}
```

`status`：`created` | `skipped` | `failed`。

## 服务端逻辑

- 对每个 provider：复用 `CreateModel` / `CreateModelFromCatalog`；已存在 → skipped；渠道无效 → failed。
- `batch-from-catalog`：Catalog 不存在 → 整请求 404（与单条一致）。
- 不包大事务；每条成功仍触发 ModelCreated + snapshot。

## 前端

- `ModelDialog` 注册态：渠道多选；编辑态保持单选。
- 提交走批量接口；标签对成功创建的 model_id 调用 `batchSetModelTags`。
- 汇总 toast：成功 X / 跳过 Y / 失败 Z；关闭弹窗并刷新列表。

## 非目标

- 不改上游 sync、批量编辑、默认启用策略。
