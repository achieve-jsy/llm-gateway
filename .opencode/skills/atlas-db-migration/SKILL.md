---
name: atlas-db-migration
description: Atlas 数据库迁移强制工作流。任何涉及数据库 schema 的改动都必须走此 skill——修改 GORM model、添加/删除/修改字段、加索引/约束/外键、新建表、生成或修改迁移 SQL、处理 atlas.sum 冲突或迁移乱序、排查迁移失败时使用。多人并行开发、共享同一开发库时尤其必须遵循，以避免 atlas.sum 冲突与版本乱序。
---

# Atlas 数据库迁移工作流 (atlas-db-migration)

本 skill 沉淀了 llm_gateway 项目使用 Atlas + GORM Provider 进行声明式数据库迁移的完整流程和最佳实践。

> **强制要求**：团队任何成员（含 AI 助手）生成/修改数据库迁移时，**必须先读本 skill 并完整走一遍第 0 节流程**，不得凭记忆直接手写 SQL 或手改 `atlas.sum`。这是为了在多人并行、共享开发库的场景下避免迁移冲突。

## 适用场景

- 修改 GORM model（添加/删除/修改字段）
- 添加索引、约束、外键
- 修改字段类型或属性
- 创建新表
- 排查迁移失败问题

## 0. 强制流程与多人并行铁律（先读这里）

> 任何数据库 schema 改动都从这一节开始。**生成迁移前必须先同步目标分支的迁移目录**，否则并行开发几乎必然冲突。

### 0.1 关键认知：diff 不碰共享库，冲突在「文件」层

- `make atlas-diff`（即 `atlas migrate diff --env local`）的 **dev-url 是 `docker://postgres/16/dev`——每次起一个临时 Postgres 容器算增量，用完即弃，不连接你们的共享开发库**。所以「多人连同一个开发库」本身不会让 `diff` 出错。
- 真正的冲突来源有三个，全在**迁移文件/校验和/版本顺序**层面：
  1. **`atlas.sum` 冲突**：首行是整目录哈希 + 每文件一行。两个分支各加迁移都改它 → git 必冲突；硬改 → `checksum mismatch`。
  2. **版本乱序（non-linear）**：Atlas 按文件名时间戳字典序、要求线性历史。B 分支 `…130000` 先合并并 apply，A 分支 `…120000` 后合并 → 时间戳比库内已应用的最新版本还早 → Atlas 拒绝执行。
  3. **diff 基于陈旧状态**：两人都从旧 base diff，互不可见 → 合并后变更重复或互相打架（如都给同一表加列）。
- 另外：若大家把 `atlas migrate apply` 都指向**同一个真实开发库**，会让库的 `atlas_schema_revisions` 状态与各分支的迁移目录发散。**约定 apply 只在本机临时库或由 CI 统一执行**，开发者本地一般只 `diff` + `validate`，不必 apply 到共享库。

### 0.2 黄金规则（一句话）

**迁移文件必须基于「目标分支(test/main)最新的 `atlas-migrations/`」生成，尽量在 PR 即将合并前最后再生成；永远不要手动编辑 `atlas.sum`。**

### 0.3 标准生成流程（每次必走）

```bash
# 1. 先同步目标分支，确保 atlas-migrations/ 是最新的
git fetch origin && git rebase origin/test     # 或 origin/main

# 2. 改 GORM model（一次只改一个逻辑变更）

# 3. 用 diff 生成迁移（自动起临时容器，不碰共享库）
make atlas-diff name=add_models_status_field   # 命名: 动词_表_描述

# 4. 重算校验和 + 校验（线性/完整性）
atlas migrate hash --env local
atlas migrate validate --env local

# 5. 人工审查 SQL：无意外 DROP、类型/默认值/约束正确、向后兼容

# 6. model + 新迁移.sql + atlas.sum 一起提交
git add internal/.../xxx.go atlas-migrations/
git commit -m "feat(db): add status to models"
```

> 不要手写 `.sql` 跳过 `diff`/`hash`。若确需手写 DML（数据订正），写完**必须** `atlas migrate hash --env local` 并连同 `atlas.sum` 一起提交（漏了就会像历史上那次一样 CI `checksum mismatch`）。

### 0.4 遇到 atlas.sum 冲突 / 版本乱序时的「重生成」流程

**不要手动解决 `atlas.sum` 冲突，按下面重生成**：

```bash
# 1. rebase 到最新目标分支（保留别人已合并的迁移）
git fetch origin && git rebase origin/test
# 2. 删掉自己本地新加的那一个迁移文件
rm atlas-migrations/<你的时间戳>_xxx.sql
# 3. 基于最新目录重新生成（时间戳自然排到最后，恢复线性）
make atlas-diff name=xxx
# 4. 重算校验和并校验
atlas migrate hash --env local && atlas migrate validate --env local
# 5. review SQL → 提交
```

### 0.5 团队约定速记

- 一个 PR 一个迁移、逻辑单一、命名 `动词_表_描述`。
- 提交三件套：`model` + `迁移.sql` + `atlas.sum`，缺一不可。
- 破坏性变更走 **Expand-Contract**：先加新列/双写，下个版本再删旧列，保证滚动/蓝绿期间旧代码可运行。
- 本地默认只 `diff` + `validate`；`apply` 交给 CI 或本机临时库，别乱 apply 到共享库。

## 1. 架构概览

### 核心组件

```
GORM Models (3个包)
    ↓
tools/atlas-loader/         ← Go 程序，加载全部 model
    ↓
atlas.hcl                   ← 配置文件（4 环境：local/dev/staging/prod）
    ↓
atlas migrate diff          ← 生成 SQL（开发时）
    ↓
atlas-migrations/*.sql      ← 版本化迁移文件
    ↓
atlas migrate apply         ← 应用迁移（部署时，CI 自动执行）
    ↓
PostgreSQL
```

### 文件结构

```
llm_gateway/
├── atlas.hcl                              # Atlas 配置（4 环境）
├── tools/atlas-loader/                    # GORM schema loader
│   ├── main.go                           # 加载 21 个 model
│   └── go.mod                            # 独立 module
├── atlas-migrations/                      # 迁移文件目录
│   ├── 20260609082623_baseline.sql       # baseline（21 张表）
│   └── atlas.sum                         # 校验文件
├── scripts/atlas-baseline.sh              # 线上 baseline 标记脚本
├── .gitlab-ci.yml                         # migrate stage
├── Makefile                               # atlas-diff / atlas-apply / atlas-status
└── internal/
    ├── controlplane/model/               # 11 文件，15 个 model
    ├── auth/model/                       # 3 文件，3 个 model
    └── observability/model/              # 3 文件，3 个 model
```

### Model 注册清单（21 个）

**controlplane (15):**
- Announcement, AnnouncementRead, Group, GroupModel
- HealthCheckConfig, HealthCheckLog, KeyGroupBinding, ApiKey
- Model, ModelCatalog, ModelTag, ModelTagRelation
- ProviderDefinition, Provider, SystemConfig

**auth (3):**
- Department, DevUser, User

**observability (3):**
- CallLog, CallTrace, UsageDaily

## 2. 开发者工作流（日常使用）

### 标准流程（5 步）

```bash
# 1. 修改 GORM model
vim internal/controlplane/model/provider.go
# 添加字段:
# NewFeatureFlag bool `gorm:"column:new_feature_flag;default:false;not null"`

# 2. 生成迁移（自动检测变化）
make atlas-diff name=add_provider_feature_flag

# 3. 审查生成的 SQL
cat atlas-migrations/*_add_provider_feature_flag.sql
# 确认:
# - ALTER TABLE 正确
# - 列类型正确
# - 默认值正确
# - 无意外的 DROP 语句

# 4. 提交（包含 model + 迁移文件）
git add internal/controlplane/model/provider.go atlas-migrations/
git commit -m "feat(db): add new_feature_flag to providers table"
git push

# 5. CI 自动应用（main/staging 分支）
# GitLab CI migrate stage 自动执行 atlas migrate apply
```

### Makefile 命令

| 命令 | 说明 | 需要 DB |
|------|------|---------|
| `make atlas-diff name=描述` | 生成迁移 SQL | ❌ 不需要 |
| `make atlas-apply` | 应用迁移到本地 DB | ✅ 需要 |
| `make atlas-status` | 查看迁移状态 | ✅ 需要 |

### 关键注意事项

1. **name 参数是必需的**
   ```bash
   make atlas-diff name=add_xxx_field   # ✅ 正确
   make atlas-diff                      # ❌ 错误，缺少 name
   ```

2. **diff 不需要数据库连接**
   - Atlas 使用 docker dev-url (`docker://postgres/16/dev`)
   - 无需设置 `DATABASE_URL` 即可生成 SQL

3. **审查 SQL 是强制步骤**
   - 生成后必须人工审查，确认无误再提交
   - 检查是否有意外的 DROP 语句
   - 验证字段类型、约束、默认值

4. **一次改一件事**
   - 不要在一次 commit 中修改多个无关的 model
   - 每个迁移文件对应一个逻辑变更

## 3. Model 修改模式参考

### 添加字段

```go
// 在 struct 中添加字段
type Provider struct {
    ID        uint   `gorm:"primarykey"`
    Name      string `gorm:"column:name;type:varchar(255);not null"`
    NewField  string `gorm:"column:new_field;type:varchar(100)"`  // ← 新增
    CreatedAt time.Time
}
```

生成的 SQL:
```sql
ALTER TABLE "providers" ADD COLUMN "new_field" character varying(100) NULL;
```

### 修改字段类型

```go
// 修改字段长度
Name string `gorm:"column:name;type:varchar(500);not null"`  // 原 255 → 500
```

生成的 SQL:
```sql
ALTER TABLE "providers" ALTER COLUMN "name" TYPE character varying(500);
```

### 添加索引

```go
type Provider struct {
    Name string `gorm:"column:name;type:varchar(255);index:idx_provider_name"`  // ← 添加 index
}
```

生成的 SQL:
```sql
CREATE INDEX "idx_provider_name" ON "providers" ("name");
```

### 添加唯一约束

```go
type Provider struct {
    Code string `gorm:"column:code;type:varchar(50);uniqueIndex:uniq_provider_code"`
}
```

生成的 SQL:
```sql
CREATE UNIQUE INDEX "uniq_provider_code" ON "providers" ("code");
```

### 添加外键

```go
type Model struct {
    ProviderID uint     `gorm:"column:provider_id;not null"`
    Provider   Provider `gorm:"foreignKey:ProviderID;constraint:OnDelete:CASCADE"`
}
```

### 删除字段

```go
// 注释掉或删除 struct 字段
// OldField string `gorm:"column:old_field"`  // ← 删除此行
```

⚠️ **重要**: Atlas 会生成 `DROP COLUMN`，需谨慎审查！

## 4. 故障排查

### 问题 1: 生成的 SQL 包含意外的 DROP

**症状**:
```sql
-- 意外生成
ALTER TABLE "providers" DROP COLUMN "important_field";
```

**原因**:
- GORM model 中遗漏了该字段
- 字段名拼写错误（`gorm:"column:xxx"` 不匹配）

**解决**:
1. 检查所有 model 文件，确认字段存在
2. 验证 `gorm:"column:xxx"` tag 与数据库列名一致
3. 重新运行 `make atlas-diff`
4. **绝对不要提交包含意外 DROP 的迁移文件**

### 问题 2: atlas-loader 编译失败

**症状**:
```
Error: failed to load gorm schema
```

**排查步骤**:
```bash
# 1. 手动编译 atlas-loader
cd tools/atlas-loader
go mod tidy
go build -o /dev/null .

# 2. 检查 model import
grep "internal/controlplane/model" main.go
grep "internal/auth/model" main.go
grep "internal/observability/model" main.go

# 3. 验证能生成 SQL
go run main.go | head -50
```

**常见原因**:
- 主项目 `go.mod` 更新后未 `go mod tidy`
- model 文件中有语法错误
- `replace` directive 路径错误

### 问题 3: diff 检测不到变化

**症状**:
```
The migration directory is synced with the desired state, no changes to be made
```

但明明修改了 model。

**原因**:
- atlas-loader 缓存了旧代码
- `gorm` tag 写法错误

**解决**:
```bash
# 1. 清理缓存
cd tools/atlas-loader
go clean -cache

# 2. 验证 model tag
# 确保 `gorm:"column:xxx;type:yyy"` 正确

# 3. 手动运行 loader 查看输出
go run main.go | grep "new_field"  # 查找新字段
```

### 问题 4: CI migrate 失败

**症状**:
GitLab CI `migrate` stage 报错

**排查清单**:
1. **检查 DATABASE_URL 环境变量**
   - CI job 中是否正确设置
   - 格式: `postgres://user:pass@host:port/db?sslmode=disable`

2. **检查迁移文件完整性**
   ```bash
   atlas migrate validate --env local
   ```

3. **检查 baseline 是否已标记**
   - 首次部署时必须先标记 baseline
   - 使用 `scripts/atlas-baseline.sh dev` 标记

4. **检查 atlas.sum 完整性**
   - 每次生成迁移后会更新 `atlas.sum`
   - 必须一起提交

### 问题 5: 本地 apply 失败

**症状**:
```
make atlas-apply
Error: sql/sqlclient: missing driver
```

**原因**: 未设置 `DATABASE_URL`

**解决**:
```bash
# 1. 自备本地 Postgres（Makefile 已无 dev-infra 目标，用本机或临时容器）
#    docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16

# 2. 设置环境变量
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/llm_gateway?sslmode=disable"

# 3. 应用迁移
make atlas-apply
```
> 注意：`apply` 会写真实库的 `atlas_schema_revisions`。开发期默认只 `diff`+`validate`，不要把 `apply` 指向团队共享开发库（见 §0.1）。

## 5. 首次部署到线上环境

### 前置条件

- 线上数据库已有表和数据
- baseline 迁移已生成（`atlas-migrations/20260609082623_baseline.sql`）

### 操作步骤

```bash
# 1. 连接到目标环境服务器（dev/staging/prod）

# 2. 设置数据库连接
export DATABASE_URL="postgres://user:pass@host:port/db?sslmode=disable"

# 3. 执行 baseline 标记（告诉 Atlas 现有表已存在）
./scripts/atlas-baseline.sh dev   # 或 staging / prod

# 4. 验证标记成功
atlas migrate status --env dev
# 应该显示 baseline 版本已应用
```

### 脚本说明 (scripts/atlas-baseline.sh)

```bash
#!/bin/bash
# 功能: 在已有数据的环境标记 baseline
# 用法: ./scripts/atlas-baseline.sh <env>
#   env: dev, staging, prod
# 依赖: 
#   - DATABASE_URL 环境变量
#   - atlas CLI
#   - atlas.hcl 中对应的 env 配置
```

**重要**: 每个环境只需标记一次，之后所有迁移正常 apply 即可。

## 6. CI/CD 集成

### GitLab CI 配置

```yaml
# .gitlab-ci.yml
stages:
  - build
  - migrate    # ← 在 deploy 之前
  - deploy

migrate:
  stage: migrate
  image: arigaio/atlas:latest
  needs: [build]
  script:
    - atlas migrate apply --env ${CI_ENVIRONMENT_NAME:-local}
  rules:
    - if: '$CI_COMMIT_BRANCH == "main" || $CI_COMMIT_BRANCH == "staging"'
```

### 执行时机

| 分支 | 触发条件 | 目标环境 |
|------|----------|----------|
| main | 合并后自动 | production |
| staging | 合并后自动 | staging |
| feature/* | ❌ 不触发 | - |

### CI 环境变量

必须在 GitLab CI/CD Settings 中设置：

- `DATABASE_URL` (protected, masked)
- 格式: `postgres://user:pass@host:port/db?sslmode=disable`

## 7. atlas.hcl 配置说明

### 环境配置

```hcl
data "external_schema" "gorm" {
  program = [
    "bash",
    "-c",
    "cd tools/atlas-loader && go run main.go"
  ]
}

env "local" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/16/dev?search_path=public"  # ← 不需要真实 DB
  url = getenv("DATABASE_URL")                         # ← 从环境变量读取
  migration {
    dir = "file://atlas-migrations"
  }
}

# dev / staging / prod 环境类似，只有 url 不同
```

### 关键参数

| 参数 | 说明 |
|------|------|
| `src` | 期望的 schema（从 GORM models 生成） |
| `dev` | 开发数据库（用于计算 diff，使用 docker 无需本地 Postgres） |
| `url` | 目标数据库（apply 时连接） |
| `migration.dir` | 迁移文件存储目录 |

## 8. 高级场景

### 场景 1: 数据迁移（不仅 DDL）

如果需要在 schema 变更后同步迁移数据：

```sql
-- atlas-migrations/YYYYMMDDHHMMSS_migrate_data.sql

-- 1. DDL（Atlas 自动生成）
ALTER TABLE "providers" ADD COLUMN "status" character varying(20) NULL;

-- 2. DML（手动添加）
UPDATE "providers" SET "status" = 'active' WHERE "status" IS NULL;

-- 3. 约束（手动添加）
ALTER TABLE "providers" ALTER COLUMN "status" SET NOT NULL;
```

⚠️ **注意**: 
- Atlas 只生成 DDL
- DML 需手动编辑迁移文件
- 编辑后更新 `atlas.sum`: `atlas migrate hash --env local`

### 场景 2: 多人协作冲突（atlas.sum 冲突 / 版本乱序）

**问题**: 两个开发者在各自分支同时生成迁移，合并时 `atlas.sum` 冲突或出现版本乱序。

**解决**: 见 **第 0.4 节「重生成流程」**（rebase 最新目标分支 → 删自己那个迁移文件 → `make atlas-diff` 重新生成 → `atlas migrate hash` + `validate`）。**切勿手动编辑 `atlas.sum`** 去解决冲突。根因与预防见 §0.1/§0.2。

### 场景 3: 回滚迁移

**Atlas 不支持自动 down migration**（项目配置决策）。

手动回滚步骤：
```bash
# 1. 手动编写逆向 SQL
# 例如原迁移添加了字段，现在要删除
cat > rollback.sql << EOF
ALTER TABLE "providers" DROP COLUMN "new_field";
EOF

# 2. 连接数据库手动执行
psql $DATABASE_URL -f rollback.sql

# 3. 更新 Atlas 迁移历史（删除已应用的记录）
psql $DATABASE_URL -c "DELETE FROM atlas_schema_revisions WHERE version = 'YYYYMMDDHHMMSS';"
```

⚠️ **生产环境慎用**，建议通过新迁移修复问题而非回滚。

### 场景 4: 跨环境 schema 同步检查

验证 dev/staging/prod schema 一致性：

```bash
# 1. 对每个环境运行
export DATABASE_URL="postgres://..."
atlas schema diff --env dev --from "file://atlas-migrations" --to "env://url"

# 2. 如果有差异，会输出需要的 SQL
# 3. 如果输出 "Schemas are synced, no changes to be made" 则一致
```

## 9. 团队协作规范

### Pull Request 检查清单

提交包含数据库变更的 PR 时，reviewer 必须检查：

- [ ] GORM model 修改合理（字段名、类型、tag）
- [ ] 生成的 SQL 正确（无意外 DROP，类型匹配）
- [ ] 迁移文件命名清晰（`name` 参数有意义）
- [ ] `atlas.sum` 已更新并一起提交
- [ ] 对现有数据的影响已评估（是否需要 DML）
- [ ] 向后兼容性（蓝绿部署时旧代码能否正常运行）

### 命名约定

迁移文件 `name` 参数建议格式：

```bash
# 格式: 动词_表名_描述
make atlas-diff name=add_providers_status_field
make atlas-diff name=create_audit_logs_table
make atlas-diff name=modify_models_context_length_type
make atlas-diff name=drop_deprecated_fields
```

### 分支策略

```
feature/add-xxx-field
    ↓ PR + Review
main (baseline + 迁移文件)
    ↓ CI migrate apply
production database
```

## 10. 常用命令速查

### 开发时

```bash
# 生成迁移
make atlas-diff name=描述

# 查看生成的 SQL
cat atlas-migrations/*_描述.sql

# 验证迁移文件
atlas migrate validate --env local

# 本地应用迁移（需要 DATABASE_URL）
make atlas-apply

# 查看迁移状态
make atlas-status
```

### 运维时

```bash
# 标记 baseline（首次部署）
./scripts/atlas-baseline.sh dev

# 查看迁移历史
atlas migrate status --env dev

# 手动应用迁移（跳过 CI）
export DATABASE_URL="..."
atlas migrate apply --env dev

# 验证 schema 一致性
atlas schema diff --env dev --from "file://atlas-migrations" --to "env://url"
```

### 调试时

```bash
# 检查 atlas-loader
cd tools/atlas-loader
go run main.go | head -50

# 重新编译 atlas-loader
cd tools/atlas-loader
go mod tidy && go build .

# 查看 Atlas 版本
atlas version

# 强制重新计算 hash
atlas migrate hash --env local
```

## 11. 参考资料

### 官方文档

- [Atlas 官网](https://atlasgo.io/)
- [GORM Provider](https://atlasgo.io/guides/orms/gorm)
- [Versioned Migrations](https://atlasgo.io/versioned/intro)
- [GitLab CI 集成](https://atlasgo.io/guides/ci-cd/gitlab)

### 项目文档

- `docs/database.md` - 数据库部署与迁移规范
- `README.md` - 项目快速开始
- `.sisyphus/evidence/F1-summary.md` - 完整验证报告

### 紧急联系

遇到严重问题（如生产迁移失败）时：
1. 立即停止 CI pipeline
2. 检查 `atlas_schema_revisions` 表（Atlas 迁移历史）
3. 联系 DBA 评估影响
4. 准备手动回滚脚本

---

## 总结

使用此 skill 可以：
- ✅ 安全地修改数据库 schema（声明式，可审查）
- ✅ 自动化迁移流程（CI/CD 集成）
- ✅ 版本控制迁移历史（Git + atlas.sum）
- ✅ 团队协作无冲突（基于文件的迁移）
- ✅ 快速排查问题（完整的故障排查手册）

**核心理念**: 
- Schema as Code（GORM models 是唯一真相源）
- 声明式迁移（描述期望状态，Atlas 生成 SQL）
- 审查优先（生成后必须人工审查）
- 渐进式部署（baseline → 增量迁移）
