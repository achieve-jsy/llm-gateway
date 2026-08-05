package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultZstdVersion 是标定过的 zstd 版本（生产部署版本），封块前会与实际
// 二进制核对（见 Sealer.verifyZstdVersion）。
//
// 【为什么版本要锁死】规格 §5.3.1.1 的压缩比标定是【版本特定】的：方案 C 采用
// C 版 zstd CLI 的结论建立在 v1.4.8 的实测上（同数据 CLI 产物 202.6 MB，Go 库
// 384.0 MB，比值 1.90x），验收线（≤1.30x CLI）也是按这个版本定的。zstd 的
// 匹配查找器/熵编码在不同版本间会变，换版本后压缩比可能漂移出验收区间，
// 因此【换版本必须重新标定】，不能让它静默通过。
//
// ⚠️ 本机开发环境实测是 v1.5.7，与生产 v1.4.8 不同：本地跑真封块用例时必须把
// 本机版本显式传给 SealerOptions.ExpectedZstdVersion（测试辅助 requireZstd
// 已返回本机版本），这也是「期望版本必须可配置」的原因之一。
const defaultZstdVersion = "1.4.8"

// defaultSealMinAge 是「认定一个 spool 已停止追加」的默认最小静默时长（3 小时）。
//
// 【取值推导（评审裁决 1）——这个数不是随手取的】
//   - 实测摄取速率 ≈ 37 GiB/天 ≈ 1.54 GiB/小时；
//   - 块大小目标 defaultBlockBytes = 1536 MiB ⇒ 满流量下约 1 小时填满一个块；
//   - 本进程唯一的保护就是这个时间差（见 (*Sealer).ListPendingSeals 的说明），
//     它必须【严格大于】网关的 part 滚动间隔 ⇒ 取 3 × 1h = 3 小时，留 3 倍安全余量。
//
// 【两个不同的命题，不要混为一谈（评审第 ⑥ 条）】
//  1. 正确性约束：minAge 必须严格大于网关滚动间隔。**现有
//     defaultSealInterval=15m 满足这一条**（3h 对它有 12 倍余量），因此当前
//     默认值在正确性上是安全的。
//  2. 压缩比档位目标（**15m 不满足这一条**）：计划 2 应把网关滚动间隔调到 ~1h，
//     好让 1536 MiB 的【大小】阈值先触发，从而落在已标定 10.26x 的档位上；
//     当前 15m 会让【时间】阈值先触发，块被截到 ~400 MB，压缩比落进**未标定区间**。
//     也就是说，10.26x 这个验收线只有在第 2 条落地后才真正被覆盖。
//
// 成本核算：未封块 spool 约 3h × 1.54 GiB/h ≈ 4.6 GiB 未压缩驻留，
// 相对 /datalog 的 240 GiB 可用空间可忽略。
//
// ⚠️ 对端约束：网关的滚动间隔必须 ≤ 1h（计划 2 落地），否则本值不再成立。
const defaultSealMinAge = 3 * time.Hour

// SealerOptions 是封块器配置。
type SealerOptions struct {
	Root            string
	ZstdBinary      string // zstd 可执行文件；空则回退到 "zstd"（部署项，规格 §11.1）
	ZstdLevel       int    // 0 取 defaultZstdLevel
	ZstdWindowBytes int    // 0 取 defaultZstdWindowBytes；换算为 CLI 的 --long=<log2(窗口)>
	MinAge          time.Duration

	// ExpectedZstdVersion 是期望的 zstd 版本（"X.Y.Z"）；空则取 defaultZstdVersion。
	// 首次封块时用 `<ZstdBinary> --version` 探测一次并比对，不符则拒绝封块
	// （标定是版本特定的，规格 §11.1；推理见 defaultZstdVersion 注释）。
	ExpectedZstdVersion string
}

// Sealer 把 spool 明文封块为带内嵌索引的产物文件。
//
// 【方案 C】编码由【C 版 zstd CLI 子进程】完成，不是 Go 库（规格 §5.3.1.1）：
// 同数据、同级别、同窗口下 Go 库产物是 CLI 的 1.90 倍（384.0 MB vs 202.6 MB），
// 且 Go 库峰值 RSS ≈ 窗口 × 2.08，CLI ≈ × 1.08。由此编码器内存完全不落在
// 网关的 2 GiB cgroup 内 —— sealer 是独立进程，跑在宿主机上。
//
// Go 侧只负责：扫描 spool 采集行偏移 → 把 spool 喂给 CLI → 追加 trailer/footer
// → fsync → rename → 删 spool。
// **trailer 与 footer 永远由 Go 侧追加，CLI 只产 zstd 帧。**
type Sealer struct {
	opts  SealerOptions
	paths Paths

	// 本结构体【没有运行期可变状态】（评审裁决 1）：版本探测刻意不做记忆化。
	// 代价与收益：记忆化省下的只是一天几十次（回溯追赶时最多上百次）
	// `zstd --version`，合计秒级；代价却是引入一个未同步的可变字段，让这个看起来
	// 可以共享的值类型在【任意】并发调用下产生数据竞争。不划算。
	//
	// 【并发边界（评审第 ⑦ 条，原注释说过头了）】不同 Ref 之间可以并发调用 Seal；
	// **同一个 (dt, inst, part) 必须串行**——它们共用同一个 partPath+".tmp"
	// （见 Seal 里 tmpPath 的用法），并发跑会互相交错/覆盖同一个临时文件。
	// 这里的「无共享可变状态」只保证不存在数据竞争，不等于同一块可以并发封。
	//
	// 唯一的例外是下面的测试注入钩子，它只在单测里、并发调用之前设置（生产恒为 nil）。
	failBeforeRename func() error
}

// NewSealer 构造封块器。
func NewSealer(opts SealerOptions) (*Sealer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdBinary == "" {
		opts.ZstdBinary = "zstd"
	}
	if opts.ZstdLevel <= 0 {
		opts.ZstdLevel = defaultZstdLevel
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if opts.MinAge <= 0 {
		opts.MinAge = defaultSealMinAge
	}
	if opts.ExpectedZstdVersion == "" {
		opts.ExpectedZstdVersion = defaultZstdVersion
	}
	if _, err := zstdLongArg(opts.ZstdWindowBytes); err != nil {
		return nil, err
	}
	return &Sealer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Seal 执行一次封块。任何一步失败都保留 spool 并返回错误，绝不留下半个产物。
//
// 失败语义（规格 §7.2、§9.2）：
//   - DT/Inst 非法         → 参数错误（与 Append 同一套校验，见下方入口校验）
//   - 产物已存在           → ErrPartExists（幂等，拒绝覆盖）
//   - spool 行数与记录不符  → ErrRowCountMismatch（保留 spool）
//   - 子进程非零退出/缺失   → ErrSealFailed（保留 spool、删 .tmp）
func (s *Sealer) Seal(ctx context.Context, task SealTask) error {
	// 【入口校验：路径分量（终审 A 的实测缺陷 ②，F1 修）】DT/Inst 与 Append 的 dt
	// 一样，都是【调用方输入】，且都直接拼进目录名，因此必须用【同一套】校验：
	// validateDT（严格日期）+ validateInst（blue/green 白名单），不另造一套、不复用
	// 发现路径的判断（那是另一条路径的宽松跳过策略）。
	//
	// 实测（修复前）：task.DT="2026-09-24/../.." ⇒ Seal 返回 **nil**，产物落到
	// <root 之外>/inst=blue/part-000000.jsonl.zst —— 越出 dt=/inst= 目录布局，
	// 且该路径与 spool 目录塌缩为同一层；task.Inst="../../green" 同理越出 inst= 层。
	// 原因是 SpoolDir/PartDir 用 filepath.Join 拼接，值里含 "/" 时其中的裸 ".."
	// 会被 Clean 折叠（只有【裸 ".." 元素】才折叠，"dt=.." 这种字面名不折叠）。
	//
	// plan:4031 承认 SealTask 属调用方输入——这正是必须校验的理由，而不是免于
	// 校验的理由：Append 的 dt 同样是调用方输入，且它就在入口校验。
	//
	// 位置：先于下面 ⓪ 的路径一致性判定，也先于任何 I/O。两者都是纯内存判定，
	// 顺序上先判「值本身是否合法」再判「SpoolPath 是否与 Ref 一致」——畸形调用
	// 不该先去看世界状态。
	//
	// 错误沿用本包风格：与 Append 一样直接返回参数错误（无哨兵），见 validateDT /
	// validateInst 的文档说明。
	if err := validateDT(task.DT); err != nil {
		return err
	}
	if err := validateInst(task.Inst); err != nil {
		return err
	}

	// ⓪ 路径一致性（评审第 ⑤ 条）：task.SpoolPath 必须就是 (DT,Inst,Part) 对应的
	// 那个 spool 文件。Seal 是【导出 API】，task 由调用方构造；若两者不匹配，产物
	// 会以 Ref 寻址、却装进另一个文件的内容 —— 点读会【静默返回错正文】，这是本项目
	// 最不可接受的一类失效。成本只是一次字符串比较，因此前置在所有 I/O 之前
	//（纯内存判定，连 stat 都不做；畸形调用不该先去看世界状态）。
	//
	// 当前唯一调用方 ListPendingSeals 由 SpoolFile 构造 path，天然自洽；这条守卫是
	// 给未来调用方与重构用的。
	//
	// 比较前对 task.SpoolPath 做 filepath.Clean（评审复审 3）：调用方可能用 "./"、
	// "x/../" 这类**等价但拼写不同**的路径构造 task，Clean 后仍指向同一文件，不该被误拒。
	// 刻意【不】做 EvalSymlinks：那要读文件系统，会破坏「纯内存判定」这一性质。
	// 已知残余限制（只登记，不修）：经符号链接或不同绝对拼写指向同一文件的 Root
	// 仍可能被误拒——放宽它需要 I/O，代价大于收益。
	wantSpool := s.paths.SpoolFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})
	if filepath.Clean(task.SpoolPath) != wantSpool {
		return fmt.Errorf("%w: task.SpoolPath=%q 与 Ref{%s, %s, %d} 对应的 %q 不一致（拒绝封块：产物会以 Ref 寻址却装进另一文件的内容）",
			ErrSealFailed, task.SpoolPath, task.DT, task.Inst, task.Part, wantSpool)
	}

	partPath := s.paths.PartFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})

	// I4: 同一个 (dt, inst, part) 只允许封块一次。
	// 若产物已存在，宁可留下 spool 也不能覆盖——那是唯一副本。
	if _, err := os.Stat(partPath); err == nil {
		return fmt.Errorf("%w: %s", ErrPartExists, partPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	// ① 先顺序扫描 spool，采集每行首字节的未压缩偏移。
	// 方案 C 下 CLI 是独立进程，Go 侧看不到压缩过程中的行边界，
	// 因此在喂数据之前先扫一遍（一次顺序读，成本可忽略）。
	offsets, _, err := scanSpoolOffsets(task.SpoolPath)
	if err != nil {
		return err
	}
	// ② 行数交叉校验放在启动子进程之前：不匹配就不必浪费一次压缩。
	// task.Rows == 0 表示调用方（ListPendingSeals 扫目录）不知道行数。
	if task.Rows > 0 && len(offsets) != task.Rows {
		return fmt.Errorf("%w: 扫描=%d 调用方=%d（拒绝封块，保留 spool）",
			ErrRowCountMismatch, len(offsets), task.Rows)
	}

	// 【版本守卫的位置（评审裁决 3a）】放在【所有不依赖编码器的判定之后、
	// 第一次触碰编码器之前】：产物已存在（ErrPartExists）与行数不符
	// （ErrRowCountMismatch）都是纯粹的判定 + 读取，必须在 zstd 缺失/版本不符时
	// 照样能报出来——否则一个配错的编码器会掩盖「产物已存在」「行数不符」这两类
	// 真实的数据问题，运维看到的根因就是错的（简报的这两个用例明确要求了这个顺序）。
	// 放在这里同时也保证了：确认编码器可信之前，不创建任何文件（含 .tmp）。
	if err := s.verifyZstdVersion(); err != nil {
		return err
	}

	// 【落地修正 1】产物目录必须在创建 .tmp 之前建好。
	// 简报的实现把 MkdirAll 放在 rename 之前，而 .tmp 就落在产物目录里，
	// 于是「目录还不存在」时 os.OpenFile(.tmp) 直接 ENOENT —— 所有真封块用例
	// 都会以「建临时产物失败」告终（实测输出见任务报告）。
	// .tmp 与产物同目录是刻意的（rename 必须同文件系统），因此目录只能提前建。
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		return fmt.Errorf("tracearchive: 建产物目录失败: %w", err)
	}

	tmpPath := partPath + ".tmp"
	frameSHA, err := s.runZstd(ctx, task.SpoolPath, tmpPath)
	if err != nil {
		// 子进程非零退出 / 无法启动 / 压缩 I/O 失败：
		// 清理 .tmp、保留 spool、不产生产物（规格 §9.2）。
		os.Remove(tmpPath)
		return err
	}

	// ③ 帧已就绪，此刻追加 trailer 与 footer（CLI 只写了帧）
	if err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if s.failBeforeRename != nil {
		if err := s.failBeforeRename(); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	// I2: 产物只能通过 rename 出现，永不出现半个文件
	if err := os.Rename(tmpPath, partPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: rename 失败: %w", err)
	}
	if err := fsyncDir(filepath.Dir(partPath)); err != nil {
		return fmt.Errorf("tracearchive: fsync 目录失败: %w", err)
	}

	// I5: 到此才允许删 spool
	if err := os.Remove(task.SpoolPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 删除 spool 失败: %w", err)
	}
	return nil
}

// scanSpoolOffsets 顺序扫描 spool，返回每行首字节的未压缩偏移与总字节数。
//
// 索引记录的是「行首在未压缩流中的位置」。方案 C 下这份偏移必须在喂给 CLI
// 之前采集（见 Seal 注释），且必须与实际喂进去的字节流完全一致——
// 因此这里逐字节累加，不做任何行内改写。
//
// 【残行守卫：为什么必须有它】写入器把 bufio 直接写到 fd 上，盘满（ENOSPC，
// 恰是本子系统要解决的问题）时可能只写出去半行：行尾没有换行符。这种残行
// 【永不可被封块】—— 产物格式假定行稠密且以 '\n' 分隔（index.go 的
// encodeTrailer 是「每行一个差分 varint」），残行会被当成一整行烤进产物，
// 其后的行边界全部错位，点读于是静默返回错误的正文（本项目最不能接受的一类
// 缺陷）。而封块器有两条发现路径：
//
//   - SealTask.Rows > 0（写入器自己报的块）：下面的行数交叉校验会因残行
//     永久 ErrRowCountMismatch —— fail-safe，可接受；
//   - SealTask.Rows == 0（ListPendingSeals 扫目录发现，行数未知）：
//     **该交叉校验被跳过**，残行没有任何别的拦截点。
//
// 因此这里把「spool 必须以换行符结尾」作为硬性前置条件，让第二条路径也变成
// 响亮的拒绝（ErrSealFailed、保留 spool、不产生产物）。注意判据必须是
// 「EOF 时仍带回了非空字节」：0 字节的空 spool 是合法状态（row_count=0），
// 不能被误判为残行。
func scanSpoolOffsets(path string) ([]int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	var offsets []int64
	var off int64
	for {
		line, readErr := br.ReadBytes('\n')
		// ReadBytes 在 EOF 前找不到 '\n' 时，会同时返回已读到的字节与 io.EOF。
		// 带回了字节就说明最后一行没有换行符 —— 拒绝封块（见上方残行守卫）。
		if readErr == io.EOF && len(line) > 0 {
			return nil, 0, fmt.Errorf(
				"%w: spool 末尾缺少换行符（最后一行不完整，可能落盘时被截断，尾部 %d 字节）: %s",
				ErrSealFailed, len(line), path)
		}
		if len(line) > 0 {
			offsets = append(offsets, off)
			off += int64(len(line))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
		}
	}
	return offsets, off, nil
}

// zstdLongArg 把窗口字节数换算为 CLI 的 --long=<log2(窗口)>。
// 512 MiB = 2^29 ⇒ "29"。zstd 的 --long 语义要求窗口是 2 的幂，故此处严格校验。
func zstdLongArg(windowBytes int) (string, error) {
	if windowBytes <= 0 || windowBytes&(windowBytes-1) != 0 {
		return "", fmt.Errorf("tracearchive: zstd 窗口必须是 2 的幂（实际 %d 字节）", windowBytes)
	}
	return strconv.Itoa(bits.Len(uint(windowBytes)) - 1), nil
}

// verifyZstdVersion 用 `<binary> --version` 探测编码器版本并与期望值比对。
//
// 【每次都探测，不做记忆化（评审裁决 1）】省下的是每天几十次 `zstd --version`
// （约 5 ms/次，合计秒级），代价是给 Sealer 引入一个未同步的可变字段——
// 一个看起来可以共享的值类型就不该有数据竞争。宁可每次多花几毫秒。
//
// 【为什么在这里而不是 NewSealer（评审裁决 3a）】
//  1. 发现路径 (*Sealer).ListPendingSeals 是纯文件系统操作，不该要求一个可用的
//     zstd 二进制；构造期探测会让「只做发现」也依赖编码器版本。
//  2. 版本校验失效的是【封块产物】，放在 Seal 入口正好覆盖它，且「二进制缺失」
//     仍与 runZstd 同处一类错误，不重复报错。
//
// 失败一律用既有哨兵 ErrSealFailed（不新增哨兵）：与「子进程非零退出/缺失」
// 同属「这台机器上的编码器不可信」，调用方处置相同——保留 spool、告警、下轮重试。
//
// 【错误文案是诊断接口，不是装饰】四条错误路径各自的措辞都可被断言区分
// （"找不到可执行文件" / "探测 zstd 版本失败" / "无法解析" / "期望 X，实际 Y"）：
// 运维排查「为什么封块全停了」时，「输出不可解析」与「版本不符」指向完全不同的
// 处置（换构建 vs 重新标定），所以文案有测试价值，不能随合并。
func (s *Sealer) verifyZstdVersion() error {
	out, err := exec.Command(s.opts.ZstdBinary, "--version").Output()
	if err != nil {
		// 【注释按实测收敛（终审 A 第 5 条）】这一分支【只有 ZstdBinary 含 "/" 时可达】：
		//   - 裸名（默认 "zstd"）：exec.Command 会先 LookPath，失败生成 *exec.Error，
		//     实测 errors.Is(err, os.ErrNotExist) == false，但 errors.Is(err, exec.ErrNotFound) == true
		//     ⇒ 身份由 *exec.Error 自带，根本走不到这里；
		//   - 含 "/" 的路径：不做 LookPath，由 Start 报 *fs.PathError(ENOENT)，
		//     实测 errors.Is(err, os.ErrNotExist) == true ⇒ 只有这种情况进本分支。
		// 因此这里不是「保留 ErrNotFound 身份的唯一防线」，只是把含路径写法的缺失也
		// 归到同一身份（部署事故，规格 §11.1）。分类不变，仍用 ErrSealFailed。
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: 探测 zstd 版本失败，找不到可执行文件 %q（部署事故，规格 §11.1）: %w: %w",
				ErrSealFailed, s.opts.ZstdBinary, exec.ErrNotFound, err)
		}
		return fmt.Errorf("%w: 探测 zstd 版本失败（%s --version）: %w",
			ErrSealFailed, s.opts.ZstdBinary, err)
	}
	got, ok := parseZstdVersion(string(out))
	if !ok {
		return fmt.Errorf("%w: 无法解析 zstd 版本输出 %q，无法确认编码器版本（规格 §11.1 要求锁定版本，拒绝在未标定的编码器上封块）",
			ErrSealFailed, strings.TrimSpace(string(out)))
	}
	if got != s.opts.ExpectedZstdVersion {
		return fmt.Errorf("%w: zstd 版本不符：期望 %s，实际 %s。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开",
			ErrSealFailed, s.opts.ExpectedZstdVersion, got)
	}
	return nil
}

// parseZstdVersion 从 `zstd --version` 的输出里取出三段式版本号。
// 输出形如：*** Zstandard CLI (64-bit) v1.5.7, by Yann Collet ***
//
// 严格三段数字：形如 "1.5.7-beta" 一律视为不可解析（宁可拒绝，也不要把一个
// 未标定的构建当成已标定版本放过去）。
func parseZstdVersion(out string) (string, bool) {
	i := strings.Index(out, " v")
	if i < 0 {
		return "", false
	}
	rest := out[i+2:]
	j := strings.IndexByte(rest, ',')
	if j < 0 {
		return "", false
	}
	v := strings.TrimSpace(rest[:j])
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, p := range parts {
		if p == "" {
			return "", false
		}
		for k := 0; k < len(p); k++ {
			if p[k] < '0' || p[k] > '9' {
				return "", false
			}
		}
	}
	return v, true
}

// runZstd 用 C 版 zstd CLI 把 spoolPath 压缩为 tmpPath，
// 返回 zstd 帧字节的 sha256 前 8 字节。
//
// 命令形如：zstd -19 --long=29 -T1 -c
//
//	-19        级别 19（规格 §5.3.1.1：Go 库 19 与 22 产物相同，22 只多耗 CPU）
//	--long=29  窗口 512 MiB —— 与 Go 库的唯一实质差异（长距离匹配器）
//	-T1        单线程：多并发会把流切成独立块，显著削弱压缩比
//	-c         写 stdout（不让 zstd 自己命名文件）
//
// 退出码非 0 一律视为封块失败（ErrSealFailed），绝不接受「看起来有产物」的半成品。
func (s *Sealer) runZstd(ctx context.Context, spoolPath, tmpPath string) ([8]byte, error) {
	var empty [8]byte

	long, err := zstdLongArg(s.opts.ZstdWindowBytes)
	if err != nil {
		return empty, err
	}

	// 【纵深防御，不是唯一防线（评审第 ② 条：原理由经实测不成立）】
	// 原注释称「删掉它会静默产出空帧 + 正确索引」。实测（go1.26.1/darwin）：
	// 若这里改成把 typed-nil 的 *os.File 交给子进程，得到的是【坏 fd】而不是 EOF——
	// cat 退 1、zstd 退 37 ⇒ cmd.Run() 非 nil ⇒ 下面「子进程失败」那条守卫照样以
	// ErrSealFailed 拦下，**不会**静默产出空帧。所以本分支不是唯一防线。
	//
	// 它测试不可达（spool 在扫描与本次打开之间被删除/替换的 TOCTOU 窗口），保留它
	// 的理由是：将来若有人重构掉上层守卫，这里仍会响亮失败而不是静默通过。
	src, err := os.Open(spoolPath)
	if err != nil {
		return empty, fmt.Errorf("%w: 打开 spool 失败: %w", ErrSealFailed, err)
	}
	defer src.Close()

	// O_RDWR：子进程作为 stdout 写入，随后 Go 侧还要 seek 回起点算帧的 sha256
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return empty, fmt.Errorf("%w: 建临时产物失败: %w", ErrSealFailed, err)
	}
	defer dst.Close()

	args := []string{"-" + strconv.Itoa(s.opts.ZstdLevel), "--long=" + long, "-T1", "-c"}
	cmd := exec.CommandContext(ctx, s.opts.ZstdBinary, args...)
	cmd.Stdin = src  // 直接把文件交给子进程，不经内存中转
	cmd.Stdout = dst // 只写 zstd 帧
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, fmt.Errorf("%w: 封块上下文取消: %w", ErrSealFailed, ctxErr)
		}
		// 「二进制缺失 → 部署事故」的归一化只保留在 verifyZstdVersion 一处：
		// 版本探测先于压缩执行，二进制不存在时在探测阶段就会以
		// ErrSealFailed + exec.ErrNotFound 失败（见 TestSealFailsWhenZstdMissing）。
		// 这里原样上抛子进程错误即可；若二进制在探测与压缩之间被删（TOCTOU），
		// 报出来的仍是响亮失败，只是不再额外挂 ErrNotFound 身份——那个窗口不值得
		// 第二份重复的归一化代码（重复分支无法被任何用例区分）。
		return empty, fmt.Errorf("%w: zstd 子进程失败（args=%v）: %w（stderr: %s）",
			ErrSealFailed, args, err, strings.TrimSpace(stderr.String()))
	}

	// 此刻 .tmp 的内容即纯 zstd 帧（trailer/footer 尚未追加），据此计算 frame_sha256
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return empty, fmt.Errorf("%w: 回绕临时产物失败: %w", ErrSealFailed, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, dst); err != nil {
		return empty, fmt.Errorf("%w: 计算帧摘要失败: %w", ErrSealFailed, err)
	}
	var sum [8]byte
	copy(sum[:], hasher.Sum(nil)[:8])
	return sum, nil
}

// appendTrailerAndFooter 在临时产物末尾追加索引与定长 footer，并 fsync。
//
// 【方案 C 的关键点】CLI 只写了 zstd 帧，索引与 footer 全部在这里由 Go 侧追加，
// 保证索引与内容出自同一次封块操作（I3）。
func (s *Sealer) appendTrailerAndFooter(tmpPath string, offsets []int64, frameSHA [8]byte) error {
	trailer := encodeTrailer(offsets)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("tracearchive: 打开临时产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	trailerOffset := info.Size()

	if _, err := f.Write(trailer); err != nil {
		return fmt.Errorf("tracearchive: 写 trailer 失败: %w", err)
	}
	footerBytes := encodeFooter(footer{
		TrailerOffset: trailerOffset,
		TrailerLen:    int32(len(trailer)),
		RowCount:      int32(len(offsets)),
		FrameSHA:      frameSHA,
	})
	if _, err := f.Write(footerBytes); err != nil {
		return fmt.Errorf("tracearchive: 写 footer 失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("tracearchive: fsync 临时产物失败: %w", err)
	}
	return nil
}

// fsyncDir 目录 fsync：保证 rename 本身落盘，否则崩溃后可能看不到产物。
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ListPendingSeals 扫描 <root>/spool/dt=*/inst=*/part-*.jsonl，
// 返回 mtime 早于 now-minAge 的 spool（即「已停止追加」的块）。
//
// 为什么用 mtime + minAge 而不是信号（规格 §7.2）：sealer 是独立进程，
// 网关无法给它发信号，而目录扫描不需要任何进程间通信，并且【幂等且可重启】——
// 任何一轮崩溃后，下一轮扫描会重新发现同一个 spool 并重试
// （产物已存在则由 Seal 以 ErrPartExists 拒绝覆盖）。
//
// 纯函数：只读文件系统元信息，不修改任何状态，因此可直接单测。
//
// 【minAge 必须 > 网关的 part 滚动间隔 —— 它不是性能参数，是本进程唯一的保护】
// 网关自己周期滚动 part（到年龄即 MarkSealed 换新 part、关闭旧 fd），而 sealer
// 是独立进程、收不到任何信号：本进程无法区分「已停止追加」与「暂时空闲」，
// 两者的唯一区别就是这个时间差。若 minAge 不大于滚动间隔，就会去封一个网关
// 仍在追加的块 —— 那些行写进的是已被 unlink 的 inode：字节落盘却永久不可达，
// 且它们的 Ref 指向一个行数更少的产物（静默丢数据，见计划缺陷 D7）。
//
// 调用方还必须保证 minAge 远大于网关的 flush 周期（规格 §7.1）：刚建立的块在
// 磁盘上可能还是空文件、mtime 停留在创建时刻，只有 flush 才会推进它。
// 默认值见 defaultSealMinAge（3 小时）——取值推导与对端约束都写在那个常量上。
//
// 【目录名校验（评审裁决 3b；理由经评审第 ③ 条更正）】dt/inst 直接来自目录名，
// 非法值一律跳过该目录（不报错中断整轮扫描）。理由是【健壮性/一致性】：畸形目录名
// 会让下游拿到一个语义不通的 SealTask（DT=".."、DT="2026-9-24"、Inst="bluex"），
// 而写入侧早已严格校验，两侧应当对称。
// **不是防路径穿越** —— 原注释的该前提经实测不成立，详见循环体内 dt 处的注释。
//
// 【为什么只有方法、没有包级函数（评审裁决 2）】发现路径需要 Root 与 MinAge
// 两个配置项，包级 ListPendingSeals(root, minAge) 允许它们来自不同来源，
// 从而产生「用 A 实例的 root 配 B 实例的 minAge」这类组合错误；方法形式
// 只可能拿到同一份 SealerOptions，因此本包只暴露 (*Sealer).ListPendingSeals()。
func (s *Sealer) ListPendingSeals() ([]SealTask, error) {
	spoolRoot := filepath.Join(s.opts.Root, "spool")
	cutoff := time.Now().Add(-s.opts.MinAge)

	dts, err := os.ReadDir(spoolRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 还没写过任何正文
		}
		return nil, fmt.Errorf("tracearchive: 读 spool 根目录失败: %w", err)
	}

	var out []SealTask
	for _, dtEnt := range dts {
		if !dtEnt.IsDir() || !strings.HasPrefix(dtEnt.Name(), "dt=") {
			continue
		}
		dt := strings.TrimPrefix(dtEnt.Name(), "dt=")
		// 【裁决 3b，理由经评审第 ③ 条更正】原注释称目录名 "dt=.." 会被 filepath.Join
		// 的清理语义带出 spool 子树 —— **该前提经实测不成立**：
		//   Join("/r", "dt=..", "inst=blue") == "/r/dt=../inst=blue"
		// 因为 "dt=.." 是一个【字面目录名】，不是父目录记号，Clean 不会折叠它；
		// 而且 SpoolDir/PartDir 会拼回 "dt="/"inst=" 前缀，目录名本身也不可能含 "/"。
		// 所以这里的校验价值是【健壮性/对称性】：不让畸形目录名产出语义不通的
		// SealTask（DT=".."、"2026-9-24"、Inst="bluex"），与写入侧严格校验对称，
		// **不是防路径穿越**。
		//
		// 精确表述（避免矫枉过正成「穿越不可能」）：穿越在【一般】情形下确实可能，
		// 但那要求值里含 "/"，让 Join 看到裸 ".." 元素才折叠，例如（均实测，root=/r）：
		//   dt="2026-09-24/../.."    ⇒ SpoolDir=/r/inst=blue、PartDir=/inst=blue（越出 root）
		//   inst="../../green"       ⇒ /r/dt=X/green（越出 inst= 层）
		//   inst="a/../../green"     ⇒ /r/green
		// 写入侧取值来自调用方参数（可含 "/"）⇒ 那里穿越成立、必须校验；本条列举
		// 路径的 dt/inst 只能来自【目录名】（不含 "/"）⇒ 此处穿越不成立。
		//
		// 非法即【跳过】而不是报错：一个坏目录不该让当天所有封块停摆
		// （与下面「非布局条目静默忽略」同一策略），所以这里是 continue。
		if err := validateDT(dt); err != nil {
			continue
		}
		dtDir := filepath.Join(spoolRoot, dtEnt.Name())

		insts, err := os.ReadDir(dtDir)
		if err != nil {
			return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dtDir, err)
		}
		for _, instEnt := range insts {
			if !instEnt.IsDir() || !strings.HasPrefix(instEnt.Name(), "inst=") {
				continue
			}
			inst := strings.TrimPrefix(instEnt.Name(), "inst=")
			// 【裁决 3b】同 dt：白名单外（含 "inst=.."、"inst=bluex"）一律跳过。
			// 与写入侧 Writer 的 Inst 白名单对称，避免「写侧严格、列举侧宽松」。
			if inst != instBlue && inst != instGreen {
				continue
			}
			dir := filepath.Join(dtDir, instEnt.Name())

			ents, err := os.ReadDir(dir)
			if err != nil {
				return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dir, err)
			}
			for _, e := range ents {
				// 只认 spool 名 part-NNNNNN.jsonl；
				// .tmp、产物名（.jsonl.zst）、非 part- 前缀一律忽略。
				if !strings.HasSuffix(e.Name(), ".jsonl") {
					continue
				}
				part, ok := parsePartSeq(e.Name())
				if !ok {
					continue
				}
				info, err := e.Info()
				if err != nil {
					return nil, fmt.Errorf("tracearchive: stat %s 失败: %w", e.Name(), err)
				}
				if info.ModTime().After(cutoff) {
					continue // 仍在追加（或刚写过），不碰
				}
				out = append(out, SealTask{
					DT:                dt,
					Inst:              inst,
					Part:              part,
					SpoolPath:         filepath.Join(dir, e.Name()),
					UncompressedBytes: info.Size(),
					// Rows 留 0：扫目录时不知道行数，交给 Seal 扫描后自证
				})
			}
		}
	}
	// 稳定顺序（dt/inst/part），便于测试断言与日志排查
	sort.Slice(out, func(i, j int) bool {
		if out[i].DT != out[j].DT {
			return out[i].DT < out[j].DT
		}
		if out[i].Inst != out[j].Inst {
			return out[i].Inst < out[j].Inst
		}
		return out[i].Part < out[j].Part
	})
	return out, nil
}
