# xuanwu-activity × /xf/implement 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：替换逐任务执行为 code-generator SKILL + 适配双端代码结构

玄武活动的代码实现完全由 `code-generator` SKILL 接管：构建任务 DAG、安排并行波次、约束 write set、执行 client-impl-checker + server-impl-checker、自动修订（最多 3 轮），并输出 `impl` 报告。命令层不再逐任务执行 `[ ] → [x]` 的循环，也不强制 ReadLints（Lua 与 FGUI 资源不走 IDE lint）。

### 保留不动

- **前置条件**：`constitution.md` / `task.md` / `change.yaml.phase ∈ {task, implement}`；**implement 命令本身无门禁**（上一关 `/xf/task` 的门禁已卡在前置条件里）
- **步骤 0（Profile 分支加载）**
- **步骤 2（初始化与确定范围）**：首次执行将 `change.yaml.phase = implement`；用户指定的任务范围参数仍沿用

### 跳过 step 1（加载上下文）

`code-generator` SKILL 自身工作流里已经定义了输入集，命令层不再预读。

- 文件存在性由**前置条件**段兜底
- `change.yaml` 仍由命令层在 step 2 读写

### 替换 step 1.5（检测可用验证命令）

玄武双端代码栈与通用 Web 项目不同。该步骤替换为：

```
📐 xuanwu-activity 验证清单：
  客户端（client/，FGUI + TS）：
    - ReadLints 对 .ts 文件生效
    - 资源自检由 client-impl-checker（code-generator SKILL 内部）统一执行
  服务端（lua/，AwardLua）：
    - ReadLints 对 Lua 不适用
    - 语法/接口自检由 server-impl-checker（code-generator SKILL 内部）统一执行
  命令层不强制 package.json scripts 探测
```

### 更新 change.yaml phase

进入实现阶段时（首次执行），将 `xspec/changes/change.yaml` 的 `phase` 字段更新为 `implement`。

### 替换 step 3（逐任务执行）

通用 step 3 的"读任务 → 写代码 → ReadLints → 标 `[x]`"循环整段替换为调 SKILL：

读取 [`.opencode/skills/code-generator/SKILL.md`](.opencode/skills/code-generator/SKILL.md)，按该 SKILL 指令执行：

- **输入**：
  - Handoff：`xspec/changes/activity/task-handoff.json`
  - 任务分片 / freeze index / coverage map：按 handoff 本端声明读取
  - 自检报告：`xspec/changes/activity/task_client_self_check.md` + `xspec/changes/activity/task_server_self_check.md`
  - 宪章：`xspec/constitution.md`
- **执行模式**：
  - SKILL 自行构建任务 DAG、安排并行波次、约束 write set、派发子 Agent
  - **并行执行（强约束）**：**严格按照该 SKILL 内部定义的并行规则并行执行**——运行环境**允许**子 Agent 并行时**必须**按 SKILL 的 DAG + 波次调度启动子 Agent 并行写代码（同波次内任务相互独立），**禁止**因"主 Agent 串行写更简单"而擅自降级为单线程逐任务。仅当运行环境**不允许**并行代理时才可串行退化，且须在 `impl-report.md` 中显式标注"串行退化执行"
- **输出**：
  - 服务端代码：落盘到宪章"技术方向"表格中记录的服务端代码路径（默认 `lua/`）
  - 客户端代码：落盘到客户端代码路径（默认 `client/`）
  - 实现报告：`xspec/changes/activity/impl-report.md`（或 SKILL 规定的位置）
  - 任务卡进度：SKILL 内部维护 `task.md` 的勾选状态

**任务推进节奏以 SKILL 为准**：命令层不硬性要求"逐任务完成后立即打勾"，也不介入 SKILL 的 DAG 调度与 write set 约束。若 SKILL 出现 blocked 项，SKILL 会自行上报，命令层按 SKILL 返回的阻塞列表提示用户。

### 替换 step 5（阶段验证） + step 8（最终验证）

通用阶段/最终验证依赖 `npm run build / lint / typecheck / test`，玄武双端不走此工具链。**本 profile 下替换为**：

- 阶段级验证：由 SKILL 的 checker 波次式执行，不在命令层重复调用
- 最终验证：读取 SKILL 输出的 `impl-report.md`，检查：
  - DAG 是否全部完成
  - 是否存在未消化的 blocked 项
  - client-impl-checker + server-impl-checker 终态是否 PASS

若发现异常项，先回给 SKILL 修复，不在命令层手工改代码。

### 跳过 step 7（执行集成任务）

单模块 `activity` 下，`integration.md` 已被 `/xf/task` 方言写成 N/A 版本（无 I 编号任务）。打印一行提示即可：

```
ℹ️ 单模块活动工程，无跨模块集成任务
```

### 保留 step 4（异常处理） + step 6（阶段性报告）

- step 4：设计文档与实际冲突、需要额外技术决策等情况，仍按通用规则（推荐 + 最多 3 个选项）让用户介入，不甩给 SKILL
- step 6：阶段性进度报告仍由命令层汇总输出，数据来源切换为 SKILL 的执行记录

### 覆写完成报告

```
✅ 实现完成（玄武活动模式）

模块：activity (xuanwu-activity)
代码输出（由 code-generator SKILL 执行）：
  服务端：lua/ 下 X 个文件（AwardLua）
  客户端：client/ 下 X 个文件（FGUI + TS）

任务进度：X/Y (100%)
DAG 波次：X 轮
自检结果：
  client-impl-checker：✅ 通过 / ❌ <未解决项>
  server-impl-checker：✅ 通过 / ❌ <未解决项>
  blocked 项：X 个

实现报告：xspec/changes/activity/impl-report.md

👉 下一步：/xf/archive 归档当前活动
```

### 理由

- 玄武双端代码栈（Lua + FGUI TS）与通用 Web 工具链差异显著，通用的 `npm run build/lint` 探测对本工程全程不适用
- code-generator SKILL 的 DAG + 波次 + write set + 自动修订机制是玄武实现阶段的核心能力，命令层逐任务循环会打碎并发
- 命令层仅负责接管异常沟通与阶段性报告，实现质量由 SKILL 内部 checker 保障
