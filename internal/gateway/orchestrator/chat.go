package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// ginCtxKey is the context key used to carry *gin.Context into the orchestrator
	// so that provider/token info can be written back for the Logger middleware.
	ginCtxKey = contextKey("gin_context")
)

type contextKey string

// routerProvider is a minimal interface that the orchestrator needs from providers.Router.
// Using an interface (rather than a concrete type) keeps the orchestrator testable.
type routerProvider interface {
	ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	ListModels(ctx context.Context) (*core.ModelsResponse, error)
	Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error)
	Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error)
	ModelCount() int
}

// ChatOrchestrator 聊天编排器，协调 Router、core.Provider
type ChatOrchestrator struct {
	router       *providers.Router
	routerGetter func() *providers.Router // 动态获取最新 router（热更新时使用）
}

// NewChatOrchestrator 创建编排器实例
func NewChatOrchestrator(router *providers.Router) *ChatOrchestrator {
	return &ChatOrchestrator{
		router: router,
	}
}

// NewChatOrchestratorWithGetter 创建支持热更新的编排器实例。
// routerGetter 在每次请求时被调用以获取最新的 Router（从 ProviderLoader 的 atomic.Value 读取）。
func NewChatOrchestratorWithGetter(routerGetter func() *providers.Router) *ChatOrchestrator {
	return &ChatOrchestrator{
		routerGetter: routerGetter,
	}
}

// routerSnapshot returns the current router snapshot for use within a single request.
// 当配置了 routerGetter 时，动态获取最新 router（支持热更新）；
// 否则使用启动时传入的静态 router 指针（向后兼容）。
func (o *ChatOrchestrator) routerSnapshot() *providers.Router {
	if o.routerGetter != nil {
		return o.routerGetter()
	}
	return o.router
}

// WithGinContext attaches the Gin context to a Go context so the orchestrator
// can write provider/usage info back to it for structured logging.
func WithGinContext(ctx context.Context, c *gin.Context) context.Context {
	return context.WithValue(ctx, ginCtxKey, c)
}

// ginContextFrom extracts the Gin context from the request context (may be nil).
func ginContextFrom(ctx context.Context) *gin.Context {
	v, _ := ctx.Value(ginCtxKey).(*gin.Context)
	return v
}

// Chat 执行普通聊天调用
// 校验模型可用性 → 调用 Router → 返回归一化响应
// 调用方负责在传入的 ctx 上设置超时，Chat 本身不再创建超时 context。
func (o *ChatOrchestrator) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	// 创建上游调用 span
	ctx, span := tracer.Start(ctx, "upstream.chat",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.messages_count", len(req.Messages)),
		),
	)
	defer span.End()

	slog.DebugContext(ctx, "upstream_call_start",
		"model", req.Model,
		"messages_count", len(req.Messages),
		"stream", false,
	)

	start := time.Now()

	// 阶段计时：上游开始前，记录 route 完成时间和 upstream 开始时间
	if gc := ginContextFrom(ctx); gc != nil {
		if reqStart, ok := gc.Get("start_time"); ok {
			if t, ok := reqStart.(time.Time); ok && !t.IsZero() {
				elapsed := time.Since(t).Milliseconds()
				// elapsed >= 0 且不超出 int16 范围（32767ms ≈ 32s），否则丢弃
				if elapsed >= 0 && elapsed <= 32767 {
					ms := int16(elapsed)
					gc.Set("t_route_ms", ms)
					gc.Set("t_upstream_ms", ms)
				}
			}
		}
	}

	// 通过 Router 分发请求
	resp, err := router.ChatCompletion(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("upstream_call_error",
			"model", req.Model,
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
		}
		return nil, errormap.MapProviderError(err)
	}

	// 记录 token 用量到 OTel span
	span.SetAttributes(
		attribute.Int("llm.usage.prompt_tokens", resp.Usage.PromptTokens),
		attribute.Int("llm.usage.completion_tokens", resp.Usage.CompletionTokens),
		attribute.Int("llm.usage.total_tokens", resp.Usage.TotalTokens),
	)
	span.SetStatus(codes.Ok, "")

	// 将 provider 名、token 用量写回 Gin context 供 Logger / 埋点采集器使用
	if gc := ginContextFrom(ctx); gc != nil {
		// routed_model 仅在组调度未写入时才填（直接请求 model 名的场景）；
		// 组调度路径已由 GroupAwareDispatcher 写入 models.name，此处不覆盖。
		if resp.Model != "" {
			if _, exists := gc.Get("routed_model"); !exists {
				gc.Set("routed_model", resp.Model)
			}
		}
		if _, exists := gc.Get("provider"); !exists {
			if name := router.GetProviderName(req.Model); name != "" {
				gc.Set("provider", name)
			} else if resp.Provider != "" {
				gc.Set("provider", resp.Provider)
			}
		}
		// 写完整 Usage 对象，使 collector 能通过 ExtractCostInput 解析缓存字段
		gc.Set("usage", resp.Usage)
		gc.Set("stream", false)
		// 保存响应体供 TraceWriter 使用（仅非流式）
		gc.Set("resp_object", resp)
	}

	slog.DebugContext(ctx, "upstream_call_done",
		"model", req.Model,
		"duration_ms", elapsed.Milliseconds(),
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens,
		"total_tokens", resp.Usage.TotalTokens,
	)

	return resp, nil
}

// Rerank executes a rerank request through the Router.
func (o *ChatOrchestrator) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.rerank",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.documents_count", len(req.Documents)),
		),
	)
	defer span.End()

	slog.DebugContext(ctx, "upstream_rerank_start",
		"model", req.Model,
		"documents_count", len(req.Documents),
	)
	start := time.Now()

	// 阶段计时：上游开始前，记录 route 完成时间和 upstream 开始时间
	if gc := ginContextFrom(ctx); gc != nil {
		if reqStart, ok := gc.Get("start_time"); ok {
			if t, ok := reqStart.(time.Time); ok && !t.IsZero() {
				elapsed := time.Since(t).Milliseconds()
				if elapsed >= 0 && elapsed <= 32767 {
					ms := int16(elapsed)
					gc.Set("t_route_ms", ms)
					gc.Set("t_upstream_ms", ms)
				}
			}
		}
	}

	resp, err := router.Rerank(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("upstream_rerank_error",
			"model", req.Model,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err.Error(),
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
		}
		return nil, errormap.MapProviderError(err)
	}

	span.SetAttributes(
		attribute.Int("llm.usage.total_tokens", resp.Usage.TotalTokens),
		attribute.Int("llm.results_count", len(resp.Results)),
	)
	span.SetStatus(codes.Ok, "")

	if gc := ginContextFrom(ctx); gc != nil {
		if resp.Model != "" {
			if _, exists := gc.Get("routed_model"); !exists {
				gc.Set("routed_model", resp.Model)
			}
		}
		if _, exists := gc.Get("provider"); !exists {
			if name := router.GetProviderName(req.Model); name != "" {
				gc.Set("provider", name)
			} else if resp.Provider != "" {
				gc.Set("provider", resp.Provider)
			}
		}
		gc.Set("usage", resp.Usage)
		gc.Set("stream", false)
		gc.Set("resp_object", resp)
	}

	slog.DebugContext(ctx, "upstream_rerank_done",
		"model", req.Model,
		"duration_ms", time.Since(start).Milliseconds(),
		"total_tokens", resp.Usage.TotalTokens,
		"results_count", len(resp.Results),
	)

	return resp, nil
}

// Embeddings executes an embeddings request through the Router.
func (o *ChatOrchestrator) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.embeddings",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.input_chars", embeddingInputChars(req)),
		),
	)
	defer span.End()

	slog.DebugContext(ctx, "upstream_embedding_start", "model", req.Model)
	start := time.Now()

	// 阶段计时：上游开始前，记录 route 完成时间和 upstream 开始时间
	if gc := ginContextFrom(ctx); gc != nil {
		if reqStart, ok := gc.Get("start_time"); ok {
			if t, ok := reqStart.(time.Time); ok && !t.IsZero() {
				elapsed := time.Since(t).Milliseconds()
				if elapsed >= 0 && elapsed <= 32767 {
					ms := int16(elapsed)
					gc.Set("t_route_ms", ms)
					gc.Set("t_upstream_ms", ms)
				}
			}
		}
	}

	resp, err := router.Embeddings(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("upstream_embedding_error",
			"model", req.Model,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err.Error(),
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
		}
		return nil, errormap.MapProviderError(err)
	}

	span.SetAttributes(
		attribute.Int("llm.usage.prompt_tokens", resp.Usage.PromptTokens),
		attribute.Int("llm.usage.total_tokens", resp.Usage.TotalTokens),
	)
	span.SetStatus(codes.Ok, "")

	if gc := ginContextFrom(ctx); gc != nil {
		if resp.Model != "" {
			if _, exists := gc.Get("routed_model"); !exists {
				gc.Set("routed_model", resp.Model)
			}
		}
		if _, exists := gc.Get("provider"); !exists {
			if name := router.GetProviderName(req.Model); name != "" {
				gc.Set("provider", name)
			} else if resp.Provider != "" {
				gc.Set("provider", resp.Provider)
			}
		}
		gc.Set("usage", resp.Usage)
		gc.Set("stream", false)
		gc.Set("resp_object", resp)
	}

	slog.DebugContext(ctx, "upstream_embedding_done",
		"model", req.Model,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", resp.Usage.PromptTokens,
		"total_tokens", resp.Usage.TotalTokens,
	)

	return resp, nil
}

// embeddingInputChars 估算 embedding 请求的输入字符数，用于 span 属性。
func embeddingInputChars(req *core.EmbeddingRequest) int {
	if req == nil {
		return 0
	}
	total := 0
	switch v := req.Input.(type) {
	case string:
		total += len(v)
	case []string:
		for _, s := range v {
			total += len(s)
		}
	case []any:
		for _, s := range v {
			if str, ok := s.(string); ok {
				total += len(str)
			}
		}
	}
	return total
}
