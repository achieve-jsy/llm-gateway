package notification

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
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

// loginAuth 实现 SASL LOGIN 认证机制，用于不支持 PLAIN 的 SMTP 服务器。
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(string(fromServer)) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected server challenge: %q", string(fromServer))
	}
}

func isCertError(err error) bool {
	if err == nil {
		return false
	}
	var hostnameErr x509.HostnameError
	var unknownAuthErr x509.UnknownAuthorityError
	var certInvalidErr x509.CertificateInvalidError
	if errors.As(err, &hostnameErr) || errors.As(err, &unknownAuthErr) || errors.As(err, &certInvalidErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "x509:") || strings.Contains(msg, "tls: failed to verify")
}

func isNetError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused")
}

func (e *EmailSender) dialAndSend(msg []byte, recipients []string) error {
	addr := fmt.Sprintf("%s:%d", e.host, e.port)
	plainAuth := smtp.PlainAuth("", e.from, e.password, e.host)
	loginA := &loginAuth{username: e.from, password: e.password}

	// Port 465 uses implicit TLS (direct TLS), try direct TLS first
	// Port 587/25 use STARTTLS, try STARTTLS first
	var client *smtp.Client
	var err error

	if e.port == 465 {
		// Implicit TLS for port 465
		slog.Info("email: using direct TLS for port 465", "addr", addr)
		client, err = e.dialDirectTLS(addr)
		if err != nil {
			slog.Info("email: direct TLS failed, falling back to STARTTLS", "addr", addr, "error", err)
			client, err = e.dialSTARTTLS(addr)
			if err != nil {
				return e.formatDialError(addr, err)
			}
		}
	} else {
		// STARTTLS for port 587/25
		slog.Info("email: using STARTTLS for port", "addr", addr, "port", e.port)
		client, err = e.dialSTARTTLS(addr)
		if err != nil {
			if isCertError(err) {
				return e.formatCertError(err)
			}
		slog.Info("email: STARTTLS failed, falling back to direct TLS", "addr", addr, "error", err)
		client, err = e.dialDirectTLS(addr)
		if err != nil {
			return e.formatDialError(addr, err)
		}
		}
	}

	return e.sendMailAfterDial(client, msg, recipients, plainAuth, loginA)
}

func (e *EmailSender) formatDialError(addr string, err error) error {
	if isCertError(err) {
		return e.formatCertError(err)
	}
	return fmt.Errorf("failed to connect to %s: %w", addr, err)
}

func (e *EmailSender) formatCertError(err error) error {
	if e.skipVerify {
		return fmt.Errorf("TLS certificate verification failed (skipVerify=true): %w", err)
	}
	return fmt.Errorf("TLS certificate verification failed (server cert does not match hostname). "+
		"Check 'Enable SMTP TLS Skip Verify' in settings. Detail: %w", err)
}

func (e *EmailSender) sendMailAfterDial(client *smtp.Client, msg []byte, recipients []string, plainAuth, loginA smtp.Auth) error {
	slog.Info("email: connection established, authenticating", "host", e.host, "port", e.port)

	// Authenticate
	if err := e.authenticate(client, plainAuth, loginA); err != nil {
		client.Close()
		return fmt.Errorf("smtp auth: %w", err)
	}

	// Send mail
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

func (e *EmailSender) authenticate(client *smtp.Client, plainAuth smtp.Auth, loginAuth smtp.Auth) error {
	slog.Info("email: attempting PLAIN auth", "host", e.host)
	err := client.Auth(plainAuth)
	if err == nil {
		return nil
	}
	errMsg := err.Error()
	slog.Warn("email: PLAIN auth failed", "host", e.host, "error", errMsg)
	if strings.Contains(errMsg, "use of closed network connection") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "broken pipe") {
		return fmt.Errorf("SMTP server closed connection during auth: %w", err)
	}
	if strings.Contains(errMsg, "Unrecognized authentication type") ||
		strings.Contains(errMsg, "504") {
		slog.Info("email: PLAIN auth not supported, trying LOGIN")
		return client.Auth(loginAuth)
	}
	return err
}

func (e *EmailSender) dialDirectTLS(addr string) (*smtp.Client, error) {
	cfg := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: e.skipVerify}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
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
	return client, nil
}

func (e *EmailSender) dialSTARTTLS(addr string) (*smtp.Client, error) {
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
	cfg := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: e.skipVerify}
	if err := client.StartTLS(cfg); err != nil {
		client.Close()
		return nil, fmt.Errorf("STARTTLS: %w", err)
	}
	return client, nil
}

func buildMIME(from string, to []string, subject, body string) []byte {
	header := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n",
		from, strings.Join(to, ","), subject, time.Now().Format(time.RFC1123Z),
	)
	return []byte(header + body)
}
