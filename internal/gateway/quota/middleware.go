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

// quotaReleaseTokensKey 请求级「待释放并发令牌」在 gin context 中的键。
// 同一请求的各层（middleware 的 subject/group 层、ModelQuotaGate 的 model 层）
// 都往同一个列表追加令牌，请求结束时统一按 lease id 归还。
const quotaReleaseTokensKey = "quota_release_tokens"

// releaseToken 并发释放令牌。
// leaseID 为租约式计数器返回的槽位标识：释放必须按租约 id 精确移除，
// 不能用「释放一次减一」的语义，否则丢失一次归还或重复释放都会影响其他在途请求。
// leaseID 为空表示该层未真正占用（limit<=0 不限，或 Redis fail-open），释放时跳过。
type releaseToken struct {
	key     string
	leaseID string
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
		// 必须用 defer：handler 内 panic / c.Abort 提前返回时也要归还租约，
		// 否则该请求的并发槽位只能等租约到期才自愈。
		defer m.AfterUpstream(c)
		c.Next()
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
		leaseID, err := m.store.AcquireConcurrency(c.Request.Context(), layer.concKey, layer.limits.MaxConcurrency)
		if err != nil {
			m.releaseAll(tokens)
			WriteQuotaRejected(c, quota.QuotaViolation{
				SubjectType: subjectType, SubjectID: subjectID, Layer: layer.layer,
				Gate: "concurrency", Limit: layer.limits.MaxConcurrency,
			})
			return err
		}
		tokens = append(tokens, releaseToken{key: layer.concKey, leaseID: leaseID})
		if err := m.store.CheckRPM(c.Request.Context(), layer.rpmKey, layer.limits.RPM); err != nil {
			m.releaseAll(tokens)
			WriteQuotaRejected(c, quota.QuotaViolation{
				SubjectType: subjectType, SubjectID: subjectID, Layer: layer.layer,
				Gate: "rpm", Limit: layer.limits.RPM,
			})
			return err
		}
	}
	c.Set(quotaReleaseTokensKey, tokens)
	c.Set("quota_subject_type", subjectType)
	c.Set("quota_subject_id", subjectID)
	return nil
}

func (m *QuotaMiddleware) AfterUpstream(c *gin.Context) {
	if final, ok := c.Get(quotaReleaseTokensKey); ok {
		if tokens, ok := final.([]releaseToken); ok {
			m.releaseAll(tokens)
		}
	}
	c.Set(quotaReleaseTokensKey, []releaseToken{})
}

// releaseAll 逆序释放所有并发租约（按 lease id 精确 ZREM）。
// 使用 5s 超时避免 Redis 不可用时阻塞 goroutine 退出。
func (m *QuotaMiddleware) releaseAll(tokens []releaseToken) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := len(tokens) - 1; i >= 0; i-- {
		m.store.ReleaseConcurrency(ctx, tokens[i].key, tokens[i].leaseID)
	}
}

// findToken 在本次请求已持有的令牌列表中按 key 查找（返回最后一个匹配项）。
//
// 用途：同一请求内对同一 key 重复 acquire 时必须复用同一个租约，只占一个槽。
// 触发场景：failover / 重试路径会重复调用 ModelQuotaGate.BeforeUpstream
// （internal/gateway/handler/chat.go、messages.go 的多个调用点），而释放只发生一次。
func findToken(c *gin.Context, key string) (releaseToken, bool) {
	v, ok := c.Get(quotaReleaseTokensKey)
	if !ok {
		return releaseToken{}, false
	}
	tokens, ok := v.([]releaseToken)
	if !ok {
		return releaseToken{}, false
	}
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].key == key && tokens[i].leaseID != "" {
			return tokens[i], true
		}
	}
	return releaseToken{}, false
}

// appendToken 把令牌追加进 context 中的令牌列表（供释放阶段统一归还）。
func appendToken(c *gin.Context, tok releaseToken) {
	if tokens, ok := c.Get(quotaReleaseTokensKey); ok {
		if t, ok := tokens.([]releaseToken); ok {
			c.Set(quotaReleaseTokensKey, append(t, tok))
			return
		}
	}
	c.Set(quotaReleaseTokensKey, []releaseToken{tok})
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
//
// 幂等语义：同一请求内对同一 concKey 重复调用本方法时复用首次获取的租约
// （只占一个槽、不续期、也不重复登记令牌）。failover / 重试路径
// （internal/gateway/handler/chat.go、messages.go 的多个调用点）会重复触发本方法，
// 而释放只发生一次；若不幂等，同一次请求会白占两个槽并永久泄漏一个。
func (g *ModelQuotaGate) BeforeUpstream(c *gin.Context, subjectType, subjectID, modelUUID string) error {
	snap := g.holder.Get()
	limits := ResolveModelLimits(snap, modelUUID)
	ctx := c.Request.Context()

	rpmKey := RPMKey(subjectType, subjectID, "model", modelUUID)
	concKey := ConcurrencyKey(subjectType, subjectID, "model", modelUUID)

	// 先检查并发再检查 RPM，与 QuotaMiddleware 保持一致的顺序：
	// RPM 检查写入 ZADD 不可回滚，并发失败时不应产生 RPM 条目泄漏。
	//
	// 并发 acquire：成功时拿到租约 id，后续释放与回滚都必须用它。
	// 若本请求已持有该 key 的租约，则直接复用（幂等），既不重复占槽，
	// 也绝不延长旧条目的有效期 —— 并发占用的唯一权威是各条目自己的到期时间。
	//
	// 租约粒度 = (request, key)：同一请求内对**同一** key 重复 acquire 复用同一租约；
	// failover 切到**不同** model key 时各 key 各自独立获取（key 不同即不复用）。
	//
	// 语义契约「失败即不持有」：两条失败分支（并发超限、RPM 超限）都会在 return 前
	// 归还本次新占用的租约，且**不登记令牌**。因此失败时本请求在 concKey 上不持有任何占用，
	// 也就不会出现「拿不到 lease id 用于复用」的问题 —— 调用方拿到 err 后不应对本层做任何释放。
	leaseID := ""
	reused := false
	if tok, ok := findToken(c, concKey); ok {
		leaseID = tok.leaseID
		reused = true
	} else {
		acquired, err := g.store.AcquireConcurrency(ctx, concKey, limits.MaxConcurrency)
		if err != nil {
			WriteQuotaRejected(c, quota.QuotaViolation{
				SubjectType: subjectType,
				SubjectID:   subjectID,
				Layer:       "model",
				Gate:        "concurrency",
				Limit:       limits.MaxConcurrency,
			})
			return err
		}
		leaseID = acquired
	}

	// RPM 检查
	if err := g.store.CheckRPM(ctx, rpmKey, limits.RPM); err != nil {
		// RPM 失败需要回滚本次新获取的并发租约（按 lease id 精确移除）。
		// 若租约是从本请求已有令牌复用而来，则不能在此移除：本次调用并未新占槽，
		// 且该租约仍被请求（失败前的处理路径/已成功的层）持有，需留给正常释放路径。
		if !reused {
			g.store.ReleaseConcurrency(ctx, concKey, leaseID)
		}
		WriteQuotaRejected(c, quota.QuotaViolation{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Layer:       "model",
			Gate:        "rpm",
			Limit:       limits.RPM,
		})
		return err
	}

	// 仅新占用的租约才登记令牌；复用时不重复登记，避免重复释放与令牌列表膨胀
	if !reused {
		appendToken(c, releaseToken{key: concKey, leaseID: leaseID})
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
