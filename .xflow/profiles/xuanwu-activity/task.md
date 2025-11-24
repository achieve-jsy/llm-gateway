# xuanwu-activity × /xf/task 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：替换任务拆解为 task-work SKILL + integration.md 极简化

玄武活动的任务拆解由 `task-work` SKILL 基于 `design_server_freeze.md` + `design_client_freeze.md` 产出 `task.md`。命令层不再做逐模块拆解和跨模块依赖对齐（单模块下无意义）。

### 保留不动

- **前置条件**（含 `xf-gate.mjs --to task` 门禁硬停）：门禁未通过整条命令终止，SKILL 不会被调用
- **步骤 0（Profile 分支加载）**
- **步骤 6（验证任务质量）**：SKILL 产出 `task.md` 后，仍按通用维度（可执行性/粒度/产出物/追溯性/并行标注/检查点/格式）做一次质量核对。如因玄武任务卡规范差异触发格式项误判，本方言不私自放行；由用户统一修 validate 规则
- **步骤 7（更新 change.yaml.phase = task）**

### 替换 step 2 + step 3（按模块拆解任务 + 生成模块级 task.md）

通用 step 2 + step 3 会直接按通用数字任务格式逐条拆解并写入 `task.md`。**本 profile 下整段替换为调 SKILL，并使用 xuanwu 专属语义 task id（如 `fgui-001`、`state-001`、`server-core-001`）**：

读取 [`.opencode/skills/task-work/SKILL.md`](.opencode/skills/task-work/SKILL.md)，按该 SKILL 指令执行：

- **输入**：
  - `xspec/changes/activity/design_server_freeze.md`
  - `xspec/changes/activity/design_client_freeze.md`
  - `xspec/changes/activity/spec_*.md`
  - `xspec/constitution.md`
  - 任务卡规范（`xwRef/xuanwu/xuanwu-activity/tech-design/rules/*.md`）
- **输出**：
  - 任务索引：`xspec/changes/activity/task.md`（模板：`.xflow/templates/xuanwu-activity/task.md`）
  - 其他附属任务文件（依 SKILL 内部工作流而定）
- **边界**（严格遵守 SKILL 约束）：
  - 冻结文档是唯一输入，任务内容只能来自冻结文档
  - 一个 task 最多改动 3 个文件；不得放宽该上限
  - 无真实路径的任务标 `blocked: <原因>`
  - 发现冻结文档有缺口时 SKILL 会**停止并报告**，不自行补需求
- **并行执行（强约束）**：**严格按照该 SKILL 内部定义的并行规则并行执行**——运行环境**允许**子 Agent 并行时**必须**按 SKILL 规定的粒度（典型为客户端/服务端双端或任务分组）启动子 Agent 并行拆解，**禁止**因"主 Agent 串行更简单"而擅自降级。仅当运行环境**不允许**并行代理时才可串行退化，且须在返回结果中显式标注"串行退化执行"

### 跳过 step 1（加载并分析所有设计文档）

SKILL 自身工作流里已经定义了要读的全套输入（冻结文档 + spec_*.md + 宪章 + 任务卡规范），命令层不再预读，避免重复劳动和"上下文加载"责任分散。

- 文件存在性由**前置条件**段兜底（不在本 step 中处理）
- `change.yaml` 这类流程元数据仍由命令层在 step 7 读写（SKILL 不管 phase 字段）

### 跳过 step 4（跨模块依赖对齐检查）

单模块 `activity` 下不存在跨模块依赖（`task-id@other-module` 语法无适用场景）。打印一行提示即可：

```
ℹ️ 单模块活动工程，跳过跨模块依赖对齐检查
```

### 替换 step 5（生成变更级 integration.md）

通用 step 5 会基于 `.xflow/templates/_shared/integration.md` 模板生成跨模块协调视图。单模块下该文件不承载信息，但为了保持归档完整性，仍写入一个**极简 N/A 版本**：

```markdown
# <change-id> 跨模块协调视图

> 项目 profile：xuanwu-activity（单模块活动工程）
>
> 本活动工程仅包含 `activity` 单模块，不存在跨模块依赖、跨模块集成任务或跨模块质量门禁。
> 任务编排与执行顺序完全收敛在 `xspec/changes/activity/task.md`。
> 本文件保留用于归档结构完整性，不承载业务内容。

## 模块任务索引

| 模块 | 类型 | 任务清单 |
| :--- | :--- | :--- |
| activity | xuanwu-activity | [task.md](activity/task.md) |

## 跨模块执行顺序

N/A（单模块）

## 跨模块集成任务

N/A（单模块）

## 质量门禁

由 task-work SKILL 在 `xspec/changes/activity/task.md` 产出的任务清单驱动，实现阶段由 code-generator SKILL 执行 client-impl-checker + server-impl-checker。
```

路径：`xspec/changes/integration.md`

### 更新 change.yaml phase

任务质量核对完成后，将 `xspec/changes/change.yaml` 的 `phase` 字段更新为 `task`。

### 覆写完成报告

```
✅ 任务拆解完成（玄武活动模式）

模块：activity (xuanwu-activity)
任务输出（由 task-work SKILL 生成）：
  机器入口：xspec/changes/activity/task-handoff.json
  冻结索引：xspec/changes/activity/task_client_freeze_index.md / task_server_freeze_index.md
  任务分片：xspec/changes/activity/task_client_part_*.md / task_server_part_*.md
  任务总数：X（P0: X / P1: X / P2: X）
  blocked 任务：X 个（<列出 blocked 原因摘要>）

跨模块协调视图：xspec/changes/integration.md（单模块，内容为 N/A）

任务质量核对：通过 / <列出未通过项>

👉 下一步：/xf/implement
```

### 理由

- task-work SKILL 比通用命令更懂玄武任务卡规范（服务端入口签名、模块引用、常量定义、禁止 API 等）
- 单模块下跨模块依赖无意义，跳过不是偷懒而是符合事实
- 保留 integration.md 的最小占位是为归档结构统一，避免归档后目录残缺引发审计困惑
