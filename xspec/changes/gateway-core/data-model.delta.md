# gateway-core 数据模型变更 — CHG-20260609-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0)（无 data-model.md 基线）

## 变更摘要

gateway-core 本身不新增任何数据库表，仅在内存数据结构（Go struct）上扩展 `VerifyKeyResponse`，并定义 context key 常量。

---

## 修改

### VerifyKeyResponse 扩展

```go
// internal/gateway/infra/auth_client.go

// 变更前
type VerifyKeyResponse struct {
    UserID        string   `json:"user_id"`
    AllowedGroups []string `json:"allowed_groups"`
}

// 变更后（新增字段，JSON 反序列化向后兼容）
type VerifyKeyResponse struct {
    UserID        string   `json:"user_id"`
    AllowedGroups []string `json:"allowed_groups"`
    DeptID        string   `json:"dept_id"`    // 新增
    StudioID      string   `json:"studio_id"`  // 新增
}
```

### Gin Context Key 常量

```go
// internal/gateway/middleware/org_check.go（新增文件）
const (
    CtxKeyDeptID   = "org_dept_id"
    CtxKeyStudioID = "org_studio_id"
)
```

## 新增

无（不新增数据库表，gateway 不直连 PG）。

## 不变

KeyCache、ProviderLoader、所有现有中间件数据结构均不变。
