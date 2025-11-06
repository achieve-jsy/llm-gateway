# xuanwu-activity × /xf/detail 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：跳过方案探索 + 替换文档生成为 design-detailer SKILL

玄武活动的详细设计由 `design-detailer` SKILL 按 `xuanwu-activity/tech-design/bundle.yaml` 调度，承接 `hld_server_freeze.md` / `hld_client_freeze.md`，产出设计冻结文档与任务拆分文档。命令层不再做独立的"方案探索"交互，也不再按 manifest 单文件填充 `design.md`。

### 保留不动

- **前置条件**：`constitution.md` / `change.yaml.phase ∈ {design, detail}` / 各模块 `hld*.md` 与 `spec.md` 存在；**detail 命令本身无门禁**（门禁在下一关 `/xf/task` 前置条件）
- **步骤 0（Profile 分支加载）**
- **步骤 2（确保骨架文件存在）**：为 `activity` 模块 scaffold detail 阶段骨架文件
- **步骤 3.2（技术研究硬规则）**：发现不确定项必须自主调 `xf-research` subagent，不允许留 TODO
- **步骤 5（宪章合规校验）**：SKILL 调用后对照宪章做校验
- **步骤 5.5（产出自检强制）**：`xf-validate.mjs --phase detail` + `.xflow/validate-rules.yaml` 语义检查。如因玄武特有产物触发误报，本方言不私自跳检；若确需放行应回归修 validate-rules（由用户统一处理，不在本方言范围）
- **步骤 6（更新 change.yaml.phase = detail）**

### 跳过 step 1（加载上下文）

`design-detailer` SKILL 自身工作流里已经定义了输入集，命令层不再预读。

- 文件存在性由**前置条件**段兜底
- step 5（宪章合规校验）需要 `constitution.md` 时按需 Read，不做一次性预读
- `change.yaml` 仍由命令层在 step 6 读写

### 跳过 step 3.1 / step 3.3（方案探索的交互式决策）

通用 step 3 的方案探索轮次基于 hld.md 重新识别实现决策点。玄武模式下：

- `hld_server_freeze.md` / `hld_client_freeze.md` 已经是实现冻结文档，不需要再探索方案
- design-detailer SKILL 的职责是"承接、校验、补全和任务拆分，不能补发需求，也不能直接编写业务代码"（见其 SKILL.md 定位）
- 因此跳过 step 3.1（识别与分组设计决策点）和 step 3.3（确认决策汇总）；step 3.2 的研究硬规则保留

### 替换 step 4（逐模块生成技术文档）

通用 step 4 会按 manifest 查出 `design.md / data-model.md / contracts.md / evaluation.md` 等逐一填充。**本 profile 下整段替换为调 SKILL**：

读取 [`.opencode/skills/design-detailer/SKILL.md`](.opencode/skills/design-detailer/SKILL.md)，按该 SKILL 指令执行：

```
design-detailer --profile xuanwu-activity --module activity
```

- **输入**：`xspec/changes/activity/hld*.md`（arch-designer 输出）+ `xspec/changes/activity/spec_*.md`
- **输出**：
  - 主索引：`xspec/changes/activity/design.md`
  - 客户端冻结：`xspec/changes/activity/design_client_freeze.md`
  - 服务端冻结：`xspec/changes/activity/design_server_freeze.md`
  - 客户端任务拆分：`xspec/changes/activity/design_client_tasks.md`
  - 服务端任务拆分：`xspec/changes/activity/design_server_tasks.md`
  - 客户端/服务端自检：`design_client_self_check.md` / `design_server_self_check.md`
  - 设计报告：`xspec/changes/activity/design-report.md`
- bundle（`.xflow/templates/xuanwu-activity/tech-design/bundle.yaml`）决定具体文档集，本方言不预设完整列表
- `design_client_freeze.md` / `design_server_freeze.md` 是下游 `/xf/task` 与 `/xf/implement` 的正式主输入
- **并行执行（强约束）**：**严格按照该 SKILL 内部定义的并行规则并行执行**——运行环境**允许**子 Agent 并行时**必须**按 bundle 文档粒度启动子 Agent 并行生成（客户端/服务端冻结、任务拆分、自检等彼此独立的文档可并行），**禁止**因"主 Agent 串行更简单"而擅自降级。仅当运行环境**不允许**并行代理时才可串行退化，且须在返回结果中显式标注"串行退化执行"

### 跳过 step 5 中的"前后端交叉对齐"

通用 step 5 的"前后端 contracts / data-model 交叉对齐"假设有两个独立的 `web-frontend` + `web-backend` 模块。玄武单模块下前后端在 design-detailer 内部已对齐（同一 SKILL 一次性出双端冻结文档），因此跳过此节。保留"宪章合规校验"。

### 更新 change.yaml phase

所有文档生成和自检完成后，将 `xspec/changes/change.yaml` 的 `phase` 字段更新为 `detail`。

### 覆写完成报告

```
✅ 详细设计完成（玄武活动模式）

模块：activity (xuanwu-activity)
设计输出（由 design-detailer SKILL 生成）：
  主索引：xspec/changes/activity/design.md
  双端冻结：xspec/changes/activity/design_server_freeze.md / design_client_freeze.md
  双端任务：xspec/changes/activity/design_server_tasks.md / design_client_tasks.md
  双端自检：xspec/changes/activity/design_server_self_check.md / design_client_self_check.md
  设计报告：xspec/changes/activity/design-report.md

技术研究：<xf-research 课题数量或"无不确定项">
宪章合规校验：✅ 通过 / ❌ 冲突列表

自检结果：
  脚本检查（xf-validate --phase detail）：✅ 通过 / ❌ <未解决项>
  语义检查：
    [PASS/FAIL/SKIP] <check-name> — <说明>

⚠️ 本阶段产出需要人工审查。请审查后在终端执行：
  xflow approve
审批通过后即可执行 /xf/task。

👉 下一步：/xf/task（由 task-work SKILL 按冻结文档拆解任务）
```

### 理由

- 玄武设计冻结由 SKILL 统一管理，避免命令层重复做决策询问和文档填充
- 方案探索在 `arch-designer` 和 `design-detailer` 内部完成，命令层无价值重复
- 宪章合规和研究硬规则是安全网，不能松弛
