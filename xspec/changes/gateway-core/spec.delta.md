# gateway-core 需求变更 — CHG-20260609-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0)

## 变更摘要

在 Key 验证通过后将组织身份信息（user→dept→studio）注入请求上下文，供 observability-hub 埋点使用；同时新增前置拦截：pending_studio 状态的员工调用模型时返回明确的 403 提示。

---

## 新增需求

### N1. pending_studio 前置拦截（P0）

- Key 验证通过后，检查当前用户是否处于 `pending_studio` 状态（`dept_id` 有值但 `studio_id` 为空）
- 若是，在调度前返回 403，错误体格式：
  ```json
  {
    "error": {
      "code": "org_pending_studio",
      "message": "您所在部门尚未配置工作室归属，请联系管理员",
      "type": "organization_error"
    }
  }
  ```
- 此拦截发生在鉴权通过、调度之前（AuthMiddleware 后、Scheduler 前）

### N2. 身份链注入请求上下文（P0）

- `verify-key` 响应中新增 `dept_id`、`studio_id` 字段（由 control-plane M1 变更提供）
- 将这两个字段注入 Gin context，供后续 observability 埋点读取
- gateway-core 本身不持久化、不解析组织逻辑，只做透传

---

## 修改需求

### M1. AuthClient 响应结构扩展（P0）

- 原：`VerifyKeyResponse` 含 `UserID`、`AllowedGroups`
- 改：新增 `DeptID`（string）、`StudioID`（string）字段；字段为空时不影响现有逻辑

---

## 新增验收标准

| 编号 | Given | When | Then | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| AC-GW-N1 | 员工 dept 已归集但 dept_studio_map 中无映射（pending_studio） | 发起任意模型调用 | 返回 403，body 含 `org_pending_studio` 错误码和人可读提示 | P0 |
| AC-GW-N2 | 员工 dept 已正确映射工作室 | 发起模型调用 | pending_studio 拦截不触发，请求正常调度 | P0 |
| AC-GW-N3 | verify-key 响应含 dept_id 和 studio_id | 任意鉴权通过的请求 | Gin context 中可读取 dept_id 和 studio_id 值 | P0 |

---

## 修改验收标准

- AC-01（原：Key 验证通过）→ 增加后置断言：「Gin context 中 dept_id 字段被正确注入」
