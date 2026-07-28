package tracearchive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Ref 是正文行的定位键。
//
// 为什么用复合键而不是物理字节偏移（规格 §5.4）：
// 压缩后的字节偏移只有在封块结束那一刻才知道，若要在写入时就写进 PG，
// 就必须在封块后回填 UPDATE（每天约 9 万次）。这四个分量在写入追加时
// 即可确定，无需回填，且在冷层合并后依然有效（规格 §5.5）。
type Ref struct {
	DT   string // 归属天，如 "2026-09-24"
	Inst string // 实例标识，"blue" / "green"（蓝绿两实例并存，必须隔离）
	Part int    // 块序号
	Row  int    // 块内行序号，从 0 开始
}

// String 返回可读形式，便于日志与错误信息。
func (r Ref) String() string {
	return fmt.Sprintf("%s/%s/part-%06d#row=%d", r.DT, r.Inst, r.Part, r.Row)
}

// Paths 负责把定位键映射为文件系统路径。
type Paths struct{ Root string }

// NewPaths 构造路径解析器。
func NewPaths(root string) Paths { return Paths{Root: root} }

// PartDir 返回某天某实例的产物目录。
func (p Paths) PartDir(dt, inst string) string {
	return filepath.Join(p.Root, "dt="+dt, "inst="+inst)
}

// SpoolDir 返回某天某实例的未压缩暂存目录。
func (p Paths) SpoolDir(dt, inst string) string {
	return filepath.Join(p.Root, "spool", "dt="+dt, "inst="+inst)
}

// PartFile 返回定位键对应的产物文件路径。
func (p Paths) PartFile(ref Ref) string {
	return filepath.Join(p.PartDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl.zst"))
}

// SpoolFile 返回定位键对应的未压缩暂存文件路径。
// 未封块的近期正文从这里读，因此新增行不必等封块即可读回。
func (p Paths) SpoolFile(ref Ref) string {
	return filepath.Join(p.SpoolDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl"))
}

func partName(seq int, ext string) string {
	return fmt.Sprintf("part-%06d%s", seq, ext)
}

// parsePartSeq 从文件名反解块序号。写指针以目录内容为权威来源（规格 §7.3），
// 因此这里必须严格：带 .tmp 后缀、非 part- 前缀、位数不对的一律拒绝。
func parsePartSeq(name string) (int, bool) {
	if !strings.HasPrefix(name, "part-") {
		return 0, false
	}
	rest := strings.TrimPrefix(name, "part-")
	// 只允许 .jsonl 与 .jsonl.zst，其余（含 .tmp 后缀的半成品、大小写不符
	// 的 .ZST、.gz）一律拒绝——写指针绝不能被半成品冒名顶替。
	// digits 以 rest 起手（而非空串）：没被认可扩展名裁剪掉的 remainder
	// 一定不是数字段，default 是它唯一的护栏。
	digits := rest
	switch {
	case strings.HasSuffix(rest, ".jsonl.zst"):
		digits = strings.TrimSuffix(rest, ".jsonl.zst")
	case strings.HasSuffix(rest, ".jsonl"):
		digits = strings.TrimSuffix(rest, ".jsonl")
	default:
		return 0, false
	}
	if len(digits) != 6 {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
