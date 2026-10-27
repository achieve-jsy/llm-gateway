# Anthropic ↔ OpenAI 流式工具调用参数转换规范

> 本文是"网关把上游 OpenAI 流式 `tool_calls` 转成下游 Anthropic `tool_use` 流"这一转换层的
> 事实源：协议约束、各中转实现对照、本项目状态机、回归用例与已知限制。
> 任何改动 `internal/gateway/handler/anthropic_stream.go` 或
> `internal/providers/chat_via_responses_stream.go` 的人，先读本文再动手。

## 1. 为什么需要这份文档

2026-10-20 现网事故：Claude Code 经网关（Anthropic `/v1/messages`）调用 `bf-glm-5.3-flash`
（后端实际是 h1004 上的 SGLang 0.5.22 + DeepSeek-V4.1-Flash-1017）时，每次工具调用的
`tool_use.input` 都是 `{}`，客户端报：

```
InputValidationError: Bash failed due to the following issue:
The required parameter `command` is missing
```

模型反复重试、任务无法推进（"GLM 70 分钟完成的任务，8 小时还没跑完"）。非流式请求、以及
参数分多块流出的大模型（官方 `deepseek-v4.1-flash-official`、`deepseek-v4-flash`）都正常，
**只有"首块即带完整参数"的后端会踩中**。

根因不在 SGLang：OpenAI 流式协议里，同一个 `index` 的工具调用每个分块里的
`function.arguments` 都是**待拼接的片段，第一块也不例外**。SGLang 把一次调用的
`id`/`name`/`arguments` 全放在同一个分块里，是完全合法的输出；是网关的转换层错误地假定
"首块参数一定为空"并把首块参数丢了。

## 2. 两侧协议事实（基线）

### 2.1 OpenAI 侧：`arguments` 无脑拼接，首块没有特权

- `openai-python` 的 `accumulate_delta`：`tool_calls[i].function.arguments` 一律
  `+=` 追加，首块不做任何特判；`index` 必填，`id`/`name`/`arguments` 均可选。
- 官方服务习惯在首块放**空串**，但协议不要求。SGLang、vLLM 部分工具解析器、缓存/重放响应
  都可能一次性给出全量参数。
- 一个 chunk 里可以同时出现多个并行工具调用（不同 `index`），每个都可能自带完整或部分参数。

### 2.2 Anthropic 侧：`partial_json` 是字符串分片，最终必须能解析成 JSON 对象

- `content_block_start`（`type: tool_use`，`input: {}`）→ 若干 `content_block_delta`
  （`delta.type = input_json_delta`，`delta.partial_json` 为片段）→ `content_block_stop`。
- 客户端把同一块的 `partial_json` 顺序拼接后解析成工具入参。拼出来为空或不是合法 JSON 对象，
  Claude Code 的参数校验就会失败。
- 官方流示例本身就会发一条 `partial_json: ""`，所以空串 delta 合法，无需特判。

### 2.3 客户端 SDK 的累加器决定了哪些实现"能跑"

`anthropic-sdk-typescript`（Claude Code 同源）的 `MessageStream` 累加逻辑：

```ts
case 'content_block_start':
  snapshot.content.push({ ...event.content_block });   // ← push！index 必须等于当前数组长度
  return snapshot;
case 'content_block_delta': {
  const snapshotContent = snapshot.content.at(event.index);
  // input_json_delta: jsonBuf = 旧buf + partial_json；jsonBuf 为空 → input 恒为 {}
}
```

两个直接后果：

1. **`content_block_start` 的 index 必须密集升序**（0,1,2…）。这正是"块索引要与上游
   `tool_calls[].index` 解耦、自己按开块顺序分配"的原因（上游 index 可能乱序/跳号）。
2. `jsonBuf` 为空时 `input` 就是 `{}` —— 与事故现象完全一致。

## 3. 各中转实现对照（结论：没有一家丢首块参数）

| 项目 | 语言 | 新调用判据 | 首块带参数时 | 块索引分配 | 多块并行 open |
|---|---|---|---|---|---|
| **new-api** | Go | `index` 优先 + `id` 辅助 | **会发**：先 `content_block_start`，紧接着把积压的 `PendingArguments` 作为一条 `input_json_delta` 发出 | `base + len(ToolCalls)`（按到达顺序密集） | **是**（收尾统一 stop） |
| **LiteLLM** | Python | `name` 非空（不看 index） | 会发：把触发块的 delta 重新入队再发（注释点名 xAI/Gemini 首块带 args） | 自增 | 否（hold-and-release 保证顺序） |
| **vLLM** | Python/Go | `index`→id 映射，id 变化且 name 非空 | 会发（`Handle initial arguments if present`） | 自增 | 是 |
| **llama.cpp** | C++ | 上游 index 推导 | 会发（start 与 delta 同轮） | `(thinking?1:0)+(text?1:0)+index` | 是 |
| **claude-code-router** | TS | `index` 未知即新调用 | 会发（start 与 args delta 是两个**并列 if**） | 原子自增 | 会 close 前一个 |
| **CLIProxyAPI** | Go | index，带缺失兜底 | 会发，但参数攒到收尾一次性发（牺牲增量） | 自增 | 否（严格单块） |
| **one-api** | Go | — | 根本没有 `/v1/messages` 方向，无此实现 | — | — |
| **本项目（修复前）** | Go | `tc.ID != ""` | **丢弃**（只发 `input: {}`） | 单份 `openBlockIndex` | 否，且并行会串台 |

主流写法的最小共识（本项目早期实现全都违反了）：

1. **不要用 `id != ""` 当"是否新调用"的判据**——首块带参数时参数走 start 分支被吞，而 id 只出现在首块，
   续块只能靠 `else` 分支，逻辑天然分裂。
2. **不要用 `else if` 短路**把"开块"和"发参数"绑成互斥分支；参数应该在"状态就绪"时发，
   而不是"块恰好开着"时发。
3. **块索引与上游 index 解耦**，按开块顺序密集自增。
4. **name 晚到要等**（或缺省占位后只 start 一次），不能先发空 name 再补一个 start。

## 4. 本项目实现（`anthropic_stream.go`）

### 4.1 状态

```go
type toolBlockState struct {
    index       int             // 上游 tool_calls[].index
    id          string          // 上游 id（缺失时用 toolu_<blockIndex> 兜底）
    name        string          // 上游 function name（可能晚于 id 到达）
    blockIndex  int             // 自己的 Anthropic content block index；-1 = 尚未开块
    pendingArgs strings.Builder // name 未到时的参数暂存
    closed      bool            // 是否已发 content_block_stop
}
```

`state.toolBlocks map[int]*toolBlockState` 按上游 index 索引，`state.toolBlockOrder` 记录首次出现顺序；
`state.nextBlockIndex` 是**唯一**的块索引分配器（文本块与工具块共用，保证密集升序）。

### 4.2 处理单个 `tool_calls` 分块（`handleToolCallDelta`）

1. 该 index 的块已 closed（例如同一 index 已经跑完一次调用）→ 一律重建状态、另开新块：
   closed 的块无法再追加 delta，复用状态会把整次调用静默丢掉。
2. 同一 index 出现新 `id`（与已知 id 不同）→ 同样视为新调用，先关闭旧块、另起新状态。
3. 记录 `id` / `name`。
4. 尚未开块：没有 `name` 就把 `arguments` 暂存进 `pendingArgs`（避免发出 name 为空的 tool_use 块）；
   有 `name` 立即 `openToolBlock`（顺带给打开的文本块收尾）。
5. **无论首块还是续块**，把 `pendingArgs + arguments` 作为一条 `input_json_delta` 发到自己的块。
   这正是事故的修复点。

### 4.3 收尾（`closeAllBlocks`）

1. 收集所有仍打开的块（文本块 + 工具块），按 block index **升序**发 `content_block_stop`。
2. 再把"name 始终未到但有参数"的调用补开出来（`openToolBlock` + `input_json_delta` + stop）并
   `slog.Warn`：这类块只能等到真正收尾才确定"没有 name 可等"，提前发会得到 name 为空的半成品块。

finish_reason / 末尾 usage-only chunk / `[DONE]` / EOF / scanner error 五条路径都走这里，
由 `state.finished` 守卫保证 `message_delta`+`message_stop` 只发一次；`finished` 之后不再开新块。

`inferStopReason()` 决定 `stop_reason`：

- 上游给了 `finish_reason` 就优先用映射结果；但若映射成 `end_turn` 而流里已有 `tool_use` 块，
  改判 `tool_use`（部分后端把 tool_calls 报成 `finish_reason:"stop"`，`end_turn` 会让 agent
  循环直接结束、工具永不执行）；
- 保留 `max_tokens`：参数可能被截断，客户端应继续生成而不是执行半个工具调用；
- 上游完全没给 `finish_reason` 时，有 `tool_use` 块即 `tool_use`，否则 `end_turn`。

### 4.4 事件形态（单 chunk 内两个并行调用，实测输出）

```
event: message_start
event: content_block_start   index=1  tool_use id=call_1 name=Bash     input={}
event: content_block_delta   index=1  input_json_delta {"command": "git status", ...}
event: content_block_start   index=2  tool_use id=call_2 name=TodoWrite input={}
event: content_block_delta   index=2  input_json_delta {"todos":[]}
event: content_block_stop    index=1
event: content_block_stop    index=2
event: message_delta         stop_reason=tool_use
event: message_stop
```

**取舍说明**：内容块（文本块、多个并行工具块）会同时处于 open 状态，`content_block_stop` 推迟到
收尾按 block index 升序统一发。这偏离了 Anthropic 官方"一次只开一个块"的示例，但与
new-api / vLLM / llama.cpp 的做法一致；官方 SDK 按 `index` 累加（`push` 只要求索引密集升序），
实测 Claude Code 正常消费。

为什么不让"后到的内容类型"把前一个块关掉：那是"用块的 open 状态当参数发不发的守卫"的变体。
一旦文本 delta 把工具块关掉，"参数被文本打断"的流就会把尾部参数丢在已关闭的块上（拼出非法
JSON）；同理并行调用的块被关掉会把后续参数丢进别人的块（串台）。所以规则统一为：**块只在
收尾时关，或在该 index 明确开始新调用时关**。

代价有两个：并行工具块的 stop 事件不再即时；严格顺序方案（LiteLLM 的 hold-and-release）需要
在网关里缓冲后到的调用、等前一块 close 后再 start+flush，实现复杂度更高。这里选择与多数 Go
中转实现一致的简单方案。

## 5. 另一个方向：Responses API → OpenAI Chat（`chat_via_responses_stream.go`）

同一类假设在另一条通道上也有：`ResponsesToChatStreamConverter` 假定参数一定通过
`response.function_call_arguments.delta` 分片流出，于是

- `response.output_item.added` 里 `item.arguments` 被硬编码成空串丢弃；
- `convertEvent` 完全没有 `response.output_item.done` 分支；
- `response.function_call_arguments.done`（只给完整参数）同样没有入口；
- `response.completed` 不回填 `output[*].arguments`。

修复方式：把 `added` / `output_item.done` / `function_call_arguments.done` / `completed` 四处都当作
"参数候选来源"，用 `toolArgsSent`（按归一化 item id，`id` 优先、缺失退回 `call_id`）做
**once-only 门控**：先到者生效、后到者跳过，既不丢参数也不翻倍；空参数在收尾补 `"{}"` 收成合法 JSON；
并行调用按 `tool_calls[].index` 独立门控。

## 6. 回归用例（改这段代码必须全绿）

`internal/gateway/handler/anthropic_stream_test.go`：

| 用例 | 钉住的行为 |
|---|---|
| `TestAnthropicStream_ToolArgs_FirstChunkWithFullArguments` | 首块 id+name+全量参数：start 之后**紧接**一条 `input_json_delta`，拼接 == 原始 arguments，且与非流式 `ChatResponseToAnthropic` 的 `input` 字段级相等 |
| `..._FirstChunkFullArgs_ExactEventSequence` | 完整事件序列与 index 严格正确 |
| `..._ParallelCallsInSingleChunk` | 单 chunk 内 3 个调用：3 块、index 依次 0/1/2、参数不串块 |
| `..._TraditionalChunked_Regression` | 传统"首块空 + 多块续传（无 id）"行为不变 |
| `..._EmptyFirstChunkThenContinuationWithoutID` | 首块显式空串 + 无 id 续传 |
| `..._InterleavedContinuationAcrossParallelBlocks` | 并行交错续传：每个 delta 落在自己块的 start→stop 内 |
| `..._NameInFirstChunkArgsInSecond` | name 先到、参数后到 |

`internal/gateway/handler/anthropic_stream_toolargs_edge_test.go`：

| 用例 | 钉住的行为 |
|---|---|
| `..._TextDeltaAfterToolBlock` | 工具块打开期间到达的文本 delta 不被丢，且文本块不与工具块共用 index |
| `..._NoIDWithNameAndArgs` | 上游不给 id 也能开块，id 走 `toolu_<blockIndex>` 兜底 |
| `..._NameNeverArrives` | name 始终缺失：收尾阶段补开块并保住参数 |
| `..._IndexReusedByNewCallID` | 同 index 换新 id → 切成两个块，参数各归其主 |
| `..._ParallelThenNoFinishChunk` | 没有 finish_reason（只有 `[DONE]`）：并行块按升序收尾、只发一次 `message_stop` |

`internal/gateway/handler/anthropic_stream_toolargs_regression_test.go`（评审回合补的回归）：

| 用例 | 钉住的行为 |
|---|---|
| `..._TextInterruptsArguments` | 参数被文本 delta 打断（args→text→args，续传无 id）时尾部参数不能丢；修复过程中曾回归成 `{"comm` 这种非法 JSON |
| `..._SameIndexNewIDAfterText` | 文本打断后同 index 换新 id → 两块；曾因复用已关闭的 slot 导致第二次调用整体消失 |
| `..._FinishReasonStopWithToolBlock` | 上游把 tool_calls 报成 `finish_reason:"stop"` 时 stop_reason 仍必须是 `tool_use` |
| `..._TextAndToolCallsInSameChunk` | 单 chunk 同时带文本与 tool_calls：文本块 0、工具块 1，两者都完整 |
| `..._SameIndexThreeSequentialCalls` | 同 index 连续三次调用 → 三块、参数不串 |
| `..._IDArrivesAfterBlockStart` | id 晚于 name/参数到达：块已用兜底 id 打开，不能另开块 |
| `..._EmptyArgumentsToolInputEmptyObject` | 空参数工具：无 `input_json_delta`、`input={}`、stop_reason 仍是 tool_use |
| `..._TextToolText` | text → tool → text：两个文本块（index 0/2）+ 工具块（index 1），文本拼接完整、三个块都收尾 |

`internal/gateway/handler/anthropic_stream_choice_test.go`（多 choice 与错误路径）：

| 用例 | 钉住的行为 |
|---|---|
| `..._MultiChoice_OnlyFirstChoiceConverted` | n=2 时文本只来自 `choices[0]`（修前拼成 `"ab"`） |
| `..._MultiChoice_ToolCallsOnlyFromFirstChoice` | n=2 时工具调用只来自 `choices[0]`（修前出现 2 个 tool_use 块） |
| `..._MultiChoice_EmptyChoices` | `choices: []` 不 panic、不发内容块 |
| `..._MultiChoice_FinishReasonFromIgnoredChoice` | finish_reason 只挂在非 0 choice 上时仍要取到（否则 `max_tokens` 被误报成 `end_turn`） |

`internal/gateway/handler/anthropic_stream_error_path_test.go`：

| 用例 | 钉住的行为 |
|---|---|
| `..._UpstreamReadError_NoNormalTermination` | 上游读失败：错误沿 pipe 传给调用方，不补发 `message_delta`/`message_stop`，也不给仍打开的工具块补 `content_block_stop` |
| `..._ReadErrorBeforeAnyChunk` | 一帧都没读出来就失败时同样不发空壳收尾 |
| `..._ClientCloseMidStream` | 客户端提前 `Close`：`Read` 报错、转换 goroutine 退出，不 panic |

`tests/gateway/handler/messages_stream_toolargs_test.go`：HTTP 层端到端（`/v1/messages` + stream=true）
回放事故 SSE，断言客户端收到的 tool_use 块 id/name/完整 input 与事件顺序。

`tests/providers/responses_stream_toolargs_test.go`（组件层）：参数只在 added 里给 / 传统 delta 分片 /
added 为裸 `"{}"` 占位三种上游形态，断言下游 `arguments` 拼接后是合法 JSON。

`internal/providers/chat_via_responses_stream_toolargs_test.go`：A 首帧即全量 / B delta 回归 /
C 仅 completed / D `output_item.done`（含缺 added 的兼容形态）/ E 并行不串台 / F 空参数收口 /
G `"{}"` 占位不重复拼接 / H delta-only 流补 id/name / I name 晚到由 done/completed 补 /
J id 与 call_id 全缺时合成 `call_<idx>`。

**新增测试的自我验证约定**：每个修复都要能"退回修复就变红"。文档里列出的关键用例（首块全量参数、
文本打断参数、同 index 换新 id、`finish_reason:"stop"` + tool 块、`"{}"` 占位、name 晚到、
id 兜底）都已用"临时退回对应改动"的方式确认过会失败，避免写出恒绿的实现无关断言。

## 7. 已知限制

1. **同一个上游 index 被多次使用**：块关闭后不能再追加，因此"已关闭 slot 又来数据"一律按
   **新的一次调用**处理（另开块）。这覆盖了上游复用 index / 省略 index（恒为 0）的形态；
   真正的"同一次调用参数在块关闭后继续到达"无法表达，只能丢弃并 `slog.Warn`（实际不会发生：
   OpenAI/vLLM/SGLang 的工具调用参数按 index 顺序单调流出）。
2. **多 choice（`n > 1`）只转换第一个**：Anthropic 的一条 `message` 只能装一份内容，所以
   `choices[1..]` 被丢弃并 `slog.Warn`（带 choices 数量）。这比"把多个 choice 的 delta 灌进同一份状态"
   安全：后者会让文本串台、并行工具调用的参数互相污染。若确有 per-choice 需求，只能拆成多条 message
   （协议层不支持），或由调用方显式发起多次请求。
3. **非法 JSON 的收口**：流式通道无法在收尾把非法 JSON 改写成 `{}`（客户端已按分片累加）；
   非流式通道（`core.responseMessageToAnthropicBlocks`）对非法 JSON 有"包成 JSON 字符串"的兜底。
4. **LiteLLM 的 hold-and-release 严格顺序**未采用，见 §4.4 取舍说明。

## 8. 排障速查

- 判断某个后端是不是"首块即全量参数"：抓上游 SSE，看 `tool_calls[0]` 的第一个分块是否
  `function.arguments` 非空（或 `id`/`name`/`arguments` 全在同一块）。
- 网关日志关键字：`dropping tool_call arguments for a closed block`（交错/回填类异常）、
  `tool_call never got a function name`（上游始终没给 name，已按空 name 补块）。
- 客户端侧复现：非流式正常、流式 `tool_use.input` 为空 → 优先怀疑这条转换链。

## 9. 参考来源

- `openai-python` `accumulate_delta`（arguments 一律 `+=`）
- `anthropic-sdk-typescript` `MessageStream.#accumulateMessage`（`content_block_start` → `push`；
  `withLazyInput` 在 jsonBuf 为空时 `input` 恒为 `{}`）
- new-api（Calcium-Ion/new-api）：`relay/channel/claude` 相关流式转换、`PendingArguments` 与
  `ToolCallBaseIndex + len(ToolCalls)`
- LiteLLM（BerriAI/litellm）：首块带 args 重新入队的注释与回归测试、`_CombinedChunkSplitter`
- vLLM / llama.cpp 的 `/v1/messages` 实现、claude-code-router 的 transformer、CLIProxyAPI
- 详细源码摘录（含 commit SHA 与行号）见内部调研报告
  `reports/anthropic-stream-tool-args/industry-implementation-research.md`（`reports/` 已 gitignore，
  属本地分析材料）与同目录 `codebase-audit.md`
