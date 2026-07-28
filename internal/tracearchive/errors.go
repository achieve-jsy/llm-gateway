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

	// ErrNotArchived 产物与 spool 都不存在，且无 body_missing 标记。
	// 说明元数据与存储不一致，属异常，对应 HTTP 500。
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

	// ErrSealFailed 封块失败：含 zstd 子进程非零退出、子进程无法启动（二进制缺失）、
	// 或压缩 I/O 失败。语义固定为「保留 spool、删除 .tmp、不产生产物、下轮重试」。
	ErrSealFailed = errors.New("tracearchive: 封块失败")
)
