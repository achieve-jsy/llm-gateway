---
name: proto-generator
description: 读取 bundle.yaml 处理玄武定制流程中的 proto 设计产物、编译流水线和生成产物契约。
disable-model-invocation: true
---

# proto-generator

玄武协议生成技能。该技能自身只定义通用调度逻辑；profile、phase、脚本、输入、产物、命名契约和跳过/阻塞规则全部来自 `xwRef/xuanwu/<profile>/proto/bundle.yaml`。

## 1. 定位与边界

**职责**：

- 读取 `xwRef/xuanwu/<profile>/proto/bundle.yaml`
- 根据 `--profile <profile> --module <module> --phase <design|detail>` 选择唯一 job
- 按 job 的 `handler` 执行 proto 契约校验、源文件生成或编译脚本
- 按 job 的 `validation` 校验关键产物
- 返回结构化结果，供调用方写入阶段完成报告

**不职责**：

- 不补发需求，不发明协议 ID、消息名或字段含义
- 不把 profile 差异硬编码在 `SKILL.md`
- 不修改 `.xflow/validate-rules.yaml`
- 不绕过脚本失败；脚本退出码非 0 时必须返回 `blocked`
- 不在 `/xf:task` 或 `/xf:implement` 阶段重新生成 proto

## 2. 执行口径

```text
proto-generator --profile <profile> --module <module> --phase <design|detail>
```

可执行组合以 `xwRef/xuanwu/<profile>/proto/bundle.yaml` 的 `jobs[]` 为唯一来源。若对应 bundle 不存在，直接返回：

```yaml
status: skipped
reason: bundle-not-found
```

若 bundle 存在但找不到匹配 job，返回：

```yaml
status: skipped
reason: unsupported-combination
```

不得在 `SKILL.md` 中新增 profile 分支；新增或调整方言时只改对应 profile 的 `proto/bundle.yaml`。

## 3. 配置协议

### bundle 位置

```text
xwRef/xuanwu/<profile>/proto/bundle.yaml
```

缺少该文件不是错误；表示当前 profile 没有 proto 定制流程，skill 必须返回 `skipped: bundle-not-found`，调用方继续后续步骤。

### job 匹配键

每个 job 必须声明：

```yaml
job_id: <stable-id>
module: <module>
phase: design | detail
handler: <handler-name>
```

当前 handler 类型：

| handler | 用途 |
| ------- | ---- |
| `append_existing_proto_and_sync` | 读取 HLD endpoint，补全已有 proto，再运行同步脚本 |
| `validate_contract_only` | 不生成 proto，只校验命名契约与骨架锚点 |
| `generate_proto_and_build` | 从 HLD 协议表生成 proto 源文件，再运行编译脚本 |

## 4. 通用执行流程

1. 根据 `--profile` 解析 bundle 路径：`xwRef/xuanwu/<profile>/proto/bundle.yaml`。
2. 若 bundle 不存在，返回 `status: skipped, reason: bundle-not-found`。
3. 读取并解析 bundle。
4. 校验 bundle 顶层 `profile` 与入参 `--profile` 一致。
5. 用 `module + phase` 匹配唯一 job。
6. 读取 `xspec/changes/change.yaml`，将 `{game_id}`、`{server_module_name}`、`{abbr}` 等占位符替换为运行时值。
7. 校验 job 的 `inputs.required_change_fields`、`inputs.*` 与 `validation.required_files`。
8. 执行 `skip_when`：
   - 命中允许跳过条件时返回 `status: skipped`
   - 未命中时继续执行 handler
9. 执行 job handler。
10. 若 job 声明 `script.command`，执行占位符替换后的命令。
11. 脚本退出码非 0 且 `block_on_non_zero_exit: true` 时返回 `status: blocked`。
12. 按 `validation.generated_artifacts` 校验产物；缺失时按 `missing_artifact_status` 返回。
13. 返回结构化结果。

## 5. Handler 语义

### append_existing_proto_and_sync

用于已有 proto 源文件的增量补全。

执行要求：

- 从 `inputs.hld_endpoint_sources.any_of` 读取 endpoint 定义
- 若 `skip_when.condition: no_new_endpoint` 命中，返回可接受的 `skipped`
- 按 `append_rules` 修改 `inputs.proto_source`
- 不修改已有字段编号
- 不执行 `append_rules.forbidden_edits` 中禁止的操作
- 运行 `script.command`
- 按 `validation.generated_artifacts` 与 `mtime_not_older_than` 校验生成物

### validate_contract_only

用于 design 阶段的协议命名契约与锚点校验。

执行要求：

- 不生成 proto，不运行编译脚本
- 校验 `inputs.required_change_fields`
- 展开并校验 `validation.required_files`
- 返回 `naming_contract.client_proto_artifacts` 和 `downstream_anchor.note`
- 若发现设计输入或 HLD 草稿引入 `forbidden_client_proto_patterns`，返回 `blocked`

### generate_proto_and_build

用于从 HLD 协议总表生成设计 proto 产物并编译。

执行要求：

- 从 `inputs.protocol_tables` 读取协议总表
- 按 `proto_generation` 写入唯一设计 proto 产物
- 只包含配置允许的消息集合
- 执行 `script.command`，必须保留 `--force` 等配置参数
- 按 `naming_contract` 固化客户端产物名
- 按 `validation.generated_artifacts` 校验设计、服务端、客户端产物

## 6. 返回格式

返回结果必须包含以下标准字段：

```yaml
status: completed | skipped | blocked
job_id: <job_id>
bundle: xwRef/xuanwu/<profile>/proto/bundle.yaml
profile: <profile>
module: <module>
phase: <phase>
source_files:
  - <path>
generated_artifacts:
  design: []
  server: []
  client: []
script_command: <expanded command or N/A>
reason: <N/A or skip/block reason>
anchor_note: <downstream anchor note>
```

调用方报告中统一写：

```text
Proto 处理（proto-generator）：
  job：<job_id>
  状态：completed / skipped / blocked
  源文件：<source_files>
  产物：<generated_artifacts>
  脚本：<script_command>
  说明：<reason or anchor_note>
```

## 7. 阶段完成条件

以下全部满足才视为 proto 处理完成：

- 匹配到唯一 job，或按配置返回允许的 `skipped`
- required change fields 与 required files 已校验
- handler 已按 YAML 配置执行
- 脚本成功，或配置允许跳过脚本
- generated artifacts 已按配置校验
- 下游 SKILL 的协议锚点明确返回
