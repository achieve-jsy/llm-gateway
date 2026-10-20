package traceverify

// 本文件是 C5（解码内存预算）的**算术单源**（计划 4 / 任务 9 步骤 1-2）。
//
// 为什么它必须是一个可测的纯算术，而不是散在 `cmd/trace-verify` 里的一个常量：
// 读侧（计划 3）的 `TRACE_BODY_READ_MAX_CONCURRENT` 直接取 `MaxConcurrent` 的返回值
// （本计划交接 **H5** / **H10**），而「K 是否安全」的判据就是这里的减法与取整。
// 把它做成纯函数，`tests/traceverify/budget_test.go` 才能把每一项与取整方向逐条钉住；
// `decode-bench` 只负责**测量**，判定留给报告。
//
// 【算术的推导链（承重，不得只留结论）】
//
//		一次并发点读的峰值 ≈ 解码窗口 + 解码索引 + 单次点读固定缓冲
//		                   = 512 MiB   + 64 MiB    + 64 MiB   = 640 MiB
//
//	  - 512 MiB 是冻结包的 `defaultZstdWindowBytes`（`internal/tracearchive/defaults.go:57`）：
//	    生产封块用 C 版 `zstd -19 --long=29`，读侧用 Go 库解码且 `WithDecoderMaxWindow`
//	    取同一个值 ⇒ **解码器内存 ≈ 窗口大小**（规格 §8.4 的 C5 就是标定这个量）。
//	  - 64 MiB 索引 = 8 B × `maxTrailerEntries`：冻结包 `reader.go:30-62` 的条目数上界把
//	    「trailer 字节 + 解码索引」压到 ≤128 MiB 的一半；正是这个上界把更早实测的病态值
//	    （单次点读峰值堆 1090 MiB、稳态 2 并发 1666 MiB、连续多轮不做 GC 时 3268 MiB）
//	    挡在门外。
//	  - 64 MiB 是冻结包 `reader.go` 里 `tb`（trailer 字节缓冲）的上界（= `maxTrailerBytes`）。
const (
	// ControlPlaneLimitBytes 是 C5 预算：控制面容器的内存上限。取**蓝绿生产编排**的值（D72）：
	// deploy/blue-green/docker-compose.{blue,green}.yml 两个文件都是 memory: 2g。
	ControlPlaneLimitBytes int64 = 2 << 30
	// DecodeWindowBytes = 冻结包 defaultZstdWindowBytes（512 MiB）：点读必须把整个窗口载入。
	DecodeWindowBytes int64 = 512 << 20
	// PerReadFixedBytes 是单次点读的固定缓冲：冻结包 reader.go 里 tb 的 64 MiB 上界。
	PerReadFixedBytes int64 = 64 << 20
	// MaxIndexBytes 是索引上界：8 B × maxTrailerEntries = 64 MiB。
	MaxIndexBytes int64 = 64 << 20
)

// PerReadPeakBytes 返回**一次并发点读**的峰值内存（bytes）= 窗口 + 索引 + 固定缓冲。
//
// ⚠️ 它是**加法**，不是乘法：三项是同一时刻在同一 goroutine 里同时在场的三块内存。
// 任何一项被漏加都会把预算算小、把并发上限算大 —— 那正是「control-plane 被 OOM」
// 的入口，故 `TestPerReadPeakMatchesSpecBudget` 把 640 MiB 与三项各自的值都钉住。
func PerReadPeakBytes() int64 {
	return DecodeWindowBytes + MaxIndexBytes + PerReadFixedBytes
}

// MaxConcurrent 由「限额 − 基线 − 安全垫」除以单次点读峰值算出**可取的最大并发数**。
//
// 口径（逐条，全部是承重语义）：
//
//   - **向下取整**（整数除法）：向上取整会在「刚好多出 0.3 次点读」时放行一个必然超限
//     的并发数（2.33 ⇒ 3 ⇒ 超额）⇒ C5 直接失守。
//   - **减法必须真的存在**：安全垫（runtime/GC/连接池等非解码开销）与基线（空转 RSS）
//     都要从限额里扣掉，否则算出的并发数会把控制面推到限额之上。
//   - **下限是 1，不是 0**：算出来 <1 时返回 1，保证调用方（计划 3 的读侧）永远拿到
//     一个可用上限，而不是 0 导致点读功能全关。⚠️ **1 是下限，不是安全保证** ——
//     当「基线 + 安全垫 + 一次点读」已超过限额时，真实需求本就超限，此时报告必须
//     给出「降低并发」或「提高控制面限额」的结论（计划任务 9 的停止点）。
//   - **perReadBytes ≤ 0 ⇒ 返回 1**：非法输入（0 会让整数除法 panic，负数会算出负的
//     并发数）。这里绝不允许 panic，也绝不允许返回负值。
func MaxConcurrent(limitBytes, baselineBytes, safetyBytes, perReadBytes int64) int {
	if perReadBytes <= 0 {
		return 1
	}
	n := (limitBytes - baselineBytes - safetyBytes) / perReadBytes
	if n < 1 {
		n = 1
	}
	return int(n)
}
