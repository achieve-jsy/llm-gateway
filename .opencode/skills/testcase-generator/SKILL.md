---
name: testcase-generator
description: "在 /xf:task 完成后手动调用，基于 task 最终产物生成 QA 执行版与 DEV 技术自查及冒泡测试 Markdown。"
disable-model-invocation: true
---

# testcase-generator

基于 `/xf:task` 已完成的最终产物生成两份面向人的测试文档：

- `task_testcase_qa.md`：给测试同事执行验收。
- `task_testcase_dev.md`：给客户端、服务端、联调开发做提测前冒泡测试与技术自查。

## 边界

- 只读取 `task-handoff.json` 和 `bundle.yaml` 声明的 task/spec/design 输入。
- 不修改 task、freeze、coverage map、task-handoff 或实现代码。
- 不生成旧产物：`task_testcase_part_core.md`、`task_testcase_coverage_map.md`、`task_testcase_self_check.md`、`testcase-report.md`。
- 不生成只给机器消费的追溯大表；追溯关系直接写到 QA/DEV 表格的 `追踪来源`。
- `spec_*` / `design_*` 只能补充数据、步骤、预期和观测点，不能绕过 task 自由生成用例。

## 输入输出

| 类型 | 路径 |
| ---- | ---- |
| Bundle | `xwRef/xuanwu/<profile>/testcase/bundle.yaml` |
| Handoff | `xspec/changes/<module>/task-handoff.json` |
| QA 输出 | `xspec/changes/<module>/task_testcase_qa.md` |
| DEV 输出 | `xspec/changes/<module>/task_testcase_dev.md` |

## 执行

1. 读取 `bundle.yaml`，按 `.claude/skills/xuanwu/bundle-orchestrator/SKILL.md` 解析默认 docs 和 phase。
2. 只读取 `task-handoff.json` 的路径清单与 required 状态，校验 required 文件存在。
3. 在 `testcase-docs` phase 中并行启动两个子 Agent：`testcase-qa` 与 `testcase-dev` 必须在同一轮派发，不能由主 Agent 串行代写。
4. 每个子 Agent 只读取自己 doc 声明的 `input_files`、`extract_template`、`publish_template` 和 `rules`：
   - `testcase-qa` 只读取 QA 提取要求、QA 发布模板和 QA rules，输出 `task_testcase_qa.md`。
   - `testcase-dev` 只读取 DEV 提取要求、DEV 发布模板和 DEV rules，输出 `task_testcase_dev.md`。
5. 子 Agent 先按 `extract_template` 形成中间结果，再按 `publish_template` 的标题、章节、表格结构落盘最终 Markdown；禁止把提取要求文件直接当最终输出模板。
6. 检查双文档质量：无占位符、无空追踪来源、无 `tc-core-*`、无受众错位内容。

如果 `task-handoff.json` 缺失或 required 路径不存在，直接 blocked：

```text
⛔ testcase-generator 无法启动

缺口项：
  - xspec/changes/<module>/task-handoff.json 不存在
  - <required path> 不存在

请先完成 /xf:task 后重新执行 testcase-generator
```

## 完成条件

- `task_testcase_qa.md` 与 `task_testcase_dev.md` 均已生成。
- QA 只包含测试同事可执行或可观察的验收场景。
- DEV 同时包含开发冒泡测试、技术自查、风险与阻塞项。
- 每条 QA 用例、DEV 冒泡用例、DEV 检查项都有非空 `追踪来源`。
- 不出现 `待确认`、`TBD`、`TODO`、孤立 `?`、模板占位符或旧 `tc-core-*` 追踪。

## 资源

| 文件 | 用途 |
| ---- | ---- |
| `xwRef/xuanwu/<profile>/testcase/extract-spec/testcase-qa.md` | QA 提取与生成要求 |
| `xwRef/xuanwu/<profile>/testcase/extract-spec/testcase-dev.md` | DEV 提取与生成要求 |
| `xwRef/xuanwu/<profile>/testcase/publish/testcase-qa.md` | QA 最终输出模板 |
| `xwRef/xuanwu/<profile>/testcase/publish/testcase-dev.md` | DEV 最终输出模板 |
| `xwRef/xuanwu/<profile>/testcase/rules/testcase-qa-rules.md` | QA 质量规则 |
| `xwRef/xuanwu/<profile>/testcase/rules/testcase-dev-rules.md` | DEV 质量规则 |
