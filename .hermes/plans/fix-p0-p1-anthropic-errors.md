# Task: Fix P0 (empty error_message in 503) & P1 (Anthropic → OpenAI protocol conversion)

## Background

llm_gateway is a Go-based LLM API gateway that supports both OpenAI `/v1/chat/completions` and Anthropic `/v1/messages` formats, converting between them transparently and routing to multiple upstream providers (Anthropic Claude, Bailian qwen3.7-max, DeepSeek, etc.) via a model group system.

**Two production bugs today (2026-07-11) for user cuiguangyu calling `BF_ULTRA` group:**

1. **P0**: ~489 of 503 responses have `error_message` = empty string in `call_traces` table. The HTTP response body IS populated, but the database trace field is missing — breaking ops monitoring and diagnosis.

2. **P1**: When `/v1/messages` requests are routed to `qwen3.7-max` (Bailian provider, OpenAI-compatible), Bailian returns HTTP 400 with `Unexpected item type in content`. Root cause: our Anthropic→OpenAI message conversion is missing several validation/normalization steps that litellm (the reference Python gateway) performs.

## Repository

- **Location**: `/home/hgf/llm_gateway`
- **Branch**: `fix/p0-p1-anthropic-errors` (already created, checked out, tracking origin/main)
- **Language**: Go
- **Main entry**: `cmd/gateway/main.go`
- **Run tests**: `go test ./...` (must pass before commit)
- **No network to proxy.golang.org** — if module download needed, set `GOPROXY=https://goproxy.cn,direct`

## Key File Locations

| File | Role |
|------|------|
| `internal/gateway/handler/messages.go` | `/v1/messages` handler (Anthropic format) |
| `internal/gateway/handler/chat.go` | `/v1/chat/completions` handler (OpenAI format) |
| `internal/core/anthropic.go` | Anthropic ↔ OpenAI message conversion (649 lines) |
| `internal/gateway/orchestrator/chat.go` | Sets `error_message` in Gin context on upstream error |
| `internal/gateway/orchestrator/group_dispatcher.go` | GroupAwareDispatcher that wraps errors |
| `internal/gateway/middleware/usage_collector.go` | Middleware that reads `error_message` from Gin context and writes to `call_traces` |
| `internal/gateway/scheduler/router.go` | `Router.Route()` returns `ErrNoAvailableModel` when group has 0 healthy models |
| `internal/model/errors.go` | Defines `ErrNoAvailableModel` and other sentinel errors |

## P0 Fix — Empty `error_message` in 503 Responses

### Root Cause

When **all** models in a group (e.g., `BF_ULTRA`) are `auto_disabled` by the health checker:

1. Control plane excludes them from snapshot → RouteTable has empty `Models` slice
2. `Router.Route()` returns `ErrNoAvailableModel`
3. `GroupAwareDispatcher.Dispatch` wraps it as `model.ErrNoAvailableModel`
4. `MessagesHandler.HandleMessages` calls `respondAnthropicGroupError(c, err)` and returns **early** (line ~93-98)
5. `respondAnthropicGroupError` calls `respondAnthropicError(c, 503, "overloaded_error", "model_unavailable", "No available model for this group", nil)`
6. **Neither function ever calls `c.Set("error_message", ...)`**
7. `UsageCollector` middleware reads `c.Get("error_message")` — finds nothing → writes empty string to DB

Note: The upstream-call path (chat.go:138-139 / stream.go:84-85) DOES set `error_message` correctly. The bug is specifically the **early-return dispatch failure path**.

### Fix Specification

In `internal/gateway/handler/messages.go`:

**A. `respondAnthropicGroupError` function** — before each `return`, set `error_message`:

```go
func (h *MessagesHandler) respondAnthropicGroupError(c *gin.Context, err error) {
    if err == model.ErrNoAvailableModel {
        c.Set("error_message", "No available model for this group")  // ADD
        c.Set("error_code", "model_unavailable")                      // ADD
        h.respondAnthropicError(c, http.StatusServiceUnavailable, "overloaded_error", "model_unavailable", "No available model for this group", nil)
        return
    }
    // ... handle other dispatch errors similarly
}
```

**B. `respondAnthropicError` function** — as a safety net, if `error_message` not yet set in Gin context, set it from the `message` parameter:

```go
func (h *MessagesHandler) respondAnthropicError(c *gin.Context, httpStatus int, errType string, code string, message string, upstream *gatewayerrors.UpstreamInfo) {
    // Safety net: ensure error_message is set for trace recording
    if _, exists := c.Get("error_message"); !exists && message != "" {
        c.Set("error_message", message)
    }
    if _, exists := c.Get("error_code"); !exists && code != "" {
        c.Set("error_code", code)
    }
    // ... rest of function unchanged
}
```

**C. Mirror fix in `internal/gateway/handler/chat.go`** — find the equivalent `respondError` function and apply the same safety-net pattern so `/v1/chat/completions` 503s also record `error_message`.

### Verification for P0

- Unit test: simulate dispatch failure (mock scheduler returning `ErrNoAvailableModel`) and verify `c.Get("error_message")` returns the expected string after calling the handler.
- Grep for all places that call `respondAnthropicError` / `respondError` and confirm the safety net catches them.

## P1 Fix — Anthropic → OpenAI Protocol Conversion

### Root Cause

`AnthropicToChatRequest` in `internal/core/anthropic.go` does basic conversion but misses several validation/normalization steps that litellm (Python reference implementation at github.com/BerriAI/litellm) performs. Bailian's OpenAI-compatible endpoint is strict and rejects malformed messages with 400.

### Reference: What litellm Does

| Check | litellm location | Our status |
|-------|------------------|------------|
| Sanitize `tool_use_id` to `[a-zA-Z0-9_-]+` | `factory.py:1561 _sanitize_anthropic_tool_use_id` | ❌ Missing |
| Merge consecutive same-role messages | `factory.py:2342 anthropic_messages_pt` | ❌ Missing |
| Add dummy `tool_result` for orphaned `tool_calls` | `factory.py:2073 _add_missing_tool_results` | ❌ Missing |
| Drop orphaned `tool_result` (no matching tool_call) | `factory.py:2175 _is_orphaned_tool_result` | ❌ Missing |
| Drop `thinking`/`redacted_thinking` blocks for non-Anthropic | `adapter/transformation.py:~450` | Partially (not in `decodeContentBlocks`) |
| Force `tool` role message content to string (not array) | implicit in conversion | ❌ Missing |
| First message must be user | `factory.py:1081` | ❌ Missing |
| Placeholder for empty text content | `factory.py:2019 _sanitize_empty_text_content` | ❌ Missing |

### Fix Specification

Add a new function `sanitizeMessagesForProviders(msgs []Message) []Message` in `internal/core/anthropic.go`. **Call it at the end of `AnthropicToChatRequest`** before returning.

```go
// sanitizeMessagesForProviders applies provider-agnostic message normalization
// that upstream OpenAI-compatible endpoints require. Modeled after litellm's
// sanitize_messages_for_tool_calling() + anthropic_messages_pt() validation.
func sanitizeMessagesForProviders(msgs []Message) []Message {
    // Step 1: Sanitize tool_use_id format in tool_calls (assistant messages)
    //         and tool_call_id (tool messages). Regex: [^a-zA-Z0-9_-] → "_"
    msgs = sanitizeToolUseIDs(msgs)

    // Step 2: Drop thinking / redacted_thinking content blocks
    //         (already excluded in decodeContentBlocks but verify in
    //          ContentPart arrays that bypass the block decoder)
    msgs = dropThinkingBlocks(msgs)

    // Step 3: Force tool-role message content to plain string.
    //         If []ContentPart, concatenate text parts.
    msgs = flattenToolMessageContent(msgs)

    // Step 4: Detect orphaned tool_results (tool message without preceding
    //         assistant tool_call with matching id) — drop them.
    msgs = dropOrphanedToolResults(msgs)

    // Step 5: Detect orphaned tool_calls (assistant tool_call without
    //         following tool message with matching id) — add dummy tool result.
    msgs = addDummyToolResults(msgs)

    // Step 6: Merge consecutive same-role messages.
    //         user+user → single user (concatenate content)
    //         assistant+assistant → single assistant (concatenate content + tool_calls)
    //         tool+tool → KEEP SEPARATE (each tool message must match one tool_call_id)
    msgs = mergeConsecutiveSameRole(msgs)

    // Step 7: Ensure first message is user (insert placeholder "." if not).
    msgs = ensureFirstMessageIsUser(msgs)

    // Step 8: Replace empty text content with placeholder "."
    msgs = sanitizeEmptyContent(msgs)

    return msgs
}
```

**Integrate into `AnthropicToChatRequest`:**

```go
func AnthropicToChatRequest(anthReq *AnthropicMessagesRequest) *ChatRequest {
    // ... existing code ...
    req.Messages = prependSystemMessage(anthReq.System, req.Messages)
    req.Messages = sanitizeMessagesForProviders(req.Messages)  // ADD
    return req
}
```

### Implementation Details for Each Helper

#### `sanitizeToolUseIDs`
```go
var validIDChar = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeToolUseIDs(msgs []Message) []Message {
    for i := range msgs {
        // assistant.tool_calls[].ID
        for j := range msgs[i].ToolCalls {
            msgs[i].ToolCalls[j].ID = validIDChar.ReplaceAllString(msgs[i].ToolCalls[j].ID, "_")
        }
        // tool message's ToolCallID
        if msgs[i].Role == "tool" {
            msgs[i].ToolCallID = validIDChar.ReplaceAllString(msgs[i].ToolCallID, "_")
        }
    }
    return msgs
}
```

#### `flattenToolMessageContent`
```go
func flattenToolMessageContent(msgs []Message) []Message {
    for i := range msgs {
        if msgs[i].Role != "tool" {
            continue
        }
        switch c := msgs[i].Content.(type) {
        case string:
            // OK
        case []ContentPart:
            var sb strings.Builder
            for _, part := range c {
                if part.Type == "text" && part.Text != "" {
                    if sb.Len() > 0 { sb.WriteString("\n") }
                    sb.WriteString(part.Text)
                }
            }
            msgs[i].Content = sb.String()
        case []any:
            var sb strings.Builder
            for _, p := range c {
                if m, ok := p.(map[string]any); ok {
                    if t, _ := m["type"].(string); t == "text" {
                        if text, _ := m["text"].(string); text != "" {
                            if sb.Len() > 0 { sb.WriteString("\n") }
                            sb.WriteString(text)
                        }
                    }
                }
            }
            msgs[i].Content = sb.String()
        case nil:
            msgs[i].Content = ""
        }
    }
    return msgs
}
```

#### `addDummyToolResults`
```go
func addDummyToolResults(msgs []Message) []Message {
    // Build set of tool_call_ids that have corresponding tool messages
    satisfied := map[string]bool{}
    for _, m := range msgs {
        if m.Role == "tool" && m.ToolCallID != "" {
            satisfied[m.ToolCallID] = true
        }
    }
    // For each assistant tool_call, if not satisfied, append dummy tool message
    var result []Message
    for i, m := range msgs {
        result = append(result, m)
        if m.Role == "assistant" && len(m.ToolCalls) > 0 {
            for _, tc := range m.ToolCalls {
                if !satisfied[tc.ID] {
                    // Insert dummy right after this assistant message
                    dummy := Message{
                        Role:       "tool",
                        ToolCallID: tc.ID,
                        Content:    fmt.Sprintf("[System: Tool execution skipped. No result provided for tool '%s'.]", tc.Function.Name),
                    }
                    result = append(result, dummy)
                    satisfied[tc.ID] = true
                }
            }
        }
        // Also look ahead to skip past existing tool messages (avoid duplicates)
        _ = i
    }
    return result
}
```

#### `dropOrphanedToolResults`
```go
func dropOrphanedToolResults(msgs []Message) []Message {
    // Build set of tool_call_ids declared by assistant messages
    declared := map[string]bool{}
    for _, m := range msgs {
        if m.Role == "assistant" {
            for _, tc := range m.ToolCalls {
                declared[tc.ID] = true
            }
        }
    }
    var result []Message
    for _, m := range msgs {
        if m.Role == "tool" && !declared[m.ToolCallID] {
            // Orphaned — drop
            continue
        }
        result = append(result, m)
    }
    return result
}
```

#### `mergeConsecutiveSameRole`
```go
func mergeConsecutiveSameRole(msgs []Message) []Message {
    if len(msgs) == 0 {
        return msgs
    }
    var result []Message
    result = append(result, msgs[0])
    for i := 1; i < len(msgs); i++ {
        prev := &result[len(result)-1]
        cur := msgs[i]
        // Only merge user+user and assistant+assistant (NOT tool+tool)
        if cur.Role == prev.Role && (cur.Role == "user" || cur.Role == "assistant") {
            // Merge content
            prevText := messageToString(prev)
            curText := messageToString(&cur)
            if prevText != "" && curText != "" {
                prev.Content = prevText + "\n" + curText
            } else if curText != "" {
                prev.Content = curText
            }
            // Merge tool_calls (assistant only)
            prev.ToolCalls = append(prev.ToolCalls, cur.ToolCalls...)
        } else {
            result = append(result, cur)
        }
    }
    return result
}

func messageToString(m *Message) string {
    switch c := m.Content.(type) {
    case string:
        return c
    case []ContentPart:
        var sb strings.Builder
        for _, p := range c {
            if p.Type == "text" && p.Text != "" {
                if sb.Len() > 0 { sb.WriteString("\n") }
                sb.WriteString(p.Text)
            }
        }
        return sb.String()
    default:
        return ""
    }
}
```

#### `ensureFirstMessageIsUser`
```go
func ensureFirstMessageIsUser(msgs []Message) []Message {
    if len(msgs) == 0 {
        return msgs
    }
    // System messages allowed at index 0
    start := 0
    if msgs[0].Role == "system" {
        if len(msgs) == 1 {
            return append(msgs, Message{Role: "user", Content: "."})
        }
        start = 1
    }
    if msgs[start].Role != "user" {
        // Insert placeholder user message after system
        placeholder := Message{Role: "user", Content: "."}
        result := make([]Message, 0, len(msgs)+1)
        result = append(result, msgs[:start]...)
        result = append(result, placeholder)
        result = append(result, msgs[start:]...)
        return result
    }
    return msgs
}
```

#### `sanitizeEmptyContent`
```go
func sanitizeEmptyContent(msgs []Message) []Message {
    for i := range msgs {
        if msgs[i].Role == "tool" || len(msgs[i].ToolCalls) > 0 {
            continue  // tool messages and assistant with tool_calls can have empty content
        }
        if msgs[i].Content == nil {
            msgs[i].Content = "."
            continue
        }
        if s, ok := msgs[i].Content.(string); ok && strings.TrimSpace(s) == "" {
            msgs[i].Content = "."
        }
    }
    return msgs
}
```

#### `dropThinkingBlocks`
Verify `decodeContentBlocks` already skips `type="thinking"` and `type="redacted_thinking"`. If not (it currently only checks `block.Type != ""` which INCLUDES thinking), add explicit exclusion:

```go
if block.Type == "" || block.Type == "thinking" || block.Type == "redacted_thinking" {
    continue  // skip empty or thinking blocks
}
```

Currently `decodeContentBlocks` line ~214 has `if block.Type != ""` — this **INCLUDES** thinking blocks in the output. Fix: explicitly filter them out.

### Verification for P1

**Unit tests** (add to `internal/core/anthropic_test.go`):

1. `TestSanitizeToolUseIDs` — IDs with dots/spaces are replaced with underscores
2. `TestFlattenToolMessageContent` — `[]ContentPart` → string
3. `TestAddDummyToolResults` — orphaned tool_call gets dummy result
4. `TestDropOrphanedToolResults` — tool message without matching tool_call is dropped
5. `TestMergeConsecutiveSameRole` — two user messages become one
6. `TestEnsureFirstMessageIsUser` — assistant-first gets placeholder prepended
7. `TestDropThinkingBlocks` — thinking blocks removed from content
8. `TestAnthropicToChatRequest_ToolUse` — end-to-end: Anthropic request with tool_use/tool_result → valid OpenAI-format ChatRequest
9. `TestAnthropicToChatRequest_ThinkingBlocks` — Anthropic request with thinking blocks → output has no thinking blocks

Run: `cd /home/hgf/llm_gateway && go test ./internal/core/... ./internal/gateway/handler/... -v`

### Build Verification

```bash
cd /home/hgf/llm_gateway
go build ./cmd/gateway/...
go test ./... 2>&1 | tail -50
```

Must pass with zero failures. If any test fails, fix before committing.

## Commit Convention

```
fix(gateway): populate error_message for dispatch failures & harden Anthropic→OpenAI conversion

P0: respondAnthropicGroupError / respondAnthropicError now set error_message
in Gin context so UsageCollector middleware records it to call_traces DB.
Previously, 503s from "all models auto_disabled" path had empty error_message.

P1: Added sanitizeMessagesForProviders() in core/anthropic.go with 8
normalization steps modeled after litellm's message validation:
- sanitize tool_use_id format ([a-zA-Z0-9_-]+)
- drop thinking/redacted_thinking blocks
- flatten tool message content to string
- add dummy tool_results for orphaned tool_calls
- drop orphaned tool_results
- merge consecutive same-role messages
- ensure first message is user
- replace empty content with placeholder "."

Fixes: bailian/qwen3.7-max returning 400 "Unexpected item type in content"
when /v1/messages requests route to non-Anthropic providers.
```

## What NOT to Do

- Do NOT modify the control plane, scheduler, circuit breaker, or health checker
- Do NOT change the DB schema or call_traces table
- Do NOT touch streaming paths (stream.go) beyond mirroring the `error_message` safety net
- Do NOT modify provider-level code (anthropic/, openai/, bailian/)
- Do NOT run `git commit` — I (the lead) will commit after review
- Do NOT push to origin — I will handle the push after merge review

## Acceptance Criteria

- [ ] `go build ./cmd/gateway/...` succeeds
- [ ] `go test ./...` passes (all existing + new tests)
- [ ] `git diff --stat` shows only the intended files changed
- [ ] P0: dispatch-failure path sets `error_message` in Gin context (covered by unit test)
- [ ] P1: Anthropic requests with tool_use/tool_result/thinking convert cleanly to OpenAI format (covered by unit tests)
