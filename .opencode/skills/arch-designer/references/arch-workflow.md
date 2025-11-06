# arch-designer 工作流参考

本文档定义 `arch-designer` 使用的 `bundle.yaml` 协议、主文档契约和校验规则。

补充说明：

- `bundle.yaml` 允许添加 YAML 注释，解析器会自动忽略
- `execution_plan` 用于声明阶段级调度
- `docs[].execution` 用于声明单文档的执行偏好
- 通用解析器：`.claude/tools/bundle/bundle_loader.py`
- 通用 bundle 调度技能：`.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`
- 若后续实现 bundle 解析器，应优先解析 `execution_plan`，无该字段时回退为 `depends_on` 拓扑执行

## 1. 目录协议

### Skill 目录

- `.claude/skills/xuanwu/arch-designer/SKILL.md`
- `.claude/skills/xuanwu/arch-designer/references/arch-workflow.md`
- `.claude/skills/xuanwu/arch-designer/assets/arch-report-template.md`

### 通用管理工具

- `.claude/tools/bundle/bundle_loader.py`
- `.claude/tools/bundle/README.md`

### 通用 Skill

- `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md`

### Profile 目录

- `xwRef/xuanwu/<profile>/tech/bundle.yaml`
- `xwRef/xuanwu/<profile>/tech/extract-spec/*.md`（提炼模板）
- `xwRef/xuanwu/<profile>/tech/publish/*.md`（落盘模板）

### 输入目录

- `xspec/changes/<module>/spec_*.md`（req-extractor 输出）

### 输出目录

- `xspec/changes/<module>/hld.md`（主索引文档）
- `xspec/changes/<module>/hld_server_<id>.md`（服务端文档）
- `xspec/changes/<module>/hld_client_<id>.md`（客户端文档）
- `xspec/changes/<module>/技术架构报告.md`

## 2. bundle.yaml 协议

### 顶层字段

| 字段              | 必填 | 说明                                     |
| ----------------- | ---- | ---------------------------------------- |
| `version`         | 是   | bundle 协议版本，当前固定 `1`            |
| `profile`         | 是   | profile 名称，如 `activity`              |
| `stage`           | 是   | 当前固定 `tech`                          |
| `main_artifact`   | 是   | 阶段主产物文件名，固定为 `hld.md`        |
| `report_artifact` | 是   | 生成报告文件名，`技术架构报告.md`        |
| `default_inputs`  | 否   | 主 Skill 默认加载的输入声明              |
| `execution_plan`  | 否   | 阶段级执行计划，定义文档分阶段调度       |
| `docs`            | 是   | 要生成的领域文档列表                     |

### `default_inputs`

| 字段            | 必填 | 说明                                        |
| --------------- | ---- | ------------------------------------------- |
| `spec_dir`      | 否   | 是否读取 `xspec/changes/<module>/` 需求文档 |

### `docs[]`

| 字段                 | 必填 | 说明                                                          |
| -------------------- | ---- | ------------------------------------------------------------- |
| `doc_id`             | 是   | 文档唯一标识                                                  |
| `title`              | 是   | 文档显示名                                                    |
| `template`           | 是   | 模板文件路径，相对 `bundle.yaml` 所在目录                     |
| `output`             | 是   | 输出文件名（`hld.md` / `hld_server_<id>.md` / `hld_client_<id>.md`） |
| `role`               | 是   | `main` 或 `supplement`                                        |
| `enabled_by_default` | 是   | 默认是否生成                                                  |
| `selection_aliases`  | 否   | 用户选择文档子集时可命中的别名                                |
| `input_files`        | 否   | 推荐读取的文件名列表（相对输出目录或 `../` 引用上级需求文件） |
| `depends_on`         | 否   | 依赖的 `doc_id` 列表                                          |
| `summary`            | 否   | 主索引文档里展示的一句摘要                                    |
| `execution`          | 否   | 单文档执行偏好，如 `mode/group/priority`                      |

### `execution_plan`

| 字段                   | 必填 | 说明                       |
| ---------------------- | ---- | -------------------------- |
| `phases`               | 是   | 阶段列表                   |
| `phases[].phase_id`    | 是   | 阶段唯一标识               |
| `phases[].description` | 否   | 阶段说明                   |
| `phases[].mode`        | 是   | `sequential` 或 `parallel` |
| `phases[].docs`        | 是   | 该阶段包含的 `doc_id` 列表 |

### `docs[].execution`

| 字段         | 必填 | 说明                                    |
| ------------ | ---- | --------------------------------------- |
| `mode`       | 否   | 单文档执行偏好                          |
| `group`      | 否   | 阶段内逻辑分组                          |
| `priority`   | 否   | 排序优先级，数字越小越早                |
| `agent_role` | 是   | 子 Agent 角色标签，决定子任务的执行策略 |

### `agent_role` 详细说明

| 角色         | Agent 文件                        | 职责                                 | 适用场景                         |
| ------------ | --------------------------------- | ------------------------------------ | -------------------------------- |
| `extractor`  | `xuanwu_extractor.md`             | 从输入文件中提取结构化信息并填充模板 | 需求提取、协议定义               |
| `architect`  | `xuanwu_architect.md`             | 基于提取结果设计技术架构、模块划分   | 架构设计、技术方案制定           |
| `reviewer`   | `xuanwu_reviewer.md`              | 校验文档一致性、完整性               | 文档评审、质量检查               |
| `developer`  | `xuanwu_developer.md`             | 开发实现                             | 代码生成、实现落地               |

## 3. 解析规则

1. 先按 `profile` 找到 `xwRef/xuanwu/<profile>/tech/bundle.yaml`
2. `template` 路径相对 `bundle.yaml` 目录解析
3. `doc_id`、`output` 必须唯一
4. `depends_on` 只能引用已存在的 `doc_id`
5. 用户指定 `--docs` 时：匹配、依赖补齐、phase 顺序与失败回退统一遵循 `bundle-orchestrator`
6. 解析后的文档集合需要自动补齐依赖项

## 4. 主文档契约

`hld.md` 必须由主 Skill 汇总生成，不直接绑定某个 profile 模板。

推荐最小结构：

```markdown
# 技术架构

## 1. 基本信息

## 2. 架构目标与范围

## 3. 输入来源

## 4. 文档生成清单

## 5. 核心架构决策

## 6. 领域文档索引

## 7. 风险与待确认项
```

规则：
- 若存在 `role: main` 的文档，应将其核心摘要纳入"核心架构决策"
- 所有生成文档都要出现在"领域文档索引"
- 主文档只做摘要、约束和导航，不重复复制所有正文

## 5. 子任务输入规则

每个子任务只读取：
- 自己的模板文件
- `depends_on` 指向的已生成文档
- `bundle.yaml` 中为自己声明的 `input_files`

不要读取：
- 其他模板文件
- 无关 profile 的模板
- 未声明的需求文件全集

## 6. 报告契约

`技术架构报告.md` 至少包含：
- 基本信息
- 输入来源
- 生成结果
- 默认值替换记录
- 未明确项汇总
- 校验结果
- 下一步建议

## 7. 校验清单

生成完成后至少检查：

1. `hld.md` 和 `技术架构报告.md` 存在
2. 所有选中的 `docs[].output` 存在
3. 输出文档中没有模板占位符残留
4. 关键名词一致：活动名 / 模块名 / 协议名 / 配置字段名 / 状态枚举与接口名
5. `depends_on` 依赖顺序已满足
6. `hld.md` 中的索引表与真实生成文件一致
