package tracesealer_test

// 本文件是 B 层测试（tests/tracesealer，package tracesealer_test），但它读的不是
// 产物而是**仓库里的 Go 源码**（`internal/tracearchive/**` 与
// `internal/gateway/config/trace_body.go`）。
//
// 【为什么必须这样钉】`tracesealer` 里有一批**镜像常量**：`MaxTrailerEntries`
// （冻结包未导出）、`GatewayMaxRollInterval` / `DefaultMinAge`（网关侧未导出）。
// 它们不是「同一个值的三份抄写」，而是**跨包契约**：一旦冻结包或网关侧改了值而镜像
// 没跟，A8 的「行数不超上界」判断与 `--min-age` 的强制下界都会**静默失真** ——
// 判断还在跑、日志还在打，只是拿错了标尺。Go 里没有「引用未导出常量」的办法，所以
// 唯一能自动发现漂移的手段就是现读源码、**求值**、比对。
//
// 【为什么走语法树求值，而不是 grep 出数字】正则钉的是「这一行长什么样」：
// `64 << 20` 改写成 `32 << 21`（同一个值）会假红，`64 << 20` 改成 `64 << 19`
// （真漂移）却可能因为正则只匹配到前半段而假绿。这里用 go/parser 解析出表达式、
// 按整数/时长语义**真正求值**，断言的才是常量真正的值。凡是求值不出来的形态
// （新增了本文件不支持的运算符、常量落进 iota 块）都会**响亮失败**，不会静默取 0。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// repoRoot 返回仓库根：`go test` 把测试二进制的 CWD 设在包目录，从那里向上找 go.mod。
// （与 tests/deploy 的同名辅助同形；测试辅助不能跨包共享，故此处独立一份。）
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

// readRepoFile 读仓库根下的相对路径文件；**读不到就失败**（产物缺失必须红，不是跳过）。
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "读不到被钉住的源文件 %s（它必须存在；缺失 ⇒ 失败，不是跳过）", rel)
	return string(b)
}

// constExprs 收集 Go 源文件里所有 `const <name> = <expr>` 声明（单值形态）。
// 需要的五个常量都是这一形态；若要求的名字落在 iota 块或其它形态里，查找会失败并
// 明确报错，而不是静默取到错值。
func constExprs(t *testing.T, src string) map[string]ast.Expr {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "const.go", src, parser.SkipObjectResolution)
	require.NoError(t, err, "被钉住的源文件必须能被 Go 语法树解析")
	out := map[string]ast.Expr{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if i < len(vs.Values) {
					out[n.Name] = vs.Values[i]
				}
			}
		}
	}
	return out
}

// intConst 在 src 里找到名为 name 的常量并**求值**为 int64（同一文件内的其它常量
// 可以互相引用，例如 maxTrailerEntries = maxTrailerBytes / 8）。
func intConst(t *testing.T, src, name string) int64 {
	t.Helper()
	exprs := constExprs(t, src)
	memo := map[string]int64{}
	var resolve func(string) (int64, error)
	resolve = func(n string) (int64, error) {
		if v, ok := memo[n]; ok {
			return v, nil
		}
		e, ok := exprs[n]
		if !ok {
			return 0, fmt.Errorf("源文件里找不到常量 %s", n)
		}
		v, err := evalIntExpr(e, resolve)
		if err != nil {
			return 0, err
		}
		memo[n] = v
		return v, nil
	}
	v, err := resolve(name)
	require.NoErrorf(t, err, "求值整数常量 %s 失败（形态不受支持时不得静默取值）", name)
	return v
}

// evalIntExpr 求值整数常量表达式：整数、标识符（同文件常量）、括号、一元 ±，
// 以及 + - * / << >> 六个二元运算符。
func evalIntExpr(e ast.Expr, lookup func(string) (int64, error)) (int64, error) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return evalIntExpr(x.X, lookup)
	case *ast.BasicLit:
		if x.Kind != token.INT {
			return 0, fmt.Errorf("不是整数常量：%s", x.Value)
		}
		return strconv.ParseInt(strings.ReplaceAll(x.Value, "_", ""), 0, 64)
	case *ast.Ident:
		return lookup(x.Name)
	case *ast.UnaryExpr:
		v, err := evalIntExpr(x.X, lookup)
		if err != nil {
			return 0, err
		}
		switch x.Op {
		case token.SUB:
			return -v, nil
		case token.ADD:
			return v, nil
		}
		return 0, fmt.Errorf("不支持的一元运算符 %s", x.Op)
	case *ast.BinaryExpr:
		a, err := evalIntExpr(x.X, lookup)
		if err != nil {
			return 0, err
		}
		b, err := evalIntExpr(x.Y, lookup)
		if err != nil {
			return 0, err
		}
		switch x.Op {
		case token.ADD:
			return a + b, nil
		case token.SUB:
			return a - b, nil
		case token.MUL:
			return a * b, nil
		case token.QUO:
			if b == 0 {
				return 0, fmt.Errorf("除零")
			}
			return a / b, nil
		case token.SHL:
			return a << uint(b), nil
		case token.SHR:
			return a >> uint(b), nil
		}
		return 0, fmt.Errorf("不支持的二元运算符 %s", x.Op)
	}
	return 0, fmt.Errorf("不支持的整数表达式类型 %T", e)
}

// durationConst 在 src 里找到名为 name 的时长常量并求值为 time.Duration。
func durationConst(t *testing.T, src, name string) time.Duration {
	t.Helper()
	exprs := constExprs(t, src)
	memo := map[string]time.Duration{}
	var resolve func(string) (time.Duration, error)
	resolve = func(n string) (time.Duration, error) {
		if v, ok := memo[n]; ok {
			return v, nil
		}
		e, ok := exprs[n]
		if !ok {
			return 0, fmt.Errorf("源文件里找不到常量 %s", n)
		}
		v, err := evalDurationExpr(e, resolve)
		if err != nil {
			return 0, err
		}
		memo[n] = v
		return v, nil
	}
	v, err := resolve(name)
	require.NoErrorf(t, err, "求值时长常量 %s 失败（形态不受支持时不得静默取值）", name)
	return v
}

// evalDurationExpr 求值时长常量表达式：`time.Hour/Minute/...`、整数、同文件时长常量、
// 括号，以及 `3 * time.Hour` / `time.Hour * 3` / 时长相加相减相除。
func evalDurationExpr(e ast.Expr, lookup func(string) (time.Duration, error)) (time.Duration, error) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return evalDurationExpr(x.X, lookup)
	case *ast.SelectorExpr:
		d, ok := timeUnit(x)
		if !ok {
			return 0, fmt.Errorf("不支持的包限定常量: %s", x.Sel.Name)
		}
		return d, nil
	case *ast.Ident:
		return lookup(x.Name)
	case *ast.BasicLit:
		if x.Kind != token.INT {
			return 0, fmt.Errorf("不是整数常量：%s", x.Value)
		}
		v, err := strconv.ParseInt(strings.ReplaceAll(x.Value, "_", ""), 0, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(v), nil
	case *ast.BinaryExpr:
		if x.Op == token.MUL {
			// `3 * time.Hour` 与 `time.Hour * 3` 两种书写都要认。
			if d, n, ok := mulDurationByInt(x.X, x.Y, lookup); ok {
				return d * time.Duration(n), nil
			}
			if d, n, ok := mulDurationByInt(x.Y, x.X, lookup); ok {
				return time.Duration(n) * d, nil
			}
			return 0, fmt.Errorf("不支持的时长乘法形态")
		}
		a, err := evalDurationExpr(x.X, lookup)
		if err != nil {
			return 0, err
		}
		b, err := evalDurationExpr(x.Y, lookup)
		if err != nil {
			return 0, err
		}
		switch x.Op {
		case token.ADD:
			return a + b, nil
		case token.SUB:
			return a - b, nil
		case token.QUO:
			if b == 0 {
				return 0, fmt.Errorf("除零")
			}
			return a / b, nil
		}
		return 0, fmt.Errorf("不支持的二元运算符 %s", x.Op)
	}
	return 0, fmt.Errorf("不支持的时长表达式类型 %T", e)
}

// mulDurationByInt 判断 (durExpr, intExpr) 这一侧是不是「时长 × 整数」。
// 【为什么先看表达式的**形状**再看值】裸整数 `3` 会被 evalDurationExpr 当成 3ns
// 成功返回 ⇒ 若不加这道形状检查，`3 * time.Hour` 会被误判成「3ns × time.Hour」而
// 在第二侧求值失败，整条表达式就被判为不支持。
func mulDurationByInt(durExpr, intExpr ast.Expr, lookup func(string) (time.Duration, error)) (time.Duration, int64, bool) {
	switch durExpr.(type) {
	case *ast.SelectorExpr, *ast.Ident, *ast.ParenExpr:
	default:
		return 0, 0, false
	}
	d, err := evalDurationExpr(durExpr, lookup)
	if err != nil {
		return 0, 0, false
	}
	n, err := evalIntExpr(intExpr, func(string) (int64, error) {
		return 0, fmt.Errorf("整数侧不得引用标识符")
	})
	if err != nil {
		return 0, 0, false
	}
	return d, n, true
}

// timeUnit 把 `time.Hour` 这类选择子映射成时长；不是时间单位 ⇒ ok=false。
func timeUnit(x *ast.SelectorExpr) (time.Duration, bool) {
	pkg, ok := x.X.(*ast.Ident)
	if !ok || pkg.Name != "time" {
		return 0, false
	}
	switch x.Sel.Name {
	case "Nanosecond":
		return time.Nanosecond, true
	case "Microsecond":
		return time.Microsecond, true
	case "Millisecond":
		return time.Millisecond, true
	case "Second":
		return time.Second, true
	case "Minute":
		return time.Minute, true
	case "Hour":
		return time.Hour, true
	}
	return 0, false
}

// TestSealerMaxTrailerEntriesMatchesFrozenReader 钉住 `tracesealer.MaxTrailerEntries`
// 等于冻结包 `internal/tracearchive/reader.go` 里**真实求值出来的** `maxTrailerEntries`。
//
// 【为什么必须这样钉】它是**未导出**常量，本包只能复制；漂移之后 A8 的「行数不超
// 上界」判断会静默失真（不会有人发现，因为判断本身还在跑，只是上界换了）。
// 冻结包 `internal/tracearchive/**` 是本计划的**冻结面**，因此它这一侧的值也一并
// 钉住：改动必须是自觉的（改了它就要同步改镜像与本用例，而不是悄悄漂移）。
func TestSealerMaxTrailerEntriesMatchesFrozenReader(t *testing.T) {
	src := readRepoFile(t, "internal/tracearchive/reader.go")
	bytesCap := intConst(t, src, "maxTrailerBytes")
	entries := intConst(t, src, "maxTrailerEntries")

	require.Equal(t, int64(64<<20), bytesCap, "冻结包的 maxTrailerBytes 变了：镜像值必须重新推导（读侧内存预算的另一半）")
	require.Equal(t, bytesCap/8, entries, "maxTrailerEntries 必须是 maxTrailerBytes / 8（条目数 × 8 字节 ≤ 字节上界）")
	require.Equal(t, int64(8_388_608), entries, "冻结包当前的条目数上界是 8,388,608")
	require.Equal(t, int(entries), tracesealer.MaxTrailerEntries,
		"tracesealer.MaxTrailerEntries 是未导出常量的镜像：一旦漂移，A8 的『行数不超上界』判断会静默失真")
}

// TestSealerGatewayRollBoundMatchesConfig 钉住 `tracesealer.GatewayMaxRollInterval`
// == 1h == `internal/gateway/config/trace_body.go` 的 `traceBodyMaxRollInterval`。
//
// 【它的承重面】`--min-age` 的强制下界由这个镜像决定（`min-age` 必须**严格大于**
// 它）。镜像被放大 ⇒ 什么都不拒绝；被缩小 ⇒ 放行一个可能封到「网关仍在追加」的块
// （正文写进已 unlink 的 inode ⇒ 永久丢失，D7）。
func TestSealerGatewayRollBoundMatchesConfig(t *testing.T) {
	src := readRepoFile(t, "internal/gateway/config/trace_body.go")
	frozen := durationConst(t, src, "traceBodyMaxRollInterval")

	require.Equal(t, time.Hour, frozen, "规格 §15.1 R8：网关滚动间隔的硬上界是 1h")
	require.Equal(t, frozen, tracesealer.GatewayMaxRollInterval,
		"tracesealer 的镜像上界漂移后，--min-age 的『严格大于网关滚动上界』判据会按错的上界放行")
}

// TestSealerDefaultMinAgeMatchesFrozenDefault 钉住**三处 3h 必须一致**：
//
//	tracesealer.DefaultMinAge == internal/tracearchive/sealer.go 的 defaultSealMinAge
//	                        == internal/gateway/config/trace_body.go 的 traceBodyMinAge
//
// 【注意（如实登记，不得夸大）】网关侧那条 `roll_interval < minAge` 的判据在现有
// 校验次序下**不可达**（它自己的注释已登记：更早的 `RollInterval > 1h` 守卫先拦下了
// 所有 `>= 3h` 的取值）。因此本用例钉的是**常量一致性**，**不得**声称「网关的那条
// 检查承重」。
func TestSealerDefaultMinAgeMatchesFrozenDefault(t *testing.T) {
	sealerSrc := readRepoFile(t, "internal/tracearchive/sealer.go")
	cfgSrc := readRepoFile(t, "internal/gateway/config/trace_body.go")

	frozenDefault := durationConst(t, sealerSrc, "defaultSealMinAge")
	gatewayMirror := durationConst(t, cfgSrc, "traceBodyMinAge")

	require.Equal(t, 3*time.Hour, frozenDefault, "冻结包的 defaultSealMinAge 是 3h（3 × 网关滚动上界）")
	require.Equal(t, frozenDefault, gatewayMirror, "网关侧 traceBodyMinAge 是冻结包的镜像（规格 §7.2）")
	require.Equal(t, frozenDefault, tracesealer.DefaultMinAge,
		"tracesealer 的 DefaultMinAge 必须与冻结包和网关侧同时一致（三处同为 3h）")
}

// TestSealerFooterSizeMatchesFrozenPackage 钉住镜像的 footer 长度等于冻结包**已导出**
// 的 `tracearchive.FooterSize`。
//
// 【为什么这条比常量一致性更硬】本常量是**唯一**一个可以直接比对（不必读源码）的
// 镜像值 —— 冻结包把它导出了。因此这里用「直接引用」而不是「读源码求值」：那是对
// 同一个事实的最强证据（编译期绑定 + 运行期断言）。
func TestSealerFooterSizeMatchesFrozenPackage(t *testing.T) {
	require.Equal(t, 32, ta.FooterSize, "冻结包的 footer 是 32 字节定长（格式版本 1）")
	require.Equal(t, ta.FooterSize, tracesealer.FooterSize,
		"tracesealer.FooterSize 是冻结包 footer 布局的镜像长度；不一致 ⇒ RowCount 会去读错位置")
}
