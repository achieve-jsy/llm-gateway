// 本文件是 B 层测试（tests/deploy，package deploy_test）：**只读仓库里的编排产物**
// （deploy/blue-green/docker-compose.{blue,green}.yml 与根 docker-compose.yml），用纯文本
// 断言钉住「挂载与 env 契约」，不启动 docker、不引入 YAML 库。
//
// 契约（计划 4 / 任务 6）：
//
//	gateway        读写挂载 /datalog（spool 是功能开启的必要条件；与读侧路由无关 —— D69）
//	control-plane  只读挂载 /datalog（冻结包 reader 全程不写文件）
//	蓝 / 绿         各自注入 TRACE_BODY_INST=blue / green（颜色↔inst 一一对应，
//	               inst 绝不能来自全局 YAML，否则两个颜色会写进同一个 inst= 目录）
//	根编排          只给挂载、不给任何 TRACE_BODY_*（没有颜色概念；一旦设了
//	               TRACE_BODY_ROOT，`docker compose up` 就会再造一个写者到 inst 未定义、
//	               启动直接失败的状态）
//	TRACE_BODY_ROOT 若**生效**，其父目录必须正好是同文件里的某个挂载目标 —— 这是
//	               D70「整包零建目录」的机器可判定形态：父目录没挂 ⇒ Docker 会在宿主
//	               根 FS 上现建一个目录，正文就此写到根盘上（2026-09-24 事故的同一类）
//
// 辅助函数（本文件私有，不与 trace_sealer_unit_test.go 的同包辅助重名）：
//
//	serviceBlock    按缩进切出某个顶层服务的块（避免对整文件做宽松 grep）
//	bindMounts      切出服务块 volumes: 段里的长语法挂载
//	envValue        取服务块 environment: 段里某个键的生效取值
//	effectiveAssignments 扫整文件，收**非注释行**里以某前缀起头的 KEY: VALUE
package deploy_test

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 三个编排文件（真实路径；顺序固定：蓝 / 绿 / 根）。
const (
	composeBlueFile  = "deploy/blue-green/docker-compose.blue.yml"
	composeGreenFile = "deploy/blue-green/docker-compose.green.yml"
	composeRootFile  = "docker-compose.yml"
)

// composeFiles 供各用例统一遍历。
var composeFiles = []string{composeBlueFile, composeGreenFile, composeRootFile}

// indentOf 返回行首空格数（YAML 缩进只由空格构成）。
func indentOf(ln string) int { return len(ln) - len(strings.TrimLeft(ln, " ")) }

// isCommentLine 判断整行是否为 YAML 注释（前导空白之后以 # 起头）。
func isCommentLine(ln string) bool { return strings.HasPrefix(strings.TrimSpace(ln), "#") }

// serviceBlock 从 compose 文本里按缩进切出某个顶层服务的块。
//
// 【真实 service 键（已核实 deploy/blue-green/docker-compose.blue.yml:10,45 与
// docker-compose.green.yml 同构）】三个编排的顶层 services 键**只有** `gateway` 与
// `control-plane` —— 没有任何把颜色缀进 service 键的形态（不存在 `-blue` / `-green`
// 后缀的 service 键）。颜色只体现在两处：① `container_name: llm-blue-gateway` /
// `llm-blue-control-plane`（green 同理）；② compose 项目名 `-p llm-blue` / `-p llm-green`。
//
// 【为什么不用 YAML 库】引入 yaml 直接依赖会让 go.mod/go.sum 变动，而被测对象只是
// 「某服务下有没有这几行」；按缩进切块足够且更抗重排。
//
// 服务键缺失 ⇒ **失败**（缺失/读不到必须红，不是跳过）。
func serviceBlock(t *testing.T, file, service string) string {
	t.Helper()
	lines := strings.Split(readRepoFile(t, file), "\n")

	start := -1
	for i, ln := range lines {
		if strings.TrimRight(ln, " \t") == "services:" {
			start = i
			break
		}
	}
	require.NotEqual(t, -1, start, "%s 里找不到顶层 `services:` 键", file)

	key := "  " + service + ":"
	begin := -1
	for i := start + 1; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], " \t")
		if ln == key {
			begin = i
			break
		}
		// 非注释的顶格行 ⇒ 已经走出 services 块，服务键不存在
		if strings.TrimSpace(ln) != "" && !isCommentLine(ln) && indentOf(ln) == 0 {
			break
		}
	}
	require.NotEqual(t, -1, begin,
		"%s 的 services: 下找不到服务 %q（服务键必须存在，且**不得**把颜色缀进键名）", file, service)

	end := len(lines)
	for i := begin + 1; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(ln) == "" || isCommentLine(ln) {
			continue
		}
		if indentOf(ln) <= 2 {
			end = i
			break
		}
	}
	blk := strings.Join(lines[begin:end], "\n")
	require.Contains(t, blk, key)
	return blk
}

// composeMount 是长语法挂载（`type: bind` + source/target[+ read_only]）的可读投影。
type composeMount struct {
	kind     string
	source   string
	target   string
	readOnly bool
}

// bindMounts 切出服务块 volumes: 段里的挂载条目。
// 没有 volumes: 段 ⇒ 失败（挂载缺失必须红）。
func bindMounts(t *testing.T, file, service, blk string) []composeMount {
	t.Helper()
	lines := strings.Split(blk, "\n")

	vol := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "volumes:" {
			vol = i
			break
		}
	}
	require.NotEqual(t, -1, vol, "%s 的服务 %q 没有 volumes: 段（挂载必须存在：缺失 ⇒ 失败）", file, service)
	vi := indentOf(lines[vol])

	var mounts []composeMount
	cur := -1
	for i := vol + 1; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(ln) == "" || isCommentLine(ln) {
			continue
		}
		if indentOf(ln) <= vi {
			break
		}
		body := strings.TrimSpace(ln)
		if strings.HasPrefix(body, "- ") || body == "-" {
			mounts = append(mounts, composeMount{})
			cur = len(mounts) - 1
			body = strings.TrimSpace(strings.TrimPrefix(body, "-"))
			if body == "" {
				continue
			}
		}
		require.NotEqual(t, -1, cur, "%s 的服务 %q 的 volumes: 段里出现游离条目 %q", file, service, body)
		k, v, ok := strings.Cut(body, ":")
		require.True(t, ok, "%s 的服务 %q 的挂载条目 %q 不是 key: value 形态", file, service, body)
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "type":
			mounts[cur].kind = v
		case "source":
			mounts[cur].source = v
		case "target":
			mounts[cur].target = v
		case "read_only":
			mounts[cur].readOnly = (v == "true")
		}
	}
	return mounts
}

// mountTo 返回 target == want 的挂载；并要求它是长语法 `type: bind`
// （短语法 `-v /a:/b` 会让 Docker 代建宿主路径 ⇒ 正是要堵死的形态）。
// 找不到 ⇒ 失败（不是跳过）。
func mountTo(t *testing.T, file, service string, mounts []composeMount, want string) composeMount {
	t.Helper()
	for _, m := range mounts {
		if m.target == want {
			require.Equal(t, "bind", m.kind,
				"%s 的服务 %q 的 %s 挂载必须是长语法 `type: bind`（短语法会让 Docker 代建宿主路径）",
				file, service, want)
			return m
		}
	}
	require.FailNow(t, "找不到挂载目标",
		"%s 的服务 %q 没有任何挂载的 target 是 %q；现有挂载 = %v", file, service, want, mounts)
	return composeMount{}
}

// envValue 返回服务块 environment: 段里 key 的**生效**取值（跳过整行注释），去掉成对引号。
// 键不存在 ⇒ ok=false（调用方必须把它当失败处理，不许当「跳过」）。
func envValue(t *testing.T, file, service, blk, key string) (string, bool) {
	t.Helper()
	lines := strings.Split(blk, "\n")

	env := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "environment:" {
			env = i
			break
		}
	}
	require.NotEqual(t, -1, env, "%s 的服务 %q 没有 environment: 段", file, service)
	ei := indentOf(lines[env])

	val, found := "", false
	for i := env + 1; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(ln) == "" || isCommentLine(ln) {
			continue
		}
		if indentOf(ln) <= ei {
			break
		}
		k, v, ok := strings.Cut(strings.TrimSpace(ln), ":")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		val, found = unquoteYAML(strings.TrimSpace(v)), true
	}
	return val, found
}

// unquoteYAML 去掉成对的双/单引号（`"1536"` → `1536`）。
func unquoteYAML(v string) string {
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return v[1 : len(v)-1]
	}
	return v
}

// effectiveAssignments 扫整个文件，收集**非注释行**里以 prefix 起头的 `KEY: VALUE`。
// 只用于 TRACE_BODY_* 这类全局唯一键名，不做 YAML 层级解析。
func effectiveAssignments(text, prefix string) map[string][]string {
	out := map[string][]string{}
	for _, ln := range strings.Split(text, "\n") {
		if isCommentLine(ln) {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(ln), ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if strings.HasPrefix(k, prefix) {
			out[k] = append(out[k], unquoteYAML(strings.TrimSpace(v)))
		}
	}
	return out
}

// TestComposeGatewayMountsDatalogReadWrite 三个编排里 gateway 都必须读写挂载 /datalog
// （写 spool 是功能开启的必要条件；与读侧路由无关 —— D69）。
func TestComposeGatewayMountsDatalogReadWrite(t *testing.T) {
	for _, file := range composeFiles {
		blk := serviceBlock(t, file, "gateway")
		m := mountTo(t, file, "gateway", bindMounts(t, file, "gateway", blk), "/datalog")
		require.Equal(t, "/datalog", m.source, "%s：gateway 的 /datalog 必须来自宿主 /datalog", file)
		require.False(t, m.readOnly,
			"%s：gateway 必须**读写**挂载 /datalog（spool 是功能开启的必要条件）", file)
	}
}

// TestComposeControlPlaneMountsDatalogReadOnly 控制面点读：只读挂载即可
// （冻结包 reader.go 全程不写文件，已核 0 处 Create/OpenFile/Mkdir）。
// 服务键是 `control-plane`（**不是**把颜色缀进键名的那种形态），三个文件各断言一次。
func TestComposeControlPlaneMountsDatalogReadOnly(t *testing.T) {
	for _, file := range composeFiles {
		blk := serviceBlock(t, file, "control-plane")
		m := mountTo(t, file, "control-plane", bindMounts(t, file, "control-plane", blk), "/datalog")
		require.Equal(t, "/datalog", m.source, "%s：控制面的 /datalog 必须来自宿主 /datalog", file)
		require.True(t, m.readOnly,
			"%s：控制面必须**只读**挂载 /datalog（它只点读，不写任何文件）", file)
	}
}

// TestComposeBluePinsInstBlue 蓝文件的 inst 必须是 blue（颜色↔inst 一一对应；
// inst 绝不能来自全局 YAML，否则两个颜色会写进同一个 inst= 目录）。
func TestComposeBluePinsInstBlue(t *testing.T) {
	blk := serviceBlock(t, composeBlueFile, "gateway")
	v, ok := envValue(t, composeBlueFile, "gateway", blk, "TRACE_BODY_INST")
	require.True(t, ok, "%s：gateway 必须**显式**注入 TRACE_BODY_INST", composeBlueFile)
	require.Equal(t, "blue", v, "%s：蓝文件的 inst 必须是 blue（颜色↔inst 一一对应）", composeBlueFile)
	require.NotContains(t, blk, "TRACE_BODY_INST: green",
		"%s：蓝文件里不得出现 green 的 inst", composeBlueFile)
}

// TestComposeGreenPinsInstGreen 同上，green。
func TestComposeGreenPinsInstGreen(t *testing.T) {
	blk := serviceBlock(t, composeGreenFile, "gateway")
	v, ok := envValue(t, composeGreenFile, "gateway", blk, "TRACE_BODY_INST")
	require.True(t, ok, "%s：gateway 必须**显式**注入 TRACE_BODY_INST", composeGreenFile)
	require.Equal(t, "green", v, "%s：绿文件的 inst 必须是 green（颜色↔inst 一一对应）", composeGreenFile)
	require.NotContains(t, blk, "TRACE_BODY_INST: blue",
		"%s：绿文件里不得出现 blue 的 inst", composeGreenFile)
}

// TestComposeColorFilesCarrySpecTraceBodyValues 蓝/绿的四个非开关参数必须与规格/
// 计划 2 的默认值**一致地显式写出**（C2：生产配置不靠代码默认值）。
func TestComposeColorFilesCarrySpecTraceBodyValues(t *testing.T) {
	want := [][2]string{
		{"TRACE_BODY_BLOCK_MB", "1536"},
		{"TRACE_BODY_FLUSH_INTERVAL", "1m"},
		{"TRACE_BODY_ROLL_INTERVAL", "1h"},
		{"TRACE_BODY_DISK_WATERMARK_PERCENT", "90"},
	}
	for _, file := range []string{composeBlueFile, composeGreenFile} {
		blk := serviceBlock(t, file, "gateway")
		for _, kv := range want {
			v, ok := envValue(t, file, "gateway", blk, kv[0])
			require.True(t, ok,
				"%s：gateway 必须**显式**写出 %s（C2：生产配置不靠代码默认值）", file, kv[0])
			require.Equal(t, kv[1], v, "%s：%s 必须与规格/计划 2 的默认值一致", file, kv[0])
		}
	}
}

// TestComposeRootComposeDoesNotEnableTraceBody 根 compose 没有「颜色」概念：
// 一旦在那里设了 TRACE_BODY_ROOT，`docker compose up` 就会再造一个写者到
// inst 未定义的配置文件状态（TRACE_BODY_INST 缺失 ⇒ 启动直接失败）。
// 因此根 compose 只给挂载、不给开关 —— 也不给 inst。
func TestComposeRootComposeDoesNotEnableTraceBody(t *testing.T) {
	eff := effectiveAssignments(readRepoFile(t, composeRootFile), "TRACE_BODY_")
	require.Empty(t, eff,
		"%s：根编排不得出现任何**生效**的 TRACE_BODY_*（没有颜色概念；设了 TRACE_BODY_ROOT "+
			"就会再造一个写者到 inst 未定义、启动即失败的状态）", composeRootFile)
}

// TestComposeTraceBodyRootParentIsMounted 承重的耦合：**任何**文件里若存在生效的
// TRACE_BODY_ROOT=<p>，则同文件里必须有 mount target == dirname(p)。
// 【这条是 D70 的机器可判定形态】root 的父目录没挂 ⇒ Docker 会在宿主根 FS 上
// 现建一个目录，正文就此写到根盘上（2026-09-24 事故的同一类）。
func TestComposeTraceBodyRootParentIsMounted(t *testing.T) {
	for _, file := range composeFiles {
		roots := effectiveAssignments(readRepoFile(t, file), "TRACE_BODY_ROOT")["TRACE_BODY_ROOT"]
		if len(roots) == 0 {
			continue
		}
		mounts := bindMounts(t, file, "gateway", serviceBlock(t, file, "gateway"))
		targets := map[string]bool{}
		for _, m := range mounts {
			targets[m.target] = true
		}
		for _, p := range roots {
			require.True(t, targets[path.Dir(p)],
				"%s：生效的 TRACE_BODY_ROOT=%s 的父目录 %s 必须正好是某个挂载目标"+
					"（否则 Docker 会在宿主根文件系统上现建目录，正文写到根盘上）",
				file, p, path.Dir(p))
		}
	}
}

// TestComposeNeverCarriesSealIntervalKey D64/D71：这个键名无效（网关侧叫
// TRACE_BODY_ROLL_INTERVAL），出现在编排里只会让人以为它有用。
func TestComposeNeverCarriesSealIntervalKey(t *testing.T) {
	for _, file := range composeFiles {
		require.NotContains(t, readRepoFile(t, file), "TRACE_BODY_SEAL_INTERVAL",
			"%s：TRACE_BODY_SEAL_INTERVAL 不是有效键名（网关侧叫 TRACE_BODY_ROLL_INTERVAL）", file)
	}
}
