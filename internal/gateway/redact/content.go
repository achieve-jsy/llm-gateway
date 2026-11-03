package redact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/example/llm-gateway/internal/core"
)

func errInvalidMode(mode string) error {
	return fmt.Errorf("redact: unknown mode %q (want off|detect|mask)", mode)
}

func jsonOrNil(s string) json.RawMessage {
	if !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}

// ProtectContent 脱敏 message content：纯字符串或 text part。其他形态原样返回。
func (s *Session) ProtectContent(ctx context.Context, content core.MessageContent) (core.MessageContent, error) {
	switch v := content.(type) {
	case string:
		out, err := s.ProtectText(ctx, v)
		if err != nil {
			return nil, err
		}
		return out, nil
	case []core.ContentPart:
		for i := range v {
			if v[i].Type != "" && v[i].Type != "text" {
				continue
			}
			out, err := s.ProtectText(ctx, v[i].Text)
			if err != nil {
				return nil, err
			}
			v[i].Text = out
		}
		return v, nil
	case []core.AnthropicContentBlock:
		for i := range v {
			if v[i].Type != "text" || v[i].Text == "" {
				continue
			}
			out, err := s.ProtectText(ctx, v[i].Text)
			if err != nil {
				return nil, err
			}
			v[i].Text = out
		}
		return v, nil
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := m["type"].(string); ok && t != "text" {
				continue
			}
			text, ok := m["text"].(string)
			if !ok {
				continue
			}
			out, err := s.ProtectText(ctx, text)
			if err != nil {
				return nil, err
			}
			m["text"] = out
		}
		return v, nil
	default:
		return content, nil
	}
}

// responsesTextTypes 是 Responses 方言里承载纯文本的 part 类型。
// 图片/音频/视频/文件 part（input_image、input_audio、input_file、input_video…）
// 不带 text 字段，天然不会被改写。
var responsesTextTypes = map[string]bool{
	"text": true, "input_text": true, "output_text": true,
	"summary_text": true, "reasoning_text": true,
}

// ProtectResponsesInput 脱敏 POST /v1/responses 的 input。
//
// 支持的形态（即 core.ResponsesRequest.UnmarshalJSON 解出的两种真实形态）：
//   - string：整段输入文本
//   - []core.ResponsesInputElement，按 Type 分类：
//     message（type="" 或 "message"）→ Content：string 或
//     [{"type":"input_text","text":...}, ...]（[]any + map）
//     function_call_output → Output（工具结果纯文本）
//     reasoning → Summary / ReasoningContent 里的文本 part
//     function_call → 不动（CallID / Name / Arguments 属工具调用协议字段，
//     与 Chat 侧只保护消息正文的约定一致）
//
// 其他形态原样返回（与 ProtectContent 同策略：不猜结构，宁可不动）。
func (s *Session) ProtectResponsesInput(ctx context.Context, input any) (any, error) {
	switch v := input.(type) {
	case string:
		return s.ProtectText(ctx, v)
	case []core.ResponsesInputElement:
		for i := range v {
			if err := s.protectResponsesElement(ctx, &v[i]); err != nil {
				return nil, err
			}
		}
		return v, nil
	default:
		return input, nil
	}
}

func (s *Session) protectResponsesElement(ctx context.Context, e *core.ResponsesInputElement) error {
	switch e.Type {
	case "function_call":
		return nil
	case "function_call_output":
		out, err := s.ProtectText(ctx, e.Output)
		if err != nil {
			return err
		}
		e.Output = out
		return nil
	case "reasoning":
		if err := s.protectResponsesItems(ctx, e.Summary); err != nil {
			return err
		}
		return s.protectResponsesItems(ctx, e.ReasoningContent)
	default: // message（type="" 或 "message"）
		out, err := s.protectResponsesContent(ctx, e.Content)
		if err != nil {
			return err
		}
		e.Content = out
		return nil
	}
}

func (s *Session) protectResponsesItems(ctx context.Context, items []core.ResponsesContentItem) error {
	for i := range items {
		if items[i].Text == "" {
			continue
		}
		out, err := s.ProtectText(ctx, items[i].Text)
		if err != nil {
			return err
		}
		items[i].Text = out
	}
	return nil
}

func (s *Session) protectResponsesContent(ctx context.Context, content any) (any, error) {
	switch v := content.(type) {
	case string:
		return s.ProtectText(ctx, v)
	case []core.ContentPart:
		return s.ProtectContent(ctx, v)
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); !responsesTextTypes[t] {
				continue
			}
			text, ok := m["text"].(string)
			if !ok {
				continue
			}
			out, err := s.ProtectText(ctx, text)
			if err != nil {
				return nil, err
			}
			m["text"] = out
		}
		return v, nil
	default:
		return content, nil
	}
}

// RestoreResponses 就地还原 Responses 响应里的文本输出：
// output[].content[].text（output_text）、output[].summary[].text（summary_text）、
// output[].arguments（工具调用参数，与 Chat 侧一致）。
// 未覆盖：error.message、annotations、usage 等非文本输出字段。
func (s *Session) RestoreResponses(resp *core.ResponsesResponse) {
	if s == nil || s.s == nil || resp == nil {
		return
	}
	for i := range resp.Output {
		item := &resp.Output[i]
		for j := range item.Content {
			if item.Content[j].Text != "" {
				item.Content[j].Text = s.RestoreText(item.Content[j].Text)
			}
		}
		for j := range item.Summary {
			if item.Summary[j].Text != "" {
				item.Summary[j].Text = s.RestoreText(item.Summary[j].Text)
			}
		}
		if item.Arguments != "" {
			item.Arguments = s.RestoreText(item.Arguments)
		}
	}
}

// RestoreContent 还原 message content 里的占位符，形态保持不变。
func (s *Session) RestoreContent(content core.MessageContent) core.MessageContent {
	switch v := content.(type) {
	case string:
		return s.RestoreText(v)
	case []core.ContentPart:
		for i := range v {
			if v[i].Text != "" {
				v[i].Text = s.RestoreText(v[i].Text)
			}
		}
		return v
	case []core.AnthropicContentBlock:
		for i := range v {
			if v[i].Text != "" {
				v[i].Text = s.RestoreText(v[i].Text)
			}
		}
		return v
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok {
				m["text"] = s.RestoreText(text)
			}
		}
		return v
	default:
		return content
	}
}
