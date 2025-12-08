# Data Model: 模型自动恢复 (Model Auto Recovery)

**Feature**: 001-model-auto-recovery | **Date**: 2026-06-10

本文档定义本功能涉及的数据模型变更。

---

## 1. Model 实体变更

### 现有字段（不变）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `uuid` | 主键 |
| `name` | `varchar(200)` | 模型名称 |
| `provider_id` | `uuid` | 所属渠道 |
| `enabled` | `bool` | 是否启用 |
| `consecutive_failures` | `int` | 连续失败次数 |
| `last_check_error` | `varchar(500)` | 最后检测错误 |
| ... | ... | 其他字段保持不变 |

### 新增字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `status` | `int` | `1` | 状态标识：1=启用，2=手动禁用，3=自动禁用 |
| `consecutive_recovery_successes` | `int` | `0` | 连续恢复成功次数（用于恢复判断） |

### GORM Model 定义

```go
// internal/controlplane/model/model.go

// ModelStatus 模型状态枚举
const (
    ModelStatusEnabled          = 1 // 启用
    ModelStatusManuallyDisabled = 2 // 手动禁用
    ModelStatusAutoDisabled     = 3 // 自动禁用（健康检测触发）
)

type Model struct {
    // ... 现有字段 ...
    
    // 新增字段
    Status                       int `gorm:"not null;default:1" json:"status"`
    ConsecutiveRecoverySuccesses int `gorm:"not null;default:0" json:"consecutive_recovery_successes"`
}
```

### 状态转换规则

```
                    ┌─────────────────────────────────────┐
                    │                                     │
                    ▼                                     │
    ┌───────────────────────────┐                        │
    │   status=1 (ENABLED)      │                        │
    │   enabled=true            │                        │
    └───────────────────────────┘                        │
           │              │                              │
           │              │                              │
    手动禁用           连续失败≥3次                   连续成功≥2次
           │              │                              │
           ▼              ▼                              │
    ┌─────────────┐  ┌─────────────┐                     │
    │ status=2    │  │ status=3    │ ────────────────────┘
    │ MANUALLY_   │  │ AUTO_       │  自动恢复
    │ DISABLED    │  │ DISABLED    │
    │ enabled=    │  │ enabled=    │
    │ false       │  │ false       │
    └─────────────┘  └─────────────┘
           │
           │ 手动启用
           │
           ▼
    返回 status=1, enabled=true
```

**关键规则**：
1. `status=2`（手动禁用）的模型**不参与**自动恢复检测
2. `status=3`（自动禁用）的模型**继续参与**健康检测，用于恢复判断
3. 自动恢复后，`status` 恢复为 `1`，`enabled` 恢复为 `true`

---

## 2. ModelStatusLog 新增实体

记录模型状态变更的完整历史。

### 表结构

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | `uuid` | PK, default gen_random_uuid() | 主键 |
| `model_id` | `uuid` | NOT NULL, INDEX | 模型 ID |
| `model_name` | `varchar(200)` | NOT NULL | 模型名称（冗余，便于展示） |
| `provider_id` | `uuid` | NOT NULL | 所属渠道 ID |
| `provider_name` | `varchar(200)` | NOT NULL | 渠道名称（冗余） |
| `change_type` | `varchar(20)` | NOT NULL | 变更类型 |
| `reason` | `varchar(500)` | NOT NULL DEFAULT '' | 变更原因描述 |
| `error_message` | `text` | DEFAULT '' | 错误信息（禁用时） |
| `operator_id` | `uuid` | NULL | 操作人 ID（手动操作时） |
| `operator_name` | `varchar(128)` | DEFAULT '' | 操作人名称 |
| `created_at` | `timestamptz` | NOT NULL | 变更时间 |

### 索引

- `idx_model_status_logs_model_id` ON `(model_id)` — 按模型查询
- `idx_model_status_logs_created_at` ON `(created_at)` — 按时间范围查询
- `idx_model_status_logs_change_type` ON `(change_type)` — 按类型筛选

### change_type 枚举值

| 值 | 说明 | 触发场景 |
|----|------|----------|
| `auto_disabled` | 自动禁用 | 健康检测连续失败达阈值 |
| `auto_recovered` | 自动恢复 | 被自动禁用的模型连续检测成功达阈值 |
| `manual_disabled` | 手动禁用 | 管理员在界面禁用模型 |
| `manual_enabled` | 手动启用 | 管理员在界面启用模型 |

### GORM Model 定义

```go
// internal/controlplane/model/model_status_log.go

package model

import (
    "time"
    "github.com/google/uuid"
)

// ChangeType 变更类型常量
const (
    ChangeTypeAutoDisabled   = "auto_disabled"
    ChangeTypeAutoRecovered  = "auto_recovered"
    ChangeTypeManualDisabled = "manual_disabled"
    ChangeTypeManualEnabled  = "manual_enabled"
)

// ModelStatusLog 模型状态变更日志
type ModelStatusLog struct {
    ID           uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    ModelID      uuid.UUID  `gorm:"type:uuid;not null;index:idx_model_status_logs_model_id" json:"model_id"`
    ModelName    string     `gorm:"type:varchar(200);not null" json:"model_name"`
    ProviderID   uuid.UUID  `gorm:"type:uuid;not null" json:"provider_id"`
    ProviderName string     `gorm:"type:varchar(200);not null" json:"provider_name"`
    ChangeType   string     `gorm:"type:varchar(20);not null;index:idx_model_status_logs_change_type" json:"change_type"`
    Reason       string     `gorm:"type:varchar(500);not null;default:''" json:"reason"`
    ErrorMessage string     `gorm:"type:text;default:''" json:"error_message"`
    OperatorID   *uuid.UUID `gorm:"type:uuid" json:"operator_id,omitempty"`
    OperatorName string     `gorm:"type:varchar(128);default:''" json:"operator_name"`
    CreatedAt    time.Time  `gorm:"not null;autoCreateTime;index:idx_model_status_logs_created_at" json:"created_at"`
}

func (ModelStatusLog) TableName() string {
    return "model_status_logs"
}
```

---

## 3. HealthCheckConfig 实体变更

### 新增字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `model_recovery_enabled` | `bool` | `true` | 是否启用模型自动恢复 |
| `model_recovery_success_threshold` | `int` | `2` | 恢复所需连续成功次数 |

### GORM Model 变更

```go
// internal/controlplane/model/health_check.go

type HealthCheckConfig struct {
    // ... 现有字段 ...
    
    // 新增字段
    ModelRecoveryEnabled          bool `gorm:"not null;default:true" json:"model_recovery_enabled"`
    ModelRecoverySuccessThreshold int  `gorm:"not null;default:2" json:"model_recovery_success_threshold"`
}
```

---

## 4. 数据库迁移 SQL

```sql
-- atlas-migrations/20260610XXXXXX_add_model_status.sql

-- 1. Model 表新增字段
ALTER TABLE models ADD COLUMN status INTEGER NOT NULL DEFAULT 1;
ALTER TABLE models ADD COLUMN consecutive_recovery_successes INTEGER NOT NULL DEFAULT 0;

-- 数据修复：将已禁用的模型标记为手动禁用
UPDATE models SET status = 2 WHERE enabled = false;

-- 2. 新增模型状态变更日志表
CREATE TABLE model_status_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id UUID NOT NULL,
    model_name VARCHAR(200) NOT NULL,
    provider_id UUID NOT NULL,
    provider_name VARCHAR(200) NOT NULL,
    change_type VARCHAR(20) NOT NULL,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    error_message TEXT DEFAULT '',
    operator_id UUID,
    operator_name VARCHAR(128) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_status_logs_model_id ON model_status_logs(model_id);
CREATE INDEX idx_model_status_logs_created_at ON model_status_logs(created_at);
CREATE INDEX idx_model_status_logs_change_type ON model_status_logs(change_type);

-- 3. HealthCheckConfig 表新增字段
ALTER TABLE health_check_configs ADD COLUMN model_recovery_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE health_check_configs ADD COLUMN model_recovery_success_threshold INTEGER NOT NULL DEFAULT 2;
```

---

## 5. 关联关系图

```
┌─────────────────┐         ┌─────────────────────┐
│    providers    │         │  health_check_      │
│                 │         │  configs            │
│  id             │         │                     │
│  name           │         │  model_recovery_    │
│  status (1/2/3) │◀───┐    │  enabled            │
│  ...            │    │    │  model_recovery_    │
└─────────────────┘    │    │  success_threshold  │
        │              │    └─────────────────────┘
        │ 1:N          │
        ▼              │
┌─────────────────┐    │
│     models      │    │
│                 │    │
│  id             │    │
│  provider_id    │────┘
│  status (1/2/3) │  NEW
│  consecutive_   │
│  recovery_      │  NEW
│  successes      │
│  ...            │
└─────────────────┘
        │
        │ 1:N
        ▼
┌─────────────────────┐
│ model_status_logs   │  NEW TABLE
│                     │
│  id                 │
│  model_id           │
│  model_name         │
│  provider_id        │
│  provider_name      │
│  change_type        │
│  reason             │
│  error_message      │
│  operator_id        │
│  operator_name      │
│  created_at         │
└─────────────────────┘
```
