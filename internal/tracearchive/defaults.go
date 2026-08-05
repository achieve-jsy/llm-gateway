package tracearchive

import "time"

// 压缩与封块参数（规格 §5.3.3）。
//
// ⚠️ 2026-09-24 的标定暴露了规格未预料的缺口，本文件的注释以实测为准，
// 与规格 §5.3.3 的表述存在差异——差异处已在下方逐条标注。
//
// 标定样本：/datalog/emergency-archive/dt=2026-09-18 前 8 个 part，
// 解压后 2078.7 MB（2,179,635,659 字节），以 embedding 请求为主，是最难压的真实数据。
//
//	Go 库 klauspost/compress              C 版 zstd CLI
//	级别 19  窗口 512MB  384.0 MB  5.41x  级别 19  --long=29  512MB  202.6 MB  10.26x
//	级别 19  窗口 128MB  423.0 MB  4.91x  级别 19  --long=27  128MB  397.1 MB   5.23x
//	级别  9  窗口 512MB  557.6 MB  3.73x  级别  9  --long=29  512MB  224.8 MB   9.25x
//	级别 22  窗口 512MB  384.0 MB  5.41x  （--long 缺省窗口）        535.1 MB   3.88x
//
// 三条实测结论（与规格 §15 U7 的预期不符）：
//
//  1. U7 不成立。Go 库产物是 C 版 CLI 的 1.90 倍，而验收线是 ≤1.30 倍。
//  2. WithWindowSize 在 Go 库上只花内存、几乎不给收益。级别 9 下窗口
//     128→512 MiB 产物一个字节都没变（557.6→557.6 MB）；级别 19 只降 9.2%。
//     同条件 C 版 CLI 降 49.0%。CLI 的收益来自 --long 的长距离匹配器，
//     klauspost/compress 没有等价实现——这是能力缺口，不是参数没调好。
//  3. 内存：Go 库 512 MiB 窗口实测峰值 RSS 1066 MB（≈窗口 × 2.08）；
//     C 版 CLI 同窗口为 570 MB（≈窗口 × 1.08），规格 §5.3.3 引用的正是后者。
//
// 因此「用哪个编码器」在标定后提交裁决，结论为**方案 C**：
//
//	封块移出网关，由独立的 sealer 进程调用 C 版 zstd CLI（-19 --long=29）。
//
// 如此选择的原因：压缩比 5.41x → 10.26x；容量约 19 天 → 约 29 天；
// 且编码器的 570 MB 峰值内存不再计入网关的 2 GiB cgroup（归零）。
// 代价是新增一个部署单元，以及网关容器外需要 zstd 二进制。
//
// 对本包的影响：本文件的两个常量仍被使用，但含义变了——
//   - defaultZstdLevel 直接作为 CLI 的 -N 参数
//   - defaultZstdWindowBytes 需换算成 CLI 的 --long=<log2(窗口)>=29
//     （换算在 Sealer 内完成，见计划 Task 6）
//
// 解码侧不受影响：仍用 Go 库 klauspost/compress/zstd，故依赖保留。
const (
	// defaultZstdLevel zstd 压缩级别。
	// 实测：Go 库级别 19 与 22 产物完全相同（均 384.0 MB），级别 22 只多耗 CPU，
	// 故取 19。难压样本上 9→19 使产物从 557.6 MB 降到 384.0 MB（-31%）。
	defaultZstdLevel = 19

	// defaultZstdWindowBytes zstd 窗口。
	// ⚠️ 方案 C 之后，写侧已【没有 Go 编码器】：封块由独立的 sealer 进程调 C 版
	// zstd CLI 完成，本值只用于换算 CLI 的 --long=<log2>=29（换算在 sealer.go 的
	// zstdLongArg），因此上面「对 Go 库几乎无效」那条实测结论只解释【为什么当初
	// 没靠调窗口打赢】，不代表当前写侧还在用 Go 库。保留 512 MiB 的理由：
	//  1) C 版 CLI --long=29 是实测最优档（比 128 MiB 档降 49%）；
	//  2) 读取端仍在网关内用 Go 库解码，WithDecoderMaxWindow 必须与写入侧一致
	//     （解码器内存 ≈ 窗口）。
	defaultZstdWindowBytes = 512 << 20

	// defaultBlockBytes 未压缩块大小阈值（1.5 GiB）。
	// 块越大，单次点读需要解压的字节越多；1.5 GiB 是「压缩比」与「读取尾部延迟」
	// 之间的暂定折中，最终值由规格 §15 U1 的实测决定。
	defaultBlockBytes = 1536 << 20

	// defaultSealInterval 时间触发封块阈值。
	// 若不按时间封块，低峰天永远达不到大小阈值 ⇒ 正文写了却读不到。
	// 该值是本包唯一凭直觉取的值：调小可缩短「刚写入的正文要等多久才能从产物读」，
	// 代价是块变小、压缩比下降。
	defaultSealInterval = 15 * time.Minute
)
