# 测试规范

## 测试数据库隔离

### 强制规则

- **禁止**使用生产数据库（`llm_gateway`）运行集成测试
- 集成测试必须使用独立的测试数据库：`llm_gateway_test`
- 测试数据库不存在时，测试启动前自动创建

### 数据库配置

| 环境 | 数据库名 | 用途 |
| :--- | :--- | :--- |
| 生产/开发 | `llm_gateway` | 服务运行数据 |
| 测试 | `llm_gateway_test` | 集成测试专用，每次测试前清表重建 |

### 连接信息

```
Host: 115.190.242.137
Port: 5433
User: llm_gateway
Password: llm_gateway_dev_2026
Test DB: llm_gateway_test
```

### 测试启动流程

```
1. 检查 llm_gateway_test 数据库是否存在
2. 不存在 → 连接默认库（postgres/llm_gateway）执行 CREATE DATABASE llm_gateway_test
3. 连接 llm_gateway_test
4. DropTable + AutoMigrate 重建全部表
5. 执行测试
6. 测试结束后数据保留（方便排查，下次运行时会重建）
```

### 环境变量

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `RUN_INTEGRATION_TESTS` | 空 | 设为 `1` 启用集成测试 |
| `TEST_DB_HOST` | `115.190.242.137` | 测试数据库地址 |
| `TEST_DB_PORT` | `5433` | 测试数据库端口 |
| `TEST_DB_USER` | `llm_gateway` | 测试数据库用户 |
| `TEST_DB_PASSWORD` | `llm_gateway_dev_2026` | 测试数据库密码 |
| `TEST_DB_NAME` | `llm_gateway_test` | 测试数据库名称（不可与生产相同） |
| `ENCRYPTION_KEY` | 无 | 加密密钥（测试用固定值：`12345678901234567890123456789012`） |

### 执行命令

```bash
RUN_INTEGRATION_TESTS=1 \
ENCRYPTION_KEY="12345678901234567890123456789012" \
TEST_DB_NAME="llm_gateway_test" \
go test ./tests/integration/ -v -count=1 -timeout 120s
```

### 注意事项

1. **CI/CD 环境**：必须配置独立的 PG 实例或独立数据库名，不得复用开发/生产库
2. **本地开发**：如未设置 `RUN_INTEGRATION_TESTS=1`，集成测试自动跳过（不会影响 `go test` 默认行为）
3. **并行测试**：当前不支持并行（`setupTestDB` 每次清表），如需并行需改为每个测试用随机 schema
4. **数据残留**：测试结束后数据保留在 `llm_gateway_test` 中，方便 debug；下次测试启动时会重建
