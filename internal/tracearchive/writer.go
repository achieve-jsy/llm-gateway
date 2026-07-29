package tracearchive

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// validateLine 校验一行正文能否安全落盘（与 validateDT 同属写入前置校验：
// 都在公开入口 Append 里、都先于任何文件系统副作用）。
//
// 产物是 .jsonl，一行 = 一条原始 JSON。由此得到两条硬约束：
//
//  1. 正文里不得含裸 '\n'/'\r'。承重不变量是「一次 Append == 一条物理行」；
//     若正文自带换行，一次 Append 会产出多条物理行而行号只 +1，此后 row=N
//     会读到第 N+1 行的正文——静默错正文。这里选择**拒绝**而不是转义：
//     转义会让产物不再是「原样 JSONL」，并把一个反转义变换强加给读取侧；
//     而 JSON 序列化本身不会产生裸换行（'\n' 会被写成 "\\n"），所以含裸换行
//     的输入只可能是调用方 bug，快速失败才是对的。
//
//  2. trim 掉末尾换行后不得为空。空行不是合法 JSON，任何按行解析 .jsonl 的
//     工具都会在它上面失败——与 failedRowMarker 不用空行是同一个理由。
//
// line 末尾的换行是**分隔符不是内容**：调用方传 "a\n" 与传 "a" 等价
// （trimNewline 去掉末尾整段 '\r'/'\n'，任意个）。因此本函数只接收 trim
// 之后的字节，也就只拒绝**行内**换行，带尾换行的既有调用不受影响。
//
// 不设哨兵错误：与 dt/inst 同理，这是调用方无从分支处理的参数错误。
func validateLine(body []byte) error {
	if i := bytes.IndexAny(body, "\r\n"); i >= 0 {
		return fmt.Errorf("tracearchive: 正文必须是单行 JSON，第 %d 字节处出现裸换行（%q）；"+
			"换行只能由调用方序列化为 \\n", i, body[i])
	}
	if len(body) == 0 {
		return fmt.Errorf("tracearchive: 正文不能是空行（去掉末尾换行后长度为 0）")
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

	// poisoned 表示本块经历过一次写失败，已被隔离且不再可写。
	// 详见 Writer 的「写失败契约」与 poisonCurLocked。
	poisoned bool
}

// Writer 负责把正文行顺序追加到 spool。
//
// 请求路径上只做顺序追加、不做压缩（规格 §7.1）。压缩由 Sealer 在后台完成。
// Writer 不是并发安全的：TraceWriter 单协程消费事件队列，因此无需加锁；
// 唯一需要互斥的是 PendingSeals 与 Append 都可能触碰 cur，故用 mu 保护。
//
// # 写失败契约（盘满等）
//
// spool 每行 = 一条物理行，行号 row 由「第 row 行」定位（产物 trailer 与
// spool 兜底读取都是按行取），因此「物理行数 == 已分配行号数」是承重不变量。
// 一次写失败可能已经在盘上留下**没有换行收尾的残行**（bufio 直写 fd 时可能
// 短写）。残行的后果有两条，一条响亮、一条静默：
//   - 封块侧（响亮但持久）：任务 6 的 scanSpoolOffsets 遇到未终止的尾行会以
//     ErrSealFailed 拒绝封块（sealer.go 的「残行守卫」），于是这个块**永远
//     封不掉**，告警与重试会一直持续；若写入器还继续往同一文件追加，spool
//     会在此之上无限增长——首轮实现（写失败只「不推进行号」）正是如此。
//   - 读取侧（静默）：任务 7 的 spool 兜底读取是「顺序取第 row 行」，**不校验
//     行尾终止符**。写入器一旦在残行之后继续追加，残行就占掉一个行位，其后
//     每一行整体错位 ⇒ row=N 读到第 N+1 行的正文，静默错正文。
//
// 所以写失败时的硬要求是：**残行永远不可被封块、也不可再有后续行追加进来**。
// 本包的做法：
//
//  1. 把 spool 回退到最后一个 '\n' 边界，抹掉写了一半的残行；
//  2. 把文件改名隔离为 part-NNNNNN.jsonl.zst —— 该后缀由两条既有冻结规则
//     共同保证安全：ListPendingSeals 只收 ".jsonl" 结尾的名字，故 sealer
//     永远发现不了它（两条发现路径一起堵死）；而 parsePartSeq 接受
//     ".jsonl.zst"，故写指针仍把它算作已用序号，**块号 N 不会被复用**。
//     复用会让 PG 里旧的 (part=N, row=r) 静默读到另一个 part 的正文——
//     最坏的一类错，必须由命名规则而不是流程纪律来挡住。
//  3. 毒化该块：不再写入、不再被 PendingSeals 报出、Close 只释放 fd；
//     后续 Append/AppendFailed 会自动换到 N+1（调用方无需重置任何东西）。
//
// 隔离件保留在 spool 目录里供人工抢救：释放空间后把它改回 part-NNNNNN.jsonl，
// 因文件已回退到行边界，sealer 会封出「真实完整行数」的产物（该 part 尾部
// 丢失的行读出来是行号越界，即响亮报错，不会读到别的正文）。
//
// 【为什么不是「截断回 cur.bytes 后继续用」】cur.bytes 是逻辑已接受字节数，
// 未 flush 时大于文件实际长度，Truncate(cur.bytes) 会把文件补零延长（往
// .jsonl 里塞 NUL）；而短写也可能停在一条**已成功**行的中间，要判断盘上
// 究竟有几个完整行必须全文件扫描（最大 1.5 GiB）或维护逐行偏移表。加上
// bufio 错误粘性、其缓冲内容不可读（无法「重置缓冲继续写」），在 ENOSPC
// 下无法把该块恢复到「可继续写且可正常封块」。见报告 §11。
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

// Append 追加一行正文并返回该行的定位键。
//
// 前置条件（调用方义务）：
//   - line 是一条**单行** JSON 对象，trim 掉末尾换行后非空（校验见 validateLine）；
//   - line 不得是字面量 null——那是 AppendFailed 的失败标记，读取侧按整行
//     相等识别它（任务 7 的约定），一条真实正文若恰好是 null 会被误判成
//     「正文从未落盘」。生产侧正文是 struct 序列化（恒为 {...}），实际不会
//     撞上，故这里**不做校验**：禁止 null 会连标记行一起禁掉。这是一条
//     未成文义务，已登记在报告里。
//
// dt 是路径分量（与 NewWriter 的 Inst 白名单成对），这里的校验是唯一的
// dt 入口之一（另一个是 valiateLine 的兄弟入口，两者都在本方法内）。
//
// 写失败（盘满、只读等）时：本行不写入、**行号不推进**（返回的 Ref 无效），
// 返回错误；该块随即被隔离并停止写入（见 Writer 的「写失败契约」）。调用方
// 应把这一行标为 body_missing=true（方案 C 的降级约定），然后照常继续下一次
// 事件——下一次 Append/AppendFailed 会自动换到新 part，不需要调用方做任何
// 重置、换块或重试。此时不存在「行号已推进而字节没写」的窗口。
func (w *Writer) Append(dt string, line []byte) (Ref, error) {
	if err := validateDT(dt); err != nil {
		return Ref{}, err
	}
	body := trimNewline(line)
	if err := validateLine(body); err != nil {
		return Ref{}, err
	}
	return w.appendLineLocked(dt, body)
}

// appendLineLocked 是 Append 与 AppendFailed 共享的落盘路径。
//
// 【为什么这里刻意不做参数校验】校验必须只放在公开入口 Append 上：
// AppendFailed 走的也是这条路径，若在这里校验，标记行会被自己的守卫拒绝
// （校验「非空/非 null」之类会直接打死失败标记）。
//
// 反过来，把行号推进、字节计数、缓冲写入全部集中在**这一段**代码里，
// 是为了让标记行与真实正文由构造共享同一套不变量（trim/换行/行号连续/
// 字节计数），而不是靠两处实现各自维护。
//
// 必须持锁调用。
func (w *Writer) appendLineLocked(dt string, body []byte) (Ref, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur, err := w.ensureBlockLocked(dt)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{DT: dt, Inst: w.opts.Inst, Part: cur.part, Row: cur.rows}

	n, err := cur.w.Write(body)
	if err != nil {
		return Ref{}, w.poisonCurLocked(cur, ref, fmt.Errorf("tracearchive: 追加正文失败（%s）: %w", ref, err))
	}
	if err := cur.w.WriteByte('\n'); err != nil {
		// 走到这里时正文已进缓冲/已落盘但缺行尾换行：残行危险性与短写相同，
		// 一律按写失败处理。
		return Ref{}, w.poisonCurLocked(cur, ref, fmt.Errorf("tracearchive: 追加行尾换行失败（%s）: %w", ref, err))
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
//
// 【写失败时本方法也可能失败】首轮注释曾写「调用方只能走 AppendFailed」，
// 那是错的：bufio 的错误是**粘性**的，同一块上任何后续写入（包括本方法的
// 标记行）都必然失败，ENOSPC 场景下确实写不进标记行。现在的语义是：
// 写失败会把该块毒化并隔离，本方法下一次调用会**自动换到新 part** 再写标记行；
// 若新 part 也建不出来（盘仍然满），本方法返回错误，调用方只能把该行记为
// body_missing（其 Ref 读出来是行号越界/未归档，不会读到别的行的正文）。
func (w *Writer) AppendFailed(dt string) error {
	// 刻意复用 Append，而不是自己再写一遍写入逻辑：
	// 标记行与真实正文在 trim/换行/行号推进/字节计数上共用同一段代码，
	// 「物理行序 == row_seq」由构造保证，而不是靠两处实现同步维护。
	// 校验因此只留在公开入口 Append 上，不会被标记行自己触发。
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
	// 毒化块不可封块：它的 rows 与盘上完整行数已不可信（短写可能吃掉一条
	// 已成功行的尾部），封出去必然带错的 row_count；而且它的文件已被隔离成
	// .jsonl.zst，进程外的 sealer 也发现不了它。这里不 flush（bufio 的错误
	// 是粘性的，且文件已隔离，再写只会污染隔离件）。
	if cur.poisoned {
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
//
// 若当前块已被毒化（写过失败），这里只释放 fd，不 flush 也不重复上报那次
// 写失败（Append 已经上报过，bufio 的粘性错误只会是同一个原因）。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.poisoned {
		return w.dropCurLocked()
	}
	return w.closeCurLocked()
}

func (w *Writer) closeCurLocked() error {
	if w.cur == nil {
		return nil
	}
	if w.cur.poisoned {
		return w.dropCurLocked()
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

// dropCurLocked 丢弃当前块：不 flush、直接关闭 fd。仅用于已毒化的块——
// 它的缓冲内容要么已经写坏、要么属于已隔离的文件，flush 毫无意义。
func (w *Writer) dropCurLocked() error {
	if w.cur == nil {
		return nil
	}
	f := w.cur.f
	w.cur = nil
	return f.Close()
}

// ensureBlockLocked 确保存在可追加的当前块，必要时按目录内容恢复块序号。
func (w *Writer) ensureBlockLocked(dt string) (*curBlock, error) {
	if w.cur != nil && w.cur.dt == dt && !w.cur.poisoned {
		return w.cur, nil
	}
	if w.cur != nil && w.cur.poisoned {
		// 换 part 是写失败契约的一部分：毒化块已被隔离且不可封，直接放弃，
		// 让下面的 nextSeqLocked 算出 N+1（隔离件的 .jsonl.zst 名字仍占着 N）。
		_ = w.dropCurLocked()
	} else if err := w.closeCurLocked(); err != nil {
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

// quarantineSuffix 是写失败后被隔离的 spool 文件后缀（见 Writer 的「写失败契约」）。
//
// 选它是因为两条既有冻结规则共同给出我们要的两条性质：
//   - ListPendingSeals 只收 ".jsonl" 结尾的文件名 ⇒ 隔离件不进 sealer 的
//     发现列表（进程外扫描这条路径被堵死）；
//   - parsePartSeq 接受 ".jsonl.zst" ⇒ 写指针仍把序号 N 算作已用，N 不被复用。
//
// 【已知副作用（有意接受）】隔离件的内容是**明文 JSONL 而不是 zstd 帧**，
// 但它的名字与产物等价。若一条毒化前已成功写入的 PG 元数据带着 body_ref_part=N
// 去点读，读取路径会把隔离件当产物解析，最终返回 ErrFooterCorrupt。这个错误
// 是响亮的（符合「宁可响亮报错也不静默错正文」），但语义有误导性：真相是
// 「该块因写入失败被写入器隔离」而非「产物损坏」。
//
// 运维判据（供部署文档）：spool 目录下若出现「内容是明文 JSONL 的
// part-*.jsonl.zst」，即表示该块因写入失败被隔离，其中的正文已不能通过正常
// 路径读取；释放空间后把它改回 part-NNNNNN.jsonl 即可让 sealer 正常封块
// （文件已回退到行边界，故产物行数 = 真实完整行数）。
//
// 读取侧不为它新增分支或哨兵：保持读取路径简单，语义解释留给这条注释与报告。
const quarantineSuffix = ".jsonl.zst"

// quarantinePath 返回某块的隔离件路径：拿 partName 生成 part-NNNNNN.jsonl.zst。
//
// 必须经 partName 生成，**不能**把 quarantineSuffix 直接拼到 spool 名后面：
// spool 名已以 ".jsonl" 结尾，拼接会得到 part-NNNNNN.jsonl.jsonl.zst，而
// parsePartSeq 拒绝它 ⇒ 序号 N 不再被占住、会被下一个块复用（最坏的一类错）。
// 该性质由 TestWriterPoisonedBlockCannotBeSealed 的文件名断言兜住。
func quarantinePath(spoolPath string, part int) string {
	return filepath.Join(filepath.Dir(spoolPath), partName(part, quarantineSuffix))
}

// poisonCurLocked 处理一次写失败：抹掉残行、隔离文件、毒化块，并返回要上抛给
// 调用方的错误（用 %w 保留原始错误，调用方仍可 errors.Is(err, syscall.ENOSPC)）。
//
// 必须持锁调用。
func (w *Writer) poisonCurLocked(cur *curBlock, ref Ref, cause error) error {
	cur.poisoned = true
	spoolPath := w.paths.SpoolFile(ref)
	isolated := quarantinePath(spoolPath, ref.Part)

	// ① 回退到最后一个完整行边界，抹掉写了一半的残行。
	//
	// 界：残行长度 <= **本次失败那一行的长度**。正文内不含裸换行（validateLine），
	// 所以文件里最后一个 '\n' 之后只可能是一条写了一半的行，反向分块扫描最多
	// 覆盖「该行长度 + 一个块」，不会无限倒退；若整个文件一个 '\n' 都没有
	//（首次追加即失败），回退为 Truncate(0)。
	notes := make([]string, 0, 2)
	if err := truncateToLastNewline(spoolPath); err != nil {
		notes = append(notes, fmt.Sprintf("回退残行失败: %v", err))
	}

	// ② 隔离：改名到 sealer 看不见、但写指针仍会计数的名字（见 quarantineSuffix）。
	if err := os.Rename(spoolPath, isolated); err != nil {
		notes = append(notes, fmt.Sprintf("隔离改名失败: %v", err))
		// 退路：清空文件。0 字节 spool 里没有任何残行，只会被 sealer 封成 0 行
		// 产物（该 part 的读请求一律行号越界），即响亮报错而非错正文。
		if terr := os.Truncate(spoolPath, 0); terr != nil {
			notes = append(notes, fmt.Sprintf("清空失败: %v", terr))
		}
	}

	// ③ 毒化已由开头的赋值完成：后续 Append/AppendFailed 经 ensureBlockLocked
	// 自动换到新 part，PendingSeals 不再报出该块，Close 只释放 fd。
	note := fmt.Sprintf("该块已毒化并停止写入（隔离件 %s），"+
		"后续 Append/AppendFailed 会自动换到新 part，本行请记 body_missing=true", isolated)
	if len(notes) > 0 {
		note += "；" + strings.Join(notes, "；")
	}
	return fmt.Errorf("%w；%s", cause, note)
}

// truncateToLastNewline 把文件回退到最后一个 '\n' 之后（一个 '\n' 都没有则
// 清空为 0 字节），用于抹掉写失败留下的残行。
//
// 用只读句柄做反向分块扫描：spool 的写句柄是 O_WRONLY，读不了。
// 扫描界见 poisonCurLocked ①：最多覆盖「文件头 + 一条正文」。
func truncateToLastNewline(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	const chunk = 64 << 10
	buf := make([]byte, chunk)
	for end := size; end > 0; {
		start := end - chunk
		if start < 0 {
			start = 0
		}
		n := int(end - start)
		if _, err := f.ReadAt(buf[:n], start); err != nil {
			return err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return os.Truncate(path, start+int64(i)+1)
		}
		end = start
	}
	return os.Truncate(path, 0)
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
