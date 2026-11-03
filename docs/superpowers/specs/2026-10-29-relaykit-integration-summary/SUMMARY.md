# Relaykit Integration - Task 6: 压测与回归验证 总结

> 日期: 2026-10-29
> 分支: protocol-relaykit worktree
> 环境: Apple M2 Pro, macOS (arm64)

---

## 1. 文件创建

| 文件 | 行数 | 说明 |
|------|------|------|
| `internal/protocol/bench_test.go` | ~220 | 7 个 Benchmark 函数 + 辅助函数 |

### Benchmark 列表

| # | Benchmark | 说明 |
|---|-----------|------|
| 1 | `BenchmarkBridge_ClaudeToChat` | 端到端: decode -> convert -> bridge |
| 2 | `BenchmarkBridge_ClaudeToChat_WithTools` | 同上，带 5 个 tool 定义 |
| 3 | `BenchmarkSSEReader_SingleFrame` | 读取单个 SSE frame |
| 4 | `BenchmarkSSEReader_100Frames` | 连续读取 100 个 SSE frame |
| 5 | `BenchmarkStreamAdapter_10Chunks` | 流式转换 10 个 chunk |
| 6 | `BenchmarkStreamAdapter_100Chunks` | 流式转换 100 个 chunk |
| 7 | `BenchmarkCollectDiagnostics_50Diags` | 合并 50 个诊断信息 |

---

## 2. 文件修改

本次任务**未修改任何已有文件**，仅新增 `internal/protocol/bench_test.go`。

协议包已有文件（均为 Task 1-5 产物，无变更）:

| 文件 | 说明 |
|------|------|
| `internal/protocol/session.go` | Session 类型与 NewSession |
| `internal/protocol/policy.go` | Mode / Policy / LoadPolicy |
| `internal/protocol/request.go` | DecodeClaudeRequest / ConvertRequest |
| `internal/protocol/bridge.go` | BridgeToChatRequest / BridgeChatResponseToSource |
| `internal/protocol/diagnostics.go` | CollectDiagnostics / ApplyLossPolicy |
| `internal/protocol/sse.go` | SSEFrame / SSEReader |
| `internal/protocol/stream.go` | StreamAdapter |
| `internal/protocol/doc.go` | 包文档 |

---

## 3. 测试结果

### 协议包测试 (49 测试, 7 benchmark, 100% 通过)

```
=== 测试分组 ===
Bridge 测试:        10 PASS (BasicText, WithTools, WithThinking, DocumentBlock, NilTemperature, TopP, ServerToolUse, ResponseToSource, NoBlocks, Nil)
Diagnostics 测试:   17 PASS (CollectDiagnostics x5, FormatLossHeader x4, ApplyLossPolicy x5, ConversionError x3)
Policy 测试:         5 PASS (LoadPolicy x4, ModeFor)
SSE 测试:           10 PASS (SingleLine, MultiLineData, WithEvent, PartialRead, ExceedsLimit, DONE, Comments, MultipleFrames, EmptyLines, OnlyEvent)
Stream 测试:         7 PASS (BasicText, ToolUse, UpstreamReadError, ClientDisconnect, FinishBeforeUsage, NoDoneMarker, OutputFormat)
```

### 全仓回归 (go test ./... -count=1 -short)

| 模块 | 测试数 | 状态 |
|------|--------|------|
| internal/protocol | 50 | OK 全部通过 |
| internal/core | 97 | OK 全部通过 |
| internal/providers | 297 | OK 全部通过 |
| internal/gateway/* (non-handler) | 393 | OK 全部通过 |
| internal/controlplane/* | 494 | OK 全部通过 |
| internal/errors, notification, observability | 429 | OK 全部通过 |
| tests/ | 2,088 | OK 全部通过 |
| **总计** | **~3,848** | **100% 通过 (可编译部分)** |

### 预存编译失败（非本次引入）

以下包因 relaykit 集成进行中而存在编译错误，均与本次 benchmark 无关：

- `internal/gateway/handler` — `messages.go` 中 `bytes` 和 `types` 未使用的 import
- `internal/gateway/server` — `router.go` 中 `setup` 函数签名不匹配 + `SetProtocolPolicy` 方法缺失
- `cmd/gateway`, `tests/gateway/handler` — 依赖上述包
- `test/awsbisect`, `test/bedrock_model_id_check` — 预存问题
- `internal/controlplane/repository` — 1 个 SQLite 迁移语法错误（预存）

**Legacy 路径完全未受影响。**

---

## 4. Benchmark 结果

**环境**: Apple M2 Pro, macOS arm64, Go 1.24

| Benchmark | ns/op | B/op | allocs/op | 吞吐 |
|-----------|-------|------|-----------|------|
| Bridge_ClaudeToChat | **2,955** | 4,922 | 38 | ~400K ops/s |
| Bridge_ClaudeToChat_WithTools | **62,452** | 52,028 | 869 | ~19K ops/s |
| SSEReader_SingleFrame | **5,399** | 65,656 | 5 | ~220K ops/s |
| SSEReader_100Frames | **14,273** | 83,168 | 302 | ~88K ops/s |
| StreamAdapter_10Chunks | **52,574** | 90,408 | 252 | ~23K ops/s |
| StreamAdapter_100Chunks | **356,652** | 204,465 | 1,964 | ~3.4K ops/s |
| CollectDiagnostics_50Diags | **1,769** | 6,696 | 4 | ~700K ops/s |

### 关键发现

1. **E2E Bridge (ClaudeToChat)** 性能优异：~3us/op，38 allocs，瓶颈在 relaykit JSON 解析与结构映射。
2. **Tools 转换** 显著增加开销：5 个 tool 定义使 ns/op 从 3us 涨到 62us（~21x），allocs 从 38 涨到 869（~23x），主因是 tool schema JSON 解析与 map 构建。
3. **SSE 读取** 非常轻量：单帧 ~5us，100 帧 ~14us（每帧 ~143ns 摊销），主要分配来自 bufio.Scanner 缓冲区。
4. **流式转换** 的主要成本在 relaykit 的 ConvertStreamResponseChunk：10 chunk 52us，100 chunk 357us（线性缩放，每 chunk ~3.5us）。
5. **诊断合并** 几乎零成本：50 个诊断仅 ~1.8us / 4 allocs。

---

## 5. 已知限制

1. **预存编译失败**: handler/server 包因 relaykit 集成进行中无法编译（非本次引入）。
2. **Benchmark 不覆盖 Gemini 路径**: 当前仅覆盖 Claude->OpenAI Chat 方向。
3. **StreamAdapter benchmark 每次创建真实 pipe**: goroutine 调度开销包含在测量中，实际生产环境中影响会分摊到每个请求。
4. **SSEReader benchmark 复用相同输入**: 不同帧大小模式可能表现出不同的分配特性。

---

## 6. 下一步 (R2)

1. **Outbound 转换**: 实现 OpenAI Chat Response -> Claude Response 的响应回程桥接（`BridgeChatResponseToSource` 已有雏形，需完善流式回程）。
2. **Native 路径**: 为非 OpenAI 上游实现原生转换路径，跳过 Chat Completions 中间格式。
3. **补齐 handler 集成**: 完成 `messages.go` 和 `router.go` 中的 relaykit Policy 注入与 wire。
4. **Gemini 路径 benchmark**: 添加 Gemini 协议的 benchmark 覆盖。
5. **Shadow mode E2E 测试**: 实现双路径并行对比的集成测试。
