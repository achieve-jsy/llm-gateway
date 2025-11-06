# xuanwu-activity × /xf/design 覆写

> 激活条件：`project_profile: xuanwu-activity`

## 覆写动作：跳过架构方向交互 + 替换文档生成为 arch-designer SKILL

玄武活动的架构范式已由 `xuanwu-activity/tech/bundle.yaml` 固化（客户端 FGUI + 服务端 AwardLua 的双端结构已是既定事实），不需要在命令层再让用户挑 A/B/C 方向。概要设计的产物生成完全交给 `arch-designer` SKILL 按 bundle 调度。

### 保留不动

- **前置条件**（含 `xf-gate.mjs --to design` 门禁硬停）：门禁未通过整条命令终止，SKILL 不会被调用
- **步骤 0（Profile 分支加载）**
- **步骤 1（加载上下文）**：仍要读 constitution + change.yaml + spec_*.md + PRODUCT.md
- **步骤 3（技术研究硬规则）**：发现技术不确定项必须自主调 `xf-research` subagent，不允许留 TODO。此硬规则对 SKILL 调用前后都生效
- **步骤 6（更新 change.yaml.phase = design）**

### 跳过 step 2.2（架构方向探索）

通用 step 2.2 要求向用户展示 2-3 个架构方向让其选择。在本 profile 下：

- 架构方向由 `arch-designer` SKILL 在读取 `.xflow/templates/xuanwu-activity/tech/bundle.yaml` 时按玄武双端范式固化，**不再向用户弹 A/B/C 选择题**
- 若 SKILL 内部发现必须由人拍板的方向选择，由 SKILL 自行发起询问，不走命令层

### 跳过 step 2.3（架构方案展示）

同样由 arch-designer SKILL 在生成 `hld.md` 时完成；命令层不再做独立的方案确认轮次。

### 保留 step 2.1（技术范围界定）+ step 2.4（宪章合规检查）

SKILL 调用**之前**仍向用户做一次轻量技术范围确认（起点/终点/不涉及），SKILL 调用**之后**对照 `constitution.md` 的"核心原则"+"技术方向"做一次宪章合规校验，发现冲突则提醒用户。这两步是防护网，SKILL 无法替代。

### 替换 step 5（生成概要设计文档）

通用 step 5 会按 `xf-manifest.mjs` 查出的模板（`hld.md` 单文件）由命令直接填充。**本 profile 下整段替换为调 SKILL**：

读取 [`.opencode/skills/arch-designer/SKILL.md`](.opencode/skills/arch-designer/SKILL.md)，按该 SKILL 指令执行：

```
arch-designer --profile xuanwu-activity --module activity
```

- **输入**：`xspec/changes/activity/spec_*.md`（req-extractor 已落盘）+ `xspec/constitution.md`
- **输出**：
  - 主索引：`xspec/changes/activity/hld.md`
  - 服务端领域文档：`xspec/changes/activity/hld_server_<id>.md`（含 `hld_server_freeze.md`）
  - 客户端领域文档：`xspec/changes/activity/hld_client_<id>.md`（含 `hld_client_freeze.md`）
  - 技术架构报告：`xspec/changes/activity/技术架构报告.md`
- bundle（`.xflow/templates/xuanwu-activity/tech/bundle.yaml`）决定具体生成哪些子文档，本方言不预设完整列表
- `hld_server_freeze.md` 与 `hld_client_freeze.md` 将作为下游 `/xf/detail` 的正式主输入
- **并行执行（强约束）**：**严格按照该 SKILL 内部定义的并行规则并行执行**——运行环境**允许**子 Agent 并行时**必须**按 bundle 文档粒度启动子 Agent 并行生成（服务端/客户端领域文档之间天然可并行），**禁止**因"主 Agent 串行更简单"而擅自降级。仅当运行环境**不允许**并行代理时才可串行退化，且须在返回结果中显式标注"串行退化执行"

### 更新 change.yaml phase

SKILL 调用和宪章合规校验完成后，将 `xspec/changes/change.yaml` 的 `phase` 字段更新为 `design`。

### 覆写完成报告

```
✅ 概要设计完成（玄武活动模式）

模块：activity (xuanwu-activity)
技术范围：<step 2.1 确认结果>
架构输出（由 arch-designer SKILL 生成）：
  主索引：xspec/changes/activity/hld.md
  服务端冻结：xspec/changes/activity/hld_server_freeze.md
  客户端冻结：xspec/changes/activity/hld_client_freeze.md
  其他领域文档：<SKILL 返回列表>
  架构报告：xspec/changes/activity/技术架构报告.md

宪章合规校验：✅ 通过 / ❌ 冲突列表
技术研究：<xf-research 课题数量或"无不确定项">

👉 下一步：执行 /xf/detail（由 design-detailer SKILL 产出详细设计与冻结文档）
```

### 理由

- 玄武活动的技术栈双端范式由 bundle 固化，命令层再让用户选架构方向是重复工
- arch-designer 作为玄武领域专属 SKILL 比通用命令更清楚玄武活动该怎么落架构
- 宪章合规校验和技术不确定项研究是 xflow 核心不变量，不因 profile 切换而松弛
