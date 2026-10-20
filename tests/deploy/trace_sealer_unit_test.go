// 本文件是 B 层测试（tests/deploy，package deploy_test）：**只读仓库里的产物文件**
// （systemd unit / 运维脚本 / Makefile / .gitlab-ci.yml），全部用纯文本与权限位断言，
// 不需要触达任何未导出符号（docs/guardrails.md Q3）。
//
// 「产物即契约」：unit 的每条关键指令、脚本的默认 dry-run、Makefile 的目标名、CI 里的
// 编译与安装命令都被这些用例钉住 —— 改了产物就必须同步改测试，否则测试红。
//
// 三个 deploy 包测试文件共享下面几个辅助（同一个 package）：
//
//	repoRoot       从测试二进制的 CWD（= 本包目录）向上找 go.mod
//	readRepoFile   读仓库根下的相对路径文件；**读不到就失败**（不是跳过）
//	unitLines      把 unit 拆成「去行尾空白与续行反斜杠」的行
//	requireUnitLine 断言 unit 里存在**整行等于** want 的行
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// repoRoot 返回仓库根。`go test` 把测试二进制的 CWD 设在包目录，因此从那里向上找 go.mod。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上找不到 go.mod（仓库根）", dir)
		}
		dir = parent
	}
}

// readRepoFile 读仓库根下的相对路径文件。读不到 ⇒ 用例失败（产物缺失必须红，不能跳过）。
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "读不到产物文件 %s：它必须存在（缺失 ⇒ 失败，不是跳过）", rel)
	return string(b)
}

// unitLines 把 unit 拆成「去行尾空白与续行反斜杠、再 TrimSpace」的行。
// 这样既能断言单行指令（Type=simple / RequiresMountsFor=/datalog），也能断言
// ExecStart 续行块里的参数行（--root … / --min-age 3h …）。
func unitLines(unit string) []string {
	raw := strings.Split(unit, "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		ln = strings.TrimRight(ln, " \t\\")
		out = append(out, strings.TrimSpace(ln))
	}
	return out
}

// requireUnitLine 断言 unit 里存在**整行等于** want 的行。
// 用「整行相等」而不是子串：否则 `RequiresMountsFor=/datalog` → `/datalog/other`
// 这类「保留前缀」的改动会假绿（它断言的正是「挂载点就是这个值」）。
func requireUnitLine(t *testing.T, unit, want string) {
	t.Helper()
	for _, ln := range unitLines(unit) {
		if ln == want {
			return
		}
	}
	t.Fatalf("unit 里缺少整行 %q", want)
}

// denyUnitLine 断言 unit 里**没有**整行等于 want 的行（与 requireUnitLine 对称）。
// 不能用 NotContains：`ReadWritePaths=/datalog` 天然包含 `ReadWritePaths=/`，
// 子串断言会把正确配置误判成违规。
func denyUnitLine(t *testing.T, unit, want string) {
	t.Helper()
	for _, ln := range unitLines(unit) {
		if ln == want {
			t.Fatalf("unit 里不该出现整行 %q", want)
		}
	}
}

// TestSealerUnitIsResidentLoopWithRestart 形态与重启策略：常驻循环（Type=simple）+
// Restart=on-failure。oneshot+timer 形态会让 Restart=on-failure 失去意义，而且
// 「timer 被 mask 了」只能靠 systemctl list-timers 才发现。
func TestSealerUnitIsResidentLoopWithRestart(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "Type=simple")
	requireUnitLine(t, u, "ExecStart=/usr/local/bin/trace-sealer")
	requireUnitLine(t, u, "Restart=on-failure")
	requireUnitLine(t, u, "RestartSec=30s")
	require.NotContains(t, u, "Type=oneshot",
		"选了常驻形态；oneshot+timer 会让 Restart=on-failure 失去意义")
	require.NotContains(t, u, "Restart=always",
		"锁冲突（人为再起一个 --once）会被 always 放大成重启风暴")
	denyUnitLine(t, u, "Type=oneshot")
	denyUnitLine(t, u, "Restart=always")
}

// TestSealerUnitPinsZstdBinaryLevelAndVersion 编码器参数逐字锁定：换 zstd 版本必须
// 重新标定压缩比（10.26× 是 v1.4.8 专属），所以版本与绝对路径都是承重配置，
// 不许走 PATH 漂移。
func TestSealerUnitPinsZstdBinaryLevelAndVersion(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "--zstd-binary /usr/bin/zstd")
	requireUnitLine(t, u, "--zstd-level 19")
	requireUnitLine(t, u, "--zstd-window-mb 512")
	requireUnitLine(t, u, "--expected-zstd-version 1.4.8")
	require.NotContains(t, u, "--zstd-binary zstd",
		"不许走 PATH：宿主机 PATH 漂移会让版本探测失效")
}

// TestSealerUnitPinsMinAgeAboveGatewayRollBound R8 在 unit 层的落点：min-age 取 3h
// （网关滚动上界 1h1m 的 3 倍）；进程里还有一道强制下界（>1h），这里只是把
// 「被静态改小」这条路径也堵死。
func TestSealerUnitPinsMinAgeAboveGatewayRollBound(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "--min-age 3h")
	require.NotContains(t, u, "--min-age 30m")
	require.NotContains(t, u, "--min-age 1h")
}

// TestSealerUnitKnowsItsRootAndLockFile 根目录与锁文件：锁必须落在同一个 root 里
// （root 之外没有可写保证 —— unit 只把 /datalog 打开为可写）。
func TestSealerUnitKnowsItsRootAndLockFile(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "--root /datalog/trace-archive-v4")
	requireUnitLine(t, u, "--lock-file /datalog/trace-archive-v4/.sealer.lock")
	require.NotContains(t, u, "--lock-file /tmp/",
		"锁落在 /tmp 会在 PrivateTmp=true 下失去跨进程语义")
}

// TestSealerUnitRequiresDatalogMount 挂载依赖（纵深防御）：挂载不在场就不启动，
// 被 umount 时本单元随之下线。主防线仍是进程内的根目录身份护栏。
func TestSealerUnitRequiresDatalogMount(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "RequiresMountsFor=/datalog")
	requireUnitLine(t, u, "After=local-fs.target")
}

// TestSealerUnitConfinesWritesToDatalog 可写面收窄（承重）：除 /datalog 之外整个文件
// 系统只读 —— 这条把「因为建目录/写日志把宿主根文件系统写满」这类事故直接堵死。
func TestSealerUnitConfinesWritesToDatalog(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "ProtectSystem=strict")
	requireUnitLine(t, u, "ReadWritePaths=/datalog")
	requireUnitLine(t, u, "ProtectHome=true")
	requireUnitLine(t, u, "PrivateTmp=true")
	requireUnitLine(t, u, "NoNewPrivileges=true")
	requireUnitLine(t, u, "RestrictSUIDSGID=true")
	denyUnitLine(t, u, "ProtectSystem=no")
	denyUnitLine(t, u, "ProtectSystem=full")
	denyUnitLine(t, u, "ReadWritePaths=/")
	denyUnitLine(t, u, "ReadWritePaths=/datalog/")
}

// TestSealerUnitLimitsMemoryCpuAndIO 资源限额（规格 §11.2 V1）：编码器内存落在本机，
// 不再进网关的 2 GiB；权重减半是因为 sealer 有 3h 的余量，不该和网关抢 IO/CPU。
func TestSealerUnitLimitsMemoryCpuAndIO(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "MemoryMax=2G")
	requireUnitLine(t, u, "MemoryHigh=1500M")
	requireUnitLine(t, u, "CPUWeight=50")
	requireUnitLine(t, u, "IOWeight=50")
	requireUnitLine(t, u, "IOSchedulingClass=best-effort")
	requireUnitLine(t, u, "IOSchedulingPriority=6")
	requireUnitLine(t, u, "Nice=10")
	requireUnitLine(t, u, "LimitNOFILE=4096")
}

// TestSealerUnitLogsToJournal 日志全部进 journald，且标识固定 —— 告警规则按
// SyslogIdentifier 匹配，改名等于把告警静音。
func TestSealerUnitLogsToJournal(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	requireUnitLine(t, u, "StandardOutput=journal")
	requireUnitLine(t, u, "StandardError=journal")
	requireUnitLine(t, u, "SyslogIdentifier=trace-sealer")
}

// TestSealerUnitNeverUsesSealIntervalKey D64/D71：规格 §11.1 的
// TRACE_BODY_SEAL_INTERVAL **不是有效键名**（同一概念在网关侧叫
// TRACE_BODY_ROLL_INTERVAL，生产取 1h），sealer 也没有 --seal-interval ——
// 它绝不能出现在 unit 里冒充有效配置（假配置比缺失配置更危险：运维以为改了就生效）。
func TestSealerUnitNeverUsesSealIntervalKey(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	require.NotContains(t, u, "SEAL_INTERVAL")
	require.NotContains(t, u, "--seal-interval")
}

// TestSealerUnitNeverPreparesFilesystem 启动前不得有任何「先建目录/先写文件」的形态
// （ExecStartPre 一类）：那正是把消失的挂载点重建到**宿主根文件系统**上的入口
// （2026-09-24 `/` 被写满的同族事故）。sealer 用 --root 而不是环境变量，因此
// TRACE_BODY_ROOT 也不该出现在 unit 里（那是网关侧的接口名）。
func TestSealerUnitNeverPreparesFilesystem(t *testing.T) {
	u := readRepoFile(t, "deploy/systemd/trace-sealer.service")
	require.NotContains(t, u, "ExecStartPre")
	require.NotContains(t, u, "ExecStartPost")
	require.NotContains(t, u, "TRACE_BODY_ROOT")
	require.NotContains(t, u, "mkdir")
}
