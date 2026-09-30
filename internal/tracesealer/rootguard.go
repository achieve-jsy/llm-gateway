package tracesealer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrRootUnavailable 根目录不存在/不是目录/不可访问。
// ErrRootIdentityMismatch 根目录还在，但背后的挂载已经不是启动时那一个。
//
// 【为什么必须分成两个哨兵】两者的运维动作完全不同（「挂载掉了，去 remount」对
// 「挂载换了，去查为什么换了盘/是不是有人动了 fstab」，以及「目录没了，去看是不是
// 被清理脚本删了」），而这个区分会进 ERROR 日志与看板；靠错误文本匹配是脆的。
// 两者都**不是**冻结包的哨兵：本包绝不新增或复用冻结包的读侧哨兵（草案 D50 同族）。
var (
	ErrRootUnavailable      = errors.New("tracesealer: 根目录不可用")
	ErrRootIdentityMismatch = errors.New("tracesealer: 根目录身份与启动基线不一致")
)

const mountInfoPath = "/proc/self/mountinfo"

// Identity 返回路径背后挂载的身份串："<挂载点>|<设备号>|<文件系统类型>"。
//
// 【为什么不能只看「目录在不在」】umount 之后挂载点目录**仍然存在**（它只是退化成
// 宿主根 FS 上的一个普通目录），于是 os.Stat 照旧成功 —— 「根目录还在」这个判断
// 完全失效，正文会被写进另一块盘。/proc/self/mountinfo 里那条挂载记录消失后，
// 覆盖该路径的最长前缀挂载点变成上一层，身份串必然变化，这个变化是**机器可判定**的。
// 网关侧对同一问题的分析见 internal/gateway/observability/spooler.go（D49），
// 两侧都用同一套思路、各自实现（不改那一侧是为了不使它的变异绑定失效）。
//
// 非 Linux 读不到 mountinfo ⇒ 退化为「设备号」，此时只能发现换设备。
func Identity(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return fallbackIdentity(abs)
	}
	best, bestLen := "", -1
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		halves := strings.SplitN(line, " - ", 2)
		if len(halves) != 2 {
			continue
		}
		left := strings.Fields(halves[0])
		right := strings.Fields(halves[1])
		if len(left) < 5 || len(right) < 1 {
			continue
		}
		mp := unescapeMountField(left[4])
		if !covers(mp, abs) {
			continue
		}
		if len(mp) > bestLen {
			// left[2] 是 major:minor，right[0] 是文件系统类型。
			bestLen = len(mp)
			best = mp + "|" + left[2] + "|" + right[0]
		}
	}
	if best == "" {
		return "", fmt.Errorf("mountinfo 里没有任何挂载覆盖 %s", abs)
	}
	return best, nil
}

// covers 判断挂载点 mp 是否覆盖路径 p（两者都必须是已 Clean 的绝对路径）。
//
// 【"/" 必须特判】根挂载点是唯一的以 "/" 结尾的挂载点；不特判就会走
// strings.HasPrefix(p, "//") 这条永远为假的分支，于是「路径其实就在根文件系统上」
// 这种**最危险**的情形反而取不到身份。
func covers(mp, p string) bool {
	if mp == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == mp || strings.HasPrefix(p, mp+"/")
}

// unescapeMountField 还原 mountinfo 的八进制转义（空格/Tab/换行/反斜杠）。
func unescapeMountField(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// fallbackIdentity 非 Linux 上的退化实现：只有设备号。
func fallbackIdentity(path string) (string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return "", err
	}
	return fmt.Sprintf("dev=%d", uint64(st.Dev)), nil
}

// RootGuard 守住「本轮还能不能封块」。
type RootGuard struct {
	root     string
	baseline string
	identity func(string) (string, error)
}

// NewRootGuard 建立启动基线。**本函数（以及本文件）没有任何建目录调用** —— 这是
// 刻意的：替运维「补一个」根目录，就等于把已经消失的挂载点在宿主根 FS 上重建，
// 正文随后写进那片没人会去看的空间（2026-09-24 事故的同一类）。
// 静态检查（p4-task-3-rootguard.py 的 CHECKS）会钉住这一点。
func NewRootGuard(root string, identity func(string) (string, error)) (*RootGuard, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("根目录必须是绝对路径，实际 %q（systemd 的工作目录不等于你以为的那个）", root)
	}
	if filepath.Clean(root) == "/" {
		return nil, fmt.Errorf("根目录不得是文件系统根 %q（这正是 `/` 被写满那一类事故）", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("%w: stat %s 失败: %w（本进程**不会**替你创建它）", ErrRootUnavailable, root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s 不是目录", ErrRootUnavailable, root)
	}
	if identity == nil {
		identity = Identity
	}
	id, err := identity(root)
	if err != nil {
		return nil, fmt.Errorf("%w: 取启动基线失败: %w", ErrRootIdentityMismatch, err)
	}
	return &RootGuard{root: root, baseline: id, identity: identity}, nil
}

// Check 每轮调用一次；任何失败都**不得**触发建目录、**不得**触发 Seal。
func (g *RootGuard) Check() error {
	info, err := os.Stat(g.root)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRootUnavailable, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s 不再是目录", ErrRootUnavailable, g.root)
	}
	id, err := g.identity(g.root)
	if err != nil {
		return fmt.Errorf("%w: 复检身份失败: %w", ErrRootIdentityMismatch, err)
	}
	if id != g.baseline {
		return fmt.Errorf("%w: 启动基线=%s 现在=%s", ErrRootIdentityMismatch, g.baseline, id)
	}
	return nil
}

// Baseline 供排障时对比（运维看到「启动基线 vs 现在」才知道是换盘还是被 umount）。
func (g *RootGuard) Baseline() string { return g.baseline }
