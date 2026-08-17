package observability

import (
	"encoding/json"
	"strconv"
	"time"
)

// traceArchiveTSLayout 是归档行里 ts 的格式。带 +08:00 偏移，
// 与 /datalog/emergency-archive 的存量格式一致（规格 §4.1 步骤①：字段与顺序对齐现有归档格式）。
const traceArchiveTSLayout = "2006-01-02T15:04:05-07:00"

// traceLineZone 是行内 ts 使用的时区：固定 UTC+8。
//
// 【为什么这里再定义一份，而不是 import config.TraceBodyZone】
// internal/gateway/observability 不该依赖 internal/gateway/config：
// 依赖方向一旦反过来，observability 的测试就要跟着配置体系走，
// 而它本身是纯序列化逻辑。时区由调用方通过 TraceBodySink 注入语义，
// 这里只固定「行内 ts 用 +08:00」这一条格式契约（与 dt 口径同源）。
//
// 【为什么不用 time.Local / LoadLocation】与 config.TraceBodyZone 同一理由：
// 生产容器未设 TZ（UTC）且没有 tzdata ⇒ time.Local 会随宿主漂移、
// LoadLocation 直接失败；而 ts 与 dt 必须指向同一天，否则「元数据说这天、目录在那天」。
var traceLineZone = time.FixedZone("CST", 8*60*60)

// traceRawTruncateLen 是降级/包装后的最大长度（沿用 trace_event.go 的 256 字节契约）。
const traceRawTruncateLen = 256

// traceEventLine 把一条事件序列化成**一整行** JSON（规格 §4.1 步骤①）。
//
// 返回的字节**不含**结尾换行（Writer.Append 自己补），且**内部不含裸换行** ——
// 后者是承重不变量：internal/tracearchive 的 validateLine 会拒绝行内裸换行，
// 因为「一次 Append == 一条物理行」是行号与 row_seq 对应的前提
// （writer.go:88-110）。JSON 序列化本身不会产生裸换行（正文里的 \n 编码成 \\n），
// 所以这一条由构造保证，但必须有测试盯着（TestTraceEventLineKeyOrderAndTSFormat）。
//
// 字段规则与 traceEventToModel **逐条对应**（这不是巧合：两条路径共用同一件事的
// 两种表达，规则必须同源，否则「关闭功能时写 PG、开启时写 spool」会给出两套数据）：
//   - request_headers：仅当 json.Valid 时写入，并做 NUL 清洗；
//   - request_body：三条路径，NUL 清洗在 switch **之前**统一做一次（单一应用点）：
//     未校验路径（BodyRawUntrusted）不做前置 json.Valid，合法性由 rawOrQuoted 内部判一次，
//     非法则降级成字符串；已校验且合法时直接嵌入（谓词已判过，不再重复校验）；
//     已校验但非法时包装成 {"_raw_truncated": "前 256 字节"}；
//     两条降级路径都保证字段不为 null，**绝不让整行失效**；
//   - response_body：仅当 json.Valid 时写入并清洗 —— 与请求体不同，
//     响应体非法时**整体省略**（沿用 trace_event.go:231-233 的既有行为）。
func traceEventLine(e TraceEvent) []byte {
	type line struct {
		RequestID      string          `json:"request_id"`
		TS             string          `json:"ts"`
		RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
		RequestBody    json.RawMessage `json:"request_body,omitempty"`
		ResponseBody   json.RawMessage `json:"response_body,omitempty"`
		ErrorCode      string          `json:"error_code"`
		ErrorMessage   string          `json:"error_message"`
	}

	out := line{
		RequestID:    e.RequestID,
		TS:           e.TS.In(traceLineZone).Format(traceArchiveTSLayout),
		ErrorCode:    e.ErrorCode,
		ErrorMessage: e.ErrorMessage,
	}

	if len(e.RequestHeadersRaw) > 0 && json.Valid(e.RequestHeadersRaw) {
		out.RequestHeaders = rawOrQuoted(scrubNULEscapes(e.RequestHeadersRaw))
	}
	if len(e.RequestBodyRaw) > 0 {
		// NUL 清洗在这里做**一次**，三条路径共用同一份已清洗字节。
		// 【为什么必须前置】清洗规则要在唯一应用点完成：若每条分支各写一次
		// scrubNULEscapes，就多出可被静默漏掉的位置（这与请求头那处共用 helper 同理）。
		scrubbedBody := scrubNULEscapes(e.RequestBodyRaw)
		switch {
		// 未校验路径（大正文直存）：不做前置 json.Valid，合法性由 rawOrQuoted 内部判**一次**，
		// 非法则降级成字符串 —— 未校验 ≠ 一定合法，而非法的 json.RawMessage 会让
		// 整行 json.Marshal 失败（比丢一个字段严重得多）。
		case e.BodyRawUntrusted:
			out.RequestBody = rawOrQuoted(scrubbedBody)
		// 已校验路径：谓词里已经判过合法性，这里直接嵌入、不再重复校验。
		// （重复校验 + Marshal 对 RawMessage 的 compact 会让唯一写协程把同一份
		// 正文字节扫三遍；拆开后两条路径各为「1 次 json.Valid + 1 次 compact」。）
		case json.Valid(e.RequestBodyRaw):
			out.RequestBody = json.RawMessage(scrubbedBody)
		default:
			// sanitize 兜底路径：原始请求体非合法 JSON（极少发生），
			// 包装为 {"_raw_truncated": "..."} 写入，确保字段不为 null
			wrapped, err := json.Marshal(map[string]string{
				"_raw_truncated": string(truncateForRaw(e.RequestBodyRaw)),
			})
			if err == nil {
				out.RequestBody = rawOrQuoted(scrubNULEscapes(wrapped))
			}
		}
	}
	if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {
		out.ResponseBody = rawOrQuoted(scrubNULEscapes(e.ResponseBodyRaw))
	}

	buf, err := json.Marshal(out)
	if err != nil {
		// 防御分支：正常情况下不可达（每个 json.RawMessage 字段在赋值前都已保证合法）。
		// 保留兜底是为了「宁可行内少字段，也不能让整条 trace 消失」——但不能静默，
		// 因此把错误编码进行内（调用方与测试都能看见）。
		// 变异 m02（rawOrQuoted 不再降级）会让这里成为唯一活路并被用例抓住。
		return []byte(`{"request_id":` + strconv.Quote(e.RequestID) +
			`,"_line_error":` + strconv.Quote(err.Error()) + `}`)
	}
	return buf
}

// truncateForRaw 截断到 traceRawTruncateLen 字节（按**字节**截断，与既有实现一致）。
func truncateForRaw(b []byte) []byte {
	if len(b) > traceRawTruncateLen {
		return b[:traceRawTruncateLen]
	}
	return b
}

// rawOrQuoted 在字节是合法 JSON 时返回原样 RawMessage，否则降级成 JSON 字符串。
//
// 【为什么要这一层】json.RawMessage 在 Marshal 时会做 compact 校验，
// 非法字节会让**整个对象**的 Marshal 失败。降级成字符串保证了
// 「一个坏字段不会带走整条记录」——这正是 2026-09-23 事故里
// 「单条坏记录连坐整批」教训的同一类防御（trace_event.go:152-160）。
//
// 【降级时为什么也要截断】未校验的大正文（BodyRawUntrusted，可达数十 MB）一旦非法，
// 不截断就会把同样大小的字符串写进 spool 行。既有契约是「降级/包装后的正文最多
// 256 字节」（trace_event.go:225），本函数是该契约在两条降级路径上的唯一出口。
func rawOrQuoted(b []byte) json.RawMessage {
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	return quotedJSON(truncateForRaw(b))
}

// quotedJSON 把任意字节编码成一个 JSON 字符串值（失败时退化为空串，保证仍是合法 JSON）。
func quotedJSON(b []byte) json.RawMessage {
	q, err := json.Marshal(string(b))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(q)
}
