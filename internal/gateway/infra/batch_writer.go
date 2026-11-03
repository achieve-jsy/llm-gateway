package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// BatchWriter consumes UsageEvent from a channel, estimates costs per-model
// from the pricing sync.Map, and batch-inserts into call_logs with idempotency.
// Designed to run as a single background goroutine.
type BatchWriter struct {
	ch            <-chan UsageEvent
	db            *gorm.DB
	batchSize     int           // max events per flush, default 200
	flushInterval time.Duration // ticker interval, default 1s
	pricing       *sync.Map     // map[string]ModelPricing — model name → pricing

	stopFn context.CancelFunc
	done   chan struct{}
}

// NewBatchWriter creates a BatchWriter with sensible defaults.
//   - batchSize:      200
//   - flushInterval:  1s
//   - pricing:        empty sync.Map (caller populates via SetPricing)
func NewBatchWriter(ch <-chan UsageEvent, db *gorm.DB) *BatchWriter {
	return &BatchWriter{
		ch:            ch,
		db:            db,
		batchSize:     200,
		flushInterval: time.Second,
		pricing:       new(sync.Map),
		done:          make(chan struct{}),
	}
}

// Start launches the background goroutine. It returns immediately — the
// goroutine runs until ctx is cancelled or Stop() is called.
func (bw *BatchWriter) Start(ctx context.Context) {
	ctx, bw.stopFn = context.WithCancel(ctx)
	go bw.loop(ctx)
}

// Stop signals the background goroutine to shut down and waits for it to
// complete. After Stop returns, the BatchWriter is no longer usable.
func (bw *BatchWriter) Stop() {
	if bw.stopFn != nil {
		bw.stopFn()
	}
	<-bw.done
}

// SetPricing updates the pricing map for a given model name. Safe for
// concurrent use — called by the pricing cache refresh loop.
func (bw *BatchWriter) SetPricing(modelName string, pricing ModelPricing) {
	bw.pricing.Store(modelName, pricing)
}

func (bw *BatchWriter) loop(ctx context.Context) {
	defer close(bw.done)

	batch := make([]UsageEvent, 0, bw.batchSize)
	ticker := time.NewTicker(bw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				bw.flush(batch)
			}
			return

		case evt, ok := <-bw.ch:
			if !ok {
				if len(batch) > 0 {
					bw.flush(batch)
				}
				return
			}
			batch = append(batch, evt)
			if len(batch) >= bw.batchSize {
				bw.flush(batch)
				batch = make([]UsageEvent, 0, bw.batchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				bw.flush(batch)
				batch = make([]UsageEvent, 0, bw.batchSize)
			}
		}
	}
}

const callLogColumnCount = 26

func (bw *BatchWriter) flush(batch []UsageEvent) {
	if len(batch) == 0 {
		return
	}

	start := time.Now()

	var (
		valueStrings   []string
		valueArgs      []interface{}
		pricingMissing int
		usageMissing   int
	)

	for i, evt := range batch {
		cost, pm := bw.estimateEventCost(evt)
		if pm {
			pricingMissing++
		}
		if evt.UsageMissing {
			usageMissing++
		}

		base := i * callLogColumnCount

		valueStrings = append(valueStrings, fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5,
			base+6, base+7, base+8, base+9, base+10,
			base+11, base+12, base+13, base+14, base+15,
			base+16, base+17, base+18, base+19, base+20,
			base+21, base+22, base+23, base+24, base+25,
			base+26,
		))

		valueArgs = append(valueArgs,
			evt.RequestID,         //  1: request_id
			evt.KeyID,             //  2: key_id
			evt.UserID,            //  3: user_id
			evt.DeptID,            //  4: dept_id (nullable *string)
			evt.StudioID,          //  5: studio_id (nullable *string)
			evt.Model,             //  6: model
			evt.StatDate,          //  7: stat_date
			evt.PromptTokens,      //  8: prompt_tokens
			evt.CompletionTokens,  //  9: completion_tokens
			evt.ReasoningTokens,   // 10: reasoning_tokens
			evt.CachedTokens,      // 11: cached_tokens
			evt.TTFTMs,            // 12: ttft_ms (nullable *int)
			evt.DurationMs,        // 13: duration_ms (nullable *int)
			evt.ProviderDurationMs, // 14: provider_duration_ms (nullable *int)
			evt.ProviderType,      // 15: provider_type
			evt.ProviderModelID,   // 16: provider_model_id (nullable *string)
			evt.IsStream,          // 17: is_stream
			evt.SessionID,         // 18: session_id (nullable *string)
			evt.Status,            // 19: status
			evt.UsageMissing,      // 20: usage_missing
			cost.PromptCost,       // 21: prompt_est_cost
			cost.CompletionCost,   // 22: completion_est_cost
			cost.ReasoningCost,    // 23: reasoning_est_cost
			cost.CachedCost,       // 24: cached_est_cost
			cost.TotalCost,        // 25: total_est_cost
			pm,                    // 26: pricing_missing
		)
	}

	query := fmt.Sprintf(`INSERT INTO call_logs (
		request_id, key_id, user_id, dept_id, studio_id,
		model, stat_date, prompt_tokens, completion_tokens,
		reasoning_tokens, cached_tokens,
		ttft_ms, duration_ms, provider_duration_ms,
		provider_type, provider_model_id,
		is_stream, session_id,
		status, usage_missing,
		prompt_est_cost, completion_est_cost, reasoning_est_cost, cached_est_cost, total_est_cost,
		pricing_missing
	) VALUES %s ON CONFLICT(request_id) DO NOTHING`, strings.Join(valueStrings, ","))

	if err := bw.db.Exec(query, valueArgs...).Error; err != nil {
		UsageMetricsInstance.BatchWriteErrors.Inc()
	} else {
		UsageMetricsInstance.Written.Add(int64(len(batch)))
	}

	// ---- call_payloads INSERT ----
	var payloadEvents []UsageEvent
	for _, evt := range batch {
		if len(evt.Messages) > 0 || evt.ResponseText != "" {
			payloadEvents = append(payloadEvents, evt)
		}
	}
	if len(payloadEvents) > 0 {
		const payloadCols = 7
		var pvStrings []string
		var pvArgs []interface{}
		for i, evt := range payloadEvents {
			base := i * payloadCols
			pvStrings = append(pvStrings, fmt.Sprintf(
				"($%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7,
			))
			var messagesJSON []byte
			if len(evt.Messages) > 0 {
				messagesJSON, _ = json.Marshal(evt.Messages)
			}
			var toolsJSON []byte
			if len(evt.Tools) > 0 {
				toolsJSON, _ = json.Marshal(evt.Tools)
			}
			pvArgs = append(pvArgs,
				evt.RequestID,        // 1: request_id
				messagesJSON,          // 2: messages (JSONB)
				evt.ResponseText,      // 3: response_text
				toolsJSON,             // 4: tools (JSONB)
				evt.ByteSize,          // 5: byte_size
				evt.Truncated,         // 6: truncated
				"pg",                  // 7: storage_backend
			)
		}
		payloadQuery := fmt.Sprintf(`INSERT INTO call_payloads (
			request_id, messages, response_text, tools, byte_size, truncated, storage_backend
		) VALUES %s ON CONFLICT(request_id) DO NOTHING`, strings.Join(pvStrings, ","))
		if err := bw.db.Exec(payloadQuery, pvArgs...).Error; err != nil {
			UsageMetricsInstance.BatchWriteErrors.Inc()
		}
	}

	if pricingMissing > 0 {
		UsageMetricsInstance.PriceMissing.Add(int64(pricingMissing))
	}
	if usageMissing > 0 {
		UsageMetricsInstance.UsageMissing.Add(int64(usageMissing))
	}

	UsageMetricsInstance.BatchWriteDuration.ObserveDuration(start)
}

// estimateEventCost looks up pricing for the event's model and returns the
// estimated cost bucketed by prompt/completion/reasoning/cached.
func (bw *BatchWriter) estimateEventCost(evt UsageEvent) (CostBuckets, bool) {
	pricingRaw, ok := bw.pricing.Load(evt.Model)
	if !ok {
		return CostBuckets{}, true
	}

	p := pricingRaw.(ModelPricing)
	cost := EstimateCost(TokenBuckets{
		PromptTokens:     evt.PromptTokens,
		CompletionTokens: evt.CompletionTokens,
		ReasoningTokens:  evt.ReasoningTokens,
		CachedTokens:     evt.CachedTokens,
	}, p)

	return cost, p.PricingMissing
}
