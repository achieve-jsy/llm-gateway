package tracesealer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// FooterSize 是产物文件末尾定长 footer 的字节数。
//
// 【为什么这里要再写一遍（镜像实现）】冻结包的 footer 编码 —— 字段次序、字节序、
// magic、版本、32 字节长度 —— 全部是**未导出**的：`tracearchive` 的 `footer` 结构体、
// `encodeFooter`、`decodeFooter` 都不可见，而本包只需要其中**一个**字段
// （`row_count`）来支撑 A8 的行数观测。Go 没有「引用未导出常量」的办法，所以只能
// 复制一份。它的每一个偏移都必须与 `internal/tracearchive/footer.go` 逐字对应：
//
//	[0:4)   magic            "TBA4"
//	[4:6)   format_version   1（大端 uint16）
//	[6:14)  trailer_offset   大端 uint64 读作 int64
//	[14:18) trailer_len      大端 uint32 读作 int32
//	[18:22) row_count        大端 uint32 读作 int32  ← 本文件唯一的产出
//	[22:30) frame_sha256[:8]
//	[30:32) reserved
//
// 【谁钉住这份镜像】① 本包内的 `tests/tracesealer/footer_test.go` 用**按上述布局
// 手工拼出**的 32 字节断言解析结果（纯函数证据，不需要 zstd）；②
// `tests/tracesealer/footer_roundtrip_test.go` 用冻结包的 `Sealer` 真封一块再解析
// （真实产物证据，需要 zstd）；③ `tests/tracesealer/constants_contract_test.go` 直接
// 断言本常量等于冻结包**已导出**的 `tracearchive.FooterSize`（唯一一个可以直接比对的
// 镜像值）。三者缺一：镜像一旦漂移，A8 的「行数不超上界」判断会**静默失真** ——
// 判断本身还在跑，只是读的是别的字节。
const FooterSize = 32

// footerMagic / footerFormatVersion 是冻结包 footer.go 的 footerMagic / formatVersion
// 的镜像（同样未导出 ⇒ 只能复制）。magic 或版本不符都说明「这段字节不是本格式的
// footer」，必须报错，绝不能把一段随机字节当行数返回。
const (
	footerMagic         = "TBA4"
	footerFormatVersion = uint16(1)
)

// ErrFooterCorrupt 表示产物的定长 footer 与文件本身不相容。实际覆盖：
//   - 文件不足 FooterSize 字节；
//   - magic 不符、format_version 未知；
//   - footer 字段自相矛盾：负的 trailer_offset / trailer_len / row_count；
//   - trailer 未紧邻 footer（trailer_offset + trailer_len ≠ size − FooterSize）。
//
// 【为什么导出、且为什么与冻结包同名】它与「文件不存在」（os.ErrNotExist）是两件
// 事：前者是**产物损坏**（要么修产物、要么按事故看），后者是「这个 part 还没封」。
// 把两者折成一个「读不到就当 0 行」，A8 的行数分布会在产物损坏时静默画成「块都很小」，
// 而这正是本计划反复登记的那一类「观测面自己失真」。名字与冻结包的
// `tracearchive.ErrFooterCorrupt` 有意一致：同一类失效、同一套处置。
var ErrFooterCorrupt = errors.New("tracesealer: 产物 footer 损坏")

// RowCount 只读解析产物末尾的定长 footer，返回其 row_count。
//
// 【只读末尾 32 字节】不打开 zstd、不读 trailer、不解压：本函数的调用点在 sealer
// 每封出一块的路径上，任何「顺带解压一遍」的实现都会把观测变成第二个负载。
//
// 【为什么不做 frame_sha256 校验】校验它需要把帧区整体读一遍（sha256 的输入就是
// 全部帧字节）。本函数只服务于行数观测，而 A8 要压的正是「读侧的内存预算」，不是
// 「逐字节完整性」—— 完整性由冻结包的 Reader 在真正读取时承担（失败会响亮报错）。
// 这个取舍是**刻意的**，不是漏掉：镜像到这里故意止步于「不需要读帧区就能做的字段
// 自洽校验」。
func RowCount(artifactPath string) (int, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		// 不包装成 ErrFooterCorrupt：文件不存在 ≠ 产物损坏（调用方要能分开判）。
		return 0, fmt.Errorf("tracesealer: 打开产物 %s 失败: %w", artifactPath, err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("tracesealer: stat 产物 %s 失败: %w", artifactPath, err)
	}
	size := fi.Size()
	if size < int64(FooterSize) {
		return 0, fmt.Errorf("%w: %s 仅 %d 字节（不足 footer %d）", ErrFooterCorrupt, artifactPath, size, FooterSize)
	}

	buf := make([]byte, FooterSize)
	off := size - int64(FooterSize)
	if _, err := f.ReadAt(buf, off); err != nil {
		return 0, fmt.Errorf("tracesealer: 读取产物 %s 末尾 %d 字节失败: %w", artifactPath, FooterSize, err)
	}
	return decodeRowCount(buf, size, artifactPath)
}

// decodeRowCount 是冻结包 decodeFooter 的**部分**镜像：只解出 row_count，并做那些
// 「不需要读帧区就能做」的字段自洽校验（长度 / magic / 版本 / 三个负数 / trailer 是否
// 紧邻 footer）。任何不自洽都返回 ErrFooterCorrupt。
func decodeRowCount(b []byte, fileSize int64, artifactPath string) (int, error) {
	if len(b) != FooterSize {
		return 0, fmt.Errorf("%w: footer 长度应为 %d，实际 %d（%s）", ErrFooterCorrupt, FooterSize, len(b), artifactPath)
	}
	if string(b[0:4]) != footerMagic {
		return 0, fmt.Errorf("%w: magic 不符，期望 %q（%s）", ErrFooterCorrupt, footerMagic, artifactPath)
	}
	if v := binary.BigEndian.Uint16(b[4:6]); v != footerFormatVersion {
		return 0, fmt.Errorf("%w: 未知 format_version %d（%s）", ErrFooterCorrupt, v, artifactPath)
	}

	trailerOffset := int64(binary.BigEndian.Uint64(b[6:14]))
	trailerLen := int32(binary.BigEndian.Uint32(b[14:18]))
	rowCount := int32(binary.BigEndian.Uint32(b[18:22]))

	if trailerOffset < 0 || trailerLen < 0 {
		return 0, fmt.Errorf("%w: 负的 trailer_offset=%d / trailer_len=%d（%s）",
			ErrFooterCorrupt, trailerOffset, trailerLen, artifactPath)
	}
	if rowCount < 0 {
		return 0, fmt.Errorf("%w: 负的 row_count=%d（%s）", ErrFooterCorrupt, rowCount, artifactPath)
	}
	if trailerOffset+int64(trailerLen) != fileSize-int64(FooterSize) {
		return 0, fmt.Errorf("%w: trailer 未紧邻 footer（offset=%d + len=%d ≠ size=%d − footer=%d）（%s）",
			ErrFooterCorrupt, trailerOffset, trailerLen, fileSize, FooterSize, artifactPath)
	}
	return int(rowCount), nil
}
