---
name: bundle-orchestrator
description: Load bundle.yaml, resolve selected docs/templates, expand depends_on, and compute phase execution order using the shared management bundle loader. Use when a skill is driven by bundle.yaml, supports --docs/--templates, or needs execution_plan-based orchestration.
version: 1.0
last_updated: 2026-04-04
disable-model-invocation: true
---

# Bundle Orchestrator

为基于 `bundle.yaml` 驱动的业务 Skill 提供统一的解析、选择和 phase 调度约定。

适用场景：
- 业务 Skill 使用 `xwRef/xuanwu/<profile>/<category>/bundle.yaml`
- 需要支持 `--docs` / `--templates`
- 需要支持 `depends_on`
- 需要支持 `execution_plan.phases`

## 通用脚本

统一使用：

`.claude/tools/bundle/bundle_loader.py`

该脚本负责：
- 读取 bundle 文件
- 标准化 `docs[]`
- 按 `selection_aliases` 匹配用户选择
- 自动补齐 `depends_on`
- 计算 `execution_plan`
- 在 phase 内结合 `depends_on + execution.priority` 排序
- 为 `docs[]` 补齐 `extract_template` / `publish_template` 双模板兼容信息
- **路径解析**：自动将 bundle 中的相对路径解析为仓库相对路径（相对 bundle 所在目录）

**路径解析规则**：
```python
# bundle.yaml 中的相对路径
template: "extract-spec/overview.md"

# 自动解析为仓库相对路径（相对于 bundle 所在目录）
xwRef/xuanwu/<profile>/req/extract-spec/overview.md
```

## 使用方式

### Step 1：定位 bundle 文件
按业务 category 定位：
- req：`xwRef/xuanwu/<profile>/req/bundle.yaml`
- tech：`xwRef/xuanwu/<profile>/tech/bundle.yaml`
- tech-design：`xwRef/xuanwu/<profile>/tech-design/bundle.yaml`
- task：`xwRef/xuanwu/<profile>/task/bundle.yaml`
- testcase：`xwRef/xuanwu/<profile>/testcase/bundle.yaml`
- impl：`xwRef/xuanwu/<profile>/impl/bundle.yaml`

### Step 2：读取 bundle
业务 Skill 先确定：
- `profile`
- `stage`
- `main_artifact`
- `report_artifact`
- `id_key`
- `spec_dir`（可选）：`xspec/changes/<module>/` 目录路径，用于 `auto_enable` 判断时解析 `input_files` 中的相对路径

然后按这些参数调用通用 bundle loader。

常见 `id_key`：
- req：`template_id`
- tech：`doc_id`
- task / tech-design / testcase / impl：`doc_id`

**`default_inputs` 字段说明**：
bundle 顶层的 `default_inputs` 声明主 Skill 在该阶段默认需要加载的输入类型。业务 Skill 应据此准备对应的输入文件：
- `req`：`requirement_main_artifact: true` 表示需要需求主文件，`metadata: true` 表示需要元数据
- `tech`：`breakdown_dir: true` 表示需要 `xspec/changes/` 下的需求拆解目录
- `tech-design`：`arch_hld_dir: true` 表示需要技术架构概要文档目录
- `task`：`design_dir: true` / `coverage_map: true` 等表示需要设计冻结文档或任务覆盖账本；实际读取范围仍以各 `docs[].input_files` 为准
- `testcase`：`task_handoff: true` 表示需要 `/xf:task` 产出的 `task-handoff.json`，测试用例生成输入仍以各 `docs[].input_files` 为准
- `impl`：`design_client_freeze: true` / `task_client: true` 等表示需要客户端/服务端冻结文档和任务卡

**`docs[].kind` 字段说明**：
bundle 可用 `kind` 声明文档在业务 Skill 中的语义类别。`bundle-orchestrator` 不对 `kind` 做业务判断，只负责透传给业务 Skill 和子任务 prompt。常见取值示例：

- task 阶段：`freeze_shard`、`freeze_index`、`task_shard`、`coverage_reconcile`、`gap_scan`、`self_check`、`handoff`、`report`
- testcase 阶段：`testcase_shard`、`testcase_coverage`、`testcase_self_check`、`testcase_report`

新增 `kind` 不应要求修改业务 Skill；只要对应 phase、输入、模板、输出、规则以及可选 checklist 已由 bundle 声明，业务 Skill 应按通用 phase 调度规则执行。

### Step 3：解析用户选择
若用户未显式指定子集：
- 选择 `enabled_by_default: true` 的全部条目

若用户显式指定：
- 先按唯一 ID 匹配
- 再按 `selection_aliases` 匹配
- 自动补齐 `depends_on`
- 若仍未命中，报错并列出可选值

常见映射：
- req：`--templates overview,reward`
- tech：`--docs proto,server`

#### 3.1 触发与选择规则

默认整套执行：
- 当用户说"生成整套""批量提取""全部生成""生成 tech""提取需求"这类泛化请求时
- 默认选择 bundle 中 `enabled_by_default: true` 的全部条目

指定子集执行：
- 当用户明确说"只生成某几个文档"或传入 `--docs` / `--templates` 时
- 只执行命中的条目及其自动补齐依赖

歧义处理：
- 若用户只说"生成协议""提取配置"这类局部目标，但 profile 或条目标识不够明确
- 先由业务 Skill 结合上下文判断
- 仍不明确时，再由业务 Skill 向用户澄清

#### 3.2 自动启用规则（auto_enable）

部分文档的 `enabled_by_default` 为 `false` 但配置了 `auto_enable: { enabled: true }`。解析器会：
1. 扫描该文档的 `input_files`，筛选出指向 `spec_*` 的路径
2. 检查这些文件在 `spec_dir` 下是否存在且有实质内容（超过 100 字符且不含大量占位符）
3. 若存在，将该文档的 `enabled` 自动设为 `true`，无需用户显式指定

典型用例：`client-payment-design` 仅在存在有效 `spec_payment.md` 时才启用。

### Step 4：计算 phase 顺序
1. 若 bundle 定义 `execution_plan.phases`，先按 phase 顺序执行
2. phase 内：
   - 先满足 `depends_on`
   - 再参考 `execution.priority`
3. 若无 `execution_plan`：
   - 回退为基于 `depends_on` 的拓扑执行

`execution_plan.phases` 允许业务 Skill 扩展阶段名。以 testcase 阶段为例，可声明：

- `testcase-docs`：并行生成 `testcase-qa` 与 `testcase-dev`

这些 phase 与普通 phase 一样遵守 `mode`、`depends_on`、`execution.priority`、失败回退和路径解析规则；测试维度与输出格式由 bundle 的 template/rules 决定，不应写进 Skill。

### Step 5：phase 执行规则

#### 5.1 执行模式
1. `phase.mode = sequential`
   - 逐项执行
   - 任一失败时，当前 phase 立即停止
2. `phase.mode = parallel`
   - 必须在**同一个 response message 中并发发出多个 `task` 工具调用**（同一消息内含多个 tool call block），禁止按列表顺序逐一启动
   - phase 结束后统一汇总成功/失败状态
   - ⚠️ 若不在同一消息内并发发出，子 Agent 将串行执行，失去并行意义
3. phase 内无 `priority` 时，保持 bundle 声明顺序
4. 若某条目依赖未满足，不得强行继续执行

#### 5.2 Agent 角色传递
启动每个子任务时：
1. 读取 `docs[].execution.agent_role`
2. 若未定义，默认使用 `extractor`
3. 读取 `.claude/agents/xuanwu_<agent_role>.md` 获取该角色的详细定义和 Prompt 模板
4. 在子任务 `prompt` 中明确声明角色职责（使用 Agent 定义中的标准 Prompt 模板）
5. 使用 `task` 工具启动子任务，`subagent_type` 固定为 `general`

Agent 定义目录：`.claude/agents/`
- `xuanwu_extractor.md`: 信息提取器
- `xuanwu_architect.md`: 架构师
- `xuanwu_reviewer.md`: 评审员
- `xuanwu_developer.md`: 开发者

#### 5.3 子任务 Prompt 结构
子任务 prompt 必须包含：
1. **角色声明**：明确 `agent_role` 和职责
2. **输入清单**：bundle 为该文档声明的 `input_files` 和依赖文档
3. **模板路径**：`docs[].extract_template` / `docs[].publish_template` / `docs[].template` 解析后的仓库相对路径；双模板场景必须明确提取模板和发布模板
4. **输出路径**：`docs[].output` 的仓库相对路径
5. **执行要求**：只读取自己的模板、规则和输入；提取阶段使用 `extract_template`，最终落盘结构使用 `publish_template`
6. **校验标准**：输出文件存在、无占位符残留、关键字段一致

### Step 6：失败回退
1. `sequential` phase 中任一条目失败：
   - 立即停止当前 phase
   - 标记下游依赖为 blocked
2. `parallel` phase 中部分失败：
   - 保留成功结果
   - 仅重跑失败项
3. 汇总后发现一致性问题：
   - 只重写受影响项
   - 再重建主报告/主索引文档
4. 若关键依赖项失败：
   - 不允许进入后续依赖 phase
5. 若条目 `execution.mode=deterministic` 且声明了 `fallback_mode: ai_subagent`：
   - 先记录 deterministic 失败原因
   - 将该条目按原有 `extract_template + input_files` 回退为一次 AI subagent 执行
   - 仅当 deterministic 与 fallback 均失败时，才阻断后续依赖 phase

### Step 7：业务 Skill 接入方式
业务 Skill 至少需要提供：
- bundle 路径
- `id_key`
- 阶段输入来源
- 输出目录
- 报告产物
- 是否推进阶段

若 bundle 使用双模板：
- 提取阶段读取 `extract_template`
- 最终落盘读取 `publish_template`
- 未声明时统一回退到 `template`

业务 Skill 不需要重复描述：
- `--docs` / `--templates` 匹配规则
- `depends_on` 补齐规则
- `execution_plan` phase 规则
- phase 失败后的 blocked / retry 策略
- “默认整套 vs 指定子集”的通用触发规则
- “整套执行 / 子集执行 / 独立目录模式”的通用示例模式

### Step 8：通用执行示例模式

整套执行：
- 输入一个工作项 ID 或输入目录
- 不显式指定子集
- 读取 bundle 后，执行 `enabled_by_default: true` 的全部条目

子集执行：
- 显式传入 `--docs` 或 `--templates`
- 匹配条目并自动补齐依赖
- 输出“实际命中项 + 自动补齐项”

独立目录模式：
- 输入 `profile + input + output`
- 不依赖 dmctl
- 仍按同一套 bundle 规则执行

## 业务 Skill 的职责边界

`bundle-orchestrator` 只负责：
- bundle 解析
- 子集选择与依赖补齐
- phase 顺序与执行规则
- phase 失败回退规则

业务 Skill 仍负责：
- 输入源定位
- 报告结构
- 主文档汇总
- 阶段推进
- 领域特有校验

## 重要约束
- 不在业务 Skill 中重复实现 bundle 解析逻辑
- 不再为每个 Skill 新增一份 `parse_bundle.py` 包装层，优先直接复用 management 通用脚本
- 旧包装脚本若无兼容必要，应删除
- `bundle.yaml` 可以有注释，但注释不应承载机器依赖的语义
