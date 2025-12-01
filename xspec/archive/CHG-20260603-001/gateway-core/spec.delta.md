# gateway-core 需求变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0)

## 变更摘要

新增调用埋点采集能力：在请求完成后异步构造 UsageEvent（含 Token reasoning/cached 分桶、TTFT/延迟细分、provider 路由信息、stream 标记、session_id），通过 collector 投递 batch writer 批量落库；补全流式 usage 回填和完整身份解析链路。

## 新增需求

### US-NEW-01：调用埋点采集（P0）

作为系统，每次 LLM 请求完成后，异步记录 UsageEvent（模型/Token 数含 reasoning+cached 分桶/TTFT+延迟细分/provider 路由信息/is_stream/session_id/状态/Key/dept_id/成本），投递 collector → 批量写入 call_logs 明细表。

- 采集点位在请求收尾处（Logger 中间件 c.Next() 之后），此时各字段已由 orchestrator 写回上下文
- **Token 分桶**：prompt_tokens（含 text/audio/image 细分）、completion_tokens、reasoning_tokens、cached_tokens
- **延迟细分**：ttft_ms（startTime → completionStartTime）、duration_ms（总耗时）、provider_duration_ms（上游实际耗时，从响应头提取）
- **Provider 路由**：provider_type（如 openai/anthropic）、provider_model_id（命中的实际模型 DB ID）
- **请求元数据**：is_stream（流式标记）、session_id（会话 ID，多轮对话关联）
- 调用 EstimateCost 按多桶单价算成本并锁价，与明细一起写入
- 投递为非阻塞 send（channel），队列满时丢弃并计数，绝不阻塞热路径

### US-NEW-02：流式 usage 回填（P0）

作为系统，对流式请求注入 `stream_options.include_usage=true`，在流式转发收尾时解析末尾含 usage 的 SSE chunk，回填 Token 字段（与非流式同口径、同缓存分桶字段）。

- OpenAI 兼容上游：解析最后 chunk 中 `usage.prompt_tokens` / `completion_tokens` / `prompt_tokens_details.cached_tokens` / `completion_tokens_details.reasoning_tokens`
- Anthropic：累计 `message_start` / `message_delta` 中的 `input_tokens` / `output_tokens` / `cache_read_input_tokens` / `cache_creation_input_tokens` / `thinking_tokens`
- 上游不支持 include_usage 时：token 记 0 且 UsageMissing=true

### US-NEW-03：身份解析补全（P0）

作为系统，Key 验证通过后从缓存/回源结果中提取完整身份链（key_id → user_id → dept_id），写入请求上下文供 collector 读取。

- dept_id 直接从 SSO 已有的身份信息获取（非预留）
- studio_id 当前置空，schema 已预留
- Key 有效但身份缺失时：放行请求，dept_id 记空，计入 identity_missing 告警

### US-NEW-04：调用内容采集（P1）

作为系统，将每次请求的完整 messages 和响应正文（流式已重组）异步写入 call_payloads 表。

- 请求侧：从已解析的请求体中取 messages / params（不含鉴权头）
- 响应侧（非流式）：取最终响应的 choices[].message
- 响应侧（流式）：在流式收尾点将各 delta.content 重组为完整 response_text，tool_calls 合并
- 走独立队列/writer，与计量管道分离
- 超大 body（> 256KB）截断，置 truncated=true

### US-NEW-05：Batch Writer（P0）

作为系统，从 collector 队列批量消费 UsageEvent，批量写入 call_logs 明细表。

- 按条数（200 条）或时间（1s）触发批量写入
- ON CONFLICT(request_id) DO NOTHING 保证幂等
- 写入失败记录错误指标，不影响热路径

## 修改需求

- 原 spec §3「超出范围」第 3 条「不负责用量统计的持久化存储」→ 修改为：不负责用量统计的聚合与查询（由 observability-hub 负责），但负责将调用明细通过 batch writer 写入 call_logs / call_payloads

## 新增验收标准

| 编号 | Given（前置状态） | When（触发动作） | Then（预期结果） | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| AC-NEW-01 | Given 一次成功的非流式 LLM 调用完成 | When 请求收尾 | Then call_logs 中出现对应 request_id 的行，含正确的 token 分桶和锁价成本 | P0 |
| AC-NEW-02 | Given 一次流式调用且上游支持 include_usage | When 流式转发完成 | Then token 字段非零，与同模型非流式调用口径一致 | P0 |
| AC-NEW-03 | Given 上游不支持 include_usage | When 流式调用完成 | Then token 记 0，usage_missing=true | P0 |
| AC-NEW-04 | Given collector channel 已满（10000 条） | When 新事件到达 | Then 事件被丢弃，usage_dropped 指标 +1，请求延迟不增加 | P0 |
| AC-NEW-05 | Given 同一 request_id 的事件被重复投递（failover 场景） | When batch writer 执行 | Then 仅写入一行，不重复计数 | P0 |
| AC-NEW-06 | Given Key 验证通过 | When SSO 身份含 dept_id | Then call_logs 中 dept_id 字段正确记录 | P0 |
| AC-NEW-07 | Given 一次 LLM 调用完成 | When 内容采集执行 | Then call_payloads 中出现完整的请求 messages 和响应正文 | P1 |
| AC-NEW-08 | Given 响应 body > 256KB | When 内容采集执行 | Then call_payloads.truncated=true，正文被截断存储 | P1 |
| AC-NEW-09 | Given 一次 Qwen 调用返回 reasoning_tokens=3412 | When 请求收尾 | Then call_logs 中 reasoning_tokens=3412，不与 completion_tokens 混淆 | P0 |
| AC-NEW-10 | Given 一次流式调用，上游返回 completion_tokens_details.reasoning_tokens | When 流式收尾 | Then reasoning_tokens 正确回填，与非流式同口径 | P0 |
| AC-NEW-11 | Given 一次调用 TTFT=2000ms，上游耗时=1950ms | When 请求收尾 | Then call_logs.ttft_ms=2000, provider_duration_ms=1950, duration_ms 为总耗时 | P0 |
| AC-NEW-12 | Given 请求通过 bedrock provider 转发 | When 请求收尾 | Then call_logs.provider_type="bedrock"，provider_model_id 为实际命中模型 ID | P0 |
| AC-NEW-13 | Given 一次流式请求 | When 请求收尾 | Then call_logs.is_stream=true | P0 |
| AC-NEW-14 | Given 一次调用含 session_id | When 请求收尾 | Then call_logs.session_id 正确记录 | P1 |

## 修改验收标准

- 无（原有验收标准不受影响）
