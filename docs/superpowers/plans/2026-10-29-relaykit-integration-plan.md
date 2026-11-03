# RelayKit 协议转换层集成 — 实施计划

> **For agentic workers:** Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 集成 relaykit v0.3.5，替代 Messages/Responses 入口的手写协议转换。

**Architecture:** 新增 internal/protocol 包作为 relaykit ↔ core 的唯一桥接层；handler 通过 policy 开关逐条启用新路径；chat 热路径与已有 provider 不动。

**Tech Stack:** Go 1.26.1, gin, relaykit v0.3.5, encoding/json (Codec 可注入), stdlib testing

**Spec:** docs/superpowers/specs/2026-10-29-relaykit-integration-design.md

## Global Constraints

- Go 版本: 1.26.1
- 依赖新增上限: relaykit + 其传递依赖 (gjson/sjson/lo/uuid/testify)
- core 不反引 protocol; protocol 不引 handler/providers/Gin
- 文件不超过 1000 行; 接口 7 方法主接口不变
- 测试模式: 手写 fake + 表驱动; 外部测试包 (package xxx_test)
- Conventional Commits; 中文注释

## Review Focus

1. Messages 请求在 legacy 默认下行为完全不变
2. 流式转换器正确传播上游读错误与下游取消
3. usage 在相同录制输入下与 legacy 完全一致
4. failover 跨尝试时不混用 Session 状态
5. 单事件超过 2 MiB 时明确报错, 不静默丢弃

---

### Task 0: 依赖与包骨架

**Files:**
- Modify: go.mod
- Create: internal/protocol/doc.go
- Create: internal/protocol/session.go
- Create: internal/protocol/policy.go
- Create: internal/protocol/policy_test.go

**Interfaces:**
- Produces: Session, NewSession, Policy, LoadPolicy, TargetConfig

- [ ] **Step 1: 编写 policy 测试**

文件: internal/protocol/policy_test.go (package protocol_test)

测试: TestLoadPolicy_DefaultLegacy — 空配置返回所有入口 legacy
测试: TestLoadPolicy_ActiveForKey — Key 白名单匹配返回 active
测试: TestLoadPolicy_ShadowForEntry — 按入口灰度 shadow

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/protocol/... -v -run TestLoadPolicy`
Expected: FAIL (package not found)

- [ ] **Step 3: 实现 policy.go**

文件: internal/protocol/policy.go

- type Policy struct (immutable, loaded once at startup)
- DefaultMode: legacy | shadow | active
- PerKey map[string]Mode  
- PerEntry map[string]Mode (messages / responses / chat)
- func LoadPolicy(cfg PolicyConfig) (*Policy, error)

文件: internal/protocol/session.go

- type Session struct: convmeta.Values + sourceFormat + targetFormat + diagnostics + toolState + id
- func NewSession(source, target RelayFormat, opts convmeta.Options, model, upstreamModel string) *Session

文件: internal/protocol/doc.go (包文档)

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/protocol/... -v -run TestLoadPolicy`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/protocol/ go.mod go.sum
git commit -m "feat(protocol): add Policy, Session types and package skeleton"
```

---

### Task 1: 请求桥 — Claude Request → ChatRequest

**Files:**
- Create: internal/protocol/request.go
- Create: internal/protocol/request_test.go  
- Create: internal/protocol/bridge.go
- Create: internal/protocol/bridge_test.go

**Interfaces:**
- Consumes: Session (Task 0), relaykit relayconvert.ConvertRequest, dto.ClaudeRequest
- Produces: DecodeAndConvertRequest(session, rawJSON []byte) (any, error), BridgeToChatRequest(result any, ...) (*core.ChatRequest, error)

- [ ] **Step 1: 编写 bridge 测试 (contract tests)**

文件: internal/protocol/bridge_test.go (package protocol_test)

- TestBridgeClaudeToChat_BasicText: 简单文本请求 → ChatRequest
- TestBridgeClaudeToChat_WithTools: tool_use 定义 → ChatRequest tools
- TestBridgeClaudeToChat_WithThinking: thinking config → ChatRequest reasoning
- TestBridgeClaudeToChat_DocumentBlock: document 内容块 → 诊断或转换
- TestBridgeClaudeToChat_NilTemperature: temperature 不出现时桥不注入零值
- TestBridgeClaudeToChat_TopP: top_p → ExtraFields

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/protocol/... -v -run TestBridge`
Expected: FAIL

- [ ] **Step 3: 实现 request.go + bridge.go**

文件: internal/protocol/request.go
- func DecodeClaudeRequest(rawJSON []byte) (*dto.ClaudeRequest, error)
- func DecodeResponsesRequest(rawJSON []byte) (*dto.OpenAIResponsesRequest, error)
- func ConvertRequest(session *Session, request any) (*relayconvert.RequestResult, error)
- func ConvertRequestVia(session *Session, request any, path ...RelayFormat) (*relayconvert.RequestResult, error)

文件: internal/protocol/bridge.go  
- func BridgeToChatRequest(session *Session, result *relayconvert.RequestResult) (*core.ChatRequest, error)
  从 relayconvert.RequestResult.Value (GeneralOpenAIRequest) 映射到 core.ChatRequest
- func BridgeChatResponse(session *Session, chatResp *core.ChatResponse) (*relayconvert.ResponseResult, error)
  反向: ChatResponse → GeneralOpenAIRequest 的响应 DTO → ConvertResponse 回到源格式

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/protocol/... -v -run TestBridge`
Expected: PASS (all bridge contract tests)

- [ ] **Step 5: Commit**

```
git add internal/protocol/request.go internal/protocol/bridge.go internal/protocol/*_test.go
git commit -m "feat(protocol): add request decoding, relaykit conversion, and ChatRequest bridge"
```

---

### Task 2: 流式转换适配器

**Files:**
- Create: internal/protocol/sse.go
- Create: internal/protocol/sse_test.go
- Create: internal/protocol/stream.go
- Create: internal/protocol/stream_test.go

**Interfaces:**
- Consumes: Task 0 Session, Task 1 bridge, relaykit ResponseStreamState, core.ChatResponse
- Produces: NewStreamAdapter(upstream io.ReadCloser, session *Session, ...) io.ReadCloser

- [ ] **Step 1: 编写 SSE 读取器测试**

文件: internal/protocol/sse_test.go

- TestReadSSEFrame_SingleLine: "data: {...}\n\n"
- TestReadSSEFrame_MultiLineData: 多行 data 拼接
- TestReadSSEFrame_WithEvent: "event: ping\ndata: {}\n\n"
- TestReadSSEFrame_PartialRead: 分片读取重新组装
- TestReadSSEFrame_ExceedsLimit: 单事件超过 2MiB → error
- TestReadSSEFrame_EmptyLines: 多余空行正确跳过
- TestReadSSEFrame_DONE: 收到 [DONE] 标记

- [ ] **Step 2: 运行测试确认失败**

Expected: FAIL

- [ ] **Step 3: 实现 sse.go**

文件: internal/protocol/sse.go

- type SSEFrame struct { Event string; Data []byte }
- type SSEReader struct { scanner, buffer, maxFrameSize }
- func NewSSEReader(r io.Reader, maxFrameSize int) *SSEReader
- func (r *SSEReader) ReadFrame() (*SSEFrame, error)

- [ ] **Step 4: 实现 stream.go 测试**

文件: internal/protocol/stream_test.go

- TestStreamAdapter_BasicText: 3 个 text delta chunk → Anthropic 输出
- TestStreamAdapter_ToolUse: tool_use delta → Anthropic content_block_start/delta/stop
- TestStreamAdapter_UsageChunk: 尾帧 usage → message_delta + message_stop  
- TestStreamAdapter_UpstreamError: 上游 read error → pipe error propagation
- TestStreamAdapter_ClientDisconnect: 下游关闭 → 上游也被关闭
- TestStreamAdapter_MultipleToolsInterleaved: 多工具交错
- TestStreamAdapter_FinishBeforeUsage: finish_reason 在 usage 之前到达
- TestStreamAdapter_NoDoneAfterFinish: 有 finish 无 [DONE] 仍 finalize

- [ ] **Step 5: 实现 stream.go**

文件: internal/protocol/stream.go

- type StreamAdapter struct: upstream, pipe, session, state, wg
- func NewStreamAdapter(upstream io.ReadCloser, session *Session, model string, estimatedInputTokens int) io.ReadCloser
- 内部: goroutine 读上游 SSE → 逐 chunk 调 ConvertStreamResponseChunk → 输出 Anthropic SSE
- 正常结束: FinalizeStreamResponse
- 异常: 传播错误, 不输出假的 message_stop

- [ ] **Step 6: 运行流式测试确认通过**

Run: `go test ./internal/protocol/... -v -run TestStream`
Expected: PASS

- [ ] **Step 7: Commit**

```
git add internal/protocol/sse.go internal/protocol/stream.go internal/protocol/*_test.go
git commit -m "feat(protocol): add SSE frame reader and stream adapter"
```

---

### Task 3: 诊断与错误映射

**Files:**
- Create: internal/protocol/diagnostics.go
- Create: internal/protocol/diagnostics_test.go
- Modify: internal/gateway/handler/messages.go (仅新增错误路径)

**Interfaces:**
- Consumes: Task 1 Session, relaykit ConversionDiagnostic
- Produces: CollectDiagnostics, FormatLossHeader, ConversionError

- [ ] **Step 1: 编写诊断测试**

文件: internal/protocol/diagnostics_test.go

- TestCollectDiagnostics_NoLoss: 空诊断列表 → 空摘要
- TestCollectDiagnostics_SingleWarning: 单个 warning → X-Protocol-Loss 含 code
- TestCollectDiagnostics_MultipleMixed: warning + error → 均有
- TestFormatLossHeader_Truncation: 超过 512 bytes 截断
- TestConversionError_HTTPStatus: safe/strict 策略 → 400

- [ ] **Step 2 & 3: 实现 + 通过**

文件: internal/protocol/diagnostics.go

- type ConversionError struct { Diagnostics []ConversionDiagnostic; HTTPStatus int }
- func CollectDiagnostics(session *Session, relayDiags []ConversionDiagnostic, bridgeDiags []ConversionDiagnostic) []ConversionDiagnostic
- func FormatLossHeader(diags []ConversionDiagnostic) string
- func ApplyLossPolicy(policy ConversionLossPolicy, diags []ConversionDiagnostic) error

- [ ] **Step 4: Commit**

```
git add internal/protocol/diagnostics.go internal/protocol/diagnostics_test.go
git commit -m "feat(protocol): add conversion diagnostics and loss policy"
```

---

### Task 4: Messages Handler 集成

**Files:**
- Modify: internal/gateway/handler/messages.go
- Modify: internal/gateway/server/router.go
- Modify: internal/gateway/config/config.go
- Modify: config/dev.yaml

**Interfaces:**
- Consumes: Task 0-3 (protocol 包全量)
- Mode: legacy 默认 — 新路径通过 policy + config 开启

- [ ] **Step 1: 编写 handler 集成测试**

文件: tests/gateway/handler/messages_relaykit_test.go

- TestMessagesHandler_LegacyDefault: policy=legacy → 走旧路径, 行为不变
- TestMessagesHandler_RKActive_BasicText: active → 走 relaykit 桥, 简单文本往返
- TestMessagesHandler_RKActive_ThinkingConfig: thinking 配置正确传递
- TestMessagesHandler_RKActive_Streaming: 流式场景 SSE 输出正确
- TestMessagesHandler_RKActive_UpstreamError: 上游错误 → Anthropic error 格式

- [ ] **Step 2: 实现 handler 修改**

文件: internal/gateway/handler/messages.go

- HandleMessages 入口新增分支:
  if policy.ModeFor(key, "messages") != legacy → DecodeClaudeRequest → ConvertRequest → BridgeToChatRequest
- handleNonStream 新增: 响应回程调 BridgeChatResponse
- handleStream 新增: 用 NewStreamAdapter 替代 anthropicStreamAdapter (仅 new 路径)

文件: internal/gateway/config/config.go

- 新增 ProtocolConfig 结构体 + 从 viper 加载
- 默认值: mode=legacy

文件: config/dev.yaml

- 新增 protocol: 节示意

- [ ] **Step 3: 运行全部测试**

Run: `go test ./internal/protocol/... ./internal/gateway/handler/... ./tests/gateway/handler/... -v`
Expected: 所有测试 PASS (legacy 路径 0 行为变更)

- [ ] **Step 4: Commit**

```
git add internal/gateway/handler/messages.go internal/gateway/config/config.go config/dev.yaml tests/
git commit -m "feat(handler): integrate relaykit into MessagesHandler with policy gating"
```

---

### Task 5: Responses 共用桥 + E2E

**Files:**
- Modify: internal/gateway/handler/responses.go
- Modify: internal/providers/responses_adapter.go
- Create: tests/gateway/handler/responses_relaykit_test.go
- Create: tests/e2e/protocol_relaykit_test.go

- [ ] **Step 1: 编写 Responses 集成测试**

- TestResponsesHandler_RKActive_BasicConversation
- TestResponsesHandler_RKActive_Streaming
- TestResponsesHandler_RKActive_Failover

- [ ] **Step 2: 编写 E2E golden 测试**

文件: tests/e2e/protocol_relaykit_test.go

- TestE2E_ClaudeMessagesToChat: 完整 messages→chat→messages 往返 vs golden
- TestE2E_ResponsesToChat: responses→chat→responses 往返 vs golden
- TestE2E_StreamingConsistency: legacy vs rk 输出逐帧对比

- [ ] **Step 3: 运行 E2E**

Run: `go test ./tests/e2e/... -v -tags=e2e`
Expected: PASS

- [ ] **Step 4: Final commit**

```
git commit -m "feat(protocol): integrate Responses bridge and E2E golden tests"
```

---

### Task 6: 压测与回归验证

**Files:**
- Create: internal/protocol/bench_test.go

- [ ] **Step 1: 编写 benchmark**

- BenchmarkBridge_ClaudeToChat
- BenchmarkStreamAdapter_10Chunks
- BenchmarkStreamAdapter_100Chunks

- [ ] **Step 2: 运行回归测试套件**

Run: `go test ./... -count=1` (全仓, 除 integration tag)
Expected: 0 failures vs main baseline

- [ ] **Step 3: 记录基线性能**

Run: `go test ./internal/protocol/... -bench=. -benchmem -count=5`
Record: ns/op, B/op, allocs/op

- [ ] **Step 4: Final commit**

```
git commit -m "perf(protocol): add benchmarks and verify regression suite"
```
