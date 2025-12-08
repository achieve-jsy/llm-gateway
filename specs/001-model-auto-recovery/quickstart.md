# Quickstart: 模型自动恢复功能验证

**Feature**: 001-model-auto-recovery | **Date**: 2026-06-10

本文档提供模型自动恢复功能的端到端验证指南。

---

## 前置条件

1. **服务运行中**
   - control-plane 运行在 `:8081`
   - gateway 运行在 `:8080`
   - PostgreSQL 和 Redis 正常运行

2. **数据库已迁移**
   ```bash
   # 确认迁移已执行
   atlas migrate status --env gorm
   ```

3. **测试数据准备**
   - 至少一个已配置的 Provider（渠道）
   - 至少一个已注册的 Model（模型）

---

## 验证场景

### 场景 1: 模型自动禁用流程

**目标**：验证模型连续健康检测失败后被自动禁用

**步骤**：

1. **配置一个会失败的模型**
   - 在管理界面创建一个模型，指向一个无效的上游地址
   - 或者临时修改一个正常模型的 `upstream_model_id` 为不存在的模型名

2. **启用健康检测**
   ```bash
   curl -X PUT http://localhost:8081/api/v1/health-check/config \
     -H "Authorization: Bearer <your-jwt-token>" \
     -H "Content-Type: application/json" \
     -d '{
       "model_check_enabled": true,
       "model_check_interval_seconds": 60,
       "model_check_failure_threshold": 3
     }'
   ```

3. **等待自动禁用触发**
   - 等待约 3 分钟（3 次检测周期）
   - 查看模型状态：
   ```bash
   curl http://localhost:8081/api/v1/models/<model-id> \
     -H "Authorization: Bearer <your-jwt-token>"
   ```

4. **预期结果**
   - `enabled` = `false`
   - `status` = `3`（自动禁用）
   - `consecutive_failures` >= 3

5. **验证状态日志**
   ```bash
   curl "http://localhost:8081/api/v1/model-status-logs?model_id=<model-id>" \
     -H "Authorization: Bearer <your-jwt-token>"
   ```
   - 应有一条 `change_type` = `auto_disabled` 的记录

---

### 场景 2: 模型自动恢复流程

**目标**：验证被自动禁用的模型在上游恢复后自动启用

**步骤**：

1. **使用场景 1 的结果**
   - 确保模型处于 `status=3` 状态

2. **修复模型配置**
   - 将模型的 `upstream_model_id` 恢复为有效值
   - 或者修复上游服务

3. **启用自动恢复**
   ```bash
   curl -X PUT http://localhost:8081/api/v1/health-check/config \
     -H "Authorization: Bearer <your-jwt-token>" \
     -H "Content-Type: application/json" \
     -d '{
       "model_recovery_enabled": true,
       "model_recovery_success_threshold": 2
     }'
   ```

4. **等待自动恢复触发**
   - 等待约 2 分钟（2 次检测周期）
   - 查看模型状态：
   ```bash
   curl http://localhost:8081/api/v1/models/<model-id> \
     -H "Authorization: Bearer <your-jwt-token>"
   ```

5. **预期结果**
   - `enabled` = `true`
   - `status` = `1`（启用）
   - `consecutive_failures` = `0`
   - `consecutive_recovery_successes` = `0`（恢复后重置）

6. **验证状态日志**
   ```bash
   curl "http://localhost:8081/api/v1/model-status-logs?model_id=<model-id>" \
     -H "Authorization: Bearer <your-jwt-token>"
   ```
   - 应有一条 `change_type` = `auto_recovered` 的记录

---

### 场景 3: 手动禁用不触发自动恢复

**目标**：验证手动禁用的模型不会被系统自动恢复

**步骤**：

1. **手动禁用一个正常模型**
   ```bash
   curl -X PATCH http://localhost:8081/api/v1/models/batch-status \
     -H "Authorization: Bearer <your-jwt-token>" \
     -H "Content-Type: application/json" \
     -d '{
       "ids": ["<model-id>"],
       "enabled": false
     }'
   ```

2. **确认模型状态**
   ```bash
   curl http://localhost:8081/api/v1/models/<model-id> \
     -H "Authorization: Bearer <your-jwt-token>"
   ```
   - `enabled` = `false`
   - `status` = `2`（手动禁用）

3. **等待多个检测周期**
   - 等待约 5 分钟

4. **再次检查模型状态**
   ```bash
   curl http://localhost:8081/api/v1/models/<model-id> \
     -H "Authorization: Bearer <your-jwt-token>"
   ```

5. **预期结果**
   - `enabled` 仍为 `false`
   - `status` 仍为 `2`
   - 模型**未被自动恢复**

---

### 场景 4: 状态变更日志看板

**目标**：验证前端状态日志看板的展示

**步骤**：

1. **访问管理界面**
   ```
   http://localhost:8090/admin/model-status-logs
   ```

2. **验证筛选功能**
   - 按模型筛选
   - 按渠道筛选
   - 按变更类型筛选
   - 按时间范围筛选

3. **验证数据展示**
   - 变更类型使用正确的颜色标签
   - 时间显示为相对时间
   - 点击模型名可跳转到模型详情

4. **验证模型详情页入口**
   - 在模型列表页找到操作列的"状态历史"按钮
   - 点击后展示该模型的状态变更历史

---

## 快速测试脚本

```bash
#!/bin/bash
# test-model-recovery.sh

BASE_URL="http://localhost:8081"
TOKEN="<your-jwt-token>"

echo "=== 1. 获取健康检测配置 ==="
curl -s "$BASE_URL/api/v1/health-check/config" \
  -H "Authorization: Bearer $TOKEN" | jq .

echo ""
echo "=== 2. 启用自动恢复 ==="
curl -s -X PUT "$BASE_URL/api/v1/health-check/config" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model_check_enabled": true,
    "model_recovery_enabled": true,
    "model_recovery_success_threshold": 2
  }' | jq .

echo ""
echo "=== 3. 查看状态日志 ==="
curl -s "$BASE_URL/api/v1/model-status-logs?page_size=5" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## 数据模型和 API 契约参考

- [数据模型设计](./data-model.md)
- [状态日志 API 契约](./contracts/model-status-log-api.md)
- [健康检测配置 API 契约](./contracts/health-check-config-api.md)
