// 本文件是 B 层测试（tests/traceverify，package traceverify_test）：`ParseVmHWM` 是纯函数，
// `VmHWMProbe` 的读取面可注入（`Read` 字段）⇒ 采样循环的**行为**在 darwin 上也能真跑
// （不需要 /proc）。`Root` 面用真实临时目录驱动一次真实文件读取，避免「只测替身」。
//
// 契约（计划 4 / 任务 8 步骤 3 的接口）：
//
//	ParseVmHWM 取 /proc/<pid>/status 里 VmHWM 行的 kB 值 × 1024（bytes）
//	          缺行 / 值非十进制 / 值为负 / 单位不是 kB / 溢出 ⇒ 报错（读不到必须 FAIL）
//	Peak      对每个 pid 采样 Samples 次、间隔 Interval，返回**峰值**；任一次读不到 ⇒ 报错
package traceverify_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/traceverify"
)

// statusText 拼一段形状与 `/proc/<pid>/status` 一致的文本（VmHWM 行含制表符与前导空格，
// 与真实文件逐字同形）。
func statusText(vmHWMField string) string {
	return "Name:\tgateway\nUmask:\t0022\nState:\tS (sleeping)\n" +
		"VmPeak:\t 1234567 kB\nVmSize:\t 1230000 kB\n" +
		"VmHWM:\t" + vmHWMField + "\n" +
		"VmRSS:\t  99000 kB\nThreads:\t12\n"
}

// statusWithVmHWMKB 用 kB 值拼一段 status 文本。
func statusWithVmHWMKB(kb int64) string { return statusText(fmt.Sprintf("%8d kB", kb)) }

// TestParseVmHWMReadsPeakResidentBytes VmHWM 行 ⇒ 峰值常驻内存（bytes）。
// 单位换算是承重的：A6/C5 的内存预算与 `peak_vmhwm_bytes` 都按 bytes 比较。
func TestParseVmHWMReadsPeakResidentBytes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int64
	}{
		{"真实形态", statusWithVmHWMKB(100000), 100000 * 1024},
		{"零值", statusText("       0 kB"), 0},
		{"单空格", statusText("42 kB"), 42 * 1024},
		{"多空格", statusText("      12   kB"), 12 * 1024},
		{"重复行取第一条", statusText("11 kB") + "VmHWM:\t      99 kB\n", 11 * 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := traceverify.ParseVmHWM(c.text)
			require.NoError(t, err, "解析 %q 不该报错", c.name)
			require.Equal(t, c.want, got, "%s：VmHWM 必须换算成 bytes（× 1024）", c.name)
		})
	}
}

// TestParseVmHWMRejectsMalformedStatus 读不到 / 读不懂必须报错 —— 绝不允许「返回 0
// 继续往下走」：0 会被下游当成一个合法的峰值读数，把「没有证据」洗成「内存很小」。
func TestParseVmHWMRejectsMalformedStatus(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"空文本", ""},
		{"缺VmHWM行", "Name:\tgateway\nVmRSS:\t  100 kB\n"},
		{"只有键没有值", statusText("")},
		{"值非数字", statusText("abc kB")},
		{"值带前缀符号", statusText("+100 kB")},
		{"值为负", statusText("-1 kB")},
		{"单位不是kB", statusText("100 MB")},
		{"缺单位", statusText("100")},
		{"值溢出int64", statusText("9007199254740992 kB")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := traceverify.ParseVmHWM(c.text)
			require.Error(t, err, "%s：必须报错（读不到/读不懂不许静默返回 0）", c.name)
		})
	}
}

// TestParseVmHWMReadsPeakResidentNotVirtual status 里 **VmPeak（峰值虚拟）与 VmHWM（峰值
// 常驻）同时在且取值不同** ⇒ 必须取 VmHWM：C5 的预算、2 GiB cgroup 与 `peak_vmhwm_bytes`
// 比的都是**常驻**内存；取成 VmPeak 会把虚拟地址空间（含未落页的映射）当成实际占用。
// 同时钉住「只有 VmPeak、没有 VmHWM ⇒ 报错」—— 缺字段时必须红，不许拿 VmPeak 顶替。
// killer：`fields[0] != "VmHWM:"` 被改成 `fields[0] != "VmPeak:"`（见 p4-task-9-vmhwm.py 的 m01）。
func TestParseVmHWMReadsPeakResidentNotVirtual(t *testing.T) {
	text := "Name:\tgateway\n" +
		"VmPeak:\t 9999999 kB\n" + // 峰值虚拟：比 VmHWM 大一个量级
		"VmSize:\t 8888888 kB\n" +
		"VmHWM:\t  111111 kB\n" + // 峰值常驻：唯一正确读数
		"VmRSS:\t  222222 kB\n"
	got, err := traceverify.ParseVmHWM(text)
	require.NoError(t, err)
	require.Equal(t, int64(111111*1024), got,
		"必须取 VmHWM（峰值常驻 111111 kB）；取 VmPeak 会得到 9999999 kB")

	_, err = traceverify.ParseVmHWM("Name:\tgateway\nVmPeak:\t 9999999 kB\nVmRSS:\t  100 kB\n")
	require.Error(t, err, "只有 VmPeak、没有 VmHWM ⇒ 必须报错（不许拿 VmPeak 当读数）")
	require.ErrorIs(t, err, traceverify.ErrVmHWMMissing,
		"缺字段的错误身份必须是 ErrVmHWMMissing（调用方据此区分「读不到」与「读不懂」）")
}

// TestParseVmHWMConvertsKilobytesToBytes kB → bytes 必须乘 1024：漏乘是这类解析里最容易
// 发生的静默错（读数小 1024 倍 ⇒ 内存预算看起来极宽裕）。用 1 kB 与 640 MiB 两个量级钉住，
// 使「只对某些量级凑对」不可能发生。
// killer：`return kb * bytesPerKB, nil` 被改成 `return kb, nil`（m02）。
func TestParseVmHWMConvertsKilobytesToBytes(t *testing.T) {
	got, err := traceverify.ParseVmHWM(statusWithVmHWMKB(1))
	require.NoError(t, err)
	require.Equal(t, int64(1024), got, "1 kB = 1024 bytes（漏乘 1024 会把读数缩小 1024 倍）")

	got, err = traceverify.ParseVmHWM(statusWithVmHWMKB(640 * 1024)) // 640 MiB
	require.NoError(t, err)
	require.Equal(t, int64(640<<20), got, "640 MiB 的 kB 值应换算成 640 MiB 的 bytes 值")

	// 单位不是 kB 时不得按 1024 硬乘（必须报错）—— 否则换一个单位就静默算错。
	_, err = traceverify.ParseVmHWM("Name:\tgateway\nVmHWM:\t 100000 MB\n")
	require.Error(t, err, "单位不是 kB ⇒ 必须报错（不许静默当成 kB 乘 1024）")
}

// TestParseVmHWMErrorsOnMissingField 缺 VmHWM 行时必须**报错**，绝不返回 0 冒充读数：
// 0 会被下游当成一个合法的峰值，把「没有证据」洗成「内存很小」（本包最不可接受的失效）。
// killer：`return 0, fmt.Errorf("%w: 没有任何以 \`VmHWM:\` 起头的行", ErrVmHWMMissing)`
// 被改成 `return 0, nil`（m03）。
func TestParseVmHWMErrorsOnMissingField(t *testing.T) {
	for _, text := range []string{
		"",
		"Name:\tgateway\nVmRSS:\t  100 kB\n",
		"VmHWM 没有冒号\n",
		"VmHWMX:\t  100 kB\n", // 前缀相似但不是该字段
	} {
		got, err := traceverify.ParseVmHWM(text)
		require.Error(t, err, "缺 VmHWM 行必须报错（输入 %q）", text)
		require.ErrorIs(t, err, traceverify.ErrVmHWMMissing, "输入 %q 的错误身份", text)
		require.Zero(t, got, "报错时不得同时返回一个像读数一样的值（输入 %q）", text)
	}
}

// scriptedRead 是「按调用次序吐出预设读数」的替身 reader：每调用一次返回一段合成的
// status 文本，并把 pid 记进 calls（用来钉遍历次序与采样次数）。
type scriptedRead struct {
	values []int64
	calls  []int
}

func (s *scriptedRead) read(pid int) (string, error) {
	s.calls = append(s.calls, pid)
	i := len(s.calls) - 1
	if i >= len(s.values) {
		i = len(s.values) - 1
	}
	return statusWithVmHWMKB(s.values[i]), nil
}

// TestPeakVmHWMTracksMaximumAcrossSamples 峰值必须取「全部 pid × 全部采样」的最大值
// （只在单次快照上取最大 ⇒ 采样循环形同虚设，峰值会被低估）。
// 同时钉两件事：① 每个 pid 每轮都读（次序 pid 列表 × samples 次）；② 采样间隔真的被等。
func TestPeakVmHWMTracksMaximumAcrossSamples(t *testing.T) {
	r := &scriptedRead{values: []int64{100, 50, 30, 300, 20, 40}}
	peak, err := traceverify.VmHWMProbe{Samples: 3, Read: r.read}.Peak([]int{10, 11})
	require.NoError(t, err)
	require.Equal(t, int64(300*1024), peak, "峰值必须跨采样取最大（300 kB 出现在第二轮）")
	require.Equal(t, []int{10, 11, 10, 11, 10, 11}, r.calls, "每个 pid 每轮都要读一次")

	iv := 5 * time.Millisecond
	start := time.Now()
	_, err = traceverify.VmHWMProbe{
		Samples:  3,
		Interval: iv,
		Read:     func(int) (string, error) { return statusWithVmHWMKB(1), nil },
	}.Peak([]int{1})
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 2*iv,
		"采样间隔必须被遵守：samples=3 ⇒ 至少等 samples-1 个间隔")
}

// TestPeakVmHWMReadsStatusFilesUnderRoot Root 面用真实文件驱动一次真实读取
// （`<root>/<pid>/status`，与 procfs 的布局一致）。
func TestPeakVmHWMReadsStatusFilesUnderRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "4242")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status"),
		[]byte("Name:\tgateway\nVmHWM:\t  4096 kB\nVmRSS:\t  100 kB\n"), 0o644))

	peak, err := traceverify.VmHWMProbe{Root: root, Samples: 1}.Peak([]int{4242})
	require.NoError(t, err)
	require.Equal(t, int64(4096*1024), peak)
}

// TestPeakVmHWMRejectsInvalidArguments 参数非法必须**在任何读取之前**报错
// （pids 为空 / samples ≤ 0 / interval < 0），且一次读都不许发生。
func TestPeakVmHWMRejectsInvalidArguments(t *testing.T) {
	r := &scriptedRead{values: []int64{100}}
	cases := []struct {
		name     string
		pids     []int
		samples  int
		interval time.Duration
	}{
		{"pids为空", nil, 1, 0},
		{"samples为零", []int{1}, 0, 0},
		{"samples为负", []int{1}, -1, 0},
		{"interval为负", []int{1}, 1, -time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(r.calls)
			_, err := traceverify.VmHWMProbe{
				Samples: c.samples, Interval: c.interval, Read: r.read,
			}.Peak(c.pids)
			require.Error(t, err, "%s：必须报错", c.name)
			require.Equal(t, before, len(r.calls), "%s：参数校验必须发生在任何读取之前", c.name)
		})
	}
}

// TestPeakVmHWMPropagatesReadErrors 读不到 ⇒ 报错（不许当成 0 继续），包括
// ① reader 报错；② 文本里没有 VmHWM 行；③ 默认 procfs 根下不存在的 pid。
func TestPeakVmHWMPropagatesReadErrors(t *testing.T) {
	t.Run("reader报错", func(t *testing.T) {
		p := traceverify.VmHWMProbe{Samples: 1, Read: func(int) (string, error) {
			return "", fmt.Errorf("traceverify_test: 读不到 status")
		}}
		_, err := p.Peak([]int{7})
		require.Error(t, err, "reader 报错必须向上传播")
	})

	t.Run("文本缺VmHWM行", func(t *testing.T) {
		p := traceverify.VmHWMProbe{Samples: 1, Read: func(int) (string, error) {
			return "Name:\tgateway\nVmRSS:\t  100 kB\n", nil
		}}
		_, err := p.Peak([]int{7})
		require.Error(t, err, "缺 VmHWM 行必须报错")
	})

	t.Run("默认根下不存在的pid", func(t *testing.T) {
		// 默认 procfs 根：本机（darwin）没有 /proc；Linux 上 1<<30 也不存在
		// ⇒ 两个平台上都必须以「读不到」失败（而不是静默返回 0）。
		_, err := traceverify.PeakVmHWM([]int{1 << 30}, 1, 0)
		require.Error(t, err, "不存在的进程必须报错")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// decode-bench 的承重逻辑（与 VmHWM 采样同源：被测对象就是解码路径）
// ─────────────────────────────────────────────────────────────────────────────

// TestParseConcurrencyLevelsNormalizesAndRejects `--concurrency 1,2,3` 的解析：
// 去重后升序（档位的测试次序必须确定），空档/非整数/越界/重复一律报错。
func TestParseConcurrencyLevelsNormalizesAndRejects(t *testing.T) {
	got, err := traceverify.ParseConcurrencyLevels("1,2,3")
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, got)

	got, err = traceverify.ParseConcurrencyLevels("3, 1 ,2")
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, got, "输出必须升序（与入参顺序无关）")

	for _, in := range []string{"", "   ", "1,,2", "a", "1,a", "0", "-1", "65", "2,2"} {
		t.Run(in, func(t *testing.T) {
			_, err := traceverify.ParseConcurrencyLevels(in)
			require.Error(t, err, "--concurrency %q 必须报错", in)
		})
	}
}

// TestDecodeArtifactRoundTripsRealArtifact 解码必须走**真实**的 zstd 产物（用与读侧
// 同一个 klauspost 库写、同一个库读），并返回解压后的字节数；读不到 / 不是 zstd ⇒ 报错。
// 这是 decode-bench 的承重前置：解压字节数错了，`peak_vmhwm_bytes` 的对照就没有意义。
func TestDecodeArtifactRoundTripsRealArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "part-000001.jsonl.zst")

	payload := []byte("{\"row\":0}\n{\"row\":1}\n")
	f, err := os.Create(path)
	require.NoError(t, err)
	w, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	require.NoError(t, err)
	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())

	n, err := traceverify.DecodeArtifact(path, 0)
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), n, "必须返回**解压后**的字节数")

	n, err = traceverify.DecodeArtifact(path, 1<<20)
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), n, "显式窗口同样能解")

	_, err = traceverify.DecodeArtifact(filepath.Join(dir, "不存在.zst"), 0)
	require.Error(t, err, "读不到产物必须报错")
	_, err = traceverify.DecodeArtifact("", 0)
	require.Error(t, err, "空路径必须报错")

	plain := filepath.Join(dir, "not-zstd.jsonl")
	require.NoError(t, os.WriteFile(plain, payload, 0o644))
	_, err = traceverify.DecodeArtifact(plain, 0)
	require.Error(t, err, "不是 zstd 的输入必须报错（不许当成 0 字节顺利返回）")
}

// ─────────────────────────────────────────────────────────────────────────────
// decode-bench 的点读负载（计划任务 9 步骤 3-4）
//
// 这些用例用**替身 reader** 驱动真实负载：不需要产物、不需要 zstd 子进程、不需要 /proc
// ⇒ 在 darwin 上也能真跑。「K 路并发」「随机行号」「错误传播」这三条是 A6/C5 结论的
// 承重前提（配置了 K 却实际串行 ⇒ 峰值偏低 ⇒ 结论失真）。
// ─────────────────────────────────────────────────────────────────────────────

// TestPointReadLoadRunsConcurrentlyAndRandomly K 个 goroutine × rounds × points 次点读：
// 点数必须跑满、实测同时在飞峰值必须等于档位、行号必须随机落在 [0, rows) 内。
// killer：行号被固定成 0（m04，DistinctRows==1）或 goroutine 数写成 K+1（m05）。
func TestPointReadLoadRunsConcurrentlyAndRandomly(t *testing.T) {
	const rows, k, rounds, points = 8, 4, 5, 50

	var inFlight, maxInFlight int64
	var mu sync.Mutex
	seen := map[int]int{}
	read := func(_ context.Context, ref tracearchive.Ref) ([]byte, error) {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			old := atomic.LoadInt64(&maxInFlight)
			if cur <= old || atomic.CompareAndSwapInt64(&maxInFlight, old, cur) {
				break
			}
		}
		time.Sleep(200 * time.Microsecond) // 让并发真的重叠（否则「在飞峰值」没有意义）
		atomic.AddInt64(&inFlight, -1)
		mu.Lock()
		seen[ref.Row]++
		mu.Unlock()
		return []byte("x"), nil
	}

	res, err := traceverify.PointReadLoad{
		Ref:                tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1},
		Rows:               rows,
		Concurrency:        k,
		Rounds:             rounds,
		PointsPerGoroutine: points,
		Seed:               42,
		Read:               read,
	}.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, k*rounds*points, res.Points, "点数必须是 K × rounds × 每 goroutine 每轮点数")
	require.Equal(t, k, res.MaxInFlight, "实测同时在飞峰值必须等于档位（否则这一档没真正并发）")
	require.Equal(t, int64(k*rounds*points), res.Bytes, "每行 1 字节 ⇒ 总字节数 == 点数")
	require.Greater(t, res.DistinctRows, 1, "行号必须随机（固定成 0 会让该断言转红）")
	require.Equal(t, len(seen), res.DistinctRows, "DistinctRows 必须等于实际命中过的行号个数")
	for row := range seen {
		require.GreaterOrEqual(t, row, 0, "行号下界")
		require.Less(t, row, rows, "行号必须落在 [0, rows) 内")
	}
}

// TestPointReadLoadRejectsInvalidArguments 参数非法必须在**任何点读之前**报错
// （Read 为空 / Rows ≤ 0 / Concurrency ≤ 0 / Rounds ≤ 0），且一次读都不许发生。
func TestPointReadLoadRejectsInvalidArguments(t *testing.T) {
	var calls int64
	read := func(context.Context, tracearchive.Ref) ([]byte, error) {
		atomic.AddInt64(&calls, 1)
		return nil, nil
	}
	base := traceverify.PointReadLoad{
		Ref:         tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1},
		Rows:        8,
		Concurrency: 2,
		Rounds:      5,
		Read:        read,
	}
	cases := []struct {
		name string
		load traceverify.PointReadLoad
	}{
		{"Read为空", func() traceverify.PointReadLoad { l := base; l.Read = nil; return l }()},
		{"Rows为零", func() traceverify.PointReadLoad { l := base; l.Rows = 0; return l }()},
		{"Rows为负", func() traceverify.PointReadLoad { l := base; l.Rows = -1; return l }()},
		{"Concurrency为零", func() traceverify.PointReadLoad { l := base; l.Concurrency = 0; return l }()},
		{"Rounds为零", func() traceverify.PointReadLoad { l := base; l.Rounds = 0; return l }()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := atomic.LoadInt64(&calls)
			_, err := c.load.Run(context.Background())
			require.Error(t, err, "%s：必须报错", c.name)
			require.Equal(t, before, atomic.LoadInt64(&calls), "%s：参数校验必须发生在任何点读之前", c.name)
		})
	}
}

// TestPointReadLoadPropagatesReadErrors 任一次点读失败 ⇒ Run 必须把该错误原样上抛
// （不许吞掉、不许返回半个读数）：吞掉会让「盘有问题」看起来像一次成功的标定。
// killer：错误分支被放宽成 `if err != nil && false`（m06）。
func TestPointReadLoadPropagatesReadErrors(t *testing.T) {
	var calls int64
	boom := errors.New("traceverify_test: 产物损坏")
	read := func(_ context.Context, ref tracearchive.Ref) ([]byte, error) {
		if atomic.AddInt64(&calls, 1) == 3 {
			return nil, boom
		}
		return []byte("x"), nil
	}
	_, err := traceverify.PointReadLoad{
		Ref:                tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1},
		Rows:               4,
		Concurrency:        2,
		Rounds:             5,
		PointsPerGoroutine: 10,
		Read:               read,
	}.Run(context.Background())
	require.Error(t, err, "点读失败必须上抛（不许当成成功）")
	require.ErrorIs(t, err, boom, "原始错误必须被包装保留（errors.Is 可判）")
}

// TestArtifactRowsBinaryProbesRowCount 行数探测：用 GetBody 的错误身份（越界 ⇒
// ErrNotArchived；标记行 ⇒ ErrBodyMissing）倍增 + 二分求出行数。逐档覆盖 1 / 2 / 3 /
// 1000 与「中点是标记行」的形态。
// killer：`return lo + 1` 被改成 `return lo`（m07，整体差一行）。
func TestArtifactRowsBinaryProbesRowCount(t *testing.T) {
	for _, rows := range []int{1, 2, 3, 5, 1000} {
		rows := rows
		t.Run(fmt.Sprintf("rows=%d", rows), func(t *testing.T) {
			read := func(_ context.Context, ref tracearchive.Ref) ([]byte, error) {
				if ref.Row < 0 || ref.Row >= rows {
					return nil, fmt.Errorf("%w: 越界", tracearchive.ErrNotArchived)
				}
				if rows > 2 && ref.Row == rows/2 {
					return nil, fmt.Errorf("%w: 标记行", tracearchive.ErrBodyMissing)
				}
				return []byte("body"), nil
			}
			got, err := traceverify.ArtifactRows(context.Background(), read,
				tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1}, 0)
			require.NoError(t, err)
			require.Equal(t, rows, got, "探测出的行数必须等于真实行数")
		})
	}
}

// TestArtifactRowsPropagatesNonRangeErrors 未知错误必须原样上抛：把「读不懂的错误」
// 当成「行号越界」会让探测出一个错的（偏小的）行数，随机点读随即落到不存在的行上。
func TestArtifactRowsPropagatesNonRangeErrors(t *testing.T) {
	boom := errors.New("traceverify_test: footer 损坏")
	read := func(_ context.Context, ref tracearchive.Ref) ([]byte, error) {
		if ref.Row >= 2 {
			return nil, boom
		}
		return []byte("body"), nil
	}
	_, err := traceverify.ArtifactRows(context.Background(), read,
		tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1}, 0)
	require.Error(t, err, "未知错误必须上抛（不许当成越界/在范围）")
	require.ErrorIs(t, err, boom)
}

// TestPointReadLoadAbortsWhenPeakReachesLimit 峰值到 `AbortAtBytes` ⇒ 取消负载、置
// `Aborted`、把观测到的峰值留下来 —— **中止不是失败**（返回部分读数）。
// 这条是 GOGC=off 形态的唯一安全出口：没有它，进程会被 OOM 杀掉、什么都报不出来。
// killer：中止分支被放宽成 `… && false`（m09）。
func TestPointReadLoadAbortsWhenPeakReachesLimit(t *testing.T) {
	const limit = int64(2 << 30)
	peak := int64(3 << 30) // 3 GiB ≥ 2 GiB 阈值
	read := func(context.Context, tracearchive.Ref) ([]byte, error) {
		time.Sleep(time.Millisecond) // 让监控协程有机会先跑
		return []byte("x"), nil
	}
	res, err := traceverify.PointReadLoad{
		Ref:                tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1},
		Rows:               8,
		Concurrency:        2,
		Rounds:             5,
		PointsPerGoroutine: 50,
		Read:               read,
		Peak:               func() (int64, error) { return peak, nil },
		AbortAtBytes:       limit,
		SampleInterval:     time.Millisecond,
	}.Run(context.Background())
	require.NoError(t, err, "到限中止不是失败：必须返回部分读数，而不是 error")
	require.True(t, res.Aborted, "峰值 ≥ 阈值 ⇒ 必须置 Aborted")
	require.GreaterOrEqual(t, res.PeakBytes, limit, "必须把观测到的峰值留下来（≥ 阈值）")
	require.Greater(t, res.PeakSamples, 0, "必须真的采过样")
	require.Less(t, res.Points, 2*5*50, "到限必须中止负载，而不是跑满")
}

// TestPointReadLoadPropagatesPeakErrors 峰值读不到 ⇒ 必须报错（绝不把「没有证据」洗成
// 「内存很小」），并且取消负载。
// killer：采样失败分支被放宽成 `_ = err`（m08）。
func TestPointReadLoadPropagatesPeakErrors(t *testing.T) {
	boom := errors.New("traceverify_test: procfs 读不到")
	read := func(context.Context, tracearchive.Ref) ([]byte, error) {
		time.Sleep(time.Millisecond)
		return []byte("x"), nil
	}
	_, err := traceverify.PointReadLoad{
		Ref:                tracearchive.Ref{DT: "2026-10-08", Inst: "blue", Part: 1},
		Rows:               8,
		Concurrency:        2,
		Rounds:             5,
		PointsPerGoroutine: 50,
		Read:               read,
		Peak:               func() (int64, error) { return 0, boom },
		SampleInterval:     time.Millisecond,
	}.Run(context.Background())
	require.Error(t, err, "峰值采样失败必须上抛（不许当成成功）")
	require.ErrorIs(t, err, boom)
}
