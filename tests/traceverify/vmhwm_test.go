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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
