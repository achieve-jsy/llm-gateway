package tracesealer_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestRootGuardRejectsFilesystemRoot 根目录是 `/` 必须被拒。
// 【为什么这是第一条】2026-09-24 的事故就是往 `/` 上写；任何形式的「默认值兜底成 /」
// 都会把同一类事故带回来，所以这一条必须在构造期就响亮失败。
func TestRootGuardRejectsFilesystemRoot(t *testing.T) {
	_, err := tracesealer.NewRootGuard("/", tracesealer.Identity)
	require.ErrorContains(t, err, "文件系统根")
}

// TestRootGuardRejectsRelativePath 相对路径必须被拒：sealer 的工作目录由 systemd
// 决定（WorkingDirectory 默认 /），相对路径会把正文写到谁都没想到的地方。
func TestRootGuardRejectsRelativePath(t *testing.T) {
	_, err := tracesealer.NewRootGuard("datalog/trace-archive-v4", tracesealer.Identity)
	require.ErrorContains(t, err, "绝对路径")
}

// TestRootGuardNeverCreatesRoot 根目录不存在时**必须**失败，且**不得**把它创建出来。
//
// 【这条是本计划最重要的安全断言】一旦用建目录「修复」缺失的根目录，消失的挂载
// 就会被重建到宿主根文件系统上（umount 之后 mountpoint 目录仍然存在，只是变成了
// 宿主根 FS 上的一个普通目录），正文随后写进那片没人会去看的空间。
func TestRootGuardNeverCreatesRoot(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "trace-archive-v4")
	_, err := tracesealer.NewRootGuard(missing, tracesealer.Identity)
	require.ErrorIs(t, err, tracesealer.ErrRootUnavailable)
	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr), "护栏绝不能把根目录创建出来")
}

// TestRootGuardCheckDetectsIdentityChange 目录还在、但背后的挂载换了 ⇒ 必须报
// 身份不符（这就是「撤掉挂载」的机器可判定形态：umount 后 mountinfo 里那条记录没了，
// 最长前缀覆盖退化成上一层挂载 ⇒ 身份串必然变化）。
func TestRootGuardCheckDetectsIdentityChange(t *testing.T) {
	root := t.TempDir()
	cur := "baseline-identity"
	g, err := tracesealer.NewRootGuard(root, func(string) (string, error) { return cur, nil })
	require.NoError(t, err)
	require.NoError(t, g.Check(), "身份未变时必须是绿的")
	cur = "/datalog|8:1|ext4" // 模拟：挂载点被撤掉后身份退化成宿主根 FS
	err = g.Check()
	require.ErrorIs(t, err, tracesealer.ErrRootIdentityMismatch)
	require.ErrorContains(t, err, "启动基线")
}

// TestRootGuardCheckReportsVanishedRoot 运行中根目录整个消失 ⇒ 报「不可用」。
func TestRootGuardCheckReportsVanishedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "r")
	require.NoError(t, os.Mkdir(root, 0o755))
	g, err := tracesealer.NewRootGuard(root, func(string) (string, error) { return "x", nil })
	require.NoError(t, err)
	require.NoError(t, os.Remove(root))
	require.ErrorIs(t, g.Check(), tracesealer.ErrRootUnavailable)
}

// TestRootGuardBaselineIsNonEmpty 基线必须可抄进日志（排障时要能对比前后值）。
func TestRootGuardBaselineIsNonEmpty(t *testing.T) {
	g, err := tracesealer.NewRootGuard(t.TempDir(), func(string) (string, error) { return "dev=42", nil })
	require.NoError(t, err)
	require.Equal(t, "dev=42", g.Baseline())
}

// TestRootGuardRejectsWhenBaselineCannotBeTaken 取不到启动基线（mountinfo 读不了、
// 路径 stat 不了）⇒ **构造期必须响亮失败**，绝不能拿一个空基线继续跑。
//
// 【为什么这条必须有】空基线与「任何身份」都不相等 ⇒ 之后每一轮 Check 都会报
// 「身份不符」，而运维看到的是一条持续的、指向错误方向的告警（挂载看起来没问题）。
// 唯一的正确形态是启动期就失败，并说明是「取基线失败」而不是「身份变了」。
func TestRootGuardRejectsWhenBaselineCannotBeTaken(t *testing.T) {
	_, err := tracesealer.NewRootGuard(t.TempDir(), func(string) (string, error) {
		return "", errors.New("mountinfo 读不了：permission denied")
	})
	require.ErrorIs(t, err, tracesealer.ErrRootIdentityMismatch)
	require.ErrorContains(t, err, "取启动基线失败")
}
