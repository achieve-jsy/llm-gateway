# API Contract: 健康检测配置（恢复相关）

**Feature**: 001-model-auto-recovery | **Date**: 2026-06-10

本文档定义健康检测配置中与模型自动恢复相关的 API 变更。

---

## 现有 API 扩展

### 获取健康检测配置

```
GET /api/v1/health-check/config
```

#### 响应变更

在现有响应基础上新增以下字段：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    // ... 现有字段 ...
    "model_check_enabled": true,
    "model_check_interval_seconds": 300,
    "model_check_failure_threshold": 3,
    
    // 新增字段
    "model_recovery_enabled": true,
    "model_recovery_success_threshold": 2
  }
}
```

| 新增字段 | 类型 | 默认值 | 说明 |
|----------|------|--------|------|
| `model_recovery_enabled` | `bool` | `true` | 是否启用模型自动恢复 |
| `model_recovery_success_threshold` | `int` | `2` | 恢复所需连续成功次数 |

---

### 更新健康检测配置

```
PUT /api/v1/health-check/config
```

#### 请求体变更

新增可选字段：

```json
{
  // ... 现有字段 ...
  
  // 新增字段
  "model_recovery_enabled": true,
  "model_recovery_success_threshold": 2
}
```

#### 校验规则

| 字段 | 规则 |
|------|------|
| `model_recovery_success_threshold` | 必须 >= 1 且 <= 10 |

#### 错误响应

| HTTP 状态码 | code | message | 场景 |
|-------------|------|---------|------|
| 400 | 1003 | "model_recovery_success_threshold must be between 1 and 10" | 阈值超出范围 |

---

## 配置说明

### 恢复机制工作原理

1. **触发条件**：当 `model_recovery_enabled = true` 时启用
2. **检测范围**：状态为 `status=3`（自动禁用）的模型会参与健康检测
3. **恢复条件**：连续成功次数达到 `model_recovery_success_threshold` 时自动恢复
4. **恢复行为**：
   - 设置 `enabled = true`
   - 设置 `status = 1`
   - 重置 `consecutive_failures = 0`
   - 重置 `consecutive_recovery_successes = 0`
   - 记录状态变更日志

### 推荐配置

| 场景 | model_recovery_enabled | model_recovery_success_threshold | 说明 |
|------|------------------------|----------------------------------|------|
| 默认 | `true` | `2` | 平衡保守与敏捷 |
| 保守 | `true` | `3` | 更谨慎的恢复策略 |
| 关闭 | `false` | - | 禁用自动恢复，需手动恢复 |

### 与禁用阈值的关系

- **禁用阈值**（`model_check_failure_threshold`）：默认 3 次
- **恢复阈值**（`model_recovery_success_threshold`）：默认 2 次

恢复阈值建议小于禁用阈值，原因：
1. 避免模型在临界状态反复切换
2. 恢复可以更激进一些（模型已经证明能正常响应）

---

## 前端集成说明

### 设置页面位置

在现有健康检测设置对话框中添加"模型恢复"配置区域：

```
┌─────────────────────────────────────┐
│ 健康检测设置                        │
├─────────────────────────────────────┤
│ 渠道检测                            │
│   [x] 启用渠道检测                  │
│   检测间隔: [300] 秒                │
│   失败阈值: [3] 次                  │
├─────────────────────────────────────┤
│ 模型检测                            │
│   [x] 启用模型检测                  │
│   检测间隔: [300] 秒                │
│   失败阈值: [3] 次                  │
├─────────────────────────────────────┤
│ 模型恢复 (新增)                     │
│   [x] 启用自动恢复                  │
│   恢复阈值: [2] 次连续成功          │
│                                     │
│   提示：被自动禁用的模型会继续接受   │
│   健康检测，连续成功达到阈值后自动   │
│   恢复。手动禁用的模型不会自动恢复。 │
└─────────────────────────────────────┘
```

### 表单校验

- `model_recovery_success_threshold`：整数，范围 1-10
- 当 `model_recovery_enabled = false` 时，`model_recovery_success_threshold` 输入框禁用
