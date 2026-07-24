package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"sync/atomic"
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

	// BodyRawUntrusted 为 true 表示 RequestBodyRaw 来自"大正文直存"路径（未经 sanitize/校验）。
	// 用于跳过写入侧的 json.Valid 全量校验，避免 30MB 级别的校验阻塞写入协程。
	BodyRawUntrusted bool

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

	// maxBatchBytes：单次批量 INSERT 的正文字节上限（防止 pgx 简单协议内联出 GB 级 SQL 文本）
	maxBatchBytes int64
	// maxQueueBytes：channel 中待写正文的字节上限（防止大正文在队列中无界滞留）
	maxQueueBytes int64
	// queuedBytes：channel 中待写正文的字节数（各行 len(RequestBodyRaw)+len(ResponseBodyRaw) 之和）。
	// Emit 投递前累加、Run 取走事件后立即递减，用于按字节闸门丢弃过载事件。
	queuedBytes atomic.Int64
}

// TraceWriterLimits 按字节约束写入器，防止单次 flush 的 SQL 文本与队列滞留失控。
type TraceWriterLimits struct {
	MaxBatchBytes int64 // 单次批量 INSERT 的正文字节上限，默认 8 MB
	MaxQueueBytes int64 // channel 中待写正文的字节上限，默认 128 MB
}

const (
	DefaultTraceMaxBatchBytes int64 = 8 << 20   // 8 MB
	DefaultTraceMaxQueueBytes int64 = 128 << 20 // 128 MB
)

// NewTraceWriter 创建 TraceWriter，bufSize = channel 缓冲大小
// 保留既有签名以向后兼容，等价于使用默认字节限额的新构造器。
func NewTraceWriter(db *gorm.DB, bufSize, flushN int, interval time.Duration) *TraceWriter {
	return NewTraceWriterWithLimits(db, bufSize, flushN, interval, TraceWriterLimits{})
}

// NewTraceWriterWithLimits 创建带字节限额的 TraceWriter；limits 中非正的字段取默认值。
func NewTraceWriterWithLimits(db *gorm.DB, bufSize, flushN int, interval time.Duration, limits TraceWriterLimits) *TraceWriter {
	if limits.MaxBatchBytes <= 0 {
		limits.MaxBatchBytes = DefaultTraceMaxBatchBytes
	}
	if limits.MaxQueueBytes <= 0 {
		limits.MaxQueueBytes = DefaultTraceMaxQueueBytes
	}
	return &TraceWriter{
		db:            db,
		ch:            make(chan TraceEvent, bufSize),
		flushN:        flushN,
		interval:      interval,
		maxBatchBytes: limits.MaxBatchBytes,
		maxQueueBytes: limits.MaxQueueBytes,
	}
}

// traceEventBodyBytes 返回单条 trace 的原始正文字节数。
// 必须按 TraceEvent 的原始 body 长度统计，不能用 traceEventToModel 转换后的长度
// （转换会 scrub/包装，长度与原始约定不一致）。
func traceEventBodyBytes(e TraceEvent) int64 {
	return int64(len(e.RequestBodyRaw) + len(e.ResponseBodyRaw))
}

// traceEmitBlockTimeout 当 trace channel 满时，Emit 最多阻塞等待一个写槽位的时长。
// 用于吸收瞬时突发（如大量长流式请求在同一窗口集中完成，trace 集中 emit 挤爆 channel）。
const traceEmitBlockTimeout = 500 * time.Millisecond

// traceShutdownFlushTimeout 是关停时写完最后一批 trace 的时间上限。
// 关停路径的 ctx 已取消，必须靠 WithoutCancel + 本超时才能把最后一批真正落库。
const traceShutdownFlushTimeout = 5 * time.Second

// Emit 投递 trace 事件。优先非阻塞快速路径；channel 满时短暂阻塞等待（吸收突发，
// 避免大量并发完成的长请求一起 emit 把 512 槽 channel 挤掉），仅当持续过载（超过
// traceEmitBlockTimeout 仍无空槽）才丢弃——trace 数据非关键路径，不无限阻塞请求。
// 投递前先按字节闸门预占队列额度：queued+n 超过 maxQueueBytes 时直接丢弃，
// 避免大正文在 channel 中无界滞留（字节上限是槽位数之外的额外闸门）。
func (w *TraceWriter) Emit(e TraceEvent) {
	n := traceEventBodyBytes(e)
	if w.queuedBytes.Add(n) > w.maxQueueBytes {
		// 预占失败：回滚计数后丢弃，不能把额度泄漏在计数里
		w.queuedBytes.Add(-n)
		log.Printf("[trace] queue bytes limit exceeded (limit=%d), dropping trace for request_id=%s", w.maxQueueBytes, e.RequestID)
		return
	}
	select {
	case w.ch <- e:
		return
	default:
	}
	select {
	case w.ch <- e:
	case <-time.After(traceEmitBlockTimeout):
		// 投递最终失败：必须归还预占额度，否则计数泄漏会让后续 trace 被永久丢弃
		w.queuedBytes.Add(-n)
		log.Printf("[trace] channel full after %v, dropping trace for request_id=%s", traceEmitBlockTimeout, e.RequestID)
	}
}

// Run 消费 channel，批量写入 DB
func (w *TraceWriter) Run(ctx context.Context) {
	batch := make([]obsmodel.CallTrace, 0, w.flushN)
	// batchBytes 为当前批各行的原始正文字节数之和，用于按字节限批（D5）
	var batchBytes int64
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	flush := func(flushCtx context.Context, b []obsmodel.CallTrace) {
		if len(b) == 0 {
			return
		}
		if err := w.db.WithContext(flushCtx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&b).Error; err != nil {
			log.Printf("[trace] batch insert error, falling back to per-record: %v", err)
			// 单条坏记录（如 jsonb 含 PG 不接受的 NUL 转义）会导致整批回滚，
			// 降级为逐条插入并跳过坏记录，避免连坐丢弃正常 trace。
			for i := range b {
				if err := w.db.WithContext(flushCtx).
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
			// 本分支不接收事件，故无需归还队列额度；batch 中的字节已在各事件出队时归还。
			//
			// 关停时仍要尽力写完最后一批：ctx 此刻已取消，若直接 WithContext(ctx)，
			// 这次写入必然立即失败（且逐条降级同样失败），最后一批 trace 每次关停都会丢。
			// 因此用 WithoutCancel 剥离取消信号，再套一个短超时避免关停被无限拖住。
			flushCtx, cancelFlush := context.WithTimeout(context.WithoutCancel(ctx), traceShutdownFlushTimeout)
			flush(flushCtx, batch)
			cancelFlush()
			return
		case e := <-w.ch:
			// 事件已出队：立即按原始 body 长度归还队列字节额度，
			// 保证任何后续路径（flush 失败、ctx 取消）都不会漏减。
			n := traceEventBodyBytes(e)
			w.queuedBytes.Add(-n)
			batch = append(batch, traceEventToModel(e))
			batchBytes += n
			if len(batch) >= w.flushN || batchBytes >= w.maxBatchBytes {
				flush(ctx, batch)
				batch = batch[:0]
				batchBytes = 0
			}
		case <-ticker.C:
			flush(ctx, batch)
			batch = batch[:0]
			batchBytes = 0
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
		// BodyRawUntrusted（D3）：正文来自「大正文直存」路径（len(body) > MaxParseBytesInTrace），
		// 既未 sanitize 也未校验，且体积可达数十 MB。此处按契约跳过 json.Valid 的全量扫描
		// ——该扫描运行在唯一的写入协程上，会按 body 体积线性阻塞整条 trace 写入链。
		// 极端情况下（客户端发来的超大正文本身不是合法 JSON）该行会被 PG jsonb 拒绝，
		// 由 flush 的逐条降级自然跳过，不影响同批其他记录。
		if e.BodyRawUntrusted || json.Valid(e.RequestBodyRaw) {
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
