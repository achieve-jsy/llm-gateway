package tracesealer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// RoundResult 是一轮封块的汇总（对外可观测面：日志、退出码、告警、看板）。
//
// 【MaxRows / MinBytesPerRow / MinRatio 的来源与零值语义】这三个量由
// `observeSealedPart` 从**每个成功封出的 part 的产物 footer** 采集（任务 10）：
// 块内行数只有从 footer 才拿得到（`RowCount`），而「未压缩字节/行」与压缩比都需要
// 行数作分母。因此零值 = 「本轮无样本」—— 没有成功封出的块，或者块的 footer 读不
// 出来（后者会另留一条带稳定 reason 的 ERROR，见 observeSealedPart），而**不是**
// 「0 行」/「0 字节每行」/「比值 0」。消费方必须先判 `Sealed > 0` 再读它们。
//
// 【为什么依然不拿 0 当分母】非法分母（0 行 / 未压缩字节为 0）不产生 Inf/NaN，
// 也不被写进这三个量：宁可「本轮无样本」，也不把一个**凑出来的**读数混进 A8/A6
// 的观测面 —— 那会把「没观测到」伪装成「观测结论」。
type RoundResult struct {
	Eligible, Sealed                                                int
	PartExists, RowMismatch, SealFailed, TooManyRows, UnknownFailed int
	MaxRows                                                         int
	MinBytesPerRow                                                  int64
	MinRatio                                                        float64
	Duration                                                        time.Duration
	// RootUnavailable = true ⇒ 本轮因**根目录不可用**而完全没有调用 Seal（护栏在发现
	// 阶段之前就拦下了它）。它与「本轮没有可封块」是两件事：前者是环境级故障、要人去看
	// 挂载；后者是正常的空轮次。两者若共用零值/同一个字段，看板会把「盘掉了」读成
	// 「今天没活干」。
	RootUnavailable bool
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
	// guard 是根目录护栏（任务 3）：构造期建立启动基线，每轮复检。
	guard *RootGuard
	// logger 在构造期由 resolveLogger 定好。它不是第二个配置源：Options.Slog /
	// Options.Logf 的优先级只在那一个函数里有定义。
	logger *slog.Logger
	// lock 是跨进程单实例锁（任务 4）：同一个 root 只允许一个 sealer。systemd 只
	// 保证**它自己**起一个，挡不住运维手工 --once 与常驻循环撞车（见 lock.go）。
	lock *Lock
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
	// 护栏在**构造期**建立启动基线：配错根目录（不存在 / 是 "/" / 是相对路径 / 挂载
	// 身份取不到）必须在启动那一秒就响亮失败，而不是等第一个 poll 周期之后。
	guard, err := NewRootGuard(opts.Root, opts.Identity)
	if err != nil {
		return nil, fmt.Errorf("构造根目录护栏失败: %w", err)
	}
	// 【装配次序本身是契约】normalize() → NewRootGuard()（会 stat 根目录、**不建目录**）
	// → AcquireLock()。锁文件必须落在 root 之内（默认 <root>/.sealer.lock，由
	// normalize 填好）：root 之外没有可写保证（`ProtectSystem=strict` +
	// `ReadWritePaths=/datalog`），而且放在 root 里才能被「同一个 root 只有一个写者」
	// 这条语义覆盖。反过来（先取锁再 stat 根目录）也不可：root 不存在时锁文件会先
	// 失败成「打开锁文件失败」这种把运维指向错误方向的信息。
	lock, err := AcquireLock(opts.LockFile)
	if err != nil {
		return nil, err
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
		// 装配失败必须把已取得的资源释放干净，否则一次配错会把锁一直握到进程退出。
		_ = lock.Release()
		return nil, fmt.Errorf("构造封块器失败: %w", err)
	}
	return &Runner{opts: opts, sealer: s, guard: guard, logger: resolveLogger(opts), lock: lock}, nil
}

// Close 释放单实例锁。**进程退出前必须调用**（`cmd/trace-sealer` 的 `defer r.Close()`）；
// 幂等 —— defer 与显式调用两条路径都会走到，第二次调用无害。
//
// 注意：进程被杀死时内核会关闭 fd、flock 随之释放，因此 Close 不是「锁的最后防线」，
// 而是**可控退出**路径上的确定性释放。
func (r *Runner) Close() error {
	if r == nil || r.lock == nil {
		return nil
	}
	l := r.lock
	r.lock = nil
	return l.Release()
}

// logRootUnavailable 打护栏失败的那条 ERROR。reason 是**稳定串**
// （root_unavailable / root-identity-mismatch，与网关侧 spooler.go 的同一套命名
// 一致）：运维的采集规则与看板按 reason 匹配，不按错误文本。
func (r *Runner) logRootUnavailable(err error) {
	reason := "root_unavailable"
	if errors.Is(err, ErrRootIdentityMismatch) {
		reason = "root-identity-mismatch"
	}
	r.logger.Error("trace-sealer 根目录护栏未通过，本轮**不封任何块**（也不补目录）",
		"reason", reason, "root", r.opts.Root, "err", err)
}

// Loop 常驻循环：跑一轮 → 等一个周期 → 再跑一轮。**轮次严格串行**。
//
// 【为什么先跑一轮再等】启动即处理积压；更重要的是让「根目录配错了」在启动那一秒
// 就出现在日志里，而不是等一个 poll 周期（默认 5 分钟）之后。
//
// 【为什么轮次不能并发】并发的两个 Round 会对同一 (dt,inst,part) 同时 Seal，而冻结包
// 明确要求同一个三元组串行（两块共用同一个 .tmp）。
//
// 【为什么环境级失败不退出】退出会被 systemd 按 RestartSec 反复拉起，形成更密的重试；
// 而根目录不可用是环境问题，重启不会修好它。心跳（每轮一条汇总）才是它的告警面。
func (r *Runner) Loop(ctx context.Context) error {
	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := r.Round(ctx); err != nil {
			if ctx.Err() != nil {
				// 收到 SIGTERM/SIGINT：干净退出，封到一半的那块由 ctx 取消路径处理。
				return nil
			}
			r.logger.Error("trace-sealer 本轮失败，下一个周期重试",
				"err", err, "poll_interval", r.opts.PollInterval.String())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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

// observeSealedPart 采集本轮成功封出的 part 的观测量（A8 行数分布、A6 压缩比）。
//
// 【为什么它只是「观测」而不是「判定」】A8 的判定门槛（`max(rows)` 相对
// `MaxTrailerEntries` 的余量）是**人的决策**：真的越界时要在运行手册里登记「按行数
// 滚动」并重新标定，而不是让 sealer 自己改行为。因此本函数只写 RoundResult 的三个
// 极值/最小值与日志，绝不因为 rows 接近上界而拒绝、回滚或改写已经封好的产物。
//
// 【为什么读不出来只记日志、不改计数】`Sealed` 已经在调用点累加 —— 冻结包确确实实
// 把产物写出来了；读不出 footer 是**观测面**的问题，不是封块失败。把它并进失败计数
// 会让「一次观测缺陷」把整轮判成故障。但它也绝不静默：`footer_unreadable` /
// `artifact_stat_failed` 两条 ERROR 带**稳定 reason**，日志规则按 reason 匹配。
// 「读不到就当 0 行」正是 A8 最不可接受的一类失效（分布会画成「块都很小」，而没人
// 会去查）。
//
// 【每个 part 一行 INFO，每轮一行汇总】per-part 的 INFO 让运维能下钻到「是哪一块
// 撑大的」；轮次汇总（`Round` 末尾那条心跳）带上 max_rows / min_bytes_per_row /
// min_ratio 三个量（字段集在 alert.go 的 Summarize 里，恒在）。两者一起构成 A8/A6 的
// **持续**观测面。
func (r *Runner) observeSealedPart(res *RoundResult, task ta.SealTask) {
	refStr := fmt.Sprintf("%s/%s/part-%06d", task.DT, task.Inst, task.Part)
	partPath := ta.NewPaths(r.opts.Root).PartFile(ta.Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})

	rows, err := RowCount(partPath)
	if err != nil {
		r.logger.Error("trace-sealer 读不出产物的行数（本块的观测量不计入本轮；产物本身已经封好）",
			"reason", "footer_unreadable", "ref", refStr, "part_path", partPath, "err", err)
		return
	}
	fi, err := os.Stat(partPath)
	if err != nil {
		r.logger.Error("trace-sealer 读不到产物的字节数（本块的观测量不计入本轮；产物本身已经封好）",
			"reason", "artifact_stat_failed", "ref", refStr, "part_path", partPath, "err", err)
		return
	}
	artifactBytes := fi.Size()
	// RowCount 成功 ⇒ 文件至少有 FooterSize 字节 ⇒ artifactBytes ≥ 32 恒成立，因此
	// 这里**不**再判它一次（那条分支是证明不可达的死代码，E64① 要求要么删掉、要么
	// 由用例触达 —— 我们选择删掉）。
	if rows <= 0 || task.UncompressedBytes <= 0 {
		r.logger.Warn("trace-sealer 观测量分母非法（跳过本块，不发明读数）",
			"reason", "observation_skipped", "ref", refStr,
			"rows", rows, "artifact_bytes", artifactBytes,
			"uncompressed_bytes", task.UncompressedBytes)
		return
	}

	// 0 行没有「每行字节数」这个概念。上面的守卫已经拦下 rows <= 0，这里再加一层
	// 只是为了「守卫将来被改动时也不会除以 0」—— 除零是 panic，不是可恢复的错误。
	bytesPerRow := int64(0)
	if rows > 0 {
		bytesPerRow = task.UncompressedBytes / int64(rows)
	}
	ratio := float64(task.UncompressedBytes) / float64(artifactBytes)

	if rows > res.MaxRows {
		res.MaxRows = rows
	}
	// `== 0 ||` 短路是零值语义的**唯一**实现点：0 表示「本轮还没有样本」，不能被
	// 当成一个真实读数参与比较（否则第一个块之后的最小值就永远定死在那里）。
	if res.MinBytesPerRow == 0 || bytesPerRow < res.MinBytesPerRow {
		res.MinBytesPerRow = bytesPerRow
	}
	if res.MinRatio == 0 || ratio < res.MinRatio {
		res.MinRatio = ratio
	}

	partFields := []any{
		"ref", refStr,
		"rows", rows,
		"bytes_per_row", bytesPerRow,
		"ratio", ratio,
		"artifact_bytes", artifactBytes,
		"uncompressed_bytes", task.UncompressedBytes,
		"row_cap_pct", float64(rows) / float64(MaxTrailerEntries) * 100,
	}
	r.logger.Info("trace-sealer sealed part", partFields...)
	// A8 的预警线：行数一旦越过上界的一半，余量就只剩 2 倍（冻结包超限时返回
	// ErrTooManyRows ⇒ 该块**永远不会**被封出、spool 无限增长）。这是**预警**，
	// 不是拒绝：处置是人的决策（登记「按行数滚动」或下调 TRACE_BODY_BLOCK_MB）。
	if rows > MaxTrailerEntries/2 {
		r.logger.Warn("trace-sealer 块内行数已超过 maxTrailerEntries 的一半（A8 余量不足 2 倍）", partFields...)
	}
}

// Round 跑一轮：护栏复检 → 发现 → 逐个封块 → 分类计数 → 汇总 → 打日志。
//
// 【为什么护栏是本轮的第一件事】根目录不可用时「发现」这一步看到的是一片空（挂载点
// 目录还在、只是背后换了文件系统），若照常往下走就会把正文封到**错的那块盘**上。
// 因此护栏失败 ⇒ res.RootUnavailable = true、打一条带稳定 reason 的 ERROR、直接返回，
// 既不 ListPendingSeals 更不 Seal。**绝不**用「补一个目录」来修它。
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

	if err := r.guard.Check(); err != nil {
		res.RootUnavailable = true
		res.Duration = r.opts.Now().Sub(start)
		r.logRootUnavailable(err)
		return res, err
	}

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
			// 观测量（A8/A6）在**封块成功之后**采集：产物已经在盘上，读它的 footer
			// 不会失败成「封块失败」，也不会改变分类结果。
			r.observeSealedPart(&res, task)
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
