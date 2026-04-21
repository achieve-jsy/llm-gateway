# Tool Message Sanitization Implementation Plan

> **For agentic workers:** Implement task-by-task with TDD.

**Goal:** Always sanitize tool message history before Anthropic/Bedrock upstream calls.

**Architecture:** Shared `core.SanitizeToolMessages` on OpenAI-shaped `[]Message`; invoke from Anthropic and Bedrock translators.

**Tech Stack:** Go, existing `internal/core` types

---

### Task 1: Core sanitizer + unit tests

- [ ] Write failing tests in `internal/core/tool_message_sanitize_test.go`
- [ ] Implement `internal/core/tool_message_sanitize.go`
- [ ] `go test ./internal/core/ -run SanitizeToolMessages`

### Task 2: Wire Anthropic + Bedrock

- [ ] Call sanitizer at start of message conversion in `request_translation.go` and `bedrock/chat.go`
- [ ] `go test ./internal/providers/anthropic/ ./internal/providers/bedrock/ ./internal/core/`
