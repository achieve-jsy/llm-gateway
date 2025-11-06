---
name: arch-designer
description: Generate technical architecture overview documents from profile-specific tech templates and bundle.yaml. Use when designing technical architecture or when the user asks for 活动总设计方案, Lua技术方案, 客户端架构总览, or 服务端架构总览.

disable-model-invocation: true
---

# 通用技术架构生成器（arch-designer）

为技术架构阶段生成框架总览与双端技术概要方案（3 个核心文档）。

## 1. 定位与边界

**职责**：

- 将结构化需求转换为高层技术架构文档
- 生成 `hld.md` 主索引与双端概要文档
- 编排 bundle 解析、子 Agent 生成、自检与汇总报告
- 产出框架架构：生成 `hld.md` 主索引——AwardLua 框架技术骨架、开发模式选择、公共接口规范
- 产出服务端概要：`hld_server.md`——脚本落点、命令职责、状态机、配置契约、Redis 约束
- 产出客户端概要：`hld_client.md`——模块划分、文件落点、事件表、宿主挂载点

**不负责**：

- 不做详细设计，不做实现冻结
- 不在 Skill 内硬编码领域模板或框架规则
- 不扩展到 FGUI 设计、核心模块设计、Redis 详细设计等 tech-design 阶段内容

**核心约定**：

- `hld.md` 是阶段主产物，也是索引文档
- `hld_server.md` 与 `hld_client.md` 是下游详细设计的正式主输入
- tech 阶段只产出这 3 个核心文档 + 1 个技术架构报告，不做详细设计，不做实现冻结
- 具体领域文档由 `xwRef/xuanwu/<profile>/tech/bundle.yaml` 声明
- 主 Skill 负责编排、校验、汇总，不在 Skill 内硬编码领域模板
- bundle 允许添加 YAML 注释，解析器会自动忽略
- bundle 的通用解析与调度规则统一委托给 `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`

## 2. 适用场景

- 用户要求生成技术架构、活动总设计方案、Lua 技术方案、客户端架构总览、服务端架构总览
- 已存在 `xspec/changes/<module>/spec_*.md` 结构化需求，希望产出 tech 阶段文档
- **不适用**：需要 FGUI 设计、核心模块设计、Redis 详细设计等——那属于 design-detailer（tech-design 阶段）

## 3. 输入与输出

### 输入

| 来源              | 路径                                                               |
| ----------------- | ------------------------------------------------------------------ |
| 结构化需求        | `xspec/changes/<module>/spec_*.md`                                 |
| Tech Bundle       | `xwRef/xuanwu/<profile>/tech/bundle.yaml`               |
| bundle 调度规则   | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`               |
| 额外规则/检查清单 | 由 `bundle.yaml` 中的 `rules`、`shared_rules`、`validation.*` 声明 |

### 输出

| 产物           | 路径                                                              |
| -------------- | ----------------------------------------------------------------- |
| 主索引文档     | `xspec/changes/<module>/hld.md`                                   |
| 服务端概要     | `xspec/changes/<module>/hld_server_<id>.md`（如 `hld_server.md`） |
| 客户端概要     | `xspec/changes/<module>/hld_client_<id>.md`（如 `hld_client.md`） |
| 服务端检查报告 | `xspec/changes/<module>/hld-check-server-report.md`               |
| 客户端检查报告 | `xspec/changes/<module>/hld-check-client-report.md`               |
| 技术架构报告   | `xspec/changes/<module>/技术架构报告.md`                          |

### 输出命名说明

**独立文件模式**（唯一模式）：

- 输入：`xspec/changes/<module>/spec_*.md`（req-extractor 输出）
- 输出目录：`xspec/changes/<module>/`
- 输出命名：
  - 主文档：`hld.md`
  - 服务端文档：`hld_server_<id>.md`（如 `hld_server.md`）
  - 客户端文档：`hld_client_<id>.md`（如 `hld_client.md`）

## 4. 执行口径

```
arch-designer --profile <profile> --module <module>
```

可选参数：

- `--docs <doc_id,...>`：指定子集生成
- `--profile <...>`：显式覆盖 profile

参数组合、依赖补齐与选择规则统一遵循 `bundle-orchestrator`。

### 返回结果必须包含

1. 输出目录路径
2. `hld.md` 路径
3. `技术架构报告.md` 路径
4. 本次实际生成的文档列表
5. 因依赖自动补齐的文档列表
6. 未明确项数量
7. `hld-check-server-report.md` 路径
8. `hld-check-client-report.md` 路径
9. 执行方式说明（子 Agent 并行执行 / 主 Agent 串行退化）

## 5. 角色职责

| 维度     | 主 agent                                                                 | 文档生成子 Agent                                             | 自检子 Agent                                                  |
| -------- | ------------------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------- |
| **触发** | Skill 入口                                                               | 主 agent 按 bundle 派发                                      | 主 agent 在文档生成后并行派发                                 |
| **输入** | `spec_*.md`、`bundle.yaml`、bundle-orchestrator                          | 分配到的 `input_files`、依赖文档、模板、规则文件             | 对应端文档、checklist、validation rules                       |
| **职责** | 解析参数、确认 profile、解析 bundle、准备 `hld.md`、调度 phase、汇总报告 | 按模板与规则生成目标文档，补齐可推断内容，保留关键未知项标记 | 检查并修复文档，输出检查报告，配合主 agent 做跨文档一致性校验 |
| **输出** | `hld.md`、`技术架构报告.md`、最终返回结果                                | `hld_server*.md`、`hld_client*.md` 或 bundle 定义的其他文档  | `hld-check-server-report.md`、`hld-check-client-report.md`    |
| **禁止** | 不硬编码领域模板，不绕过 bundle-orchestrator，不静默忽略遗留问题         | 不读取自己职责外模板和输入，不偏离模板标题层级               | 不只检查不修复；仍不符合时必须标记遗留                        |

## 6. 执行流程

不允许跳步。bundle 解析、phase 选择、依赖补齐与失败回退统一遵循 `bundle-orchestrator`。

**主 agent 读取与派发约束**：

- 主 agent 负责读取 `bundle.yaml`、`bundle-orchestrator` 以及 bundle 声明的规则入口，不在 Skill 中硬编码领域模板或框架规则
- 主 agent 负责编排与汇总，不替子 Agent 展开模板内容，也不替子 Agent 消化其专属 `input_files`
- 文档正文生成由子 Agent 完成；主 agent 只负责准备 `hld.md` 索引、组织 phase、派发检查与汇总报告
- 框架规范在生成前必须加载，在生成后必须校验；若校验失败，必须重写受影响文档
- 遗留问题必须显式记录并告知用户，不允许静默忽略

| Step | 动作                   | 关键要求                                                                                                                                                                                                                                                                                                                                                                                                                    | 规则来源                                                                                                       |
| ---- | ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| 1    | 确认输入信息           | 确认模块名（`--module`），确定 `xspec/changes/<module>/`；确认 profile：用户指定 `--profile` 直接使用，否则自动从需求文档关键词匹配并向用户确认；可选 `--docs <doc_id,...>` 指定子集生成                                                                                                                                                                                                                                    | `bundle-orchestrator`                                                                                          |
| 2    | 解析输入输出路径       | 校验 `xspec/changes/<module>/spec_*.md` 存在；输出目录不存在则自动创建；确定 bundle 路径：`xwRef/xuanwu/<profile>/tech/bundle.yaml`                                                                                                                                                                                                                                                                              | `bundle.yaml`                                                                                                  |
| 3    | 加载 profile 技术配置  | 读取 `bundle.yaml`；按 `bundle-orchestrator` 规则解析 bundle、选择文档、补齐依赖、计算 phase 顺序；若 bundle 声明了框架规则（`rules`/`shared_rules`），识别真实代码仓库锚点；校验 `main_artifact` 存在、`doc_id` 唯一、模板文件存在、`output` 不重复                                                                                                                                                                        | `bundle.yaml`、`.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`                                            |
| 4    | 加载框架规范           | Skill 不得硬编码框架规则，只读取并应用 bundle 声明的规则文件；生成文档前**必须**加载框架规范，生成后**必须**校验是否满足约束，失败则重新生成；子任务 `prompt` 中必须显式传递框架规范读取指令                                                                                                                                                                                                                                | `bundle.yaml` 中的 `shared_rules` / `rules`                                                                    |
| 5    | 准备主索引文档         | 生成 `hld.md` 初稿，至少包含：需求来源（`spec_*.md` 文件列表）、profile 与 bundle 摘要、本次将生成的文档列表、统一架构目标、模块边界、风险与待确认项、附属文档链接区                                                                                                                                                                                                                                                        | `bundle.yaml`、`spec_*.md`                                                                                     |
| 6    | 按 bundle 生成附属文档 | 对每个选中 `docs[]`，读取 `docs[].execution.agent_role`（默认 `architect`）；使用 `task` 工具（`subagent_type: general`）启动子 Agent；子 Agent `prompt` 必须包含角色声明、输入清单、模板路径、规则文件、输出绝对路径、框架规范约束、执行要求；按 `execution_plan` phase 顺序执行（`framework` → `overviews`），phase 内按 mode 决定串行/并行，`depends_on` 未成功则阻塞；每完成一个文档输出 `✅ <文件名> 已完成 (n/total)` | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`、`bundle.yaml`、bundle 声明的 `shared_rules` / `rules_md` |
| 7    | 规范自检               | 并行启动服务端与客户端自检子 Agent：读取 bundle 的 `validation.checklists`，检查并修复对应文档，输出检查报告；每项按“检查 → 修复 → 重检 → 遗留”执行；结果标注 `✅通过` / `🔧修复后通过` / `❌遗留问题`；**阻塞条件**：两份报告都完成（含遗留标注）后方可进入下一步                                                                                                                                                          | `bundle.yaml` 中的 `validation.checklists`                                                                     |
| 8    | 跨文档校验与汇总       | 两个自检 Agent 返回后，主 agent 读取 `validation.rules` 检查跨文档字段一致性，不一致则重写受影响文档；生成 `技术架构报告.md`（使用 `assets/arch-report-template.md`）；遗留问题须告知用户                                                                                                                                                                                                                                   | `bundle.yaml` 中的 `validation.rules`、`assets/arch-report-template.md`                                        |
| 9    | 完成                   | 输出结果文件列表，并返回必需结果字段：输出目录路径、`hld.md` 路径、`技术架构报告.md` 路径、本次实际生成的文档列表、因依赖自动补齐的文档列表、未明确项数量、`hld-check-server-report.md` 路径、`hld-check-client-report.md` 路径、执行方式说明                                                                                                                                                                               | 返回结果约束                                                                                                   |

### Step 6 子 Agent Prompt 必含项

- 角色声明
- 输入文件清单（`input_files` + 依赖文档）
- 模板路径（`template_path`）
- 规则文件（`shared_rules` / `rules_md`，优先遵守规则再遵守模板）
- 输出路径（`xspec/changes/<module>/<output>` 的绝对路径）
- 框架规范约束（若 bundle 声明了框架规则）
- 执行要求：只读取自己的模板和输入、严格沿模板标题层级、可推断内容直接推断补全，仅关键未知项（影响跨端对齐、接口契约、`TYPE_ID` 等）才写 `?（文档未说明）`、生成完整 Markdown
- 校验标准：无占位符、关键字段一致

## 7. 文档校验要求

生成 `xspec/changes/<module>/技术架构报告.md` 时，至少校验：

1. `hld.md` 和 `技术架构报告.md` 存在，所有选中文档已生成
2. 无残留模板占位符（`{活动名}`、`{...}`）
3. 跨文档关键字段一致：活动/协议/模块命名、配置字段、接口与状态字段
4. `TYPE_ID`、`activity_script`、`script_path` 在 `hld.md`、`hld_server.md` 中完全一致
5. “统一返回数据结构”根字段集合在 `hld.md`、`hld_client.md`、`hld_server.md` 中完全一致

## 8. 阶段完成条件

以下全部满足才视为 tech 阶段完成：

- `hld.md`、`hld_server.md`、`hld_client.md` 已生成
- `技术架构报告.md` 已生成
- `hld-check-server-report.md` 与 `hld-check-client-report.md` 已生成
- 所有选中的 bundle 文档已完成，依赖补齐文档已纳入结果
- 无残留模板占位符
- 跨文档关键字段已完成一致性校验
- 遗留问题已明确告知用户，不允许静默忽略

## 9. 触发与选择规则

**自动整套生成**：识别 profile，生成 bundle 中 `enabled_by_default: true` 的全部文档（`framework-arch`、`server-overview`、`client-overview`）。触发词：技术架构、架构设计、生成活动总设计方案、generate architecture

**指定子集生成**：`--docs server-overview` 等。默认整套/指定子集、匹配、依赖补齐与 phase 调度统一遵循 `bundle-orchestrator`。语义不明确时反问用户。

## 10. bundle 与子任务规范

### bundle 读取规则

- bundle 文件：`xwRef/xuanwu/<profile>/tech/bundle.yaml`，路径相对 bundle 所在目录解析
- `docs[].role`：`main`（汇总进 `hld.md`）/ `supplement`（通过索引链接暴露）
- 详细字段规范：`references/arch-workflow.md`

### Agent 角色

`docs[].execution.agent_role` 支持：

- `extractor`（`.claude/agents/xuanwu_extractor.md`）：提取结构化信息填充模板
- `architect`（`.claude/agents/xuanwu_architect.md`）：设计技术架构、模块划分
- `reviewer`（`.claude/agents/xuanwu_reviewer.md`）：校验文档一致性、完整性
- `developer`（`.claude/agents/xuanwu_developer.md`）：开发实现

### 子任务 Prompt 结构

启动子任务时 `prompt` 必须包含：角色声明、输入文件+依赖文档、模板路径、规则文件（若有）、输出绝对路径、执行要求（只读自己、沿用模板、标注默认值、生成完整 Markdown）、校验标准（无占位符、关键字段一致）

## 11. 重要约束

- 主产物始终是 `hld.md`，tech 阶段仅输出 3 个核心文档（`hld.md`、`hld_server.md`、`hld_client.md`）+ 1 个汇总报告（`技术架构报告.md`）
- 不在 tech 阶段产出 FGUI、核心模块、Redis 详细设计、实现冻结等——这些属于 design-detailer（tech-design 阶段）
- 输入来源固定为 `xspec/changes/<module>/spec_*.md`，不读取其他目录
- bundle 与模板不一致先修复 bundle，无技术模板先补骨架

## 12. 资源索引

| 文件                                                 | 用途                                   |
| ---------------------------------------------------- | -------------------------------------- |
| `references/arch-workflow.md`                        | bundle 协议与字段规范                  |
| `assets/arch-report-template.md`                     | 技术架构报告模板                       |
| `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md` | 通用 bundle 调度、phase 与依赖补齐规则 |
| `.claude/agents/xuanwu_extractor.md`                 | extractor 子 Agent 角色定义            |
| `.claude/agents/xuanwu_architect.md`                 | architect 子 Agent 角色定义            |
| `.claude/agents/xuanwu_reviewer.md`                  | reviewer 子 Agent 角色定义             |
| `.claude/agents/xuanwu_developer.md`                 | developer 子 Agent 角色定义            |
