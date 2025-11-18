package observability

import (
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

// Emit 非阻塞投递，满队列丢弃（trace 数据非关键路径）
func (w *TraceWriter) Emit(e TraceEvent) {
	select {
	case w.ch <- e:
	default:
		log.Printf("[trace] channel full, dropping trace for request_id=%s", e.RequestID)
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
			log.Printf("[trace] batch insert error: %v", err)
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
		ct.RequestHeaders = e.RequestHeadersRaw
	}
	if len(e.RequestBodyRaw) > 0 && json.Valid(e.RequestBodyRaw) {
		ct.RequestBody = e.RequestBodyRaw
	}
	if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {
		ct.ResponseBody = e.ResponseBodyRaw
	}
	return ct
}
