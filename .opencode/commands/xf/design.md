对已澄清的需求进行概要设计（High-Level Design），定义技术范围、架构方案和技术选型。

## 用户输入

```
$ARGUMENTS
```

如指定了模块名则对单个模块进行概要设计，否则对当前变更中所有受影响模块依次设计。

## 前置条件

- `xspec/changes/change.yaml` 必须存在（含 scope 和 affected_modules）
- `xspec/changes/change.yaml` 的 phase 为 `clarify` 或 `brainstorm` 或 `design`（允许重入）
- `xspec/changes/<module>/spec.md` 至少一个模块的需求规范已存在
- `xspec/constitution.md` 必须存在
- **结构不变量校验（硬停）**：执行 `node .xflow/scripts/xf-check-invariants.mjs`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ 结构不变量违反：<脚本输出的报告>

  请按报告建议修复后重试。
  ```
- **phase 入口校验（硬停）**：执行 `node .xflow/scripts/xf-check-phase.mjs --command design`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ phase 入口不合法：<脚本输出的报告>

  请按报告建议处理后重试。
  ```
- **门禁检查（硬停）**：执行 `node .xflow/scripts/xf-gate.mjs --to design`。如返回 `pass:false`，**必须立即终止本命令**，仅输出以下固定提示后结束（不搜索、不分析、不提供替代方案、不教用户手动写 `.gates/` 文件）：
  ```
  ⛔ 门禁未通过：<脚本返回的 reason 原文>

  请在终端执行：xflow approve
  审批通过后重新执行 /xf/design
  ```

---

## 步骤 0：Profile 分支加载（始终先于其他步骤执行）

1. 执行 `node .xflow/scripts/xf-profile.mjs --value-only` 读取当前 `project_profile`
2. 显眼打印一行：`📌 project_profile = <profile>`
3. 如果 `.xflow/profiles/<profile>/design.md` 存在，**读取该文件**并作为本次运行的行为覆写规则优先执行：
   - 指令 `终止整条命令` → 按方言固定输出打印并结束，**不执行下文任何步骤**
   - 指令 `跳过 <步骤>` → 执行到该步骤时直接跳过（例如 xuanwu-activity 下跳过步骤 2.2 的架构方向探索）
   - 指令 `替换 <步骤>` → 改用方言文件中的替代内容（例如 xuanwu-activity 下替换步骤 5 为调用 arch-designer SKILL）
   - 指令 `追加 <位置>` → 在该位置附加方言文件的内容
4. 方言文件不存在或 profile 为 `general` 时，按下文通用流程执行

> 原则：本文件只维护通用逻辑；领域方言统一在 `.xflow/profiles/<profile>/` 下管理。**禁止**在本文件硬编码 `if profile == xxx` 分支。
> 门禁边界：方言的覆写动词**仅作用于步骤 0 之后的业务步骤**。"前置条件"段落（含 `xf-check-invariants.mjs` 结构校验与 `xf-gate.mjs` 门禁硬停）对方言不可见、不可改——硬停未通过整条命令已在此之上终止，SKILL 不会被调用。方言本身也必须遵守 xflow 的结构不变量。

---

## 步骤 1：加载上下文

1. 读取 `xspec/constitution.md` 了解核心原则和技术方向
2. 读取 `xspec/changes/change.yaml` 获取受影响模块列表和功能范围边界（scope）
3. 读取各模块的 `xspec/changes/<module>/spec.md` 或 `spec.delta.md` 了解模块级需求规范
4. 对于变更模块：读取 `xspec/modules/<name>/` 下已有文档了解现状
5. 读取 `xspec/modules/PRODUCT.md` 了解模块依赖关系



## 步骤 2：概要设计（逐模块）

对每个受影响模块，执行以下设计流程：

### 2.1 技术范围界定

向用户展示分析结果，获得确认：

```
📐 [module-name] 概要设计

技术范围：
  起点：[输入来源]
  终点：[输出目标]
  解决的问题：[一句话总结]

技术边界：
  上游：[依赖方]
  下游：[被依赖方]
  不涉及：[明确排除]

推荐：选项 A — 范围界定准确，继续设计

| 选项 | 说明 |
| :--- | :--- |
| A | 范围准确，进入架构方向探索 |
| B | 需要调整（请说明） |

```

### 2.2 架构方向探索（轻量头脑风暴，1 轮）

在确定架构方案之前，先快速探索可选的架构方向。**聚焦架构层面，不涉及实现细节**。

基于技术范围和宪章约束，分析可行的架构方向。如果宪章或需求特征已明确指向唯一合理方向（如宪章要求单体架构、或需求规模决定了技术路线），直接采用并说明理由，跳过本步骤。只有存在多个可行方向时，才列出 2-3 个选项（遵循 CLAUDE.md 全局交互规则：推荐 + 最小化 + 选项表）：

```
🧭 [module-name] 架构方向探索

推荐：选项 A — [1-2 句推荐理由，基于宪章约束和需求特征]

| 选项 | 方向 | 核心思路 | 风险 |
| :--- | :--- | :--- | :--- |
| A | [方向1] | [一句话] | [主要风险] |
| B | [方向2]（最小化） | [一句话，最简单直接的路径] | [主要风险] |
| C | [方向3，如有] | [一句话] | [主要风险] |

```

用户确认后，将选定方向和理由记录到 hld.md 的「架构方向探索」部分。

**注意**：这一步是快速定方向，不深入对比细节。如果某个方向涉及技术不确定性，步骤 3 会自主启动研究解决。

### 2.3 架构方案

基于用户选定的架构方向，展开概要级架构设计：

- 整体架构图（mermaid flowchart）
- 核心流程图（mermaid sequence diagram）
- 技术选型建议（框架、数据库、中间件等）

展示给用户确认（遵循全局交互规则：选项表 + 推荐 + 最小化）：

```
推荐：选项 A — 按上述方案推进

| 选项 | 说明 |
| :--- | :--- |
| A | 确认上述架构方案，进入下一步 |
| B | 需要调整（请简述调整方向） |
| C | 简化架构，采用最直接的实现路径（最小化） |

```

### 2.4 宪章合规检查

对照 `constitution.md` 检查：

- 技术选型是否符合宪章约束
- 架构方案是否满足非功能性要求
- 如有冲突，提出替代方案

## 步骤 3：技术研究（自主决定，xf-research subagent）

分析设计过程中是否存在**技术不确定项**（API 可用性、算法细节、性能特征、兼容性等）。

**硬规则**：
- **禁止**将技术不确定项标记为「待研究」「TODO」「留到实现阶段」。设计阶段发现的技术问题**必须在设计阶段解决**，这是 xflow「设计先行、研究先行」的核心原则。
- **自主决定**是否调用 `xf-research` subagent——**不要询问用户**「是否需要研究」。如果存在不确定项，直接启动研究。
- 若判断无技术不确定项，跳过本步骤即可。

**触发研究的信号**（满足任一即应启动）：
- 涉及团队未使用过的 API / 框架 / 技术模式
- 存在 2 个以上有竞争力的技术选型
- 需要 benchmark 或 PoC 验证
- 特定平台/版本的 API 可用性或行为不确定

**调用方式**：使用 `xf-research` subagent，传递以下上下文：
- **研究课题**：识别出的技术不确定项（含影响范围）
- **模块信息**：模块名称、类型
- **宪章约束**：`xspec/constitution.md` 中的技术约束
- **已有决策**：步骤 2 中已确认的技术选型
- **输出路径**：`xspec/changes/<module>/research.md`（索引）及 `research/` 子目录（深研究子文件）

subagent 完成后，读取 `research.md` 索引表中的结论摘要，**基于结论调整概要设计**（如结论影响架构方向，须更新 hld.md 相应部分）。

## 步骤 5：生成概要设计文档

该阶段的文件清单由 CATALOG.yaml 驱动（`phase=design`）。查询当前模块应生成的文件：

```bash
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --phase design --change-type <added|modified>
```

对当前所有注册类型，design 阶段清单均为 `hld.md`（或 `hld.delta.md`）。按 `change_type` 区分输出：

**新增模块**（`change_type: added`）：
1. 模板路径：`.xflow/templates/<resolved_type>/hld.md`，其中 `resolved_type` 由 manifest 返回（自定义类型自动回退 `_default`）
2. 填充实际设计内容
3. 写入 `xspec/changes/<module>/hld.md`

**变更模块**（`change_type: modified`）：
1. 读取 `xspec/modules/<name>/hld.md` 和 `module.yaml` 了解现有概要设计及版本
2. 基于现有设计和本次变更需求，生成 `xspec/changes/<module>/hld.delta.md`（格式见下方）

### hld.delta.md 格式

```markdown
# [模块名] 概要设计变更 — [变更ID]

> 基线：xspec/modules/<name>/hld.md (v<version>)

## 变更摘要
[一句话描述架构变更核心内容]

## 架构变更
[用 mermaid 图展示变更部分]

## 技术选型变更
[原选型] → [新选型]，理由：[说明]

## 新增组件
[新增的架构组件及职责]
```

## 步骤 6：更新 change.yaml

```yaml
phase: "design"
```

---

## 输出产物

- `xspec/changes/<module>/hld.md`（新增模块的完整概要设计）
- `xspec/changes/<module>/hld.delta.md`（变更模块的增量概要设计变更）
- `xspec/changes/<module>/research.md`（研究索引，如进行了技术研究）
- `xspec/changes/<module>/research/*.md`（深研究子文件，如有）
- `xspec/changes/change.yaml`（phase 更新）

## 完成报告

```
✅ 概要设计完成

变更：<change-id>
完成模块：X 个
  - <module-a>：hld.md ✅ | research.md ✅
  - <module-b>：hld.md ✅ | research.md ⏭️ (跳过)

技术研究：Y 个课题（均已完成）/ 无需研究1. 

👉 下一步：执行 /xf/detail 进行详细设计
```

## 行为规则

- 需要用户做选择时，遵循 CLAUDE.md 全局交互规则（选项式 + 推荐 + 最小化 + 优先使用平台原生选择工具）
- 架构方向探索聚焦「架构层面」：单体/微服务、同步/异步、数据存储范式等，不涉及具体库和实现方案
- 概要设计聚焦「技术范围、架构方案、技术选型」，不涉及详细实现
- 所有架构和流程使用 mermaid 图表展示
- 技术选型必须对照宪章合规
- **设计阶段不允许留技术 TODO**：发现不确定项必须当场用 `xf-research` subagent 研究解决，不得标记「待确认」「留到实现」
- research 调用由 agent **自主决定**，不询问用户；subagent 结果按深浅分治写入 research.md 索引及 research/ 子文件
- 文件清单与模板路径以 `xf-manifest.mjs` 输出为准；自定义 `module_type` 无专属模板时由 manifest 自动回退 `_default`
- 所有输出使用中文
