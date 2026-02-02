package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"
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

func (e *EmailSender) SetSkipTLSVerify(v bool) { e.skipVerify = v }

func (e *EmailSender) SendText(ctx context.Context, text string) error {
	if len(e.recipients) == 0 {
		slog.Warn("email: no recipients configured, skipping")
		return nil
	}
	return e.sendMail(ctx, text, e.recipients)
}

func (e *EmailSender) SendTextTo(ctx context.Context, text string, recipients []string) error {
	if len(recipients) == 0 {
		return nil
	}
	return e.sendMail(ctx, text, recipients)
}

func (e *EmailSender) UpdateRecipients(recipients []string) {
	e.recipients = recipients
}

func (e *EmailSender) sendMail(ctx context.Context, text string, recipients []string) error {
	if e.host == "" || e.from == "" {
		return fmt.Errorf("email: SMTP not configured (host=%q from=%q)", e.host, e.from)
	}

	subject := "[LLM Gateway] 告警通知"
	msg := buildMIME(e.from, recipients, subject, text)

	done := make(chan error, 1)
	go func() {
		done <- e.dialAndSend(msg, recipients)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(15 * time.Second):
		return fmt.Errorf("email: send timeout after 15s")
	}
}

func (e *EmailSender) dialAndSend(msg []byte, recipients []string) error {
	addr := fmt.Sprintf("%s:%d", e.host, e.port)
	auth := smtp.PlainAuth("", e.from, e.password, e.host)

	// 优先尝试 STARTTLS（端口 587/25，先明文连接后升级加密）；
	// 失败再降级到直接 TLS（端口 465）。
	client, errTryStartTLS := e.dialSTARTTLS(addr, auth)
	if errTryStartTLS != nil {
		slog.Info("email: STARTTLS failed, fallback to direct TLS", "addr", addr, "error", errTryStartTLS)
		var errDirect error
		client, errDirect = e.dialDirectTLS(addr)
		if errDirect != nil {
			return fmt.Errorf("tls dial %s (starttls=%v, direct=%v)", addr, errTryStartTLS, errDirect)
		}
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(e.from); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			slog.Warn("email: bad recipient skipped", "to", rcpt, "error", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	slog.Info("email: alert sent", "recipients", strings.Join(recipients, ","))
	return nil
}

func (e *EmailSender) dialDirectTLS(addr string) (*smtp.Client, error) {
	cfg := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: e.skipVerify}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	return smtp.NewClient(conn, e.host)
}

func (e *EmailSender) dialSTARTTLS(addr string, auth smtp.Auth) (*smtp.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	client, err := smtp.NewClient(conn, e.host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}
	if err := client.Hello(e.host); err != nil {
		client.Close()
		return nil, fmt.Errorf("smtp HELO: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		client.Close()
		return nil, fmt.Errorf("server does not advertise STARTTLS")
	}
	cfg := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: e.skipVerify}
	if err := client.StartTLS(cfg); err != nil {
		client.Close()
		return nil, fmt.Errorf("STARTTLS: %w", err)
	}
	_ = auth
	return client, nil
}

func buildMIME(from string, to []string, subject, body string) []byte {
	header := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n",
		from, strings.Join(to, ","), subject, time.Now().Format(time.RFC1123Z),
	)
	return []byte(header + body)
}
