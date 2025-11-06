---
name: task-work
description: "读取设计文档提取冻结约束，调度分片 agent 并行拆分原子任务，生成 freeze index / coverage map / gap-scan / 自检报告与 task-handoff.json。"

disable-model-invocation: true
---

# task-work

task 阶段技能。进入前，默认 detail 阶段已经收口。本技能只负责冻结约束提取、任务拆分、覆盖对账、自检与 handoff，不负责重新设计方案。

## 1. 定位与边界

**职责**：

- 读取 `design_*` 与 HLD 文档，提取轻量冻结约束（freeze constraints）
- 生成端侧 freeze index
- 按 bundle 调度多个 shard agent 并行拆分原子任务
- 生成双端 coverage map、gap-scan、自检报告与 `task-handoff.json`

**不职责**：

- 不重新做设计决策；发现上游设计缺口时必须停止并报告
- 不脱离冻结约束自由发挥 task 内容
- 不为了实现效率合并或重排实现颗粒；实现阶段由 `code-generator` 基于 Coverage Task 聚合 IU

**核心原则**：

- freeze 是任务前结构化约束层，不是第二套详细设计文档
- task 内容只能来自 freeze constraints、freeze index 和显式允许的 design 证据
- task-work 产出的 task 是 **Coverage Task**，用于 freeze 覆盖、gap-scan、自检和追溯，不等同于实现阶段的 agent 派发颗粒
- 一个 task 只做一个原子动作，最多改动 2 个文件
- 改动文件必须是真实路径；无真实路径则标 `blocked: 原因`
- 固定切面保证稳定性，`gap-scan` 负责查漏补缺，避免切面外实现点静默丢失
- 每次执行都按完整流程从 Step 1 开始；若目录中已存在旧的 task 阶段产物，只视为既有文件背景，不触发跳步

## 2. 输入与输出

### 输入

| 来源         | 路径                                                                |
| ------------ | ------------------------------------------------------------------- |
| Task Bundle  | `xwRef/xuanwu/<profile>/task/bundle.yaml`                |
| 设计文档     | `xspec/changes/<module>/design_*`、`hld_*` 及 bundle 声明的相关输入 |
| 通用调度规则 | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`                |

### 输出

| 产物                | 路径                                                 |
| ------------------- | ---------------------------------------------------- |
| 客户端 freeze index | `xspec/changes/<module>/task_client_freeze_index.md` |
| 服务端 freeze index | `xspec/changes/<module>/task_server_freeze_index.md` |
| freeze 分片         | `xspec/changes/<module>/task_*_freeze_*.md`          |
| task 分片           | `xspec/changes/<module>/task_*_part_*.md`            |
| coverage ledger     | `xspec/changes/<module>/task_*_coverage_ledger.md`   |
| coverage map        | `xspec/changes/<module>/task_*_coverage_map.md`      |
| gap-scan            | `xspec/changes/<module>/task_*_gap_scan.md`          |
| 自检报告            | `xspec/changes/<module>/task_*_self_check.md`        |
| handoff             | `xspec/changes/<module>/task-handoff.json`           |
| 阶段报告            | `xspec/changes/<module>/task-report.md`              |

## 3. 上下文分片架构

本 SKILL 采用**切面分片 + 语义化编号前缀预分配**架构，避免单 agent 读全量文档导致 context 爆炸。

> 架构详情（三层冻结产物、消费层次、编号规则）见 `bundle.yaml` 注释中的 Context-Sharding Architecture 说明。

跨分片 `depends` 引用直接使用全局语义编号（如 `depends: fgui-001, state-003`），因为每个分片的编号前缀已预分配，不会冲突。

任务分片固定前缀如下：

- 客户端：`fgui`、`state`、`entry`、`peri`
- 服务端：`server-core`、`server-service`
- 补漏：`client-gap`、`server-gap`

## 4. 角色职责

| 维度     | 主 agent                                                                                                      | 冻结 / 任务分片 subagent                                          |
| -------- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| **触发** | SKILL 入口                                                                                                    | 主 agent 并行派发                                                 |
| **输入** | 只读 `bundle.yaml`                                                                                            | 按 `bundle.yaml` `input_files` 自行读取                           |
| **职责** | 解析 bundle、按 phase 并发派发 subagent、汇总结果                                                             | 读取自己切面的 freeze / task / extract-template / rules，生成产物 |
| **禁止** | 读取任何 extract template、rules、`input_files` 声明的文档；检查设计完整性只用 shell 检查文件存在性，不读内容 | 跨切面读取其他分片文件                                            |

## 5. 执行流程

不允许跳步。详细规则见各步骤引用的 bundle / rules。

**主 agent 文件读取约束**：`bundle.yaml` 中各 doc 的 `input_files` 是声明性范围描述，不是加载指令。主 agent 在整个执行周期内：

- **禁止**预读任何 extract template 文件（`extract-spec/freeze/*.md`、`extract-spec/tasks/*.md` 等）；这些文件由 subagent 在自己的 context 内按需读取
- **禁止**读取 rules 文件（`rules/*.md`）；rules 由对应 subagent 在执行时按需读取
- **禁止**读取 `design_*`、`hld_*` 等设计文档；检查设计文档完整性只做文件存在性检查（shell `ls`），不读取文件内容
- **禁止**读取 freeze 产物（`task_*_freeze_*.md`）和任务分片产物（`task_*_part_*.md`）；这些由下游 phase 的 subagent 消费
- Step 1 只读 `bundle.yaml`，**未完成 Step 1，禁止进入 Step 2**

| Step | 动作                                                                                                                                                                                                                                                                                                    | 规则 / 关键约束                                                                                                                                                          |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1    | 读取 `xwRef/xuanwu/<profile>/task/bundle.yaml`；按 `bundle-orchestrator` 规则解析 bundle、计算 phase 顺序；从 `id_allocation` 提取各分片语义编号前缀并注入到对应 subagent prompt；检查上游 `design_*` 是否具备提取 freeze constraints 所需证据（只做 shell `ls` 文件存在性验证，不读取内容） | `bundle.yaml`、`.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`                                                                                                      |
| 2    | Phase 1：轻冻结约束分片。**在同一个 response message 中并发发出对应 phase 的 `task` 工具调用**（`subagent_type: general`）                                                                                                                                                                              | 分片列表从 `execution_plan.phases[client-freeze-constraints]` 与 `[server-freeze-constraints]` 读取；主 agent 只传递解析后的路径字符串，不读取 extract template 文件本身 |
| 3    | Phase 2：端侧 freeze index。等待 Freeze phase 全部完成后，运行本地确定性 projector/checker：`node .xflow/scripts/xuanwu/xf-freeze-index.mjs --module {module}`                                                                                                                                          | 成功（exit 0）则直接使用脚本产物；任何非 0 退出（exit 2 = `FREEZE_INDEX_FALLBACK`，exit 1 = 异常崩溃）一律将该端 `freeze-index` 条目回退为 AI subagent 执行，不中断流程  |
| 4    | Phase 3：任务分片。等待 freeze index phase 全部完成后，**在同一个 response message 中并发发出 shard `task` 工具调用**                                                                                                                                                                                   | 分片列表从 `execution_plan.phases[client-task-shards]` 与 `[server-task-shards]` 读取；编号前缀从 `id_allocation.tasks` 读取                                             |
| 5    | Phase 4：coverage reconcile（双端并行）。等待任务分片全部完成后，运行本地确定性 coverage map projector：`node .xflow/scripts/xuanwu/xf-coverage-map.mjs --module {module} --side client` 与 `--side server`；主 agent 按脚本 tri-state 结果分流（`ok` / `valid_with_unresolved` / `invalid_requires_fallback`）                                                                 | `ok` / `valid_with_unresolved` 直接进入 Step 6；仅 `invalid_requires_fallback` 回退对应端 AI subagent                                                                 |
| 6    | Phase 5：gap-scan（双端并行）。**在同一个 response message 中并发发出 2 个 `task` 工具调用**                                                                                                                                                                                                            | 分片列表从 `execution_plan.phases[task-gap-scan]` 读取；gap-scan 任务作为独立 shard 保留，并由 `task-handoff.json` 声明给 code-generator 消费                            |
| 7    | Phase 6：自检（双端并行）。**必须等待 Step 6 的 client/server gap-scan 都完成并回填 coverage map 后**，再在同一个 response message 中并发发出 2 个 `task` 工具调用                                                                                                                                       | 自检除格式外，必须拦截 `missing` 覆盖状态，并直接修复或上报                                                                                                              |
| 8    | Phase 7：handoff + 报告（并行）。自检全部完成后，**在同一个 response message 中并发发出 2 个 `task` 工具调用**                                                                                                                                                                                          | `task-handoff.json` 是 code-generator 唯一新入口；report subagent 读取 handoff 声明的文件并生成 `task-report.md`                                                         |

### Step 1 补充约束

**路径注入约束**：注入 subagent prompt 前，先将 `extract_template` / `rules` 按 bundle 所在目录解析为仓库相对路径。例如 `extract-spec/tasks/client-data-fgui.md` 应注入为 `xwRef/xuanwu/<profile>/task/extract-spec/tasks/client-data-fgui.md`。

检查设计文档是否存在缺口，有缺口时立即停止：

```text
⛔ 详细设计文档不完整，无法拆分任务

缺口项：
  - design_server_protocol.md § 接口定义：{具体缺口说明}

请先完善 design-detailer 产出后重新执行 /xf:task
```

### Step 3 补充说明：freeze index fallback 与约束

> 输入/输出列表从 `bundle.yaml` `execution_plan.phases[freeze-index]` 及对应 docs 读取。

**失败回退**：

- 若脚本 exit 0（`FREEZE_INDEX_OK`），直接使用脚本产出的 freeze index 继续后续 phase
- 若脚本 exit 非 0（`FREEZE_INDEX_FALLBACK` / exit 2，或意外崩溃 / exit 1），主 agent **不中断整条流程**；将该端 `freeze-index` 条目回退为 AI subagent 执行，并将脚本 stderr 输出的错误详情附到 subagent prompt 作为参考
- 回退后的 AI subagent 只允许读取该端 freeze shards 与 exceptions，按 `extract_template` 产出 `task_*_freeze_index.md`，不得额外扩读无关 design / detail

freeze index 是端侧全集基准，由本端 freeze shard 投影生成；固定字段为：
`freeze_id / facet / source_design / requirement_summary / acceptance`。

**关键约束**：

- 每个 `task_*_freeze_*.md` 中的 freeze item 必须出现在本端 freeze index
- freeze index 不得自行发明 freeze_id
- `cross_cutting=yes`、`facet=unclassified`、`blocked_reason != —` 的 freeze item 必须出现在对应 `*_exceptions.md`
- freeze shard 必须统一使用横向 Markdown 表，列名固定为 `freeze_id / facet / source_design / requirement_summary / real_paths / depends_on / acceptance / blocked_reason / cross_cutting / notes`
- exceptions 文件必须复用主 freeze item 编号，不得在 exceptions 中发明 `*-C*` / `*-B*` 新编号
- freeze / exceptions 任一单元格若包含 `|`，必须写成 `\\|`
- 脚本任何非 0 退出均触发回退，主 agent 不因脚本校验问题中断 task-work 流程
- 主 agent 不派发 freeze-index subagent，也不读取 freeze 正文做模型总结

### Step 4 补充说明：任务分片约束

> 分片列表从 `bundle.yaml` `execution_plan.phases[client-task-shards]` 和 `[server-task-shards]` 读取。
> 编号前缀从 `bundle.yaml` `id_allocation.tasks` 读取。

**关键约束**：

- shard agent 只允许处理自己负责的切面
- shard agent 必须读取解析后的 `extract_template` 仓库相对路径，不得把未解析的 `extract-spec/...` 当作仓库根路径；模板缺失判断必须基于解析后的路径
- 无法归类的 freeze item 不得吞掉，必须写入 `cross_cutting` / `unclassified` 结果并交由 `gap-scan`
- 同一个 freeze item 至少落入 `task`、`blocked`、`intentionally_merged` 三者之一
- 每个 shard 除任务文件外，还必须产出一个 `coverage ledger`，只记录自己处理过的 freeze item 及其结果；ledger 文件名由 bundle.yaml 对应分片的 `output_ledger` 字段指定

**cross_cutting 任务归属规则**：

- `cross_cutting=yes` 的 freeze item 由**标记该 item 的冻结分片所属 shard** 负责生成任务（即“谁冻结谁认领”）
- 若 cross_cutting item 涉及的改动文件属于**另一切面**（如 peripheral item 需改 foundation 域文件），任务仍由原 shard 生成，但必须在任务 `depends` 字段中注明对应 foundation 任务，并在 `notes` 中注明跨切面改动；另一切面 shard 不重复生成该任务
- 若 cross_cutting item 确实无法在现有切面边界内合理归属，则转入 `unclassified` 并交由 gap-scan 决定归属，不得由两个 shard 各自认领

### Step 5 补充说明：coverage map fallback 与约束

> 输入/输出列表从 `bundle.yaml` `execution_plan.phases[task-coverage-reconcile]` 及对应 docs 读取。

**执行规则**：

- `xf-coverage-map.mjs` 必须始终先执行；主 agent 不能只凭 exit code 判断成功，必须读取 tri-state 结果
- `ok` / `valid_with_unresolved`：直接使用脚本产物进入 Step 6；`valid_with_unresolved` 由 gap-scan 收口真实 `unresolved`
- `invalid_requires_fallback`：仅该端回退 AI subagent；回退 subagent 只允许读取本端 freeze index、exceptions、coverage ledgers，不默认扩读 detail / design

**关键约束**：

- required 输入必须齐全且被实际读取；缺失不允许静默 `continue`
- `freeze_total = covered + blocked + intentionally_merged + unresolved + missing`
- 不允许将路径缺失、schema 漂移、统计不守恒或大面积 `no matching coverage ledger row` 视为可信 coverage map
- fallback 回传需标记原因类别：`path_mismatch` / `schema_drift` / `ledger_incomplete` / `true_semantic_unresolved`

### Step 6 补充说明：gap-scan 规则

> 分片列表从 `bundle.yaml` `execution_plan.phases[task-gap-scan]` 读取。

双端 `gap-scan` 只允许读取可信 coverage map + exceptions，先收敛候选 `freeze_id` 集合，再按项读取 ledger；只有候选单项仍无法闭环时才允许回看对应证据。候选集合包括 coverage map 中 `unresolved` / `missing`，以及 exceptions 中需由 gap-scan 收口的 blocked / unclassified / `suggested_owner=gap-scan` / `suggested_action=confirm_*|evaluate_new_task` 项。路径缺失、schema 漂移、输入不兼容不属于 gap-scan 职责，必须在 Step 5 收口。完成后回填本端 coverage map。gap-scan 任务作为独立 shard 保留，并由 `task-handoff.json` 声明给 code-generator 消费。

gap-scan 编号前缀（已在 `bundle.yaml` `id_allocation` 中预分配，subagent prompt 注入时必须携带）：

- `client-gap-scan`：**client-gap**
- `server-gap-scan`：**server-gap**

### Step 7 补充说明：自检修复后的 coverage map 同步规则

> 分片列表从 `bundle.yaml` `execution_plan.phases[task-self-check]` 读取。

- 自检若直接修复了任务内容（新增或修改 task），必须同步更新本端 `coverage_map.md` 中对应条目的 `covered_by_task_ids` 和 `status`
- 修复后在自检报告末尾追加一节“自检修复汇总”，列出所有被修改的 freeze_id 及修改原因，供 task-report 阶段使用
- 若自检选择“上报”而非“修复”，则将该 freeze_id 标记为 `missing`，task-report 会将其列为阻塞项

### Step 8 补充说明：handoff + 报告

> 分片列表从 `bundle.yaml` `execution_plan.phases[task-finalize]` 读取。

`task-handoff.json` 是 code-generator 唯一新入口，只记录路径、端侧、类型、required 状态，不复制 task / freeze 正文。必须包含本端 `freeze_index`、`freeze_shards`、`task_shards`、`coverage_map`、`self_check`、`gap_scan`。

report subagent 负责：读取 `task-handoff.json` 声明的文件、双端 coverage map、self-check、gap-scan、freeze index，按 `extract-spec/task-report.md` + `publish/task-report.md` 规范生成并写入 `task-report.md`。

**进度输出**：subagent 完成后主 agent 输出 `✅ task-handoff.json / task-report.md 已完成`

## 6. 阶段完成条件

以下全部满足才允许推进到实现阶段：

- 客户端与服务端 freeze index 均已生成，且无 blocking 错误
- 所有必须的 freeze 分片、task 分片、coverage map、gap-scan、自检产物均已生成
- `coverage_map` 中 `missing` 项已直接修复或正确上报为阻塞项
- `task-handoff.json` 已生成，且 required 文件声明完整可用
- `task-report.md` 已生成，并能汇总双端 coverage 与分片统计结果
- 不存在因输入缺失、freeze index blocked、coverage reconcile 失败且 fallback 也失败而遗留的未处理阻塞

## 7. 任务卡标准格式

所有分片产出的任务卡必须使用以下统一格式：

```markdown
- [ ] {semantic-prefix}-{NNN} [P?] {原子动作描述}
      切面: {固定切面值}
      覆盖冻结项: {freeze_id 列表，如 C-CORE-001, C-FGUI-D003}
      来源设计: {source_design 文件名}
      改动文件: {真实路径（≤2个）}
      参考锚点: {仓库中已存在的同类实现路径}
      depends: {依赖的 task id 列表，或 —}
      完成标准: {具体代码产出，如字段名/节点名/参数名/函数签名}
      约束: {冻结签名/最小改动范围/外部依赖的处理方案}
      DoD: {仅写切面专属追加项；无追加项可省略此字段}
      coverage_status: covered | blocked | intentionally_merged
      notes: {若转交 gap-scan 或合并，写明原因；否则可省略}
```

### 字段说明

| 字段              | 必填 | 说明                                                                   |
| ----------------- | ---- | ---------------------------------------------------------------------- |
| `切面`            | ✅   | 固定切面值，不允许自由命名                                             |
| `覆盖冻结项`      | ✅   | 必须使用 freeze ID，不允许自然语言                                     |
| `改动文件`        | ✅   | 真实仓库路径，最多 2 个文件                                            |
| `参考锚点`        | ✅   | 仓库中已存在的同类实现                                                 |
| `depends`         | ✅   | 依赖的 task id，无依赖写 `—`                                           |
| `完成标准`        | ✅   | 具体代码产出（字段 / 节点 / 参数等）                                   |
| `约束`            | ✅   | 本卡的边界和限制                                                       |
| `DoD`             | ⚪   | 仅写切面专属追加项；通用 2 项由执行 agent 全局强制，无追加项省略此字段 |
| `coverage_status` | ✅   | 覆盖状态                                                               |
| `notes`           | ⚪   | 可选，转交或合并时填写                                                 |

### 文件数规则

单个 task 最多改动 2 个文件。若超过 2 个文件，必须继续拆小；不得放宽该上限。

### 完成标准写法

按切面类型，完成标准必须写明具体产出：

- `data_model`：逐字段列出 DTO 字段名（如 `id, stage, rewards, expire_time`）
- `fgui_view_binding` / `ui_state_render`：逐一列出 `KW_*` 节点名
- `analytics`：列出全部埋点参数名
- `entry_mount` / `popup_or_settlement_hook`：写明打开目标、关闭方式、触发条件
- `module_flow`：写明 `send / parse / write-back / dispatch` 的顺序

禁止“与文档一致”“按设计实现”等模糊表述。

### DoD（完成定义）

以下 2 项为**执行 agent 全局强制规则**，适用于所有 task，无需在任务卡中重复声明：

- [ ] 代码可编译，无语法错误
- [ ] 产出完整，字段 / 节点 / 签名与完成标准逐一对应

以下项**按切面条件适用**，由生成 task 的 shard agent 按需写入任务卡 `DoD` 字段：

- 有运行时逻辑的切面（`module_flow`、`entry_mount`、`popup_or_settlement_hook` 等）：`日志可定位，含关键 trace 信息`
- 有调用入口的切面：`边界已处理，异常有明确反馈`

任务卡中 `DoD` 字段**仅用于追加切面专属项**（如需要），无追加项时省略该字段。

### 约束写法

- 必须写明本卡冻结的接口签名
- 必须写明最小改动范围
- 若有外部依赖，必须写明假实现 / mock 方案

### 黑名单（禁止出现）

以下词汇禁止出现在任何 task 字段中：

- “优化 / 完善 / 健壮性 / 体验更好 / 通用化”
- “抽象封装 / 重构以提升质量 / 提升性能”
- “某配置文件”“待定”“后续补充”“与文档一致”
- 任何无法直接验证的模糊表述

## 8. 分片统计与输出约定

### 分片统计行格式

每个分片文件末尾必须输出统计行，供下游读取时只需扫描末尾：

```text
[SHARD-STATS] <shard-name>: id_prefix=<semantic-prefix>, id_used=N, freeze_covered=N/Y, intentionally_merged=N, blocked=N
```

### 输出约定

完成后报告：

- 最终保留文件路径（`task-handoff.json` / `task_client_freeze_index.md` / `task_server_freeze_index.md` / freeze shards / task shards / coverage maps / gap-scan / self-check / `task-report.md`）
- 各切面任务数汇总（P0 / P1 / P2）
- 各分片统计行（freeze 覆盖率、`intentionally_merged`、blocked 项数）
- 双端 coverage map 汇总（covered / blocked / intentionally_merged / unresolved / missing）
- overflow 警告（若某分片编号超出预分配段）

## 9. 资源索引

| 文件                                                   | 用途                                      |
| ------------------------------------------------------ | ----------------------------------------- |
| `xwRef/xuanwu/<profile>/task/bundle.yaml`   | task 阶段执行计划、分片定义、输入输出声明 |
| `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`   | 通用 bundle 调度规则                      |
| `xwRef/xuanwu/<profile>/task/extract-spec/` | freeze / task / report 提取模板           |
| `xwRef/xuanwu/<profile>/task/rules/`        | task 阶段各类规则文件                     |
