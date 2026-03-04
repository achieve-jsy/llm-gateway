package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

const (
	// DefaultNonStreamTimeout 非流式默认超时，对齐 LiteLLM。
	DefaultNonStreamTimeout = 600 * time.Second
	// DefaultStreamTimeout 流式默认超时。
	DefaultStreamTimeout = 600 * time.Second
)

// ChatOrchestratorInterface 是 ProtocolDispatcher 依赖的编排器抽象。
// 实现者：internal/gateway/orchestrator.ChatOrchestrator。
type ChatOrchestratorInterface interface {
	Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
}

// ProtocolDispatcher 统一请求调度器：封装 group routing + failover + timeout。
// 供所有协议 handler（chat / responses / messages）共用，不感知具体协议格式。
// 各 handler 负责将自身协议格式转换为 core.ChatRequest / core.ChatResponse，
// 再交由 ProtocolDispatcher 完成路由、容错和超时管理。
type ProtocolDispatcher struct {
	group *GroupAwareDispatcher
	orch  ChatOrchestratorInterface
}

// NewProtocolDispatcher 创建 ProtocolDispatcher。
// group 可为 nil（此时直接调用 orch，不走 group routing）。
func NewProtocolDispatcher(group *GroupAwareDispatcher, orch ChatOrchestratorInterface) *ProtocolDispatcher {
	return &ProtocolDispatcher{group: group, orch: orch}
}

// Chat 执行非流式请求，包含 group routing + failover + 每轮独立超时。
// req.Model / req.Provider 会被就地修改为路由后的实际值。
func (d *ProtocolDispatcher) Chat(c *gin.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	originalModel := req.Model
	baseCtx := WithGinContext(c.Request.Context(), c)
	timeout := d.timeout(originalModel, false)

	if err := d.resolveGroup(c, req); err != nil {
		return nil, err
	}

	// 第一轮：primary
	primaryCtx, cancel := context.WithTimeout(baseCtx, timeout)
	resp, err := d.orch.Chat(primaryCtx, req)
	cancel()
	if err == nil {
		return resp, nil
	}

	// 若未走 group routing（没有改变 model），直接返回错误
	if d.group == nil || !c.GetBool("routed_via_group") {
		return nil, err
	}

	return d.chatWithFailover(c, baseCtx, req, originalModel, err, timeout)
}

// OpenStream 建立流式请求，包含 group routing + failover + 独立超时。
// 返回的 io.ReadCloser 为 cancelOnClose 包装：Close() 时自动释放超时 context。
// failover 只在建流阶段进行；一旦流建立成功则不中途切换。
func (d *ProtocolDispatcher) OpenStream(c *gin.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	originalModel := req.Model
	baseCtx := WithGinContext(c.Request.Context(), c)
	timeout := d.timeout(originalModel, true)

	if err := d.resolveGroup(c, req); err != nil {
		return nil, err
	}

	open := func(r *core.ChatRequest) (io.ReadCloser, error) {
		ctx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := d.orch.ChatStream(ctx, r)
		if e != nil {
			cancel()
			return nil, e
		}
		return &dispatcherCancelOnClose{ReadCloser: s, cancel: cancel}, nil
	}

	stream, err := open(req)
	if err == nil {
		return stream, nil
	}

	if d.group == nil || !c.GetBool("routed_via_group") {
		return nil, err
	}

	return d.streamWithFailover(c, req, originalModel, err, open)
}

// resolveGroup 通过 GroupAwareDispatcher 将 group 名解析为实际模型，
// 就地更新 req.Model / req.Provider。
func (d *ProtocolDispatcher) resolveGroup(c *gin.Context, req *core.ChatRequest) error {
	if d.group == nil {
		return nil
	}
	decision, err := d.group.Dispatch(c, req.Model, req)
	if err != nil {
		return err
	}
	if decision != nil {
		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
	}
	return nil
}

func (d *ProtocolDispatcher) chatWithFailover(
	c *gin.Context,
	baseCtx context.Context,
	req *core.ChatRequest,
	originalModel string,
	primaryErr error,
	timeout time.Duration,
) (*core.ChatResponse, error) {
	failedID := dispatcherFailedID(c, req.Model)
	slog.Warn("dispatcher: primary failed",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"failed_model", req.Model,
		"error", primaryErr.Error(),
	)

	decision, err := d.group.HandleFailover(c, originalModel, failedID, req)
	if err != nil {
		slog.Error("dispatcher: no failover target",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
		)
		return nil, primaryErr
	}

	c.Set("failover_used", true)
	req.Model, req.Provider = decision.ModelName, decision.ProviderName
	slog.Info("dispatcher: failover switching",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"target_provider", decision.ProviderName,
		"target_model", decision.ModelName,
	)

	failoverCtx, cancel := context.WithTimeout(baseCtx, timeout)
	resp, err := d.orch.Chat(failoverCtx, req)
	cancel()
	if err == nil {
		c.Set("error_message", "")
		slog.Info("dispatcher: failover success",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
		)
		return resp, nil
	}

	slog.Error("dispatcher: failover also failed",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"target_provider", decision.ProviderName,
		"error", err.Error(),
	)
	return nil, err
}

func (d *ProtocolDispatcher) streamWithFailover(
	c *gin.Context,
	req *core.ChatRequest,
	originalModel string,
	primaryErr error,
	open func(*core.ChatRequest) (io.ReadCloser, error),
) (io.ReadCloser, error) {
	failedID := dispatcherFailedID(c, req.Model)
	slog.Warn("dispatcher: stream primary failed",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"failed_model", req.Model,
		"error", primaryErr.Error(),
	)

	decision, err := d.group.HandleFailover(c, originalModel, failedID, req)
	if err != nil {
		slog.Error("dispatcher: no stream failover target",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
		)
		return nil, primaryErr
	}

	c.Set("failover_used", true)
	req.Model, req.Provider = decision.ModelName, decision.ProviderName
	slog.Info("dispatcher: stream failover switching",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"target_provider", decision.ProviderName,
		"target_model", decision.ModelName,
	)

	stream, err := open(req)
	if err == nil {
		c.Set("error_message", "")
		slog.Info("dispatcher: stream failover success",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
		)
		return stream, nil
	}

	slog.Error("dispatcher: stream failover also failed",
		"request_id", c.GetString("request_id"),
		"group", originalModel,
		"error", err.Error(),
	)
	return nil, err
}

func (d *ProtocolDispatcher) timeout(modelName string, stream bool) time.Duration {
	def := DefaultNonStreamTimeout
	if stream {
		def = DefaultStreamTimeout
	}
	if d.group != nil {
		return d.group.GetGroupTimeout(modelName, def)
	}
	return def
}

// dispatcherCancelOnClose 包装 io.ReadCloser，Close 时调用 cancel 释放超时 context。
type dispatcherCancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (w *dispatcherCancelOnClose) Close() error {
	w.cancel()
	return w.ReadCloser.Close()
}

// dispatcherFailedID 从 DispatchContext 取路由决策时记录的 ModelID（数字 ID），
// 用于 failover 排除；若取不到则回退到 resolvedModel 名称。
func dispatcherFailedID(c *gin.Context, resolvedModel string) string {
	if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
		return dc.RoutedModelID
	}
	return resolvedModel
}
