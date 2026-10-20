// 本文件是 B 层测试（tests/traceverify，package traceverify_test）：C5 解码内存预算的
// **算术**（计划 4 / 任务 9 步骤 1）。它是纯函数，不需要 /proc、不需要 zstd、不碰磁盘
// ⇒ 在 darwin 上也能真跑。
//
// 契约（计划 4 任务 9 的接口块 + 交接 H5/H10）：
//
//	PerReadPeakBytes = DecodeWindowBytes(512 MiB) + MaxIndexBytes(64 MiB) + PerReadFixedBytes(64 MiB)
//	                 = 640 MiB（**加法**，三项同时在场）
//	MaxConcurrent    = floor((限额 − 基线 − 安全垫) / 每次点读)，向下取整；<1 ⇒ 1；
//	                   perRead ≤ 0 ⇒ 1（不许 panic、不许负值）
package traceverify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/traceverify"
)

// TestPerReadPeakMatchesSpecBudget 640 MiB = 512（窗口）+ 64（索引）+ 64（tb 缓冲）。
// killer：把任一项改错，或把加法改成乘法（见 p4-task-9-budget.py 的 m01）。
func TestPerReadPeakMatchesSpecBudget(t *testing.T) {
	// 三项各自的取值先钉住：只钉合计的话，「两项各错一点、合计凑对」会漏过去。
	require.Equal(t, int64(512<<20), traceverify.DecodeWindowBytes, "窗口必须是 512 MiB（冻结包 defaultZstdWindowBytes）")
	require.Equal(t, int64(64<<20), traceverify.MaxIndexBytes, "索引上界必须是 64 MiB（8 B × maxTrailerEntries）")
	require.Equal(t, int64(64<<20), traceverify.PerReadFixedBytes, "单次点读固定缓冲必须是 64 MiB（冻结包 tb 上界）")

	require.Equal(t, int64(640<<20), traceverify.PerReadPeakBytes(),
		"一次并发点读的峰值 = 512 + 64 + 64 = 640 MiB（加法，不是乘法）")

	// 减法形态的等价断言：任一项被漏加 ⇒ 合计必然小于 640 MiB。
	require.Equal(t,
		traceverify.DecodeWindowBytes+traceverify.MaxIndexBytes+traceverify.PerReadFixedBytes,
		traceverify.PerReadPeakBytes(),
		"合计必须是三项之和；漏加任一项都会把预算算小（并发上限随之被算大）")

	// C5 预算取自蓝绿生产编排（D72 的 memory: 2g），不是旧文档里的 512 MB。
	require.Equal(t, int64(2<<30), traceverify.ControlPlaneLimitBytes, "控制面限额必须是蓝绿编排的 2 GiB（D72）")
}

// TestMaxConcurrentMatchesControlPlaneBudget 2 GiB 限额、基线 300 MiB、安全垫 256 MiB
// ⇒ (2048−300−256)/640 = 2.33 ⇒ **2**（向下取整）。
// killer：floor 改 ceil（会算成 3 ⇒ 超额）。另用一条**刚好卡边**的输入把
// 「安全垫必须真的被减掉」这一条也钉住（不减时会算成 2 —— 与正确答案不同）。
func TestMaxConcurrentMatchesControlPlaneBudget(t *testing.T) {
	const baseline = int64(300 << 20)
	const safety = int64(256 << 20)

	got := traceverify.MaxConcurrent(traceverify.ControlPlaneLimitBytes, baseline, safety,
		traceverify.PerReadPeakBytes())
	require.Equal(t, 2, got, "(2048-300-256)/640 = 2.33 ⇒ 向下取整 = 2（ceil 会算成 3 ⇒ 超额）")

	// 向下取整方向必须确定：把限额加到刚好够 3 次之前，仍然是 2。
	justUnderThree := baseline + safety + 3*traceverify.PerReadPeakBytes() - 1
	require.Equal(t, 2, traceverify.MaxConcurrent(justUnderThree, baseline, safety, traceverify.PerReadPeakBytes()),
		"差 1 字节不够第 3 次 ⇒ 必须是 2（向下取整；ceil 会给 3）")
	// 刚好够 3 次 ⇒ 3。
	exactThree := baseline + safety + 3*traceverify.PerReadPeakBytes()
	require.Equal(t, 3, traceverify.MaxConcurrent(exactThree, baseline, safety, traceverify.PerReadPeakBytes()),
		"刚好够 3 次 ⇒ 3（边界不能提前退档）")

	// 【减法存在】的边界：perRead=640 MiB、baseline=0、safety=128 MiB、limit=1408 MiB−1。
	//   减掉安全垫 ⇒ (1408−128) MiB − 1 = 1279 MiB ⇒ floor(1.998) = 1
	//   不减安全垫 ⇒ 1408 MiB − 1     ⇒ floor(2.19)  = 2
	// 两者不同 ⇒ 这一条能杀掉「忘了减安全垫」（也会连带杀掉 ceil）。
	edgeLimit := int64(1408<<20) - 1
	require.Equal(t, 1, traceverify.MaxConcurrent(edgeLimit, 0, 128<<20, 640<<20),
		"安全垫必须真的被减掉：不减会算成 2（超出限额）；ceil 同样会算成 2")
	// 同一组输入、安全垫改成 0 ⇒ 2 ⇒ 证明上面那个 1 确实来自「减掉了安全垫」这一项。
	require.Equal(t, 2, traceverify.MaxConcurrent(edgeLimit, 0, 0, 640<<20),
		"安全垫为 0 时同一限额下是 2 ⇒ 上一条的 1 由「减法」造成")
}

// TestMaxConcurrentIsAtLeastOne 算出来 <1 时返回 1，并明确「1 是下限，不是安全保证」：
// 这条保证调用方（计划 3 的读侧）永远拿到一个可用上限，而不是 0 导致功能全关。
// killer：下限 `if n < 1 { n = 1 }` 被删掉（返回 0）。
func TestMaxConcurrentIsAtLeastOne(t *testing.T) {
	const baseline = int64(512 << 20)
	const safety = int64(256 << 20)
	const limit = int64(1 << 30)

	// 先用不等式把「这组输入本来就超限」这件事钉住 —— 否则本用例会退化成
	// 「随便一个能返回 1 的输入」，读者无法判断 1 是下限还是正确答案。
	require.Greater(t, baseline+safety+traceverify.PerReadPeakBytes(), limit,
		"本用例刻意用「基线+安全垫+一次点读 > 限额」的输入：算术结果必然 <1")

	require.Equal(t, 1, traceverify.MaxConcurrent(limit, baseline, safety, traceverify.PerReadPeakBytes()),
		"算出来 <1 必须返回 1（下限，不是安全保证）；返回 0 会把读侧功能整体关掉")

	// 负数也算 <1：限额被基线吃掉的情形不能返回负并发数。
	require.Equal(t, 1, traceverify.MaxConcurrent(limit, 2<<30, safety, traceverify.PerReadPeakBytes()),
		"分子为负 ⇒ 仍是 1（绝不返回负值）")
}

// TestMaxConcurrentRejectsNonPositivePerRead perRead ≤ 0 ⇒ 返回 1（不许除零/负值，
// 也不许 panic）：整数除法在 Go 里对 0 会 panic，而这条路径由调用方传参决定。
// killer：`perReadBytes <= 0` 的兜底被放宽（非法分母被当成 1 ⇒ 返回一个巨大的并发数）。
func TestMaxConcurrentRejectsNonPositivePerRead(t *testing.T) {
	for _, perRead := range []int64{0, -1, -640 << 20} {
		var got int
		require.NotPanics(t, func() {
			got = traceverify.MaxConcurrent(traceverify.ControlPlaneLimitBytes, 0, 0, perRead)
		}, "perRead=%d 不得 panic（整数除法对 0 会 panic）", perRead)
		require.Equal(t, 1, got, "perRead=%d ≤ 0 ⇒ 必须返回 1（不许除零/负值/panic）", perRead)
	}
}
