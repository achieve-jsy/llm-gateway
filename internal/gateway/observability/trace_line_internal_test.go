package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 本文件是 A 层（包内）：被断言的 traceEventLine 是未导出函数，
// package observability_test 无法访问。分成 A 层的必要性在「逐字节键序」这一条上最强：
// 键序是对外格式契约，用 json.Unmarshal 到 map 断言会丢掉顺序信息。
//
// 测试用的时区固定为 UTC+8，与生产 dt/ts 口径一致。

// TraceBodyZoneForTest 是测试用的 +08:00 固定时区。
//
// 【为什么测试里自带一份，而不是 import internal/gateway/config 的 TraceBodyZone】
// observability 包不得依赖 config 包（依赖方向：config 是装配层，本包是纯序列化逻辑），
// 测试同样不许把这个依赖引进包内 —— 一旦引进，本包的测试就跟着配置体系走。
// 生产侧的口径由本包的 traceLineZone 固定，两份的一致性由
// TestTraceEventLineKeyOrderAndTSFormat 的 +08:00 断言钉住。
var TraceBodyZoneForTest = time.FixedZone("CST", 8*60*60)

func fixedEvent() TraceEvent {
	return TraceEvent{
		RequestID:         "req-1",
		TS:                time.Date(2026, 9, 24, 12, 0, 0, 0, TraceBodyZoneForTest),
		RequestHeadersRaw: []byte(`{"x-a":"1"}`),
		RequestBodyRaw:    []byte(`{"model":"gpt"}`),
		ResponseBodyRaw:   []byte(`{"ok":true}`),
		// 标量字段取**互不相同**的非空值：这样「写反/写错字段」的实现在
		// TestTraceEventLineKeyOrderAndTSFormat 的透传断言上必然转红
		//（若都留空，把 ErrorCode 写成 ErrorMessage 会全绿 —— 见变异 m13）。
		ErrorCode:    "upstream_timeout",
		ErrorMessage: "upstream request timed out",
	}
}

// TestTraceEventLineKeyOrderAndTSFormat 固定对外格式：
// 键序与归档格式一致，ts 必须是 +08:00，且整行不含裸换行
// （裸换行会让 Writer.Append 的 validateLine 拒绝该行 —— 那是承重不变量。
//
//	「一次 Append == 一条物理行」的入口侧见 internal/tracearchive/writer.go:101-110）。
func TestTraceEventLineKeyOrderAndTSFormat(t *testing.T) {
	line := traceEventLine(fixedEvent())

	wantOrder := []string{"request_id", "ts", "request_headers", "request_body", "response_body", "error_code", "error_message"}
	got := keyOrder(t, line)
	if len(got) != len(wantOrder) {
		t.Fatalf("键数不符：want %v got %v", wantOrder, got)
	}
	for i := range wantOrder {
		if got[i] != wantOrder[i] {
			t.Fatalf("键序不符：want %v got %v", wantOrder, got)
		}
	}

	if bytes.ContainsAny(line, "\n\r") {
		t.Fatalf("行内不得有裸换行：%q", line)
	}

	var probe struct {
		RequestID    string `json:"request_id"`
		TS           string `json:"ts"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("必须是一行合法 JSON：%v", err)
	}
	// 标量字段的**值透传**：不只看键存在。四个字段在 fixedEvent() 里两两不同，
	// 因此任何「写错字段/互换」的实现都会在这里转红（变异 m13 即 ErrorCode/ErrorMessage 互换）。
	if probe.RequestID != "req-1" {
		t.Fatalf("request_id 未透传：%q", probe.RequestID)
	}
	if probe.ErrorCode != "upstream_timeout" {
		t.Fatalf("error_code 未透传：%q", probe.ErrorCode)
	}
	if probe.ErrorMessage != "upstream request timed out" {
		t.Fatalf("error_message 未透传：%q", probe.ErrorMessage)
	}
	if !strings.HasSuffix(probe.TS, "+08:00") {
		t.Fatalf("ts 必须以 +08:00 结尾，实际 %q", probe.TS)
	}
	if probe.TS != "2026-09-24T12:00:00+08:00" {
		t.Fatalf("ts 格式不符：%q", probe.TS)
	}
}

// TestTraceEventLineScrubsNUL 固定既有的 NUL 转义清洗规则
// （PG jsonb 拒绝 \u0000，SQLSTATE 22P05；见 trace_event.go:237-245）。
// 这条规则从「写 jsonb 列」平移到「写 spool 行」时必须保持，否则新路径会把
// 一个历史上被清洗掉的坏字节重新引入。
//
// 本用例只覆盖**请求体**路径；请求头路径是同一函数里的另一处代码，
// 由 TestTraceEventLineScrubsNULInRequestHeaders 单独覆盖
// （否则删掉请求头清洗时本用例仍全绿 —— 见变异集合 m03）。
func TestTraceEventLineScrubsNUL(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte(`{"a":"x\u0000y"}`)
	line := traceEventLine(e)
	if bytes.Contains(line, []byte(`\u0000`)) {
		t.Fatalf("NUL 转义必须被替换：%s", line)
	}
	if !bytes.Contains(line, []byte(`\ufffd`)) {
		t.Fatalf("应替换为 U+FFFD：%s", line)
	}
}

// TestTraceEventLineScrubsNULInRequestHeaders 是请求头路径的 NUL 清洗守卫。
//
// 【为什么必须单列一条】请求头清洗（`out.RequestHeaders = ...`）与请求体清洗
// （`out.RequestBody = ...`）是两行独立代码。TestTraceEventLineScrubsNUL 只把 NUL
// 放进请求体 ⇒ 删掉请求头那一行的清洗时它**仍然全绿**（变异集合 m03 原本就是这种形态）。
// 本条把被守卫的性质改坏后确实会红（见任务报告 §变异证据 m03）。
func TestTraceEventLineScrubsNULInRequestHeaders(t *testing.T) {
	e := fixedEvent()
	e.RequestHeadersRaw = []byte(`{"authorization":"tok\u0000en"}`)
	line := traceEventLine(e)
	if bytes.Contains(line, []byte(`\u0000`)) {
		t.Fatalf("请求头里的 NUL 转义必须被替换：%s", line)
	}
	if !bytes.Contains(line, []byte(`\ufffd`)) {
		t.Fatalf("应替换为 U+FFFD：%s", line)
	}
}

// TestTraceEventLineWrapsInvalidRequestBody 固定「非法请求体包装」规则
// （trace_event.go:222-229）：sanitize 兜底路径下原始请求体不是合法 JSON，
// 包成 {"_raw_truncated": "..."} 且最多 256 字节，保证字段不为 null。
//
// 输入特意构造成「非法 + 首 256 字节内含 \u0000 转义」：包装后的 JSON 字符串里
// 这个转义会被 Marshal 转义成 \\u0000，必须再由 scrubNULEscapes 消掉
// （trace_event.go:227 的同一条规则）⇒ 覆盖包装分支的 NUL 清洗（变异 m15）。
func TestTraceEventLineWrapsInvalidRequestBody(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte(`{"a":"x\u0000y"` + strings.Repeat("z", 400))
	line := traceEventLine(e)
	if !bytes.Contains(line, []byte(`_raw_truncated`)) {
		t.Fatalf("非法请求体必须被包装：%s", line)
	}
	var probe struct {
		RequestBody struct {
			Raw string `json:"_raw_truncated"`
		} `json:"request_body"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("包装后必须仍是合法 JSON：%v", err)
	}
	if len(probe.RequestBody.Raw) != 256 {
		t.Fatalf("截断到 256 字节，实际 %d", len(probe.RequestBody.Raw))
	}
	if bytes.Contains(line, []byte(`\u0000`)) {
		t.Fatalf("包装分支也必须清洗 NUL 转义：%s", line)
	}
}

// TestTraceEventLineUntrustedBodyStaysRaw 固定 BodyRawUntrusted 的语义
// （trace_event.go:34-36、:215-221）：大正文直存路径**合法**的原始字节
// 必须原样进入行内（逐字节相等），不能被重新编码。
func TestTraceEventLineUntrustedBodyStaysRaw(t *testing.T) {
	e := fixedEvent()
	raw := []byte(`{"messages":[{"role":"user","content":"你好"}]}`)
	e.RequestBodyRaw = raw
	e.BodyRawUntrusted = true
	line := traceEventLine(e)
	if !bytes.Contains(line, raw) {
		t.Fatalf("合法的大正文必须逐字节原样嵌入：%s", line)
	}
}

// TestTraceEventLineValidBodyStaysRaw 固定**已校验**路径的同一性质
// （trace_event.go:212-221）：sanitize 后的请求体已是合法 JSON，
// 必须原样嵌入（经 Marshal 的 compact，不改变无空白的紧凑正文），不得重新编码成字符串。
//
// 【为什么必须与上面那条分开】实现里这是**两条独立的分支**（未校验路径走 rawOrQuoted、
// 已校验路径直接嵌入 json.RawMessage）。两条分支各需一条杀手用例：否则把已校验分支改成
// quotedJSON（合法正文被转义成字符串）不会有任何用例转红（变异 m14）。
func TestTraceEventLineValidBodyStaysRaw(t *testing.T) {
	e := fixedEvent()
	raw := []byte(`{"messages":[{"role":"user","content":"你好"}],"temperature":0}`)
	e.RequestBodyRaw = raw
	// 已校验路径：BodyRawUntrusted 保持 false
	line := traceEventLine(e)
	if !bytes.Contains(line, raw) {
		t.Fatalf("已校验的合法正文必须原样嵌入（不得重新编码）：%s", line)
	}
}

// TestTraceEventLineInvalidUntrustedBodyFallsBackToString 是上面那条的**反面**：
// BodyRawUntrusted 只表示「没校验过」，不表示「一定合法」。若原始字节非法，
// 直接以 json.RawMessage 嵌入会让 json.Marshal 整体失败 ——
// 那会让**整条 trace 丢失**（比丢一个字段严重得多）。
// 因此非法字节必须降级成**字符串字段**（不是 _raw_truncated 包装：那条规则属于
// 「已校验但非法」的 sanitize 兜底路径，两条路径的降级形态必须不同）。
func TestTraceEventLineInvalidUntrustedBodyFallsBackToString(t *testing.T) {
	e := fixedEvent()
	e.RequestBodyRaw = []byte("not json at all")
	e.BodyRawUntrusted = true
	line := traceEventLine(e)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("非法正文不得让整行失效：%v（line=%s）", err, line)
	}
	body, ok := probe["request_body"]
	if !ok {
		t.Fatalf("request_body 键必须存在（规格要求字段不为 null）：%s", line)
	}
	if !bytes.HasPrefix(body, []byte(`"`)) {
		t.Fatalf("未校验且非法的正文必须降级成 JSON 字符串，实际 %s", body)
	}
}

// TestTraceEventLineOmitsInvalidResponseBody 固定响应体的既有规则
// （trace_event.go:231-233）：响应体从不 sanitize，非法时**整体省略**该字段，
// 而不是包装 —— 与请求体规则不同，这是既有行为，不得在平移时改变。
//
// 【为什么断言「键不存在」而不是「解码成 *json.RawMessage 后为 nil」】
// 后者无法区分「键省略」与「显式输出 null」：JSON null 解码到指针也是 nil ⇒
// 把 tag 的 omitempty 去掉（于是输出 "response_body":null）时用例仍全绿。
// 字段为 null 与省略在归档行里是两种不同的东西，规格要求的是**省略**。
func TestTraceEventLineOmitsInvalidResponseBody(t *testing.T) {
	e := fixedEvent()
	e.ResponseBodyRaw = []byte("{invalid")
	line := traceEventLine(e)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("非法响应体不得让整行失效：%v", err)
	}
	if v, ok := probe["response_body"]; ok {
		t.Fatalf("非法响应体应整体省略该键（既不是包装也不是 null），实际 %s", v)
	}
}

// TestTraceEventLineOmitsInvalidRequestHeaders 固定请求头的既有规则
// （trace_event.go:209-211）：仅当 json.Valid 时写入。
// 请求头没有「包装兜底」这条规则（那属于请求体），非法时**整体省略**该键；
// 删掉 json.Valid 守卫后该键会以 JSON 字符串形式出现 ⇒ 本用例转红。
// 断言形态同上：必须断言「键不存在」，而不是指针为 nil（null 也能骗过后者）。
func TestTraceEventLineOmitsInvalidRequestHeaders(t *testing.T) {
	e := fixedEvent()
	e.RequestHeadersRaw = []byte("{not json")
	line := traceEventLine(e)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(line, &probe); err != nil {
		t.Fatalf("非法请求头不得让整行失效：%v", err)
	}
	if v, ok := probe["request_headers"]; ok {
		t.Fatalf("非法请求头应整体省略该键（既不是包装也不是 null），实际 %s", v)
	}
}

// keyOrder 用 json.Decoder 的 Token 流取出顶层键顺序（json.Unmarshal 到 map 会丢顺序）。
func keyOrder(t *testing.T, line []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("读 JSON 起始符失败：%v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("行必须是 JSON 对象，实际起始符 %v", tok)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatalf("读键失败：%v", err)
		}
		keys = append(keys, k.(string))
		if err := skipValue(dec); err != nil {
			t.Fatalf("跳过值失败：%v", err)
		}
	}
	return keys
}

// skipValue 消费掉当前值的全部 token（含嵌套对象/数组）。
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || (d != '{' && d != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}
