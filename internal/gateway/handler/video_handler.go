package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/core/video"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

const maxVideoTaskBodyBytes = 64 * 1024

// ModelResolver resolves a model name to its provider type string.
// Returns "" if the model is unknown or no resolver is configured.
type ModelResolver func(model string) string

// ProviderNameResolver resolves a provider type to a display name.
type ProviderNameResolver func(providerType string) string

// VideoGroupDispatcher 将模型组名解析为上游成员（与 chat/embeddings 共用 GroupAwareDispatcher）。
type VideoGroupDispatcher interface {
	Dispatch(c *gin.Context, modelName string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
}

// VideoTaskRecord 创建任务时写入持久化层的完整字段。
type VideoTaskRecord struct {
	TaskID       string
	APIKeyID     string
	APIKeyName   string
	APIKeyType   string
	UserID       string
	UserName     string
	Model        string
	ProviderName string
	ProviderType string
	Status       string
	RequestBody  string
}

type VideoTaskUpdate struct {
	Status       string
	VideoURL     string
	ResponseBody string
	ErrorMessage string
}

// VideoTaskStore 持久化并查询视频任务 → 渠道映射。
type VideoTaskStore interface {
	Record(ctx context.Context, rec VideoTaskRecord) error
	LookupProviderType(ctx context.Context, taskID string) (string, error)
	Update(ctx context.Context, taskID string, update VideoTaskUpdate) error
	UpdateStatus(ctx context.Context, taskID, status string) error
}

// ---------------------------------------------------------------------------
// Adapter Registry
// ---------------------------------------------------------------------------

// VideoAdapterRegistry maps provider type strings to VideoProvider instances.
// Swap 支持配置热更新：改渠道后无需重启 gateway。
type VideoAdapterRegistry struct {
	mu         sync.RWMutex
	relay      video.VideoProvider
	volcengine video.VideoProvider
	relayName  string
	volcName   string
}

// NewVideoAdapterRegistry creates a registry with the given adapters.
func NewVideoAdapterRegistry(relay, volcengine video.VideoProvider) *VideoAdapterRegistry {
	return &VideoAdapterRegistry{
		relay:      relay,
		volcengine: volcengine,
		relayName:  "seedance-relay",
		volcName:   "seedance-volcengine",
	}
}

// Swap 原子替换适配器与展示名（由 Provider snapshot 热加载调用）。
func (r *VideoAdapterRegistry) Swap(relay, volcengine video.VideoProvider, relayName, volcName string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if relay != nil {
		r.relay = relay
	}
	if volcengine != nil {
		r.volcengine = volcengine
	}
	if relayName != "" {
		r.relayName = relayName
	}
	if volcName != "" {
		r.volcName = volcName
	}
}

// ProviderName 返回渠道展示名（热更新后立即生效）。
func (r *VideoAdapterRegistry) ProviderName(providerType string) string {
	if r == nil {
		return providerType
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	switch providerType {
	case "seedance-volcengine":
		if r.volcName != "" {
			return r.volcName
		}
	default:
		if r.relayName != "" {
			return r.relayName
		}
	}
	return providerType
}

// Get returns the adapter for the given provider type.
// Falls back to relay if the type is unknown.
func (r *VideoAdapterRegistry) Get(providerType string) video.VideoProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	switch providerType {
	case "seedance-volcengine":
		if r.volcengine != nil {
			return r.volcengine
		}
	default:
		if r.relay != nil {
			return r.relay
		}
	}
	if r.relay != nil {
		return r.relay
	}
	return r.volcengine
}

// ---------------------------------------------------------------------------
// Redis-backed VideoTaskStore（热缓存；权威数据在控制面 PG）
// ---------------------------------------------------------------------------

// RedisVideoTaskStore implements VideoTaskStore using Redis hashes.
// Key pattern: video_task:{task_id} → hash{provider_type, model, created_at}
// TTL: 30 days.
type RedisVideoTaskStore struct {
	hset   func(ctx context.Context, key string, values ...interface{}) error
	hget   func(ctx context.Context, key, field string) (string, error)
	expire func(ctx context.Context, key string, expiration time.Duration) error
	ttl    time.Duration
}

// NewRedisVideoTaskStore creates a Redis-backed store from function adapters.
func NewRedisVideoTaskStore(
	hset func(ctx context.Context, key string, values ...interface{}) error,
	hget func(ctx context.Context, key, field string) (string, error),
	expire func(ctx context.Context, key string, expiration time.Duration) error,
	ttl time.Duration,
) *RedisVideoTaskStore {
	return &RedisVideoTaskStore{hset: hset, hget: hget, expire: expire, ttl: ttl}
}

func (s *RedisVideoTaskStore) Record(ctx context.Context, rec VideoTaskRecord) error {
	if rec.TaskID == "" {
		return nil
	}
	key := "video_task:" + rec.TaskID
	if err := s.hset(ctx, key,
		"provider_type", rec.ProviderType,
		"model", rec.Model,
		"created_at", time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}
	if s.ttl > 0 {
		_ = s.expire(ctx, key, s.ttl)
	}
	return nil
}

func (s *RedisVideoTaskStore) LookupProviderType(ctx context.Context, taskID string) (string, error) {
	if taskID == "" {
		return "", nil
	}
	return s.hget(ctx, "video_task:"+taskID, "provider_type")
}

func (s *RedisVideoTaskStore) UpdateStatus(ctx context.Context, taskID, status string) error {
	// Redis 仅作路由缓存，不存 status
	return nil
}

func (s *RedisVideoTaskStore) Update(ctx context.Context, taskID string, update VideoTaskUpdate) error {
	// Redis 仅作路由缓存，不存详情字段
	return nil
}

// ---------------------------------------------------------------------------
// Composite store: Redis 热缓存 + 控制面 PG
// ---------------------------------------------------------------------------

// ControlPlaneVideoTaskWriter 控制面写入/查询接口（由 infra.VideoTaskClient 实现）。
type ControlPlaneVideoTaskWriter interface {
	Create(ctx context.Context, rec VideoTaskRecord) error
	Update(ctx context.Context, taskID string, update VideoTaskUpdate) error
	UpdateStatus(ctx context.Context, taskID, status string) error
	LookupProviderType(ctx context.Context, taskID string) (string, error)
}

// CompositeVideoTaskStore 先写 Redis，再写控制面；查询 Redis → PG。
type CompositeVideoTaskStore struct {
	redis VideoTaskStore
	cp    ControlPlaneVideoTaskWriter
}

// NewCompositeVideoTaskStore 组合 Redis 与控制面。
func NewCompositeVideoTaskStore(redis VideoTaskStore, cp ControlPlaneVideoTaskWriter) *CompositeVideoTaskStore {
	return &CompositeVideoTaskStore{redis: redis, cp: cp}
}

func (s *CompositeVideoTaskStore) Record(ctx context.Context, rec VideoTaskRecord) error {
	if s.redis != nil {
		if err := s.redis.Record(ctx, rec); err != nil {
			slog.Warn("redis video task record failed", "task_id", rec.TaskID, "error", err)
		}
	}
	if s.cp != nil {
		return s.cp.Create(ctx, rec)
	}
	return nil
}

func (s *CompositeVideoTaskStore) LookupProviderType(ctx context.Context, taskID string) (string, error) {
	if s.redis != nil {
		if pt, err := s.redis.LookupProviderType(ctx, taskID); err == nil && pt != "" {
			return pt, nil
		}
	}
	if s.cp != nil {
		return s.cp.LookupProviderType(ctx, taskID)
	}
	return "", nil
}

func (s *CompositeVideoTaskStore) UpdateStatus(ctx context.Context, taskID, status string) error {
	return s.Update(ctx, taskID, VideoTaskUpdate{Status: status})
}

func (s *CompositeVideoTaskStore) Update(ctx context.Context, taskID string, update VideoTaskUpdate) error {
	if s.cp != nil {
		return s.cp.Update(ctx, taskID, update)
	}
	return nil
}

// ---------------------------------------------------------------------------
// VideoHandler
// ---------------------------------------------------------------------------

// VideoHandler 视频生成 Handler
type VideoHandler struct {
	adapterReg    *VideoAdapterRegistry
	modelResolver ModelResolver
	nameResolver  ProviderNameResolver
	groupDispatch VideoGroupDispatcher
	taskStore     VideoTaskStore
}

// AdapterRegistry 返回适配器注册表（供热加载 Swap）。
func (h *VideoHandler) AdapterRegistry() *VideoAdapterRegistry {
	if h == nil {
		return nil
	}
	return h.adapterReg
}

// VideoHandlerOption is a functional option for VideoHandler.
type VideoHandlerOption func(*VideoHandler)

// WithModelResolver sets the model → provider resolver.
func WithModelResolver(r ModelResolver) VideoHandlerOption {
	return func(h *VideoHandler) { h.modelResolver = r }
}

// WithProviderNameResolver sets provider type → display name resolver.
func WithProviderNameResolver(r ProviderNameResolver) VideoHandlerOption {
	return func(h *VideoHandler) { h.nameResolver = r }
}

// WithTaskStore sets the video task persistence store.
func WithTaskStore(s VideoTaskStore) VideoHandlerOption {
	return func(h *VideoHandler) { h.taskStore = s }
}

// WithGroupDispatcher 注入模型组调度（组名 → 上游模型 ID / 渠道类型）。
func WithGroupDispatcher(d VideoGroupDispatcher) VideoHandlerOption {
	return func(h *VideoHandler) { h.groupDispatch = d }
}

// NewVideoHandler 创建视频 Handler
func NewVideoHandler(relayAdapter, volcengineAdapter video.VideoProvider, opts ...VideoHandlerOption) *VideoHandler {
	h := &VideoHandler{
		adapterReg: NewVideoAdapterRegistry(relayAdapter, volcengineAdapter),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *VideoHandler) selectAdapter(model string) video.VideoProvider {
	return h.adapterReg.Get(h.resolveProviderType(model))
}

func (h *VideoHandler) resolveProviderType(model string) string {
	if h.modelResolver != nil {
		if pt := h.modelResolver(model); pt != "" {
			return pt
		}
	}
	return "seedance-relay"
}

func (h *VideoHandler) resolveProviderName(providerType string) string {
	if h.nameResolver != nil {
		if name := h.nameResolver(providerType); name != "" {
			return name
		}
	}
	if h.adapterReg != nil {
		if name := h.adapterReg.ProviderName(providerType); name != "" {
			return name
		}
	}
	return providerType
}

// CreateTask 创建视频任务
func (h *VideoHandler) CreateTask(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	originalModel := strings.TrimSpace(req.Model)
	upstreamModel := originalModel

	providerType := h.resolveProviderType(originalModel)
	providerName := h.resolveProviderName(providerType)

	// 组名 → 上游模型 ID（与 chat/embeddings 一致）；非组名时 decision=nil，原样透传
	if h.groupDispatch != nil && originalModel != "" {
		decision, err := h.groupDispatch.Dispatch(c, originalModel, nil)
		if err != nil {
			respondGroupError(c, err)
			return
		}
		if decision != nil {
			if name := strings.TrimSpace(decision.ModelName); name != "" {
				upstreamModel = name
			}
			if ct := strings.TrimSpace(decision.ChannelType); ct != "" {
				providerType = ct
			}
			if pn := strings.TrimSpace(decision.ProviderName); pn != "" {
				providerName = pn
			}
			body, err = rewriteVideoBodyModel(body, upstreamModel)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
		}
	}

	// 供 UsageCollector / call_logs 使用：对外模型名保留组名
	c.Set("model", originalModel)
	c.Set("provider", providerName)

	adapter := h.adapterReg.Get(providerType)

	taskID, err := adapter.CreateTask(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if h.taskStore != nil && taskID != "" {
		rec := VideoTaskRecord{
			TaskID:       taskID,
			APIKeyID:     c.GetString("key_id"),
			APIKeyName:   c.GetString("key_name"),
			APIKeyType:   c.GetString("key_type"),
			UserID:       c.GetString("user_id"),
			UserName:     c.GetString("user_name"),
			Model:        originalModel,
			ProviderName: providerName,
			ProviderType: providerType,
			Status:       "submitted",
			RequestBody:  truncateVideoTaskBody(body),
		}
		go func() {
			if err := h.taskStore.Record(context.Background(), rec); err != nil {
				slog.Warn("failed to record video task", "task_id", taskID, "error", err)
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{"id": taskID})
}

// rewriteVideoBodyModel 将请求体中的 model 字段替换为上游模型 ID，其余字段原样保留。
func rewriteVideoBodyModel(body []byte, upstreamModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	modelJSON, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, err
	}
	raw["model"] = modelJSON
	return json.Marshal(raw)
}

// GetTask 查询任务状态
func (h *VideoHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")

	providerType := "seedance-relay"
	if h.taskStore != nil {
		if pt, err := h.taskStore.LookupProviderType(c.Request.Context(), taskID); err == nil && pt != "" {
			providerType = pt
		}
	}
	adapter := h.adapterReg.Get(providerType)

	result, err := adapter.GetTask(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// 解析上游结果，异步回写 PG 详情字段
	if h.taskStore != nil && taskID != "" {
		status, videoURL, errorMessage := extractUpstreamTaskDetails(result)
		update := VideoTaskUpdate{
			Status:       status,
			VideoURL:     videoURL,
			ResponseBody: truncateVideoTaskBody(result),
			ErrorMessage: errorMessage,
		}
		if update.Status != "" || update.VideoURL != "" || update.ResponseBody != "" || update.ErrorMessage != "" {
			go func() {
				if err := h.taskStore.Update(context.Background(), taskID, update); err != nil {
					slog.Warn("failed to update video task", "task_id", taskID, "status", update.Status, "error", err)
				}
			}()
		}
	}

	c.Data(http.StatusOK, "application/json", result)
}

func extractUpstreamStatus(raw json.RawMessage) string {
	status, _, _ := extractUpstreamTaskDetails(raw)
	return status
}

func extractUpstreamTaskDetails(raw json.RawMessage) (status, videoURL, errorMessage string) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", "", ""
	}
	status = firstString(obj,
		[]string{"status"},
		[]string{"state"},
		[]string{"task_status"},
	)
	videoURL = firstString(obj,
		[]string{"video_url"},
		[]string{"url"},
		[]string{"content", "video_url"},
		[]string{"content", "url"},
		[]string{"data", "video_url"},
		[]string{"data", "url"},
		[]string{"result", "video_url"},
		[]string{"result", "url"},
		[]string{"output", "video_url"},
		[]string{"output", "url"},
	)
	errorMessage = firstString(obj,
		[]string{"error_message"},
		[]string{"error", "message"},
		[]string{"error"},
	)
	return status, videoURL, errorMessage
}

func firstString(obj map[string]interface{}, paths ...[]string) string {
	for _, path := range paths {
		if v, ok := nestedValue(obj, path); ok {
			switch typed := v.(type) {
			case string:
				if s := strings.TrimSpace(typed); s != "" {
					return s
				}
			case json.Number:
				return typed.String()
			}
		}
	}
	return ""
}

func nestedValue(obj map[string]interface{}, path []string) (interface{}, bool) {
	var current interface{} = obj
	for _, key := range path {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func truncateVideoTaskBody(body []byte) string {
	if len(body) <= maxVideoTaskBodyBytes {
		return string(body)
	}
	return string(body[:maxVideoTaskBodyBytes])
}
