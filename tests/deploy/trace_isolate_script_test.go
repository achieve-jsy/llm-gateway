package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// script 读 deploy/systemd 下的运维脚本（只读文本，不执行 —— 这些脚本会动 systemd，
// 只能在生产机上、经人工确认点执行）。
func script(t *testing.T, name string) string {
	t.Helper()
	return readRepoFile(t, "deploy/systemd/"+name)
}

// indexOf 返回 needle 第一次出现的字节下标；找不到 ⇒ 失败。
// 用它断言「次序」：dry-run 闸门必须在任何变更命令**之前**、删除谓词审查必须在
// mask **之前** —— 次序本身就是这几条规则的承重部分。
func indexOf(t *testing.T, hay, needle string) int {
	t.Helper()
	i := strings.Index(hay, needle)
	require.GreaterOrEqual(t, i, 0, "脚本里找不到 %q", needle)
	return i
}

// TestIsolateScriptDefaultIsDryRun 脚本默认必须**不动手**：dry-run 闸门
//（`[ "$CONFIRM" != "1" ]`）必须在任何 systemctl 变更命令之前 —— 默认执行就等于
// 一次误敲命令把生产旧归档停掉。
func TestIsolateScriptDefaultIsDryRun(t *testing.T) {
	s := script(t, "isolate-old-trace-archive.sh")
	gate := `[ "$CONFIRM" != "1" ]`
	require.Contains(t, s, gate, "默认 dry-run 的闸门必须在")
	require.Contains(t, s, `[ "${1:-}" = "--confirm" ]`, "只有显式 --confirm 才置 1")
	require.Contains(t, s, "CONFIRM=1")
	require.Contains(t, s, "exit 0", "dry-run 分支必须以 0 退出（不是 1/3）")
	for _, mut := range []string{
		`systemctl disable --now "$OLD_TIMER"`,
		`systemctl mask "$OLD_TIMER"`,
	} {
		require.Contains(t, s, mut)
		require.Less(t, indexOf(t, s, gate), indexOf(t, s, mut),
			"dry-run 闸门必须出现在 %q 之前", mut)
	}
}

// TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot 删除谓词审查必须存在，
// 且命中新树（或 /datalog 顶层）时以 exit 3 直接 STOP —— 这是 D65 的机器可判定部分：
// 旧脚本的 `find <根> -mtime +5 -delete` 一旦落在 /datalog 上，会删掉我们的新树。
func TestIsolateScriptRefusesWhenDeletionPredicateTouchesNewRoot(t *testing.T) {
	s := script(t, "isolate-old-trace-archive.sh")
	require.Contains(t, s, "HITS=", "必须先收集删除类语句（留证 + 判定）")
	require.Contains(t, s, `rm[[:space:]]+-[[:alnum:]]*[rf]`, "rm -rf/-f 形态必须在审查范围内")
	require.Contains(t, s, `find[[:space:]].*-delete`, "find … -delete 形态必须在审查范围内")
	require.Contains(t, s, `rsync[[:space:]].*--delete`, "rsync --delete 形态必须在审查范围内")
	require.Contains(t, s, "grep -qE", "命中判定用 grep -qE")
	require.Contains(t, s, "$NEW_ROOT", "判定必须针对新树（$NEW_ROOT）")
	require.Contains(t, s, "STOP:")
	require.Contains(t, s, "exit 3", "命中新树 ⇒ exit 3（禁止上线）")
	require.Less(t, indexOf(t, s, "exit 3"), indexOf(t, s, `systemctl mask "$OLD_TIMER"`),
		"审查必须在 mask 之前：先核实再上线")
}

// TestIsolateScriptStopsAndMasksTimer 变更面只有两件事：`disable --now` + `mask`，
// 且必须复核 is-active 并为运维打印回滚命令（回滚不可省 —— mask 会把旧 timer 的
// 归档与保留/清理一起停掉）。
func TestIsolateScriptStopsAndMasksTimer(t *testing.T) {
	s := script(t, "isolate-old-trace-archive.sh")
	require.Contains(t, s, `systemctl disable --now "$OLD_TIMER"`)
	require.Contains(t, s, `systemctl mask "$OLD_TIMER"`)
	require.Contains(t, s, `systemctl is-active --quiet "$OLD_TIMER"`, "必须复核已停")
	require.Contains(t, s, `systemctl list-timers --all --no-pager | grep -F "$OLD_TIMER"`,
		"必须复核它确实从 list-timers 里消失")
	require.Contains(t, s, "restore-old-trace-archive.sh --confirm", "必须给出回滚命令")
	require.Contains(t, s, `echo "完成。回滚： bash restore-old-trace-archive.sh --confirm"`,
		"回滚命令必须由脚本在**运行时打印**出来 —— 只写在注释里等于现场没有回滚入口")
	require.NotContains(t, s, "systemctl unmask", "隔离脚本不该 unmask（那是回滚脚本的事）")
}

// TestRestoreScriptUnmasksAndEnables 回滚脚本必须 unmask → enable --now → 复核
// is-enabled / is-active / list-timers 又能看到它，并且同样是默认 dry-run。
func TestRestoreScriptUnmasksAndEnables(t *testing.T) {
	s := script(t, "restore-old-trace-archive.sh")
	gate := `[ "$CONFIRM" != "1" ]`
	require.Contains(t, s, gate, "回滚同样是默认 dry-run")
	require.Contains(t, s, `systemctl unmask "$OLD_TIMER"`)
	require.Contains(t, s, `systemctl enable --now "$OLD_TIMER"`)
	require.Contains(t, s, `systemctl is-enabled --quiet "$OLD_TIMER"`, "必须复核 is-enabled")
	require.Contains(t, s, `systemctl is-active --quiet "$OLD_TIMER"`, "必须复核 is-active")
	require.Contains(t, s, `systemctl list-timers --all --no-pager | grep -F "$OLD_TIMER"`,
		"判据是「重新出现在 list-timers 里」，不是「命令没报错」")
	require.Contains(t, s, "isolate-old-trace-archive.sh --confirm", "必须打印重新隔离的命令")
	require.NotContains(t, s, "systemctl mask", "回滚脚本不得再 mask")
	require.Less(t, indexOf(t, s, gate), indexOf(t, s, `systemctl unmask "$OLD_TIMER"`),
		"dry-run 闸门必须在 unmask 之前")
}

// TestIsolateAndRestoreScriptsAreExecutable 两个脚本必须带可执行位 —— 它们用
// `bash 脚本` 调用也能跑，但运维会顺手 `./isolate-…`，缺可执行位是部署期的坑；
// 权限位由 git 的 mode 承载，因此这里断言的是**仓库里**的文件模式。
func TestIsolateAndRestoreScriptsAreExecutable(t *testing.T) {
	for _, name := range []string{"isolate-old-trace-archive.sh", "restore-old-trace-archive.sh"} {
		st, err := os.Stat(filepath.Join(repoRoot(t), "deploy/systemd", name))
		require.NoError(t, err, "读不到 %s", name)
		require.NotZero(t, st.Mode().Perm()&0o111, "%s 必须带可执行位（仓库里已 chmod +x）", name)
	}
}
