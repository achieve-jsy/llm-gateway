# Tool Message Sanitization Design

**Date:** 2026-07-14  
**Status:** Approved (always-on)

## Goal

Align with LiteLLM `modify_params` message sanitization so Anthropic/Bedrock no longer 400 on orphaned `tool_use` / `tool_result` or empty text content.

## Behavior (always on)

Operate on canonical `[]core.Message` (OpenAI shape) before provider translation:

1. **Orphaned tool calls** — assistant `tool_calls` without matching `role=tool` → inject dummy tool message:  
   `[System: Tool execution skipped/interrupted. No result provided for tool '<name>'.]`
2. **Orphaned tool results** — `tool_call_id` not present in the preceding assistant tool_calls → drop
3. **Empty content** — user/assistant with empty/whitespace-only text and no tool_calls → placeholder:  
   `[System: Empty message content sanitised to satisfy protocol]`

## Scope

- Call from Anthropic `convertToAnthropicRequest` and Bedrock `convertMessages`
- Not applied to raw `/v1/p/*` passthrough
- No feature flag

## Non-goals

- Rewriting client sessions
- Changing OpenAI-compatible providers that already tolerate orphans
