# Data Model: 模型组授权与开放 (Group Authorization)

**Feature**: 003-group-authorization | **Date**: 2026-06-16

本文档定义本功能涉及的数据模型变更。

---

## 1. Group 实体变更（model_groups 表）

### 现有字段（不变）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `uint` PK | 主键 |
| `name` | `varchar(64)` | 模型组名称（唯一） |
| `description` | `text` | 描述 |
| `strategy` | `varchar(32)` | 调度策略 |
| `enabled` | `bool` | 是否启用 |
| `max_switches` | `int` | 最大切换次数 |
| `same_model_retries` | `int` | 同模型重试 |
| `failover_cooldown_seconds` | `int` | 故障冷却 |
| `timeout_seconds` | `int` | 超时时间 |
| ... | ... | 其他字段保持不变 |

### 新增字段

| 字段 | 类型 | 默认值 | 约束 | 说明 |
|------|------|--------|------|------|
| `open_mode` | `varchar(16)` | `'public'` | `NOT NULL; CHECK open_mode IN ('public','controlled')` | 开放模式 |

### GORM Model 定义

```go
// internal/controlplane/model/group.go

type Group struct {
    // ... 现有字段不变 ...

    // 新增字段
    OpenMode string `gorm:"type:varchar(16);not null;default:'public';check:open_mode IN ('public','controlled')" json:"open_mode"`
}
```

---

## 2. GroupEntitlement 新增实体（group_entitlements 表）

用统一一张表表达两级授权（工作室级、部门级），靠 `scope_type` 区分：

### 表结构

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | `uuid` | PK, DEFAULT gen_random_uuid() | 主键 |
| `group_id` | `uint` | NOT NULL | 关联 model_groups.id |
| `scope_type` | `varchar(16)` | NOT NULL | `studio` / `dept` |
| `scope_id` | `varchar(64)` | NOT NULL | studio UUID 或 SSO dept_id |
| `enabled` | `bool` | NOT NULL DEFAULT true | 是否有效 |
| `granted_by` | `uuid` | NULL | 操作人 user ID |
| `created_at` | `timestamptz` | NOT NULL | 创建时间 |
| `updated_at` | `timestamptz` | NOT NULL | 更新时间 |

### 索引

- `idx_ent_unique` UNIQUE ON `(group_id, scope_type, scope_id)` — 防重复授权
- `idx_ent_scope` ON `(scope_type, scope_id)` — 按工作室/部门查询
- `idx_ent_group_id` ON `(group_id)` — 按模型组查询

### scope_type 含义

| scope_type | 含义 | 谁能写 |
|-----------|------|--------|
| `studio` | 平台把受控组下放给某工作室 | 平台管理员（`role=admin`） |
| `dept` | 工作室/部门负责人为某部门开通 | 工作室负责人（`role=studio_admin`）或部门负责人（`role=dept_admin`） |

### GORM Model 定义

```go
// internal/controlplane/model/group_entitlement.go

package model

import (
    "time"

    "github.com/google/uuid"
)

// EntitlementScopeType 授权域类型常量
const (
    EntitlementScopeStudio = "studio"
    EntitlementScopeDept   = "dept"
)

// GroupEntitlement 模型组授权记录
type GroupEntitlement struct {
    ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    GroupID   uint      `gorm:"not null;uniqueIndex:idx_ent_unique" json:"group_id"`
    ScopeType string    `gorm:"type:varchar(16);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_type"`
    ScopeID   string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_id"`
    Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
    GrantedBy *uuid.UUID `gorm:"type:uuid" json:"granted_by,omitempty"`
    CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
    UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (GroupEntitlement) TableName() string { return "group_entitlements" }
```

---

## 3. UserIdentity 变更（网关内存结构）

### 现有字段

```go
type UserIdentity struct {
    UserID          string `json:"user_id"`
    KeyID           string `json:"key_id"`
    DepartmentID    string `json:"department_id"`
    StudioID        string `json:"studio_id,omitempty"`
    AccessAllGroups bool   `json:"access_all_groups"`
    LogEnabled      bool   `json:"log_enabled"`
}
```

### 新增字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `UsableGroups` | `[]string` | 可用模型组名集合（控制面计算后下发） |

### 变更后定义

```go
// internal/gateway/model/identity.go

type UserIdentity struct {
    UserID          string   `json:"user_id"`
    KeyID           string   `json:"key_id"`
    DepartmentID    string   `json:"department_id"`
    StudioID        string   `json:"studio_id,omitempty"`
    AccessAllGroups bool     `json:"access_all_groups"`
    LogEnabled      bool     `json:"log_enabled"`
    UsableGroups    []string `json:"usable_groups"` // NEW: public ∪ dept已开通controlled
}
```

---

## 4. VerifyKeyResponse 变更（internal/auth/dto）

### 新增字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `usable_groups` | `[]string` | 控制面计算的可用组集合 |

### 变更后定义（Data 内层）

```go
// internal/auth/dto/internal.go

type VerifyKeyResponse struct {
    Code      int    `json:"code"`
    Message   string `json:"message"`
    RequestID string `json:"request_id,omitempty"`
    Data      struct {
        Status          string   `json:"status"`
        KeyID           string   `json:"key_id,omitempty"`
        KeyName         string   `json:"key_name,omitempty"`
        AccessAllGroups bool     `json:"access_all_groups"`
        LogEnabled      *bool    `json:"log_enabled,omitempty"`
        UsableGroups    []string `json:"usable_groups"` // NEW
        User            *struct {
            UserID       string `json:"user_id"`
            Name         string `json:"name"`
            DepartmentID string `json:"department_id,omitempty"`
            DeptName     string `json:"dept_name,omitempty"`
            StudioID     string `json:"studio_id,omitempty"`
            StudioName   string `json:"studio_name,omitempty"`
        } `json:"user,omitempty"`
    } `json:"data"`
}
```

---

## 5. 可用集合计算逻辑

```sql
-- UsableGroups(dept_id, studio_id) 的 SQL 等价表达

-- 所有公开组
SELECT name FROM model_groups 
WHERE open_mode = 'public' AND enabled = true AND deleted_at IS NULL

UNION

-- 已开通的受控组（dept 行 enabled AND 对应 studio 行 enabled）
SELECT mg.name FROM model_groups mg
JOIN group_entitlements ge_dept 
  ON ge_dept.group_id = mg.id 
  AND ge_dept.scope_type = 'dept' 
  AND ge_dept.scope_id = :dept_id 
  AND ge_dept.enabled = true
JOIN group_entitlements ge_studio 
  ON ge_studio.group_id = mg.id 
  AND ge_studio.scope_type = 'studio' 
  AND ge_studio.scope_id = :studio_id 
  AND ge_studio.enabled = true
WHERE mg.open_mode = 'controlled' 
  AND mg.enabled = true 
  AND mg.deleted_at IS NULL
```

---

## 6. 关联关系图

```
┌─────────────────────┐          ┌─────────────────────┐
│    studios          │          │   departments       │
│                     │          │                     │
│  id (uuid)          │          │  dept_id (varchar)  │
│  name               │◀─ 1:N ──│  studio_id (uuid)   │
│  owner_id           │          │  dept_name          │
└─────────────────────┘          └─────────────────────┘
         │                                │
         │ scope_id                       │ scope_id
         ▼                                ▼
┌───────────────────────────────────────────────────────┐
│              group_entitlements  (NEW)                 │
│                                                       │
│  id (uuid) PK                                         │
│  group_id   ──→ model_groups.id                       │
│  scope_type     (studio | dept)                       │
│  scope_id       (studio.id | departments.dept_id)     │
│  enabled        (bool)                                │
│  granted_by     (操作人 user_id)                      │
│  created_at / updated_at                              │
│                                                       │
│  UNIQUE (group_id, scope_type, scope_id)              │
└───────────────────────────────────────────────────────┘
         │
         │ group_id
         ▼
┌─────────────────────┐        ┌─────────────────────┐
│  model_groups       │        │  api_keys           │
│  (改造)             │        │                     │
│                     │        │  id (uuid)          │
│  id (uint) PK       │        │  user_id            │
│  name               │        │  dept_id            │
│  open_mode (NEW)    │        │  studio_id          │
│  enabled            │        │  access_all_groups  │
│  strategy           │        └─────────────────────┘
│  ...                │                │
└─────────────────────┘                │
         │                             │
         │ 1:N                         │ key_group_bindings (并存)
         ▼                             ▼
┌─────────────────────┐        ┌─────────────────────┐
│  group_models       │        │ key_group_bindings  │
│                     │        │ (保留，作为缩小子集)│
│  group_id           │        │ key_id → group_id   │
│  model_id           │        └─────────────────────┘
│  priority / weight  │
└─────────────────────┘
```

---

## 7. 数据库迁移 SQL

```sql
-- migrations/NNN_add_group_open_mode_and_entitlements.sql

-- 1. model_groups 表新增 open_mode 字段
ALTER TABLE model_groups ADD COLUMN open_mode VARCHAR(16) NOT NULL DEFAULT 'public';
ALTER TABLE model_groups ADD CONSTRAINT chk_model_groups_open_mode CHECK (open_mode IN ('public', 'controlled'));

-- 2. 新建 group_entitlements 表
CREATE TABLE group_entitlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id INTEGER NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    scope_id VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_group_entitlements_group FOREIGN KEY (group_id) REFERENCES model_groups(id) ON DELETE CASCADE
);

-- 唯一约束：同一组+同一域类型+同一域ID 不可重复
CREATE UNIQUE INDEX idx_ent_unique ON group_entitlements(group_id, scope_type, scope_id);

-- 按域查询（如查某工作室被授权的所有组）
CREATE INDEX idx_ent_scope ON group_entitlements(scope_type, scope_id);

-- 按组查询（如查某组的所有授权记录）
CREATE INDEX idx_ent_group_id ON group_entitlements(group_id);

-- 3. 存量数据迁移：所有组默认 public（字段 DEFAULT 已处理，无需额外 UPDATE）
-- 上线后管理员按需将需管控的组改为 controlled
```

---

## 8. 状态转换规则

### 模型组开放模式变更

```
┌─────────────────────────────┐
│  open_mode = 'public'       │
│  全员自动可见可用             │
└─────────────────────────────┘
         │              ▲
    管理员设为          管理员设为
    controlled          public
         │              │
         ▼              │
┌─────────────────────────────┐
│  open_mode = 'controlled'   │
│  默认不可见不可用             │
│  须逐级授权后才可用          │
└─────────────────────────────┘
```

### 授权生效判定（联级）

```
受控组 G 对部门 D 可用？

  ① G.open_mode = 'controlled'?     → No:  G是public，直接可用
  ② group_entitlements 有 (G, studio, S, enabled=true)?
     （S = D所属工作室）            → No:  工作室未被授权，403
  ③ group_entitlements 有 (G, dept, D, enabled=true)?
                                     → No:  部门未开通，403
  ④ 全部满足                        → 可用
```

### 级联回收

```
管理员禁用 studio 行 (G, studio, S, enabled=false)
         ↓
联级判定：dept 行即使 enabled=true，
因 studio 行 enabled=false → 判定为不可用
         ↓
S 下所有部门即时失效（无需逐行修改 dept 行）
```
