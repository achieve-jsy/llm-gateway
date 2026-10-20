// Package traceverify 是**验收工具**的支撑库（计划 4 / 任务 8）：
// 破坏性操作的目标护栏、容量预检算术、峰值常驻内存（VmHWM）解析与采样、解码标定。
//
// 它不进任何镜像、不参与生产上线（`make build` 只编 gateway / control-plane /
// trace-sealer）；`cmd/trace-verify` 是它唯一的调用方，而 `cmd/trace-verify` 只在
// 测试环境/A3 宿主上人工执行。
package traceverify

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// 尺寸单位（1024 进制）。
const (
	KiB int64 = 1 << 10
	MiB int64 = 1 << 20
	GiB int64 = 1 << 30
	TiB int64 = 1 << 40
)

// ErrScratchTargetUnsafe 是「破坏性操作的目标不安全」的哨兵错误：调用方
// （`cmd/trace-verify`）用它给出非零退出码，任何拒绝都必须包住它（errors.Is 可判）。
var ErrScratchTargetUnsafe = errors.New("traceverify: 破坏性操作的目标不安全")

// GuardScratchTarget 拒绝任何「落在文件系统根」或「与 forbidden 之一同一个设备」的目标，
// 并要求调用方显式确认（confirm）。stat 可注入（设备号），便于用例在 darwin 上造设备号。
//
// 【调用时机是契约，不是建议】`scratch-prepare` 必须在 `mount -o loop` **成功之后**、
// 任何真正破坏性动作（往回环 FS 里填盘 / scratch-cleanup）**之前**调用它：
// 挂载前 `/srv/trace-a3` 只是宿主根 FS 上的一个普通目录（与 `/` 同设备）⇒ 校验必然误拒，
// 而它看到的也就不是回环 ext4 的设备号。次序由 `cmd/trace-verify/main.go` 的调用位置与
// `tests/deploy` 的文本次序用例共同钉住。
//
// 【保守拒绝】拿不到设备号（stat 报错 / 回调为 nil）时**拒绝**：这一步之后紧跟的是
// 「把一块盘写满」，不能证明安全就必须停下来。
func GuardScratchTarget(target string, confirm bool, stat func(string) (dev uint64, err error), forbidden ...string) error {
	if !confirm {
		return fmt.Errorf("%w: 目标 %q 缺少显式确认（--confirm）⇒ 连准备动作都不做",
			ErrScratchTargetUnsafe, target)
	}
	if filepath.Clean(target) == "/" {
		return fmt.Errorf("%w: 目标是文件系统根 %q（2026-09-24 的事故形态：`/` 被写满）",
			ErrScratchTargetUnsafe, target)
	}
	if stat == nil {
		return fmt.Errorf("%w: 目标 %q 没有可用的 stat 回调 ⇒ 无法证明它与 / 和 /datalog 不同设备",
			ErrScratchTargetUnsafe, target)
	}
	dev, err := stat(target)
	if err != nil {
		return fmt.Errorf("%w: 目标 %q 的设备号读不到（%v）⇒ 无法证明它不与 / 和 /datalog 同设备",
			ErrScratchTargetUnsafe, target, err)
	}
	for _, f := range forbidden {
		fd, err := stat(f)
		if err != nil {
			return fmt.Errorf("%w: forbidden %q 的设备号读不到（%v）⇒ 无法证明 %q 与它不同设备",
				ErrScratchTargetUnsafe, f, err, target)
		}
		if dev == fd {
			return fmt.Errorf("%w: 目标 %q 与 %q 在同一个文件系统（设备号 %d）⇒ 拒绝在它上面做破坏性操作",
				ErrScratchTargetUnsafe, target, f, dev)
		}
	}
	return nil
}

// ParseSize 把 "1G" / "980M" / "512" 解析成 bytes：后缀 K/M/G/T（大小写不敏感，
// 1024 进制），无后缀即 bytes。空串、非十进制、非正数、未知后缀、溢出一律报错
// （拒绝静默取 0 —— 0 会让容量预检形同虚设）。
func ParseSize(s string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, errors.New("traceverify: 尺寸为空")
	}
	mult := int64(1)
	switch last := raw[len(raw)-1]; last {
	case 'K', 'k':
		mult, raw = KiB, raw[:len(raw)-1]
	case 'M', 'm':
		mult, raw = MiB, raw[:len(raw)-1]
	case 'G', 'g':
		mult, raw = GiB, raw[:len(raw)-1]
	case 'T', 't':
		mult, raw = TiB, raw[:len(raw)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("traceverify: 尺寸 %q 不是十进制整数（可带 K/M/G/T 后缀）: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("traceverify: 尺寸 %q 必须为正数", s)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if n > maxInt64/mult {
		return 0, fmt.Errorf("traceverify: 尺寸 %q 溢出 int64", s)
	}
	return n * mult, nil
}

// ScratchCapacityMarginBytes 是镜像所需的额外余量（1 GiB）。
//
// 【风险登记 R2】`GuardScratchTarget` 只护**挂载点**，镜像文件本身（约 1 GiB）落在
// `/srv` 所在文件系统上、**不受**设备护栏保护 ⇒ 容量预检是它唯一的前置防线。
const ScratchCapacityMarginBytes int64 = 1 << 30

// ScratchCapacitySufficient 判断镜像所在文件系统的可用空间是否满足
// 「镜像大小 + 1 GiB 余量」（R2）。可用空间或镜像大小 ≤ 0 时一律判为不满足。
func ScratchCapacitySufficient(availableBytes, imageBytes int64) bool {
	if availableBytes <= 0 || imageBytes <= 0 {
		return false
	}
	return availableBytes >= imageBytes+ScratchCapacityMarginBytes
}
