# Research: 模型自动恢复 (Model Auto Recovery)

**Feature**: 001-model-auto-recovery | **Date**: 2026-06-10

本文档记录技术决策的研究过程和结论。

---

## 决策 1: 如何区分"手动禁用"和"自动禁用"

### 背景

当前 Model 实体只有 `Enabled *bool` 字段，无法区分模型是被管理员手动禁用还是被健康检测自动禁用。需求要求：
- 只对"自动禁用"的模型尝试自动恢复
- 手动禁用的模型应尊重用户操作，不自动恢复

### 方案对比

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| **A: 隐式判断** | 用 `Enabled + ConsecutiveFailures` 组合判断 | 无需改表结构 | 语义不明确，恢复条件复杂 |
| **B: 新增 DisabledReason** | 新增 `disabled_reason varchar(20)` 字段 | 语义清晰，改动小 | 需要迁移，与 Provider 模式不一致 |
| **C: 参考 Provider 的 Status** | 新增 `status int` 字段，值为 1/2/3 | 与现有模式一致，语义清晰 | 需要迁移，改动稍大 |

### 决策

**选择方案 C**：为 Model 新增 `Status int` 字段

**理由**：
1. **一致性**：Provider 已有 `Status` 字段设计（`status=1` 启用，`status=2` 手动禁用，`status=3` 自动禁用），Model 采用相同模式便于理解和维护
2. **可扩展**：整数类型方便未来扩展更多状态
3. **查询简单**：`WHERE status = 3` 直接筛选自动禁用的模型

**现有参考实现** (`internal/controlplane/repository/provider_repo.go`):
```go
// UpdateStatus 更新渠道启用状态
func (r *ProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error {
    status := 2 // MANUALLY_DISABLED
    if enabled {
        status = 1 // ENABLED
    }
    return r.db.Model(&model.Provider{}).Where("id = ?", id).Updates(map[string]interface{}{
        "enabled": enabled,
        "status":  status,
    }).Error
}

// AutoDisable 设置 status=3（自动禁用）并关闭 enabled
func (r *ProviderRepo) AutoDisable(id uuid.UUID) error {
    return r.db.Model(&model.Provider{}).Where("id = ?", id).Updates(map[string]interface{}{
        "enabled": false,
        "status":  3,
    }).Error
}
```

### 数据迁移策略

1. 新增 `status` 字段，默认值为 `1`（启用）
2. 执行数据修复：`UPDATE models SET status = 2 WHERE enabled = false`（将已禁用的模型标记为手动禁用）
3. 之后的禁用操作会正确设置 `status` 值

---

## 决策 2: 恢复检测的触发条件

### 背景

需要确定：
1. 何时检测被自动禁用的模型？
2. 达到什么条件触发自动恢复？

### 方案对比

| 方案 | 触发时机 | 恢复条件 | 优点 | 缺点 |
|------|----------|----------|------|------|
| **A: 复用现有检测周期** | 与正常模型检测同周期 | 连续 N 次成功 | 实现简单 | 检测量可能较大 |
| **B: 独立恢复检测周期** | 单独配置的恢复检测间隔 | 连续 N 次成功 | 可独立控制频率 | 增加配置复杂度 |
| **C: 被动触发** | 用户手动触发恢复检测 | 单次检测成功 | 不占用系统资源 | 不满足"自动"需求 |

### 决策

**选择方案 A**：复用现有检测周期，修改检测逻辑包含 `status=3` 的模型

**理由**：
1. **实现简单**：只需修改 `runBackgroundModelCheck` 的过滤条件
2. **行为一致**：使用相同的探测方式和间隔
3. **配置简化**：只需新增一个配置项 `RecoverySuccessThreshold`（恢复所需连续成功次数）

**恢复条件**：
- 被自动禁用的模型（`status=3`）连续 N 次检测成功后自动恢复
- 默认 N=2（低于禁用阈值 3，在保守与敏捷之间取得平衡）
- 恢复后重置 `consecutive_failures = 0`

**需要修改的检测逻辑** (`health_check_service.go:322-326`):
```go
// 当前逻辑：跳过所有 enabled=false 的模型
if m.Enabled == nil || !*m.Enabled {
    continue
}

// 修改后：跳过手动禁用(status=2)，但检测自动禁用(status=3)
if m.Enabled == nil || !*m.Enabled {
    if m.Status != 3 { // 非自动禁用的模型跳过
        continue
    }
    // status=3 的模型继续检测，用于恢复判断
}
```

---

## 决策 3: 模型状态变更日志的存储设计

### 背景

需要记录模型状态变更的完整历史，包括：
- 变更时间
- 变更类型（自动禁用、自动恢复、手动禁用、手动启用）
- 触发原因
- 错误信息（禁用时）
- 操作人（手动操作时）

### 方案对比

| 方案 | 存储位置 | 优点 | 缺点 |
|------|----------|------|------|
| **A: 独立日志表** | 新建 `model_status_logs` 表 | 结构清晰，查询高效 | 需要新建表 |
| **B: 复用 health_check_logs** | 扩展现有表 | 无需新建表 | 语义混杂，字段冗余 |
| **C: 结构化日志** | 写入应用日志/ELK | 无需数据库变更 | 查询不便，无UI支持 |

### 决策

**选择方案 A**：新建 `model_status_logs` 表

**理由**：
1. **职责单一**：专门记录状态变更，与健康检测日志分离
2. **查询高效**：可按模型ID、时间范围、变更类型快速检索
3. **UI友好**：便于前端实现状态历史看板

**表结构设计**：
```sql
CREATE TABLE model_status_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id UUID NOT NULL,
    model_name VARCHAR(200) NOT NULL,
    provider_id UUID NOT NULL,
    provider_name VARCHAR(200) NOT NULL,
    change_type VARCHAR(20) NOT NULL,  -- auto_disabled, auto_recovered, manual_disabled, manual_enabled
    reason VARCHAR(500) NOT NULL DEFAULT '',
    error_message TEXT DEFAULT '',
    operator_id UUID,  -- NULL for auto actions
    operator_name VARCHAR(128) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_status_logs_model_id ON model_status_logs(model_id);
CREATE INDEX idx_model_status_logs_created_at ON model_status_logs(created_at);
CREATE INDEX idx_model_status_logs_change_type ON model_status_logs(change_type);
```

---

## 决策 4: HealthCheckConfig 新增字段

### 背景

需要支持配置恢复相关的参数。

### 决策

在 `HealthCheckConfig` 实体新增以下字段：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `model_recovery_enabled` | `bool` | `true` | 是否启用模型自动恢复 |
| `model_recovery_success_threshold` | `int` | `2` | 恢复所需连续成功次数 |

**理由**：
- 提供开关控制自动恢复功能
- 允许调整恢复灵敏度

---

## 决策 5: Gateway 侧的感知方式

### 背景

Gateway 不直连数据库，需要确定它如何感知模型状态变化。

### 决策

**无需额外改动**。

**理由**：
1. Gateway 通过 `ProviderLoader` 从 Redis snapshot 加载配置
2. 控制面的 `SaveProviderSnapshot` 已包含模型的 `Enabled` 状态
3. 模型自动恢复后，`Enabled` 从 `false` 变为 `true`，会触发 `config:change` 事件
4. Gateway 的 `ProviderLoader` 监听该事件并重建 `providers.Router`

**现有机制已满足需求**：
- 控制面修改模型状态 → 调用 `SaveProviderSnapshot` → 写 Redis + pub `config:change`
- Gateway `ProviderLoader` 收到事件 → 重新加载配置 → 感知到模型启用状态变化

---

## 后续考虑

1. **日志清理策略**：`model_status_logs` 表可能需要定期清理，建议保留最近 90 天的记录
2. **告警集成**：未来可考虑在模型自动禁用/恢复时发送告警通知
3. **Dashboard 指标**：可考虑在健康监控看板展示自动恢复的统计信息
