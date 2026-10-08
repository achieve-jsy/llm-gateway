// 本文件是 B 层测试（tests/traceverify，package traceverify_test）：**只用公开 API ＋
// 注入的 stat 回调**，不碰真实文件系统、不需要 root、不需要 loop 设备。
//
// 【为什么设备号要注入】A3 的安全边界是「真实盘满只允许发生在**专用回环 ext4** 上」，
// 它的机器可判定形态就是这条护栏。护栏的判据是**设备号比较** ⇒ 把它做成可注入的
// stat 回调，边界本身就能在 darwin 上被判红判绿（E64：不得用「本平台不可达」为
// 在册清单从未触达的路径开脱）。
//
// 契约（计划 4 / 任务 8 步骤 1 与 3）：
//
//	目标就是 `/`（任何等价写法）        ⇒ 拒（ErrScratchTargetUnsafe）
//	目标与 forbidden 之一同设备          ⇒ 拒；forbidden 里包含目标自己 ⇒ 同样拒
//	没有 confirm                        ⇒ 拒，且**连 stat 都不许调用**（连准备动作都不做）
//	设备号读不到（stat 失败 / 回调为 nil）⇒ 拒（「读不到」必须红，不许放行）
//	专用设备（与全部 forbidden 不同设备）＋ confirm ⇒ 通过
package traceverify_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/traceverify"
)

// devTable 是 stat 注入替身：把路径映射成设备号；**未登记**的路径返回错误
// （模拟真实世界里的「读不到设备号」），并把每次调用记进 calls。
//
// calls 用来钉一条**次序**契约：confirm 缺失时护栏必须在任何 stat 之前拒绝 ——
// 「连准备动作都不许做」的机器可判定形态。
type devTable struct {
	devs  map[string]uint64
	calls []string
}

func (d *devTable) stat(p string) (uint64, error) {
	d.calls = append(d.calls, p)
	dev, ok := d.devs[p]
	if !ok {
		return 0, fmt.Errorf("traceverify_test: 路径 %q 无设备号（模拟读不到）", p)
	}
	return dev, nil
}

// TestGuardRejectsFilesystemRoot 目标就是文件系统根 ⇒ 拒。
//
// forbidden 里**不含** `/`：本用例只测「根」这一条判据 —— 若 forbidden 含 `/`，
// 删掉根判断后设备号比较仍会拒绝，本用例就失去了对那条判据的判别力。
// 设备号故意取成与 forbidden 不同的值，使唯一能拒绝它的是「目标是 /」这一条。
func TestGuardRejectsFilesystemRoot(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/": 1, "/datalog": 2}}
	err := traceverify.GuardScratchTarget("/", true, d.stat, "/datalog")
	require.Error(t, err, "目标是文件系统根时必须拒绝（2026-09-24 的事故形态）")
	require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
}

// TestGuardRejectsSameDeviceAsForbidden 目标与 `/datalog` 同设备 ⇒ 拒。
// 这是 D66 的机器可判定形态：A3 要「真的把盘写满」，唯一可接受的目标是一块
// **专用**文件系统（回环 ext4）；与 `/` 或 `/datalog` 同设备 ⇒ 拒绝执行。
func TestGuardRejectsSameDeviceAsForbidden(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/srv/trace-a3": 7, "/": 1, "/datalog": 7}}
	err := traceverify.GuardScratchTarget("/srv/trace-a3", true, d.stat, "/", "/datalog")
	require.Error(t, err, "与 forbidden 同设备时必须拒绝（否则 A3 会填满 / 或 /datalog）")
	require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
}

// TestGuardRequiresExplicitConfirm 没有 confirm ⇒ 拒（连准备动作都不许做）。
//
// 两半都是承重的：① 必须报错；② **stat 一次都不许调用** —— 若护栏先 stat 再判
// confirm，那么「准备动作」已经开始了（真实实现里 stat 是挂载后复核的第一步，
// 但它同样是「已经开始处理这个目标」的证据）。
func TestGuardRequiresExplicitConfirm(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/srv/trace-a3": 7, "/": 1, "/datalog": 2}}
	err := traceverify.GuardScratchTarget("/srv/trace-a3", false, d.stat, "/", "/datalog")
	require.Error(t, err, "没有 --confirm 时必须拒绝")
	require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
	require.Empty(t, d.calls, "没有 confirm 时连 stat 都不许调用（拒绝必须发生在任何探测之前）")
}

// TestGuardAcceptsDedicatedFilesystem 专用文件系统（与全部 forbidden 不同设备）
// ＋ confirm ⇒ 通过。这是 A3 的正例：护栏不能把唯一合法的目标也拒掉。
func TestGuardAcceptsDedicatedFilesystem(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/srv/trace-a3": 7, "/": 1, "/datalog": 2}}
	err := traceverify.GuardScratchTarget("/srv/trace-a3", true, d.stat, "/", "/datalog")
	require.NoError(t, err, "回环 ext4（与 / 和 /datalog 都不同设备）+ confirm 必须通过")
}

// TestGuardRejectsTargetOnSameDeviceAsForbiddenSelf 若干 forbidden 里包含目标**自己**
// ⇒ 拒。这一条防的是「调用方把目标顺手写进 forbidden，于是自己给自己开绿灯」——
// forbidden 里出现目标本身，等价于声明「我不打算检查它」，必须拒绝而不是跳过。
func TestGuardRejectsTargetOnSameDeviceAsForbiddenSelf(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/srv/trace-a3": 7, "/": 1}}
	err := traceverify.GuardScratchTarget("/srv/trace-a3", true, d.stat, "/srv/trace-a3", "/")
	require.Error(t, err, "forbidden 里包含目标自己时必须拒绝，不得跳过该条")
	require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
}

// TestGuardRejectsWhenDeviceCannotBeRead 「读不到」必须红，不许放行 —— 三条路径都测：
// ① 目标的 stat 失败；② 某个 forbidden 的 stat 失败；③ stat 回调为 nil。
//
// 判据是**保守拒绝**：拿不到设备号就**无法证明**目标与 `/`、`/datalog` 不同设备，
// 而这一步之后紧跟的是「把一块盘写满」⇒ 不能证明安全就必须停下来。
func TestGuardRejectsWhenDeviceCannotBeRead(t *testing.T) {
	t.Run("目标读不到", func(t *testing.T) {
		d := &devTable{devs: map[string]uint64{"/": 1, "/datalog": 2}}
		err := traceverify.GuardScratchTarget("/srv/trace-a3", true, d.stat, "/", "/datalog")
		require.Error(t, err, "目标设备号读不到时必须拒绝")
		require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
	})

	t.Run("forbidden读不到", func(t *testing.T) {
		d := &devTable{devs: map[string]uint64{"/srv/trace-a3": 7, "/": 1}}
		err := traceverify.GuardScratchTarget("/srv/trace-a3", true, d.stat, "/", "/datalog")
		require.Error(t, err, "forbidden 的设备号读不到时无法证明不同设备 ⇒ 必须拒绝")
		require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
	})

	t.Run("stat回调为空", func(t *testing.T) {
		err := traceverify.GuardScratchTarget("/srv/trace-a3", true, nil, "/", "/datalog")
		require.Error(t, err, "没有 stat 回调就无从判定 ⇒ 必须拒绝")
		require.ErrorIs(t, err, traceverify.ErrScratchTargetUnsafe)
	})
}

// TestGuardUnsafeErrorIsSentinel 错误必须能被 errors.Is 识别（调用方据此给出退出码），
// 且拒绝消息必须指出目标（运维要能一眼看出是哪个路径被拒）。
func TestGuardUnsafeErrorIsSentinel(t *testing.T) {
	d := &devTable{devs: map[string]uint64{"/": 1}}
	err := traceverify.GuardScratchTarget("/", true, d.stat, "/datalog")
	require.True(t, errors.Is(err, traceverify.ErrScratchTargetUnsafe), "必须包住哨兵错误")
	require.Contains(t, err.Error(), "/", "拒绝消息里必须带上被拒的目标")
}
