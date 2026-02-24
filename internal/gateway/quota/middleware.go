package quota

import (
	"context"
	"log/slog"

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
		// 系统探针 key（__system_ 前缀）豁免
		if v, ok := c.Get("key_name"); ok {
			if name, ok := v.(string); ok && len(name) >= 9 && name[:9] == "__system_" {
				c.Next()
				return
			}
		}

		identityVal, exists := c.Get("identity")
		if !exists {
			c.Next()
			return
		}
		id, ok := identityVal.(*gwmodel.UserIdentity)
		if !ok {
			c.Next()
			return
		}

		subjectType, subjectID := ResolveSubject(id)
		snap := m.holder.Get()

		// 模型组名 = 请求中的 model 参数（即组名）
		groupName := extractModelParam(c)

		// 检查层列表
		type layerCheck struct {
			layer   string
			limits  EffectiveResult
			rpmKey  string
			concKey string
		}
		layers := []layerCheck{
			{
				layer:   "subject",
				limits:  ResolveSubjectLimits(snap, subjectType, subjectID),
				rpmKey:  RPMKey(subjectType, subjectID, "", ""),
				concKey: ConcurrencyKey(subjectType, subjectID, "", ""),
			},
		}
		if groupName != "" {
			layers = append(layers, layerCheck{
				layer:   "group",
				limits:  ResolveGroupLimits(snap, groupName),
				rpmKey:  RPMKey(subjectType, subjectID, "group", groupName),
				concKey: ConcurrencyKey(subjectType, subjectID, "group", groupName),
			})
		}

		// 逐层检查并 acquire
		tokens := []releaseToken{}
		ctx := c.Request.Context()

		for _, l := range layers {
			// RPM 检查
			if err := m.store.CheckRPM(ctx, l.rpmKey, l.limits.RPM); err != nil {
				m.releaseAll(tokens)
				WriteQuotaRejected(c, quota.QuotaViolation{
					SubjectType: subjectType,
					SubjectID:   subjectID,
					Layer:       l.layer,
					Gate:        "rpm",
					Limit:       l.limits.RPM,
				})
				return
			}
			// 并发 acquire
			if err := m.store.AcquireConcurrency(ctx, l.concKey, l.limits.MaxConcurrency); err != nil {
				m.releaseAll(tokens)
				WriteQuotaRejected(c, quota.QuotaViolation{
					SubjectType: subjectType,
					SubjectID:   subjectID,
					Layer:       l.layer,
					Gate:        "concurrency",
					Limit:       l.limits.MaxConcurrency,
				})
				return
			}
			tokens = append(tokens, releaseToken{key: l.concKey})
		}

		// 存储 tokens 供后续释放（handler 完成后）
		c.Set("quota_release_tokens", tokens)
		c.Set("quota_subject_type", subjectType)
		c.Set("quota_subject_id", subjectID)

		// 执行 handler
		c.Next()

		// 释放并发（含流式：在 flush 完成后的 defer）
		m.releaseAll(tokens)
	}
}

// releaseAll 逆序释放所有并发令牌。
func (m *QuotaMiddleware) releaseAll(tokens []releaseToken) {
	ctx := context.Background()
	for i := len(tokens) - 1; i >= 0; i-- {
		m.store.ReleaseConcurrency(ctx, tokens[i].key)
	}
}

// extractModelParam 从请求中提取模型组名（request body 中的 model 字段）。
// 在 auth middleware 之后，request body 已经被解析过一次。
// 这里尝试从 gin context 获取（需要 TraceCapture 或 body parser 设置）。
func extractModelParam(c *gin.Context) string {
	// 优先从已解析的 body 获取
	if v, ok := c.Get("request_model"); ok {
		if m, ok := v.(string); ok {
			return m
		}
	}
	// 回退：从 query 参数获取
	return c.Query("model")
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

	// RPM 检查
	if err := g.store.CheckRPM(ctx, rpmKey, limits.RPM); err != nil {
		WriteQuotaRejected(c, quota.QuotaViolation{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Layer:       "model",
			Gate:        "rpm",
			Limit:       limits.RPM,
		})
		return err
	}

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
