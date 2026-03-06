package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/notification"
)

// EmailService 封装告警/报表邮件发送。优先使用 SMTP 配置（system_configs），
// 若 SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD 均未配置则降级为 slog 打印。
type EmailService struct {
	host       string
	port       int
	user       string
	password   string
	from       string
	skipVerify bool
}

// NewEmailService 从环境变量创建 EmailService。
// 若 SMTP_HOST 为空则进入降级模式（仅打印日志）。
func NewEmailService() *EmailService {
	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	skipVerify := strings.EqualFold(os.Getenv("SMTP_TLS_SKIP_VERIFY"), "true")

	port := 587
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			port = p
		}
	}

	return &EmailService{
		host:       host,
		port:       port,
		user:       user,
		password:   password,
		from:       from,
		skipVerify: skipVerify,
	}
}

// NewEmailServiceFromConfig 从 system_configs 读取 SMTP 配置创建 EmailService。
func NewEmailServiceFromConfig(host, portStr, user, password, from, skipVerifyStr string) *EmailService {
	port := 587
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			port = p
		}
	}
	if from == "" {
		from = user
	}
	return &EmailService{
		host:       host,
		port:       port,
		user:       user,
		password:   password,
		from:       from,
		skipVerify: strings.EqualFold(skipVerifyStr, "true"),
	}
}

// isDegraded 判断是否处于降级模式（SMTP 未配置）。
func (s *EmailService) isDegraded() bool {
	return s == nil || s.host == "" || s.from == ""
}

// SendAlertEmail 发送告警邮件。若 SMTP 未配置则降级为 slog 打印（不报错）。
func (s *EmailService) SendAlertEmail(recipients []string, subject, body string) error {
	if s == nil {
		slog.Warn("email: EmailService is nil, logging instead of sending",
			"subject", subject, "recipients", recipients)
		return nil
	}

	if s.isDegraded() {
		// TODO: 配置 SMTP 后即可真发。当前降级为日志打印。
		slog.Info("email: SMTP not configured, logging instead of sending",
			"subject", subject,
			"to", recipients,
			"body", body,
		)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sender := notification.NewEmailSender(s.host, s.port, s.from, s.password, recipients)
	if s.skipVerify {
		sender.SetSkipTLSVerify(true)
	}

	if err := sender.SendTextTo(ctx, body, recipients); err != nil {
		return fmt.Errorf("email: send failed: %w", err)
	}
	return nil
}