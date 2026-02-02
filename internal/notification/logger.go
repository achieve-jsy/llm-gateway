package notification

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AlertLogger struct {
	logDir string
	mu     sync.Mutex
}

func NewAlertLogger(logDir string) *AlertLogger {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		slog.Warn("alert_logger: cannot create log dir", "dir", logDir, "error", err)
	}
	return &AlertLogger{logDir: logDir}
}

func (l *AlertLogger) Log(ctx context.Context, alertType, source, text string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	filename := filepath.Join(l.logDir, fmt.Sprintf("alerts_%s.log", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("alert_logger: open %s: %w", filename, err)
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	line := fmt.Sprintf("[%s] [%s] [%s] %s\n", ts, alertType, source, text)
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("alert_logger: write: %w", err)
	}
	return nil
}
