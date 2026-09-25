package tracesealer

import (
	"context"
	"errors"
	"fmt"
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
}

// Options 返回一份配置副本。Options 里有函数字段，调用方拿到的是同一批函数值
// （这是可接受的：它们本就无状态，注入的替身也不需要被复制）。
func (r *Runner) Options() Options { return r.opts }

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
	return &Runner{opts: opts, sealer: s}, nil
}

// Round 跑一轮：发现 → 逐个封块 → 分类计数 → 汇总。
//
// 【为什么一个 part 失败不能中断整轮】冻结包的发现路径是纯目录扫描、天然幂等；
// 一个坏块（例如超限块）应当只影响它自己。若在第一个失败处 return，
// 后面所有健康块都会被无限期拖延 —— 而它们本来可以正常封块读回。
func (r *Runner) Round(ctx context.Context) (RoundResult, error) {
	start := r.opts.Now()
	var res RoundResult

	tasks, err := r.sealer.ListPendingSeals()
	if err != nil {
		res.Duration = r.opts.Now().Sub(start)
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
			r.opts.Logf("[trace-sealer] ERROR 封块失败 kind=%s ref=%s/%s/part-%06d spool=%s err=%v",
				kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		case KindSealFailed:
			res.SealFailed++
			r.opts.Logf("[trace-sealer] ERROR 封块失败 kind=%s ref=%s/%s/part-%06d spool=%s err=%v",
				kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		case KindTooManyRows:
			res.TooManyRows++
			// 永久：重试永远不会成功。文案必须点明「需人工介入」，否则运维会
			// 一直等下一轮自己好 —— 它不会好。
			r.opts.Logf("[trace-sealer] ERROR 封块被永久拒绝（需人工介入，重试不会成功）kind=%s "+
				"ref=%s/%s/part-%06d spool=%s err=%v", kind, task.DT, task.Inst, task.Part, task.SpoolPath, err)
		default:
			res.UnknownFailed++
			r.opts.Logf("[trace-sealer] ERROR 封块失败（未分类，按可重试处理）ref=%s/%s/part-%06d spool=%s err=%v",
				task.DT, task.Inst, task.Part, task.SpoolPath, err)
		}
	}
	res.Duration = r.opts.Now().Sub(start)
	r.opts.Logf("[trace-sealer] INFO 轮次结束 eligible=%d sealed=%d part_exists=%d row_mismatch=%d "+
		"seal_failed=%d too_many_rows=%d unknown=%d max_rows=%d min_bytes_per_row=%d min_ratio=%.2f duration=%s",
		res.Eligible, res.Sealed, res.PartExists, res.RowMismatch, res.SealFailed,
		res.TooManyRows, res.UnknownFailed, res.MaxRows, res.MinBytesPerRow, res.MinRatio, res.Duration)
	return res, nil
}
