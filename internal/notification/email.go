package notification

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gopkg.in/gomail.v2"
)

type EmailSender struct {
	host       string
	port       int
	from       string
	password   string
	recipients []string
	skipVerify bool
}

func NewEmailSender(host string, port int, from, password string, recipients []string) *EmailSender {
	return &EmailSender{
		host:       host,
		port:       port,
		from:       from,
		password:   password,
		recipients: recipients,
	}
}

func (e *EmailSender) SetSkipTLSVerify(v bool)              { e.skipVerify = v }
func (e *EmailSender) UpdateRecipients(recipients []string) { e.recipients = recipients }

func (e *EmailSender) SendText(ctx context.Context, text string) error {
	return e.sendWithSubject(ctx, "[LLM Gateway] 告警通知", text, e.recipients, "")
}

func (e *EmailSender) SendTextWithSubject(ctx context.Context, subject, text string) error {
	return e.sendWithSubject(ctx, subject, text, e.recipients, "")
}

// SendTextWithSubjectAndDedupeKey 使用稳定 Message-ID，便于邮件网关和客户端折叠 outbox 重试。
func (e *EmailSender) SendTextWithSubjectAndDedupeKey(ctx context.Context, subject, text, dedupeKey string) error {
	return e.sendWithSubject(ctx, subject, text, e.recipients, dedupeKey)
}

func (e *EmailSender) SendTextTo(ctx context.Context, text string, recipients []string) error {
	return e.sendWithSubject(ctx, "[LLM Gateway] 告警通知", text, recipients, "")
}

func (e *EmailSender) sendWithSubject(ctx context.Context, subject, text string, recipients []string, dedupeKey string) error {
	if len(recipients) == 0 {
		slog.Warn("email: no recipients configured, skipping")
		return nil
	}
	if e.host == "" || e.from == "" {
		return fmt.Errorf("email: SMTP not configured: host=%q from=%q", e.host, e.from)
	}

	done := make(chan error, 1)
	go func() {
		done <- e.dialAndSend(subject, text, recipients, dedupeKey)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("email: send timeout after 30s")
	}
}

func (e *EmailSender) dialAndSend(subject, text string, recipients []string, dedupeKey string) error {
	d := gomail.NewDialer(e.host, e.port, e.from, e.password)

	// gomail 自动处理 SSL/STARTTLS
	d.SSL = (e.port == 465)
	d.TLSConfig = &tls.Config{
		ServerName:         e.host,
		InsecureSkipVerify: e.skipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	// gomail 会自动协商 AUTH 方法
	// 默认使用 gomail 的自动检测（优先 CRAM-MD5 → LOGIN → PLAIN）
	// 当服务器在 EHLO 中广播 AUTH 方法时，gomail 会自动选择合适的认证方式

	slog.Info("email: dialing SMTP server", "host", e.host, "port", e.port, "ssl", d.SSL, "skipVerify", e.skipVerify)

	slog.Info("email: calling d.Dial()")
	s, err := d.Dial()
	if err != nil {
		slog.Error("email: dial failed", "error", err)
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	defer s.Close()

	slog.Info("email: connection established, building message")

	m := gomail.NewMessage()
	m.SetHeader("From", e.from)
	m.SetHeader("To", recipients...)
	m.SetHeader("Subject", subject)
	if dedupeKey != "" {
		sum := sha256.Sum256([]byte(dedupeKey))
		m.SetHeader("Message-ID", fmt.Sprintf("<%x@llm-gateway.local>", sum[:]))
	}
	m.SetBody("text/plain; charset=\"utf-8\"", text)

	slog.Info("email: sending message", "to", recipients)

	if err := gomail.Send(s, m); err != nil {
		slog.Error("email: send failed", "error", err)
		return fmt.Errorf("smtp send failed: %w", err)
	}

	slog.Info("email: alert sent", "recipients", strings.Join(recipients, ","))
	return nil
}
