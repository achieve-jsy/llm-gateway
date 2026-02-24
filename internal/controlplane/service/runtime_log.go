package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync/atomic"
)

// RuntimeLogConfig 运行时日志配置（字段对齐前端 OpsRuntimeLogConfig 契约）
type RuntimeLogConfig struct {
	Level              string  `json:"level"`
	SlowRequestMs      int     `json:"slow_request_ms"`
	SampleRate         float64 `json:"sample_rate"`
	LogRequestBody     bool    `json:"log_request_body"`
	LogResponseBody    bool    `json:"log_response_body"`
	MaxBodyBytes       int     `json:"max_body_bytes"`
	RetentionDays      int     `json:"retention_days"`
	EnableCaller       bool    `json:"enable_caller"`
	EnableStacktrace   bool    `json:"enable_stacktrace"`
	StacktraceLevel    string  `json:"stacktrace_level"`
	SamplingInitial    int     `json:"sampling_initial"`
	SamplingThereafter int     `json:"sampling_thereafter"`
}

// currentLevel 全局原子日志级别，默认 info。
var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(slog.LevelInfo))
}

// GetCurrentLogLevel 返回当前日志级别。
func GetCurrentLogLevel() slog.Level {
	return slog.Level(currentLevel.Load())
}

// parseLevel 将级别字符串转为 slog.Level，未知字符串默认 info。
func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// levelHandler 按原子级别过滤日志的 slog.Handler 包装。
type levelHandler struct {
	level   *atomic.Int32
	handler slog.Handler
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	minLevel := slog.Level(h.level.Load())
	return level >= minLevel
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{level: h.level, handler: h.handler.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{level: h.level, handler: h.handler.WithGroup(name)}
}

// ApplyRuntimeLogConfig 实时应用运行时日志配置到全局 slog logger。
// 至少 level 可实时生效；caller 通过 AddSource 控制。
func ApplyRuntimeLogConfig(cfg RuntimeLogConfig) {
	level := parseLevel(cfg.Level)
	currentLevel.Store(int32(level))

	opts := slog.HandlerOptions{
		Level:     slog.LevelDebug, // 让 levelHandler 做过滤
		AddSource: cfg.EnableCaller,
	}

	handler := slog.NewJSONHandler(os.Stderr, &opts)
	lh := &levelHandler{level: &currentLevel, handler: handler}
	slog.SetDefault(slog.New(lh))

	slog.Info("runtime_log_config_applied", "level", cfg.Level, "caller", cfg.EnableCaller)
}

// RuntimeLogConfigFromMap 从 map[string]interface{} 提取 RuntimeLogConfig。
func RuntimeLogConfigFromMap(m map[string]interface{}) RuntimeLogConfig {
	cfg := RuntimeLogConfig{
		Level:              "info",
		SlowRequestMs:      5000,
		SampleRate:         1.0,
		MaxBodyBytes:       2048,
		RetentionDays:      30,
		EnableCaller:       true,
		StacktraceLevel:    "error",
		SamplingInitial:    5,
		SamplingThereafter: 200,
	}

	if v, ok := m["level"]; ok {
		if s, ok := v.(string); ok {
			cfg.Level = s
		}
	}
	if v, ok := m["slow_request_ms"]; ok {
		cfg.SlowRequestMs = toInt(v)
	}
	if v, ok := m["sample_rate"]; ok {
		if f, ok := v.(float64); ok {
			cfg.SampleRate = f
		}
	}
	if v, ok := m["log_request_body"]; ok {
		cfg.LogRequestBody = toBool(v)
	}
	if v, ok := m["log_response_body"]; ok {
		cfg.LogResponseBody = toBool(v)
	}
	if v, ok := m["max_body_bytes"]; ok {
		cfg.MaxBodyBytes = toInt(v)
	}
	if v, ok := m["retention_days"]; ok {
		cfg.RetentionDays = toInt(v)
	}
	if v, ok := m["enable_caller"]; ok {
		cfg.EnableCaller = toBool(v)
	}
	if v, ok := m["enable_stacktrace"]; ok {
		cfg.EnableStacktrace = toBool(v)
	}
	if v, ok := m["stacktrace_level"]; ok {
		if s, ok := v.(string); ok {
			cfg.StacktraceLevel = s
		}
	}
	if v, ok := m["sampling_initial"]; ok {
		cfg.SamplingInitial = toInt(v)
	}
	if v, ok := m["sampling_thereafter"]; ok {
		cfg.SamplingThereafter = toInt(v)
	}

	return cfg
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func toBool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	}
	return false
}