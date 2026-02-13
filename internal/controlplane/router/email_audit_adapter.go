package router

import (
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/audit"
)

// emailAuditAdapter bridges notification.AuditRecorder to audit.Recorder.
type emailAuditAdapter struct {
	recorder audit.Recorder
}

func newEmailAuditAdapter(r audit.Recorder) *emailAuditAdapter {
	return &emailAuditAdapter{recorder: audit.Wrap(r)}
}

func (a *emailAuditAdapter) RecordEmailSend(recipients []string, success bool, errorMsg string) {
	status := 200
	if !success {
		status = 500
	}
	a.recorder.Record(audit.Input{
		Action:       audit.ActionEmailAlert,
		ResourceType: audit.ResourceEmail,
		ResourceName: "告警邮件",
		StatusCode:   status,
		Details: map[string]any{
			"recipients": strings.Join(recipients, ", "),
			"success":    success,
			"error":      errorMsg,
		},
	})
}
