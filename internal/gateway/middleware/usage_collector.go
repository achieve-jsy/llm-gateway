package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/example/llm-gateway/internal/gateway/budget"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
)

// PostCostHook 是埋点后的回调钩子，用于预算核销 + 告警触发。
// 参数：gin.Context、实际成本 (USD)、HTTP 状态码。
// 当状态码 >= 400 或 usage 缺失时，视为失败请求，应冲销预扣。
// Hook 由调用者（server/router.go）负责提供，内部应异步执行以避免阻塞响应。
type PostCostHook func(c *gin.Context, realCostUSD float64, httpStatus int)

// UsageCollector 注入 Collector 到请求处理链
// 在 c.Next() 后构造 UsageEvent 并异步投递埋点
// 仅对 LLM 调用路径（/v1/chat/completions, /v1/embeddings 等）生效
// postHook：埋点提交后的可选回调，用于预算核销等业务；传 nil 则跳过
func UsageCollector(collector *gwobs.Collector, traceWriter *gwobs.TraceWriter, postHook PostCostHook) gin.HandlerFunc {
	return UsageCollectorWithRedis(collector, traceWriter, postHook, nil)
}

// UsageCollectorWithRedis 与 UsageCollector 功能一致，额外支持 Redis QPS/TPS 计数器
func UsageCollectorWithRedis(collector *gwobs.Collector, traceWriter *gwobs.TraceWriter, postHook PostCostHook, rdb *redis.Client) gin.HandlerFunc {
	tw := traceWriter

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// 仅采集有实际 LLM 调用的路由，跳过模型列表、健康检查等非调用接口
		if path == "/health" || path == "/ready" ||
			path == "/v1/models" || path == "/v1/model/info" || path == "/v1/health/models" {
			c.Next()
			return
		}

		// start_time 由 RequestID 中间件设置，以便所有阶段耗时使用统一基准
		start := time.Now()
		if st, ok := c.Get("start_time"); ok {
			if t, ok := st.(time.Time); ok {
				start = t
			}
		}

		// 提前拿 key_name，系统探针请求跳过埋点和 trace
		// __system_ 前缀为内部保留命名空间，不计入用量统计
		if isSystemKey(c) {
			c.Next()
			_ = budget.ReleaseBillableReservation(c)
			return
		}
		defer func() { _ = budget.ReleaseBillableReservation(c) }()

		c.Next()

		// c.Next() 之后 key_name 才由鉴权中间件写入 context，再次检查一遍
		// 防止系统探针 key 在鉴权完成后才可见的情况
		if isSystemKey(c) {
			_ = budget.ReleaseBillableReservation(c)
			return
		}

		totalMs := int(time.Since(start).Milliseconds())

		// ── 记录策略：所有请求的 Usage 与 Trace（提示词/响应体）均始终写入。
		// 已移除 log_enabled 门控：应收尽收，杜绝"某部分人上下文缺失"（按 key 关闭日志的机制废弃）。 ──

		// ── Usage 埋点：始终写入（耗时/Token/费用） ──
		if collector != nil {
			event := collector.NewUsageEvent(c)
			event.Status = c.Writer.Status()
			event.DurationMs = int64(totalMs)

			// 媒体准入闸门（A2）判定「无价」时打的标（拒绝时打、开关关闭放行时也打）：
			// 强制 usage_missing=true，让「有字段但无价」这事落到 call_logs.usage_missing，
			// 而不是以 est_cost_usd=0 的形态悄悄混进正常账目里。
			_, mediaUnpriced := MediaPricingMissingInfoFrom(c)
			if mediaUnpriced {
				event.UsageMissing = true
			}

			// ── 预算核销：按「是否发生计价事实」判定，而不是按「金额是否 > 0」 ──
			//
			// 改前：`event.EstCostUSD > 0` 是唯一门槛。无价（catalog 媒体价列为 NULL/0）
			// 或用量缺失时金额恒为 0，请求连记账钩子都进不去 —— 预算层与账目层
			// 对这次调用完全无感，这正是「静默 0 元」。
			// 改后：只要有计价事实（有金额 / 有计价意图但价或用量缺失 / 有真实用量），
			// 就交给钩子；金额为 0 时钩子自身会短路，不会污染账目。
			billable := hasBillableSignal(event, mediaUnpriced)
			if postHook != nil && c.Writer.Status() < 400 && billable {
				if _, checked := budget.GetAccountingPlan(c); checked {
					postHook(c, event.EstCostUSD, c.Writer.Status())
					event.PendingBudgetAccount = budget.GetPendingAccount(c)
				}
			}
			// 有计价事实却算不出钱：必须留计数（E 队告警口径），媒体链路再补一条 Warn 日志。
			// 这里刻意只对媒体链路打日志：token 链路上游不回 usage 是既有常态，
			// 逐请求打日志会淹掉真正需要处理的无价告警。
			if billable && event.EstCostUSD <= 0 && c.Writer.Status() < 400 {
				recordBillableCostMissing(c, billableCostMissingReason(mediaUnpriced, event))
				if mediaUnpriced || event.Media.HasUsage() {
					slog.Warn("媒体请求金额为 0：本次未产生计费事实",
						"request_id", event.RequestID,
						"model", event.Model,
						"routed_model", event.RoutedModel,
						"provider", event.Provider,
						"inbound_endpoint", event.InboundEndpoint,
						"media_pricing_missing", mediaUnpriced,
						"usage_missing", event.UsageMissing,
					)
				}
			}
			collector.Emit(event)
			if event.BillableReserved && !collector.HandlesBillableReservation() {
				_ = budget.ReleaseBillableReservation(c)
			}

			// ── Redis QPS/TPS 计数器（用于运维看板实时指标） ──
			if rdb != nil {
				now := time.Now()
				minuteKey := now.Format("200601021504") // YYYYMMDDHHMM

				// QPS 计数（请求数）
				qpsKey := fmt.Sprintf("ops:qps:%s", minuteKey)
				rdb.Incr(context.Background(), qpsKey)
				rdb.Expire(context.Background(), qpsKey, 10*time.Minute)

				// TPS 计数（Token 数）
				if event.TotalTokens > 0 {
					tpsKey := fmt.Sprintf("ops:tps:%s", minuteKey)
					rdb.IncrBy(context.Background(), tpsKey, int64(event.TotalTokens))
					rdb.Expire(context.Background(), tpsKey, 10*time.Minute)
				}
			}

		}
		if collector == nil {
			_ = budget.ReleaseBillableReservation(c)
		}
		// ── Trace 记录（提示词/响应内容）：恒写入（除系统探针 key 已在前面跳过） ──
		if tw == nil {
			return
		}
		reqID, _ := c.Get("request_id")
		reqIDStr, _ := reqID.(string)
		if reqIDStr == "" {
			return
		}

		te := gwobs.TraceEvent{
			RequestID: reqIDStr,
			TS:        start,
		}

		// 各阶段耗时
		te.StageTotalMs = &totalMs
		if v, ok := getInt16(c, "t_auth_ms"); ok {
			te.StageAuthMs = &v
		}
		if v, ok := getInt16(c, "t_route_ms"); ok {
			te.StageRouteMs = &v
		}
		if v, ok := getInt16(c, "t_upstream_ms"); ok {
			te.StageUpstreamMs = &v
		}
		if v, ok := c.Get("first_token_ms"); ok {
			if ms, ok := v.(int); ok {
				te.StageTTFTMs = &ms
			}
		}
		if v, ok := c.Get("t_ttfb_ms"); ok {
			if ms, ok := v.(int); ok {
				te.StageTTFBMs = &ms
			}
		}

		// 请求头
		if rh, ok := c.Get("trace_req_headers"); ok {
			if b, ok := rh.([]byte); ok {
				te.RequestHeadersRaw = b
			}
		}

		// 请求体：两个 context key 互斥，优先取 sanitize 后的正文。
		// trace_req_body：小正文（<= MaxParseBytesInTrace）经 sanitize 处理后的正文；
		// trace_req_body_raw：大正文直存路径的原始字节，未经 sanitize/校验，
		// 置 BodyRawUntrusted 以跳过写入侧的 json.Valid 全量校验。
		if rb, ok := c.Get("trace_req_body"); ok {
			if b, ok := rb.([]byte); ok {
				te.RequestBodyRaw = b
			}
		} else if rb, ok := c.Get("trace_req_body_raw"); ok {
			if b, ok := rb.([]byte); ok {
				te.RequestBodyRaw = b
				te.BodyRawUntrusted = true
			}
		}

		// 响应体
		isStream, _ := c.Get("stream")
		if isStream == true {
			// 流式：优先使用 streamUsageObserver 在流中逐 chunk 拼接的合成响应
			// 这是根本性优化：observer 已在 ObservedSSEStream 旁路中解析每个 chunk，
			// 无需 responseBodyWriter buf 双写，峰值内存从 O(响应体大小) 降为 O(文本大小)
			if assembled, ok := c.Get("stream_assembled_response"); ok {
				if b, err := json.Marshal(assembled); err == nil {
					te.ResponseBodyRaw = b
				}
			}
			// 兜底：仅存 usage 摘要（observer 未触发时）
			if len(te.ResponseBodyRaw) == 0 {
				if usage, ok := c.Get("usage"); ok {
					if b, err := json.Marshal(map[string]any{"usage": usage, "stream": true}); err == nil {
						te.ResponseBodyRaw = b
					}
				}
			}
		} else {
			// 非流式：直接从 orchestrator 写回的完整响应对象序列化
			// 避免 responseBodyWriter buf 对非流式响应的重复内存占用
			if respObj, ok := c.Get("resp_object"); ok {
				if b, err := json.Marshal(respObj); err == nil {
					te.ResponseBodyRaw = b
				}
			}
		}

		// 流式 observer 未拼接成功时，尝试 handler 登记的 resp_object（如错误响应）
		if len(te.ResponseBodyRaw) == 0 {
			if respObj, ok := c.Get("resp_object"); ok {
				if b, err := json.Marshal(respObj); err == nil {
					te.ResponseBodyRaw = b
				}
			}
		}

		// 错误信息
		if v, ok := c.Get("error_message"); ok {
			if msg, ok := v.(string); ok && msg != "" {
				te.ErrorMessage = msg
				te.ErrorCode = "upstream_error"
			}
		}
		if c.Writer.Status() >= 400 && te.ErrorMessage == "" {
			te.ErrorCode = "http_error"
		}

		tw.Emit(te)
	}
}

// hasBillableSignal 判定本次请求是否发生了「应当计价」的事实。
//
// 与「金额 > 0」的区别（这正是不再被绕过的关键）：
//   - 无价（catalog 媒体价列为 NULL / 字段存在但为 0 占位）→ 金额恒为 0；
//   - 用量缺失（上游不回 usage / 媒体链路没拿到终态用量）→ 金额同样为 0。
//
// 这两种情况下「计价事实已经发生」，必须走完留痕（usage_missing + 计数）与核销路径，
// 不能被金额门槛静默吞掉。只有「既没有金额、也没有任何计价意图」的请求
// （例如非计费端点的探测请求）才返回 false，此时不必惊动预算钩子。
func hasBillableSignal(event gwobs.UsageEvent, mediaUnpriced bool) bool {
	if event.EstCostUSD > 0 {
		return true // 有价且算出了钱
	}
	if mediaUnpriced || event.UsageMissing {
		return true // 有计价意图，但价或用量缺失
	}
	if event.Media.HasUsage() || event.TotalTokens > 0 || event.SearchUnits > 0 {
		return true // 有真实用量，只是这次金额算出来是 0
	}
	return false
}

// billableCostMissingReason 给出「有计价事实但金额为 0」的原因（低基数，可安全作为指标标签）。
func billableCostMissingReason(mediaUnpriced bool, event gwobs.UsageEvent) string {
	switch {
	case mediaUnpriced:
		return "media_pricing_missing"
	case event.UsageMissing:
		return "usage_missing"
	default:
		return "cost_zero"
	}
}

// isSystemKey 判断当前请求是否使用了系统保留 key（__system_ 前缀）
// 系统探针/健康检查请求不写入用量日志和 trace
func isSystemKey(c *gin.Context) bool {
	v, ok := c.Get("key_name")
	if !ok {
		return false
	}
	name, ok := v.(string)
	return ok && len(name) >= 9 && name[:9] == "__system_"
}

func getInt16(c *gin.Context, key string) (int16, bool) {
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int16:
		return n, true
	case int:
		return int16(n), true
	case int64:
		return int16(n), true
	}
	return 0, false
}
