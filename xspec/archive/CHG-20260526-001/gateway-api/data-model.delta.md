# gateway-api 数据模型变更 — CHG-20260526-001

> 基线：xspec/modules/gateway-api/data-model.md (v1.2)

## 变更摘要

gateway-api 本模块为无状态接入层，不持有业务数据表。本次变更新增外部数据消费关系：通过 HTTP 调用 control-plane 接口实时获取 Key 绑定的模型组列表。无数据库变更。

## 新增数据消费

| 数据源 | 消费方式 | 用途 | 数据内容 |
| :--- | :--- | :--- | :--- |
| control-plane `/internal/v1/keys/:id/groups` | HTTP GET，500ms 超时 | 获取 Key 绑定的模型组列表及启停状态 | `[{id, name, enabled}]` |

## 新增内存结构

### BoundGroupInfo

gateway-api 消费 control-plane 响应后的内存表示：

```go
// model/bound_group.go

type BoundGroupInfo struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Enabled bool   `json:"enabled"`
}
```

### 消费上下文（修改）

在 identity 上下文中消费 key_id 并作为调用参数：

```go
// 调度上下文中使用
identity := Identity{
    KeyID:   c.GetString("key_id"),    // AuthMiddleware 注入，已有
    UserID:  c.GetString("user_id"),   // 已有
    DeptID:  c.GetString("dept_id"),   // 已有
}
```

## 缓存策略

| 数据 | 缓存 | TTL | 说明 |
| :--- | :--- | :--- | :--- |
| Key 绑定组列表 | 不缓存 | — | 每次实时查询 control-plane，保证绑定变更即时生效；后续高频场景按需补 Redis 缓存 |

## 对现有数据模型的影响

- `GroupNameCache`（v1.2 引入）不变：继续缓存组名与组内模型映射
- `DispatchContext`（v1.2 引入）不变：调度上下文结构无变更
- 无表结构变更、无迁移操作