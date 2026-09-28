package tracesealer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// RoundResult 是一轮封块的汇总（对外可观测面：日志、退出码、告警、看板）。
//
// 【MaxRows / MinBytesPerRow / MinRatio 的零值语义】这三个量在计划 4/4 的任务 10
// 之前**恒为零值**：块内行数只有从产物 footer 才拿得到（任务 10 的 RowCount），
// 而「未压缩字节/行」与压缩比都需要行数作分母。因此零值 = 「本轮无样本」，
// 而**不是**「0 行」/「0 字节每行」/「比值 0」——消费方必须先判 Sealed > 0 再读它们。
// 本任务只承重「计数与分类」：把未知量显式留成零值，好过拿 0 当分母算出 Inf/NaN
// 混进告警（那会把「还没实现」伪装成「观测结论」）。
type RoundResult struct {
	Eligible, Sealed                                                int
	PartExists, RowMismatch, SealFailed, TooManyRows, UnknownFailed int
	MaxRows                                                         int
	MinBytesPerRow                                                  int64
	MinRatio                                                        float64
	Duration                                                        time.Duration
}

// Failed 是「需要人看一眼」的失败数：可重试的（行数不符 / 封块失败 / 未分类）
// 加永久的（超限块）。**不含 PartExists** —— 产物已存在是幂等无害的预期状态，
// 把它算进失败会让每天有一个恒定的「失败」告警，最终让真告警失效。
func (r RoundResult) Failed() int {
	return r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed
}

type Runner struct {
	opts   Options
	sealer Sealer
	// logger 在构造期由 resolveLogger 定好。它不是第二个配置源：Options.Slog /
	// Options.Logf 的优先级只在那一个函数里有定义。
	logger *slog.Logger
}

// Options 返回一份配置副本。Options 里有函数字段，调用方拿到的是同一批函数值
// （这是可接受的：它们本就无状态，注入的替身也不需要被复制）。
func (r *Runner) Options() Options { return r.opts }

// resolveLogger 决定日志的落地面（任务 2）。优先级三级：
//
//	① Options.Slog 显式注入 ⇒ 原样使用：结构化字段直达调用方给的 sink（systemd
//	   的 journal、看板采集器都可能在这里接手）。
//	② 否则 Options.Logf —— **只在 Slog == nil 时使用**。它是任务 1 已有的注入面
//	   （Options.normalize 把默认值填成 log.Printf），本任务保留它：运维脚本与既有
//	   测试都按它注入。这里把它**桥接**成 slog 的 sink，而不是另开一条 printf 路径
//	   —— 两条并行的打印路径必然漂移，而日志是出事时唯一的现场。
//	③ 二者皆无 ⇒ slog.Default()（进程级默认）。如实登记：normalize 会保证 Logf
//	   非 nil ⇒ 走 NewRunner 的正常路径到不了这一级；它是防御分支，为的是让
//	   resolveLogger 在「Options 未 normalize」时也不返回 nil logger（nil logger
//	   会在第一轮 panic，比日志缺失更糟）。
func resolveLogger(opts Options) *slog.Logger {
	if opts.Slog != nil {
		return opts.Slog
	}
	if opts.Logf != nil {
		return slog.New(slog.NewTextHandler(logfWriter{logf: opts.Logf}, nil))
	}
	return slog.Default()
}

// logfWriter 把 slog 的每条记录交给 printf 形态的 Logf（③ 未生效时这也是
// `log.Printf` 的默认落地面）。它保证「日志形态」只有一种：级别、消息、结构化
// 字段全部经过 slog，而 Logf 只是一个 sink。
type logfWriter struct {
	logf func(format string, args ...any)
}

func (w logfWriter) Write(p []byte) (int, error) {
	// TextHandler 每条记录自带结尾换行；log.Printf 只在缺失时才补一个 ⇒ 直接透传。
	w.logf("%s", string(p))
	return len(p), nil
}

// fieldArgs 把 Alert.Fields 摊成 slog 的 key/value 序列，**按 key 排序**：map 的
// 迭代序是随机的，不排序会让同一种故障的两次日志无法直接 diff。这是 Alert.Fields
// 与日志字段之间**唯一**的转换点 —— 键名只有一份来源（alert.go 的稳定字段集）。
func fieldArgs(fields map[string]any) []any {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, 0, 2*len(keys))
	for _, k := range keys {
		args = append(args, k, fields[k])
	}
	return args
}

func NewRunner(opts Options) (*Runner, error) {
	if opts.Root == "" {
		return nil, errors.New("tracesealer: Root 不能为空")
	}
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	if opts.NewSealer == nil {
		opts.NewSealer = func(so ta.SealerOptions) (Sealer, error) { return ta.NewSealer(so) }
	}
	s, err := opts.NewSealer(ta.SealerOptions{
		Root:                opts.Root,
		ZstdBinary:          opts.ZstdBinary,
		ZstdLevel:           opts.ZstdLevel,
		ZstdWindowBytes:     opts.ZstdWindowMB << 20,
		MinAge:              opts.MinAge,
		ExpectedZstdVersion: opts.ExpectedZstdVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("构造封块器失败: %w", err)
	}
	return &Runner{opts: opts, sealer: s, logger: resolveLogger(opts)}, nil
}

// logPartFailure 打印**单个块**的失败。一块失败绝不中断整轮（见 Round 的注释），
// 所以这里是日志而不是返回。字段名与轮次汇总共用同一套（kind / ref / spool / err），
// 便于按 ref 从汇总下钻到具体块。
func (r *Runner) logPartFailure(kind FailureKind, note string, task ta.SealTask, err error) {
	r.logger.Error("trace-sealer 封块失败",
		"kind", string(kind),
		"note", note,
		"ref", fmt.Sprintf("%s/%s/part-%06d", task.DT, task.Inst, task.Part),
		"spool", task.SpoolPath,
		"err", err)
}

// Round 跑一轮：发现 → 逐个封块 → 分类计数 → 汇总 → 打日志。
//
// 【为什么一个 part 失败不能中断整轮】冻结包的发现路径是纯目录扫描、天然幂等；
// 一个坏块（例如超限块）应当只影响它自己。若在第一个失败处 return，
// 后面所有健康块都会被无限期拖延 —— 而它们本来可以正常封块读回。
//
// 【日志形态（任务 2）】逐块失败走 Error（带 kind/ref/spool/err），轮次汇总走
// 心跳 Info（每轮必打）+ 告警线（级别随 Alert.Severity：Warn / Error）。
// 告警规则匹配的是**稳定串** Alert.Reason，不是错误文本。
func (r *Runner) Round(ctx context.Context) (RoundResult, error) {
	start := r.opts.Now()
	var res RoundResult

	tasks, err := r.sealer.ListPendingSeals()
	if err != nil {
		res.Duration = r.opts.Now().Sub(start)
		r.logger.Error("trace-sealer 列举待封块失败", "err", err)
		return res, fmt.Errorf("列举待封块失败: %w", err)
	}
	res.Eligible = len(tasks)

	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			res.Duration = r.opts.Now().Sub(start)
			return res, err
		}
		sealCtx, cancel := context.WithTimeout(ctx, r.opts.SealTimeout)
		err := r.sealer.Seal(sealCtx, task)
		cancel()

		kind := Classify(err)
		switch kind {
		case "":
			res.Sealed++
		case KindPartExists:
			res.PartExists++
			// 幂等无害：不告警。下轮扫描仍会看到同一 spool（它不会被删），
			// 这是**预期**状态（产物已存在、spool 保留），不需要任何动作。
		case KindRowMismatch:
			res.RowMismatch++
			r.logPartFailure(kind, "可重试（保留 spool，等下一轮）", task, err)
		case KindSealFailed:
			res.SealFailed++
			r.logPartFailure(kind, "可重试（保留 spool，等下一轮）", task, err)
		case KindTooManyRows:
			res.TooManyRows++
			// 永久：重试永远不会成功。文案必须点明「需人工介入」，否则运维会
			// 一直等下一轮自己好 —— 它不会好。
			r.logPartFailure(kind, "永久（需人工介入，重试不会成功）", task, err)
		default:
			res.UnknownFailed++
			// 未分类 = 「我们还不知道这是什么」，不是「没问题」⇒ 按最坏处理。
			r.logPartFailure(kind, "未分类（按最坏处理）", task, err)
		}
	}
	res.Duration = r.opts.Now().Sub(start)

	alert := Summarize(res)
	summary := append([]any{"reason", alert.Reason}, fieldArgs(alert.Fields)...)
	summary = append(summary, "duration", res.Duration.String())
	// 心跳：**每轮结束必打**（它不在任何条件分支里 —— 静态检查钉住这一点）。
	// 运维侧的告警规则是「>3 × poll-interval 没看到这条 ⇒ 进程挂了 / 被 systemd
	// mask 了」；它一旦被挪进 if，就会在正是需要它的那种故障下消失。
	r.logger.Info("trace-sealer round", summary...)
	// 告警线：级别随分级（WARN = 等下一轮；ERROR = 永久失败或未分类）。
	// 全绿轮次（SeverityNone）**不打**这条 —— 每天 288 条恒定告警会把真故障埋掉。
	switch alert.Severity {
	case SeverityError:
		r.logger.Error("trace-sealer 轮次告警", summary...)
	case SeverityWarn:
		r.logger.Warn("trace-sealer 轮次告警", summary...)
	}
	return res, nil
}
