package tracesealer_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestSummarizeCleanRoundIsSilent 全绿轮次**不得**产生任何告警
// （否则每天 288 轮的 INFO 会把真正的故障埋掉）。
func TestSummarizeCleanRoundIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 3, Sealed: 3})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizePartExistsAloneIsSilent ErrPartExists 是预期状态 ⇒ 不得告警。
func TestSummarizePartExistsAloneIsSilent(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, PartExists: 1})
	require.Equal(t, tracesealer.SeverityNone, a.Severity)
}

// TestSummarizeRetryableFailureIsWarn 可重试失败 = WARN（等下一轮），不是 ERROR。
func TestSummarizeRetryableFailureIsWarn(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, SealFailed: 1})
	require.Equal(t, tracesealer.SeverityWarn, a.Severity)
}

// TestSummarizeTooManyRowsIsErrorAndNamed 永久失败必须是 ERROR，且 reason 是稳定串
// （alert 规则按它匹配，不能靠错误文本）。
func TestSummarizeTooManyRowsIsErrorAndNamed(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, TooManyRows: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "too_many_rows", a.Reason)
}

// TestSummarizeUnknownFailureIsError 未分类失败按 ERROR 处理（宁可吵，不可静默）。
func TestSummarizeUnknownFailureIsError(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{Eligible: 1, UnknownFailed: 1})
	require.Equal(t, tracesealer.SeverityError, a.Severity)
	require.Equal(t, "unknown_failure", a.Reason)
}

// TestSummarizeFieldsAreStable 字段集必须固定（看板/告警规则依赖它），
// 且零值也要出现（缺失字段会被误读成「没问题」）。
func TestSummarizeFieldsAreStable(t *testing.T) {
	a := tracesealer.Summarize(tracesealer.RoundResult{})
	for _, k := range []string{"eligible", "sealed", "part_exists", "row_mismatch",
		"seal_failed", "too_many_rows", "unknown_failed", "max_rows", "min_bytes_per_row", "min_ratio"} {
		require.Contains(t, a.Fields, k, "字段 %s 必须恒在", k)
	}
}

// ---------------------------------------------------------------------------
// 告警线的**运行时**行为（runner.go 的 Round）
//
// 【为什么必须有这三条，而不是只有上面的 Summarize 用例】`Summarize` 只回答「该不该
// 告警」；真正落到运维采集面上的还有一半职责在 `Round` 里：把 `Alert.Severity` 映射
// 成**日志级别**（ERROR / WARN / 全绿不打线）、把 `Alert.Reason` 作为 `reason` 字段
// 打进日志。告警规则与看板匹配的是**这条线**（级别 + reason 串），不是 `Summarize`
// 的返回值 —— 只钉住 `Summarize` 无法证明这条线存在、更无法证明它的级别正确。
// 因此这三条用例注入 `Options.Slog` 捕获 sink，对**具体级别与具体 reason 串**下断言。
//
// 【为什么断言的是级别与串，不是「有输出」】「有输出」在 SeverityNone 也成立（心跳
// 总会打）；只有钉住 `slog.LevelError` / `slog.LevelWarn` / 具体 reason 串，才能区分
// 「永久失败报成 ERROR」「可重试报成 WARN」「全绿一条告警线都不许有」这三件事。
// ---------------------------------------------------------------------------

// 告警线/心跳的消息串是 runner.go 与告警规则之间的匹配面：这里逐字钉住它们。
const (
	alertLineMsg = "trace-sealer 轮次告警"
	heartbeatMsg = "trace-sealer round"
)

// capturedRecord 是一条 slog 记录的最小投影（级别 + 消息 + 结构化字段）。
type capturedRecord struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// captureSink 是 `Options.Slog` 的落地面：把每条记录留成可断言的**数据**。
// 它只存在于测试侧 —— 实现不感知它，因此断言的是「runner 真的经由注入的 Slog 落盘」
// 这一接线事实，而不是某个测试专用的出口。
type captureSink struct {
	mu   sync.Mutex
	recs []capturedRecord
}

func (s *captureSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *captureSink) Handle(_ context.Context, r slog.Record) error {
	rec := capturedRecord{level: r.Level, msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	s.mu.Lock()
	s.recs = append(s.recs, rec)
	s.mu.Unlock()
	return nil
}

func (s *captureSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *captureSink) WithGroup(string) slog.Handler      { return s }

// byMsg 取某个消息串的全部记录（告警线与心跳共用一个 sink，必须按消息区分）。
func (s *captureSink) byMsg(msg string) []capturedRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []capturedRecord
	for _, r := range s.recs {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

// newCapturingRunner 用替身封块器 + 捕获 sink 造一个 Runner。
// 【为什么不用 round_test.go 的 newTestRunner】它不注入 Slog，走到 logfWriter ⇒
// 级别与结构化字段会被降维成一行文本，无法断言「具体级别」。
func newCapturingRunner(t *testing.T, f *fakeSealer) (*tracesealer.Runner, *captureSink) {
	t.Helper()
	sink := &captureSink{}
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root: t.TempDir(),
		Slog: slog.New(sink),
		NewSealer: func(ta.SealerOptions) (tracesealer.Sealer, error) {
			return f, nil
		},
	})
	require.NoError(t, err)
	return r, sink
}

// TestRoundTooManyRowsEmitsErrorAlertLine 永久失败轮必须打出**恰好一条 ERROR 告警线**，
// 且 reason 是稳定串 `too_many_rows`（不是错误文本）。
// 杀手（集合 m04 打级别、m05 打 reason）：把 `Summarize` 的该分级降成 WARN，或把
// reason 拼上细节 ⇒ 本条转红。
func TestRoundTooManyRowsEmitsErrorAlertLine(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0)},
		byPart: map[int]error{0: fmt.Errorf("%w: 行数=8388609", ta.ErrTooManyRows)},
	}
	r, sink := newCapturingRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.TooManyRows)
	lines := sink.byMsg(alertLineMsg)
	require.Len(t, lines, 1, "永久失败轮必须恰好一条轮次告警线")
	require.Equal(t, slog.LevelError, lines[0].level, "永久失败（重试永远不会成功）必须是 ERROR")
	require.Equal(t, "too_many_rows", lines[0].attrs["reason"])
}

// TestRoundRetryableFailureEmitsWarnAlertLine 纯可重试轮必须打出**恰好一条 WARN 告警线**，
// reason = `round_failed`（语义是「等下一轮」，不是「出事了」）。
// 杀手（集合 m03）：把该分级升级成 ERROR ⇒ 本条转红。
func TestRoundRetryableFailureEmitsWarnAlertLine(t *testing.T) {
	f := &fakeSealer{
		tasks:  []ta.SealTask{mkTask(0)},
		byPart: map[int]error{0: fmt.Errorf("%w: 子进程退出 37", ta.ErrSealFailed)},
	}
	r, sink := newCapturingRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.SealFailed)
	require.Equal(t, 0, res.TooManyRows)
	lines := sink.byMsg(alertLineMsg)
	require.Len(t, lines, 1, "纯可重试轮必须恰好一条轮次告警线")
	require.Equal(t, slog.LevelWarn, lines[0].level, "可重试失败是 WARN（等下一轮），不是 ERROR")
	require.Equal(t, "round_failed", lines[0].attrs["reason"])
}

// TestRoundCleanRoundEmitsOnlyHeartbeat 全绿轮**零告警线**，且心跳**恰好一次**。
// 【零告警线】每天 288 轮恒定告警会把真故障埋掉 ⇒ 全绿轮的告警线必须为空。
// 杀手（集合 m01）：把全绿轮的分级改成 WARN ⇒ 告警线出现，本条转红。
// 【心跳恰好一次】运维规则是「>3 × poll-interval 没看到这条 ⇒ 进程挂了/被 mask 了」，
// 多打一次或一次不打都会让计数规则失真。这一条断言的**可失败性**由静态检查
// （缩进/重命名/新增第二次心跳即红）与一次人工实测共同承载，本集合的变异面在
// `alert.go`，无法覆盖 `runner.go` 的心跳行（见集合 docstring 与计划 E62）。
func TestRoundCleanRoundEmitsOnlyHeartbeat(t *testing.T) {
	f := &fakeSealer{tasks: []ta.SealTask{mkTask(0), mkTask(1)}, byPart: map[int]error{}}
	r, sink := newCapturingRunner(t, f)
	res, err := r.Round(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, res.Sealed)
	require.Empty(t, sink.byMsg(alertLineMsg), "全绿轮不得有任何轮次告警线")
	beats := sink.byMsg(heartbeatMsg)
	require.Len(t, beats, 1, "每轮必须恰好一条心跳")
	require.Equal(t, slog.LevelInfo, beats[0].level)
}
