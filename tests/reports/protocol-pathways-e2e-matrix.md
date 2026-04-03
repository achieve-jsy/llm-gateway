# 协议通路端到端测试报告（全矩阵）

- **范围**：OpenAI Chat 协议 → Azure/OpenAI GPT-5.x（含 Chat→Responses 桥接）
- **分支/版本**：基于 `v0.105.0`（`feat/chat-via-responses-bridge`）
- **执行日期**：2026-07-15
- **结论**：**离线矩阵全绿 + Azure Live 7/7 全绿**

---

## 1. 执行命令与结果

| 层 | 命令 | 结果 |
|---|---|---|
| 离线矩阵 | `go test ./tests/providers/openai/ ./tests/providers/azure/ ./tests/providers/ ./internal/providers/openai/ -count=1 -v` | **PASS** |
| Azure Live | `go test ./tests/providers/azure/ -tags=live -count=1 -v -timeout 240s -run Live` | **PASS（7/7）** ~20s |

---

## 2. 通路矩阵（离线假上游）

### OpenAI CompatibleProvider（`TestE2E_ProtocolMatrix_*`）

| 通路 | 期望上游 | 结果 |
|---|---|---|
| 普通 Chat（`max_tokens`→`max_completion_tokens`） | `/chat/completions` | PASS |
| OpenCode 别名无 tools（清洗 summary/effort/temperature） | `/chat/completions` | PASS |
| 仅 tools | `/chat/completions` | PASS |
| 仅 `reasoningSummary` + tools（不桥） | `/chat/completions` | PASS |
| tools + `reasoning_effort` + summary | `/responses` + `reasoning_content` 回灌 | PASS |
| tools + `reasoningEffort`（camel） | `/responses` | PASS |
| tools + nested `reasoning:{effort,summary}` | `/responses` | PASS |
| gpt-5.1 + tools + reasoning（非 5.4+） | `/chat/completions`（不桥） | PASS |
| gpt-5-chat* 保留 temperature | `/chat/completions` | PASS |
| 流式：仅 tools | Chat SSE | PASS |
| 流式：tools+reasoning | Responses SSE→Chat chunk；无 `stream_options` | PASS |
| Anthropic `thinking`→ChatRequest 不误桥 | 门控 false | PASS |

### Azure Provider（`TestE2E_AzureProtocolMatrix_*`）

| 通路 | 期望上游 | 结果 |
|---|---|---|
| 普通 Chat | `/openai/v1/chat/completions` + `api-key` | PASS |
| 别名防火墙 | 同 Chat；剥离 `reasoningSummary`/`reasoningEffort` | PASS |
| tools+reasoning 桥接 | `/openai/v1/responses` + `reasoning_content` | PASS |
| 流式桥接 | `/openai/v1/responses`；禁止 `stream_options` | PASS |

---

## 3. Azure Live（真实 West US / gpt-5.5）

| 用例 | 结果 | 观测 |
|---|---|---|
| `ChatParamNormalize_NoTools` | PASS | 清洗后 Chat 成功 |
| `ChatViaResponses_ToolsAndReasoning` | PASS | `finish=tool_calls`，usage 67/19/86 |
| `StreamChatViaResponses_ToolsAndReasoning` | PASS | 2225 bytes SSE |
| `ToolsWithoutReasoning_StaysChat` | PASS | 不误桥 |
| `PlainChat` | PASS | 普通对话 |
| `SummaryOnlyWithTools_NoBridge` | PASS | summary-only 留在 Chat |
| `ChatAndResponses`（直连+probe） | PASS | L1/L2 ok |

---

## 4. 覆盖边界说明

**已覆盖（协议层 E2E）**

- OpenAI / Azure 两条 provider 路径
- 同步 + 流式
- 桥接 / 不桥接门控全组合
- 参数清洗、reasoning_content 回灌、Azure `stream_options` 兼容

**未覆盖（非本矩阵目标）**

- 完整 gateway 进程（鉴权→模型组→nginx）HTTP 黑盒
- Claude Code 真客户端 UI 自动化
- OpenAI 官方（非 Azure）Live 账号

---

## 5. 复现

```bash
# 离线全矩阵
go test ./tests/providers/openai/ ./tests/providers/azure/ ./tests/providers/ \
  ./internal/providers/openai/ -count=1 -v

# Azure Live
export AZURE_API_KEY=...
export AZURE_ENDPOINT=https://examplewestus.openai.azure.com
export AZURE_MODEL=gpt-5.5
go test ./tests/providers/azure/ -tags=live -count=1 -v -timeout 240s -run Live
```

用例文件：

- `tests/providers/openai/e2e_protocol_matrix_test.go`
- `tests/providers/azure/e2e_protocol_matrix_test.go`
- `tests/providers/azure/live_bridge_test.go`
- `tests/providers/openai/e2e_bridge_test.go`
