// 本文件是 B 层测试（tests/deploy，package deploy_test）：**只读仓库里的运行手册**
// （docs/deployment.md 的「调用链正文（/datalog）运行手册」一节），用纯文本断言钉住
// 「前置条件 / 撤掉挂载 ≤1 tick 闩锁 / 两种降级不对称 / 换盘必须重启 / 关停 6s /
// 功能关闭逐字节相同 / 保留与空间预算 / sealer 心跳与永久失败」的逐条契约。
// 不启动 docker、不连生产、不触碰未导出符号。
//
// 契约（计划 4 / 任务 11 步骤 1 的逐条注释）：
//
//	前置条件   `TRACE_BODY_ROOT` 非空 **且** root 目录在场（spooler.go 的 rootExists 是
//	           **构造期**要求）；功能关闭时 sink == nil、TraceSpooler 不构造 ⇒ 期望的
//	           ERROR 永不出现、观察断言**恒真**；本项的位置是「读侧就绪（D73）→ 启用
//	           写侧 → 立即执行」的门禁
//	闩锁       撤掉挂载后 ≤1 个 tick（= `TRACE_BODY_FLUSH_INTERVAL`，生产 1m）内闩锁、
//	           不再写盘、正文降级 body_missing=true；给出可粘贴的观察命令（日志正则 +
//	           文件系统断言）；停止点「禁止切换蓝绿流量」是**丢正文防护**；「关掉开关让
//	           正文回落 jsonb」只是**补救动作**；两种降级**不对称**；实验只允许在测试
//	           环境或无真实流量的生产窗口执行，并留证「累计丢失行数」
//	换盘       dev/fstype 变化 ⇒ 身份基线**永久**不匹配 ⇒ 一直闩锁到重启；处置 = 蓝绿切
//	           对端颜色 / 重启容器；**必须重启后才能恢复写盘**
//	关停       6s（traceSpoolCloseWait）内没排空 ⇒ 该批降级 body_missing=true + 一条 ERROR
//	功能关闭   与新部署前**逐字节相同**；SQL：body_ref_part IS NULL AND body_missing = false
//	           的行数应等于总行数，且正文出现在 jsonb 列
//	保留       minAge 3h × 写入速率 + 产物 + 余量；给出 /datalog 的余量检查命令（引用
//	           2026-10-08 的生产基线时必须标注日期）
//	sealer     心跳缺失（>3×poll 无「轮次结束」）⇒ 告警；too_many_rows ⇒ 人工介入（拆块 /
//	           调小 BLOCK_MB 并重新标定）；隔离件（spool 目录里的明文 .jsonl.zst）如何处置
//	D71        已被裁定的无效键（封块间隔键）不得出现在手册里
//
// 【为什么只用文本断言】手册是「部署产物即契约」：它就活在 git 仓库的 `docs/` 里，
// 运维照着它操作。读不到文件即 FAIL（readRepoFile 用 require.NoError），绝不 SKIP。
//
// 本文件的每条断言都由变异集合 `tools/trace-mutate/sets/p4-task-11-runbook.py` 的
// m01–m07 或一次「改坏→转红→还原」探针承载（见交付报告）。
package deploy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// runbookDocFile 是任务 11 的交付物：`docs/deployment.md` 里的运行手册一节。
const runbookDocFile = "docs/deployment.md"

// runbookSection 截出「调用链正文（/datalog）运行手册」一节的正文。
//
// 【为什么截一节而不是对整份文档断言】整份 `docs/deployment.md` 还有 CI/CD、蓝绿、
// 容器、nginx 等章节；只有本节的文字才承载本任务的契约。截法用「标题到下一个同级标题」
// 的区间；截不到（节被删）⇒ FAIL。
func runbookSection(t *testing.T) string {
	t.Helper()
	doc := readRepoFile(t, runbookDocFile)
	const heading = "## 调用链正文（`/datalog`）运行手册"
	i := strings.Index(doc, heading)
	require.GreaterOrEqual(t, i, 0,
		"%s：必须存在「调用链正文（`/datalog`）运行手册」一节（任务 11 步骤 3 的交付物）", runbookDocFile)
	rest := doc[i:]
	j := strings.Index(rest[len(heading):], "\n## ")
	if j < 0 {
		return rest
	}
	return rest[:len(heading)+j]
}

// TestRunbookDocumentsMountLossPrecondition 前置条件：`TRACE_BODY_ROOT` 非空**且**
// root 目录在场；并写明功能关闭时 sink == nil、TraceSpooler 不构造 ⇒ 观察断言恒真，
// 以及它的位置是「读侧就绪（D73）→ 启用写侧 → 立即执行」的门禁。
func TestRunbookDocumentsMountLossPrecondition(t *testing.T) {
	sec := runbookSection(t)

	// ① 写侧已启用的两个条件（缺一不可）。
	require.Contains(t, sec, "`TRACE_BODY_ROOT` **非空**",
		"%s：前置条件必须写「`TRACE_BODY_ROOT` 非空」", runbookDocFile)
	require.Contains(t, sec, "已取消注释",
		"%s：前置条件必须写明 compose 里那行已取消注释", runbookDocFile)
	require.Contains(t, sec, "rootExists",
		"%s：前置条件必须写明 root 目录在场（构造期的 rootExists）", runbookDocFile)
	require.Contains(t, sec, "构造期",
		"%s：前置条件必须写明 rootExists 是**构造期**要求", runbookDocFile)

	// ② 功能关闭时的空过（恒真）形态：sink == nil ⇒ TraceSpooler 不构造 ⇒ ERROR 永不出现。
	require.Contains(t, sec, "sink",
		"%s：前置条件必须写明功能关闭时 sink 是 nil", runbookDocFile)
	require.Contains(t, sec, "TraceSpooler",
		"%s：前置条件必须写明功能关闭时 TraceSpooler 不构造", runbookDocFile)
	require.Contains(t, sec, "恒真",
		"%s：前置条件必须写明此时观察断言恒真（什么都没证明）", runbookDocFile)
	require.Contains(t, sec, "root_unavailable",
		"%s：前置条件必须点名期望的 ERROR（root_unavailable / root-identity-mismatch）", runbookDocFile)

	// ③ 位置：读侧就绪（D73）→ 启用写侧 → 立即执行的门禁。
	require.Contains(t, sec, "读侧就绪",
		"%s：必须写明本项的位置是「读侧就绪 → 启用写侧」之后", runbookDocFile)
	require.Contains(t, sec, "D73",
		"%s：必须点名 D73 硬门", runbookDocFile)
	require.Contains(t, sec, "立即执行",
		"%s：必须写明这是「启用写侧后立即执行」的门禁", runbookDocFile)
}

// TestRunbookDocumentsMountLossLatchesWithinOneTick 撤掉挂载后 ≤1 个 tick 闩锁；
// 观察命令可粘贴（日志正则 + 文件系统断言）；停止点是丢正文防护；两种降级不对称；
// 「关掉开关」只是补救动作；实验只在测试环境/无真实流量窗口做并留证丢失行数。
func TestRunbookDocumentsMountLossLatchesWithinOneTick(t *testing.T) {
	sec := runbookSection(t)

	// ① 判定：≤1 个 tick = ≤ TRACE_BODY_FLUSH_INTERVAL（生产 1m），且不再写盘 + 降级。
	require.Contains(t, sec, "≤1 个 tick",
		"%s：必须写出「撤掉挂载后 ≤1 个 tick 内闩锁」", runbookDocFile)
	require.Contains(t, sec, "TRACE_BODY_FLUSH_INTERVAL",
		"%s：必须写明 tick 粒度 = TRACE_BODY_FLUSH_INTERVAL", runbookDocFile)
	require.Contains(t, sec, "不再写盘",
		"%s：必须写明闩锁后不再写盘", runbookDocFile)
	require.Contains(t, sec, "body_missing=true",
		"%s：必须写明此后正文降级为 body_missing=true", runbookDocFile)

	// ② 可粘贴的观察命令：日志正则 + 文件系统断言。
	require.Contains(t, sec, "grep -E 'trace-spool.*(root_unavailable|root-identity-mismatch)'",
		"%s：必须给出可粘贴的日志观察命令（root_unavailable / root-identity-mismatch 正则）", runbookDocFile)
	require.Contains(t, sec, "ls -l /datalog/trace-archive-v4/spool",
		"%s：必须给出可粘贴的文件系统断言（spool 目录不再出现新文件）", runbookDocFile)

	// ③ 停止点：禁止切换蓝绿流量 —— 这是丢正文防护（不是「仅降级」）。
	require.Contains(t, sec, "禁止切换蓝绿流量",
		"%s：必须写出停止点「验证窗口内禁止切换蓝绿流量」", runbookDocFile)
	require.Contains(t, sec, "丢正文防护",
		"%s：停止点必须写明它是丢正文防护（不是仅降级）", runbookDocFile)
	require.Contains(t, sec, "deploy-blue-green.sh",
		"%s：停止点必须点名禁止运行 deploy-blue-green.sh", runbookDocFile)

	// ④ 两种降级不对称 + 「关掉开关」只是补救动作。
	require.Contains(t, sec, "两种降级不对称",
		"%s：必须写出「两种降级不对称」", runbookDocFile)
	require.Contains(t, sec, "补救动作",
		"%s：必须写明「关掉写侧让正文回落 jsonb」只是事后补救动作", runbookDocFile)
	require.Contains(t, sec, "回退 jsonb",
		"%s：必须写明第二种降级既不进 /datalog 也不回退 jsonb ⇒ 丢正文", runbookDocFile)

	// ⑤ 执行环境：只允许测试环境或无真实流量的生产窗口，且留证。
	require.Contains(t, sec, "测试环境",
		"%s：必须写明本实验只允许在测试环境（或无真实流量的生产窗口）执行", runbookDocFile)
	require.Contains(t, sec, "留证",
		"%s：必须要求执行前留证「流量为零」", runbookDocFile)
	require.Contains(t, sec, "累计丢失行数",
		"%s：必须要求留证闩锁期间累计丢失行数", runbookDocFile)
	require.Contains(t, sec, "期望增量 = 0",
		"%s：必须写明丢失行数的期望增量为 0（已摘流）", runbookDocFile)
}

// TestRunbookDocumentsDiskSwapNeedsGatewayRestart 换盘（dev/fstype 变化）⇒ 身份基线
// 永久不匹配 ⇒ 一直闩锁到重启为止（已知形态，不是 bug）；处置 = 蓝绿切对端颜色或重启容器。
func TestRunbookDocumentsDiskSwapNeedsGatewayRestart(t *testing.T) {
	sec := runbookSection(t)

	require.Contains(t, sec, "fstype",
		"%s：必须写明换盘/改挂载点后 dev/fstype 变化", runbookDocFile)
	require.Contains(t, sec, "身份基线",
		"%s：必须写明身份基线是启动时取的", runbookDocFile)
	require.Contains(t, sec, "永久",
		"%s：必须写明换盘后身份与基线**永久**不匹配", runbookDocFile)
	require.Contains(t, sec, "必须重启后才能恢复写盘",
		"%s：必须写明必须重启后才能恢复写盘（不能只 remount）", runbookDocFile)
	require.Contains(t, sec, "对端颜色",
		"%s：必须给出处置之一：蓝绿切到对端颜色（零宕机）", runbookDocFile)
	require.Contains(t, sec, "不是 bug",
		"%s：必须如实登记这是**已知**形态、不是 bug", runbookDocFile)
}

// TestRunbookDocumentsShutdownSixSecondBound 关停 6s（traceSpoolCloseWait）内没排空
// ⇒ 该批降级为 body_missing=true + 一条 ERROR；给出观察命令与处置。
func TestRunbookDocumentsShutdownSixSecondBound(t *testing.T) {
	sec := runbookSection(t)

	require.Contains(t, sec, "`traceSpoolCloseWait` = 6s",
		"%s：必须写明关停排空上限 = traceSpoolCloseWait = 6s", runbookDocFile)
	require.Contains(t, sec, "6s 内没排空",
		"%s：必须写明 6s 内没排空的处理", runbookDocFile)
	require.Contains(t, sec, "body_missing=true",
		"%s：必须写明未排空的那批降级为 body_missing=true", runbookDocFile)
	require.Contains(t, sec, "ERROR",
		"%s：必须写明同时打一条 ERROR", runbookDocFile)
	require.Contains(t, sec, "docker logs",
		"%s：必须给出可粘贴的关停观察命令", runbookDocFile)
	// 【可失败性】整节不得把 6s 这个读数写成 60s（m04 正是这么改的）。
	require.NotContains(t, sec, "60s",
		"%s：关停预算不得写成 60s（真值是 traceSpoolCloseWait = 6s）", runbookDocFile)
}

// TestRunbookDocumentsFeatureOffIsByteIdentical 功能关闭时路径与新部署前逐字节相同，
// 并给出 SQL：body_ref_part IS NULL AND body_missing = false 的行数应等于总行数、
// 且正文出现在 jsonb 列。
func TestRunbookDocumentsFeatureOffIsByteIdentical(t *testing.T) {
	sec := runbookSection(t)

	require.Contains(t, sec, "逐字节相同",
		"%s：必须写明功能关闭时与改动前逐字节相同", runbookDocFile)
	require.Contains(t, sec, "body_ref_part IS NULL AND body_missing = false",
		"%s：必须给出「body_ref_part IS NULL AND body_missing = false 的行数 = 总行数」的 SQL", runbookDocFile)
	require.Contains(t, sec, "total_rows",
		"%s：SQL 必须同时给出总行数（否则「等于总行数」不可自证）", runbookDocFile)
	require.Contains(t, sec, "jsonb",
		"%s：必须写明正文出现在 jsonb 列", runbookDocFile)
	require.Contains(t, sec, "body_ref_*` 四列全 NULL",
		"%s：必须写明四个定位键全 NULL", runbookDocFile)
}

// TestRunbookDocumentsRetentionAndSpaceBudget 保留口径（minAge 3h × 写入速率 + 产物 +
// 余量）与 /datalog 的余量检查命令；引用 2026-10-08 的生产基线时必须标注日期。
func TestRunbookDocumentsRetentionAndSpaceBudget(t *testing.T) {
	sec := runbookSection(t)

	require.Contains(t, sec, "minAge 3h",
		"%s：必须写出保留口径的起点 minAge 3h", runbookDocFile)
	require.Contains(t, sec, "写入速率",
		"%s：保留口径必须写成 minAge × 写入速率 + 产物 + 余量", runbookDocFile)
	require.Contains(t, sec, "df -h /datalog",
		"%s：必须给出 /datalog 的余量检查命令（df -h /datalog）", runbookDocFile)
	// 【日期纪律】2026-10-08 的生产基线（407G 已用 / 153G 可用）必须带日期一起出现。
	require.Contains(t, sec, "2026-10-08",
		"%s：引用生产基线时必须标注日期（2026-10-08）", runbookDocFile)
	require.Contains(t, sec, "407G 已用 / 153G 可用",
		"%s：必须原样抄录 2026-10-08 的基线读数（407G 已用 / 153G 可用）", runbookDocFile)
}

// TestRunbookDocumentsSealerHeartbeatAndPermanentFailure 心跳缺失（>3×poll 无「轮次结束」）
// ⇒ 告警；too_many_rows ⇒ 人工介入的具体动作（拆块 / 调小 BLOCK_MB 并重新标定）；
// 隔离件（spool 目录里的明文 .jsonl.zst）如何处置。
func TestRunbookDocumentsSealerHeartbeatAndPermanentFailure(t *testing.T) {
	sec := runbookSection(t)

	// ① 心跳：轮次结束日志 + >3×poll 的判据 + 可粘贴命令。
	require.Contains(t, sec, "trace-sealer round",
		"%s：必须点名 sealer 的轮次结束日志（trace-sealer round 心跳）", runbookDocFile)
	require.Contains(t, sec, "3×poll",
		"%s：必须写出心跳缺失判据 >3×poll", runbookDocFile)
	require.Contains(t, sec, "journalctl -u trace-sealer",
		"%s：必须给出可粘贴的心跳检查命令", runbookDocFile)
	require.Contains(t, sec, "告警",
		"%s：必须写明心跳缺失要告警", runbookDocFile)

	// ② too_many_rows ⇒ 人工介入。
	require.Contains(t, sec, "too_many_rows",
		"%s：必须写明 too_many_rows ⇒ 人工介入", runbookDocFile)
	require.Contains(t, sec, "TRACE_BODY_BLOCK_MB",
		"%s：人工动作必须含「调小 TRACE_BODY_BLOCK_MB 并重新标定」", runbookDocFile)
	require.Contains(t, sec, "重新标定",
		"%s：人工动作必须含重新标定", runbookDocFile)

	// ③ 隔离件：形态 + 读侧语义（ErrNotArchived）+ 处置。
	require.Contains(t, sec, "隔离件",
		"%s：必须写出隔离件的处置", runbookDocFile)
	require.Contains(t, sec, "part-NNNNNN.jsonl.zst",
		"%s：必须写明隔离件形态（spool 目录里的 part-NNNNNN.jsonl.zst）", runbookDocFile)
	require.Contains(t, sec, "明文",
		"%s：必须写明隔离件内容是明文 JSONL（后缀是骗人的）", runbookDocFile)
	require.Contains(t, sec, "ErrNotArchived",
		"%s：必须写明读侧对隔离件报 ErrNotArchived（响亮降级）", runbookDocFile)
	require.Contains(t, sec, "不得",
		"%s：必须写明对隔离件的禁止动作（不得当产物解码 / 不得原地删除）", runbookDocFile)
}

// TestRunbookNeverMentionsSealInterval D71：已被裁定的无效键（封块间隔键）不得出现在
// 手册里 —— 它既不报错也不生效，写进手册会让人以为它有用。
func TestRunbookNeverMentionsSealInterval(t *testing.T) {
	doc := readRepoFile(t, runbookDocFile)
	require.NotContains(t, doc, "TRACE_BODY_SEAL_INTERVAL",
		"%s：D71 的无效键 TRACE_BODY_SEAL_INTERVAL 不得出现在运行手册里", runbookDocFile)
	require.NotContains(t, doc, "SEAL_INTERVAL",
		"%s：D71 的无效键族（*SEAL_INTERVAL*）不得出现在运行手册里", runbookDocFile)
}
