package deploy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeLine 返回 Makefile 里第一条以 prefix 开头的行（例如 "build:" / ".PHONY:"）。
// 用「行首」而不是「包含」：`build-sealer:` 也含 "build"，会串味。
func makeLine(t *testing.T, mk, prefix string) string {
	t.Helper()
	for _, ln := range strings.Split(mk, "\n") {
		if strings.HasPrefix(ln, prefix) {
			return ln
		}
	}
	t.Fatalf("Makefile 里找不到以 %q 开头的行", prefix)
	return ""
}

// makeRecipe 返回 Makefile 里某个目标（`target:`）的 recipe（制表符开头的命令块）。
func makeRecipe(t *testing.T, mk, target string) string {
	t.Helper()
	lines := strings.Split(mk, "\n")
	start := -1
	for i, ln := range lines {
		if ln == target+":" || strings.HasPrefix(ln, target+": ") {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "Makefile 里找不到目标 %s:", target)
	var body []string
	for i := start + 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "\t") {
			break
		}
		body = append(body, lines[i])
	}
	require.NotEmpty(t, body, "目标 %s: 没有 recipe（命令块）", target)
	return strings.Join(body, "\n")
}

// yamlJob 取出 .gitlab-ci.yml 里的顶层作业（形如 `name:` 起、到下一个顶格非注释行为止）。
func yamlJob(t *testing.T, ci, name string) string {
	t.Helper()
	lines := strings.Split(ci, "\n")
	start := -1
	for i, ln := range lines {
		if ln == name+":" {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "CI 里找不到作业 %s:", name)
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "#") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// TestMakefileBuildsSealerForLinux sealer 跑在 Linux 宿主机上（直接用宿主机
// /usr/bin/zstd 与 /datalog），而开发机是 darwin ⇒ 必须交叉编译，否则传上去的是
// 一个 Mach-O（在 Linux 上 exec format error）。CGO_ENABLED=0 ⇒ 静态链接。
func TestMakefileBuildsSealerForLinux(t *testing.T) {
	mk := readRepoFile(t, "Makefile")
	r := makeRecipe(t, mk, "build-sealer")
	require.Contains(t, r, "CGO_ENABLED=0")
	require.Contains(t, r, "GOOS=linux", "本机是 darwin：不交叉编译就会把 Mach-O 传上去")
	require.Contains(t, r, "GOARCH=amd64")
	require.Contains(t, r, "./cmd/trace-sealer")
	require.Contains(t, r, "$(SEALER_BIN)")
	require.NotContains(t, r, "./cmd/gateway", "sealer 目标不得编译成网关")
}

// TestMakefileBuildTargetsIncludeSealer `make build` 必须包含 sealer（否则 CI 走默认
// 目标时悄悄漏掉它），两个二进制变量必须定义（否则 `go build -o  ./cmd/...` 会以
// 「-o 后面是空串」报错），而验收工具 `build-trace-verify` **不得**进 build。
func TestMakefileBuildTargetsIncludeSealer(t *testing.T) {
	mk := readRepoFile(t, "Makefile")
	b := makeLine(t, mk, "build:")
	require.Contains(t, b, "build-sealer", "make build 必须包含 sealer")
	require.Contains(t, mk, "SEALER_BIN := bin/trace-sealer")
	require.Contains(t, mk, "VERIFY_BIN := bin/trace-verify")
	v := makeRecipe(t, mk, "build-trace-verify")
	require.Contains(t, v, "$(VERIFY_BIN)")
	require.Contains(t, v, "./cmd/trace-verify")
	require.NotContains(t, b, "build-trace-verify", "验收工具不进任何镜像、不参与生产上线")
	phony := makeLine(t, mk, ".PHONY:")
	require.Contains(t, phony, "build-sealer")
	require.Contains(t, phony, "build-trace-verify")
}

// TestCICIBuildsAndInstallsSealer 上线作业里必须出现 `go build` ./cmd/trace-sealer 与
// `install -m 0755 … /usr/local/bin/trace-sealer`：sealer 是**宿主机**进程（不进任何
// 容器镜像），所以它必须在「真的会把二进制放到目标主机」的那个作业里编译并安装。
func TestCICIBuildsAndInstallsSealer(t *testing.T) {
	ci := readRepoFile(t, ".gitlab-ci.yml")
	job := yamlJob(t, ci, "deploy-app-test-fast")
	require.Contains(t, job, "go build", "作业里必须编译")
	require.Contains(t, job, "./cmd/trace-sealer")
	require.Contains(t, job, "install -m 0755 /tmp/trace-sealer.new /usr/local/bin/trace-sealer",
		"必须安装到 /usr/local/bin/trace-sealer（unit 的 ExecStart 就是这个绝对路径）")
	require.Contains(t, job, "deploy/systemd/trace-sealer.service",
		"unit 文件必须与二进制一起同步（否则 ExecStart 指向不存在的文件）")
	require.Contains(t, job, "systemctl daemon-reload", "同步 unit 之后必须 daemon-reload")
}

// TestCICIDoesNotEnableSealerUnit CI **不得** enable/start trace-sealer.service：
// 启用是人工门（读侧就绪 D73 + 挂载在场），CI 顺手 enable 会让一个「还没被允许上线」
// 的进程在每台部署机上自己起来。
func TestCICIDoesNotEnableSealerUnit(t *testing.T) {
	ci := readRepoFile(t, ".gitlab-ci.yml")
	for _, bad := range []string{
		"systemctl enable trace-sealer",
		"systemctl enable --now trace-sealer",
		"systemctl start trace-sealer",
		"systemctl restart trace-sealer",
	} {
		require.NotContains(t, ci, bad,
			"启用 sealer 是人工门（读侧就绪 + 挂载在场），CI 只安装 + daemon-reload")
	}
}
