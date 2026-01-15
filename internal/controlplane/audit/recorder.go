package audit

import "log/slog"

// Recorder 审计记录器接口
// ─────────────────────────────────────────────────────────────
// 所有需要写审计日志的上层（中间件、handler）均依赖此接口，
// 与具体存储实现解耦。可在测试中替换为 mock，
// 也可在生产换为异步/远程实现。
type Recorder interface {
	Record(in Input)
}

// Nop 空实现，nil-safe，便于在测试或审计未启用场景下使用。
type Nop struct{}

func (Nop) Record(Input) {}

// Wrap 将任意 Recorder 包装为 nil-safe 版本：
// 当 recorder 为 nil 时自动降级为 Nop，调用方无需每次都判空。
func Wrap(r Recorder) Recorder {
	if r == nil {
		return Nop{}
	}
	return r
}

// LogRecorder 调试用，将审计事件打到 slog，不落库
type LogRecorder struct{}

func (LogRecorder) Record(in Input) {
	slog.Info("audit[log]",
		"action", in.Action,
		"resource_type", in.ResourceType,
		"resource_id", in.ResourceID,
		"user", in.UserName,
		"ip", in.ClientIP,
		"status", in.StatusCode,
	)
}
