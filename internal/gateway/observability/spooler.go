package observability

// 【本文件的硬约束（与 spool_gate.go 同一份约束）】不得出现任何建目录调用。
// 网关侧唯一的建目录动作在冻结包里（`internal/tracearchive/writer.go:489` 的
// ensureBlockLocked），网关自己一个目录都不建：一旦这里建目录，就会把消失的挂载点
// 在**容器可写层**重新建回来，从而掩盖 D49 要检测的「运行中挂载消失」。
//
// 集合里的 CHECKS 行用正则 `\bMkdir[A-Za-z]*\s*\(` 钉住**直接调用形态**；
// 它是启发式、**不是**结构不变量（把调用取出赋值再调、或走 exec 调 mkdir(1) 都能绕过；
// 反向它也会命中注释里「标识符紧跟 ASCII 左括号」的提及 —— 本文件的注释因此
// 一律不写该形态，这是这条检查对注释的反向约束，已登记在报告里）。
// 承重证据是行为用例：TestSpoolerLatchesWhenRootDisappears、
// TestSpoolerLatchesWhenRootIdentityChanges 与 TestSpoolerRefusesMissingRoot。

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// errRootIdentityMismatch 是「根目录身份与启动基线不一致 / 取不到身份」的哨兵。
//
// 【为什么需要它】两类根目录失败都必须闩锁，但运维动作完全不同（重新挂载 vs
// 其它可用性问题），而闩锁原因会进 ERROR 日志与看板。用哨兵把
// `root-identity-mismatch` 与 `root_unavailable` 分开，而不是靠错误字符串匹配。
var errRootIdentityMismatch = errors.New("正文根目录身份与启动基线不一致")

// mountInfoPath 是 Linux 的挂载表；读不到（macOS/Windows）时身份退化，见 defaultRootIdentity。
const mountInfoPath = "/proc/self/mountinfo"

// TraceSpoolerOptions 是网关侧 spool 的构造参数。
type TraceSpoolerOptions struct {
	Root          string
	Inst          string
	BlockBytes    int64         // 字节（调用方负责 MB → 字节换算）
	RollInterval  time.Duration // = WriterOptions.SealInterval，≤1h 且 < sealer 的 minAge 3h
	FlushInterval time.Duration // ≤1m；≤0 ⇒ 1m
	// DiskWatermarkPercent 是**内置 Gate** 的闩锁水位（0 ⇒ 90，与 Task 4 的既有默认一致）。
	// Task 6 用它把 `trace_body.disk_watermark_percent` 传进来即可，**不必**自己注入 Gate：
	// 注入 Gate 只是多一层绕路（身份包装仍会无条件套上，见 NewTraceSpooler），
	// 而且会让「水位来自配置」这件事散到两个地方。
	DiskWatermarkPercent int
	Gate                 *SpoolGate // nil ⇒ 内部建一个默认的（其解锁探针会先验根目录身份）
	Now                  func() time.Time
	// RootCheck nil ⇒ 默认实现 = 存在性检查 + 身份比对（A5-1）。注入它会**替换**掉
	// 默认的身份比对，因此注入只用于测试（如表达「挂载点消失后 stat 失败」）。
	RootCheck func(root string) error
	// RootIdentity 取根目录的身份（设备/挂载级标识）。nil ⇒ defaultRootIdentity（Linux
	// 上用 /proc/self/mountinfo 的挂载点 + 设备 + 类型，其它平台退化为设备 + 文件系统类型）。
	// 注入它可表达「根目录背后换了一个挂载点」，而无需真实 umount（A5-1）。
	RootIdentity func(root string) (string, error)
}

// SpoolerStats 是 spool 侧的可观测快照（规格 §9.3）。
type SpoolerStats struct {
	Flushes int64 // 周期 flush 成功执行的次数（含尚无当前块的情形：调用本身成功）
	Rolls   int64 // 滚动（MarkSealed）次数
	Gate    GateStats
}

// TraceSpooler 是网关对计划 1 写入原语的**唯一**使用者（规格 §4.2 的方案 C 边界）。
//
// 它做三件事：① 把正文行交给 Writer 顺序追加；② 按 ≤1m 周期无条件 flush；
// ③ 对达到阈值的块执行滚动（MarkSealed），使 sealer 之后可以安全封块并删除 spool。
// 它**不压缩、不写产物、不封块** —— 封块是计划 4 的独立 sealer 进程的职责。
//
// 【并发模型（A5-4）】AppendLine 由网关的**唯一写协程**调用；Run 由另一个 goroutine
// 跑周期 ticker。写协程的 Allow() 与 SpoolGate.Tick 的探针（CreateTemp + write + fsync +
// unlink）取**同一把锁**，所以 Run 的 goroutine 只做「复检根目录 + 无条件 flush + 滚动
// 判定」并把「探针到期」记成一个原子标志；真正的 gate.Tick（含探针 fsync）由写协程在
// AppendLine 入口消费该标志时执行 —— 于是探针不会在 Run 的 goroutine 上做 IO，
// 也就不会把周期 fsync 的抖动带进共享的闸门锁（Allow() 不在请求路径上，见 markProbeDue）。
type TraceSpooler struct {
	w             *ta.Writer
	gate          *SpoolGate
	root          string
	rootCheck     func(root string) error
	flushInterval time.Duration
	now           func() time.Time

	// probeDue / probeDueAt：Run 置、写协程消费。probeDue 用原子交换消费，
	// 保证「一个到期标志 ⇒ 恰好一次 gate.Tick」（不重复探针）。
	probeDue   atomic.Bool
	probeDueAt atomic.Int64

	// closed：Close 之后不得再写。
	//
	// 【为什么需要它】冻结包的 Writer.Close 只是关掉当前句柄并把 cur 置 nil，
	// **没有** closed 标志（`writer.go` 的 Close 契约）；此后 Append 会被当成
	// 「正常的新块」重新建 part ⇒ 一个已关闭的 spool 会被静默续写，
	// 而调用方（或 sealer）以为它已经封口。闩锁语义与「已关闭」在此等价：
	// 都是「不得再尝试写 spool，该行走 body_missing」。
	closed atomic.Bool

	mu      sync.Mutex
	flushes int64
	rolls   int64
}

// NewTraceSpooler 构造 spool 写入器。
//
// 【这里的启动期检查能挡什么、挡不住什么 —— 必须说准】网关容器以 root 运行，
// 冻结包建块路径里的建目录调用会在**容器可写层**建出一个不在挂载点上的 /datalog ——
// 正文会写到宿主根文件系统上，把 `/` 写满。但构造期的检查**只能挡「启动时未挂载」**：
// 网关依赖的冻结包自己**每次建块都会建目录**（`internal/tracearchive/writer.go:489`），
// 所以「**运行中**挂载消失」挡不住，由周期复检 + 身份基线 + 闩锁兜底
//（残余窗口 = 一个 tick）。构造失败时由 main 记录 ERROR 并退回既有路径
//（正文仍写 PG，不丢数据）。
func NewTraceSpooler(opts TraceSpoolerOptions) (*TraceSpooler, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.RootIdentity == nil {
		opts.RootIdentity = defaultRootIdentity
	}
	// 周期 flush 未设置 ⇒ 1m（规格 §7.1 ≤1m）。必须挡住 ≤0：time.NewTicker 对非正
	// 周期直接 panic，且「≤1m」这个规格上界就落在这条默认值上。
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = time.Minute
	}

	// 第二道防线（第一道在 config.LoadTraceBodyConfig）：文件系统根绝不可作 root。
	// "/" 能通过下面「是个目录」的检查，于是 spool 会建在宿主根文件系统上。
	if filepath.Clean(opts.Root) == "/" {
		return nil, fmt.Errorf("正文根目录不得是文件系统根 %q：spool 会被建在宿主根文件系统上", opts.Root)
	}

	// 身份基线（A5-1）：**无条件建立**。
	// ① 默认 RootCheck 要比对它；② 解锁探针的身份包装对**注入的 Gate 同样生效**
	//（见下面 guardProbe 的注释）⇒ 不再有「两边都注入就不取基线」的短路。
	if opts.RootCheck == nil {
		// 启动期的存在性确认（默认检查的第一半）。目录不在时取身份必然也失败，
		// 先报「未挂载」比报「取不到身份」更贴切。
		if err := rootExists(opts.Root); err != nil {
			return nil, fmt.Errorf("正文根目录 %s 不可用（未挂载？）: %w", opts.Root, err)
		}
	}
	baseline, identErr := opts.RootIdentity(opts.Root)
	if identErr != nil {
		// 取不到基线 ⇒ 响亮失败：若悄悄退化成「只验存在/可写」，挂载消失而容器层
		// 可写时一切检查都会通过（A5-1 要堵的正是这条静默错位）。
		return nil, fmt.Errorf("正文根目录 %s 无法取到身份基线（无法检测运行中挂载消失）: %w",
			opts.Root, identErr)
	}
	// identityErr 是「当前身份是否仍是启动时那个」的唯一判据（A5-1 第 2 点）。
	identityErr := func(root string) error {
		cur, err := opts.RootIdentity(root)
		if err != nil {
			// 【为什么 err 也算不一致】取不到身份就无法证明「根目录还是原来那个」。
			// 保守侧是闩锁（响亮、可恢复），不是继续写进一个来源不明的目录。
			return fmt.Errorf("%w：取当前身份失败: %v", errRootIdentityMismatch, err)
		}
		if cur != baseline {
			return fmt.Errorf("%w：基线 %q，当前 %q", errRootIdentityMismatch, baseline, cur)
		}
		return nil
	}

	if opts.RootCheck == nil {
		// 默认检查 = 存在性 + **身份比对**（A5-1 第 2 点）。
		//
		// 【为什么只验存在/可写不够】挂载消失后，冻结包下一次建块时的建目录调用会在
		// 容器可写层把根目录重建出来 ⇒ 存在性、IsDir、**写探针**全部成功。只验证它们
		// 会让网关继续把正文写进容器层：该层在重新部署后消失，归档目录看起来正常而
		// 数据在别处 —— 这是**静默**的数据错位，比「不闩锁」更危险。
		opts.RootCheck = func(root string) error {
			if err := rootExists(root); err != nil {
				return err
			}
			return identityErr(root)
		}
	}
	// 水位默认值：**不能省**。SpoolGate 自己**不**为 DiskWatermarkPercent 设默认值，
	// 而 Tick 的未闩锁分支判据是 `pct >= DiskWatermarkPercent` ⇒ 传 0 会让**每一次**
	// Tick 都闩锁（0% 水位），正文全部变 body_missing。这里就是「0 ⇒ 90」的落点。
	if opts.DiskWatermarkPercent <= 0 {
		opts.DiskWatermarkPercent = 90
	}
	if opts.Gate == nil {
		opts.Gate = NewSpoolGate(SpoolGateOptions{
			Root:                 opts.Root,
			DiskWatermarkPercent: opts.DiskWatermarkPercent,
		})
	}
	// A5-1 第 3 点：解锁探针必须**先验身份、再试写** —— 而且**对注入的 Gate 同样成立**。
	//
	// 【为什么无条件套用，而不是只做在 `opts.Gate == nil` 分支里】Task 6 为了尊重
	// 配置里的水位而注入自己的 Gate 时，注入的探针**没有**身份检查；而 identityErr
	// 是构造期的闭包，Task 6 也自建不出等价探针。后果：「挂载缺失但容器可写」时
	// 探针成功 + 水位正常 ⇒ 解锁 ⇒ 正文继续写进容器可写层，并按 ProbeInterval
	// **反复**发生 —— 正是 A5-1 要堵的静默错位。因此包装放在这里（两个分支之后）。
	//
	// 【写入时机】构造尚未返回、Gate 也还没被任何 goroutine 共享 ⇒ 这里改
	// `Gate.opts.ProbeWrite` 不与 Tick 并发（Tick 读它时持 g.mu，但那发生在构造之后）。
	// NewSpoolGate 已把 nil 探针换成 defaultProbeWrite ⇒ inner 必非 nil。
	opts.Gate.opts.ProbeWrite = guardProbe(identityErr, opts.Gate.opts.ProbeWrite)

	w, err := ta.NewWriter(ta.WriterOptions{
		Root:         opts.Root,
		Inst:         opts.Inst,
		BlockBytes:   opts.BlockBytes,
		SealInterval: opts.RollInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 tracearchive.Writer 失败: %w", err)
	}
	return &TraceSpooler{
		w:             w,
		gate:          opts.Gate,
		root:          opts.Root,
		rootCheck:     opts.RootCheck,
		flushInterval: opts.FlushInterval,
		now:           opts.Now,
	}, nil
}

// guardProbe 把「先验根目录身份、再调用 inner」包成一个解锁探针（A5-1 第 3 点）。
//
// 【为什么是独立函数而不是内联闭包】构造函数对**默认的 Gate 与注入的 Gate** 走同一条
// 包装路径（见 NewTraceSpooler 的调用点），独立出来让「包装无条件生效」这一形态可被
// 静态检查与变异直接盯住。
//
// inner 由调用方保证非 nil（SpoolGate 在 NewSpoolGate 里已把 nil 换成 defaultProbeWrite）。
func guardProbe(identityErr func(string) error, inner func(string) error) func(string) error {
	return func(root string) error {
		if err := identityErr(root); err != nil {
			return err
		}
		return inner(root)
	}
}

// rootExists 是默认检查的第一半：根目录必须存在且是目录。
func rootExists(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", root)
	}
	return nil
}

// mountKey 是 mountinfo 里一条挂载项的身份三元组。
type mountKey struct {
	mountPoint string
	dev        string
	fsType     string
}

// defaultRootIdentity 计算根目录的**身份**：挂载级标识优先，设备级标识兜底。
//
// 【为什么必须是身份，不是「存在 / 可写」】见 NewTraceSpooler 默认检查的注释：
// 挂载消失后容器可写层会让存在性、IsDir、写探针全部成功。
//
// 【取法（Linux）】读 /proc/self/mountinfo，取**覆盖该路径的最长前缀挂载项**
//（mountinfo 第 5 字段是挂载点，第 3 字段是 major:minor 设备号，"-" 之后是文件
// 系统类型），身份 = `mount=<挂载点>|dev=<major:minor>|fstype=<类型>`。
//
// 【为什么带挂载点而不是 mountinfo 第 1 字段的 mount ID】同一设备上的不同挂载有
// 不同的挂载点（判别力相同），而 mount ID 在**重新挂载**后会变 —— 用它会把一次
// 运维 remount 变成「身份永远回不到基线」的永久闩锁。挂载点 + 设备 + 类型既能
// 区分同设备的不同挂载，又对「同一挂载点的 remount」保持稳定。
//
// 【判别力边界（必须写明）】bind mount 不产生新的设备号 ⇒ 只用 Stat_t.Dev 无法
// 区分「同一设备上的 bind mount」与「容器可写层」；挂载点前缀的加入补上了这一条，
// 但它要求能读到 mountinfo。本部署的 `/datalog` 是独立卷（独立文件系统）⇒
// dev 本身已足够区分；该边界已登记。
//
// 【换卷必须重启网关（评审必修 3.4）】身份 = 挂载点 + dev + fstype，只在 dev/fstype
// 不变时对 remount 稳定。若运维把 `/datalog` 换到另一个卷（换盘 / 迁移数据），身份必然
// 不等于启动基线 ⇒ 网关**永久闩锁到重启**（每 tick 一跳变日志 + 闩锁期间的周期汇总），
// 正文全部 body_missing=true。这是**正确**的 fail-closed 语义（身份无法证明「还是原来
// 那个卷」，保守侧是停止写盘），但**换卷后必须重启网关**才能重建基线 —— 该条应写入
// runbook（runbook 属计划 4，本计划不改文档）。
//
// 【退化行为（非 Linux / 读不到 mountinfo）】不报错、不闩锁，退化为
// `dev=<Stat_t.Dev>|fstype=<statfs Type>`（设备级标识）——本机 macOS 的开发与
// 测试走这条路径。退化形态**不能**区分同设备的不同挂载，这是已知边界。
func defaultRootIdentity(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("stat %s: 无法取得设备标识", root)
	}
	var fsType uint64
	var stfs syscall.Statfs_t
	if err := syscall.Statfs(root, &stfs); err == nil {
		fsType = uint64(stfs.Type)
	}
	if data, err := os.ReadFile(mountInfoPath); err == nil {
		if key, ok := mountIdentityFor(data, root); ok {
			return fmt.Sprintf("mount=%s|dev=%s|fstype=%s", key.mountPoint, key.dev, key.fsType), nil
		}
	}
	return fmt.Sprintf("dev=%d|fstype=%d", uint64(st.Dev), fsType), nil
}

// mountIdentityFor 从 mountinfo 内容里挑出**覆盖 root 的最长前缀挂载项**。
//
// 单独拆成纯函数是为了让判别力本身可测（本机 macOS 上没有 /proc/self/mountinfo，
// 但仍可用合成内容断言：「/datalog/trace-archive-v4 的所属挂载从 /datalog 变成 /
//（挂载消失后落到容器层）」必须被判为不同身份）。
func mountIdentityFor(data []byte, root string) (mountKey, bool) {
	clean := filepath.Clean(root)
	var best mountKey
	bestLen := -1
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		// 固定字段 0..5（挂载 ID / 父 ID / major:minor / 根 / 挂载点 / 挂载选项），
		// 之后是可选字段，"-" 分隔符之后依次是 fstype / 源 / super options。
		if len(fields) < 7 {
			continue
		}
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 >= len(fields) {
			continue
		}
		mp := unescapeMountField(fields[4])
		if !pathCoveredBy(mp, clean) {
			continue
		}
		cand := mountKey{mountPoint: mp, dev: fields[2], fsType: fields[sep+1]}
		// 【并列的处理】同一路径被**多次挂载**时 mountinfo 里会有多条同挂载点的记录
		//（overmount）。只比长度的「先到者胜」会让身份随**行序**变化 —— 而行序不是
		// 稳定契约 ⇒ 行序一变身份就变 ⇒ 与启动期基线比较必然不等 ⇒ **永久闩锁**
		//（正是 TestDefaultRootIdentityIsStableForTheSameDirectory / m21 在防的
		// 「不稳定身份」这一类失效）。因此并列时按 dev、再按 fstype 取字典序最大者，
		// 使结果与行序无关。这是**确定性 tie-break**，不声称能选出「实际可见」的那条
		//（真实可见性由内核的挂载栈决定，mountinfo 的行序不是它的定义）。
		if len(mp) > bestLen || (len(mp) == bestLen && cand.beats(best)) {
			bestLen = len(mp)
			best = cand
		}
	}
	return best, bestLen >= 0
}

// beats 是 mountKey 的确定性 tie-break：并列挂载点（同一路径被多次挂载）时
// 按 dev、再按 fstype 取字典序最大者，使**结果与 mountinfo 的行序无关**。
// 目的只是消除「身份随行序漂移 ⇒ 永久闩锁」这一失效，不声称能选出内核实际可见的那条。
func (k mountKey) beats(o mountKey) bool {
	if k.dev != o.dev {
		return k.dev > o.dev
	}
	return k.fsType > o.fsType
}

// pathCoveredBy 判断 dir 是否是 path 的祖先目录（或就是它）。两侧都已 Clean。
func pathCoveredBy(dir, path string) bool {
	if dir == path {
		return true
	}
	if dir == "/" {
		return strings.HasPrefix(path, "/")
	}
	return strings.HasPrefix(path, dir+"/")
}

// unescapeMountField 还原 mountinfo 的八进制转义（空格 \040、制表 \011、换行 \012、反斜杠 \134）。
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// checkRoot 在**周期 tick、滚动前、以及任何 Append 失败**时复检根目录，失败即闩锁
//（`body_missing=true`、停止写盘、响亮 ERROR）。返回根目录是否可用。
//
// 【为什么必须有周期复检（登记 D49）】网关自身不建目录，但它依赖的**冻结包会**：
// `internal/tracearchive/writer.go:471-521` 的 ensureBlockLocked 在**每次建块**时
// 建出 spool 目录链（`:489`）。于是运行中挂载消失后，下一次 Append 会在**容器可写层**
// 重建 /datalog，正文写进容器层、随容器重建丢失。后果是**响亮的**：读取按
// `(dt, inst, part, row)` 定位不到 ⇒ `ErrNotArchived`（500 类、需人工介入），
// **不是**静默读到错误行 —— 不要把它说成静默丢数据。
// 残余窗口 = 一个 tick（复检周期即 FlushInterval，≤1m）。
//
// 【两者分工】存在性/可写性由 rootCheck 判；「根目录还是原来那个挂载点吗」由
// 身份比对判（A5-1）——后者才是挂载消失的真判据，因为容器层会让前者全部成功。
func (s *TraceSpooler) checkRoot() bool {
	err := s.rootCheck(s.root)
	if err == nil {
		return true
	}
	reason := "root_unavailable"
	if errors.Is(err, errRootIdentityMismatch) {
		reason = "root-identity-mismatch"
	}
	// 【规格 §9.3 的降级可观测性】Trip 的返回值表示「本次是否发生了跳变」——
	// 只有跳变才打**恰好一条** ERROR。`checkRoot` 每个 tick、每次 Append 失败都会跑，
	// 若无条件打日志，闩锁期间每行一条会把日志淹没（§9.3 要的是信号，不是噪音）。
	// 闩锁**期间**的周期汇总日志（如「已闩锁 Ns，原因 …」）属 Task 7 的职责，
	// 本任务只负责**跳变那一刻**这一条。
	if s.gate.Trip(reason, err) {
		log.Printf("[trace-spool] ERROR 正文落盘闩锁（跳变）：reason=%s root=%s cause=%v",
			reason, s.root, err)
	}
	return false
}

func (s *TraceSpooler) Gate() *SpoolGate { return s.gate }

// AppendLine 追加一行正文，返回定位键。
//
// 闩锁中或已 Close 时直接返回 ErrSpoolLatched，**不触碰文件系统**：调用方据此把该行
// 标成 body_missing=true（规格 §15.1 R9）。
//
// 【顺序 —— 已批准的偏离，Task 6 不得按「先 Allow 再消费」的字面顺序重写（A5-4）】
// ① 先消费「探针到期」标志并在**本 goroutine**（唯一写协程）上执行 gate.Tick ——
// 它做水位评估与（已闩锁时的）探针 fsync；② 再读 Allow()。
// 未闩锁时 ① 也是**唯一**的水位评估点（Run 不得调用 gate.Tick）：若只在已闩锁时
// 才消费标志，「超水位必须放弃正文」这条守卫就永远不会触发（Tick 的水位判定只在
// **未闩锁**分支里）—— 那会把这条守卫变成死代码。此顺序经控制者评审正式批准。
func (s *TraceSpooler) AppendLine(dt string, line []byte) (ta.Ref, error) {
	if s.closed.Load() {
		// Close 之后不得再写：冻结包的 Writer.Close 没有 closed 标志，
		// 继续 Append 会被当成「正常的新块」重建 part（静默续写）。
		return ta.Ref{}, fmt.Errorf("%w（原因：spooler 已 Close）", ErrSpoolLatched)
	}
	s.applyDueTick()
	if !s.gate.Allow() {
		return ta.Ref{}, fmt.Errorf("%w（原因：%s）", ErrSpoolLatched, s.gate.Reason())
	}
	ref, err := s.w.Append(dt, line)
	if err != nil {
		// 任何 Append 失败都复检根目录：把「盘满」与「挂载消失」分开。
		// 两者都要闩锁，但运维动作完全不同（清空间 vs 重新挂载），
		// 而闩锁原因会进 ERROR 日志。
		if !s.checkRoot() {
			return ta.Ref{}, fmt.Errorf("%w（原因：%s）", ErrSpoolLatched, s.gate.Reason())
		}
		// 写失败 ⇒ 立即闩锁。下一个事件不会再试（R9），
		// 直到探针确认可写为止；这段时间里所有行都走 body_missing。
		if s.gate.Trip("append_failed", err) {
			// 规格 §9.3：跳变那一刻恰好一条 ERROR（闩锁期间每行都打会淹没日志）。
			log.Printf("[trace-spool] ERROR 正文落盘闩锁（跳变）：reason=append_failed root=%s cause=%v",
				s.root, err)
		}
		return ta.Ref{}, err
	}
	return ref, nil
}

// markProbeDue 置「探针到期」标志（**只由 Run 的 ticker** 调用）。
//
// 【为什么把 Tick 的时机拆出来（A5-4）】SpoolGate.Tick 持同一把锁执行探针
//（CreateTemp + write + fsync + unlink）；若 Tick 在 Run 的 goroutine 上跑，探针的
// fsync 就与 `GateStats()` / `Allow()` 争**同一把锁**，把周期 IO 的抖动带进共享的闸门
// 状态。这里只记录「到点了」，真正的 Tick 由写协程在 AppendLine 入口执行。
//
// 【论据订正（评审必修 3.3）】**不要**用「阻塞 `Allow()` 直接影响请求 p99」来论证：
// `Allow()` 在生产上唯一的调用者是 `TraceSpooler.AppendLine`（spooler.go:434），
// 而它跑在 `cmd/gateway/main.go:293` 的**后台写协程**上，**不在请求路径上**。
// 真正的理由就是上面这一条：不在 Run 的 goroutine 上做探针 IO（探针含 fsync，
// 且会与 GateStats()/Allow() 争同一把锁）。
func (s *TraceSpooler) markProbeDue(now time.Time) {
	s.probeDueAt.Store(now.UnixNano())
	s.probeDue.Store(true)
}

// applyDueTick 原子地消掉「到期」标志并执行一次 gate.Tick。
//
// 【恰好一次】用原子交换消费标志：一个到期标志换来恰好一次 Tick，既不会漏
//（标志保留到写协程下一次 AppendLine），也不会重复探针。
//
// 【有意的延迟解锁】写协程空闲时闩锁保持、标志保留；恢复流量后由首个 AppendLine
// 完成探针与解锁。空闲时没有任何待写的行，解锁早一步晚一步都不影响正确性。
func (s *TraceSpooler) applyDueTick() {
	if !s.probeDue.Swap(false) {
		return
	}
	s.gate.Tick(time.Unix(0, s.probeDueAt.Load()))
}

// FlushAndRoll 无条件 flush 一次，并对**已达阈值**的块执行滚动。
//
// 【为什么用 PendingSeals 做周期 flush 的入口】它的 Flush 发生在空块提前返回
//（writer.go:377）与阈值提前返回（:380-384）**之前**；只有 cur == nil（:358）
// 与 poisoned（:365，故意不 flush）两条路径不 flush。因此定时调它即可满足
// 规格 §7.1 的「无条件执行」。
//
// 【为什么消费它返回的任务，而不是忽略】返回的 SealTask 是公开 API 里唯一能
// 观察到「当前块是哪个 (dt, part)」的通道，滚动需要这个标识。这**不是**
// 「网关自己封块」：网关只调 MarkSealed 关掉自己的句柄、让下一个 Append 换新 part；
// 压缩与产物生成仍是计划 4 那个进程外 sealer 的职责（方案 C）。
//
// 【空块为什么不滚动 —— 承重的守卫与「可观测后果」的区分（评审必修 3.2 订正）】
// PendingSeals 对空块提前返回，所以这里天然不会在空块上滚动。但**必须说准是哪条守卫**：
//   - 承重的是 `cur == nil`（writer.go:358）：没有 Append 就没有块，这是
//     TestSpoolerDoesNotRollEmptyBlock 在空块那一轮真正走到的那条提前返回；
//   - `rows == 0`（writer.go:377-379）是**防御性**守卫，冻结包自己标注它经公开 API
//     **不可达**（writer.go:369-376）⇒ **没有任何用例能覆盖它**，本文件也不声称覆盖。
// 有用例盯着的只是空块的**可观测后果**：该用例断言「不滚动」（Rolls == 0、且不出现新的
// part 文件），而不是断言那条不可达守卫被执行过。
// 后果说明（为什么值得盯）：MarkSealed 只关句柄，下一次 Append 才建新 part；若在空块上
// 滚动，会留下 0 字节 spool 文件，sealer 按文件名照样能发现它，可能封出 0 行产物，
// 使该 part 的所有点读从「响亮缺失」退化成「行号越界」。
//
// 【为什么必须先 flush、后滚动（R8/D7）】sealer 只按 mtime + minAge(3h) 判断
//「已停止追加」。滚动间隔 ≤1h 且严格小于 3h ⇒ 任何一块在被 sealer 判定「静止」
// 之前就已被网关滚掉，网关手里不再有它的 fd，追加不会落进已被 unlink 的 inode。
// 次序错误在这里没有守卫可加（正常流程 sealer 确实已删 spool，「stat 文件是否存在」
// 的守卫会在正常路径上误报）—— 只能靠**次序保证**，因此本函数的实现次序
//（先 flush、再 MarkSealed）是承重的，且由
// TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain 固定。
func (s *TraceSpooler) FlushAndRoll() error {
	// ① 先复检根目录（每个 tick 一次；stat + 身份比对在写协程/循环协程上，开销可忽略）。
	//    失败 ⇒ 闩锁，并且**本轮不滚动**：滚动本身只关句柄，但紧接着的 Append
	//    会建块 ⇒ 触发冻结包的建目录路径，在容器可写层重建 root。
	//    闩锁已经挡住了 Append，这里再挡一次滚动，让「根目录不可用时不做任何
	//    结构性写入」这条边界更清楚。
	rootOK := s.checkRoot()

	// ② flush 无条件执行（规格 §7.1）：即使根目录不可用也要把已接收的行交给
	//    fd，写入现有句柄不会新建任何目录。
	tasks, err := s.w.PendingSeals()
	if err != nil {
		// flush 失败必须上抛：sealer 直接读 spool，缓冲没落盘它会把
		// 不完整的块当完整的封掉（漏行）。
		return fmt.Errorf("周期 flush 失败: %w", err)
	}
	s.mu.Lock()
	s.flushes++
	s.mu.Unlock()

	if !rootOK {
		return nil
	}
	for _, task := range tasks {
		// 无返回值：签名是 func (w *Writer) MarkSealed(dt string, part int)。
		// close/Flush 的错误只能丢弃（writer.go:395-419 的契约注释已登记这一点，
		// 与「次序错误丢正文」是两件事）。
		s.w.MarkSealed(task.DT, task.Part)
		s.mu.Lock()
		s.rolls++
		s.mu.Unlock()
	}
	return nil
}

// Run 是周期循环：每个 FlushInterval 做一次「根目录复检 + 无条件 flush + 滚动判定」。
//
// 这三个动作合并在同一个 tick 里是刻意的：滚动必须发生在 flush 之后
//（先落盘、再让 sealer 可能看见），而 flush 本身由 PendingSeals 完成。
//
// 【Run 不得调用 gate.Tick（A5-4）】Tick 持锁做探针 fsync，会阻塞写协程的
// Allow() ⇒ 这里只置「探针到期」标志，由写协程消费（见 markProbeDue/applyDueTick）。
// 集合里的静态检查行钉住这一形态，行为证据是
// TestSpoolerRunDefersProbeToWriteGoroutine。
//
// 【滚动的精确上界】判定在 tick 上做，而 tick 粒度是 FlushInterval ⇒
// **实际滚动间隔 ≤ RollInterval + FlushInterval = 1h + 1m**。
// R8 要求的是「滚动间隔 ≤1h 且**严格小于** minAge(3h)」；1h1m 仍远小于 3h，
// 因此这个上界是安全的 —— 但必须写明，因为它决定了 D7 的残余窗口：
// 网关持有某个 part 的 fd 的最长时间是 1h1m，而 sealer 要等 mtime + 3h 才会碰它。
//
// 【Run 不拥有 spool 的生命周期：ctx.Done 只 return，不 Close（评审必修 1）】
// 关停时 `main` 的 `cancel()` 会同时唤醒本循环与写入侧的 Run，而写入侧还要用
// WithoutCancel 补写最后一批（≤flushN 条 / ≤TraceSpoolerOptions.FlushInterval）。
// 本函数若在这里 Close，**健康盘**上的最后一批就会撞上 AppendLine 的 closed 检查 ⇒
// 全部 body_missing=true + 一条「正文落盘已闩锁」ERROR（假磁盘降级告警），
// 且每次优雅重启都可能发生。次序改由调用方编排：
// `cancel()` → 等 `(*TraceWriter).Done()`（最后一批已写完）→ `TraceSpooler.Close()`。
// 因此**生产必须显式调用 Close**；Close 仍是幂等的（CAS）。
func (s *TraceSpooler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.markProbeDue(s.now())
			if err := s.FlushAndRoll(); err != nil {
				log.Printf("[trace-spool] 周期 flush/滚动失败: %v", err)
			}
		}
	}
}

func (s *TraceSpooler) Close() error {
	// CAS ⇒ 幂等：Run 在 ctx.Done 时会 Close，调用方通常也 Close 一次；
	// 第二次直接返回 nil（既不重复关句柄，也不会把首次的错误覆盖成 nil）。
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	return s.w.Close()
}

func (s *TraceSpooler) Stats() SpoolerStats {
	s.mu.Lock()
	flushes, rolls := s.flushes, s.rolls
	s.mu.Unlock()
	return SpoolerStats{Flushes: flushes, Rolls: rolls, Gate: s.gate.Stats()}
}
