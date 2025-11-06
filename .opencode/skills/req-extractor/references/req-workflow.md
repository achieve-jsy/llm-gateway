# req-extractor 工作流参考

本文档定义 `req-extractor` 使用的 `template/req/bundle.yaml` 协议、`--templates`
选择规则和输出约定。

补充说明：
- `bundle.yaml` 允许使用 YAML 注释，解析器会自动忽略
- `execution_plan` 用于声明阶段调度
- `docs[].execution` 用于声明单文档的调度偏好
- `tools/bundle/bundle_loader.py` 是统一解析入口
- `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md` 是统一 bundle 调度技能

## 1. 目录协议

### Skill 目录
- `.claude/skills/xuanwu/req-extractor/SKILL.md`
- `.claude/skills/xuanwu/req-extractor/references/req-workflow.md`
- `.claude/skills/xuanwu/req-extractor/references/req-validation.md`
- `.claude/skills/xuanwu/req-extractor/assets/需求提取总结报告.md`

### 通用管理工具
- `.claude/tools/bundle/bundle_loader.py`
- `.claude/tools/bundle/README.md`

### 通用 Skill
- `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`

### Profile 目录
- `xwRef/xuanwu/<profile>/req/bundle.yaml`
- `xwRef/xuanwu/<profile>/req/**/*.md`

### 输出目录
- `xspec/changes/<module>/spec_<template_id>.md`
- `xspec/changes/<module>/需求提取总结报告.md`

## 2. bundle.yaml 协议

### 顶层字段

| 字段 | 必填 | 说明 |
|------|------|------|
| `version` | 是 | 当前固定 `1` |
| `profile` | 是 | profile 名称 |
| `stage` | 是 | 当前固定 `req` |
| `main_artifact` | 是 | 输入主文档名，通常是 `需求文档.md` |
| `report_artifact` | 是 | 报告文件名，固定 `需求提取总结报告.md` |
| `default_inputs` | 否 | Skill 默认加载输入声明 |
| `validation` | 否 | checklist / rules 配置 |
| `execution_plan` | 否 | 阶段级执行计划 |
| `docs` | 是 | 需求拆解模板列表 |

### `default_inputs`

推荐默认值：

```yaml
default_inputs:
  requirement_main_artifact: true
  metadata: true
```

### `validation`

| 字段 | 必填 | 说明 |
|------|------|------|
| `checklists` | 否 | 当前 profile 的人工验收清单列表 |
| `rules` | 否 | 当前 profile 的机器规则文件列表 |

### `docs[]`

| 字段 | 必填 | 说明 |
|------|------|------|
| `template_id` | 是 | 模板唯一标识 |
| `title` | 是 | 模块显示名 |
| `template` | 否 | 兼容字段，默认视为最终输出模板 |
| `extract_template` | 否 | 提取阶段使用的模板，相对 `bundle.yaml` |
| `publish_template` | 否 | 输出阶段使用的模板，相对 `bundle.yaml` |
| `output` | 是 | 输出文件名 |
| `enabled_by_default` | 是 | 默认是否生成 |
| `selection_aliases` | 否 | `--templates` 的别名 |
| `depends_on` | 否 | 依赖的 `template_id` 列表 |
| `summary` | 否 | 汇总报告中的摘要 |
| `required_when` | 否 | 可选启用条件说明，当前仅文档化 |
| `execution` | 否 | 单文档执行偏好，如 mode / group / priority |

### 双模板规则

1. req profile 当前统一使用 `extract_template` 和 `publish_template`
2. 提取内容时读取 `extract_template`
3. 最终落盘时读取 `publish_template`
4. `bundle_loader` 对外仍保留 `template` 字段；其值默认指向 `publish_template`

### `execution_plan`

| 字段 | 必填 | 说明 |
|------|------|------|
| `phases` | 是 | 阶段列表 |
| `phases[].phase_id` | 是 | 阶段唯一标识 |
| `phases[].description` | 否 | 阶段说明 |
| `phases[].mode` | 是 | `sequential` 或 `parallel` |
| `phases[].docs` | 是 | 该阶段包含的 `template_id` 列表 |

### `docs[].execution`

| 字段 | 必填 | 说明 |
|------|------|------|
| `mode` | 否 | 单文档执行偏好，通常为 `parallel` 或 `sequential` |
| `group` | 否 | 逻辑分组名 |
| `priority` | 否 | 排序优先级，数字越小越早 |
| `agent_role` | 否 | 执行单元角色标签，如 `extractor`；若运行环境允许，则应映射为子 Agent |

## 3. `--templates` 选择规则

1. 未指定 `--templates`
   - 只生成 `enabled_by_default: true` 的模板
2. 指定 `--templates a,b,c`
   - 匹配、依赖补齐、phase 顺序与失败回退统一遵循 `bundle-orchestrator`

示例：

- `--templates overview,reward`
- `--templates 概述,奖励`
- `--templates protocol,config`

若输入 `--templates protocol`，且 `protocol` 依赖 `overview`，最终执行集合为：

```text
overview, protocol
```

## 4. 生成流程

### Step 1. 加载 bundle
1. 读取 `xwRef/xuanwu/<profile>/req/bundle.yaml`
2. 调用 `load_bundle_file(...)`
3. 调用 `resolve_selected_items(...)` 解析选中模块
4. 调用 `enrich_selected_docs_with_templates(...)` 为每个模块补齐：
   - `extract_template_path`
   - `publish_template_path`
   - `template_path`
5. 本阶段只允许解析 bundle、选择结果、依赖关系、phase 顺序和模板路径；不预读所有 `publish_template` 或 `extract_template` 的正文内容
6. 模板正文必须按需读取：
   - 提取阶段只读取当前模块的 `extract_template_path`
   - 输出阶段只读取当前模块的 `publish_template_path`

### Step 2. 提取阶段
每个执行单元只读取自己的 `extract_template_path`，从需求文档中提取结构化内容。
若当前运行环境允许并行代理/子 Agent，则必须按 bundle phase 启动子 Agent 并行调度；
若当前运行环境不允许并行代理/子 Agent，则退化为主 Agent 串行逐个执行，但仍需保持逐文件独立提取。

要求：
- 严格遵守提取模板结构
- 不直接把提取模板落盘成最终文件
- 未说明内容优先按模板规则、领域默认值、同类方案和上下文推理补全
- 只有会影响实现、联调或验收的关键未知项，才写 `?（文档未说明）`
- 记录默认值替换和未明确项

### 关键未知项白名单

以下内容默认视为“关键未知项”，文档未明确时应保留 `?（文档未说明）`，而不是自行补值：

- 全局唯一标识：活动 ID、玩法 ID、模板 ID、外部系统 ID、资源位 ID、奖励 ID、商品 ID、协议号、错误码
- 外部绑定信息：埋点事件名、接口名、协议名、支付编码、渠道编码、区服名单、白名单来源
- 会改变实现分支的规则开关：首出规则、胜负裁决规则、互斥开关、阶段插入条件、终局条件、核心玩法模式切换条件
- 会改变结算或奖励结果的关键参数：倍率、概率、保底阈值、封顶值、库存池标识、返利比例、排名阈值
- 会改变交互验收结果的关键表现：入口挂载位、主按钮文案、核心提示文案、页面跳转目标、关键弹窗触发条件

以下内容默认不属于关键未知项，文档未明确时应优先推理默认值，并在报告中记录来源：

- 通用布尔开关默认值
- 开始/结束时间缺省口径，如永久生效、永久有效
- 非关键限制项缺省口径，如不限制、不开启、无特殊处理
- 通用超时、重试、降级、一致性、幂等性策略
- 同类活动或同类棋牌玩法中稳定复用的常规流程与 UI 呈现

### Step 3. 输出阶段
提取完成后，每个模块再读取自己的 `publish_template_path`，将提取结果映射为最终输出文件。

要求：
- 最终落盘结构以 `publish_template_path` 为准
- 输出文件名以 `docs[].output` 为准
- 不允许把 `extract_template` 直接当成最终输出模板
- 必须先形成“提取中间结果”，再映射到 `publish_template_path`
- 落盘前必须执行一次“问号收敛”：非关键未知项不得保留 `?（文档未说明）`
- 所有保留下来的 `?` 都必须能归类到关键未知项白名单
- 若最终文件仍包含提取模板标题、章节名或提示语，例如“需求提炼”“提炼范围”“填写要求”，视为生成失败并重写
- 执行单元单文件检测、失败回退和主 Agent 汇总前复查的详细规则见 [`req-validation.md`](req-validation.md)

### Step 4. 报告阶段
1. 汇总所有模块的提取记录与输出结果
2. 生成 `需求提取总结报告.md`
3. 报告中至少包含：
   - 输出目录
   - 报告路径
   - 实际生成模板列表
   - 自动补齐依赖列表
   - 默认值替换数量
   - 未明确项数量
   - 执行方式说明（子 Agent 并行执行 / 主 Agent 串行退化）

## 5. 输出契约

输出目录必须包含：
1. `docs[].output` 对应的全部结果文件
2. `需求提取总结报告.md`

## 6. 解析器职责

### `.claude/tools/bundle/bundle_loader.py`
- `load_bundle_file(...)`：读取并归一化 bundle
- `resolve_selected_items(...)`：解析选择项并补齐依赖
- `build_phase_execution_order(...)`：计算 phase 顺序
- `get_doc_template_path(...)`：按 `extract` / `publish` / `default` 获取模板路径
- `enrich_selected_docs_with_templates(...)`：为选中文档补充模板绝对路径

### `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`
- 统一 bundle 读取、选择、phase 调度和失败回退规则
- 业务 Skill 不再重复实现 bundle 包装层
