# Sticky Routing 生产部署指南

## ✅ 功能验证完成

Sticky routing 功能已在测试环境（192.0.2.10）成功验证：

- ✅ 配置同步正常：Gateway 成功从 Control Plane 拉取组配置
- ✅ Sticky routing 生效：使用相同 `conversation_id` 的请求路由到相同模型
- ✅ Redis 存储正常：Session key 成功存储并读取
- ✅ 日志记录完整：Sticky 使用情况有明确的日志输出

### 验证日志示例

```json
{"time":"2026-07-13T14:12:47Z","level":"INFO","msg":"sticky: using pinned model","group":"deepseek-v4-flash","session_key":"test-con...","model_id":"16","model_name":"deepseek-v4-flash"}
```

---

## 🚀 生产部署步骤

### 1. 环境变量配置

**关键环境变量**（必须设置）：

```bash
# Redis 连接（用于 sticky session 存储）
export REDIS_PASSWORD="<production-redis-password>"

# Control Plane 通信
export CONTROL_PLANE_URL="http://localhost:8081"  # 或实际 Control Plane 地址
export INTERNAL_TOKEN="<production-internal-token>"  # 注意：是 INTERNAL_TOKEN，不是 INTERNAL_AUTH_TOKEN

# Auth 服务（用于 API Key 验证）
export AUTH_URL="http://localhost:8081"  # 或实际 Auth 服务地址
export AUTH_INTERNAL_TOKEN="<production-internal-token>"  # Auth 服务的 internal token
```

**注意事项**：
- `INTERNAL_TOKEN`：用于 Gateway → Control Plane 的配置同步
- `AUTH_INTERNAL_TOKEN`：用于 Gateway → Auth 服务的 API Key 验证
- 两者通常使用相同的值，但对应不同的配置字段

### 2. 数据库迁移

运行 migration 添加 sticky 字段：

```bash
# 检查当前迁移状态
migrate -database "postgres://..." -path migrations version

# 应用迁移
migrate -database "postgres://..." -path migrations up
```

迁移文件：
- `migrations/060_add_sticky_to_groups.up.sql`：添加 `sticky_enabled` 和 `sticky_ttl_seconds` 字段
- `migrations/060_add_sticky_to_groups.down.sql`：回滚迁移

### 3. 编译部署

```bash
# 编译 Gateway
cd cmd/gateway
go build -o gateway-sticky .

# 编译 Control Plane
cd cmd/control-plane
go build -o control-plane-sticky .

# 部署到生产服务器
scp gateway-sticky user@prod-server:/path/to/deploy/
scp control-plane-sticky user@prod-server:/path/to/deploy/
```

### 4. 启动服务

**Control Plane** (先启动):
```bash
export INTERNAL_AUTH_TOKEN="<production-internal-token>"
export DATABASE_URL="postgres://..."
export REDIS_PASSWORD="<production-redis-password>"
./control-plane-sticky
```

**Gateway**:
```bash
export REDIS_PASSWORD="<production-redis-password>"
export INTERNAL_TOKEN="<production-internal-token>"
export AUTH_INTERNAL_TOKEN="<production-internal-token>"
export CONTROL_PLANE_URL="http://localhost:8081"
export AUTH_URL="http://localhost:8081"
./gateway-sticky
```

### 5. 配置 Sticky Routing

通过 Control Plane API 为指定组启用 sticky routing：

```bash
curl -X PUT http://control-plane-url:8081/api/v1/groups/{group_id} \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "sticky_enabled": true,
    "sticky_ttl_seconds": 300
  }'
```

**参数说明**：
- `sticky_enabled`: 是否启用 sticky routing
- `sticky_ttl_seconds`: Session 存活时间（秒），默认 3600（1小时），建议 300-3600

---

## 📋 验证清单

部署后进行以下验证：

### 1. 配置同步验证

```bash
# 检查 Gateway 日志
tail -f gateway.log | grep "config sync"

# 应该看到类似日志：
# {"level":"INFO","msg":"scheduler config synced","groups":19,"models":20}
```

### 2. Sticky Routing 验证

发送两个带有相同 `conversation_id` 的请求：

```bash
# 第一个请求
curl -X POST http://gateway:8080/v1/chat/completions \
  -H "Authorization: Bearer <api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-v4-flash",
    "messages": [{"role": "user", "content": "First message"}],
    "max_tokens": 10,
    "extra_fields": {"conversation_id": "test-001"}
  }'

# 第二个请求（使用相同 conversation_id）
curl -X POST http://gateway:8080/v1/chat/completions \
  -H "Authorization: Bearer <api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-v4-flash",
    "messages": [{"role": "user", "content": "Second message"}],
    "max_tokens": 10,
    "extra_fields": {"conversation_id": "test-001"}
  }'
```

检查日志：

```bash
tail -f gateway.log | grep sticky

# 第二个请求应该看到：
# {"level":"INFO","msg":"sticky: using pinned model","group":"deepseek-v4-flash","session_key":"test-001","model_id":"16","model_name":"deepseek-v4-flash"}
```

### 3. Redis 数据验证

```bash
# 连接 Redis
redis-cli -a <password>

# 查看 sticky keys
KEYS sticky:*

# 查看具体 session
GET sticky:deepseek-v4-flash:test-001

# 检查 TTL
TTL sticky:deepseek-v4-flash:test-001
```

### 4. 健康检查

```bash
# Gateway 健康检查
curl http://gateway:8080/health

# Control Plane 健康检查
curl http://control-plane:8081/health
```

---

## 🔍 故障排查

### 问题：配置同步失败（403 Forbidden）

**症状**：
```
{"level":"WARN","msg":"scheduler config sync failed","error":"unexpected status 403: forbidden"}
```

**原因**：
- `INTERNAL_TOKEN` 环境变量未设置或错误
- Control Plane 的 `INTERNAL_AUTH_TOKEN` 与 Gateway 的 `INTERNAL_TOKEN` 不匹配

**解决**：
1. 检查环境变量：`echo $INTERNAL_TOKEN`
2. 验证 Control Plane token：`echo $INTERNAL_AUTH_TOKEN`
3. 确保两者一致
4. 重启 Gateway

### 问题：Sticky routing 不生效

**症状**：
- 相同 `conversation_id` 的请求路由到不同模型
- 日志中没有 "sticky: using pinned model" 消息

**排查步骤**：

1. **检查组配置**：
```bash
curl http://control-plane:8081/api/v1/internal/groups \
  -H "X-Auth-Token: <internal-token>" | jq '.data[] | select(.name=="deepseek-v4-flash")'
```

确认 `sticky_enabled: true`

2. **检查请求格式**：
确保请求包含 `extra_fields.conversation_id`：
```json
{
  "model": "deepseek-v4-flash",
  "messages": [...],
  "extra_fields": {
    "conversation_id": "your-session-id"
  }
}
```

3. **检查 Redis 连接**：
```bash
# Gateway 日志中查找 Redis 错误
tail -f gateway.log | grep -i redis
```

4. **检查 session key 提取**：
Gateway 启动时应该输出：
```
{"level":"INFO","msg":"scheduler started"}
```

### 问题：Auth 服务不可用

**症状**：
```
{"error":{"code":"auth_service_unavailable","message":"auth service unavailable"}}
```

**原因**：
- `AUTH_URL` 未设置或错误
- `AUTH_INTERNAL_TOKEN` 未设置

**解决**：
1. 设置正确的环境变量：
```bash
export AUTH_URL="http://localhost:8081"
export AUTH_INTERNAL_TOKEN="<production-token>"
```
2. 重启 Gateway

---

## 📊 监控指标

### 关键指标

1. **Sticky routing 使用率**：
   - 统计日志中 "sticky: using pinned model" 的频率
   - 预期：启用 sticky 的组应该有 60-90% 的请求使用 sticky routing

2. **Redis 性能**：
   - `GET` 延迟应该 < 5ms
   - `SET` 延迟应该 < 10ms

3. **配置同步**：
   - 每 5 秒同步一次
   - 失败率应该 < 0.1%

### 日志查询示例

```bash
# 统计 sticky routing 使用次数
grep "sticky: using pinned model" gateway.log | wc -l

# 统计配置同步成功次数
grep "scheduler config synced" gateway.log | wc -l

# 查看最近的 sticky sessions
grep "sticky: using pinned model" gateway.log | tail -20
```

---

## 🔄 回滚计划

如果生产环境出现问题，可以快速回滚：

### 1. 禁用 Sticky Routing（软回滚）

通过 API 禁用所有组的 sticky routing：

```bash
# 获取所有启用 sticky 的组
curl http://control-plane:8081/api/v1/internal/groups \
  -H "X-Auth-Token: <token>" | jq '.data[] | select(.sticky_enabled==true) | .id'

# 逐个禁用
curl -X PUT http://control-plane:8081/api/v1/groups/{group_id} \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{"sticky_enabled": false}'
```

### 2. 回滚服务（硬回滚）

```bash
# 停止新版本
pkill -f gateway-sticky
pkill -f control-plane-sticky

# 启动旧版本
./gateway-old
./control-plane-old
```

### 3. 回滚数据库

```bash
# 回滚 migration
migrate -database "postgres://..." -path migrations down 1
```

**注意**：回滚迁移会删除 `sticky_enabled` 和 `sticky_ttl_seconds` 字段，但不影响其他功能。

---

## 📝 配置文件示例

### Gateway 配置 (config/prod.yaml)

```yaml
server:
  port: "8080"

redis:
  addr: "redis:6379"
  password: "${REDIS_PASSWORD}"
  db: 0

provider:
  control_plane_url: "${CONTROL_PLANE_URL}"
  internal_token: "${INTERNAL_TOKEN}"

auth:
  url: "${AUTH_URL}"
  token: "${AUTH_INTERNAL_TOKEN}"
```

### Systemd 服务文件 (gateway.service)

```ini
[Unit]
Description=LLM Gateway with Sticky Routing
After=network.target redis.service

[Service]
Type=simple
User=llm-gateway
WorkingDirectory=/opt/llm-gateway
Environment="REDIS_PASSWORD=<password>"
Environment="INTERNAL_TOKEN=<token>"
Environment="AUTH_INTERNAL_TOKEN=<token>"
Environment="CONTROL_PLANE_URL=http://localhost:8081"
Environment="AUTH_URL=http://localhost:8081"
ExecStart=/opt/llm-gateway/gateway-sticky
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

---

## 🎯 性能调优

### Redis 连接池

默认配置已优化，如需调整：

```go
// internal/infra/redis.go
redis.NewClient(&redis.Options{
    PoolSize:     100,  // 连接池大小
    MinIdleConns: 10,   // 最小空闲连接
    MaxConnAge:   5 * time.Minute,
})
```

### Sticky TTL 建议

根据业务场景选择合适的 TTL：

- **对话场景**（ChatGPT-like）：600-3600 秒（10分钟-1小时）
- **长对话**：3600-7200 秒（1-2小时）
- **短会话**：300-600 秒（5-10分钟）

### 配置同步间隔

默认 5 秒，如需调整：

```go
// internal/gateway/scheduler/config.go
const defaultSyncInterval = 5 * time.Second
```

---

## 📞 联系支持

如遇到问题，请提供以下信息：

1. Gateway 和 Control Plane 的完整日志
2. 环境变量配置（脱敏后）
3. 复现步骤
4. Redis 版本和连接信息
5. 数据库迁移状态

---

**部署时间**: 2026-07-13  
**测试环境**: 192.0.2.10  
**验证状态**: ✅ 通过
