package notification_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMTPServer 起一个最小可用的 SMTP 服务端，把客户端发来的邮件正文（含头部）
// 记录下来，用于断言「主题真的被发出去了」。
//
// 背景：internal/controlplane/service 的 SendAlertEmail 曾经把 subject 丢掉、
// 去调用硬编码主题的 SendTextTo，导致日报/周报/错误摘要/账号健康四类邮件在
// 收件箱里主题完全相同。这个用例把主题透传钉死在传输层。
type fakeSMTPServer struct {
	addr     string
	port     int
	mu       sync.Mutex
	message  string
	receiver chan struct{}
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	server := &fakeSMTPServer{
		addr:     listener.Addr().String(),
		port:     listener.Addr().(*net.TCPAddr).Port,
		receiver: make(chan struct{}, 1),
	}

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		write := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }

		write("220 fake ESMTP ready")
		var body strings.Builder
		inData := false
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")

			if inData {
				if trimmed == "." {
					inData = false
					server.mu.Lock()
					server.message = body.String()
					server.mu.Unlock()
					write("250 OK queued")
					continue
				}
				body.WriteString(trimmed + "\n")
				continue
			}

			switch {
			case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
				// 只声明 AUTH LOGIN：不声明 STARTTLS，客户端就不会尝试 TLS 升级。
				write("250-fake greets you")
				write("250 AUTH LOGIN")
			case strings.HasPrefix(trimmed, "AUTH"):
				write("334 VXNlcm5hbWU6")
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
				write("334 UGFzc3dvcmQ6")
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
				write("235 authenticated")
			case strings.HasPrefix(trimmed, "MAIL FROM"), strings.HasPrefix(trimmed, "RCPT TO"):
				write("250 OK")
			case strings.HasPrefix(trimmed, "DATA"):
				inData = true
				write("354 Start mail input; end with <CRLF>.<CRLF>")
			case strings.HasPrefix(trimmed, "QUIT"):
				write("221 Bye")
				server.receiver <- struct{}{}
				return
			default:
				write("250 OK")
			}
		}
	}()

	return server
}

func (s *fakeSMTPServer) receivedMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

// TestSendTextToWithSubjectKeepsSubject 断言主题会随邮件一起发出，不会被默认主题替换。
func TestSendTextToWithSubjectKeepsSubject(t *testing.T) {
	server := startFakeSMTPServer(t)
	sender := notification.NewEmailSender("127.0.0.1", server.port, "bot@example.com", "secret", nil)

	subject := "Subject-Check-DailyReport-42"
	// 正文用 ASCII，避免 quoted-printable 编码让断言难以阅读。
	require.NoError(t, sender.SendTextToWithSubject(
		context.Background(), subject, "report-body-plain-check", []string{"ops@example.com"}))

	message := server.receivedMessage()
	require.NotEmpty(t, message, "服务端应收到邮件正文")
	assert.Contains(t, message, "Subject: "+subject, "调用方传入的主题必须原样发出")
	assert.Contains(t, message, "report-body-plain-check")
}
