# xuanwu-mahjong-game × /xf/clarify 覆写

> 激活条件：`project_profile: xuanwu-mahjong-game`

## 覆写动作总览

**核心重排**：把 `req-extractor` SKILL 从"流程末尾填 spec"前移到"提问之前的准备阶段"，让 AI 基于 SKILL 的结构化产出（未明确项 + 提取报告）**有针对性**地向用户提问，而不是凭空揣测歧义。

对通用 `clarify.md` 的具体覆写动作：

| 通用步骤 | 覆写动词 | 新内容 |
| :--- | :--- | :--- |
| step 1.5 | 追加 | 打印 profile 提示行 + "模块锁定 mahjong-game" |
| — | **追加** | **新增 step 1.7：文档预检 + change.yaml + scaffold + SKILL 提取**（插入在 step 1.5 之后、step 2 之前） |
| step 2.0 | 替换 | 问题预算：SKILL 未明确项必答（≤8）+ AI 补充 ≤3 |
| step 2.1 | 替换 | 信号源从"AI 揣测"改为"SKILL 未明确项 + AI 补充"，按影响度排序 |
| step 2.2 | 替换 | 逐题循环：每题附带 SKILL 文档位置 + 推荐默认值 |
| step 2.3 | 跳过 | 模块影响确认题（原方言规则保留） |
| step 3 | 替换 | 确认总结展示"本次解决了哪些未明确项" |
| step 4 | 替换 | 答案事务式批量回写 spec_*.md；早停/"不知道"项保留 [待确认] |

---

## 执行指令

### 覆写 step 1.5（制品要点摘要）

- 正常读取 `constitution.md` 和 `PRODUCT.md`
- 追加打印一行明显的 profile 提示：
  ```
  🎮 当前 profile = xuanwu-mahjong-game，模块锁定：mahjong-game (xuanwu-mahjong-game)
  ```
- 模块结构摘要只描述"单模块 mahjong-game"，不列多模块

---

### 追加 step 1.7（文档预检 + 基础设施就位 + SKILL 提取）

> **位置**：在 step 1.5 之后、step 2 之前整段追加。本整段**不计入任何问答预算**。

#### 1.7.1 需求文档路径预检

1. 从 `xspec/modules/PRODUCT.md` 读取 "需求文档位置" 字段（由 `/xf/product` 命令的 Q3 写入）
2. 默认值 `doc/需求文档.md`；如果 PRODUCT.md 未显式写入，按此默认值
3. 用 Node `fs` 检查该路径是否存在：
   - **存在**（本地文件，或飞书 URL 可访问）→ 直接进入 1.7.2，不打扰用户
   - **不存在** → 向用户追问 1 次（本问不计入 step 2 的预算，因为这是前置环境问题）：
     ```
     需求文档路径 `<探测到的路径>` 不存在。请提供正确路径：
     - 本地 Markdown / Word 文件绝对路径
     - 飞书文档 URL
     ```
   - 用户回答后再次校验；仍不存在则终止命令并给出提示"需求文档缺失，无法进行 clarify"

#### 1.7.2 生成 change.yaml metadata

按 `.xflow/templates/_shared/change.yaml` 模板生成 `xspec/changes/change.yaml`（**绝对路径从仓库根起算，禁止省略 `xspec/` 前缀**），**强制**填入：

- `status: "in-progress"`，`phase: "clarify"`
- `project_profile: "xuanwu-mahjong-game"`（从 `xspec/constitution.md` 同步）
- `name: "<业务名>"`（来自用户命令行输入 / PRODUCT.md 业务名字段）
- `source_doc: "<需求文档路径>"`（来自 1.7.1 的校验结果）
- `affected_modules` 恒为单项：

  ```yaml
  affected_modules:
    - name: "mahjong-game"
      change_type: "added"
      module_type: "xuanwu-mahjong-game"
      summary: "<一句话描述本次麻将游戏>"
  ```

- `change_type` 在玄武麻将游戏模式下**默认为 `added`**（每个麻将游戏都是全新工程；即便复用仓库，上一个麻将游戏已归档清空，当前目录视为全新）

此时 `scope.include` / `scope.exclude` **暂不填**，等 step 3 用户确认后再回写。

#### 1.7.3 Scaffold mahjong-game 骨架

按 `xuanwu-mahjong-game` 类型执行 scaffold，产出 CATALOG.yaml 声明的空骨架文件：

```bash
node .xflow/scripts/xf-scaffold.mjs --module mahjong-game --type xuanwu-mahjong-game
```

此命令会生成 `xspec/changes/mahjong-game/` 下的全部骨架文件（spec.md 总纲 + `spec_*.md` 各切面占位）。骨架生成后**不要**用通用模板填充 spec 内容——留给下一步的 SKILL 负责。

#### 1.7.4 调用 req-extractor SKILL 填充 spec

读取 [`.opencode/skills/req-extractor/SKILL.md`](.opencode/skills/req-extractor/SKILL.md)，按该 SKILL 指令执行：

```
req-extractor --profile xuanwu-mahjong-game --input <change.yaml.source_doc> --module mahjong-game
```

- **输入**：`xspec/changes/change.yaml` 的 `source_doc` 指向的需求文档
- **输出**：`xspec/changes/mahjong-game/spec_*.md` 全部填充 + `xspec/changes/mahjong-game/需求提取总结报告.md`
- SKILL 的 bundle 驱动（`xwRef/xuanwu/xuanwu-mahjong-game/req/bundle.yaml`）决定生成哪些 `spec_<id>.md`，本方言不预设文件列表
- SKILL 自带问号收敛硬规则（`?（文档未说明）` ≤ 8 个），超出会自动重试 / 阻塞，本方言不干预
- **并行执行（强约束）**：**严格按照该 SKILL 内部定义的并行规则并行执行**——运行环境**允许**子 Agent 并行时**必须**按模板粒度启动子 Agent 并行提取（SKILL 规定"模板数>5 时强制并行"），**禁止**因"主 Agent 串行更简单"而擅自降级。仅当运行环境**不允许**并行代理时才可串行退化，且须在返回结果中显式标注"串行退化执行"

#### 1.7.5 加载 SKILL 产物供后续步骤使用

SKILL 执行完毕后，**主 Agent 必须读取两类产物供 step 2 使用**（读入内存即可，不展示给用户）：

1. `xspec/changes/mahjong-game/需求提取总结报告.md` 的 "**未明确项汇总**" 章节——记录每个 `?（文档未说明）` 的：
   - 所在文件与字段（如 `spec_reward.md` 的"奖励 ID"）
   - 文档原文出处（如果报告里有标注）
   - SKILL 的推荐默认值（如果报告里有标注）
2. 所有 `xspec/changes/mahjong-game/spec_*.md` 的完整内容，用于 step 2 补充 AI 业务级歧义题

---

### 替换 step 2.0（问题预算与筛选）

**本 profile 的问答预算**（相对通用规则整体放宽，但有硬约束）：

| 题源 | 上限 | 规则 |
| :--- | :--- | :--- |
| **SKILL 未明确项** | 全部（受 SKILL ≤8 硬规则间接约束） | **必须逐个问到答案或明确 "[待确认]"**，不允许悄悄吞掉 |
| **AI 补充题**（业务级歧义） | **≤ 3 题** | 走原 step 2.1 的筛选规则 |

**硬约束**（和通用规则一致）：
- 一次只展示一题
- 每题强形态：选择题（2–3 个互斥选项 + 推荐项）或极短答（≤15 字）
- 允许早停：用户回复"够了 / 跳过 / 后面自己定"等，立即进入 step 3（剩余项按 step 4 的异常路径落盘）
- 题内纠偏：用户答得含糊，可在同一题内追问 1 次，不计为新题

### 替换 step 2.1（问题筛选与优先级排序）

在发出第一题之前，构建问题队列：

#### 题源 1：SKILL 未明确项（优先级高）

从 step 1.7.5 读入的"未明确项汇总"中，**逐条转成一题**，按以下优先级排序：

1. **P0 — 影响实现分支**：如麻将游戏 ID、协议名、事件名、奖励 ID、外部分配编码
2. **P1 — 影响验收结果**：如发放条件、资格校验、数值公式
3. **P2 — 其他未明确项**：资源位、埋点 key、文案等

同一优先级内按 SKILL 报告中的出现顺序。

#### 题源 2：AI 补充题（优先级低，≤ 3 题）

AI 在 step 1.7.5 后审阅所有 `spec_*.md`，识别 SKILL **无法识别**的**业务级歧义**：

**允许问**：
- **功能范围歧义**：如文档列了 3 种玩法但没说本次是否全做
- **业务优先级**：多个功能点之间的 P0/P1/P2 排序
- **跨切面冲突**：两个 spec_*.md 之间业务逻辑表述不一致（SKILL 的跨文件一致性校验只查技术 ID 对齐，不查业务含义）

**禁止问**（和通用规则一致）：
- 技术实现细节（存储 / 错误处理 / 缓存等）→ 留给 design / detail
- 宪章或 PRODUCT.md 已有答案的
- 有合理默认值可覆盖的

#### 合并队列

`[P0 SKILL 未明确项, P1 SKILL 未明确项, P2 SKILL 未明确项, AI 补充题（≤3）]`

### 替换 step 2.2（逐题循环 + 必要上下文）

**每题必须携带 3 类上下文**（让用户一眼能决策，不需要凭空想答案）：

```
问题 <N>/<total>：<问题标题>

📍 位置：xspec/changes/mahjong-game/spec_<id>.md — "<字段名>"
📄 文档出处：<需求文档的章节/段落，若 SKILL 报告有标注则引用；没有则写"文档未提及">
💡 推荐默认值：<SKILL 或 AI 推断的合理默认值；没有则写"无">

【推荐：选项 X — 理由（1–2 句）】

| 选项 | 说明 |
| :--- | :--- |
| A    | <具体取值>（SKILL 推荐 / 业务常规） |
| B    | <备选取值> |
| C    | 留为 [待确认]，由设计阶段决定 |
```

- 对于数值 / 自由文本类问题（无枚举选项），退化为极短答：
  ```
  建议：<推荐答案>— 理由（1–2 句）
  请在 15 字内回复，或直接回复"是 / 采纳"接受建议。
  ```
- 每收到有效答案后，用一两句话工作记忆当前结论，再发下一题

### 跳过 step 2.3（模块影响确认）

**整段跳过**。模块结构是 profile 常量（恒为单模块 `mahjong-game`），无任何信息量。

---

### 替换 step 3（确认总结）

展示本次澄清**实际改动了什么**（区分 SKILL 已搞定 vs 本轮问答搞定）：

```markdown
## 需求确认摘要（xuanwu-mahjong-game 模式）

### 基本信息
- 业务名：<change.yaml.name>
- 需求文档：<source_doc>
- 模块：mahjong-game (xuanwu-mahjong-game) — added

### SKILL 提取结果
- 已生成 spec 文件：<N 个> spec_*.md
- SKILL 提取报告：xspec/changes/mahjong-game/需求提取总结报告.md
- 默认值替换：<SKILL 报告中的条目数>
- 未明确项（SKILL 发现）：<初始 N 个>

### 本轮澄清解决
- 已澄清未明确项：<M 个>（逐条列出：文件 — 字段 — 本轮答案）
- AI 补充题已答：<K 个>（逐条列出）

### 仍待确认（如有）
- <剩余 X 个未明确项，保留为 [待确认]，说明原因：用户早停 / 用户回答"不知道" / 留给设计阶段>

### 功能范围（做什么）
- <从 spec_*.md 汇总>

### 明确排除（不做什么）
- <若 AI 补充题涉及则列出>
```

用户确认后进入 step 4 的回写。

---

### 替换 step 4（答案事务式批量回写）

#### 4.1 批量回写 spec_*.md

对 step 2 收集到的所有答案，**一次性**在内存中整理成 patch，然后批量写入对应 `spec_*.md`：

1. 对 SKILL 未明确项的答案：替换对应位置的 `?（文档未说明）` 为实际答案
2. 对 AI 补充题的答案：按题目类型追加到对应章节（功能范围 / 业务优先级 / 跨切面说明）
3. 对早停 / 用户回答"不知道" / 用户主动选"C 选项（留为 [待确认]）" 的项：**不回写实际值**，把 `?（文档未说明）` 替换为 `[待确认]（原因：<早停 / 待设计阶段决定 / ...>）`，保持位置可追溯

**禁止**：
- 修改 SKILL 已经填好的非 `?（文档未说明）` 内容（防止二次污染）
- 跨文件级联替换（每题只改它声明的 `📍 位置`）

#### 4.2 回填 change.yaml 的 scope

用户在 step 3 确认的功能范围，回写到 `xspec/changes/change.yaml`：

- `scope.include`：从 spec_*.md 汇总出的功能点
- `scope.exclude`：AI 补充题中用户明确说"不做"的项

---

### 替换完成报告

```
✅ 需求澄清完成（玄武麻将游戏模式）

业务名：<name>
需求文档：<source_doc>
模块：mahjong-game (xuanwu-mahjong-game) — added

SKILL 提取：
  spec 产物：<N> 个 spec_*.md，已落盘到 xspec/changes/mahjong-game/
  提取报告：xspec/changes/mahjong-game/需求提取总结报告.md
  默认值替换：<SKILL 报告条目数>
  初始未明确项：<初始 N>

本轮澄清：
  已解决未明确项：<M>
  AI 补充题已答：<K>
  仍待确认：<X>（若 > 0，已保留为 [待确认] 标记，交由后续阶段处理）

自检结果：
  脚本检查：✅ 通过 / ❌ 未通过（列出未解决项）
  语义检查：
    [PASS/FAIL] <check-name> — <说明>

⚠️ 本阶段产出需要人工审查。请审查后**在终端**执行（不要把下面这行粘给 AI）：
  xflow approve
审批通过后即可执行下一步。

👉 下一步：执行 /xf/design 进行概要设计
```

---

## 设计理由

- **时序倒置**：让 AI 在**读过 SKILL 提取结果**之后才提问，信号从"AI 揣测"变为"结构化扫描"，每题都能附带文档出处和推荐默认值，问答效率和准确度都显著提升
- **预算放宽但不失控**：SKILL 的 `?（文档未说明）` ≤ 8 硬规则间接封顶了未明确项总数，加上 AI 补充题 ≤ 3，总预算 ≤ 11 题。配合"每题附带上下文 + 推荐值 + 允许早停"，用户实际负担接近"快速点选"
- **兜底保真**：早停 / "不知道" 不会悄悄吞掉未明确项，统一以 `[待确认]` 保留在 spec 里，完成报告显眼列出，确保信息不丢失
- **模块结构常量**：profile 下模块恒为单 `mahjong-game`，跳过模块影响确认题避免浪费预算
- **强制写 `project_profile` 到 change.yaml**：便于后续 archive 等命令在 change.yaml 丢失 constitution 上下文时仍能辨识
- **change.yaml / scaffold 前移**：给 SKILL 准备好输出目录和元数据基础，流程上下文最完整；万一 scaffold 和 SKILL bundle 的文件名不一致也能更早发现
