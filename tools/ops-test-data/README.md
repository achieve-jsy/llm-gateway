# ops-test-data

运维看板测试数据生成器。

生成真实感的数据填充到 `call_logs` 和 `system_logs` 表，让所有看板图表有内容：

- ✅ 流量趋势（Traffic Trend）
- ✅ 错误趋势（Error Trend）
- ✅ 延迟分布（Latency Histogram）
- ✅ 切换率趋势（Switch Rate）
- ✅ Token 统计（Token Stats）
- ✅ 系统日志（System Logs）

## 运行

```bash
# 本地开发环境（读取 config/dev.yaml）
go run main.go

# 指定数据库连接串（测试/生产环境）
DATABASE_URL="host=xxx port=5432 user=xxx password=xxx dbname=xxx sslmode=disable" go run main.go

# 调整时间跨度（默认 24 小时）
go run main.go --hours 48

# 调整请求数量（默认 3000）
go run main.go --count 5000

# 清理生成的数据（通过 key_name 前缀识别）
go run main.go --clean
```

## 数据特征

生成的数据具有以下真实感特征：

1. **时间分布**：模拟真实业务流量
   - 工作时间（9:00-18:00）流量高
   - 夜晚（0:00-8:00）流量低
   - 周末流量减半

2. **Provider/Model 分布**：
   - 优先使用数据库中已有的 provider/model/group
   - 如果没有真实数据，使用常见的 LLM 名称
   - 热门 model 流量更大

3. **延迟分布**：
   - 对数正态分布（大部分请求 200-2000ms）
   - 少量慢请求 >5000ms
   - 错误请求延迟更长

4. **状态码分布**：
   - 成功: 95%
   - 客户端错误 (400/401/429): 4%
   - 服务端错误 (500/502/503): 1%

5. **Token 统计**：
   - Input tokens: 500-5000
   - Output tokens: 200-3000
   - 错误请求 token 较少

## 实现原理

- 所有种子数据使用特殊前缀 `__seed_{uuid}` 标记
- `--clean` 会删除所有 `key_name LIKE '__seed_%'` 的记录
- 时间戳精确到毫秒，按时间排序均匀分布
- 同一个 key_id 会生成多条记录（模拟真实用户多次调用）
- 使用 GORM 批量插入，性能优

## 清理

运行后如需清理生成的数据：

```bash
go run main.go --clean
```

或者直接：

```sql
DELETE FROM call_logs WHERE key_name LIKE '__seed_%';
```
