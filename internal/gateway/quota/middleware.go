package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core/quota"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"

	"github.com/gin-gonic/gin"
)

// releaseToken 并发释放令牌
type releaseToken struct {
	key string
}

// QuotaMiddleware 四层配额中间件（主体层 + 组层）。
// 仅挂载于推理 POST 路径（chat/embeddings/messages/responses）。
type QuotaMiddleware struct {
	holder *SnapshotHolder
	store  *RedisStore
}

// NewMiddleware 创建配额中间件。
func NewMiddleware(holder *SnapshotHolder, store *RedisStore) *QuotaMiddleware {
	return &QuotaMiddleware{holder: holder, store: store}
}

// Handler 返回 Gin 中间件函数。
func (m *QuotaMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := m.BeforeUpstream(c, ""); err != nil {
			return
		}
		c.Next()
		m.AfterUpstream(c)
	}
}

func (m *QuotaMiddleware) BeforeUpstream(c *gin.Context, groupName string) error {
	if name, ok := c.Get("key_name"); ok {
		if value, ok := name.(string); ok && strings.HasPrefix(value, "__system_") {
			return nil
		}
	}
	identityVal, exists := c.Get("identity")
	if !exists {
		return nil
	}
	id, ok := identityVal.(*gwmodel.UserIdentity)
	if !ok {
		return nil
	}
	subjectType, subjectID := ResolveSubject(id)
	snapshot := m.holder.Get()
	if snapshot != nil {
		for _, exemption := range snapshot.Exemptions {
			if exemption.SubjectType == subjectType && exemption.SubjectID == subjectID && exemption.ExpiresAt.After(time.Now()) {
				return nil
			}
		}
	}
	if groupName == "" {
		groupName = extractModelParam(c)
	}
	type layerCheck struct {
		layer   string
		limits  EffectiveResult
		rpmKey  string
		concKey string
	}
	layers := []layerCheck{{
		layer: "subject", limits: ResolveSubjectLimits(snapshot, subjectType, subjectID),
		rpmKey: RPMKey(subjectType, subjectID, "", ""), concKey: ConcurrencyKey(subjectType, subjectID, "", ""),
	}}
	if groupName != "" {
		layers = append(layers, layerCheck{
			layer: "group", limits: ResolveGroupLimits(snapshot, groupName),
			rpmKey:  RPMKey(subjectType, subjectID, "group", groupName),
			concKey: ConcurrencyKey(subjectType, subjectID, "group", groupName),
		})
	}
	tokens := []releaseToken{}
	for _, layer := range layers {
		if err := m.store.AcquireConcurrency(c.Request.Context(), layer.concKey, layer.limits.MaxConcurrency); err != nil {
			m.releaseAll(tokens)
			WriteQuotaRejected(c, quota.QuotaViolation{
				SubjectType: subjectType, SubjectID: subjectID, Layer: layer.layer,
				Gate: "concurrency", Limit: layer.limits.MaxConcurrency,
			})
			return err
		}
		tokens = append(tokens, releaseToken{key: layer.concKey})
		if err := m.store.CheckRPM(c.Request.Context(), layer.rpmKey, layer.limits.RPM); err != nil {
			m.releaseAll(tokens)
			WriteQuotaRejected(c, quota.QuotaViolation{
				SubjectType: subjectType, SubjectID: subjectID, Layer: layer.layer,
				Gate: "rpm", Limit: layer.limits.RPM,
			})
			return err
		}
	}
	c.Set("quota_release_tokens", tokens)
	c.Set("quota_subject_type", subjectType)
	c.Set("quota_subject_id", subjectID)
	return nil
}

func (m *QuotaMiddleware) AfterUpstream(c *gin.Context) {
	if final, ok := c.Get("quota_release_tokens"); ok {
		if tokens, ok := final.([]releaseToken); ok {
			m.releaseAll(tokens)
		}
	}
	c.Set("quota_release_tokens", []releaseToken{})
}

// releaseAll 逆序释放所有并发令牌。
// 使用 5s 超时避免 Redis 不可用时阻塞 goroutine 退出。
func (m *QuotaMiddleware) releaseAll(tokens []releaseToken) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := len(tokens) - 1; i >= 0; i-- {
		m.store.ReleaseConcurrency(ctx, tokens[i].key)
	}
}

// extractModelParam 从请求中提取模型组名（request body 中的 model 字段）。
//
// 配额中间件注册在 TraceCapture 之后（router.go:159 TraceCapture → :171 OrgCheck → 路由内 quotaMW），
// 故优先复用 TraceCapture 已捕获并恢复 body 后存入 context 的 trace_req_body（[]byte，已 sanitize，
// 但 model 字段是短字符串不会被截断）。trace_req_body 可能因 log_enabled=false 而未设置，
// 此时回退自行读取 c.Request.Body 并恢复（NopCloser），保证 handler 仍能读到完整 body。
// 兜底再读 query 参数 ?model=。
func extractModelParam(c *gin.Context) string {
	// 1) 兼容旧约定：显式 set 的 request_model
	if v, ok := c.Get("request_model"); ok {
		if m, ok := v.(string); ok && m != "" {
			return m
		}
	}
	// 2) 复用 TraceCapture 已捕获的 trace_req_body（已恢复给 handler，读它不影响后续）
	if v, ok := c.Get("trace_req_body"); ok {
		if raw, ok := v.([]byte); ok && len(raw) > 0 {
			if m := modelFromJSON(raw); m != "" {
				return m
			}
		}
	}
	// 3) 自行读取并恢复 body
	if c.Request != nil && c.Request.Body != nil {
		if body, err := io.ReadAll(c.Request.Body); err == nil {
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			if m := modelFromJSON(body); m != "" {
				return m
			}
		}
	}
	// 4) 兜底：query 参数
	return c.Query("model")
}

// modelFromJSON 从请求体 JSON 中提取顶层 "model" 字段（短字符串，O(1) 解析）。
// 仅取顶层字段，避免解析整个大 body；非法 JSON 返回空。
func modelFromJSON(body []byte) string {
	var p struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	return p.Model
}

// ModelQuotaGate 模型层配额检查（在路由选定具体 model 之后调用）。
type ModelQuotaGate struct {
	holder *SnapshotHolder
	store  *RedisStore
}

// NewModelQuotaGate 创建模型层配额检查器。
func NewModelQuotaGate(holder *SnapshotHolder, store *RedisStore) *ModelQuotaGate {
	return &ModelQuotaGate{holder: holder, store: store}
}

// BeforeUpstream 在转发上游之前检查模型层配额。
// 返回 nil=通过，非 nil=拒绝（已构造 429 响应）。
func (g *ModelQuotaGate) BeforeUpstream(c *gin.Context, subjectType, subjectID, modelUUID string) error {
	snap := g.holder.Get()
	limits := ResolveModelLimits(snap, modelUUID)
	ctx := c.Request.Context()

	rpmKey := RPMKey(subjectType, subjectID, "model", modelUUID)
	concKey := ConcurrencyKey(subjectType, subjectID, "model", modelUUID)

	// 先检查并发再检查 RPM，与 QuotaMiddleware 保持一致的顺序：
	// RPM 检查写入 ZADD 不可回滚，并发失败时不应产生 RPM 条目泄漏。
	// 并发 acquire
	if err := g.store.AcquireConcurrency(ctx, concKey, limits.MaxConcurrency); err != nil {
		WriteQuotaRejected(c, quota.QuotaViolation{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Layer:       "model",
			Gate:        "concurrency",
			Limit:       limits.MaxConcurrency,
		})
		return err
	}

	// RPM 检查
	if err := g.store.CheckRPM(ctx, rpmKey, limits.RPM); err != nil {
		// RPM 失败需要回滚已获取的并发许可
		g.store.ReleaseConcurrency(ctx, concKey)
		WriteQuotaRejected(c, quota.QuotaViolation{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Layer:       "model",
			Gate:        "rpm",
			Limit:       limits.RPM,
		})
		return err
	}

	// 存储模型层并发键供释放
	if tokens, ok := c.Get("quota_release_tokens"); ok {
		if t, ok := tokens.([]releaseToken); ok {
			c.Set("quota_release_tokens", append(t, releaseToken{key: concKey}))
		}
	} else {
		c.Set("quota_release_tokens", []releaseToken{{key: concKey}})
	}

	slog.Debug("quota: model gate passed",
		"subject_type", subjectType,
		"subject_id", subjectID,
		"model_uuid", modelUUID,
		"rpm", limits.RPM,
		"concurrency", limits.MaxConcurrency,
	)

	return nil
}
