package tracesealer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 【为什么这条用例必须在**包内**（计划 Q3 的「确需触达未导出符号」）】
//
// `covers` 是未导出函数，而它承载的是一条**行为要求**：「Identity 取最长前缀覆盖
// path 的挂载点，根挂载点 "/" 也必须覆盖其下路径」。这条要求无法经由公开 API 断言：
// 唯一的公开入口 `Identity(path)` 读的是**这台机器当前的** /proc/self/mountinfo ——
// 在 darwin 上根本不存在（退化成 fallbackIdentity，只剩设备号），在 Linux 上也没有
// 任何办法在测试里同时造出「宿主根挂载点」与「它下面的某个路径」这两个**确定**输入
// （mountinfo 是整机的、umount 要 root）。
//
// 因此「根挂载点覆盖其下路径」这条要求只有两个可能落点：① 静态检查（钉住
// `if mp == "/" {` 这行文字**存在**，只证明存在性）；② 包内直调 `covers`。按 **E62**
// （行为要求必须由在册行为测试承载；静态检查只承载存在/不存在）与 **E64①**
// （不得用「本平台不可达」为从未被触达的路径开脱）⇒ 必须在**包内**补这条行为用例。
// ---------------------------------------------------------------------------

// TestCoversRootMountCoversPathsUnderRoot 钉住 covers 的**根挂载点语义**：
// 根挂载点 "/" 必须覆盖它下面的每一条路径。
//
// 【它为什么值得单列一条】不特判根挂载点，`covers` 就会走到
// `strings.HasPrefix(p, "//")` 这条**恒假**分支 ⇒ 「路径其实就在宿主根文件系统上」
// 这种**最危险**的情形（背后没有任何单独挂载、正文直接写宿主根 FS）反而取不到身份。
//
// 【它是变异 m04（去掉根挂载点特判）的在册杀手】m04 把 `if mp == "/" {` 改成
// `if false {` ⇒ 本用例在下面第一条 require.True 处转红，工具表里 m04 的「杀死的
// 用例」列 = 本函数名。除根挂载点外，下面还钉住「非根挂载点按**路径分量**而不是
// 字符串前缀匹配」这一半 —— 它保证「最长前缀」的候选集不会把所有挂载点都算进来。
func TestCoversRootMountCoversPathsUnderRoot(t *testing.T) {
	require.True(t, covers("/", "/tmp/x"),
		"根挂载点必须覆盖其下路径（路径就在宿主根 FS 上 —— 最危险的那种情形）")
	require.True(t, covers("/", "/"), "根挂载点覆盖它自己")
	require.True(t, covers("/", "/var/lib/llm-gateway/datalog"), "根挂载点覆盖任意深度的绝对路径")

	require.True(t, covers("/a", "/a"), "非根挂载点覆盖它自己")
	require.True(t, covers("/a", "/a/b"), "非根挂载点覆盖它的子树")

	require.False(t, covers("/a", "/b"), "不相干路径不得被覆盖")
	require.False(t, covers("/a", "/ax"), "按路径分量匹配：/ax 不在 /a 之下")
	require.False(t, covers("/a", "/ab/c"), "按路径分量匹配：/ab/c 不在 /a 之下")
	require.False(t, covers("/datalog", "/datalog2/x"), "按路径分量匹配：/datalog2/x 不在 /datalog 之下")
}
