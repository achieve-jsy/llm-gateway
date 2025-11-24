详细设计：基于概要设计进行方案探索，为每个受影响模块生成 detail 阶段的技术文档（具体文件集由 CATALOG.yaml 决定，按模块类型动态展开）。

## 用户输入

```
$ARGUMENTS
```

如指定了模块名则对单个模块设计，否则对当前变更中所有受影响模块依次设计。

## 前置条件

- `xspec/constitution.md` 必须存在
- `xspec/changes/change.yaml` 必须存在，且 phase 为 `design` 或 `detail`
- 各模块的 `hld.md` 必须存在（由 `/xf/design` 生成）
- 各模块的 `spec.md` 必须存在
- **结构不变量校验（硬停）**：执行 `node .xflow/scripts/xf-check-invariants.mjs`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ 结构不变量违反：<脚本输出的报告>

  请按报告建议修复后重试。
  ```
- **phase 入口校验（硬停）**：执行 `node .xflow/scripts/xf-check-phase.mjs --command detail`。如退出码非 0，**必须立即终止本命令**，把脚本输出的诊断报告原样转发给用户后结束（不搜索、不分析、不试图绕过）：
  ```
  ❌ phase 入口不合法：<脚本输出的报告>

  请按报告建议处理后重试。
  ```

---

## 步骤 0：Profile 分支加载（始终先于其他步骤执行）

1. 执行 `node .xflow/scripts/xf-profile.mjs --value-only` 读取当前 `project_profile`
2. 显眼打印一行：`📌 project_profile = <profile>`
3. 如果 `.xflow/profiles/<profile>/detail.md` 存在，**读取该文件**并作为本次运行的行为覆写规则优先执行：
   - 指令 `终止整条命令` → 按方言固定输出打印并结束，**不执行下文任何步骤**
   - 指令 `跳过 <步骤>` → 执行到该步骤时直接跳过（例如 xuanwu-activity 下跳过步骤 3.1/3.3 的方案探索交互）
   - 指令 `替换 <步骤>` → 改用方言文件中的替代内容（例如 xuanwu-activity 下替换步骤 4 为调用 design-detailer SKILL）
   - 指令 `追加 <位置>` → 在该位置附加方言文件的内容
4. 方言文件不存在或 profile 为 `general` 时，按下文通用流程执行

> 原则：本文件只维护通用逻辑；领域方言统一在 `.xflow/profiles/<profile>/` 下管理。**禁止**在本文件硬编码 `if profile == xxx` 分支。
> 门禁边界：方言的覆写动词**仅作用于步骤 0 之后的业务步骤**。"前置条件"段落（含 `xf-check-invariants.mjs` 结构校验）对方言不可见、不可改——detail 命令本身无门禁，但前置条件的结构校验、`change.yaml.phase` 校验同样不允许方言绕过；方言本身也必须遵守 xflow 的结构不变量。

---

## 步骤 1：加载上下文

1. 读取 `xspec/changes/change.yaml` 获取所有模块及其 `module_type`
2. 对每个模块调用 `xf-manifest.mjs` 获取应加载/生成的文件清单：
   ```bash
   node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --all-phases --change-type <added|modified>
   ```
3. 读取每个模块的 `hld.md`（或 `hld.delta.md`）了解架构方向和技术选型
4. 读取每个模块的 `spec.md`（或 `spec.delta.md`）了解需求规范
5. 读取 `xspec/constitution.md`（技术选型必须符合宪章约束）
6. 如存在 `research.md`，读取课题索引表获取技术研究结论摘要；如需深入了解某课题，读取对应的 `research/` 子文件
7. 对变更模块：按 manifest 清单读取 `xspec/modules/<name>/` 下 phase=detail 的已有文档



## 步骤 2：确保骨架文件存在

**仅对 `change_type: added` 的模块**执行脚本补充缺失的模板文件（脚本按 CATALOG.yaml 决定复制哪些文件，自定义类型自动回退 `_default`）：

```bash
node .xflow/scripts/xf-scaffold.mjs --module <module-name> --type <module_type>
```

> ⚠️ **`change_type: modified` 的模块跳过本步**：它们的 delta 文件（`hld.delta.md` /
> `design.delta.md` 等）由后续步骤的 AI 直接撰写；完整基线在 `xspec/modules/<name>/`
> 是只读的，不要在 `changes/<name>/` 下铺完整 .md 副本。即便误传 `--change-type modified`，
> scaffold 也会 no-op 兜底，但请按命令编排层的职责正确分类，不要依赖兜底。

---

## 步骤 3：方案探索（交互式，逐模块）

在正式撰写详细设计之前，对每个模块进行方案探索——这是一个类似"设计级头脑风暴"的环节，帮助用户在多种实现方式中选择最优方案。

### 3.1 识别与分组设计决策点

基于 hld.md 的架构方案和待研究事项，识别需要做实现选择的关键点。**先过滤**：如果某个决策点的答案已被宪章、hld.md 或技术约束明确决定，则直接采用并在文档中记录理由，不纳入用户确认环节。只保留**真正有多个可行方案且各有取舍**的决策点，然后**按关联性分组**：

- **同一功能域**的决策归为一组（如数据存储相关的表结构 + 索引策略 + 缓存方案）
- **相互依赖**的决策归为一组（如 A 的选择会影响 B 的可选范围）
- **独立无关**的决策可以放在不同轮次

每轮展示一组关联决策（通常 2-4 个），遵循 CLAUDE.md 全局交互规则：**优先使用平台原生选择工具**（如 Cursor 的 AskQuestion），如决策需要展示对比表格则用文本格式：

```
🧭 [module-name] 方案探索（第 1/N 轮）

本轮主题：[分组主题，如"数据存储方案"]

决策 1：[问题描述]
  概要设计方向：[hld.md 中的初步方向]
  → 推荐 A：[方案] — [理由]
  → 备选 B：[方案]（最小化）

决策 2：[问题描述]
  → 推荐 A：[方案] — [理由]
  → 备选 B：[方案]

| 决策点 | 推荐方案 | 备选方案 | 推荐理由 |
| :--- | :--- | :--- | :--- |
| 决策1 | A | B（最小化） | [理由] |
| 决策2 | A | B | [理由] |

```

如某轮只有 1 个独立决策，直接单独呈现即可，不必强行凑批。

### 3.2 深入研究（自主决定，xf-research subagent）

**硬规则**：
- **禁止**将技术不确定项标记为「待研究」「TODO」「留到实现阶段」。详细设计阶段发现的技术问题**必须在本阶段解决**。
- **自主决定**是否调用 `xf-research` subagent——**不要询问用户**。如果存在不确定项，直接启动研究。
- 若无技术不确定项，跳过本步骤。

**触发研究的信号**（满足任一即应启动）：
- 涉及团队未使用过的新技术/新模式
- 存在 2 个以上有竞争力的实现方案
- 需要 benchmark 或 PoC 验证
- 可能影响长期维护性的架构级决策

**调用方式**：使用 `xf-research` subagent，传递以下上下文：
- **研究课题**：步骤 3.1 识别的设计决策点中需要深入研究的项
- **候选方案**：各候选方案的初步分析
- **架构方向**：模块的 `hld.md` 中的架构方案和技术选型
- **宪章约束**：`xspec/constitution.md` 中的技术约束
- **输出路径**：`xspec/changes/<module>/research.md`（索引）及 `research/` 子目录（深研究子文件）

### 3.3 确认决策汇总

所有决策点逐个确认后，整理汇总表做最终总览确认：

```
### 设计决策汇总

| 决策点 | 选择方案 | 关键理由 |
| :--- | :--- | :--- |
| [决策1] | 方案 A | [理由] |
| [决策2] | 方案 B | [理由] |

推荐：选项 A — 按上述决策推进详细设计

| 选项 | 说明 |
| :--- | :--- |
| A | 确认全部决策，开始生成文档 |
| B | 需要调整（请指出哪条决策需改） |

```

如汇总表超过 5 行，分批确认（每批 3-5 行）。

---

## 步骤 4：逐模块生成技术文档

基于确认的设计决策，为每个模块按 manifest 返回的 detail 阶段文件清单**逐一生成**：

```bash
# 查询该模块在 detail 阶段应生成的文件（含 required / optional / dynamic）
node .xflow/scripts/xf-manifest.mjs --module-type <module_type> --phase detail --change-type <added|modified>
```

清单中每个文件的填充规则如下：

### design.md / design.delta.md（所有类型 required）

- 新模块：填充完整详细设计
- 变更模块：生成 delta 文件
- **数据流转和关键流程必须使用 mermaid 图展示**
  - 架构图用 `graph TD`
  - 时序流程用 `sequenceDiagram`
  - 状态流转用 `stateDiagram-v2`
- 方案探索的决策及理由记录在 design.md 的「设计决策」部分

### data-model.md / data-model.delta.md（多数类型 required，_default 中 optional）

按模块类型填充：
- `web-frontend`：TypeScript 接口定义、实体关系
- `web-backend`：数据库表结构、ER 图（mermaid erDiagram）、索引设计、迁移方案
- `native-app`：本地存储模型、离线缓存结构
- `ai-agent`：Session / Memory / Tool Registry 等 Agent 数据结构
- `systems`：核心结构体、内存布局、序列化格式
- 其他/自定义类型：按 `_default` 模板结构填充

### contracts.md / contracts.delta.md（仅当 manifest 返回此文件时生成）

- 该文件是否需要、是 required 还是 optional，完全由 manifest 决定：
  - `web-frontend` / `web-backend`：required
  - `native-app`：optional（由 AI 判断是否有对外接口边界；无则按 `_shared/research.md` 的 `skip_when` 方式显式说明跳过）
  - `systems` / `ai-agent` / `_default`：manifest 不返回此文件，**不生成**
- 填充内容：
  - 前端：API 调用列表、请求/响应类型、错误处理
  - 后端：API 定义、参数、响应格式、错误码、安全策略

### evaluation.md / evaluation.delta.md（仅 ai-agent 类型，required）

manifest 对 `ai-agent` 在 detail 阶段会返回此文件。填充 Agent 评测方案：评测指标、数据集、对照基线、通过阈值、离线评估流程。

### research（如步骤 3 产出了新研究结果）

由 xf-research subagent 自动写入 research.md 索引及 research/ 子文件（深浅分治）。

> **统一原则**：不要凭记忆硬编码"该生成哪几个文件"——始终以 `xf-manifest.mjs` 返回的 detail 阶段清单为准。若 CATALOG.yaml 新增了类型专属文件，无需修改本命令即可被覆盖。

## 步骤 5：交叉对齐与宪章合规

如同时涉及前端和后端模块：

1. 检查后端 contracts 与前端 contracts 的接口定义一致性
2. 检查后端 data-model 与前端 data-model 的字段对应
3. 检查错误码定义一致
4. 如有不一致，列出差异并调整

**宪章合规校验**：

5. 技术选型是否符合宪章「核心原则」和「技术方向」
6. 如有违规，提醒用户并建议调整

## 步骤 5.5：产出自检（强制）

1. 执行脚本验证：
   ```bash
   node .xflow/scripts/xf-validate.mjs --phase detail
   ```

2. 如果 `pass: false`：
   - 逐个修复 `failures` 中的问题
   - 重新执行脚本验证
   - 最多修复 3 轮；仍不通过则在完成报告中列出未解决项及原因

3. 脚本通过后，读取 `.xflow/validate-rules.yaml` 中 `detail` phase 的 `semantic_checks` 清单，
   逐项执行语义检查。有 `skip_when` 条件的先判断是否跳过。
   输出结构化逐项报告：
   ```
   [PASS/FAIL/SKIP] <name> — <具体说明>
   ```
   发现 FAIL 则修复后重新检查该项。

4. 全部通过后继续更新元数据

## 步骤 6：更新元数据

将 `xspec/changes/change.yaml` 的 `phase` 更新为 `detail`

---

## Delta 文件格式（技术文档）

```markdown
# [模块名] 技术设计变更 — [变更ID]

> 基线：xspec/modules/<name>/<file>.md (v<version>)

## 变更摘要
[一句话描述技术变更的核心内容]

## 设计决策变更
[如有新增或修改的设计决策]

## 架构变更
[用 mermaid 图描述前后对比]

## 数据模型变更
### 新增
### 修改
### 删除

## 接口变更
### 新增接口
### 修改接口
### 废弃接口
```

## 输出产物

对每个受影响模块，产物 = manifest 查询结果（`--phase detail --change-type <added|modified>` 的 required 文件 + 按需生成的 optional 文件 + SKILL/外部工具产出的 dynamic 文件）。典型情况：

- `web-backend` / `web-frontend`：`design.md`、`data-model.md`、`contracts.md`
- `native-app`：`design.md`、`data-model.md`（+ 可选 `contracts.md`）
- `ai-agent`：`design.md`、`data-model.md`、`evaluation.md`
- `systems`：`design.md`、`data-model.md`
- 自定义类型（`_default`）：`design.md`（+ 可选 `data-model.md`）

此外可选产物：`research.md` + `research/*.md`（研究索引及深研究子文件，与 module_type 无关）。

## 完成报告

```
✅ 详细设计完成

变更编号：<change-id>
设计决策：
  - [决策1] → [选择的方案]
  - [决策2] → [选择的方案]

生成文件（按模块 manifest 展开，下列为示例）：
  - xspec/changes/<module>/design.md
  - xspec/changes/<module>/data-model.md
  - xspec/changes/<module>/contracts.md（如 manifest 含此项）
  - xspec/changes/<module>/evaluation.md（如 manifest 含此项，ai-agent 专属）
  - xspec/changes/<module>/research.md（如步骤 3 产出）

前后端对齐校验：通过 / [列出问题]
宪章合规校验：通过 / [列出问题]

自检结果：
  脚本检查：✅ 通过 / ❌ 未通过（列出未解决项）
  语义检查：
    [PASS/FAIL/SKIP] <check-name> — <说明>

⚠️ 本阶段产出需要人工审查。请审查后在终端执行：
  xflow approve
审批通过后即可执行下一步。

👉 下一步：执行 /xf/task 进行任务拆解
```

## 行为规则

- 需要用户做选择时，遵循 CLAUDE.md 全局交互规则（选项式 + 推荐 + 最小化 + 优先使用平台原生选择工具）
- 方案探索在前、文档撰写在后——先确认方向再落笔
- **方案探索按关联性分组**，每轮讨论一组相关决策（2-4 个），每个决策的候选方案不超过 3 个；减少确认轮次，避免逐个碎片化询问
- 技术方案必须基于已确认的需求和概要设计，不凭空创造功能
- 关键设计决策必须记录理由和备选方案
- **必须使用 mermaid 图表达数据流转和关键流程**
- 前后端接口必须交叉对齐
- 数据模型变更要考虑迁移影响
- **详细设计阶段不允许留技术 TODO**：发现不确定项必须当场用 `xf-research` subagent 研究解决，不得标记「待确认」「留到实现」
- research 调用由 agent **自主决定**，不询问用户；subagent 结果按深浅分治写入 research.md 索引及 research/ 子文件
- 所有输出使用中文
