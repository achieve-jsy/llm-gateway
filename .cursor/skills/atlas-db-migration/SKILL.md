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

### 0.6 生成前查所有目标分支的最新时间戳

> 合并多个目标分支（如同时合并 test 和 main）时，**只查一个目标分支**容易踩坑：你的迁移合并 A 分支没问题，合并 B 分支时乱序。

#### 生成迁移前必查所有目标分支

```bash
# 查所有目标分支的最新迁移时间戳（test / main / staging）
for branch in test main; do
  latest=$(git ls-tree origin/$branch atlas-migrations/ 2>/dev/null | awk '{print $4}' | sort | tail -1)
  echo "$branch: $latest"
done
```

#### 规则
你的新迁移时间戳必须 **严格大于所有目标分支**（包括已知待合并的其他分支）的最新时间戳。

#### 示例
```
test: 20260622180321_add_auto_strategy_and_complexity_tier.sql
main: 20260624120000_add_budget_chain_field.sql

你的时间戳应该 ≥ 20260625000000_xxx.sql
```

#### 为什么不能只查一个
假设只查 test（最新 20260622），生成 20260623，合并 test 通过。但 main 上已有 20260624 → 合并 main 时报乱序。**必须合并前查所有目标分支的最大值再加时间戳**。

#### 快速判断脚本
可加一个 alias 到 `~/.zshrc`：
```bash
alias atlas-check-all='for b in test main staging; do echo "$b: $(git ls-tree origin/$b atlas-migrations/ 2>/dev/null | awk '"'"'{print $4}'"'"' | sort | tail -1)"; done'
```

### 0.7 多分支合并顺序原则

> 分支要合并到多个目标分支时，**合并顺序影响成功率**。

#### 规则：先合并时间戳晚的分支，再合并早的

如果两个目标分支的最新迁移时间戳不同：
```bash
main: 20260624120000_xxx.sql   # 时间戳更晚
test: 20260622180321_yyy.sql   # 时间戳更晚
```

合并顺序应该是：**先 main，后 test**。

#### 为什么？
假设 feature 分支的迁移文件时间戳是 `20260625200000_zzz.sql`：
- **先合并 main**（main 最新 20260624，你的 20260625 > 20260624 ✅）
- **后合并 test**（test 最新 20260622，你的 20260625 > 20260622 ✅）

反例：先合并 test，后合并 main → main 合并时会乱序（你的 time 20260625 仍然大于 20260624，理论上能过，但实际如果 main 上有人刚 push 了 20260626 就会乱序）。

#### 更保险的规则
**按目标分支最新时间戳从晚到早排序**，依次合并。这样每次合并时，feature 分支的时间戳都晚于目标分支最新版。

#### 流程图
```
feature 分支（时间戳 20260625）
    ↓ merge main（main 最新 20260624）→ ✅ 通过
    ↓ merge test（test 最新 20260622）→ ✅ 通过
```

### 0.8 迁移不可变原则（关键铁律）

> 迁移文件**一旦 commit 到任何分支，就不应该再改**（包括文件名、时间戳、SQL 内容）。

#### 为什么？
- 同一个迁移可能被 apply 到多个 DB（test / staging / prod）
- 改时间戳后 Git 认为是新文件
- 旧文件在 DB 成了**孤儿记录**（`atlas_schema_revisions` 有记录但目录没文件）
- 新文件会被重新 apply 一次
- 跨环境同步时出问题

#### 典型踩坑场景
1. **合并后改时间戳**：feature 分支 `20260623` 已合并 test 并 apply → DB 有 20260623 记录 → 你改成 `20260624` 重新提交 → DB 有 20260623 孤儿 + 20260624 重复
2. **多人合并时时间戳撞车**：Dev A 用 `20260625`，Dev B 也用 `20260625`，各自合并 → 后合并的人产生孤儿

#### 正确做法
- **生成前**：按 §0.6 查所有目标分支，取最大时间戳 +1 天
- **生成后**：一旦 commit 到远端，**绝不修改文件名或时间戳**
- **必须调整**时：见 §8 高级场景的「回滚迁移」流程

#### 例外（仅本地开发时）
迁移**只在你本地 feature 分支**，还没 push 到任何共享分支，且还没 apply 到任何真实 DB → 此时可以自由改时间戳。

### 0.9 迁移 SQL 幂等性强制模板

> **所有手写 SQL（非 Atlas 自动生成的）必须使用幂等形式**，确保跨环境 DB 状态不一致时也能 apply 成功。

#### 为什么需要幂等？
| 场景 | 没 IF NOT EXISTS | 有 IF NOT EXISTS |
|------|-----------------|------------------|
| test DB 有表，main DB 没表 | main 报错「表已存在」 | ✅ 都正常 apply |
| 孤儿记录清理后重 apply | 报错「已存在」 | ✅ 跳过不冲突 |
| 部署回滚后再部署 | 报错「已存在」 | ✅ 跳过不冲突 |

#### 标准模板（强制使用）

```sql
-- 新建表（必须用 IF NOT EXISTS）
CREATE TABLE IF NOT EXISTS "audit_logs" (
  "id"          uuid          NOT NULL DEFAULT gen_random_uuid(),
  "created_at"  timestamptz   NOT NULL DEFAULT now(),
  "action"      varchar(64)   NOT NULL,
  PRIMARY KEY ("id")
);

-- 新建索引（必须用 IF NOT EXISTS）
CREATE INDEX IF NOT EXISTS "idx_audit_logs_created_at" 
  ON "audit_logs" ("created_at");

-- 新建唯一约束（必须用 IF NOT EXISTS + 条件索引）
CREATE UNIQUE INDEX IF NOT EXISTS "uniq_user_email" 
  ON "users" ("email") WHERE "deleted_at" IS NULL;

-- 加字段（必须用 IF NOT EXISTS）
ALTER TABLE "api_keys"
  ADD COLUMN IF NOT EXISTS "ip_allowlist" text NOT NULL DEFAULT '';

-- 改字段类型（PostgreSQL 不支持 IF NOT EXISTS，需要 DO $$ 块）
DO $$ 
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                 WHERE table_name = 'users' AND column_name = 'name') THEN
    ALTER TABLE "users" ALTER COLUMN "name" TYPE character varying(500);
  END IF;
END $$;

-- 加约束（必须用 IF NOT EXISTS）
ALTER TABLE "api_keys"
  ADD CONSTRAINT IF NOT EXISTS "chk_key_type_valid"
  CHECK (key_type IN ('personal', 'service'));

-- 删字段（生产慎用，必须用 IF EXISTS）
ALTER TABLE "providers"
  DROP COLUMN IF EXISTS "deprecated_field";

-- 删索引（必须用 IF EXISTS）
DROP INDEX IF EXISTS "idx_old_index";
```

#### 检查清单（提交前必查）

- [ ] 所有 `CREATE TABLE` 后有 `IF NOT EXISTS`
- [ ] 所有 `CREATE INDEX` 后有 `IF NOT EXISTS`
- [ ] 所有 `ALTER TABLE ADD COLUMN` 后有 `IF NOT EXISTS`
- [ ] 所有 `DROP COLUMN` / `DROP INDEX` / `DROP TABLE` 后有 `IF EXISTS`
- [ ] 没有裸的破坏性 DDL（除非明确是 §8.3 回滚迁移）

#### Atlas 自动生成 vs 手写
- **Atlas 自动生成的 SQL**：假设线性历史，**不加 IF NOT EXISTS**，靠 `atlas_schema_revisions` 记录保证只 apply 一次
- **手写 SQL**：必须加 IF NOT EXISTS，因为可能因各种原因被重复 apply

### 0.10 无 Docker 环境应急方案

> 当 `make atlas-diff` 因 Docker 不可用（`Unable to find image 'postgres:16'` 或 `docker-credential-desktop not found`）报错时，用此应急方案。

#### 触发场景
- 本地 Docker Desktop 未启动
- Docker 凭证配置损坏（`~/.docker/config.json` 中 `credsStore` 项有问题）
- 无权限访问 Docker（CI 环境中）
- 离线或弱网环境下拉取镜像失败

#### 应急流程

##### 1. 手写 SQL（必须用 §0.9 模板）

直接手写 SQL，但**必须严格使用 IF NOT EXISTS 模板**保证幂等。

```bash
# 创建迁移文件
touch atlas-migrations/$(date +%Y%m%d%H%M%S)_[动词]_[表名]_[描述].sql
# 例：touch atlas-migrations/20260625100000_add_audit_logs_table.sql
```

##### 2. 重算校验和（不需要 Docker）

```bash
atlas migrate hash --env local
```

##### 3. 人工审查 SQL（代替自动 diff 的审查）

- 确认无意外 DROP
- 确认类型/默认值/约束正确
- **确认全部加 IF NOT EXISTS**
- 确认与 GORM model 的字段一一对应

##### 4. 提交三件套（同 §0.5）

```bash
git add internal/.../xxx.go atlas-migrations/
git commit -m "feat(db): add audit_logs table (no-docker emergency)"
```

#### 事后必须做的
一旦 Docker 可用，用 `atlas migrate validate --env local` 重新校验一次。如果校验失败，按 §0.4 流程重生成。

#### Docker 修复命令
常见 Docker 凭证问题的修复：
```bash
# 检查 ~/.docker/config.json
cat ~/.docker/config.json
# 如果 credsStore 项是 "desktop" 且 Docker Desktop 未安装/未启动：
# 方案 A：启动 Docker Desktop
# 方案 B：临时删除 credsStore 项（会影响 Docker Hub 登录）
#   sed -i.bak '/"credsStore"/d' ~/.docker/config.json
```

#### 何时用 vs 何时不用
| 场景 | 推荐做法 |
|------|---------|
| Docker 正常启动 | ❌ 不用应急方案，走标准 §0.3 流程 |
| Docker 暂时故障（重启可恢复） | 修 Docker，不用应急方案 |
| Docker 长期不可用（无权限/离线） | ✅ 用应急方案，事后补 validate |
| CI 环境 Docker 不可用 | ⚠️ 优先修 CI 环境；应急方案是最后手段 |

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

### 2.5 Pre-commit Hook：本地自动校验（强烈建议安装）

> 通过 Git hook 在本地 `git commit` 前自动拦截常见错误，避免推到 CI 后才发现问题。**这是 Phase 2 自动化机制的第一层防线**。

#### Hook 脚本

创建文件 `.git/hooks/pre-commit`（无 `.sample` 后缀）：

```bash
#!/bin/bash
# Atlas 迁移本地 pre-commit 校验
# 安装：chmod +x .git/hooks/pre-commit

set -e

# 仅当有 atlas-migrations/ 或相关 model 变更时触发
CHANGED_FILES=$(git diff --cached --name-only)
if ! echo "$CHANGED_FILES" | grep -qE "(atlas-migrations/|model/.*\.go$)"; then
    exit 0
fi

echo "🔍 Atlas: 检测到数据库相关变更，运行自动校验..."

# 1. 检查是否有新增/修改的迁移文件
NEW_MIGRATIONS=$(echo "$CHANGED_FILES" | grep "atlas-migrations/.*\.sql$" | grep -v "atlas\.sum" || true)
if [ -n "$NEW_MIGRATIONS" ]; then
    echo "📄 发现新增/修改迁移文件："
    echo "$NEW_MIGRATIONS" | sed 's/^/   - /'
    
    # 2. 检查时间戳：必须晚于所有远程分支的最新迁移
    for branch in test staging main; do
        TARGET_LATEST=$(git ls-tree -r origin/$branch atlas-migrations/ 2>/dev/null | awk '{print $4}' | grep -v "atlas\.sum" | sort | tail -1 || true)
        if [ -n "$TARGET_LATEST" ]; then
            TARGET_TS=$(basename "$TARGET_LATEST" | cut -d_ -f1)
            for file in $NEW_MIGRATIONS; do
                FILE_BASE=$(basename "$file")
                FILE_TS=$(echo "$FILE_BASE" | cut -d_ -f1)
                # 比较时间戳（字符串比较，格式 YYYYMMDDHHMMSS）
                if [[ "$FILE_TS" < "$TARGET_TS" ]]; then
                    echo "❌ 错误：$FILE_BASE 时间戳早于远程分支 $branch"
                    echo "   远程最新: $TARGET_BASE (来自 $branch)"
                    echo "   你的文件: $FILE_BASE"
                    echo "💡 修复：修改迁移文件名，使用大于 $TARGET_TS 的时间戳"
                    exit 1
                fi
            done
        fi
    done
    echo "✅ 时间戳校验通过"
    
    # 3. 检查 SQL 中是否包含 IF NOT EXISTS
    for file in $NEW_MIGRATIONS; do
        CONTENT=$(cat "$file")
        if echo "$CONTENT" | grep -qE "(CREATE TABLE|CREATE INDEX|ALTER TABLE.*ADD COLUMN)" | grep -qvE "(IF NOT EXISTS|IF EXISTS)"; then
            echo "⚠️ 警告：$file 的 DDL 语句未使用 IF NOT EXISTS"
            echo "   建议：参考 skill §0.9 使用幂等模板"
            echo "   （不强制阻止，但强烈建议加上）"
            # 注意：这里仅警告，不 exit 1，因为有时确实不需要
        fi
    done
    echo "✅ SQL 幂等性检查完成"
fi

# 4. 验证 atlas.sum 存在且已同步
if echo "$CHANGED_FILES" | grep -q "atlas-migrations/.*\.sql$"; then
    if ! echo "$CHANGED_FILES" | grep -q "atlas-migrations/atlas\.sum$"; then
        echo "❌ 错误：新增了迁移文件但未更新 atlas.sum"
        echo "💡 修复：运行以下命令后重新提交"
        echo "   atlas migrate hash --env local"
        exit 1
    fi
    echo "✅ atlas.sum 已更新"
fi

# 5. 验证 atlas.sum 内容（需要 atlas CLI）
if command -v atlas &> /dev/null; then
    if ! atlas sum --dir atlas-migrations --format '{{.Name}}' 2>/dev/null | grep -q "atlas.sum"; then
        echo "⚠️ 警告：atlas.sum 校验失败，建议重新生成"
        echo "💡 修复：atlas migrate hash --env local"
    fi
fi

echo "✅ 所有校验通过，可以提交"
```

#### 安装步骤

```bash
# 1. 创建 hook 文件
cat > .git/hooks/pre-commit << 'EOF'
# （上面脚本内容）
EOF

# 2. 添加执行权限
chmod +x .git/hooks/pre-commit

# 3. 测试
git commit -m "test hook" --allow-empty
```

#### Hook 校验项目

| 校验项 | 触发条件 | 失败后果 | 依据 |
|--------|----------|----------|------|
| 时间戳校验 | 所有远程分支的最新迁移 | ❌ 阻止提交 | §0.6 |
| SQL 幂等性 | CREATE/ALTER DDL | ⚠️ 警告 | §0.9 |
| atlas.sum 存在性 | 新增迁移文件 | ❌ 阻止提交 | §0.5 |
| atlas.sum 有效性 | atlas CLI 可用 | ⚠️ 警告 | §0.3 |

#### 团队安装（推荐）

为避免每个开发者手动创建，可提交到仓库并配置 `core.hooksPath`：

```bash
# 1. 提交到 .git-hooks/ 目录（注意不是 .git/hooks/）
mkdir -p .git-hooks
cp .git/hooks/pre-commit .git-hooks/pre-commit
git add .git-hooks/
git commit -m "chore: add pre-commit hook for atlas migrations"

# 2. 团队统一配置（加到 README 或 onboarding 文档）
git config core.hooksPath .git-hooks
```

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

### 6.5 CI Automation：合并前自动校验

> **Phase 2 自动化机制的第二层防线**：在 MR 合并到目标分支前，CI 自动拦截所有可能的迁移冲突，避免问题进入共享分支。

#### CI 配置示例

在 `.gitlab-ci.yml` 中新增 `validate-atlas` job（在 `migrate` job 之前）：

```yaml
validate-atlas:
  stage: test  # 必须在 migrate 之前
  image: 
    name: arigaio/atlas:latest
    entrypoint: [""]
  script:
    - |
      echo "🔍 Atlas: CI 自动校验开始..."
      
      # 1. 检查新增的迁移文件
      NEW_MIGRATIONS=$(git diff --name-only origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME...HEAD \
        | grep "atlas-migrations/.*\.sql$" \
        | grep -v "atlas\.sum" || true)
      
      if [ -z "$NEW_MIGRATIONS" ]; then
        echo "✅ 本次 MR 无新增迁移文件，跳过校验"
        exit 0
      fi
      
      echo "📄 发现新增迁移文件："
      echo "$NEW_MIGRATIONS" | sed 's/^/   - /'
      
      # 2. 时间戳校验：必须晚于所有远程分支的最新迁移
      for branch in main staging test; do
        TARGET_LATEST=$(git ls-tree -r origin/$branch atlas-migrations/ 2>/dev/null \
          | awk '{print $4}' \
          | grep -v "atlas\.sum" \
          | sort \
          | tail -1 || true)
        
        if [ -n "$TARGET_LATEST" ]; then
          TARGET_BASE=$(basename "$TARGET_LATEST")
          TARGET_TS=$(echo "$TARGET_BASE" | cut -d_ -f1)
          
          for file in $NEW_MIGRATIONS; do
            FILE_BASE=$(basename "$file")
            FILE_TS=$(echo "$FILE_BASE" | cut -d_ -f1)
            
            if [[ "$FILE_TS" < "$TARGET_TS" ]]; then
              echo "❌ 错误：$FILE_BASE 时间戳早于远程分支 $branch"
              echo "   远程最新: $TARGET_BASE (来自 $branch)"
              echo "   你的文件: $FILE_BASE"
              echo "💡 修复："
              echo "   修改迁移文件名，使用大于 $TARGET_TS 的时间戳"
              echo "   然后运行: atlas migrate hash --env local"
              exit 1
            fi
          done
        fi
      done
      echo "✅ 时间戳校验通过"
      
      # 3. SQL 幂等性强制检查
      for file in $NEW_MIGRATIONS; do
        CONTENT=$(cat "$file")
        
        # 检查 CREATE TABLE
        if echo "$CONTENT" | grep -E "^\s*CREATE TABLE\b" | grep -qvE "IF NOT EXISTS"; then
          echo "❌ 错误：$file 包含 CREATE TABLE 但未使用 IF NOT EXISTS"
          echo "💡 修复：参考 skill §0.9 使用幂等模板"
          exit 1
        fi
        
        # 检查 CREATE INDEX
        if echo "$CONTENT" | grep -E "^\s*CREATE INDEX\b" | grep -qvE "IF NOT EXISTS"; then
          echo "❌ 错误：$file 包含 CREATE INDEX 但未使用 IF NOT EXISTS"
          echo "💡 修复：参考 skill §0.9 使用幂等模板"
          exit 1
        fi
        
        # 检查 DROP TABLE（必须 IF EXISTS）
        if echo "$CONTENT" | grep -E "^\s*DROP TABLE\b" | grep -qvE "IF EXISTS"; then
          echo "❌ 错误：$file 包含 DROP TABLE 但未使用 IF EXISTS"
          echo "💡 修复：使用 DROP TABLE IF EXISTS 或直接删除"
          exit 1
        fi
        
        # 检查 ALTER TABLE DROP COLUMN（必须 IF EXISTS）
        if echo "$CONTENT" | grep -E "^\s*ALTER TABLE.*DROP COLUMN\b" | grep -qvE "IF EXISTS"; then
          echo "❌ 错误：$file 包含 DROP COLUMN 但未使用 IF EXISTS"
          echo "💡 修复：使用 ALTER TABLE xxx DROP COLUMN IF EXISTS"
          exit 1
        fi
      done
      echo "✅ SQL 幂等性检查通过"
      
      # 4. atlas.sum 校验
      if ! atlas migrate hash --env local --dry-run 2>&1 | grep -q "atlas.sum is up to date"; then
        echo "❌ 错误：atlas.sum 与迁移文件不匹配"
        echo "💡 修复：运行以下命令并重新提交"
        echo "   atlas migrate hash --env local"
        exit 1
      fi
      echo "✅ atlas.sum 校验通过"
      
      echo ""
      echo "🎉 所有 CI 校验通过"
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
      changes:
        - atlas-migrations/**
        - internal/**/model/**
  allow_failure: false  # 必须通过才能合并

```

#### 校验项目对比

| 校验项 | Pre-commit Hook (§2.5) | CI Automation (§6.5) | 差异 |
|--------|------------------------|----------------------|------|
| 时间戳校验 | ⚠️ 仅警告 | ❌ 阻止合并 | CI 更严格 |
| SQL 幂等性 | ⚠️ 仅警告 | ❌ 阻止合并 | CI 更严格 |
| atlas.sum 存在性 | ❌ 阻止提交 | ❌ 阻止合并 | 一致 |
| atlas.sum 有效性 | ⚠️ 仅警告 | ❌ 阻止合并 | CI 更严格 |

**设计理念**：
- Pre-commit Hook 偏向"友好提示"，允许开发者在知晓风险时跳过（`git commit --no-verify`）
- CI Automation 偏向"强制执行"，确保问题不会进入共享分支

### 6.6 CI 迁移失败自动检测与重试（Dry-run 机制）

> **Phase 2 自动化机制的第三层防线**：在真正 apply 迁移前，先用 dry-run 模式检测所有潜在问题，并尝试自动修复。

#### CI 配置示例

修改 `.gitlab-ci.yml` 中的 `migrate` job：

```yaml
migrate:
  stage: migrate
  image: 
    name: arigaio/atlas:latest
    entrypoint: [""]
  needs: 
    - job: build
      artifacts: false
    - job: validate-atlas  # 必须先通过校验
  before_script:
    - apk add --no-cache postgresql-client jq
  script:
    - |
      echo "🚀 Atlas: 开始迁移流程"
      
      # 1. Dry-run 检测：模拟 apply 但不实际执行
      echo "📋 Step 1: Dry-run 检测"
      DRY_RUN_LOG=$(mktemp)
      
      if atlas migrate apply --env ${CI_ENVIRONMENT_NAME:-local} \
        --dry-run \
        --format '{{ json . }}' > "$DRY_RUN_LOG" 2>&1; then
        
        echo "✅ Dry-run 成功，准备 apply 以下迁移："
        cat "$DRY_RUN_LOG" | jq -r '.pending[] | "   - \(.Name) (状态: \(.Status))"' 2>/dev/null || cat "$DRY_RUN_LOG"
        
      else
        DRY_RUN_ERROR=$(cat "$DRY_RUN_LOG")
        echo "❌ Dry-run 失败："
        echo "$DRY_RUN_ERROR"
        
        # 2. 自动诊断与修复尝试
        echo ""
        echo "🔧 Step 2: 自动诊断与修复"
        
        # 2.1 检查是否是 atlas.sum 不匹配问题
        if echo "$DRY_RUN_ERROR" | grep -q "checksum mismatch"; then
          echo "⚠️ 检测到 atlas.sum 不匹配，尝试自动修复..."
          atlas migrate hash --env local
          git add atlas-migrations/atlas.sum
          git commit -m "chore(atlas): auto-fix atlas.sum checksum [skip ci]" || true
          echo "✅ 已修复 atlas.sum，重新尝试 dry-run"
          
          # 重新 dry-run
          if atlas migrate apply --env ${CI_ENVIRONMENT_NAME:-local} --dry-run > "$DRY_RUN_LOG" 2>&1; then
            echo "✅ 修复后 dry-run 成功"
          else
            echo "❌ 修复后仍失败，需要人工介入"
            echo "📞 请联系团队，参考 skill §8.5（孤儿记录修复）"
            exit 1
          fi
        fi
        
        # 2.2 检查是否是 missing migration 问题
        if echo "$DRY_RUN_ERROR" | grep -q "missing migration"; then
          MISSING_VERSION=$(echo "$DRY_RUN_ERROR" | grep -oP 'missing migration file: \K\d+' || echo "")
          if [ -n "$MISSING_VERSION" ]; then
            echo "⚠️ 检测到孤儿记录（版本: $MISSING_VERSION），需要人工介入"
            echo "📞 请联系 DBA，参考 skill §8.5（孤儿记录修复）"
            echo ""
            echo "紧急修复步骤："
            echo "1. 连接数据库: psql \$DATABASE_URL"
            echo "2. 查看孤儿记录: SELECT version FROM atlas_schema_revisions WHERE version NOT IN (...);"
            echo "3. 删除孤儿: DELETE FROM atlas_schema_revisions WHERE version = '$MISSING_VERSION';"
            exit 1
          fi
        fi
        
        # 2.3 检查是否是 out-of-order 问题
        if echo "$DRY_RUN_ERROR" | grep -q "out of order"; then
          echo "⚠️ 检测到迁移乱序，需要人工介入"
          echo "📞 请联系团队，参考 skill §0.7（多分支合并顺序）和 §8.6（长周期分支）"
          exit 1
        fi
        
        # 2.4 其他未知错误
        echo "⚠️ 未知错误，需要人工诊断"
        echo "📞 请联系团队，提供以下信息："
        echo "  - CI 日志链接: $CI_JOB_URL"
        echo "  - 错误信息: $DRY_RUN_ERROR"
        exit 1
      fi
      
      # 3. 真正 apply 迁移
      echo ""
      echo "🎯 Step 3: Apply 迁移"
      atlas migrate apply --env ${CI_ENVIRONMENT_NAME:-local}
      
      # 4. 验证迁移结果
      echo ""
      echo "✅ Step 4: 验证迁移结果"
      atlas migrate status --env ${CI_ENVIRONMENT_NAME:-local}
      
      echo ""
      echo "🎉 迁移流程完成！"
  after_script:
    - |
      # 清理临时文件
      rm -f "$DRY_RUN_LOG" || true
  rules:
    - if: '$CI_COMMIT_BRANCH == "main" || $CI_COMMIT_BRANCH == "staging"'
      changes:
        - atlas-migrations/**
  when: on_success
  retry:
    max: 2  # 最多重试 2 次
    when:
      - runner_system_failure
      - stuck_or_timeout_failure
```

#### 自动修复策略

| 问题类型 | 检测方式 | 自动修复 | 人工介入 |
|----------|----------|----------|----------|
| atlas.sum 不匹配 | `grep "checksum mismatch"` | ✅ `atlas migrate hash` | ❌ |
| orphaned migration | `grep "missing migration"` | ❌ | ✅ 需 DBA |
| out-of-order | `grep "out of order"` | ❌ | ✅ 需重命名 |
| SQL 语法错误 | dry-run 失败 | ❌ | ✅ 需修复 SQL |
| 权限不足 | `permission denied` | ❌ | ✅ 需 DBA |
| 网络超时 | `timeout` | ✅ 自动重试 | ❌ |

#### Dry-run 输出解析

Atlas 的 dry-run 输出是 JSON 格式：

```json
{
  "current": "20260622180321",
  "next": "20260623010000",
  "pending": [
    {
      "Name": "20260623010000_add_audit_logs",
      "Status": "pending",
      "Description": "添加审计日志表"
    },
    {
      "Name": "20260624010000_add_ip_allowlist",
      "Status": "pending",
      "Description": "添加 IP 白名单字段"
    }
  ],
  "applied": 15,
  "pending_count": 2,
  "ok": true
}
```

可以用 `jq` 命令解析：

```bash
# 查看所有待 apply 的迁移
cat dry-run.log | jq -r '.pending[] | "📄 \(.Name) - \(.Description)"'

# 查看已 apply 的总数
cat dry-run.log | jq '.applied'

# 检查是否有问题
cat dry-run.log | jq '.ok'
```

#### 重试策略

```yaml
retry:
  max: 2  # 最多重试 2 次
  when:
    - runner_system_failure        # Runner 系统故障
    - stuck_or_timeout_failure     # 卡住或超时
    - script_failure               # 脚本失败（谨慎使用）
```

**注意**：
- 默认不重试 `script_failure`，因为可能是真实的迁移问题
- 如需开启，建议配合 `ATLAS_MIGRATE_TIMEOUT` 环境变量
- 重试间隔建议 30 秒（GitLab CI 默认）

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

### 场景 5: 孤儿迁移记录修复 (Orphaned Migration Cleanup)

**问题**: `atlas_schema_revisions` 表中有记录，但对应的 `.sql` 文件已从目录中删除（或被重命名），导致 Atlas 报 "missing migration" 错误。

**典型触发场景**:
- 合并后改时间戳（违反 §0.8）
- revert 已 apply 的分支
- 手工删除迁移文件
- 多人合并时某人的迁移被冲突覆盖

**Atlas 行为**:
- 老版本 (v0.x): 可能直接 apply 新迁移，不报错
- 新版本 (v1.x+): 报 `missing migration file: <version>`

**诊断命令**:

```sql
-- 1. 查看所有已 apply 的迁移
SELECT version, description, applied_at, success 
FROM atlas_schema_revisions 
ORDER BY version DESC 
LIMIT 20;

-- 2. 找出孤儿记录（在 DB 但不在目录）
SELECT version 
FROM atlas_schema_revisions 
WHERE version NOT IN (
    '20260609082623', '20260610120000', '20260611000000', -- ... 列出目录里所有的
    '20260622180321', '20260623010000', '20260624010000'
);

-- 3. 简化版：用脚本自动检测
```

**自动检测脚本**:

```bash
#!/bin/bash
# detect-orphans.sh - 检测孤儿迁移记录

# 获取目录中所有迁移时间戳
DIR_VERSIONS=$(ls atlas-migrations/*.sql 2>/dev/null | grep -v atlas.sum | sed 's/.*\///' | sed 's/_.*//' | tr '\n' ',' | sed 's/,$//')

echo "目录中的迁移: $DIR_VERSIONS"

# 连接数据库查询
psql "$DATABASE_URL" << EOF
SELECT 
    version,
    description,
    applied_at,
    CASE WHEN version IN ($DIR_VERSIONS) THEN '✅ 正常' ELSE '❌ 孤儿' END as status
FROM atlas_schema_revisions
ORDER BY version DESC;
EOF
```

**修复方案选择**:

```bash
# 决策流程
1. 确认孤儿版本对应的 SQL 是否已真正执行？
   ↓
   ├─ 是 (表已建好、字段已加) → 路径 A: 清理孤儿记录
   ├─ 否 (SQL 从未执行)       → 路径 B: 恢复 SQL 文件
   └─ 不确定                  → 路径 C: 安全修复（IF NOT EXISTS + 清理）
```

**路径 A: 清理孤儿记录（推荐）**

```sql
-- A.1. 先确认 schema 已正确
\dt+ audit_logs
\d api_keys  -- 检查 ip_allowlist 字段是否存在

-- A.2. 清理孤儿记录（示例：版本 20260623010000）
-- 先备份
CREATE TABLE atlas_schema_revisions_backup AS 
SELECT * FROM atlas_schema_revisions;

-- 删除孤儿
DELETE FROM atlas_schema_revisions 
WHERE version = '20260623010000';

-- 验证
SELECT version FROM atlas_schema_revisions 
ORDER BY version DESC LIMIT 5;
```

```bash
# A.3. 重新生成 atlas.sum
atlas migrate hash --env local

# A.4. 验证
atlas migrate status --env dev
# 应该显示 "Migration directory is in sync with the database"
```

**路径 B: 恢复 SQL 文件**

```bash
# B.1. 从 git 历史找回文件
git log --all --full-history -- "atlas-migrations/20260623010000*"
# 找到对应的 commit hash

# B.2. 恢复文件
git checkout <commit-hash> -- atlas-migrations/20260623010000_xxx.sql

# B.3. 重新计算 hash
atlas migrate hash --env local

# B.4. 验证
atlas migrate validate --env local
```

**路径 C: 安全修复（IF NOT EXISTS + 清理）**

适用于：不确定 schema 状态，想确保修复后任何环境都能应用。

```bash
# C.1. 创建新的幂等迁移文件（合并所有变更）
cat > atlas-migrations/20260624010000_fix_orphaned_safe.sql << 'EOF'
-- 安全修复：使用 IF NOT EXISTS 确保幂等

CREATE TABLE IF NOT EXISTS "audit_logs" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" UUID NOT NULL,
    "action" VARCHAR(64) NOT NULL,
    "resource_type" VARCHAR(64) NOT NULL,
    "resource_id" VARCHAR(255) NOT NULL,
    "ip_address" INET,
    "metadata" JSONB,
    "created_at" TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE "api_keys" 
ADD COLUMN IF NOT EXISTS "ip_allowlist" TEXT NOT NULL DEFAULT '';
EOF

# C.2. 清理孤儿记录
psql "$DATABASE_URL" << EOF
DELETE FROM atlas_schema_revisions 
WHERE version = '20260623010000' AND success = false;
-- 或更保守：只删失败的
-- DELETE FROM atlas_schema_revisions WHERE version = '20260623010000';
EOF

# C.3. 重新 hash
atlas migrate hash --env local

# C.4. 验证
atlas migrate validate --env local
```

**预防措施**:
- 严格遵守 §0.8（迁移不可变原则）
- 使用 §2.5 pre-commit hook 阻止提交
- 使用 §6.5 CI automation 阻止合并

**生产环境修复流程**:

```bash
#!/bin/bash
# production-fix-orphan.sh

set -e

PROD_DB="postgresql://user:pass@prod-db:5432/llm_gateway"

echo "⚠️ 即将修复生产环境孤儿记录，请确认已备份"
read -p "继续? [y/N] " -n 1 -r
if [[ ! $REPLY =~ ^[Yy]$ ]]; then echo "已取消" && exit 1; fi

# 备份
psql "$PROD_DB" -c "CREATE TABLE atlas_schema_revisions_backup_$(date +%Y%m%d_%H%M%S) AS SELECT * FROM atlas_schema_revisions;"

# 诊断
psql "$PROD_DB" -c "SELECT version, description, applied_at FROM atlas_schema_revisions ORDER BY version DESC LIMIT 10;"

# 清理（根据实际情况修改版本号）
psql "$PROD_DB" -c "DELETE FROM atlas_schema_revisions WHERE version = '20260623010000';"

echo "✅ 孤儿记录已清理"
echo "📋 下一步：部署包含 atlas.sum 修复的 commit"
```

### 场景 6: Hotfix 跨分支同步

**问题**: 紧急 Hotfix 同时部署到 test/staging/prod，但三个环境的迁移历史不一致。

**典型场景**:
```
time ──────────────────────────────────────────────────►
main:   ──M1──M2──M3────────────────────────M4(hotfix)──►
test:   ──M1──M2──M5──M6──M7────────────M4(hotfix)──►
staging:──M1──M2──M5──M6──────────────M4(hotfix)──►
```

main 上没有 M5-M7，直接合并 hotfix 分支时会出现时间戳乱序。

**修复流程**:

```bash
# 1. 确认 hotfix 迁移的时间戳
ls atlas-migrations/*_hotfix_*.sql

# 2. 检查每个环境的最新时间戳
for env in test staging main; do
  LATEST=$(git ls-tree -r origin/$env atlas-migrations/ | awk '{print $4}' | sort | tail -1)
  echo "$env: $LATEST"
done

# 3. 如果 hotfix 时间戳晚于所有环境 → 直接合并 ✅
# 4. 如果 hotfix 时间戳早于某个环境 → 需要特殊处理

# 4.1 方案 A: 重命名 hotfix 文件（如果还没 apply 到任何环境）
git mv atlas-migrations/20260620_hotfix.sql \
       atlas-migrations/20260625_hotfix.sql
atlas migrate hash --env local
atlas migrate status --env test  # 验证 test 环境还没 apply
# 确认显示 "pending" 才能重命名

# 4.2 方案 B: 如果 hotfix 已 apply 到某些环境
# 使用 IF NOT EXISTS 确保幂等
echo "-- Hotfix: 添加紧急字段" > atlas-migrations/20260625_hotfix.sql
cat >> atlas-migrations/20260625_hotfix.sql << 'EOF'
ALTER TABLE "api_keys" 
ADD COLUMN IF NOT EXISTS "rate_limit" INTEGER DEFAULT 0;
EOF
atlas migrate hash --env local

# 对于已 apply 的环境，Atlas 会跳过（因为有记录）
# 对于未 apply 的环境，Atlas 会执行（IF NOT EXISTS 保证安全）
```

**Hotfix 提交模板**:

```bash
# hotfix commit message
git commit -m "hotfix(db): 紧急修复 [问题描述]

问题：[具体问题]
修复：[修复措施]
影响：[影响范围]

跨环境部署:
- ✅ test: 时间戳晚于最新迁移
- ⚠️ staging: 时间戳早于 M5，使用 IF NOT EXISTS
- ✅ main: 无冲突，直接 apply

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

### 场景 7: 长周期分支合并冲突

**问题**: Feature 分支开发数月，期间 main 已合并多个迁移，合并时时间戳早于 main 最新版本。

**典型时间线**:

```
time ──────────────────────────────────────────────────►
main:    ──M1──M2──M3──M4──M5──M6──M7──M8──M9──M10──►
feature: ──M1──M2──F1(feature migration, 时间戳早于 M5)──►
```

F1 的时间戳早于 M5-M10，直接合并会报 "out of order"。

**修复流程**:

```bash
# 1. 确认 feature 迁移的时间戳
ls atlas-migrations/*_[feature_name]*.sql

# 2. 确认 main 最新时间戳
LATEST=$(git ls-tree -r origin/main atlas-migrations/ | awk '{print $4}' | sort | tail -1)
echo "Main latest: $LATEST"

# 3. 检查 feature 迁移是否已 apply 到任何环境
for env in test staging main; do
  echo "=== $env ==="
  psql "$DATABASE_URL_$env" -c "
    SELECT version, applied_at, success 
    FROM atlas_schema_revisions 
    WHERE version = '<feature_version>';"
done

# 4. 根据 apply 状态选择策略

# 4.1 方案 A: 未 apply 到任何环境（最干净）
# 直接重命名
git mv atlas-migrations/20260601_feature.sql \
       atlas-migrations/20260615_feature.sql  # 晚于 M10
atlas migrate hash --env local
atlas migrate validate --env local

# 4.2 方案 B: 已 apply 到某些环境（复杂）

# 4.2.1 确认已 apply 环境的 schema 状态
psql "$DATABASE_URL_test" -c "\dt+ feature_table"
# 确认表已存在

# 4.2.2 创建新的幂等迁移（合并 feature 的所有变更）
cat > atlas-migrations/20260615_feature_merged.sql << 'EOF'
-- 合并 feature 分支所有迁移，使用 IF NOT EXISTS 确保幂等

-- 来自 20260601_feature_part1.sql
CREATE TABLE IF NOT EXISTS "feature_configs" (
    "id" SERIAL PRIMARY KEY,
    "name" VARCHAR(100) NOT NULL,
    "value" JSONB NOT NULL
);

-- 来自 20260605_feature_part2.sql  
ALTER TABLE "users" 
ADD COLUMN IF NOT EXISTS "feature_flags" JSONB DEFAULT '{}';

-- 来自 20260610_feature_part3.sql
CREATE INDEX IF NOT EXISTS "idx_feature_configs_name" 
ON "feature_configs" ("name");
EOF

# 4.2.3 清理已 apply 环境的旧记录
psql "$DATABASE_URL_test" << EOF
DELETE FROM atlas_schema_revisions 
WHERE version IN ('20260601_feature_part1', '20260605_feature_part2', '20260610_feature_part3');
EOF

# 4.2.4 删除旧文件
rm atlas-migrations/20260601_feature_part1.sql
rm atlas-migrations/20260605_feature_part2.sql
rm atlas-migrations/20260610_feature_part3.sql

# 4.2.5 重新 hash
atlas migrate hash --env local
atlas migrate validate --env local
```

**合并策略对比**:

| 策略 | 适用条件 | Git 复杂度 | DB 复杂度 | 风险 |
|------|----------|-----------|----------|------|
| 重命名 | 未 apply | 低 | 低 | 低 |
| 合并 + IF NOT EXISTS | 已 apply | 中 | 中 | 中 |
| 保留 + 清理孤儿 | 已 apply | 高 | 高 | 高 |

**预防措施**:
- 定期 rebase feature 分支到最新 main（每周至少一次）
- 生成迁移前先查 main 最新时间戳（§0.6）
- 使用 §6.5 CI automation 在 MR 时自动检测

### 场景 8: 生产环境紧急回滚

**问题**: 生产迁移后发现问题，需要快速回滚，但 Atlas 不支持自动 down migration。

**紧急回滚流程**:

```bash
#!/bin/bash
# emergency-rollback.sh - 生产环境紧急回滚

set -e

PROD_DB="postgresql://user:pass@prod-db:5432/llm_gateway"
VERSION_TO_ROLLBACK="20260615010000"  # 要回滚的迁移版本

echo "⚠️ 即将回滚生产环境迁移: $VERSION_TO_ROLLBACK"
echo "确认信息:"
psql "$PROD_DB" -c "SELECT version, description, applied_at FROM atlas_schema_revisions WHERE version = '$VERSION_TO_ROLLBACK';"

read -p "继续回滚? [y/N] " -n 1 -r
if [[ ! $REPLY =~ ^[Yy]$ ]]; then echo "已取消" && exit 1; fi

# 1. 应用回滚（根据具体情况调整 SQL）
cat << 'EOF' | psql "$PROD_DB"
-- 回滚 20260615010000_add_audit_logs

-- 1. 删除新建的表
DROP TABLE IF EXISTS "audit_logs" CASCADE;

-- 2. 删除新建的字段
ALTER TABLE "api_keys" 
DROP COLUMN IF EXISTS "ip_allowlist";

-- 3. 清理迁移记录
DELETE FROM atlas_schema_revisions 
WHERE version = '20260615010000';
EOF

echo "✅ 回滚完成"
echo "📋 下一步：删除迁移文件并提交"
```

```bash
# 删除迁移文件
rm atlas-migrations/20260615010000_add_audit_logs.sql

# 重新 hash
atlas migrate hash --env local

# 提交
git add -A
git commit -m "revert(db): 回滚 20260615010000 迁移

原因: [具体问题]
影响: [影响范围]
修复: 已清理相关表和字段"

git push origin main
```

**回滚策略决策树**:

```bash
# 决策流程
1. 迁移是否已 apply 到生产？
   ↓
   ├─ 否 → 直接删除文件
   └─ 是 → 继续问

2. 迁移创建了新表？
   ↓
   ├─ 是 → DROP TABLE IF EXISTS
   └─ 否 → 继续问

3. 迁移添加了新字段？
   ↓
   ├─ 是 → ALTER TABLE DROP COLUMN IF EXISTS
   └─ 否 → 继续问

4. 迁移修改了现有 schema？
   ↓
   ├─ 是 → 需要手动逆向（例如加回删除的字段）
   └─ 否 → 仅清理迁移记录

5. 是否需要保留数据？
   ↓
   ├─ 否 → 直接 DROP
   └─ 是 → 先备份再 DROP
```

**回滚前检查清单**:

- [ ] 确认问题无法通过修复迁移解决（必须回滚）
- [ ] 备份相关表和字段
- [ ] 确认回滚 SQL 的幂等性（IF EXISTS）
- [ ] 测试回滚 SQL 在 staging 环境
- [ ] 通知团队回滚计划
- [ ] 准备回滚后的代码修复方案

### 场景 9: 跨环境不一致性修复

**问题**: 不同环境的 schema 状态不一致（test 有表但 staging 没有，或字段类型不同）。

**典型症状**:

```bash
# test 环境
\dt+ audit_logs  # 存在
# staging 环境
\dt+ audit_logs  # 不存在

# 或字段类型不同
# test:  ip_allowlist TEXT
# staging: ip_allowlist VARCHAR(255)
```

**诊断流程**:

```bash
#!/bin/bash
# cross-env-check.sh - 跨环境一致性检查

ENVS=("test" "staging" "main")
TABLES=("audit_logs" "api_keys")

for table in "${TABLES[@]}"; do
  echo "=== 检查表: $table ==="
  for env in "${ENVS[@]}"; do
    DB_URL=$(eval echo "\$DATABASE_URL_${env^^}")
    echo "--- $env ---"
    psql "$DB_URL" -c "\d $table" 2>&1 | head -20
    echo ""
  done
done
```

**修复方案**:

**方案 A: 统一到最新状态（推荐）**

```sql
-- 假设 test 是最新状态，staging 缺少 audit_logs 表

-- 1. 在 staging 创建缺失的表
CREATE TABLE IF NOT EXISTS "audit_logs" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ... 其他字段
);

-- 2. 清理或更新迁移记录
-- 如果迁移已记录但表不存在（孤儿）
DELETE FROM atlas_schema_revisions 
WHERE version = '20260615010000';

-- 如果迁移未记录但表已存在（不一致）
-- 不执行任何操作，让 Atlas 在下次 apply 时自动跳过（IF NOT EXISTS）
```

**方案 B: 创建修复迁移**

```sql
-- 20260620010000_fix_cross_env_inconsistency.sql
-- 跨环境一致性修复（幂等）

-- 1. 确保所有环境都有 audit_logs 表
CREATE TABLE IF NOT EXISTS "audit_logs" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" UUID NOT NULL,
    "action" VARCHAR(64) NOT NULL,
    "resource_type" VARCHAR(64) NOT NULL,
    "resource_id" VARCHAR(255) NOT NULL,
    "ip_address" INET,
    "metadata" JSONB,
    "created_at" TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 2. 统一 ip_allowlist 字段类型为 TEXT
-- 注意：ALTER COLUMN TYPE 可能需要重建索引
DO $$ 
BEGIN
    -- 检查字段是否存在且类型不是 TEXT
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'api_keys' 
          AND column_name = 'ip_allowlist'
          AND data_type != 'text'
    ) THEN
        -- 先删除相关索引（如果有）
        EXECUTE 'DROP INDEX IF EXISTS idx_api_keys_ip_allowlist';
        
        -- 修改类型
        EXECUTE 'ALTER TABLE "api_keys" ALTER COLUMN "ip_allowlist" TYPE TEXT';
        
        -- 重建索引（如果需要）
        -- EXECUTE 'CREATE INDEX idx_api_keys_ip_allowlist ON "api_keys" USING GIN (ip_allowlist)';
    END IF;
END $$;

-- 3. 确保所有环境都有必要的索引
CREATE INDEX IF NOT EXISTS "idx_audit_logs_created_at" 
ON "audit_logs" ("created_at");
```

```bash
# 重新 hash
atlas migrate hash --env local

# 验证所有环境
for env in test staging main; do
  echo "=== $env ==="
  atlas migrate status --env $env
done
```

**方案 C: 强制同步（危险）**

```bash
#!/bin/bash
# force-sync.sh - 强制同步所有环境到代码状态
# ⚠️ 危险：可能删除生产数据

set -e

# 1. 备份所有环境
for env in test staging main; do
  DB_URL=$(eval echo "\$DATABASE_URL_${env^^}")
  echo "备份 $env..."
  pg_dump "$DB_URL" > "backup_${env}_$(date +%Y%m%d_%H%M%S).sql"
done

# 2. 清理所有迁移记录（危险！）
for env in test staging main; do
  DB_URL=$(eval echo "\$DATABASE_URL_${env^^}")
  echo "清理 $env 迁移记录..."
  psql "$DB_URL" -c "TRUNCATE TABLE atlas_schema_revisions;"
done

# 3. 应用所有迁移
for env in test staging main; do
  echo "应用迁移到 $env..."
  atlas migrate apply --env $env
done

echo "✅ 所有环境已同步"
echo "⚠️ 请检查备份文件确保数据安全"
```

**预防措施**:
- 严格按环境顺序部署（test → staging → main）
- 使用 §6.5 CI automation 确保每次部署都跑迁移
- 定期运行 `atlas migrate status --env <env>` 检查状态
- 使用 §4.5 跨环境同步检查脚本

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
