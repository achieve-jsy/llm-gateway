# 详细设计报告模板

> 通用模板，所有 profile 共享此格式

---

## 设计基本信息

| 字段 | 值 |
|------|-----|
| 模块名 | `{module}` |
| Profile | `{profile}` |
| 设计阶段 | tech-design |
| 输出目录 | `{output_dir}` |
| 设计时间 | `{design_date}` |
| 执行方式 | `{execution_mode}` |

---

## 文档导航

### 服务端详细设计
| doc_id | 输出文件 | 说明 | 状态 |
|--------|----------|------|------|
| `{doc_id}` | `design_server_{id}.md` | `{summary}` | `{status}` |

### 客户端详细设计
| doc_id | 输出文件 | 说明 | 状态 |
|--------|----------|------|------|
| `{doc_id}` | `design_client_{id}.md` | `{summary}` | `{status}` |

---

## 生成结果总览

| 项目 | 内容 |
|------|------|
| 实际生成文档数 | `{generated_doc_count}` |
| 实际生成文档列表 | `{generated_docs}` |
| 因依赖自动补齐的文档 | `{auto_included_docs}` |
| blocked 项数量 | `{blocked_count}` |
| 执行方式说明 | `{execution_mode_detail}` |

---

## 设计依据

### 主输入文档（来自 arch-designer）
| 文档名 | 路径 | 作用 |
|--------|------|------|
| 技术架构主索引 | `xspec/changes/{module}/hld.md` | 阶段主产物，架构目标与风险 |
| 服务端概要 | `xspec/changes/{module}/hld_server.md` | 服务端脚本落点、命令职责、状态机 |
| 客户端概要 | `xspec/changes/{module}/hld_client.md` | 模块划分、文件落点、事件表 |

---

## blocked 项汇总

| doc_id | 输出文件 | blocked 原因 | 影响范围 |
|--------|----------|--------------|----------|
| `{doc_id}` | `{output}` | `{blocked_reason}` | `{impact}` |

> 若无 blocked 项，填写"无"。

---

## 规范自检结果

| 检查项 | 结果 | 说明 |
|--------|------|------|
| 所有选中文档已生成 | `{pass_or_fail}` | `{remark}` |
| 无残留模板占位符 | `{pass_or_fail}` | `{remark}` |
| 跨端字段命名一致 | `{pass_or_fail}` | `{remark}` |
| hld 关键约束已深化或标风险 | `{pass_or_fail}` | `{remark}` |
| 服务端自检报告 | `{pass_or_fail}` | `design-check-server-report.md` |
| 客户端自检报告 | `{pass_or_fail}` | `design-check-client-report.md` |

---

## 下一步建议

- 若 blocked 项为 0 且自检全部通过，可执行 `task-work` 进入任务拆解阶段
- 若存在 blocked 项，需先补全 `hld_*` 概要文档中的缺口后重跑

---

**文档生成时间**: `{generated_at}`
