# gateway-core 接口契约变更 — CHG-20260609-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0)

## 变更摘要

`/v1/*` 对外接口新增一种 403 错误响应（org_pending_studio）；内部 verify-key 调用响应结构扩展两个字段。

---

## 修改接口

### POST /v1/chat/completions、POST /v1/embeddings — 新增 403 错误

**新增错误响应**（pending_studio 状态员工）：

```http
HTTP/1.1 403 Forbidden
Content-Type: application/json

{
  "error": {
    "code": "org_pending_studio",
    "message": "您所在部门尚未配置工作室归属，请联系管理员",
    "type": "organization_error"
  }
}
```

触发条件：通过鉴权的用户 `dept_id` 非空且 `studio_id` 为空（OrgCheckMiddleware 拦截）。

其余响应格式（200、401、503 等）不变，向后兼容。

---

## 内部依赖变更（非对外接口）

### verify-key 响应消费

gateway-core 的 `AuthClient` 消费 control-plane 的 verify-key 响应，新增读取 `dept_id` 和 `studio_id` 字段（详见 `data-model.delta.md`）。

变更对 gateway-core 对外接口透明，外部调用者无感知。

---

## 新增接口

不适用

## 废弃接口

不适用
