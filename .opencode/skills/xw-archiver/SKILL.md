---
name: xw-archiver
description: 玄武领域归档前生成归档材料，将原始需求与真实实现后的技术方案沉淀到本次 change，并同步可复用需求基线到 xwRef requirements。

disable-model-invocation: true
---

# xw-archiver

玄武领域归档技能。用于代码生成完成后、`/xf:archive` finalize 前，将"原始需求"和"真实实现技术方案"整理为稳定可检索的归档文档。

## 1. 定位与边界

**职责**：

- 汇总本次活动的原始需求、技术设计、任务冻结与实现结果
- 生成本地归档材料，落盘到 `xspec/changes/<module>/archive/`
- 生成需求归档基线，落盘到 `xwRef/需求归档/<archive_dir>/<topic>/<version>/`
- 让 `/xf:archive` finalize 后自动得到 `xspec/archive/<change-id>/<module>/archive/` 下的最终归档文件
- 更新 `xwRef/需求归档/index.yaml`（全局总索引），按主题 + 版本组织

**不职责**：

- 不修改业务代码
- 不重新设计技术方案
- 不替代 `xf-archive.mjs --step finalize` 的搬运与清空操作
- 不把历史活动沉淀到 `xspec/modules/`

**核心约定**：

- 输出文档的结构和生成规则由 `xwRef/xuanwu/<profile>/archive/bundle.yaml` 驱动，Skill 不硬编码内容规格
- bundle 的通用解析与调度规则统一委托给 `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`

## 2. 输入与输出

### 输入

| 来源 | 路径 |
| ---- | ---- |
| 变更元数据 | `xspec/changes/change.yaml` |
| Archive Bundle | `xwRef/xuanwu/<profile>/archive/bundle.yaml` |
| 原始需求源 | `change.yaml.source_doc` 指向的本地 Markdown / Word 文件或飞书文档 URL |
| 原始 / 结构化需求 | `xspec/changes/<module>/spec.md`、`spec_*.md`、`需求提取总结报告.md` |
| 技术概要 | `xspec/changes/<module>/hld.md`、`hld_client.md`、`hld_server.md`、`技术架构报告.md` |
| 详细设计 | `xspec/changes/<module>/design_client_*.md`、`design_server_*.md`、`design-report.md` |
| 实现冻结 / 任务 | `xspec/changes/<module>/task-handoff.json`、`task_*.md`、`task-report.md` |
| 实现结果 | `xspec/changes/<module>/impl-report.md`、`runtime/*.md`、实际改动代码清单 |

### 输出

| 产物 | 路径 | 生成方式 |
| ---- | ---- | ---- |
| 原始需求快照 | `archive/source-requirement.md` | AI 生成 |
| 技术总览（README 级） | `archive/tech-overview.md` | AI 生成 |
| 架构设计（全 Mermaid 图） | `archive/architecture.md` | AI 生成 |
| 公共契约（hld.md） | `archive/hld.md` | 直接复制 |
| 客户端 HLD | `archive/hld_client.md` | 直接复制 |
| 服务端 HLD | `archive/hld_server.md` | 直接复制 |
| 客户端详细设计原文 | `archive/design_client_*.md` | 直接复制（保留文件名） |
| 服务端详细设计原文 | `archive/design_server_*.md` | 直接复制（保留文件名） |
| xwRef 归档目录 | `xwRef/需求归档/<archive_dir>/<topic>/<version>/` | — |
| xwRef 最新快照 | `xwRef/需求归档/<archive_dir>/<topic>/latest/` | — |
| xwRef 基线元数据 | `xwRef/需求归档/<archive_dir>/<topic>/<version>/meta.yaml` | — |
| xwRef 全局总索引 | `xwRef/需求归档/index.yaml` | — |

**profile → archive_dir 映射**：

| profile | archive_dir |
| :--- | :--- |
| `xuanwu-activity` | `活动` |

**目录命名规则**：

| 维度 | 规则 | 示例 |
| :--- | :--- | :--- |
| `<topic>` | 从 `change.yaml.name` 提取业务语义名，去掉时间 / 版本修饰 | `首充活动` |
| `<version>` | 扫描已有版本自动递增 `vN`；无历史则为 `v1` | `v1` → `v2` |
| `latest/` | 每次归档复制 `vN/` 到 `latest/`，覆盖旧内容 | 始终指向最新 |

## 3. 执行口径

```text
xw-archiver --profile <profile> --module <module>
```

缺省值：

- `--profile` 缺省读取 `node .xflow/scripts/xf-profile.mjs --value-only`，或从 `xspec/changes/change.yaml.project_profile` 兜底
- `--module` 缺省从 `xspec/changes/change.yaml` 的 `affected_modules[0].name` 获取

## 4. 执行流程

不允许跳步。

| Step | 动作 | 要求 |
| ---- | ---- | ---- |
| 1 | 预检 | 确认 profile 已解析；确认 `change.yaml` 存在；确认目标 module 存在于 `affected_modules`；确认 `change.yaml.source_doc`，若缺失则继续但在 `source-requirement.md` 标注"原始需求来源缺失" |
| 2 | 确认实现状态 | 优先检查 `impl-report.md`；若不存在，允许继续但所有 AI 生成文档必须标记"实现报告缺失，需补跑 /xf:implement 后刷新" |
| 3 | 加载 bundle | 读取 `xwRef/xuanwu/<profile>/archive/bundle.yaml`；按 `bundle-orchestrator` 解析 `docs` 列表（AI 生成）和 `file_copies` 列表（直接复制），解析模板路径、规则路径、依赖顺序 |
| 4 | 生成 AI 归档文档 | 按 bundle `execution_plan.docs` 顺序，逐文档读取对应 `template` 和 `rules`，基于设计输入填充内容落盘；`architecture` 依赖 `tech-overview` 须等前置完成后再生成 |
| 5 | 直接复制文件（含原始需求） | 按 bundle `file_copies` 依次处理：① `source-requirement`：先写 metadata header（从 `change.yaml` 填 7 个字段，不读原文），再将 `source_doc` 原文直接追加到文件尾（shell 操作，不经 AI 上下文）；本地 Markdown 直接追加，Word 先转 Markdown 再追加，飞书 URL 先拉取再追加，无法读取时写明失败原因、不得伪造原文；② 其余 copy：glob 展开后逐个复制，保留原文件名；`optional: true` 的条目若源文件不存在则跳过并记录 |
| 6 | 生成本地归档目录 | 创建 `xspec/changes/<module>/archive/`，确认 AI 生成文档 + 全部复制文档落盘 |
| 7 | 同步 xwRef 需求归档 | 提取 topic → 扫描 `<topic>/versions.yaml` 确定版本号（无则为 v1）→ 复制全部文件到 `<topic>/<version>/` → 生成 `meta.yaml` → 复制到 `latest/`（先删再复制）→ 追加版本条目到 `<topic>/versions.yaml` → 更新 `index.yaml`（仅刷新 latest 路径，不写版本明细） |
| 8 | 自检 | 按 bundle `validation.checklists` 执行自检；确认所有文件存在；确认 AI 生成文档无 `{...}`、`TBD`、`待补充` 占位符；确认 `source-requirement.md` 头部元数据 7 个字段已填、正文非空；确认无 `xspec/changes/` 临时路径；确认 `architecture.md` mermaid 块语法合法 |

## 5. meta.yaml / versions.yaml / index.yaml 规范

### meta.yaml（每个版本目录下一份）

```yaml
topic: "<topic>"
version: "<version>"
change_id: "<change.yaml.id>"
name: "<change.yaml.name>"
profile: "<profile>"
source_doc: "<change.yaml.source_doc>"
created_at: "<YYYY-MM-DD>"
documents:
  # key 为 bundle doc_id 或 copy_id，value 为 xwRef 归档路径
  <doc_id>: "xwRef/需求归档/<archive_dir>/<topic>/<version>/<output>"
```

### versions.yaml（每个 topic 目录下一份，追加更新）

```yaml
topic: "<topic>"
archive_dir: "<archive_dir>"
versions:
  v<N>:
    change_id: "<change.yaml.id>"
    name: "<change.yaml.name>"
    created_at: "<YYYY-MM-DD>"
    path: "xwRef/需求归档/<archive_dir>/<topic>/v<N>"
```

### index.yaml（全局唯一，只记录 topic → latest 路径）

```yaml
# 只列 topic 名 + latest 路径，不含版本明细（版本明细见各 topic/versions.yaml）
archives:
  <topic>:
    archive_dir: "<archive_dir>"
    latest: "xwRef/需求归档/<archive_dir>/<topic>/latest"
```

## 6. 阶段完成条件

- AI 生成文档（tech-overview / architecture）已落盘到 `archive/`
- 直接复制文档（source-requirement.md / hld.md / hld_client.md / hld_server.md / design_client_*.md / design_server_*.md）已落盘到 `archive/`；`optional: true` 的条目若源文件不存在则记录跳过原因；`source-requirement.md` 正文非空或已写明失败原因
- `xwRef/需求归档/<archive_dir>/<topic>/<version>/` 各文档、`meta.yaml` 已生成
- `xwRef/需求归档/<archive_dir>/<topic>/latest/` 已刷新
- `xwRef/需求归档/<archive_dir>/<topic>/versions.yaml` 已追加新版本条目
- `xwRef/需求归档/index.yaml` 已刷新 latest 路径（不写版本明细）
- AI 生成文档不含占位符，不含 `xspec/changes/` 临时路径
- 自检清单全部通过
- 若实现报告缺失，已显式标注刷新要求

## 7. 返回结果

1. 输出目录路径
2. bundle 声明的全部输出文件路径列表
3. 是否读取到 `impl-report.md`
4. 设计依据文件清单（被采用的 hld、design、impl-report）
5. xwRef 版本目录路径与最新快照路径
6. topic 名和版本号
7. finalize 后的目标归档路径

## 8. 资源索引

- Archive Bundle：`xwRef/xuanwu/<profile>/archive/bundle.yaml`
- 通用 bundle 调度：`.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`
