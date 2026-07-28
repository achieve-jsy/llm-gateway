package tracearchive

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 路径分量校验（dt 与 inst 必须成对看待，一处漏掉就是同一个洞的另一半）。
//
// 两个分量都直接拼进目录名（<root>/dt=<dt>/inst=<inst>），而 filepath.Join
// 会做路径清理：
//   - Inst="../green" 会被清成 <root>/dt=X/green，越过 inst= 子树，把两个实例
//     的正文混进同一目录；
//   - dt="../x" 会被清成 <root>/x，越过 dt= 子树。
//
// 因此两者都用严格取值而不是「非空」：
//   - inst：枚举白名单（见 NewWriter）；
//   - dt：严格日期格式（见 validateDT）。
const (
	instBlue  = "blue"
	instGreen = "green"

	// dateLayout 是 dt 的唯一合法形式：YYYY-MM-DD，且必须补零。
	// 不补零（"2026-9-24"）会让同一天出现两个目录名，而点读只能按 Ref.DT
	// 拼出其中一个路径，另一个目录里的正文就永远读不到。
	dateLayout = "2006-01-02"
)

// validateDT 校验归属天分量（与 NewWriter 的 Inst 白名单同属「路径分量校验」，
// 两者必须成对看待，见上方常量块的说明）。
//
// time.Parse 一次挡住四类输入：
//   - 路径穿越与空串（"../x"、""）；
//   - 长度不符（"20260924"、"26-09-24"）；
//   - 未补零（"2026-9-24"）；
//   - 非法日期（"2026-13-01"、"2026-02-30"）。
//
// 不设哨兵错误：与 Inst 同理——这是调用方无从分支处理的参数错误，
// 加进本包的错误集合只会扩冻结接口。
func validateDT(dt string) error {
	if _, err := time.Parse(dateLayout, dt); err != nil {
		return fmt.Errorf("tracearchive: dt 必须是 %s 形式的合法日期，实际 %q", dateLayout, dt)
	}
	return nil
}

// WriterOptions 是追加写入器的配置。
type WriterOptions struct {
	Root         string        // 归档根目录，如 /datalog/trace-archive-v4
	Inst         string        // 实例标识，只能是 "blue" / "green"
	BlockBytes   int64         // 封块大小阈值；0 取 defaultBlockBytes
	SealInterval time.Duration // 封块时间阈值；0 取 defaultSealInterval
}

// SealTask 描述一个待封块的块。
type SealTask struct {
	DT                string
	Inst              string
	Part              int
	SpoolPath         string
	Rows              int // 0 表示未知（任务 6 的 ListPendingSeals 扫目录时不知道行数）
	UncompressedBytes int64
}

// curBlock 是内存中当前正在追加的块状态。
type curBlock struct {
	dt        string
	part      int
	rows      int
	bytes     int64
	startedAt time.Time
	f         *os.File
	w         *bufio.Writer
}

// Writer 负责把正文行顺序追加到 spool。
//
// 请求路径上只做顺序追加、不做压缩（规格 §7.1）。压缩由 Sealer 在后台完成。
// Writer 不是并发安全的：TraceWriter 单协程消费事件队列，因此无需加锁；
// 唯一需要互斥的是 PendingSeals 与 Append 都可能触碰 cur，故用 mu 保护。
type Writer struct {
	opts  WriterOptions
	paths Paths

	mu  sync.Mutex
	cur *curBlock
}

// NewWriter 构造写入器。
func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	// inst 是路径分量，与 dt 的校验成对（见文件头的「路径分量校验」与 validateDT）。
	// 空值也被这条白名单涵盖（"" 不在枚举里），因此不再单列「非空」分支：
	// 一个永远无法被测试区分的分支就是零覆盖的守卫。
	if opts.Inst != instBlue && opts.Inst != instGreen {
		return nil, fmt.Errorf("tracearchive: Inst 必须是 %q 或 %q，实际 %q", instBlue, instGreen, opts.Inst)
	}
	if opts.BlockBytes <= 0 {
		opts.BlockBytes = defaultBlockBytes
	}
	if opts.SealInterval <= 0 {
		opts.SealInterval = defaultSealInterval
	}
	return &Writer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Append 追加一行，返回该行的定位键。line 末尾的换行可有可无。
//
// dt 是路径分量（与 NewWriter 的 Inst 白名单成对），这里的校验是唯一的
// dt 入口：AppendFailed 复用本方法，因此两个写入入口共用同一道校验。
//
// 写入失败时不推进行号：bufio 的错误是粘性的，一次写失败后该块再也写不进
// 任何字节，调用方只能走 AppendFailed。因此不存在「行号已推进而字节没写」
// 的窗口（承重约束 3）。
func (w *Writer) Append(dt string, line []byte) (Ref, error) {
	if err := validateDT(dt); err != nil {
		return Ref{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	cur, err := w.ensureBlockLocked(dt)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{DT: dt, Inst: w.opts.Inst, Part: cur.part, Row: cur.rows}

	n, err := cur.w.Write(trimNewline(line))
	if err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加第 %s 行失败: %w", ref, err)
	}
	if err := cur.w.WriteByte('\n'); err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加换行失败: %w", err)
	}

	cur.rows++
	cur.bytes += int64(n) + 1
	return ref, nil
}

// failedRowMarker 是「本行正文从未落盘」的占位行内容（详见 AppendFailed）。
//
// 取 null 而不是空行或 {}：
//   - 空行会让任何按行解析 .jsonl 的工具炸掉（产物里每一行都应是合法 JSON）；
//   - {} 与「合法的空正文」无法区分；
//   - null 是合法 JSON，而 spool 的每一行都是正文对象
//     （request_headers/request_body/response_body 那组键），
//     因此它不可能与真实正文混淆。
var failedRowMarker = []byte("null")

// AppendFailed 登记「本行正文落盘失败」：推进行号并落一个标记行。
//
// 行号必须照常推进：PG 元数据行的 row_seq 由同一计数器分配，若这里不推进，
// 后续所有行的 row_seq 都会与产物文件错位，点读将静默取到错误的行。
//
// 【为什么必须写标记行，而不是只推进行号不写字节】产物格式强制行稠密：
// footer 的 row_count 是产物内行数，trailer 是「每行一个差分 varint」的
// 偏移表（index.go 的 encodeTrailer）—— varint 差分编码根本没有表达
// 「本行没有偏移」的能力，它假定行是稠密的。若这里不写字节，该行之后的
// 物理行序会比 row_seq 少 1，而读取侧无论走 spool 兜底（Reader.readFromSpool
// 取「第 row 行」）还是走产物（trailer 的第 row 条）都是按行取，
// 于是 row=N 会被读成第 N+1 行的正文 —— 静默返回错误的正文，是本项目
// 最不能接受的一类缺陷（也解释了任务 6 的行数交叉校验为何会以
// ErrRowCountMismatch 拒绝封块：那是同一个矛盾的另一表征）。
//
// 读取侧约定：遇到标记行应返回 ErrBodyMissing（正文从未落盘），
// 不得把 "null" 当作正文返回。该约定由任务 7 的 Reader 落实。
//
// dt 的路径分量校验由 Append 承担（本方法复用它），因此非法 dt 在这里同样报错。
func (w *Writer) AppendFailed(dt string) error {
	// 刻意复用 Append，而不是自己再写一遍写入逻辑：
	// 标记行与真实正文在 trim/换行/行号推进/字节计数上共用同一段代码，
	// 「物理行序 == row_seq」由构造保证，而不是靠两处实现同步维护。
	if _, err := w.Append(dt, failedRowMarker); err != nil {
		return fmt.Errorf("tracearchive: 写正文缺失标记行失败: %w", err)
	}
	return nil
}

// PendingSeals 返回当前达到封块条件的块。
//
// 双触发条件（规格 §5.3.3）：达到大小阈值，或距块首行写入超过时间阈值。
// 时间条件不可省——低峰天若不按时间封块，块永远达不到大小阈值，
// 正文写了却读不到。
//
// flush 必须【无条件】执行，而不是只在「有任务要产出」时做：
// 这是方案 C 下网关唯一的周期性 flush 入口（定时器按 flush_interval 调它，
// 规格 §7.1）。若未达阈值就跳过 flush，一个刚建立的块在其生命周期的前
// SealInterval 内磁盘上始终是空文件，而 mtime 停留在它被创建的时刻 ——
// sealer 的 minAge 判断会把这种「安静的活跃块」当成「已停止追加」，
// 于是封出一个空产物并删掉 spool，缓冲里的正文全部丢失。
// 这与 D3（忘记 flush）是同一类静默丢数据，只是触发条件是「没到阈值」。
//
// 同理，flush 失败必须上抛而不是静默：sealer 会直接读 spool 文件，
// 缓冲没落盘它就把不完整的块当完整的封掉。
func (w *Writer) PendingSeals() ([]SealTask, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur := w.cur
	if cur == nil {
		return nil, nil
	}
	if err := cur.w.Flush(); err != nil {
		return nil, fmt.Errorf("tracearchive: flush 写缓冲失败: %w", err)
	}
	// 空块（建了文件但一行都没写进去）没有可封的内容。这条守卫只能通过
	// 「Append 写失败」触及，而写失败经公开 API 无法注入，故无对应用例；
	// 保留它是为了不让封块器拿到一个 0 行的 spool。
	if cur.rows == 0 {
		return nil, nil
	}
	bySize := cur.bytes >= w.opts.BlockBytes
	byTime := time.Since(cur.startedAt) >= w.opts.SealInterval
	if !bySize && !byTime {
		return nil, nil
	}
	return []SealTask{{
		DT:                cur.dt,
		Inst:              w.opts.Inst,
		Part:              cur.part,
		SpoolPath:         w.paths.SpoolFile(Ref{DT: cur.dt, Inst: w.opts.Inst, Part: cur.part}),
		Rows:              cur.rows,
		UncompressedBytes: cur.bytes,
	}}, nil
}

// MarkSealed 在封块成功后清空当前块状态。由封块者在 rename 完成后调用
// （任务 8 的端到端流程即如此）。
//
// 这里必须连同旧句柄一起关掉：只把 cur 置 nil 会让该块的 *os.File 泄漏
// （每封一块泄漏一个 fd，长跑进程最终 EMFILE）。调用点已冻结为无返回值，
// 因此 close 错误只能丢弃；但此时 spool 已被封块器完整读取，未落盘的缓冲
// 不再可能有读者 —— 这与 PendingSeals 必须上抛 flush 错误并不矛盾：
// 那里是为了不把不完整的块交给 sealer，这里已经晚于 sealer 读文件。
func (w *Writer) MarkSealed(dt string, part int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.dt == dt && w.cur.part == part {
		_ = w.closeCurLocked()
	}
}

// Close 关闭当前块文件。注意：这不等价于封块——未封块的正文由 spool 兜底读取。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeCurLocked()
}

func (w *Writer) closeCurLocked() error {
	if w.cur == nil {
		return nil
	}
	var firstErr error
	if err := w.cur.w.Flush(); err != nil {
		firstErr = err
	}
	if err := w.cur.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	w.cur = nil
	return firstErr
}

// ensureBlockLocked 确保存在可追加的当前块，必要时按目录内容恢复块序号。
func (w *Writer) ensureBlockLocked(dt string) (*curBlock, error) {
	if w.cur != nil && w.cur.dt == dt {
		return w.cur, nil
	}
	if err := w.closeCurLocked(); err != nil {
		return nil, err
	}

	next, err := w.nextSeqLocked(dt)
	if err != nil {
		return nil, err
	}

	spoolPath := w.paths.SpoolFile(Ref{DT: dt, Inst: w.opts.Inst, Part: next})
	if err := os.MkdirAll(filepath.Dir(spoolPath), 0o755); err != nil {
		return nil, fmt.Errorf("tracearchive: 建 spool 目录失败: %w", err)
	}
	// O_APPEND：进程重启后继续往同一个 spool 追加，不覆盖已写内容
	f, err := os.OpenFile(spoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tracearchive: stat spool 失败: %w", err)
	}

	w.cur = &curBlock{
		dt:        dt,
		part:      next,
		bytes:     info.Size(),
		startedAt: time.Now(),
		f:         f,
		w:         bufio.NewWriterSize(f, 1<<20),
	}
	return w.cur, nil
}

// nextSeqLocked 以目录内容为权威来源计算下一个块序号（规格 §7.3）。
//
// 不引入 PG 表或独立指针文件：目录内容本身即指针，少一个需要保持一致的状态。
// 已封块产物与未封块 spool 取较大者，+1。
func (w *Writer) nextSeqLocked(dt string) (int, error) {
	maxSeq := -1
	for _, dir := range []string{w.paths.PartDir(dt, w.opts.Inst), w.paths.SpoolDir(dt, w.opts.Inst)} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("tracearchive: 读目录 %s 失败: %w", dir, err)
		}
		for _, e := range ents {
			if seq, ok := parsePartSeq(e.Name()); ok && seq > maxSeq {
				maxSeq = seq
			}
		}
	}
	return maxSeq + 1, nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
