---
name: design-detailer
description: Generate detailed technical design documents (FGUI, core module, entry/popup, UI analytics, payment, Redis, interface protocol, config contract) from hld overviews and spec inputs. Use when the user asks for 详细设计, FGUI设计, 核心模块设计, 接口协议, Redis设计, 配置契约, or detailed technical specification.

disable-model-invocation: true
---

# 详细技术设计生成器（design-detailer）

为 tech-design 阶段生成双端详细技术设计文档集合。

## 1. 定位与边界

**职责**：

- 将 tech 阶段的概要方案深入到可实现的技术细节
- 生成双端详细技术设计文档集合与设计阶段报告
- 编排 bundle 解析、子 Agent 生成、自检与汇总
- 产出客户端详细设计：FGUI 节点树、核心模块（常量/DTO/数据存储）、入口与弹窗、UI 平台与埋点、支付流程
- 产出服务端详细设计：Redis Key 设计、接口协议（cmdid/请求响应结构）、配置契约（字段来源/消费位置）
- 通过 bundle 引用 profile 的框架规范规则，约束双端详细设计输出

**不职责**：

- 不做实现冻结，不做任务拆分
- 不在 Skill 内硬编码具体模板或框架规则
- 不补发需求，也不直接编写业务代码

**核心约定**：

- `design_client_*.md` 与 `design_server_*.md` 是下游实现冻结（task 阶段）的正式主输入
- tech-design 阶段**只负责详细技术设计**，不做实现冻结，不做任务拆分
- 具体领域文档由 `xwRef/xuanwu/<profile>/tech-design/bundle.yaml` 声明
- 主 Skill 负责编排、校验、汇总，不在 Skill 内硬编码具体模板
- bundle 的通用解析与调度规则统一委托给 `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`
- `hld_*` 概要文档是 tech-design 的正式主输入，design-detailer 只做深化设计，不能补发需求，也不能直接编写业务代码

## 2. 适用场景

- 用户要求生成详细技术设计文档
- 已存在 `xspec/changes/<module>/hld_*.md` 概要文档，希望产出 tech-design 阶段文档
- 适用范围包括：FGUI 节点树、核心模块（常量/DTO/数据存储）、入口与弹窗、UI 平台与埋点、支付流程、Redis Key 设计、接口协议（cmdid/请求响应结构）、配置契约（字段来源/消费位置）
- 用户只想生成设计索引、FGUI 设计、核心模块、入口弹窗、UI 埋点等客户端子集
- **自动启用**：`client-payment-design` 配置了 `auto_enable`，当存在有效 `spec_payment.md` 时自动启用
- **不适用（默认不生成）**：服务端 3 文档（Redis/接口协议/配置契约）默认 `enabled_by_default: false`，需用户显式指定触发
- **不适用**：实现冻结、任务拆分——那属于 task-worker（task 阶段）

## 3. 输入与输出

### 输入

| 来源               | 路径                                                                  |
| ------------------ | --------------------------------------------------------------------- |
| HLD 概要文档       | `xspec/changes/<module>/hld*.md`                                      |
| 结构化需求         | `xspec/changes/<module>/spec_*.md`                                    |
| Tech-Design Bundle | `xwRef/xuanwu/<profile>/tech-design/bundle.yaml`           |
| bundle 调度规则    | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`                  |
| 额外规则/检查清单  | 由 `bundle.yaml` 中的 `shared_rules`、`rules_md`、`validation.*` 声明 |

### 输出

| 产物           | 路径                                                   |
| -------------- | ------------------------------------------------------ |
| 客户端详细设计 | `xspec/changes/<module>/design_client_*.md`            |
| 服务端详细设计 | `xspec/changes/<module>/design_server_*.md`            |
| 服务端检查报告 | `xspec/changes/<module>/design-check-server-report.md` |
| 客户端检查报告 | `xspec/changes/<module>/design-check-client-report.md` |
| 详细设计报告   | `xspec/changes/<module>/design-report.md`              |

### 输出命名说明

**独立文件模式**（唯一模式）：

- 输入：`xspec/changes/<module>/hld*.md`（arch-designer 输出）及 `xspec/changes/<module>/spec_*.md`
- 输出目录：`xspec/changes/<module>/`
- 输出命名：全部以 `design_` 为前缀，具体文件名由 bundle.yaml 中 `docs[].output` 声明

## 4. 执行口径

```
design-detailer --profile <profile> --module <module>
```

可选参数：

- `--docs <doc_id,...>`：只生成 bundle 的子集文档
- `--profile <...>`：显式覆盖 profile

参数组合、依赖补齐与选择规则统一遵循 `bundle-orchestrator`。

### 返回结果必须包含

1. 输出目录路径
2. 本次实际生成的文档列表
3. 因依赖自动补齐的文档列表
4. `blocked` 项数量及原因
5. `design-report.md` 路径
6. 执行方式说明（子 Agent 并行执行 / 主 Agent 串行退化）

## 5. 角色职责

| 维度     | 主 agent                                                                 | 文档生成子 Agent                                                        | 自检子 Agent                                                          |
| -------- | ------------------------------------------------------------------------ | ----------------------------------------------------------------------- | --------------------------------------------------------------------- |
| **触发** | Skill 入口                                                               | 主 agent 按 bundle 派发                                                 | 主 agent 在文档生成后并行派发                                         |
| **输入** | `hld*.md`、`spec_*.md`、`bundle.yaml`、bundle-orchestrator               | 分配到的 `input_files`、依赖文档、模板、规则文件、框架规范路径          | 对应端文档、validation checklist、validation rules                    |
| **职责** | 解析参数、确认 profile、解析 bundle、安排 phase、汇总报告与 blocked 信息 | 按模板与规则生成目标详细设计文档，处理信息缺口，必要时触发 blocked      | 检查并修复详细设计文档，输出检查报告，配合主 agent 做跨文档一致性校验 |
| **输出** | `design-report.md`、最终返回结果                                         | `design_client_*.md`、`design_server_*.md` 或 bundle 定义的其他设计文档 | `design-check-server-report.md`、`design-check-client-report.md`      |
| **禁止** | 不硬编码模板，不静默忽略 blocked 或遗留问题                              | 不补发需求，不保留占位符，不跳过信息缺口处理规则                        | 不只检查不修复；仍不符合时必须标记遗留                                |

## 6. 执行流程

不允许跳步。bundle 解析、phase 选择、依赖补齐与失败回退统一遵循 `bundle-orchestrator`。

**主 agent 读取与派发约束**：

- 主 agent 负责读取 `bundle.yaml`、`bundle-orchestrator` 以及 bundle 声明的规则入口，不在 Skill 中硬编码具体模板或框架规则
- 主 agent 负责编排与汇总，不替子 Agent 展开模板内容，也不替子 Agent 消化其专属 `input_files`
- `hld_*` 是正式主输入，主 agent 和子 Agent 都不得补发需求
- 框架规范在生成前必须加载，在生成后必须校验；若校验失败，必须重写受影响文档
- 任何 blocked 与遗留问题都必须显式记录并告知用户，不允许静默忽略

| Step | 动作                       | 关键要求                                                                                                                                                                                                                                                                                                                                                               | 规则来源                                                                                                       |
| ---- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| 1    | 确认输入信息               | 确认模块名（`--module`），确定输入输出目录 `xspec/changes/<module>/`；确认 profile：用户指定 `--profile` 直接使用，否则自动从文档关键词匹配并向用户确认；可选 `--docs <doc_id,...>` 指定子集生成                                                                                                                                                                       | `bundle-orchestrator`                                                                                          |
| 2    | 解析输入输出路径           | 校验 `xspec/changes/<module>/hld*.md` 存在；输出目录不存在则自动创建；确定 bundle 路径：`xwRef/xuanwu/<profile>/tech-design/bundle.yaml`                                                                                                                                                                                                                    | `bundle.yaml`                                                                                                  |
| 3    | 加载 profile 详细设计配置  | 读取 `xwRef/xuanwu/<profile>/tech-design/bundle.yaml`；按 `bundle-orchestrator` 的通用规则解析 bundle、选择文档、补齐依赖并计算 phase 顺序；校验 `doc_id` 唯一、模板文件存在、`output` 不重复                                                                                                                                                               | `bundle.yaml`、`.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`                                            |
| 4    | 加载框架规范               | Skill 不得硬编码框架规则，仅负责读取并应用 bundle 声明的规则文件；生成文档前**必须**加载框架规范，生成后**必须**校验是否满足约束，失败则重新生成；子任务启动时，`prompt` 中必须显式传递框架规范读取指令                                                                                                                                                                | `bundle.yaml` 中的 `shared_rules` / `rules_md`                                                                 |
| 5    | 按 bundle 生成详细设计文档 | 对每个选中 `docs[]` 项，使用 `task` 工具（`subagent_type: general`）启动子 Agent；`prompt` 必须包含角色声明、输入清单（`input_files` + 依赖文档）、模板路径、规则文件、输出绝对路径、框架规范约束与业务约束；按 `execution_plan` phase 顺序执行：`server-detailed-design`（并行）→ `client-detailed-design`（并行）；每完成一个文档输出 `✅ <文件名> 已完成 (n/total)` | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`、`bundle.yaml`、bundle 声明的 `shared_rules` / `rules_md` |
| 6    | 规范自检                   | 并行启动服务端与客户端自检子 Agent：读取 bundle 的 `validation.checklists` 指向的检查清单，检查并修复 `design_server_*.md` 与 `design_client_*.md`，输出对应检查报告；每项按“检查 → 修复 → 重检 → 遗留”执行，标注 `✅通过` / `🔧修复后通过` / `❌遗留问题`                                                                                                             | `bundle.yaml` 中的 `validation.checklists`                                                                     |
| 7    | 汇总与校验                 | 生成 `design-report.md`；至少校验：所有选中文档已生成、无残留模板占位符、跨端一致性（同一协议字段/事件名在双端文档命名一致）、`hld_*` 概要中的关键约束在 `design_*` 中已深化或显式标风险；若失败，优先重写受影响文档而不是整批重做                                                                                                                                     | `bundle.yaml` 中的 `validation.rules`、汇总规则                                                                |
| 8    | 完成                       | 输出结果文件列表、`design-report.md` 路径，以及 `blocked` 项数量及原因                                                                                                                                                                                                                                                                                                 | 返回结果约束                                                                                                   |

### Step 5 子 Agent Prompt 必含项

- 角色声明
- 输入清单（`input_files` + 依赖文档）
- 模板路径
- 规则文件（`shared_rules` / `rules_md`，优先遵守规则再遵守模板）
- 输出路径（绝对路径）
- 框架规范约束（若 bundle 声明了框架规则）
- 业务约束：
  - `hld_*` 是正式主输入，不能补发需求
  - 若真实路径、节点名、事件名、协议字段证据缺失，按信息缺口处理规则执行
  - 禁止使用占位符；可推断则推断，否则 blocked 上报

## 7. blocked 条件

- 上游 `hld_*` 文档缺失关键技术约束
- 真实代码无法确认所需文件、节点或命名依据
- FGUI 设计缺失 `uideclare` 路径或声明来源
- 协议字段、消息名或页面入口存在多种可能，且无法从仓库中证实

**blocked 上报格式**：

```
## blocked: <doc_id>
- **原因**：<具体阻塞原因，如"无法确认入口脚本路径" >
- **上游文档**：hld.md / hld_server.md / hld_client.md（列出缺失的文档）
- **建议动作**：<用户需要补充的信息，如"提供活动入口的 FGUI 包路径" >
```

## 8. 信息缺口处理规则

禁止在最终文档中出现 `?`、`待确认`、`TBD` 等未决占位符。当遇到信息缺口时，按以下优先级处理：

1. **可推断**：根据 `hld_*` 概要文档、真实代码仓库或同类活动稳定推断的内容，直接填入推断值，不留占位符。
2. **触发 blocked**：满足上方 blocked 条件（关键路径缺失、证据冲突、无法证实）时，将该条目标记为 `blocked: <原因>` 并上报，停止对该文档的后续生成，**不得**用占位符代替 blocked 语义。
3. **非关键缺口**：缺口不满足 blocked 条件、也无法稳定推断时，使用当前上下文中最合理的缺省值填入，并在 Step 7 的校验记录中注明"已使用推断缺省值：<字段>：<依据>"。

**禁止的做法**：任何情况下都不允许在最终文档中保留 `?`、`TBD`、`待确认` 等占位文字；如确实无法处理，唯一合法出口是触发 blocked 并上报。

## 9. 阶段完成条件

以下全部满足才视为 tech-design 阶段完成：

- 所有选中的详细设计文档已生成，依赖补齐文档已纳入结果
- `design-check-server-report.md` 与 `design-check-client-report.md` 已生成
- `design-report.md` 已生成
- 无残留模板占位符
- 跨端关键字段与事件命名已完成一致性校验
- `hld_*` 概要中的关键约束已在 `design_*` 中深化或显式标风险
- 所有 blocked 与遗留问题已明确记录并告知用户

## 10. 触发与选择规则

**自动整套生成**：触发词：详细设计、生成 FGUI 设计、生成核心模块、generate design detail

**指定子集生成**：`--docs client-fgui-design,server-redis-design` 等；匹配、依赖补齐与 phase 调度统一遵循 `bundle-orchestrator`。

## 11. 资源索引

| 文件                                                        | 用途                                                    |
| ----------------------------------------------------------- | ------------------------------------------------------- |
| `xwRef/xuanwu/<profile>/tech-design/bundle.yaml` | tech-design 阶段 bundle、输入输出声明、phase 与校验入口 |
| `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`        | 通用 bundle 调度、依赖补齐与 phase 规则                 |
| `.claude/agents/xuanwu_architect.md`                        | architect 子 Agent 角色定义                             |
| `.claude/agents/xuanwu_reviewer.md`                         | reviewer 子 Agent 角色定义                              |
