# 架构生成报告

## 1. 基本信息
- 工作项 ID：`{work_item_id}`
- Profile：`{profile}`
- 输出目录：`{output_dir}`
- 生成时间：`{generated_at}`

## 2. 输入来源

| 来源 | 路径 | 用途 |
|------|------|------|
| `spec_*.md` | `xspec/changes/{module}/spec_*.md` | req-extractor 产出的结构化需求文档 |
| `spec_dir` | `xspec/changes/{module}/` | 结构化需求文件目录 |

## 3. 生成结果

| 文档类型 | 输出文件 | 状态 | 说明 |
|----------|----------|------|------|
| `main_artifact` | `hld.md` | `{status}` | 阶段主索引文档 |
| `{doc_id}` | `{output}` | `{status}` | `{summary}` |

## 4. 默认值替换记录

| 文档 | 字段 | 默认值 | 原因 |
|------|------|--------|------|
| `{doc_id}` | `{field}` | `{default_value}` | `{reason}` |

## 5. 未明确项汇总

| 文档 | 位置 | 内容 |
|------|------|------|
| `{doc_id}` | `{section}` | `?（文档未说明）` |

## 6. 校验结果
- 主文档存在：`{yes_or_no}`
- 报告存在：`{yes_or_no}`
- 选中文档全部生成：`{yes_or_no}`
- 占位符已清理：`{yes_or_no}`
- 跨文档关键字段一致：`{yes_or_no}`

## 7. 下一步建议
- 若为 dmctl 工作项模式，校验通过后可执行 `advance`
- 若存在未明确项，优先补齐需求拆解文档后再重跑
