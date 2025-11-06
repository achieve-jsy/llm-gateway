---
name: req-extractor
description: 从需求文档中按 profile 的 req bundle.yaml 严格提取结构化需求，输出标准化结果文件和提取报告。支持 --templates 子集提取与依赖自动补齐。

disable-model-invocation: true
---

# req-extractor

req 阶段技能。从需求文档（本地 Markdown / Word / 飞书文档）中按领域规范模板严格提取结构化需求，输出多份标准化 Markdown 文件和提取总结报告，为后续开发阶段提供统一输入。

## 1. 定位与边界

**职责**：

- 将非结构化需求文档转换为结构化文档集合
- 按 `bundle.yaml` 模板提取 bundle 定义的全部标准化文档
- 根据运行环境决定子 Agent 并行执行或主 Agent 串行退化
- 生成标准化结果文件与 `需求提取总结报告.md`

**不职责**：

- 不自行修改模板结构；输出结构只能来自 `publish_template`
- 不在文档未明确时随意猜测业务设定
- 不把提取中间结果直接作为最终文件落盘

**核心价值**：

- 结构化提取：按 bundle 定义提取标准化文档
- 并行执行：模板数量大于 5 时强制使用子 Agent 并行执行
- 质量控制：未明确项不超过 8 个、跨文件一致性 100%、无占位符残留
- 双模板机制：`extract_template`（提取）+ `publish_template`（落盘）

**核心约定**：

- profile 的模板集合由 `xwRef/xuanwu/<profile>/req/bundle.yaml` 驱动
- bundle 允许添加 YAML 注释，解析器会自动忽略
- bundle 的通用解析与调度规则统一委托给 `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`

## 2. 输入与输出

### 输入

| 来源                 | 路径 / 形式                                                        |
| -------------------- | ------------------------------------------------------------------ |
| 需求文档             | 本地 Markdown / Word 文件路径，或飞书文档 URL                      |
| Req Bundle           | `xwRef/xuanwu/<profile>/req/bundle.yaml`                |
| 通用调度规则         | `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`               |
| 单文件校验与回退规则 | `.claude/skills/xuanwu/req-extractor/references/req-validation.md` |
| 报告模板             | `.claude/skills/xuanwu/req-extractor/assets/需求提取总结报告.md`   |

### 输出

| 产物           | 路径                                                                                                          |
| -------------- | ------------------------------------------------------------------------------------------------------------- |
| 结构化需求文档 | `xspec/changes/<module>/` 下由 `bundle.yaml` `docs[].output` 定义的结果文件（通常为 `spec_<template_id>.md`） |
| 提取总结报告   | `xspec/changes/<module>/需求提取总结报告.md`                                                                  |

### 执行口径

```text
req-extractor --profile <profile> --input <doc_or_url> --module <module>
```

可选参数：

- `--templates <...>`：指定子集提取
- `--profile <...>`：显式覆盖 profile

参数组合、依赖补齐与选择规则统一遵循 `bundle-orchestrator`。

### 返回结果必须包含

1. 输出目录路径
2. 提取报告路径
3. 本次实际生成的模板列表
4. 因依赖自动补齐的模板列表
5. 默认值替换数量
6. 未明确项数量
7. 执行方式说明（子 Agent 并行执行 / 主 Agent 串行退化）

## 3. 工作模式

**独立文件模式**（唯一模式）：

- 输入：本地文件路径 / 飞书文档 URL
- 输出目录：`xspec/changes/<module>/`
- 结果文件命名：`spec_<template_id>.md`（如 `spec_overview.md`、`spec_mechanic.md`）

通用执行模式、整套 / 子集示例统一遵循 `bundle-orchestrator`；本 Skill 仅强调 req 阶段的输入输出差异。

## 4. 角色职责

| 维度     | 主 Agent                                                                                   | 模板执行单元 / 子 Agent                                                   |
| -------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| **触发** | Skill 入口                                                                                 | 主 Agent 按模板派发                                                       |
| **输入** | 输入文档、bundle.yaml、报告模板                                                            | 当前模板对应的 `extract_template`、`publish_template`、输入文档、校验规则 |
| **职责** | 确认输入、预处理、解析 bundle、按 phase 调度模板执行单元、汇总结果、生成报告、执行阶段校验 | 独立提取单个模板、独立校验、独立落盘、上报默认值替换与未明确项            |
| **禁止** | 不预读所有模板正文；不跳过 bundle 解析与依赖补齐                                           | 不读取其他模板或无关文件；不把中间提取结果直接当最终输出                  |

## 5. 执行流程

不允许跳步。通用 phase 调度、依赖补齐与失败回退统一遵循 `bundle-orchestrator`。

| Step | 动作                                                                                                     | 规则 / 关键约束                                                                                                               |
| ---- | -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| 1    | 确认输入信息：获取需求文档路径 / URL，确认 `--module` 输出目录，接收可选参数 `--profile`、`--templates`  | 输入来源必须可解析到单一文档；输出目录固定为 `xspec/changes/<module>/`                                                        |
| 2    | 获取路径信息：校验输入文档路径 / URL 有效性，确定输出目录，若输出目录不存在则自动创建，解析 profile      | 若用户未指定 `--profile`，自动分析文档关键词匹配领域，并向用户确认后使用                                                      |
| 3    | 预处理：清理输出目录旧 `spec_*.md` 结果文件；读取需求文档                                                | 本地 Markdown / Word 直接读取；飞书 URL 委托 `xw-feishu-reader` skill 拉取为本地 Markdown 后读取                              |
| 4    | 加载领域配置：读取 req bundle，按 `bundle-orchestrator` 解析 bundle、选择模板、补齐依赖并计算 phase 顺序 | 使用 `docs[].extract_template` / `publish_template` 双模板；校验模板列表、validation.checklists / rules、输出约定与模板存在性 |
| 5    | 提取执行：按模板粒度启动独立执行单元；环境允许时并行子 Agent 执行，不允许时主 Agent 串行退化             | 无论并行还是串行，每个模板都必须独立提取、独立校验、独立落盘；每完成一个模板都输出 `✅ <文件名> 已完成 (n/total)`             |
| 6    | 汇总与生成报告：汇总执行结果，扫描最终输出文件，生成 `需求提取总结报告.md`                               | 报告必须包含基本信息、生成结果总览、默认值替换汇总、未明确项汇总、校验结果                                                    |
| 7    | 结果校验：执行机器自动化校验与人工验收清单                                                               | 机器校验失败自动重写受影响文件，最多重试 3 次；3 次后仍失败则列出阻塞项并停止推进                                             |
| 8    | 阶段完成检查清单                                                                                         | 必须满足文档齐全、报告生成、默认值替换完整、未明确项不超过 8 个、跨文件一致、无占位符                                         |
| 9    | 完成：输出结果文件列表与报告路径                                                                         | 返回结果必须包含输出目录、报告路径、模板列表、依赖补齐列表、默认值替换数、未明确项数、执行方式                                |

### Step 3 补充说明：预处理

1. 清理输出目录：删除旧 `spec_*.md` 结果文件，保留非结果文件
2. 读取需求文档：
   - 本地 Markdown / Word 文件：直接读取，Word 自动转 Markdown
   - 飞书文档 URL：委托 `xw-feishu-reader` skill，通过 `feishu_read.js` 脚本拉取并输出为本地 Markdown 文件后读取

### Step 5 补充说明：提取执行

- 若当前运行环境允许并行代理 / 子 Agent，则必须按模板粒度启动子 Agent 并行执行
- 若当前运行环境不允许并行代理 / 子 Agent，则退化为主 Agent 串行逐个执行
- 无论并行还是串行，均必须保持每个模板独立提取、独立校验、独立落盘的执行边界

每个模板对应一个独立执行单元：

1. 读取 `extract_template`
2. 严格按提取模板结构从需求文档提取内容
3. 文档未明确内容先按模板约定、领域默认值、同类玩法 / 活动与上下文进行推理补全；只有会影响实现、联调或验收的关键项才写 `?（文档未说明）`
4. 先形成“提取中间结果”，该结果只允许作为映射输入，不允许直接落盘为最终文件
5. 提取完成后读取 `publish_template`
6. 按输出模板结构重新组织最终结果文件并保存到输出目录
7. 落盘前必须通过“结构校验 + 问号收敛校验”；详细规则、失败回退与重试方式见 `references/req-validation.md`
8. 上报默认值替换记录与未明确项

调度、phase 顺序与失败回退统一遵循 `bundle-orchestrator`。

### Step 6 补充说明：报告内容

基于内置模板生成 `需求提取总结报告.md`，包含：

- 基本信息（源文档、profile、输出目录、生成时间）
- 生成结果总览（文件列表、成功 / 失败情况）
- 默认值替换汇总（逐项记录）
- 未明确项汇总（逐项记录）
- 校验结果

### Step 7 补充说明：结果校验

**机器自动化校验**（强制执行）：

1. 文件完整性检查：bundle.yaml 定义的全部文档 + 提取报告是否齐全
2. 模板结构检查：标题层级、表格格式是否符合 `publish_template`
3. 占位符检查：禁止出现 `{活动名}`、`{...}` 等未替换占位符
4. 跨文件一致性检查：协议 ID、配置键名、奖励 ID 跨文档一致
5. 问号收敛检查：`?（文档未说明）` 数量不超过 8 个

**人工验收清单**：

1. 内容合法性：业务逻辑合理、数值正确
2. 跨文件一致性：人工复核关键信息

**校验失败处理**：

- 机器校验失败：自动重写受影响文件，最多重试 3 次
- 3 次后仍失败：列出阻塞项，停止推进

## 6. 阶段完成条件

**强制执行检查清单**：

- [ ] bundle.yaml 定义的全部文档生成
- [ ] `需求提取总结报告.md` 提取报告已生成
- [ ] 默认值替换记录完整
- [ ] 未明确项数量不超过 8 个
- [ ] 跨文件一致性检查通过
- [ ] 无未替换占位符

## 7. 输出约定

### 统一输出结构

输出目录 `xspec/changes/<module>/` 下必须包含：

1. 所有模板对应的结果文件，文件名以 `bundle.yaml` 的 `output` 字段为准（格式：`spec_<template_id>.md`，如 `spec_overview.md`、`spec_mechanic.md`）
2. `需求提取总结报告.md` 提取总结报告

### 返回结果必须包含

1. 输出目录路径
2. 提取报告路径
3. 实际生成模板列表
4. 自动补齐依赖列表
5. 默认值替换数量
6. 未明确项数量
7. 执行方式说明（子 Agent 并行执行 / 主 Agent 串行退化）

## 8. 执行单元规范

1. **只读对应模板**：不读取其他模板或无关文件
2. **严格保留模板结构**：标题层级、表格、检查项完全沿用模板
3. **结构判定以 `publish_template` 为唯一来源**：若 checklist / rules 与 `publish_template` 冲突，以 `publish_template` 为准
4. **优先推理默认值**：文档未明确但可依据模板规则、领域默认值、同类方案和上下文稳定推断的内容，应直接补全为默认值
5. **关键未知项才保留问号**：仅限活动 ID、外部分配编码、资源位、事件名、奖励 ID、协议名、会影响实现分支或验收结果的关键字段，才写 `?（文档未说明）`
6. **禁止把提取模板当输出模板**：最终文件必须以 `publish_template` 的标题和章节结构为准
7. **落盘前必须做检测**：结构校验和问号收敛校验不过，不允许产出最终文件
8. **不写抽象描述**：禁止出现“按规则处理”“见其他文档”等模糊描述，必须写具体口径
9. **输出即最终文件**：必须保存完整 Markdown，不只返回摘要
10. **无无效占位符**：禁止保留 `{活动名}`、`{...}` 等模板示例占位符
11. **允许环境退化**：若运行环境允许并行代理 / 子 Agent，则必须使用子 Agent 执行；仅在运行环境不允许时，才可由主 Agent 串行执行，但仍须保持逐文件独立提取与独立校验，并在最终结果中说明本次为串行退化执行

## 9. 触发与选择规则

### 自动整套提取（默认执行全模板提取）

触发关键词：需求提取、分析需求、批量提取、extract requirements

### 指定子集提取（仅执行命中模板）

触发关键词：提取步骤、提取 UI、提取算法、提取协议、提取配置、extract steps、extract ui、extract algorithm

req 侧仅补充业务触发词；默认整套 / 指定子集、匹配、依赖补齐与 phase 调度统一遵循 `bundle-orchestrator`。若语义不明确，先询问用户需要提取的模板类型。

## 10. bundle 读取规则

- bundle 文件固定为 `xwRef/xuanwu/<profile>/req/bundle.yaml`
- 路径字段均相对 bundle 所在目录解析
- 一个 bundle 负责一个 profile 的需求拆解模板集合
- `docs[].output` 可以与模板文件名不同，用于兼容既有校验规则
- `validation.checklists` 和 `validation.rules` 作为该 profile 的校验资产入口

详细字段规范见 [`references/req-workflow.md`](references/req-workflow.md)。

## 11. 重要约束

- 主 Agent 不预读所有模板，只读取 `bundle.yaml` 或回退读取宪章配置
- 模板是唯一输出结构来源，不得自行修改结构
- 并行代理 / 子 Agent 必须并行执行
- **性能优化**：模板数量大于 5 时强制使用并行执行，减少总耗时
- 必须先清理输出目录再开始生成
- 提取报告是必需产物，不是可选附件
- 不得猜测、补充文档未明确的业务设定

## 12. 资源索引

- bundle 协议与输出契约：[`references/req-workflow.md`](/Users/shuwanchun/git/x-flow/.claude/skills/xuanwu/req-extractor/references/req-workflow.md)
- 单文件校验与失败回退：[`references/req-validation.md`](/Users/shuwanchun/git/x-flow/.claude/skills/xuanwu/req-extractor/references/req-validation.md)
- 报告模板：[`assets/需求提取总结报告.md`](/Users/shuwanchun/git/x-flow/.claude/skills/xuanwu/req-extractor/assets/需求提取总结报告.md)
- 通用 bundle 调度：[bundle-orchestrator](/Users/shuwanchun/git/x-flow/.claude/skills/xuanwu/bundle-orchestrator/SKILL.md)
