# Agent 角色配置指南

本文档说明如何在 `bundle.yaml` 中配置和使用 `agent_role`，以获得最佳的文档生成效果。

## 为什么需要 Agent 角色？

在没有明确指定 agent 角色的情况下，子任务执行时会出现以下问题：

1. **职责不清晰**：子 Agent 不知道自己是提取信息、设计架构还是校验文档
2. **执行策略混乱**：不同任务需要不同的思维方式，提取信息和设计架构需要不同的 prompt
3. **生成效果不稳定**：没有角色约束时，输出质量依赖随机性
4. **无法针对性优化**：无法为不同类型的任务定制校验标准

通过显式指定 `agent_role`，可以：

- 明确子任务的职责边界
- 使用针对性的 prompt 模板
- 应用特定的校验标准
- 提高生成质量和一致性

## Agent 角色详解

### 1. extractor（提取器）

**职责**：从输入文件中提取结构化信息并填充到模板中

**适用场景**：

- 协议文档（proto.md）
- 需求提取
- 配置项整理
- 状态数据定义
- 接口字段整理

**Prompt 关键词**：

- "提取"
- "整理"
- "结构化"
- "归纳"
- "汇总输入文件中的"

**示例配置**：

```yaml
- doc_id: proto
  title: 协议接口定义
  template: proto.md
  output: proto.md
  role: supplement
  enabled_by_default: true
  input_files:
    - steps.md
    - algorithm.md
  depends_on: []
  summary: 定义协议消息、字段、调用方向和兼容策略。
  execution:
    mode: sequential
    group: bootstrap
    priority: 10
    agent_role: extractor
```

**执行要点**：

- 严格基于输入文件，不猜测
- 未明确内容写 `?（文档未说明）`
- 保持字段命名一致性
- 不添加模板外的内容

### 2. architect（架构师）

**职责**：基于提取结果设计技术架构、模块划分、技术方案

**适用场景**：

- 活动总设计方案
- 客户端架构设计
- 服务端架构设计
- Lua 技术方案
- 模块划分与依赖关系

**Prompt 关键词**：

- "设计"
- "架构"
- "模块划分"
- "技术方案"
- "分层"
- "依赖关系"

**示例配置**：

```yaml
- doc_id: overview
  title: 活动总设计方案
  extract_template: extract-spec/overview-design.md
  publish_template: publish/overview-design.md
  output: activity-overview-design.md
  role: main
  enabled_by_default: true
  input_files:
    - activity-overview.md
    - activity-mechanic.md
    - activity-config.md
  depends_on: []
  summary: 统一定义前后端共享契约、配置口径、状态机与接口返回。
  execution:
    mode: sequential
    group: bootstrap
    priority: 10
    agent_role: architect
```

**执行要点**：

- 基于输入文件进行设计，不凭空想象
- 明确模块边界和依赖关系
- 说明技术选型理由
- 定义扩展点和演进方向

### 3. reviewer（评审员）

**职责**：校验文档一致性、完整性、跨文档口径

**适用场景**：

- 文档生成后的质量检查
- 跨文档字段一致性校验
- 模板占位符清理检查
- 关键名词一致性检查

**Prompt 关键词**：

- "校验"
- "评审"
- "检查"
- "一致性"
- "完整性"

**示例配置**：

```yaml
- doc_id: review
  title: 架构文档评审报告
  template: review-template.md
  output: review-report.md
  role: supplement
  enabled_by_default: false
  input_files:
    - hld.md
    - activity-overview-design.md
    - proto.md
  depends_on:
    - overview
    - proto
    - client
    - server
  summary: 校验所有架构文档的一致性和完整性。
  execution:
    mode: sequential
    group: review
    priority: 100
    agent_role: reviewer
```

**执行要点**：

- 检查模板占位符残留
- 校验跨文档关键字段一致
- 检查未明确项数量
- 输出详细的问题清单和改进建议

### 4. integrator（集成器）

**职责**：汇总多个子任务结果生成主索引文档和报告

**适用场景**：

- 生成 `hld.md` 主索引文档
- 生成 `arch-report.md` 生成报告
- 汇总多个文档的核心摘要
- 建立文档索引和依赖关系

**Prompt 关键词**：

- "汇总"
- "回填"
- "索引"
- "编排"
- "整合"

**示例配置**：

```yaml
# 主索引文档由主 Skill 内部处理，不在 bundle 中声明
# 但 integrator 角色用于主文档汇总子任务

# 生成报告可以不显式声明，由主 Skill 调用 integrator 生成
```

**执行要点**：

- 扫描所有已生成文档
- 提取 `role: main` 文档的核心摘要
- 建立文档索引和链接
- 汇总生成结果、默认值替换记录、未明确项

## 角色选择决策树

```
开始
  |
  v
文档类型是什么？
  |
  +-- 从输入文件提取信息 --> extractor
  |
  +-- 设计技术方案/架构 --> architect
  |
  +-- 校验文档质量 --> reviewer
  |
  +-- 汇总多个文档 --> integrator
  |
  v
不确定？
  |
  v
默认使用 extractor（保守策略）
```

## 最佳实践

### 1. 明确角色边界

每个文档只分配一个 agent_role，不要混用。

**错误示例**：

```yaml
# 不推荐：一个文档既要提取又要设计
execution:
  agent_role: extractor # 但实际需要做架构设计
```

**正确示例**：

```yaml
# 推荐：根据实际职责选择
execution:
  agent_role: architect # 明确是架构设计
```

### 2. 依赖关系与角色配合

确保 `depends_on` 和 `agent_role` 配合合理：

```yaml
# 推荐配置：
# 1. 先用 extractor 提取协议
- doc_id: proto
  execution:
    agent_role: extractor
  depends_on: []

# 2. 再用 architect 基于协议设计架构
- doc_id: server
  execution:
    agent_role: architect
  depends_on:
    - proto # 依赖已提取的协议
```

### 3. phase 分组与角色配合

```yaml
execution_plan:
  phases:
    - phase_id: bootstrap
      mode: sequential
      docs: [proto] # extractor 先提取
    - phase_id: architecture
      mode: parallel
      docs: [client, server] # architect 并行设计
    - phase_id: review
      mode: sequential
      docs: [review] # reviewer 最后检查
```

### 4. 角色与模板匹配

确保 `agent_role` 与模板类型匹配：

| 模板类型     | 推荐 agent_role |
| ------------ | --------------- |
| 协议定义模板 | extractor       |
| 架构设计模板 | architect       |
| 技术方案模板 | architect       |
| 评审报告模板 | reviewer        |
| 汇总索引模板 | integrator      |

## 常见问题

### Q1: 不指定 agent_role 会怎样？

默认使用 `extractor`，但这可能导致：

- 架构设计文档变成简单的信息提取
- 缺少技术方案的深度分析
- 生成质量不稳定

### Q2: 可以在运行时覆盖 agent_role 吗？

不建议。`agent_role` 应该在 bundle.yaml 中明确定义，运行时覆盖会破坏一致性。

### Q3: 一个文档需要多个角色怎么办？

拆分成多个文档，每个文档一个角色。例如：

- `proto-extract.md` (extractor)
- `proto-design.md` (architect)

### Q4: reviewer 角色必须吗？

不是必须，但推荐在重要项目中使用。可以：

- 提前发现一致性问题
- 减少人工评审成本
- 提高文档质量

## 完整示例

```yaml
version: 1
profile: card_game
stage: tech
main_artifact: hld.md
report_artifact: arch-report.md

execution_plan:
  phases:
    - phase_id: bootstrap
      description: 先提取协议文档
      mode: sequential
      docs: [proto]
    - phase_id: architecture
      description: 基于协议设计架构
      mode: parallel
      docs: [client, server]

docs:
  - doc_id: proto
    title: 协议接口定义
    template: proto.md
    output: proto.md
    role: supplement
    enabled_by_default: true
    input_files:
      - steps.md
      - algorithm.md
    depends_on: []
    summary: 定义协议消息、字段、调用方向。
    execution:
      mode: sequential
      group: bootstrap
      priority: 10
      agent_role: extractor # 提取协议信息

  - doc_id: client
    title: 客户端架构设计
    template: client-architecture.md
    output: 客户端架构设计.md
    role: supplement
    enabled_by_default: true
    input_files:
      - steps.md
      - algorithm.md
      - proto.md # 依赖已提取的协议
    depends_on:
      - proto
    summary: 说明客户端模块划分、界面状态流转。
    execution:
      mode: parallel
      group: architecture
      priority: 20
      agent_role: architect # 设计客户端架构

  - doc_id: server
    title: 服务端架构设计
    template: server-architecture.md
    output: 服务端架构设计.md
    role: main
    enabled_by_default: true
    input_files:
      - steps.md
      - algorithm.md
      - proto.md
    depends_on:
      - proto
    summary: 说明服务端模块分层、状态模型。
    execution:
      mode: parallel
      group: architecture
      priority: 30
      agent_role: architect # 设计服务端架构
```

## 更新历史

| 日期       | 版本 | 变更                           |
| ---------- | ---- | ------------------------------ |
| 2026-04-07 | 1.0  | 初始版本，定义 4 种 agent 角色 |
