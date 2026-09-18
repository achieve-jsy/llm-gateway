package trace_body_cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/service"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"gorm.io/gorm"
)

// 本包是点读 CLI（cmd/trace-body）的 B 层用例，只测**可导入**的部分
// （internal/observability/service/trace_body_cli.go）：不起真实进程、不连库、不碰文件系统。
// 变异集合 = tools/trace-mutate/sets/p3-task-7.py（TARGET = 该 service 文件）。
//
// 【为什么退出码/输出必须放在可导入的位置】变异证据只在 B 层产出，`package main` 不可导入
// ⇒ 放在 cmd/trace-body 里的守卫无法被任何用例杀死。

const (
	cliTestRoot = "/datalog/trace-archive-v4"
	cliTestDSN  = "postgres://trace_reader@127.0.0.1:5432/trace?sslmode=disable"
	cliTestReq  = "3f6b0c6e-2a4e-4b1a-9f4f-0f3b2c1d5a77"
)

// cliTestArgs 造一组**合法**参数（额外开关按需追加）。
func cliTestArgs(extra ...string) []string {
	return append([]string{
		"--root", cliTestRoot,
		"--dsn", cliTestDSN,
		"--request-id", cliTestReq,
	}, extra...)
}

// TestTraceBodyCLIExitCodes 是 m01 / m02 的杀手。
//
//	m01 把 ErrBodyMissing 的 3 摘掉 ⇒「写入时即失败、从未落盘」与「元数据指向归档但
//	    存储取不回来」被合并成同一个退出码，排障第一步就失去分辨能力 ⇒ 本用例红；
//	m02 把 ErrTraceBodyDisabled 的 5 摘掉 ⇒ 部署错误（读侧未配置）被报成存储错误 ⇒ 本用例红。
//
// 【为什么覆盖「被包装」的两种形态】调用方一律用 errors.Is 分类（全局约束「错误身份是契约」），
// 服务层用 %w 包装哨兵；只测裸哨兵会让「实现改成比较错误文本」这一退化不被发现。
func TestTraceBodyCLIExitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"取到正文", nil, 0},
		{"正文缺失", ta.ErrBodyMissing, 3},
		{"正文缺失（被包装）", fmt.Errorf("点读 %s: %w", cliTestReq, ta.ErrBodyMissing), 3},
		{"未归档", ta.ErrNotArchived, 4},
		{"未归档（被包装）", fmt.Errorf("点读 %s: %w", cliTestReq, ta.ErrNotArchived), 4},
		{"产物损坏", ta.ErrFooterCorrupt, 4},
		{"索引损坏", ta.ErrTrailerCorrupt, 4},
		{"元数据与存储不一致", fmt.Errorf("%w: %s", service.ErrTraceBodyInconsistent, cliTestReq), 4},
		{"读侧未配置", service.ErrTraceBodyDisabled, 5},
		{"读侧未配置（被包装）", fmt.Errorf("%w: %s", service.ErrTraceBodyDisabled, cliTestReq), 5},
		{"记录不存在", fmt.Errorf("点读 %s: %w", cliTestReq, gorm.ErrRecordNotFound), 2},
		{"其它错误", errors.New("boom"), 2},
	}
	for _, tc := range cases {
		if got := service.ExitCodeFor(tc.err); got != tc.want {
			t.Errorf("%s: ExitCodeFor = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestTraceBodyCLIParsesFlags 是 m03 的杀手：必填项校验被摘掉后，缺 --request-id
// 不再返回用法错误（CLI 的退出码不再是 1，而会带着空 request_id 去查库）⇒ 本用例红。
//
// 【本用例证明的形态】缺任一必填项 ⇒ 返回 error（**不是 panic**，也不是零值 options）——
// panic 会让 CLI 在运维手里变成带堆栈的崩溃，而不是一条可脚本化的用法错误。
func TestTraceBodyCLIParsesFlags(t *testing.T) {
	opts, err := service.ParseTraceBodyCLIOptions("trace-body", cliTestArgs())
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if opts.Root != cliTestRoot || opts.DSN != cliTestDSN || opts.RequestID != cliTestReq {
		t.Errorf("参数未逐字保留: root=%q dsn=%q request_id=%q",
			opts.Root, opts.DSN, opts.RequestID)
	}
	if opts.Mode != service.TraceBodyCLIModeRaw {
		t.Errorf("默认模式应为 raw，实际 %q", opts.Mode)
	}

	// 缺任一必填项 / 未知开关 / --help：都必须走「返回 error」而不是 panic。
	missing := []struct {
		name string
		args []string
	}{
		{"缺root", []string{"--dsn", cliTestDSN, "--request-id", cliTestReq}},
		{"缺dsn", []string{"--root", cliTestRoot, "--request-id", cliTestReq}},
		{"缺request-id", []string{"--root", cliTestRoot, "--dsn", cliTestDSN}},
		{"未知开关", cliTestArgs("--no-such-flag")},
		{"--help", []string{"--help"}},
	}
	for _, tc := range missing {
		got, err := service.ParseTraceBodyCLIOptions("trace-body", tc.args)
		if err == nil {
			t.Errorf("%s: 期望用法错误，实际得到 options=%+v 与 nil error", tc.name, got)
		}
		if got != nil {
			t.Errorf("%s: 出错时必须返回 nil options（不得带着半个配置继续跑），实际 %+v",
				tc.name, got)
		}
	}

	// 输出模式：--ref / --json 各自可选，且 --ref 优先于 --json。
	refOpts, err := service.ParseTraceBodyCLIOptions("trace-body", cliTestArgs("--ref"))
	if err != nil {
		t.Fatalf("--ref 不应报错: %v", err)
	}
	if refOpts.Mode != service.TraceBodyCLIModeRef {
		t.Errorf("--ref 的模式应为 %q，实际 %q", service.TraceBodyCLIModeRef, refOpts.Mode)
	}
	jsonOpts, err := service.ParseTraceBodyCLIOptions("trace-body", cliTestArgs("--json"))
	if err != nil {
		t.Fatalf("--json 不应报错: %v", err)
	}
	if jsonOpts.Mode != service.TraceBodyCLIModeJSON {
		t.Errorf("--json 的模式应为 %q，实际 %q", service.TraceBodyCLIModeJSON, jsonOpts.Mode)
	}
	bothOpts, err := service.ParseTraceBodyCLIOptions("trace-body", cliTestArgs("--json", "--ref"))
	if err != nil {
		t.Fatalf("--json --ref 不应报错: %v", err)
	}
	if bothOpts.Mode != service.TraceBodyCLIModeRef {
		t.Errorf("--ref 必须优先于 --json，实际 %q", bothOpts.Mode)
	}
}

// TestTraceBodyCLIRawWritesExactBytes 是 m04 的杀手：--raw 多写一个换行 ⇒
// 管道给 `jq` / `sha256sum` 的下游全部偏一位（逐字节契约破裂）⇒ 本用例红。
//
// 【为什么要断言**逐字节**而不是「包含」】CLI 的正文输出是运维的取数通道：
// 长度与内容都必须等于服务返回的 Body，任何补白都是数据损坏。
func TestTraceBodyCLIRawWritesExactBytes(t *testing.T) {
	const body = `{"request_headers":{"a":"b"},"request_body":{"x":1},"response_body":{"y":2}}`

	var buf bytes.Buffer
	if err := service.WriteRawBody(&buf, []byte(body)); err != nil {
		t.Fatalf("WriteRawBody 报错: %v", err)
	}
	if got := buf.String(); got != body {
		t.Errorf("--raw 必须逐字节写出正文：\n得到 %q\n期望 %q", got, body)
	}
	if buf.Len() != len(body) {
		t.Errorf("--raw 的字节数 = %d，期望 %d（多出的字节会让下游摘要偏移）", buf.Len(), len(body))
	}

	// 阴性对照：正文本身以换行结尾时，不得再补一个（否则同样破坏逐字节）。
	const withNewline = "{\"request_body\":{}}\n"
	buf.Reset()
	if err := service.WriteRawBody(&buf, []byte(withNewline)); err != nil {
		t.Fatalf("WriteRawBody 报错: %v", err)
	}
	if got := buf.String(); got != withNewline {
		t.Errorf("正文自带换行时不得再补一个：\n得到 %q\n期望 %q", got, withNewline)
	}
}

// TestTraceBodyCLIRefModePrintsAllFourComponents 是 m05 的杀手：--ref 少输出行号（row）
// ⇒ 运维拿着 dt/inst/part 也定位不到具体那一行 ⇒ 本用例红。
//
// 【断言的是「四个分量 + 逐字取自哪里」，不只是「有四个分量」】只查「输出里有 dt/inst/part/row
// 四个词」会被「随手构造一个定位键」满足，而那正是必须拦住的绕行（D59：同一判据两处实现）。
// 因此这里设一份**与正文无关**的定位键，断言输出**逐字**等于它。
func TestTraceBodyCLIRefModePrintsAllFourComponents(t *testing.T) {
	res := &service.TraceBodyResult{
		Body:   []byte(`{"request_body":{"x":1}}`),
		Source: service.SourceArchive,
		Ref:    ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 3, Row: 42},
	}

	var buf bytes.Buffer
	if err := service.WriteRef(&buf, res.Ref); err != nil {
		t.Fatalf("WriteRef 报错: %v", err)
	}
	want := "2026-09-24/blue/part-000003#row=42\n"
	if got := buf.String(); got != want {
		t.Errorf("--ref 必须逐字输出服务导出的四分量：\n得到 %q\n期望 %q", got, want)
	}
	// 差分定位：四分量各自可读（少任何一个单独报出来，便于把红归因到具体分量）。
	for _, part := range []string{"2026-09-24", "blue", "part-000003", "row=42"} {
		if !strings.Contains(buf.String(), part) {
			t.Errorf("--ref 缺少分量 %q（输出 %q）", part, buf.String())
		}
	}
}

// TestTraceBodyCLIJSONSummaryMatchesServiceResult：--json 的摘要必须**由服务返回的正文**
// 算出（bytes / sha256 取自 Body、source 取自 Source），且**不得**夹带正文
// （摘要的用途正是「不泄露正文地核对拿到了哪一份」）。
func TestTraceBodyCLIJSONSummaryMatchesServiceResult(t *testing.T) {
	body := []byte(`{"request_body":{"x":1},"response_body":{"y":2}}`)
	res := &service.TraceBodyResult{Body: body, Source: service.SourceSpool}

	var buf bytes.Buffer
	if err := service.WriteSummaryJSON(&buf, res); err != nil {
		t.Fatalf("WriteSummaryJSON 报错: %v", err)
	}

	var got struct {
		Source string `json:"source"`
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("--json 的输出不是合法 JSON（%q）: %v", buf.String(), err)
	}
	sum := sha256.Sum256(body)
	if got.Source != "spool" {
		t.Errorf("source = %q，期望 %q", got.Source, "spool")
	}
	if got.Bytes != len(body) {
		t.Errorf("bytes = %d，期望 %d", got.Bytes, len(body))
	}
	if want := hex.EncodeToString(sum[:]); got.SHA256 != want {
		t.Errorf("sha256 = %q，期望 %q", got.SHA256, want)
	}
	if strings.Contains(buf.String(), "request_body") {
		t.Errorf("--json 不得夹带正文，实际输出 %q", buf.String())
	}
}
