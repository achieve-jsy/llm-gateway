// 本文件是 B 层测试（tests/deploy，package deploy_test）：**只读仓库里的 A3/A7 验证产物**
// —— `deploy/verify/trace-a3/docker-compose.yml` 与 `deploy/verify/trace-a3/run.sh`，
// 外加 `cmd/trace-verify/main.go` 的**次序文本**。用纯文本断言钉住契约，不启动 docker、
// 不引 YAML 库、不需要 root/回环设备（这些只在 A3 宿主上才有）。
//
// 契约（计划 4 / 任务 8 步骤 4）：
//
//	A3 编排：水位压到 99（**真实 ENOSPC 必须先于水位闩锁发生**）、连接串一律
//	         `${VAR:?…}`（必填、无默认值）、实例名/端口与生产及蓝绿完全错开、
//	         只挂专用回环目录（**绝不挂生产 `/datalog`**）
//	run.sh：`trap … EXIT` 清理必须在**第一个破坏性步骤之前**注册；护栏调用
//	         （`scratch-prepare … --confirm`，其内部的挂载后复核就是设备护栏）
//	         必须在**归档根复核 / 起实例 / 填盘**之前
//	工具源：护栏调用必须在 `mount -o loop` **之后**、建归档根**之前**
//	         （挂载前校验必然误拒；挂载前建归档根会在宿主根 FS 上造出目录 —— D70）
package deploy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	a3ComposeFile = "deploy/verify/trace-a3/docker-compose.yml"
	a3RunScript   = "deploy/verify/trace-a3/run.sh"
	a3VerifyTool  = "cmd/trace-verify/main.go"
	a3Service     = "gateway-a3"
)

// serviceFieldValue 从服务块里取某个**直接子键**（如 container_name / network_mode）的
// 取值 —— `envValue` 只看 environment: 段，而实例隔离性体现在块级键上。
// 键不存在 ⇒ ok=false（调用方必须把它当失败处理）。
func serviceFieldValue(t *testing.T, blk, key string) (string, bool) {
	t.Helper()
	prefix := key + ":"
	for _, ln := range strings.Split(blk, "\n") {
		if isCommentLine(ln) {
			continue
		}
		body := strings.TrimSpace(ln)
		if indentOf(ln) == 4 && strings.HasPrefix(body, prefix) {
			return unquoteYAML(strings.TrimSpace(strings.TrimPrefix(body, prefix))), true
		}
	}
	return "", false
}

// servicesOf 返回 compose 文本里 `services:` 下的顶层服务键（按出现次序）。
// 用来把「只跑 gateway」写成**语义**判据（服务集合），而不是 grep 注释里的字面词。
func servicesOf(text string) []string {
	var out []string
	inServices := false
	for _, ln := range strings.Split(text, "\n") {
		if isCommentLine(ln) {
			continue
		}
		trimmed := strings.TrimRight(ln, " \t")
		switch {
		case strings.TrimSpace(trimmed) == "services:":
			inServices = true
			continue
		case !inServices, strings.TrimSpace(trimmed) == "":
			continue
		}
		if indentOf(trimmed) == 0 {
			break // 走出 services 块
		}
		if indentOf(trimmed) == 2 && strings.HasSuffix(strings.TrimSpace(trimmed), ":") {
			out = append(out, strings.TrimSuffix(strings.TrimSpace(trimmed), ":"))
		}
	}
	return out
}

// TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst 水位必须压到 99：A3 要验的是
// 「写盘**真的** ENOSPC 时闩锁与降级」，水位若留在默认档（90）就会先于 ENOSPC 闩锁，
// 语义被偷换（测的变成水位逻辑）。同时钉住归档根与实例名指向隔离挂载点。
func TestA3ComposeDisablesWatermarkSoRealEnospcHappensFirst(t *testing.T) {
	blk := serviceBlock(t, a3ComposeFile, a3Service)

	watermark, ok := envValue(t, a3ComposeFile, a3Service, blk, "TRACE_BODY_DISK_WATERMARK_PERCENT")
	require.True(t, ok, "A3 编排必须显式设 TRACE_BODY_DISK_WATERMARK_PERCENT")
	require.Equal(t, "99", watermark, "水位必须压到 99 ⇒ 真实 ENOSPC 先于水位闩锁发生")

	root, ok := envValue(t, a3ComposeFile, a3Service, blk, "TRACE_BODY_ROOT")
	require.True(t, ok, "A3 编排必须显式设 TRACE_BODY_ROOT")
	require.Equal(t, "/datalog-a3/trace-archive-v4", root, "正文根必须落在隔离挂载点内")

	inst, ok := envValue(t, a3ComposeFile, a3Service, blk, "TRACE_BODY_INST")
	require.True(t, ok, "A3 编排必须显式设 TRACE_BODY_INST")
	require.Equal(t, "blue", inst, "实例名必须显式给出（不能靠默认值）")

	blockMB, ok := envValue(t, a3ComposeFile, a3Service, blk, "TRACE_BODY_BLOCK_MB")
	require.True(t, ok, "A3 编排必须显式设 TRACE_BODY_BLOCK_MB")
	require.Equal(t, "64", blockMB, "块必须小到能在 1 GiB 回环盘上触发 ENOSPC")
}

// TestA3ComposeHasNoConnectionDefaults 三个连接串必须是 `${VAR:?…}`（**必填、无默认值**）：
// A3 实例复用测试环境的 Redis/PG，一旦带上默认值（`:-`），写错环境变量就会静默连到
// 别处 —— 这条门禁是「不可能不小心连到生产」的结构化形态。
func TestA3ComposeHasNoConnectionDefaults(t *testing.T) {
	blk := serviceBlock(t, a3ComposeFile, a3Service)
	for _, key := range []string{"DATABASE_URL", "REDIS_ADDR", "REDIS_DB"} {
		v, ok := envValue(t, a3ComposeFile, a3Service, blk, key)
		require.True(t, ok, "A3 编排必须显式声明 %s", key)
		require.Contains(t, v, "${A3_"+key+":?",
			"%s 必须写成 `${A3_%s:?…}`（必填、无默认值），实际是 %q", key, key, v)
		require.NotContains(t, v, ":-",
			"%s 不得带默认值（`:-` 会让误配置静默生效），实际是 %q", key, v)
	}

	pw, ok := envValue(t, a3ComposeFile, a3Service, blk, "REDIS_PASSWORD")
	require.True(t, ok, "REDIS_PASSWORD 必须显式列出（即使是空默认）")
	require.Equal(t, "${A3_REDIS_PASSWORD:-}", pw, "密码允许空默认（它不是连接目标）")
}

// TestA3ComposeUsesIsolatedNamesAndPorts 实例必须与生产 / 蓝绿完全错开：容器名前缀
// `llm-a3-`、端口 8090、`network_mode: host`，且**只定义一个服务**（gateway）——
// 不定义控制面服务 ⇒ 不存在 8091；否则 `docker compose down` 会误伤生产组件。
//
// 判据是**语义**的（服务键集合 + 非注释行的取值），不是对整份文本 grep「8091」——
// 注释里合法地说明了「不存在 8091」（见 E68：文本计数不是门禁）。
func TestA3ComposeUsesIsolatedNamesAndPorts(t *testing.T) {
	text := readRepoFile(t, a3ComposeFile)
	blk := serviceBlock(t, a3ComposeFile, a3Service)

	require.Equal(t, []string{a3Service}, servicesOf(text),
		"A3 编排只能定义一个服务（只跑 gateway；不得定义控制面服务）")

	name, ok := serviceFieldValue(t, blk, "container_name")
	require.True(t, ok, "A3 编排必须显式声明 container_name")
	require.Equal(t, "llm-a3-gateway", name, "容器名必须带 llm-a3- 前缀，避免与生产/蓝绿撞名")
	require.True(t, strings.HasPrefix(name, "llm-a3-"), "容器名必须带隔离前缀")

	mode, ok := serviceFieldValue(t, blk, "network_mode")
	require.True(t, ok, "A3 编排必须显式声明 network_mode")
	require.Equal(t, "host", mode, "端口靠 host 网络暴露（与蓝绿编排同形）")

	port, ok := envValue(t, a3ComposeFile, a3Service, blk, "GATEWAY_PORT")
	require.True(t, ok, "A3 编排必须显式设 GATEWAY_PORT")
	require.Equal(t, "8090", port, "端口必须与活跃颜色（8080/8082）错开")

	// 任何**非注释**赋值都不得出现控制面端口（注释里说明「不存在 8091」是合法的）。
	for k, vals := range effectiveAssignments(text, "") {
		for _, v := range vals {
			require.NotContains(t, v, "8091", "%s=%q 不得出现控制面端口 8091", k, v)
		}
	}
}

// TestA3ComposeNeverMountsProductionDatalog 只能挂专用回环目录 `/srv/trace-a3`
// （target `/datalog-a3`）；生产 `/datalog` 一次都不许出现（D66：2026-09-24 的事故形态）。
func TestA3ComposeNeverMountsProductionDatalog(t *testing.T) {
	text := readRepoFile(t, a3ComposeFile)
	blk := serviceBlock(t, a3ComposeFile, a3Service)
	mounts := bindMounts(t, a3ComposeFile, a3Service, blk)
	m := mountTo(t, a3ComposeFile, a3Service, mounts, "/datalog-a3")
	require.Equal(t, "/srv/trace-a3", m.source, "回环 FS 必须来自宿主 /srv/trace-a3")
	require.False(t, m.readOnly, "填盘需要写挂载")

	for _, mm := range mounts {
		require.NotEqual(t, "/datalog", mm.source, "挂载 source 不得是生产 /datalog")
		require.NotEqual(t, "/datalog", mm.target, "挂载 target 不得是生产 /datalog")
	}
	// 再扫非注释行：任何以精确值 `/datalog` 结尾的键值都不许出现（`/datalog-a3` 不匹配）。
	for _, ln := range strings.Split(text, "\n") {
		if isCommentLine(ln) {
			continue
		}
		body := strings.TrimSpace(ln)
		require.False(t,
			strings.HasSuffix(body, ": /datalog") || strings.HasSuffix(body, ":/datalog"),
			"非注释行 %q 直接指向生产 /datalog", body)
	}
}

// TestA3RunScriptAlwaysCleansUp 清理必须挂在 `trap … EXIT` 上，并且**在第一个破坏性
// 步骤之前**注册：中途失败也要卸载、删镜像（否则会留下挂载中的回环设备与占用中的
// 1 GiB 镜像）。trap 的处理函数自身也要跑两次收尾（停容器 + scratch-cleanup --confirm）
// 并复核「挂载点不再是挂载点、镜像已删」。
func TestA3RunScriptAlwaysCleansUp(t *testing.T) {
	s := readRepoFile(t, a3RunScript)

	require.Contains(t, s, "cleanup() {", "必须有 cleanup 处理函数")
	require.Contains(t, s, "docker compose -f \"$COMPOSE\" down", "cleanup 必须停掉隔离实例")
	require.Contains(t, s, "scratch-cleanup --image \"$IMAGE\" --mount \"$MOUNT\" --confirm",
		"cleanup 必须经唯一的破坏性删除入口 scratch-cleanup（带 --confirm）")
	require.Contains(t, s, "findmnt /srv/trace-a3", "cleanup 必须复核挂载点已卸载")
	require.Contains(t, s, "[ -e /srv/trace-a3.img ]", "cleanup 必须复核镜像已删")

	trap := indexOf(t, s, "trap cleanup EXIT")
	first := indexOf(t, s, "go run ./cmd/trace-verify scratch-prepare")
	require.Less(t, trap, first, "trap 必须在第一个破坏性步骤（scratch-prepare）之前注册")
}

// TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep 护栏调用必须**带 `--confirm`**，
// 且出现在：① 归档根复核、② 起隔离实例、③ 填盘 之前。
//
// 【边界，别读错】它的守卫范围是**填盘与 cleanup 之前**，**不包含** `mount -o loop`：
// `mount` 在 `scratch-prepare` 内部完成，而设备护栏按任务 8 步骤 3 的次序必须在挂载
// **之后**调用（挂载前目标只是宿主根 FS 上的普通目录 ⇒ 校验必然误拒，看到的也不是
// 回环 ext4 的设备号）。所以这里断言的是「护栏调用 → 复核 → 起实例 → 填盘」这条次序；
// 「护栏在 mount 之后」由 TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot 钉住。
func TestA3RunScriptCallsGuardBeforeAnyFillOrCleanupStep(t *testing.T) {
	s := readRepoFile(t, a3RunScript)

	guard := "go run ./cmd/trace-verify scratch-prepare --image /srv/trace-a3.img " +
		"--mount /srv/trace-a3 --size 1G --confirm"
	require.Contains(t, s, guard,
		"护栏调用（scratch-prepare 内部的挂载后设备复核）必须存在且带 --confirm")
	gi := indexOf(t, s, guard)

	for _, later := range []string{
		"findmnt -T /srv/trace-a3/trace-archive-v4",
		"docker compose -f deploy/verify/trace-a3/docker-compose.yml up -d --wait",
		"fallocate -l \"$FILL_SIZE\" \"$FILL\"",
		"rm -f \"$FILL\"",
	} {
		require.Less(t, gi, indexOf(t, s, later), "护栏调用必须在 %q 之前", later)
	}
}

// TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot 护栏调用的**位置**是契约：
// ① 在 `mount -o loop` **之后**（挂载前目标只是宿主根 FS 上的目录 ⇒ 校验必然误拒）；
// ② 在**建归档根之前**（D70：挂载前建归档根会在宿主根 FS 上造出一个同名空目录，
// 容器里的 TRACE_BODY_ROOT 就指到根盘上）。这是对 `cmd/trace-verify` 源码文本的次序
// 契约 —— 与 tests/deploy 其它「产物即契约」用例同一形态。
func TestA3ScratchPrepareGuardsOnlyAfterMountAndBeforeArchiveRoot(t *testing.T) {
	src := readRepoFile(t, a3VerifyTool)

	mount := indexOf(t, src, `"mount", "-o", "loop"`)
	guard := indexOf(t, src, "traceverify.GuardScratchTarget(")
	archiveRoot := indexOf(t, src, `"-d", "-m", "0755"`)

	require.Less(t, mount, guard, "设备护栏必须在 `mount -o loop` 成功之后调用")
	require.Less(t, guard, archiveRoot, "护栏必须在建归档根之前调用（D70）")
	require.Contains(t, src, "filepath.Join(*mnt, *archiveRoot)", "归档根必须建在挂载点内")
	require.Contains(t, src, "traceverify.ScratchCapacitySufficient",
		"挂载前必须有容量预检（R2：镜像本身不受设备护栏保护）")
}
