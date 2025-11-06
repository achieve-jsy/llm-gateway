---
name: code-generator
description: 基于 task-handoff.json 执行实现阶段，生成双端 Implementation Unit、独立校验本端 IU、构建 IU DAG、安排并行波次、约束 write set、执行检查并输出 impl 报告。

disable-model-invocation: true
---

# code-generator

代码实现技能。进入前，默认 tech、tech-design 和 task 阶段已收口。本技能只负责实现，不负责重新设计方案。

## 1. 定位与边界

**职责**：

- 读取任务卡，生成双端 Implementation Unit（IU），独立校验本端 IU，构建双端 IU DAG，安排波次，派发子 agent
- 执行实现检查、安全修订，输出 impl 报告

**不职责**：

- 不补发新需求，不重写设计结论，不改变原始 task 的业务含义
- 不跳步，不合并客户端与服务端 DAG
- 锚点、依赖或必须文件缺失时，必须阻塞上报，不猜测实现

## 2. 输入与输出

### 输入

| 来源            | 路径                                                 |
| --------------- | ---------------------------------------------------- |
| Task handoff    | `xspec/changes/<module>/task-handoff.json`           |
| 客户端 HLD 概要 | `xspec/changes/<module>/hld_client.md`               |
| 服务端 HLD 概要 | `xspec/changes/<module>/hld_server.md`               |
| Impl Bundle     | `xwRef/xuanwu/<profile>/impl/bundle.yaml` |

框架规范路径由 `bundle.yaml` 的 `shared_rules` 字段声明，SKILL 不硬编码。

### 输出

| 产物                  | 路径                                                            |
| --------------------- | --------------------------------------------------------------- |
| IU 草案（客户端）     | `xspec/changes/<module>/runtime/client-implementation-units.md` |
| IU 草案（服务端）     | `xspec/changes/<module>/runtime/server-implementation-units.md` |
| IU 校验记录（客户端） | `xspec/changes/<module>/runtime/client-iu-validation.md`        |
| IU 校验记录（服务端） | `xspec/changes/<module>/runtime/server-iu-validation.md`        |
| 集成修复记录          | `xspec/changes/<module>/runtime/integration-fix.md`             |
| 实现报告              | `xspec/changes/<module>/impl-report.md`                         |
| 运行时记录            | `xspec/changes/<module>/runtime/*.md`                           |
| 实际代码              | `lua/`、`client/assets/` 或 `client/fgui/` 对应真实路径         |

## 3. 代码根目录

- 服务端：`lua/`（`AwardLua/` 和 `BaseAwardLua/` 均在其下）
- 客户端代码：`client/assets/`
- FGUI 工程：`client/fgui/`（资源文件在 `client/fgui/assets/`）

## 4. 角色职责

| 维度     | 主 agent                                                                                                                                                     | IU Planner（client / server）                                                                      | 子 agent                                                                           |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| **触发** | SKILL 入口                                                                                                                                                   | 主 agent 并行派发                                                                                  | 主 agent 按波次派发                                                                |
| **输入** | task-handoff.json、bundle.yaml                                                                                                                               | 本端 handoff 声明的 task shards、freeze index、coverage map、self-check、gap-scan、本端 spec-index | IU 摘要、写集文件、框架规范路径、参考锚点路径                                      |
| **职责** | 加载框架规范；派发 planner；并行派发双端 validator；构建双端独立 DAG；输出派发计划；逐波次派发执行；汇总检查结果；执行修订；输出包含执行索引概览的 impl 报告 | 按"写集 → 切面 → 依赖层级"聚合本端 IU，输出本端 IU 草案                                            | 读取框架 spec-index 及相关子规范；在写集约束内实现代码；自检后返回结果或 `blocked` |
| **输出** | `impl-report.md`                                                                                                                                             | `runtime/client-implementation-units.md` 或 `runtime/server-implementation-units.md`               | 实际代码文件 + short handoff（流水线模式）                                         |
| **禁止** | 粘贴全量任务卡到子 agent prompt；读取 handoff 未声明的任务/冻结文件                                                                                          | 跨端读取对方 handoff 文件集合                                                                      | 修改写集外的文件；补充新需求；猜测实现                                             |

> Coverage Task（原始 task）只用于覆盖追溯、验收账本和 IU 聚合输入，不直接作为 DAG 执行节点。

## 5. 执行流程

不允许跳步。详细规则见各步骤引用的规则文件。

**主 agent 文件读取约束**：`bundle.yaml` 中各 doc 的 `input_files` 是声明性范围描述，不是加载指令。主 agent 在整个执行周期内：

- **禁止**预读实际代码文件（`.lua`、`.ts`、`.tsx`、`.xml` 等）；实际代码只由子 agent 在执行对应 IU 时按写集按需读取
- **禁止**读取任务卡正文；任务内容由 IU planner 消化。主 agent 只读取 `task-handoff.json` 的路径清单和 required 状态，不读取其中声明的 task shard 正文
- **禁止**读取框架实现子规范（eslint、fgui-create、ui-code 等）；这些由 IU 执行子 agent 按需读取
- 规则文件按步骤表"规则文件"列的指引，在对应步骤执行前按需读取，不预读

| Step | 动作                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | 规则文件                                                                                           |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| 1    | 读取 `bundle.yaml` 的 `shared_rules` 字段（仅此字段），获取双端框架规范 spec-index 路径；读取 `xspec/changes/<module>/task-handoff.json` 路径清单并校验 required 文件存在；若缺 required 文件，立即 blocked。**主 agent 只需存储路径，不需要读取 spec-index 或任务正文**。                                                                                                                                                                                                                                                                                                   | `bundle.yaml`                                                                                      |
| 2    | 并行派发 `client-IU-planner` 与 `server-IU-planner`。planner 只读本端 handoff 中 `task_shards` + `gap_scan` 声明的文件及 spec-index，**禁止**读取 freeze_index / freeze_shards / exceptions / coverage_map / coverage_ledgers / self_check，各自输出本端 IU 草案                                                                                                                                                                                                                                                                                                             | `rules/dag-scheduling-rules.md`                                                                    |
| 3    | 主 agent 回收双端草案后，**不合并、不生成 `implementation_units.md`**；并行派发 `client-IU-validator` 与 `server-IU-validator`，分别以本端 coverage map 为准校验所有 `covered` task 均已映射到本端 IU、`task -> IU -> freeze_id` 追溯完整、IU 依赖只引用本端 IU、IU 写集等于覆盖 task 的 `改动文件` 并集，`blocked` task 不进入执行 IU，`intentionally_merged` task 只映射到承接 IU；有遗漏或越界则阻塞；通过后直接进入 Step 4                                                                                                                                               | `rules/dag-scheduling-rules.md`、`rules/blocked-conditions.md`                                     |
| 4    | 分别基于 `runtime/client-implementation-units.md` 和 `runtime/server-implementation-units.md` 构建客户端和服务端 DAG（禁止合并），计算并行波次，推导 write set；同文件客户端 IU 必须串行化或 same-file-pipeline，不得进入同一并行波次                                                                                                                                                                                                                                                                                                                                        | `rules/dag-scheduling-rules.md`、`rules/write-set-rules.md`                                        |
| 5    | 执行前输出双端波次摘要（客户端 N 波、服务端 N 波，各波 IU 数量及并行/串行标记）；该摘要仅用于说明同波可并发的 IU 集合，**不表示一个 agent 承载整波**；确认无文件冲突遗漏后进入 Step 6                                                                                                                                                                                                                                                                                                                                                                                        | `rules/dag-scheduling-rules.md`                                                                    |
| 6    | 客户端与服务端执行流可并行推进；各端内部严格按波次串行；**派发单位是 IU，不是 task**——每次派发以单个 IU ID 为标识，一个 IU 对应一个子 agent，IU 内覆盖的多个 task 由该 agent 顺序完成，禁止按 task ID 拆分为多个 agent，**也禁止把同一波次中的多个 IU 合并派发给一个 agent**；每个子 agent prompt 必须包含 Step 1 加载的 spec-index 路径和"按 index 继续读取相关子规范"的要求；**主 agent 禁止在派发前自行读取框架实现规范**（fgui-create、ui-code、eslint 等），只传路径给子 agent，由子 agent 自己读取；**每个 IU 子 agent 完成后必须输出本 IU 的 `integration_findings`** | `rules/server-agent-dispatch.md`、`rules/client-agent-dispatch.md`、`rules/check-result-schema.md` |
| 7    | 所有 IU 完成后，派发独立的 `integration-fix` 子 agent，汇总双端 task execution 记录中的 `integration_findings`，去重分类，执行 `auto_fix` 项修复，记录结果到 `runtime/integration-fix.md`；**client-self-check 和 server-self-check 必须等本步完成后才能启动**                                                                                                                                                                                                                                                                                                               | `rules/check-result-schema.md`                                                                     |
| 8    | 并行派发 `client-self-check` 和 `server-self-check` 两个子 agent；不能因某侧改动少就跳过任一侧；客户端 ESLint / TypeScript 检查必须回传命令、退出码、错误数、警告数                                                                                                                                                                                                                                                                                                                                                                                                          | `checklist/impl-checklist.md`、`rules/check-result-schema.md`                                      |
| 9    | 主 agent 根据自检结论做安全修订（最多 3 轮）；每轮修订后派发 `client-recheck` 和 `server-recheck` 两个子 agent 并行复检；达到轮次上限后未解决项转为 `blocked_after_retry_limit`                                                                                                                                                                                                                                                                                                                                                                                              | `rules/check-result-schema.md`                                                                     |
| 10   | 输出 `impl-report.md`；报告前半段必须包含执行索引概览，且每个 IU 必须反向追溯到覆盖的 Task ID 和 freeze_id                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `rules/impl-report-rules.md`                                                                       |

## 6. 阶段完成条件

以下全部满足才允许推进到代码审核：

- 所有必须完成的 IU 已完成，并反向映射到覆盖 task
- `runtime/client-iu-validation.md` 与 `runtime/server-iu-validation.md` 均已生成且无 blocking 校验失败
- `runtime/integration-fix.md` 已生成，所有 `integration_findings` 均已分类处理（`auto_fix` 已修复，`blocked` 已记录，无 `needs_regen` 遗留）
- 所有检查发现已关闭或正确标记为 blocked
- 不存在 `blocking=true` 的遗留项
- 不存在未处理的 `auto_fix` 或 `needs_regen`
- 客户端 / 服务端必检项均有命令证据或明确 blocking 的 skipped 原因
- `impl-report.md` 已完整生成，并包含执行索引概览

## 7. 资源索引

| 文件                                                 | 用途                                                          |
| ---------------------------------------------------- | ------------------------------------------------------------- |
| `bundle.yaml`                                        | 执行计划、输入输出声明、shared_rules                          |
| `rules/dag-scheduling-rules.md`                      | IU 字段定义、预算、DAG 节点/波次/文件冲突规则、派发计划表格式 |
| `rules/write-set-rules.md`                           | IU 写集边界 + dispatch 必含字段 + 实现强约束                  |
| `rules/server-agent-dispatch.md`                     | 服务端锚点强制读取 + prompt 模板                              |
| `rules/client-agent-dispatch.md`                     | 客户端 prompt 模板 + 框架规范读取流程                         |
| `rules/fgui-impl-rules.md`                           | FGUI 任务依赖 + uideclare 链路要求                            |
| `rules/blocked-conditions.md`                        | Task 最低字段 + blocked 触发条件                              |
| `rules/check-result-schema.md`                       | 检查结果 schema + 分类定义 + 自动修订策略                     |
| `checklist/impl-checklist.md`                        | 客户端/服务端自检必检项                                       |
| `rules/impl-report-rules.md`                         | 实现报告（含执行索引概览）必含项与阶段推进条件                |
| `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md` | 通用 bundle 调度                                              |
| `.claude/agents/xuanwu_developer.md`                 | 开发 agent 角色定义                                           |
| `.claude/agents/xuanwu_reviewer.md`                  | 审查 agent 角色定义                                           |
