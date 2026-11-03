# 原生旁路（零转换直发）

## 一句话

当目标渠道能直接吃下客户端原本的协议时，网关**不解码、不重组、不改写**请求体，
只把 `model` 换成路由解析后的实际模型名，然后原样发上游，响应原样回传。

## 为什么

老路径是「解码成 `core.ChatRequest` → 重新序列化」。这一趟往返会：

- 丢掉网关不认识的字段（各厂商的新参数、beta 特性）
- 改变类型精度（`int64` ↔ `float64`、大整数 ID）
- 凭空增加两次 JSON 编解码

原生旁路把这三样都消掉。代价是**不能做字段级改写**——所以它只用于协议完全对得上的情况。

## 架构

```
handler                      Router                        provider
────────                     ──────                        ────────
读原始 body
  │
  ├─ PrepareAuthorizedGates（调度/预算/配额，与老路径完全一致）
  │
  └─ ChatNative(ctx, req, rawBody, format)
                               │
                               ├─ resolve(model) → provider
                               ├─ NativeRelayFormatFor(model, format)
                               │    ↳ false：返回 ErrNativeNotSupported
                               ├─ rewriteNativeBodyModel(body, resolved)
                               └─ RelayNative(ctx, body) ──→ 原始字节直发
                                                              │
   ←────────────────── 原始响应字节 ────────────────────────────┘
```

三条铁律：

1. **降级只有一个信号**：`errors.Is(err, core.ErrNativeNotSupported)`。
   其他错误立即返回，**不得二次打上游**（否则重复计费）。
2. **上游错误不包装哨兵**。上游 5xx 是真实错误，包装了会触发降级进而二次调用。
3. **model 必须在网关改写**。生产模型组名（`claude-sonnet-4-5`）与上游 ID
   （`global.anthropic.claude-sonnet-4-5-20250929-v1:0`）不同，provider 自己映射不了。

## 支持的渠道

| 原生格式 | 渠道 |
|---|---|
| Chat + Responses | openai、openrouter、azure、litellm、zai、minimax、vllm、bailian、oracle、deepseek、groq、xai、ollama |
| Gemini | gemini、vertex（vertex 委托给内部 gemini provider） |
| Claude | anthropic、bedrock |

### 新增渠道：问一句就能选对写法

**这个 provider 有没有 `*openai.CompatibleProvider` 字段？**

| 情况 | 要写什么 | 参考实现 |
|---|---|---|
| 有，且是**匿名嵌入** | **零代码**，方法自动提升 | openai、openrouter、azure |
| 有，但是**具名字段** | 手写 3 个转发方法（各 3 行） | litellm、zai、minimax、vllm、bailian、oracle |
| 没有，自己持 `llmclient.Client` | 调 `providers.NativeFormatForOpenAI` / `RelayNativeRaw` / `RelayNativeRawStream` | deepseek、groq、xai、ollama |
| 内部还有别的 provider 能干活 | 转发给它 | vertex → gemini |

**为什么会有四种**：Go 的方法提升只对**匿名嵌入**生效。`compatible *openai.CompatibleProvider`
是具名字段，不会提升，所以必须显式转发。这不是设计选择，是语言规则。
改成匿名嵌入能省掉 3 行，但会把 CompatibleProvider 的**全部**方法（包括不该暴露的）
一起提升到本 provider 上，得不偿失。

**漏写会怎样**：编译失败，不是静默降级。每个 provider 包里都有一行编译期断言：

```go
var _ core.NativeRelayProvider = (*Provider)(nil)
```

具名字段忘了转发时这行直接报错。新增渠道**记得也加上这行**——它是这个能力唯一
的防漏机制，没有它，漏写的方法只会在运行时表现为「悄悄退回转换路径」。

### 能力判定：默认按模型，唯一的例外要留证据

`NativeRelayFormatFor(model, format)` **默认必须按模型判断**：AWS 一个渠道同时挂
`global.anthropic.claude-*`（能原生走 Claude）和 `openai.gpt-*`（只能转换），
按渠道判会错。

唯一例外：**该渠道对这条 format 下的全部模型都支持**，渠道内不存在能力差异。
deepseek / groq / xai / ollama 属此类 —— 它们是纯 OpenAI 兼容服务，名下所有模型
都吃 `/chat/completions` 与 `/responses`，逐模型判断恒真。四家共用
`providers.NativeFormatForOpenAI(model, format)`，实现里显式 `_ = model`
并注明依据。

这个例外不是「可以随手忽略参数」，它有两个硬条件：

1. **依据写在实现处**，不是留在调用方各自 `_ string` 里；
2. **依据失效就改代码** —— 某家开始提供只支持 Chat 或只支持 Responses 的模型，
   就地改成按模型分支，不能留给上层打补丁。

`tests/providers/openai_compat_native_format_test.go` 钉住了这两条：
四家对 Chat/Responses 必须声明原生、对 Claude/Gemini 必须拒绝，且**判定结果与
model 无关**。哪天某家出现能力不同的模型，第三条会失败——那正是该改的信号。

## 启用开关

| 入站 | 默认 | 开启方式 |
|---|---|---|
| `/v1/chat/completions` | **关闭** | `protocol.Policy` 对该 Key/Entry 设 `active` |
| `/v1/messages` | 关闭 | 同上 |
| `/v1/responses` | 关闭 | 同上 |
| `/v1beta/models/*`（Gemini） | **开启** | 该路由是新增的，没有老路径可比 |

Chat 默认关闭的原因：原生路径下客户端未知字段会透传给上游（以前被结构体丢弃），
上游 4xx 也不再由老路径容错。这是行为变更，不该是默认值。

## 实测数据：它不是性能优化

原生旁路的价值主张容易想当然成「更快」，实测并非如此。基准在
`tests/gateway/handler/native_vs_conversion_bench_test.go`（`BenchmarkCompare_*`），
对照组是同一条 relaykit 管线，只切渠道是否实现 `NativeRelayProvider`：

| body | 原生 | 降级走转换 |
|---|---|---|
| 1.6KB | 94µs / 481 allocs | 104µs / 603 allocs |
| 23KB | **1.21ms** / 4848 allocs | **1.04ms** / 5014 allocs |

小 body 原生快约 10%，长上下文反而慢约 16%——`rewriteNativeBodyModel` 的
map 往返（实测 200µs@23KB）吃掉了省下的序列化。

**真正的差距在管线，不在旁路**：同场景下 legacy 管线 290µs、relaykit 管线 1.16ms，
即启用 relaykit 让网关自身 CPU 开销涨约 4 倍（绝对差 ~870µs，相对上游响应可忽略）。

所以原生旁路的定位是**字段保真**，不是加速。它省不掉入方向转换——
调度/预算/配额都需要 `ChatRequest`，所以 `handleMessagesRK` 在判定原生之前
必然已经完整转换过一次。

## 可观测

- call_log 的 `upstream_protocol` 列：`native_chat` / `native_responses` /
  `native_claude` / `native_gemini`；**空串代表走了转换路径**。据此算原生占比。
- trace span 有 `llm.native_relay=true` 属性。
- 计费：非流式原生响应由 `observeNativeUsage` 解析后回填 gin context，
  流式由 `streamUsageObserver` 包在上游流上。**两处都不能漏**，否则该请求按 0 计费。

## 降级目标（四条入站一致）

原生旁路返回 `ErrNativeNotSupported` 后，一律落到**该入站自己的转换管线**——
降到「某一条统一的路径」是做不到的，因为各协议的转换本来就不同（chat 无需转换）：

| 入站 | 降级到 | failover |
|---|---|---|
| `/v1/chat/completions` | chat 标准管线（无协议转换） | 有 |
| `/v1/messages` | relaykit Anthropic→Chat | 有 |
| `/v1/responses` | relaykit Responses→Chat | 有 |
| `/v1beta/models/*` | relaykit Gemini→Chat | 有 |

**四条都必须带模型组内 failover**：首次建立上游请求失败时，经
`groupDispatch.HandleFailover` 换渠道重试；流式只在「流尚未建立」时重试，
一旦首帧发出就不再切换（否则等于把两个响应拼在一起）。

这条不是可有可无的：`/v1/responses` 和 Gemini 的 relaykit 路径早期都是单次尝试，
结果就是**配了原生旁路反而比转换路径更脆弱** —— 一次上游抖动就直接失败，
而组里的备份渠道从未被尝试。

## 已知取舍

- **原生调用本身单次尝试，不做候选轮转**——这是有意的：请求已经打到上游，
  重试会产生第二次调用与重复计费。真正需要换渠道时，兜底发生在降级之后的转换路径上，
  而那条路径四条入站都带完整 failover（见上一节）。
- embeddings / rerank 没有原生旁路（`NativeRelayProvider` 契约只覆盖 chat 形状）。
- cohere 没有原生旁路：它走 `/v1/rerank` 与 `/v1/embed`，不在本契约范围内。
- cohere 未接入：默认 base URL 无 `/v1` 前缀，硬拼端点会打错地址。
- **shadow 模式语义在四个入口不一致**（未做统一）：
  chat 无此概念（等同 legacy）、messages/responses 走影子对比后仍返回 legacy 结果、
  gemini 等同 active。配置 `shadow` 前先确认目标入口的实际行为。

## 排错

| 现象 | 先查 |
|---|---|
| `/v1beta` 返回 HTML | nginx 没放行 `location /v1beta/` |
| 原生路径 400 但转换路径正常 | 客户端带了该上游不认的字段，原生路径把它们透传了 |
| 该请求计费为 0 | `upstream_protocol` 有值但 usage 没解析出来，查 `usageFromMap` |
| 降级路径没生效 | 检查错误是否真的包装了 `core.ErrNativeNotSupported` |
