package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TraceEvent 完整请求/响应记录，异步投递到 TraceWriter
type TraceEvent struct {
	RequestID string
	TS        time.Time

	// 阶段耗时（ms，从请求抵达计）
	StageAuthMs     *int16
	StageRouteMs    *int16
	StageUpstreamMs *int16
	StageTTFBMs     *int // 上游 TTFB（仅流式）：上游请求发出→收到响应头
	StageTTFTMs     *int
	StageTotalMs    *int

	// 原始 JSON body
	RequestHeadersRaw []byte // 过滤后的请求头（JSON map）
	RequestBodyRaw    []byte // 请求体
	ResponseBodyRaw   []byte // 响应体（非流式为完整 JSON；流式为重组后的合成 JSON）

	// 错误
	ErrorCode    string
	ErrorMessage string
}

// TraceWriter 批量写入 call_traces 的异步写入器
type TraceWriter struct {
	db       *gorm.DB
	ch       chan TraceEvent
	flushN   int
	interval time.Duration
}

// NewTraceWriter 创建 TraceWriter，bufSize = channel 缓冲大小
func NewTraceWriter(db *gorm.DB, bufSize, flushN int, interval time.Duration) *TraceWriter {
	return &TraceWriter{
		db:       db,
		ch:       make(chan TraceEvent, bufSize),
		flushN:   flushN,
		interval: interval,
	}
}

// traceEmitBlockTimeout 当 trace channel 满时，Emit 最多阻塞等待一个写槽位的时长。
// 用于吸收瞬时突发（如大量长流式请求在同一窗口集中完成，trace 集中 emit 挤爆 channel）。
const traceEmitBlockTimeout = 500 * time.Millisecond

// Emit 投递 trace 事件。优先非阻塞快速路径；channel 满时短暂阻塞等待（吸收突发，
// 避免大量并发完成的长请求一起 emit 把 512 槽 channel 挤掉），仅当持续过载（超过
// traceEmitBlockTimeout 仍无空槽）才丢弃——trace 数据非关键路径，不无限阻塞请求。
func (w *TraceWriter) Emit(e TraceEvent) {
	select {
	case w.ch <- e:
		return
	default:
	}
	select {
	case w.ch <- e:
	case <-time.After(traceEmitBlockTimeout):
		log.Printf("[trace] channel full after %v, dropping trace for request_id=%s", traceEmitBlockTimeout, e.RequestID)
	}
}

// Run 消费 channel，批量写入 DB
func (w *TraceWriter) Run(ctx context.Context) {
	batch := make([]obsmodel.CallTrace, 0, w.flushN)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	flush := func(b []obsmodel.CallTrace) {
		if len(b) == 0 {
			return
		}
		if err := w.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&b).Error; err != nil {
			log.Printf("[trace] batch insert error, falling back to per-record: %v", err)
			// 单条坏记录（如 jsonb 含 PG 不接受的 NUL 转义）会导致整批回滚，
			// 降级为逐条插入并跳过坏记录，避免连坐丢弃正常 trace。
			for i := range b {
				if err := w.db.WithContext(ctx).
					Clauses(clause.OnConflict{DoNothing: true}).
					Create(&b[i]).Error; err != nil {
					log.Printf("[trace] skip trace request_id=%s: %v", b[i].RequestID, err)
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush(batch)
			return
		case e := <-w.ch:
			batch = append(batch, traceEventToModel(e))
			if len(batch) >= w.flushN {
				flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			flush(batch)
			batch = batch[:0]
		}
	}
}

func traceEventToModel(e TraceEvent) obsmodel.CallTrace {
	ct := obsmodel.CallTrace{
		RequestID:       e.RequestID,
		TS:              e.TS,
		StageAuthMs:     e.StageAuthMs,
		StageRouteMs:    e.StageRouteMs,
		StageUpstreamMs: e.StageUpstreamMs,
		StageTTFBMs:     e.StageTTFBMs,
		StageTTFTMs:     e.StageTTFTMs,
		StageTotalMs:    e.StageTotalMs,
		ErrorCode:       e.ErrorCode,
		ErrorMessage:    e.ErrorMessage,
	}
	if len(e.RequestHeadersRaw) > 0 && json.Valid(e.RequestHeadersRaw) {
		ct.RequestHeaders = scrubNULEscapes(e.RequestHeadersRaw)
	}
	// RequestBodyRaw 经过 sanitizeChatRequestBody 处理后始终是合法 JSON；
	// json.Valid 作为防御性校验保留，非法时仍写入（避免因极端兜底路径导致数据丢失）
	if len(e.RequestBodyRaw) > 0 {
		if json.Valid(e.RequestBodyRaw) {
			ct.RequestBody = scrubNULEscapes(e.RequestBodyRaw)
		} else {
			// sanitize 兜底路径：原始请求体非合法 JSON（极少发生），
			// 包装为 {"_raw_truncated": "..."} 写入，确保字段不为 null
			wrapped, err := json.Marshal(map[string]string{"_raw_truncated": string(e.RequestBodyRaw[:min(256, len(e.RequestBodyRaw))])})
			if err == nil {
				ct.RequestBody = scrubNULEscapes(wrapped)
			}
		}
	}
	if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {
		ct.ResponseBody = scrubNULEscapes(e.ResponseBodyRaw)
	}
	return ct
}

// scrubNULEscapes 将 JSON 字节中的 \u0000 转义替换为 \ufffd（U+FFFD 替换字符）。
// Go 的 json.Marshal 会把字符串里的 NUL 字节编码成 \u0000，
// 而 PostgreSQL jsonb 拒绝该转义（SQLSTATE 22P05），单条坏记录即可让整批写入失败。
func scrubNULEscapes(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u0000`)) {
		return b
	}
	return bytes.ReplaceAll(b, []byte(`\u0000`), []byte(`\ufffd`))
}
