# 项目防护栏铁律

> 以下规则为**强制约束**，违反将导致 PR 被拒绝或生产事故。

## 架构铁律

### A1: Gateway 不直连数据库

**Gateway 进程禁止直接连接 PostgreSQL。**

- ✅ 通过 Redis Snapshot 获取 Provider 配置
- ✅ 通过内部 HTTP API (`/internal/v1/*`) 调用控制面
- ✅ 通过进程内 channel 异步写入埋点（设计例外，生产可替换为消息队列）
- ❌ 直接 `gorm.Open()` 或 `sql.Open()` 连接 Postgres

**原因**：Gateway 是高并发流量入口，直连 DB 会成为瓶颈且增加故障面。

### A2: 模型组能力聚合为交集

组内模型能力取**最保守值**：

- `supports_vision` / `supports_tools`：全部支持才为 `true`
- `max_tokens`：取最小正值

**原因**：保证用户使用模型组时不会因某个成员不支持而失败。

### A3: 价格单位统一

所有价格字段使用 **USD per 1,000,000 tokens**，与 LiteLLM Catalog 一致。

## 数据库铁律

### D1: 迁移文件只增不改

**已提交的迁移文件禁止修改。** 修复问题必须新增迁移。

```
❌ 修改 migrations/022_add_model_groups.sql
✅ 新增 migrations/042_fix_model_groups_xxx.sql
```

**原因**：已在生产执行的迁移被修改会导致状态不一致。

### D2: 破坏性变更用 Expand-Contract

修改列类型、重命名列、删除列等破坏性变更：

1. **Expand**：新增列，保持旧列
2. **Migrate**：应用代码写入两列，迁移脚本填充新列
3. **Contract**：确认无旧数据依赖后，删除旧列

### D3: 敏感数据加密存储

以下字段必须加密后存储：

| 表 | 字段 | 加密方式 |
|---|---|---|
| `providers` | `encrypted_credentials` | AES-256-GCM |
| `api_keys` | `encrypted_key` | AES-256-GCM |

**禁止明文存储任何凭证或密钥。**

## 配置铁律

### C1: 密钥禁止提交

以下文件/内容禁止提交到 Git：

- `.env`（实际环境变量）
- 任何包含真实密码、Token、Secret 的文件
- SSL 证书私钥

**允许提交**：`.env.example`（模板，值为占位符）

### C2: 生产配置不能有默认值

以下配置在生产环境必须显式设置，不能依赖代码默认值：

- `JWT_SECRET`
- `INTERNAL_AUTH_TOKEN`
- `ENCRYPTION_KEY`
- `DATABASE_URL`
- OAuth 相关配置

### C3: 不同环境使用不同 Redis DB

| 环境 | Redis DB |
|------|----------|
| 开发 | 0 |
| 测试 | 1 |
| 生产 | 2 |

## 质量铁律

### Q1: 编译必须通过

提交前必须确保：

```bash
go build ./...    # 无编译错误
```

### Q2: 不得随意抑制类型错误

以下模式禁止使用（除非有充分理由并注释说明）：

```go
// ❌ 禁止
_ = someError
result := value.(SomeType)  // 无类型断言检查

// ✅ 正确
if err != nil {
    return err
}
if result, ok := value.(SomeType); ok {
    // use result
}
```

### Q3: 测试物理分离

测试代码放在 `tests/` 目录，按模块镜像 `internal/` 结构：

```
tests/
├── gateway/
│   ├── handler/
│   ├── middleware/
│   └── scheduler/
└── controlplane/
    ├── service/
    └── handler/
```

**不要**在 `internal/` 下创建 `*_test.go`。

## 流程铁律

### P1: 提交信息规范

使用 Conventional Commits：

```
feat(gateway): 添加模型组调度功能
fix(auth): 修复 Token 刷新逻辑
chore(ci): 更新构建脚本
docs(readme): 补充部署说明
refactor(scheduler): 重构路由表结构
```

### P2: 分支策略

| 分支 | 用途 | 部署目标 |
|------|------|----------|
| `main` | 生产就绪 | tag 触发生产部署 |
| `test` | 测试集成 | push 触发测试部署 |
| `feat/*` `fix/*` | 功能开发 | - |

### P3: 不直接推送 main

`main` 分支只接受：
- 从 `test` 合并的 PR
- 带 `v*.*.*` tag 的发布

**禁止**直接 `git push origin main`。

## 安全铁律

### S1: 内部 API 必须鉴权

`/internal/v1/*` 端点必须通过 `InternalAuth` 中间件校验 `X-Internal-Token`。

### S2: 系统探针 Key 过滤

`__system_` 前缀的 API Key：
- 跳过埋点采集
- 跳过 Trace 记录
- 用于内部健康检查等场景

### S3: 请求体截断

写入 `call_traces` 的请求/响应体：
- 总大小截断到 512KB
- 消息 content 截断到 2048 字符
- 过滤敏感 Header（Authorization 等）
