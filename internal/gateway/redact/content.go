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
