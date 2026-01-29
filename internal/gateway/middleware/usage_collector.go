package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

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
			return
		}

		c.Next()

		// c.Next() 之后 key_name 才由鉴权中间件写入 context，再次检查一遍
		// 防止系统探针 key 在鉴权完成后才可见的情况
		if isSystemKey(c) {
			return
		}

		totalMs := int(time.Since(start).Milliseconds())

		// 判断该 key 是否开启日志记录（耗时/Token 始终计算，仅日志写入受此标志影响）
		logEnabled := true
		if v, ok := c.Get("log_enabled"); ok {
			if b, ok := v.(bool); ok {
				logEnabled = b
			}
		}

		// ── Usage 埋点：始终写入（耗时/Token/费用，无论 log_enabled） ──
		if collector != nil {
			event := collector.NewUsageEvent(c)
			event.Status = c.Writer.Status()
			event.DurationMs = int64(totalMs)
			collector.Emit(event)

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

			// ── v0.9 预算累加 + 告警触发（postHook，事后记账模式） ──
			// 仅当 Reserve() 做了检查（context 中有 budget_keys）
			// 且 Redis 检查未因不可用而跳过时触发
			if postHook != nil && event.EstCostUSD > 0 {
				_, hasChecked := c.Get(CtxKeyBudgetKeys)
				_, wasSkipped := c.Get(CtxKeyBudgetSkipped)
				if hasChecked && !wasSkipped {
					postHook(c, event.EstCostUSD, c.Writer.Status())
				}
			}
		}

		// ── Trace 记录（提示词/响应内容）：仅 log_enabled=true 时写入 ──
		if tw == nil || !logEnabled {
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

		// 请求体
		if rb, ok := c.Get("trace_req_body"); ok {
			if b, ok := rb.([]byte); ok {
				te.RequestBodyRaw = b
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