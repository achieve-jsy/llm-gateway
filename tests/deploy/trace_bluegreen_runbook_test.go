// 本文件是 B 层测试（tests/deploy，package deploy_test）：**只读仓库里的蓝绿剧本产物**
// （deploy/blue-green/README.md），用纯文本断言钉住「切换 / 回滚 / 功能降级 / 硬门 /
// 旧颜色处置 / 无效键」的逐条契约。不启动 docker、不连生产、不触碰未导出符号。
//
// 契约（计划 4 / 任务 7）：
//
//	切换     颜色↔inst 的静态对应；切换次序（起新 → /health+/ready → nginx 原子切流 →
//	         drain 旧 → 停旧）；抄录 .active-color；切换期间两个 inst 并存 ⇒ 读侧必须用
//	         行上的 body_ref_inst 定位（跨计划契约 H2 的落点），不得用配置里的 inst
//	回滚     `--rollback` 切回旧颜色；旧容器仍在（KEEP_OLD_ENV=true）+ 旧 inst 数据仍在；
//	         不需要重建数据；**sealer 必须继续运行**（未封 spool 必须封完，否则已写出的
//	         body_ref_* 会永久停在 spool 上）
//	降级     只关写侧开关 ⇒ 逐字节等价：body_ref_* 四列全 NULL + body_missing=false +
//	         正文在 jsonb 列；并给出可粘贴的 SQL 比对
//	硬门     同一 root 的读侧未就绪前不得取消注释写侧开关（D73，⛔ 停止点）
//	旧颜色   drain 期间旧颜色功能仍关闭 ⇒ 正文继续进 jsonb ⇒ 无数据损失；
//	         drain 后按新编排重建 ⇒ 两个颜色最终都按新路径写、inst 各自独立
//	D71      已被裁定的无效键（封块间隔键）不得出现在剧本里
//
// 【为什么只用文本断言】剧本是「部署产物即契约」（全局约束）：它随
// `scp -r deploy/blue-green/*` 落到生产宿主，运维在机器上读到的就是它。读不到文件
// 即 FAIL（readRepoFile 用 require.NoError），绝不 SKIP。
//
// 本文件的每条断言都由变异集合 `tools/trace-mutate/sets/p4-task-7-bluegreen.py` 的
// 五条变异或一次「改坏→转红→还原」探针承载（见交付报告）。
package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// runbookFile 是任务 7 的交付物：随 `scp -r deploy/blue-green/*` 落到宿主根目录的剧本
// （`deploy/blue-green/*` ⇒ 宿主 `${DEPLOY_DIR}/`，因此运维 `cat README.md` 即可读到）。
const runbookFile = "deploy/blue-green/README.md"

// TestBlueGreenRunbookCoversSwitch 切换剧本必须含：颜色↔inst 的静态对应说明、
// 「重启后新颜色接管 inst」、切换次序与 .active-color 的抄录点，以及
// 「切换期间两个 inst 并存 ⇒ 读侧必须用行上的 body_ref_inst 而不是配置里的 inst」
// （跨计划契约 H2 的落点 —— 用它定位才不会把旧 inst 的正文读成「找不到」）。
func TestBlueGreenRunbookCoversSwitch(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	// ① 颜色↔inst 的**静态**对应：blue 文件 ⇒ inst=blue；green 文件 ⇒ inst=green。
	// 写反会让两个颜色写进同一个 inst= 目录（part 序号与 spool 互踩）。
	require.Contains(t, doc,
		"blue 文件 ⇒ `TRACE_BODY_INST=blue`；green 文件 ⇒ `TRACE_BODY_INST=green`",
		"%s：必须写清颜色↔inst 的静态对应（blue 文件 ⇒ inst=blue、green 文件 ⇒ inst=green）", runbookFile)

	// ② 重启后新颜色接管的仍是它自己的 inst（颜色变了 inst 不漂移）。
	require.Contains(t, doc, "重启后新颜色接管的仍是它自己的 inst",
		"%s：必须写明「重启后新颜色接管的仍是它自己的 inst」", runbookFile)

	// ③ 切换的固定次序（脚本内部顺序；先起新、再停旧 ⇒ 零宕机）。
	require.Contains(t, doc,
		"启动新颜色 → `/health` + `/ready` → nginx 原子切流 → drain 旧颜色 → 停旧颜色",
		"%s：必须写出切换次序（起新 → /health+/ready → nginx 原子切流 → drain → 停旧）", runbookFile)

	// ④ 抄录切换前后的 `.active-color`（切换的现场记录点）。
	require.Contains(t, doc, "cat ${DEPLOY_DIR}/.active-color",
		"%s：必须要求抄录切换前后的 `.active-color`", runbookFile)

	// ⑤ H2 的落点（跨计划契约）：两个 inst 并存期间，读侧一律用行上的 body_ref_inst。
	require.Contains(t, doc,
		"切换期间两个 inst 并存 ⇒ 读侧必须用行上的 `body_ref_inst` 定位，\n"+
			"   绝不能用配置里的 `inst`（跨计划契约 H2 的落点）。",
		"%s：切换期间两个 inst 并存 ⇒ 读侧必须用行上的 body_ref_inst，不得用配置里的 inst（H2）", runbookFile)

	// ⑥ 禁止：两个颜色设同一个 `inst`。
	require.Contains(t, doc, "不得在两个颜色里设同一个 `inst`",
		"%s：必须禁止在两个颜色里设同一个 inst", runbookFile)
}

// TestBlueGreenRunbookCoversRollback 回滚必须含：nginx 切回旧颜色、旧 inst 的数据
// 仍可读、**sealer 不随功能回滚而停止**（未封 spool 必须继续被封，否则已写出的
// body_ref 会永久停在 spool 上）。
func TestBlueGreenRunbookCoversRollback(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	// ① 回滚命令：nginx 切回上一个颜色（要求对端仍在运行）。
	require.Contains(t, doc, "KEEP_OLD_ENV=true ./deploy-blue-green.sh --rollback",
		"%s：回滚必须给出 `KEEP_OLD_ENV=true ./deploy-blue-green.sh --rollback`", runbookFile)

	// ② 前提二：旧 inst 的数据仍在（同一 root，新颜色只写自己的 inst= 目录）。
	require.Contains(t, doc, "旧 inst 的数据仍在",
		"%s：回滚必须写明「旧 inst 的数据仍在」这一前提", runbookFile)

	// ③ 不需要重建任何数据：读侧按行上的 body_ref_inst 定位。
	require.Contains(t, doc, "不需要重建任何数据",
		"%s：回滚必须写明「不需要重建任何数据」（读侧按 body_ref_inst 定位）", runbookFile)

	// ④ sealer 不随功能回滚而停止 —— 本用例的承重断言。
	require.Contains(t, doc, "功能回滚**不**停止",
		"%s：功能回滚不得停止 sealer（未封 spool 必须继续被封）", runbookFile)
	require.Contains(t, doc, "未封的 spool 必须继续被封",
		"%s：必须写明未封的 spool 必须继续被封（否则 body_ref_* 永久停在 spool 上）", runbookFile)
}

// TestBlueGreenRunbookCoversFeatureOffByteIdentical 功能关闭时必须逐字节等价：
// body_ref_* 全 NULL + body_missing=false + 正文在 jsonb 列；给出可粘贴的 SQL 比对。
func TestBlueGreenRunbookCoversFeatureOffByteIdentical(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	// ① 逐字节等价的三要件（一句话写全，缺一不可）。
	require.Contains(t, doc,
		"`body_ref_*` 四列全 NULL、`body_missing=false`、正文在 jsonb 列",
		"%s：功能关闭必须逐字节等价（body_ref_* 全 NULL + body_missing=false + 正文在 jsonb 列）", runbookFile)

	// ② 可粘贴的 SQL 比对：三条查询各有一个具名别名（ref_rows / missing_rows / jsonb_rows），
	//    期望值都是 0。删掉 SQL 块 ⇒ 本断言必须红。
	require.Contains(t, doc, "SELECT count(*) AS ref_rows",
		"%s：必须给出可粘贴的 SQL 比对（①定位键全 NULL ⇒ ref_rows=0）", runbookFile)
	require.Contains(t, doc, "AS missing_rows",
		"%s：必须给出可粘贴的 SQL 比对（②body_missing IS TRUE ⇒ missing_rows=0）", runbookFile)
	require.Contains(t, doc, "AS jsonb_rows",
		"%s：必须给出可粘贴的 SQL 比对（③jsonb 列为 NULL ⇒ jsonb_rows=0）", runbookFile)
}

// TestBlueGreenRunbookHasReadSideHardGate 硬门：同一 root 的读侧未就绪前不得
// 取消注释写侧开关（D73）。三个条件缺一不可，且必须是「停止点」措辞。
func TestBlueGreenRunbookHasReadSideHardGate(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	// ⓪ D73 的门禁句 + 停止点标记。
	require.Contains(t, doc, "同一 root 的读侧可用之前，不得设置写侧开关",
		"%s：必须写出 D73 硬门（同一 root 的读侧可用之前不得设置写侧开关）", runbookFile)
	require.Contains(t, doc, "① 同一 root 的读侧已可用",
		"%s：硬门条件①缺失（读侧已落地且 nginx 后面的管理面能取到正文）", runbookFile)
	require.Contains(t, doc, "findmnt -T /datalog --output TARGET,SOURCE,FSTYPE",
		"%s：硬门条件②缺失（findmnt -T /datalog 必须正常）", runbookFile)
	require.Contains(t, doc, "≥ **5 天保留量**",
		"%s：硬门条件③缺失（/datalog 剩余空间 ≥ 5 天保留量）", runbookFile)
	require.Contains(t, doc, "`⛔` **停下来**",
		"%s：硬门必须以「⛔ 停下来」的停止点措辞收口（不得把未满足当成可继续）", runbookFile)
}

// TestBlueGreenRunbookCoversOldColorDrain 旧颜色在 drain 期间功能仍是关闭的，
// 正文继续进 jsonb ⇒ 无数据损失（这是「两个 inst 并存也安全」的另一半）；
// drain 后按新编排重建 ⇒ 两个颜色最终都按新路径写。
func TestBlueGreenRunbookCoversOldColorDrain(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	require.Contains(t, doc, "drain 期间旧颜色功能仍是关闭的",
		"%s：必须写明 drain 期间旧颜色功能仍是关闭的", runbookFile)
	require.Contains(t, doc, "正文继续进 jsonb",
		"%s：drain 期间正文必须继续进 jsonb（旧编排没有写侧开关）", runbookFile)
	require.Contains(t, doc, "无数据损失",
		"%s：drain 期间必须写明无数据损失", runbookFile)
	require.Contains(t, doc, "两个颜色最终都按新路径写",
		"%s：必须写明旧颜色下一次发版按新编排重建 ⇒ 两个颜色最终都按新路径写", runbookFile)
}

// TestBlueGreenRunbookNeverMentionsSealInterval D71 的无效键不得出现在剧本里：
// 同一个概念在网关侧叫 TRACE_BODY_ROLL_INTERVAL，出现在剧本里只会让人以为它有用
// （它既不报错也不生效）。
func TestBlueGreenRunbookNeverMentionsSealInterval(t *testing.T) {
	doc := readRepoFile(t, runbookFile)

	require.NotContains(t, doc, "TRACE_BODY_SEAL_INTERVAL",
		"%s：D71 —— 该键名无效（网关侧叫 TRACE_BODY_ROLL_INTERVAL），剧本里不得出现", runbookFile)
	require.NotContains(t, doc, "SEAL_INTERVAL",
		"%s：D71 —— 不得以任何形态出现封块间隔键（含前缀变体）", runbookFile)
}
