# org-service 接口契约

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260609-001 |
| **模块名称** | org-service |
| **创建时间** | 2026-06-09 |

---

## 1. 概览

org-service 对外通过 control-plane 路由暴露两类接口：
- **管理接口**（`/api/v1/org/*`）：JWTAuth，供 ops-console 调用
- **内部接口**（`/internal/org/*`）：InternalToken，供 control-plane 其他模块和 gateway-core 调用

---

## 2. 公共约定

- 所有接口均以 JSON 格式收发
- 鉴权错误统一返回 `401`；权限不足返回 `403`
- 业务错误响应格式：
  ```json
  {"code": "org.xxx", "message": "人可读描述", "details": {...}}
  ```
- 分页参数：`?page=1&page_size=20`（默认 page_size=20，最大 100）
- 时间格式：RFC3339

---

## 3. 端点索引

| 端点 | 方法 | 鉴权 | 说明 |
| :--- | :--- | :--- | :--- |
| /api/v1/org/studios | GET/POST | JWTAuth admin | 工作室列表/创建 |
| /api/v1/org/studios/:id | GET | JWTAuth | 工作室详情 |
| /api/v1/org/departments | GET | JWTAuth admin | 部门列表 |
| /api/v1/org/departments/unmapped | GET | JWTAuth admin | 未映射清单 |
| /api/v1/org/departments/:id/studio | PUT | JWTAuth admin | 手工设置映射 |
| /api/v1/org/dept-studio-map | GET | JWTAuth admin | 映射列表 |
| /api/v1/org/dept-studio-map/import | POST | JWTAuth admin | CSV 批量导入 |
| /api/v1/org/roles/grant | POST | JWTAuth | 派发角色 |
| /api/v1/org/roles/revoke | POST | JWTAuth | 卸任角色 |
| /api/v1/org/roles/:user_id | GET | JWTAuth admin | 查询用户角色 |
| /internal/org/auth/check-scope | POST | InternalToken | 管辖域鉴权 |
| /internal/org/tree | GET | InternalToken | 完整组织树 |

---

## 4. 端点详细设计

### 2.1 GET /api/v1/org/studios — 工作室列表

**权限**：admin

**响应 200**：
```json
{
  "studios": [
    {"id": "S001", "name": "研发工作室", "owner_id": "u123", "dept_count": 3, "created_at": "..."}
  ],
  "total": 5,
  "page": 1,
  "page_size": 20
}
```

### 2.2 POST /api/v1/org/studios — 创建工作室

**权限**：admin

**请求体**：
```json
{"name": "研发工作室"}
```

**响应 201**：
```json
{"id": "S001", "name": "研发工作室", "created_at": "..."}
```

**错误**：
- `422 org.duplicate_name` — 工作室名已存在

### 2.3 GET /api/v1/org/studios/:id — 工作室详情

**权限**：admin 或对应 studio_admin

**响应 200**：
```json
{
  "id": "S001",
  "name": "研发工作室",
  "owner_id": "u123",
  "departments": [
    {"id": "D001", "name": "前端部", "status": "active", "member_count": 8}
  ]
}
```

### 2.4 GET /api/v1/org/departments — 部门列表

**权限**：admin

**Query 参数**：`?status=active|pending_studio`（可选，不传返回全部）

**响应 200**：
```json
{
  "departments": [
    {"id": "D001", "name": "前端部", "studio_id": "S001", "studio_name": "研发工作室", "status": "active", "member_count": 8}
  ],
  "total": 12
}
```

### 2.5 GET /api/v1/org/departments/unmapped — 未映射部门清单

**权限**：admin

**响应 200**：
```json
{
  "departments": [
    {"id": "D010", "name": "新业务部", "member_count": 3}
  ],
  "total": 2
}
```

### 2.6 PUT /api/v1/org/departments/:id/studio — 手工设置映射

**权限**：admin

**请求体**：
```json
{"studio_id": "S001"}
```

**响应 200**：
```json
{"dept_id": "D010", "studio_id": "S001", "source": "manual", "updated_at": "..."}
```

**错误**：
- `404 org.studio.not_found` — 目标工作室不存在

---

## 3. 映射导入接口

### 3.1 GET /api/v1/org/dept-studio-map — 映射列表

**权限**：admin

**响应 200**：
```json
{
  "mappings": [
    {"dept_id": "D001", "dept_name": "前端部", "studio_id": "S001", "studio_name": "研发工作室", "source": "import", "updated_at": "..."}
  ],
  "total": 10
}
```

### 3.2 POST /api/v1/org/dept-studio-map/import — CSV 批量导入

**权限**：admin

**请求**：`Content-Type: multipart/form-data`
- `file`：CSV 文件（UTF-8，首行 header：`dept_id,studio_id`，最大 1MB，500 条）
- `dry_run`：`true` 时仅预览 diff，不写入（默认 `false`）

**响应 200**（dry_run=true）：
```json
{
  "dry_run": true,
  "diff": {
    "added": [{"dept_id": "D010", "studio_id": "S002"}],
    "updated": [{"dept_id": "D001", "old_studio_id": "S001", "new_studio_id": "S002"}],
    "unchanged": 8
  }
}
```

**响应 200**（dry_run=false，写入成功）：
```json
{
  "dry_run": false,
  "updated": 2,
  "skipped": 0,
  "backfilled": 1
}
```

**错误**：
- `422 org.import.invalid_studio` — 含非法 studio_id，响应体包含 `invalid_rows: [{line: 3, dept_id: "...", studio_id: "INVALID"}]`
- `413 org.import.file_too_large` — 文件超过 1MB
- `422 org.import.too_many_rows` — 超过 500 条

---

## 4. 角色管理接口

### 4.1 POST /api/v1/org/roles/grant — 派发角色

**权限**：admin（全范围）；studio_admin（仅本工作室内的 dept_admin）

**请求体**：
```json
{
  "user_id": "u456",
  "role": "dept_admin",
  "scope_type": "dept",
  "scope_id": "D001"
}
```

**响应 200**：
```json
{"user_id": "u456", "role": "dept_admin", "scope_id": "D001", "granted_at": "..."}
```

**错误**：
- `403 org.forbidden` — 超出派发者管辖域
- `404 org.user.not_found` — 目标用户不存在
- `422 org.role.invalid_scope` — scope_id 指向不存在的组织节点

### 4.2 POST /api/v1/org/roles/revoke — 卸任角色

**权限**：admin；studio_admin（仅本工作室内）

**请求体**：
```json
{"user_id": "u456", "role": "dept_admin", "scope_id": "D001"}
```

**响应 200**：
```json
{"user_id": "u456", "role": "user", "revoked_at": "..."}
```

**错误**：
- `403 org.forbidden` — 超出操作者管辖域
- `404 org.role.not_found` — 该用户没有指定角色记录

### 4.3 GET /api/v1/org/roles/:user_id — 查询用户角色

**权限**：admin

**响应 200**：
```json
{
  "user_id": "u456",
  "roles": [
    {"role": "dept_admin", "scope_type": "dept", "scope_id": "D001", "scope_name": "前端部", "granted_by": "u123", "created_at": "..."}
  ]
}
```

---

## 5. 内部接口（InternalToken 认证）

### 5.1 POST /internal/org/auth/check-scope — 管辖域鉴权

供 control-plane 内部其他 handler 使用。

**请求体**：
```json
{"operator_id": "u123", "target": {"dept_id": "D001", "studio_id": "S001"}}
```

**响应 200（通过）**：
```json
{"pass": true}
```

**响应 200（拒绝）**：
```json
{"pass": false, "reason": "操作者 studio_admin 的管辖域不包含目标 dept"}
```

### 5.2 GET /internal/org/tree — 完整组织树

供 control-plane 内部（如 verify-key 扩展、Dashboard API）查询组织信息。

**响应 200**：
```json
{
  "company": {
    "studios": [
      {
        "id": "S001", "name": "研发工作室",
        "departments": [
          {"id": "D001", "name": "前端部", "status": "active", "member_count": 8}
        ]
      }
    ],
    "unmapped_departments": [
      {"id": "D010", "name": "新业务部", "status": "pending_studio"}
    ]
  }
}
```

---

## 5. 错误码

| 错误码 | HTTP | 说明 |
| :--- | :--- | :--- |
| `org.forbidden` | 403 | 超出管辖域 |
| `org.duplicate_name` | 422 | 工作室/部门名重复 |
| `org.studio.not_found` | 404 | 工作室不存在 |
| `org.studio.has_departments` | 409 | 工作室下有部门，不可删除 |
| `org.user.not_found` | 404 | 用户不存在 |
| `org.role.not_found` | 404 | 角色记录不存在 |
| `org.role.invalid_scope` | 422 | scope_id 指向不存在的组织节点 |
| `org.import.invalid_studio` | 422 | CSV 含非法 studio_id |
| `org.import.file_too_large` | 413 | 文件超 1MB |
| `org.import.too_many_rows` | 422 | 超过 500 条 |

---

## 6. 版本策略与 Breaking Change 规则

- 当前为 v1，路径前缀 `/api/v1/org/`
- 不允许删除或修改已有字段语义（宪章原则二）
- 新增字段须向后兼容（非必填、有合理默认值）
- Breaking Change 须提前一个版本废弃并通知消费方

---

## 7. 非功能约束（适用时填）

| 约束 | 值 |
| :--- | :--- |
| 管理接口响应 P95 | < 500ms |
| 鉴权接口响应 P95 | < 10ms |
| 映射导入（500条）| < 5s |

---

## 8. 接口安全

- 管理接口：JWTAuth（`Authorization: Bearer <token>`）+ 服务端 CheckScope 双重校验
- 内部接口：`X-Internal-Token` header 校验
- 所有写操作记录 `actor_id`（v0.8.0 审计预留）
- CSV 内容不执行，studio_id 存在性校验防注入

---

## 9. 变更历史

| 版本 | 变更 | 变更ID |
| :--- | :--- | :--- |
| v1.0 | 初始版本 | CHG-20260609-001 |
