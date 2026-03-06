package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestApplyRuntimeLogConfig_LevelChange(t *testing.T) {
	// 保存原始 logger，测试完恢复
	orig := slog.Default()
	defer slog.SetDefault(orig)

	// 初始设为 info 级别
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "info", EnableCaller: false})

	// 验证级别已更新
	if got := GetCurrentLogLevel(); got != slog.LevelInfo {
		t.Errorf("expected level info, got %v", got)
	}

	// 切换到 debug 级别
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "debug", EnableCaller: false})

	if got := GetCurrentLogLevel(); got != slog.LevelDebug {
		t.Errorf("expected level debug, got %v", got)
	}

	// 切换到 warn 级别
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "warn", EnableCaller: false})

	if got := GetCurrentLogLevel(); got != slog.LevelWarn {
		t.Errorf("expected level warn, got %v", got)
	}

	// 切换到 error 级别
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "error", EnableCaller: false})

	if got := GetCurrentLogLevel(); got != slog.LevelError {
		t.Errorf("expected level error, got %v", got)
	}
}

func TestApplyRuntimeLogConfig_LogOutputFiltered(t *testing.T) {
	orig := slog.Default()
	defer slog.SetDefault(orig)

	// 先应用配置，设置级别为 warn
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "warn", EnableCaller: false})

	// 再替换 logger 为测试用的 buffer logger（但保留 level 过滤）
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	lh := &levelHandler{level: &currentLevel, handler: handler}
	slog.SetDefault(slog.New(lh))

	ctx := context.Background()

	// 写一条 debug 日志 —— 不应出现在输出中
	slog.DebugContext(ctx, "this should be filtered out")

	// 写一条 info 日志 —— 也不应出现在输出中
	slog.InfoContext(ctx, "this should also be filtered out")

	// 写一条 warn 日志 —— 应该出现在输出中
	slog.WarnContext(ctx, "this should appear")

	output := buf.String()

	if strings.Contains(output, "this should be filtered out") {
		t.Error("debug message should have been filtered at warn level")
	}
	if strings.Contains(output, "this should also be filtered out") {
		t.Error("info message should have been filtered at warn level")
	}
	if !strings.Contains(output, "this should appear") {
		t.Error("warn message should appear at warn level")
	}
}

func TestApplyRuntimeLogConfig_EnableCaller(t *testing.T) {
	orig := slog.Default()
	defer slog.SetDefault(orig)

	// 启用 caller
	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "info", EnableCaller: true})

	// 验证默认 logger 已更新（不会 panic）
	slog.Info("caller test")
}

func TestApplyRuntimeLogConfig_UnknownLevelDefaultsToInfo(t *testing.T) {
	orig := slog.Default()
	defer slog.SetDefault(orig)

	ApplyRuntimeLogConfig(RuntimeLogConfig{Level: "trace", EnableCaller: false})

	if got := GetCurrentLogLevel(); got != slog.LevelInfo {
		t.Errorf("expected unknown level to default to info, got %v", got)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"invalid", slog.LevelInfo},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}