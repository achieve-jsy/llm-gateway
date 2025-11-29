package middleware

import (
	"io"
	"log/slog"
)

// benchSlogInfo / benchSlogDebug：用 JSON handler 写到 io.Discard，
// 真实模拟生产环境的 slog 开销（INFO 级别，JSON 格式）。
var benchLogger = slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

func benchSlogInfo(msg string, args ...any) {
	benchLogger.Info(msg, args...)
}

func benchSlogDebug(msg string, args ...any) {
	benchLogger.Debug(msg, args...)
}
