// Package redact 把 privacy 引擎接到网关的请求/响应结构上。
//
// 设计约束（见 docs/redaction.md）：
//   - 一个请求一个 Session，绝不跨请求复用；请求处理完必须 Close。
//   - 只处理纯文本字段：message content 的 text part、system prompt。
//     工具定义、工具调用 ID、图片/音频/视频 part、Anthropic tool_use.input 一律不动。
//   - mask 模式下任何错误都不得放行原文（fail-closed），由调用方终止请求。
package redact

import (
	"context"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/privacy"
)

// Mode 是脱敏模式。
const (
	ModeOff    = "off"
	ModeDetect = "detect"
	ModeMask   = "mask"
)

// Config 是网关侧的脱敏配置。零值等价于 off。
type Config struct {
	Mode          string
	RulesOnly     bool
	Actions       map[string]string // 规则类型 → mask|warn|block，默认 mask
	MaxInputBytes int
	CacheDir      string
	Kinds         map[string]bool
	Allow         []string
}

// Engine 持有跨请求共享的 Protector。并发安全，可全局单例。
type Engine struct {
	mode      string
	protector *privacy.Protector
}

// New 按配置构造 Engine。Mode 为 off/detect 时不加载模型。
func New(cfg Config) (*Engine, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeOff
	}
	switch mode {
	case ModeOff, ModeDetect, ModeMask:
	default:
		return nil, errInvalidMode(mode)
	}
	if mode == ModeOff {
		return &Engine{mode: mode}, nil
	}
	pc := privacy.Config{
		RulesOnly:     cfg.RulesOnly || mode == ModeDetect,
		Actions:       cfg.Actions,
		MaxInputBytes: cfg.MaxInputBytes,
		CacheDir:      cfg.CacheDir,
		Kinds:         cfg.Kinds,
		Allow:         cfg.Allow,
	}
	if mode == ModeDetect {
		// detect 只报不遮，永远不因 Block 动作中断请求。
		pc.Actions = map[string]string{}
	}
	p, err := privacy.New(pc)
	if err != nil {
		return nil, err
	}
	return &Engine{mode: mode, protector: p}, nil
}

// Mode 返回当前模式。
func (e *Engine) Mode() string {
	if e == nil {
		return ModeOff
	}
	return e.mode
}

// Enabled 表示是否真的会改写请求内容。
func (e *Engine) Enabled() bool { return e.Mode() == ModeMask }

// DetectOnly 表示只检测不改写。
func (e *Engine) DetectOnly() bool { return e.Mode() == ModeDetect }

// NewSession 为一次请求创建会话。Engine 为 nil 或 off 时返回空会话。
func (e *Engine) NewSession() *Session {
	if e == nil || e.protector == nil {
		return &Session{}
	}
	return &Session{mode: e.mode, s: e.protector.NewSession()}
}

// Session 是一次请求的脱敏上下文，含占位符还原映射。不可跨请求复用。
type Session struct {
	mode string
	s    *privacy.Session
	hits []privacy.Detection
}

// Hits 返回本次请求的命中（不含凭证原文）。
func (s *Session) Hits() []privacy.Detection { return s.hits }

// Close 清空映射。请求结束时必须调用。
func (s *Session) Close() {
	if s != nil && s.s != nil {
		_ = s.s.Close()
	}
}

// ProtectText 处理一段文本：detect 模式只记录命中，mask 模式返回占位符文本。
func (s *Session) ProtectText(ctx context.Context, text string) (string, error) {
	if s == nil || s.s == nil || text == "" {
		return text, nil
	}
	if s.mode == ModeDetect {
		hits, err := s.s.Detect(ctx, text)
		s.hits = append(s.hits, hits...)
		return text, err
	}
	out, hits, err := s.s.ProtectText(ctx, text)
	s.hits = append(s.hits, hits...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// RestoreText 还原一段文本里的占位符。未知占位符原样保留。
func (s *Session) RestoreText(text string) string {
	if s == nil || s.s == nil || text == "" {
		return text
	}
	out, err := s.s.RestoreText(context.Background(), text)
	if err != nil {
		return text
	}
	return out
}

// ProtectChat 就地脱敏 OpenAI Chat 请求。
func (s *Session) ProtectChat(ctx context.Context, req *core.ChatRequest) error {
	if s == nil || s.s == nil || req == nil {
		return nil
	}
	for i := range req.Messages {
		out, err := s.ProtectContent(ctx, req.Messages[i].Content)
		if err != nil {
			return err
		}
		req.Messages[i].Content = out
	}
	return nil
}

// ProtectAnthropic 就地脱敏 Anthropic Messages 请求（system + messages）。
func (s *Session) ProtectAnthropic(ctx context.Context, req *core.AnthropicMessagesRequest) error {
	if s == nil || s.s == nil || req == nil {
		return nil
	}
	if sys, err := s.ProtectContent(ctx, req.System); err != nil {
		return err
	} else if sys != nil {
		req.System = sys
	}
	for i := range req.Messages {
		out, err := s.ProtectContent(ctx, req.Messages[i].Content)
		if err != nil {
			return err
		}
		req.Messages[i].Content = out
	}
	return nil
}

// RestoreChat 就地还原 Chat 响应。
func (s *Session) RestoreChat(resp *core.ChatResponse) {
	if s == nil || s.s == nil || resp == nil {
		return
	}
	for i := range resp.Choices {
		msg := &resp.Choices[i].Message
		msg.Content = s.RestoreContent(msg.Content)
		for j := range msg.ToolCalls {
			if msg.ToolCalls[j].Function.Arguments != "" {
				msg.ToolCalls[j].Function.Arguments = s.RestoreText(msg.ToolCalls[j].Function.Arguments)
			}
		}
	}
}

// RestoreAnthropic 就地还原 Anthropic 响应（text / thinking 块与 tool_use 参数）。
func (s *Session) RestoreAnthropic(resp *core.AnthropicMessagesResponse) {
	if s == nil || s.s == nil || resp == nil {
		return
	}
	for i := range resp.Content {
		b := &resp.Content[i]
		if b.Text != "" {
			b.Text = s.RestoreText(b.Text)
		}
		if len(b.Input) > 0 {
			b.Input = jsonOrNil(s.RestoreText(string(b.Input)))
		}
	}
}

// RestoringWriter 包装 SSE 写出端，跨 chunk 还原占位符。调用方结束时必须 Finish。
func (s *Session) RestoringWriter(w http.ResponseWriter) *privacy.RestoringWriter {
	if s == nil || s.s == nil {
		return nil
	}
	return s.s.NewRestoringWriter(w)
}
