# Chat→Responses Bridge（LiteLLM 对齐）端到端测试报告

- **功能**：GPT-5.4+ Chat Completions 在 `tools` + `reasoning_effort` 时自动桥接到 Responses API
- **分支**：`feat/chat-via-responses-bridge`
- **执行日期**：2026-07-15 10:46 CST
- **上游**：Azure OpenAI West US（`examplewestus.openai.azure.com`，`/openai/v1`）
- **模型**：`gpt-5.5`（上游回显 `gpt-5.5-2026-04-24`）
- **结论**：**全部通过**（离线 E2E + Azure Live）

---

## 1. 执行结果摘要

| 层 | 命令 | 结果 | 耗时 |
|---|---|---|---|
| 离线单测 / 假上游 E2E | `go test ./tests/providers/ ./tests/providers/openai/ ./tests/providers/azure/ ./internal/providers/ ./internal/providers/openai/ -count=1` | **PASS** | ~1.5s |
| Azure Live E2E | `go test ./tests/providers/azure/ -tags=live -count=1 -v -timeout 180s -run Live` | **PASS**（5/5） | ~13.4s |

**Live 明细**

| 用例 | 结果 | 耗时 | 关键观测 |
|---|---|---|---|
| `TestLiveAzureV1_ChatParamNormalize_NoTools` | PASS | 2.35s | Phase A：无 tools 时不桥接；`reasoningSummary` 清洗后 Chat 成功；`finish=stop` |
| `TestLiveAzureV1_ChatViaResponses_ToolsAndReasoning` | PASS | 2.25s | Phase B：桥接成功；`finish=tool_calls`；`get_weather`×1；usage `67/19/86` |
| `TestLiveAzureV1_StreamChatViaResponses_ToolsAndReasoning` | PASS | 1.74s | 流式桥接；SSE 回写 `chat.completion.chunk` + `[DONE]`；2225 bytes |
| `TestLiveAzureV1_ToolsWithoutReasoning_StaysChat` | PASS | 1.42s | 仅 tools、无 reasoning → 仍走 Chat；`finish=tool_calls` |
| `TestLiveAzureV1_ChatAndResponses` | PASS | 5.62s | 直连 Chat + Responses + L1/L2 probe |

---

## 2. 测试矩阵

| 场景 | 期望路径 | 离线 E2E | Azure Live |
|---|---|---|---|
| GPT-5.5 + OpenCode 别名（无 tools） | Chat；drop `reasoningSummary`；`reasoningEffort`→`reasoning_effort`；`max_tokens`→`max_completion_tokens` | PASS | PASS |
| GPT-5.5 + tools + `reasoning_effort` | 自动桥 `/responses`；回转 `chat.completion` | PASS | PASS |
| GPT-5.5 + tools + reasoning + stream | 桥 `/responses` stream；转 Chat SSE | PASS | PASS |
| GPT-5.5 + tools（无 reasoning） | 留在 `/chat/completions` | PASS | PASS |
| `gpt-5-chat*` 保留 temperature | 不进 reasoning 适配 | PASS | —（未部署） |
| `ShouldBridgeChatToResponses` 门控 | 仅 gpt-5.4+ ∧ tools ∧ effort | PASS | PASS（断言） |
| Azure `/openai/v1` Chat/Responses/Probe | 直连可用 | PASS（adapter） | PASS |

桥接条件（与 LiteLLM 一致）：

```text
isGPT54Plus(model) && len(tools) > 0 && hasReasoningEffort
```

---

## 3. 协议断言（假上游 E2E）

`TestE2E_ChatViaResponses_PathAndRoundTrip` 验证：

1. 命中桥接时 **0 次** `/chat/completions`、**1 次** `/responses`
2. 请求体：
   - `instructions` ← system 消息
   - `max_output_tokens` ← `max_tokens` / `max_completion_tokens`
   - `reasoning.effort` + `reasoning.summary`（summary 从 `reasoningSummary` 保留）
   - tools 扁平化为 Responses function shape（无嵌套 `function`）
3. 响应回转：
   - `object=chat.completion`
   - `finish_reason=tool_calls`
   - usage：`input_tokens`→`prompt_tokens`，`output_tokens`→`completion_tokens`

流式：`TestE2E_StreamChatViaResponses_FakeUpstream` 验证 text delta、tool_calls delta、`finish_reason=tool_calls`、usage chunk、`[DONE]`。

---

## 4. Live 踩坑与修复

### 问题

流式桥接首次 Live 失败：

```text
[azure] invalid_request_error: Unknown parameter: 'stream_options.include_usage'.
```

原因：Chat 侧 `stream_options.include_usage` 被原样带进 Responses 请求；Azure `/openai/v1/responses` 不接受该字段。

### 修复

`ConvertChatRequestToResponses` **不再转发** `StreamOptions`。  
客户端仍可要求 usage：从 `response.completed` 事件抽取后，由 `ResponsesToChatStreamConverter` 写成 Chat usage chunk。

修复后 Live 流式用例 PASS。

---

## 5. 覆盖范围与边界

**已覆盖**

- Provider 层（OpenAI / Azure `CompatibleProvider`）同步 + 流式桥接
- 参数归一（Phase A）与自动路由（Phase B）
- 真实 Azure West US GPT-5.5

**未覆盖（后续可选）**

- 经完整 gateway 进程（鉴权 → 模型组调度 → provider）的 HTTP E2E
- OpenAI 官方（非 Azure）同路径 Live
- 多轮 tool 回传（assistant `function_call` + tool `function_call_output`）长对话
- `gpt-5.4` / `gpt-5.6` 多版本矩阵 Live

---

## 6. 复现命令

```bash
# 离线
go test ./tests/providers/ ./tests/providers/openai/ ./tests/providers/azure/ \
  ./internal/providers/ ./internal/providers/openai/ -count=1

# Azure Live（需密钥，勿提交）
export AZURE_API_KEY=...
export AZURE_ENDPOINT=https://examplewestus.openai.azure.com
export AZURE_MODEL=gpt-5.5
go test ./tests/providers/azure/ -tags=live -count=1 -v -timeout 180s -run Live
```

相关用例文件：

- `tests/providers/openai/adapter_test.go`
- `tests/providers/openai/e2e_bridge_test.go`
- `tests/providers/chat_via_responses_test.go`
- `tests/providers/azure/live_test.go`
- `tests/providers/azure/live_bridge_test.go`

---

## 7. 总评

| 项 | 状态 |
|---|---|
| Phase A 参数清洗 | ✅ 离线 + Live |
| Phase B Chat→Responses 自动桥 | ✅ 离线 + Live |
| 流式桥接 | ✅ 离线 + Live（含 Azure `stream_options` 兼容修复） |
| 非桥接回归（仅 tools） | ✅ |
| 发布建议 | 可合入；建议部署后用 OpenCode（tools + reasoning）打一条真实请求复核 |
