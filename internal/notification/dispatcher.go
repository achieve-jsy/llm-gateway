package notification

import (
	"context"
	"log/slog"
	"strings"
)

type Dispatcher struct {
	email    *EmailSender
	logger   *AlertLogger
	webhook  WebhookClient
	recorder AuditRecorder
}

type WebhookClient interface {
	SendText(ctx context.Context, text string) error
}

// AuditRecorder 邮件发送审计接口，由调用方注入实现（如 audit.Recorder 适配器）
// 避免 notification 包直接依赖 audit 包。
type AuditRecorder interface {
	RecordEmailSend(recipients []string, success bool, errorMsg string)
}

func NewDispatcher(email *EmailSender, logger *AlertLogger, webhook WebhookClient) *Dispatcher {
	return &Dispatcher{email: email, logger: logger, webhook: webhook, recorder: nil}
}

func (d *Dispatcher) SetAuditRecorder(r AuditRecorder) { d.recorder = r }

func (d *Dispatcher) SendText(ctx context.Context, text string) error {
	return d.SendAlert(ctx, "alert", "system", text)
}

func (d *Dispatcher) SendAlert(ctx context.Context, alertType, source, text string) error {
	if d.logger != nil {
		if err := d.logger.Log(ctx, alertType, source, text); err != nil {
			slog.Warn("dispatcher: file log failed", "error", err)
		}
	}
	if d.webhook != nil {
		if err := d.webhook.SendText(ctx, text); err != nil {
			slog.Warn("dispatcher: webhook send failed", "error", err)
		}
	}
	if d.email != nil {
		err := d.email.SendText(ctx, text)
		if err != nil {
			if d.recorder != nil {
				d.recorder.RecordEmailSend(d.email.recipients, false, err.Error())
			}
			slog.Warn("dispatcher: email send failed", "error", err)
			return err
		}
		if d.recorder != nil {
			d.recorder.RecordEmailSend(d.email.recipients, true, "")
		}
	}
	return nil
}

func BuildRecipients(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
