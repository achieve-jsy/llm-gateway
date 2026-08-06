package tracearchive

import "errors"

// 读取侧的三类失败必须严格区分（规格 §8.1）：
// 把「文件缺失」误报成「数据不存在」是 v3 方案的严重缺陷，必须避免。
//
// 与 ErrTrailerCorrupt（index.go）、ErrFooterCorrupt（footer.go）同属本包的
// 哨兵错误集合，调用方一律用 errors.Is 分类，故对外包装时必须用 %w。
var (
	// ErrPartNotFound 产物文件缺失：正文曾落盘，但文件不在（需人工介入）。
	// 对应 HTTP 410。
	ErrPartNotFound = errors.New("tracearchive: 产物文件缺失")

	// ErrBodyMissing 正文在写入时即失败，从未落盘。对应 HTTP 404。
	ErrBodyMissing = errors.New("tracearchive: 正文从未落盘")

	// ErrNotArchived「无法按 (dt, inst, part, row) 定位到正文，且没有任何更精确的
	// 成因可归」。对应 HTTP 500。实际覆盖（不止「文件都不存在」）：
	//   - 产物与 spool 都不存在（reader.go 的 GetBody 兜底分支）；
	//   - 产物【存在】但行号越界（row ≥ footer 的 row_count）；
	//   - 产物【存在】且解压成功，但目标偏移未命中任何行首（残行/索引错位）；
	//   - spool【存在】但第 row 行是未以换行结尾的残行，或压根没有第 row 行。
	// 读数分支必须区分「没有这一行」与「存储坏了」——把后三者报成「不存在」会把
	// 存储故障伪装成业务事实（v3 方案的教训），所以它们都用这个哨兵而不是 404 类。
	// 真正的「正文写入时即失败」是 ErrBodyMissing（标记行，404）。
	ErrNotArchived = errors.New("tracearchive: 产物与暂存均不存在")
)

// 封块侧的错误（方案 C）。sealer 的调用方（计划 4 的 sealer 循环/告警）
// 需要把「重复封块」与「压缩失败」区分开：
//   - ErrPartExists 是幂等的、无害的（下轮扫描还会看到它，但不必告警）；
//   - ErrRowCountMismatch / ErrSealFailed 需要告警与重试。
//
// 三者都必须用 %w 包装，否则调用方的 errors.Is 分类失效。
var (
	// ErrPartExists 产物已存在，拒绝覆盖（规格 §7.3：宁可留下 spool，不可覆盖唯一副本）。
	ErrPartExists = errors.New("tracearchive: 产物已存在，拒绝覆盖")

	// ErrRowCountMismatch spool 的实际行数与调用方记录的行数不符
	//（spool 被截断，或写入侧与封块侧对不上）。此时拒绝封块、保留 spool。
	ErrRowCountMismatch = errors.New("tracearchive: 行数不符")

	// ErrTooManyRows 封块时 spool 的行数超过 trailer 条目数上界 maxTrailerEntries
	//（写侧与读侧共用同一个上界；常量与取值推导在 reader.go）。
	//
	// 【为什么必须与 ErrRowCountMismatch 分开（F3 缺陷 A 的裁定）】超限时调用方
	// 记录的行数可能与实测【完全相同】——「行数不符」在那时是【错误诊断】。
	// 读侧对同一形态报的是 ErrTrailerCorrupt（产物损坏），写侧若也报「损坏」，
	// 运维会去查产物损坏，而真实成因是「块内行太密，超出读侧内存预算」。
	//
	// 语义固定为「拒绝封块、保留 spool、不产生任何产物（含 .tmp 与目录）、
	// 需人工介入」：与前两者不同，单纯重试【永远不会】成功——
	// 同一个 spool 重扫多少次行数都一样，必须有人拆分块或重新标定预算。
	ErrTooManyRows = errors.New("tracearchive: spool 行数超过 trailer 条目数上界")

	// ErrSealFailed 封块失败：含 zstd 子进程非零退出、子进程无法启动（二进制缺失）、
	// 或压缩 I/O 失败。语义固定为「保留 spool、删除 .tmp、不产生产物、下轮重试」。
	ErrSealFailed = errors.New("tracearchive: 封块失败")
)
