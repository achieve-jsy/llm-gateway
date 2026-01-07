# Task 1: 数据模型 + 迁移

## 背景

模型组授权与开放功能需要对数据库进行两项变更：
1. `model_groups` 表新增 `open_mode` 字段（`public` / `controlled`，默认 `public`）
2. 新建 `group_entitlements` 表（模型组授权记录，统一表达工作室级和部门级授权）

本任务为其他所有任务的前置依赖。完成后其他任务才能基于新表结构开发。

## 需要完成的功能

### 1. Group 模型新增 `open_mode` 字段

文件：`internal/controlplane/model/group.go`

```go
type Group struct {
    // ... 现有字段不变 ...
    OpenMode string `gorm:"type:varchar(16);not null;default:'public';check:open_mode IN ('public','controlled')" json:"open_mode"`
}
```

### 2. 新建 GroupEntitlement 模型

新文件：`internal/controlplane/model/group_entitlement.go`

```go
package model

import (
    "time"
    "github.com/google/uuid"
)

const (
    EntitlementScopeStudio = "studio"
    EntitlementScopeDept   = "dept"
)

type GroupEntitlement struct {
    ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    GroupID   uint       `gorm:"not null;uniqueIndex:idx_ent_unique" json:"group_id"`
    ScopeType string     `gorm:"type:varchar(16);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_type"`
    ScopeID   string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_id"`
    Enabled   bool       `gorm:"not null;default:true" json:"enabled"`
    GrantedBy *uuid.UUID `gorm:"type:uuid" json:"granted_by,omitempty"`
    CreatedAt time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
    UpdatedAt time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (GroupEntitlement) TableName() string { return "group_entitlements" }
```

### 3. 数据库迁移 SQL

使用项目 Atlas 迁移工作流（参考 `migrations/` 目录现有迁移的命名规范）生成迁移文件。

SQL 内容：

```sql
-- model_groups 表新增 open_mode 字段
ALTER TABLE model_groups ADD COLUMN open_mode VARCHAR(16) NOT NULL DEFAULT 'public';
ALTER TABLE model_groups ADD CONSTRAINT chk_model_groups_open_mode CHECK (open_mode IN ('public', 'controlled'));

-- 新建 group_entitlements 表
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

CREATE UNIQUE INDEX idx_ent_unique ON group_entitlements(group_id, scope_type, scope_id);
CREATE INDEX idx_ent_scope ON group_entitlements(scope_type, scope_id);
CREATE INDEX idx_ent_group_id ON group_entitlements(group_id);
```

### 4. 验证

- `go build ./...` 通过
- 迁移可以正常 apply（本地数据库）
- 存量组默认 `open_mode='public'`（由 DEFAULT 保证）

## 关键约定

- 迁移文件必须走 **Atlas 迁移工作流**（项目有对应 skill：`atlas-db-migration`）
- 迁移序号递增（查看 `migrations/` 目录最后一个文件序号 +1）
- 不要修改其他现有字段
- `group_id` 外键指向 `model_groups(id)`，级联删除

## 相关文件

- `internal/controlplane/model/group.go` — Group 模型
- `migrations/` — 迁移目录
- `specs/003-group-authorization/data-model.md` — 完整数据模型设计
