package core

import (
	"fmt"
	"strings"
)

const (
	dummyToolResultFmt = "[System: Tool execution skipped/interrupted. No result provided for tool '%s'.]"
	emptyContentPlaceholder = "[System: Empty message content sanitised to satisfy protocol]"
)

// SanitizeToolMessages 对齐 LiteLLM modify_params 的消息清洗：
// 1) 孤儿 tool_calls → 补假 tool_result
// 2) 孤儿 tool_result → 删除
// 3) user/assistant 空文本 → 占位符
// 始终返回新切片，不修改入参。
func SanitizeToolMessages(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	out := make([]Message, 0, len(msgs)+2)
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]

		if msg.Role == "tool" {
			// 孤儿 tool_result：前面没有带匹配 id 的 assistant tool_calls
			if !toolResultMatchesPriorAssistant(out, msg.ToolCallID) {
				continue
			}
			out = append(out, cloneMessage(msg))
			continue
		}

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			asst := cloneMessage(msg)
			out = append(out, asst)

			// 收集紧随其后的 tool 结果（可能只覆盖部分 call）
			provided := map[string]Message{}
			j := i + 1
			for j < len(msgs) && msgs[j].Role == "tool" {
				provided[msgs[j].ToolCallID] = msgs[j]
				j++
			}

			for _, tc := range msg.ToolCalls {
				if tc.ID == "" {
					continue
				}
				if tr, ok := provided[tc.ID]; ok {
					out = append(out, cloneMessage(tr))
					delete(provided, tc.ID)
				} else {
					name := tc.Function.Name
					if name == "" {
						name = "unknown"
					}
					out = append(out, Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf(dummyToolResultFmt, name),
					})
				}
			}
			// provided 中剩余的是对不上本轮 tool_calls 的孤儿，直接丢弃
			i = j - 1
			continue
		}

		cleaned := cloneMessage(msg)
		if shouldFillEmptyContent(cleaned) {
			cleaned.Content = emptyContentPlaceholder
			cleaned.ContentNull = false
		}
		out = append(out, cleaned)
	}

	return out
}

func toolResultMatchesPriorAssistant(out []Message, toolCallID string) bool {
	if toolCallID == "" {
		return false
	}
	for i := len(out) - 1; i >= 0; i-- {
		m := out[i]
		if m.Role == "tool" {
			continue
		}
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID == toolCallID {
					return true
				}
			}
			return false
		}
		return false
	}
	return false
}

func shouldFillEmptyContent(msg Message) bool {
	if msg.Role != "user" && msg.Role != "assistant" {
		return false
	}
	if len(msg.ToolCalls) > 0 {
		return false
	}
	if HasStructuredContent(msg.Content) {
		// 结构化内容（含图片等）不按空字符串处理
		text := strings.TrimSpace(ExtractTextContent(msg.Content))
		parts, ok := msg.Content.([]ContentPart)
		if ok && len(parts) > 0 {
			return false
		}
		if _, ok := msg.Content.([]any); ok {
			return false
		}
		return text == ""
	}
	return strings.TrimSpace(ExtractTextContent(msg.Content)) == ""
}

func cloneMessage(m Message) Message {
	cp := m
	if len(m.ToolCalls) > 0 {
		cp.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
	}
	return cp
}
