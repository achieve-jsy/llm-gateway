package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	gwscheduler "github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type failFirstRedisCommandHook struct{ failed atomic.Bool }

func (h *failFirstRedisCommandHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *failFirstRedisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.failed.CompareAndSwap(false, true) {
			return errors.New("transient redis infrastructure failure")
		}
		return next(ctx, cmd)
	}
}

func (h *failFirstRedisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type countingQuotaGate struct {
	checks   int
	releases int
}

func (g *countingQuotaGate) BeforeUpstream(*gin.Context, string) error {
	g.checks++
	return nil
}

func (g *countingQuotaGate) AfterUpstream(*gin.Context) {
	g.releases++
}

func TestPrepareAuthorizedBudget_DispatchesBeforeBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	ctx.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0", Enabled: true, EffectiveAt: now.Add(-time.Hour)}},
	}}})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true, ZeroBlockEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday"})
	mr := miniredis.RunT(t)
	gate := budget.NewGate(holder, budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()})), nil, func() time.Time { return now })
	dispatched := false

	err := handler.PrepareAuthorizedBudget(ctx, "premium", func() error {
		dispatched = true
		budget.SetTierContext(ctx, budget.TierContext{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true})
		return nil
	}, gate)
	require.True(t, dispatched)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
}

func TestPrepareAuthorizedBudget_UnauthorizedSkipsBudget(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	unauthorized := errors.New("forbidden")
	err := handler.PrepareAuthorizedBudget(ctx, "premium", func() error { return unauthorized }, nil)
	require.ErrorIs(t, err, unauthorized)
	_, ok := budget.GetAccountingPlan(ctx)
	require.False(t, ok)
}

func TestBudgetGate_RebuildLockReturns503AndSuccessfulCheckReservesInflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := budget.NewGate(holder, store, nil, func() time.Time { return now })
	newContext := func(requestID string) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Set("request_id", requestID)
		ctx.Set("identity", &gwmodel.UserIdentity{UserID: "u-1", BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-1", AmountUSD: 100,
			DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		}}})
		return ctx
	}
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "owner", time.Minute).Err())
	_, err := gate.Check(newContext("blocked"))
	var unavailable *budget.UnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, "budget_rebuild_in_progress", unavailable.Code)
	require.Equal(t, http.StatusServiceUnavailable, unavailable.StatusCode)
	require.Zero(t, rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())

	require.NoError(t, rdb.Del(context.Background(), budget.RebuildLockKey).Err())
	_, err = gate.Check(newContext("accepted"))
	require.NoError(t, err)
	require.NotNil(t, rdb.ZScore(context.Background(), budget.BillableInflightKey, "accepted").Val())
}

func TestBudgetGate_AdmissionFixesGenerationBeforeRedisCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.Set(context.Background(), budget.ActiveGenerationKey, "gen-current", 0).Err())
	baseKey := budget.TotalUsedKey("member", "u-gen", "2026-07")
	require.NoError(t, rdb.Set(context.Background(), budget.GenerationKey("gen-current", baseKey), "1000000", 0).Err())
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("request_id", "gen-request")
	ctx.Set("identity", &gwmodel.UserIdentity{UserID: "u-gen", BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-gen", AmountUSD: 1,
		DailyAmountUSD: -1, WeeklyAmountUSD: -1,
	}}})

	_, err := gate.Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "gen-current", budget.GetAccountingPlanGeneration(ctx))
	require.Zero(t, rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

func TestBudgetGate_PositiveBudgetRedisInfrastructureFailureFailsOpenWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mr.Close()
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := budget.NewGate(holder, budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	}), nil, time.Now)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("request_id", "production-request-id")
	ctx.Set("identity", &gwmodel.UserIdentity{UserID: "u-fail-open", BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-fail-open", AmountUSD: 10,
		DailyAmountUSD: -1, WeeklyAmountUSD: -1,
	}}})

	outcome, err := gate.Check(ctx)
	require.NoError(t, err)
	require.True(t, outcome.FailOpen)
	require.Equal(t, budget.AdmissionInfrastructureFailOpen, budget.GetAdmissionOutcome(ctx))
	require.Nil(t, budget.GetBillableReservation(ctx))
}

func TestBudgetGate_AdmissionFailOpenRemainsStickyAfterRedisCheckRecovers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rdb.AddHook(&failFirstRedisCommandHook{})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := budget.NewGate(holder, budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	}), nil, time.Now)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("request_id", "transient-recovery-request")
	ctx.Set("identity", &gwmodel.UserIdentity{UserID: "u-transient", BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-transient", AmountUSD: 10,
		DailyAmountUSD: -1, WeeklyAmountUSD: -1,
	}}})

	outcome, err := gate.Check(ctx)
	require.NoError(t, err)
	require.True(t, outcome.FailOpen)
	plan, ok := budget.GetAccountingPlan(ctx)
	require.True(t, ok)
	require.True(t, plan.RedisCheckSucceeded, "Redis check should recover after admission failure")
	require.True(t, plan.AdmissionInfrastructureFailOpen)
	require.True(t, ctx.GetBool(budget.CtxKeyAdmissionInfrastructureFailOpen))
	require.Nil(t, budget.GetBillableReservation(ctx))

	result := gate.Account(context.Background(), plan, 321)
	require.NoError(t, result.V2Error)
	require.Zero(t, rdb.Exists(context.Background(), plan.Constraints[0].Key).Val())
}

func TestBudgetWriteHTTPError_WritesRebuild503(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	require.True(t, budget.WriteHTTPError(ctx, &budget.UnavailableError{
		Code: "budget_rebuild_in_progress", StatusCode: http.StatusServiceUnavailable,
	}))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "budget_rebuild_in_progress")
}

func TestPrepareAuthorizedGates_SixProtocolAuthorizationMatrixSkipsRedisGates(t *testing.T) {
	for _, endpoint := range []string{
		"/v1/chat/completions", "/v1/responses", "/v1/messages",
		"/v1/embeddings", "/v1/rerank", "/v1/rerank?model_type=rerank_vl",
	} {
		t.Run(endpoint, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", endpoint, nil)
			quotaGate := &countingQuotaGate{}
			unauthorized := errors.New("forbidden")
			release, err := handler.PrepareAuthorizedGates(
				ctx, "premium", func() error { return unauthorized }, nil, quotaGate,
			)
			require.ErrorIs(t, err, unauthorized)
			require.Nil(t, release)
			require.Zero(t, quotaGate.checks)
			require.Zero(t, quotaGate.releases)
			_, planned := budget.GetAccountingPlan(ctx)
			require.False(t, planned)
		})
	}
}

func TestPrepareAuthorizedGates_IntegrationChecksAndAccountsTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	budgetHolder := budget.NewSnapshotHolder()
	budgetHolder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	budgetGate := budget.NewGate(budgetHolder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	quotaGate := quota.NewMiddleware(quota.NewSnapshotHolder(), quota.NewRedisStore(rdb))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("identity", &gwmodel.UserIdentity{
		UserID: "u-1", KeyType: "personal",
		BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
			TierLimits: []gwmodel.TierLimit{{
				TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "10",
				Enabled: true, EffectiveAt: now.Add(-time.Hour),
			}},
		}},
	})
	release, err := handler.PrepareAuthorizedGates(ctx, "premium", func() error {
		budget.SetTierContext(ctx, budget.TierContext{
			TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true,
		})
		return nil
	}, budgetGate, quotaGate)
	require.NoError(t, err)
	require.NotNil(t, release)
	defer release()
	plan, ok := budget.GetAccountingPlan(ctx)
	require.True(t, ok)
	require.True(t, plan.TierAccountingEligible)
	result := budgetGate.Account(context.Background(), plan, 250_000)
	require.NoError(t, result.V2Error)
	used, err := rdb.Get(context.Background(), budget.TierUsedKey("member", "u-1", "tier-1", "2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(250_000), used)
}

type protocolOrchestrator struct{ calls atomic.Int32 }

func (o *protocolOrchestrator) Chat(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	o.calls.Add(1)
	return &core.ChatResponse{}, nil
}
func (o *protocolOrchestrator) ChatStream(context.Context, *core.ChatRequest) (io.ReadCloser, error) {
	o.calls.Add(1)
	return io.NopCloser(strings.NewReader("data: [DONE]\n\n")), nil
}
func (o *protocolOrchestrator) Responses(context.Context, *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	o.calls.Add(1)
	return &core.ResponsesResponse{}, nil
}
func (o *protocolOrchestrator) ResponsesStream(context.Context, *core.ResponsesRequest) (io.ReadCloser, error) {
	o.calls.Add(1)
	return io.NopCloser(strings.NewReader("data: [DONE]\n\n")), nil
}
func (o *protocolOrchestrator) Embeddings(context.Context, *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	o.calls.Add(1)
	return &core.EmbeddingResponse{}, nil
}
func (o *protocolOrchestrator) Rerank(context.Context, *core.RerankRequest) (*core.RerankResponse, error) {
	o.calls.Add(1)
	return &core.RerankResponse{}, nil
}

type protocolScheduler struct{}

func (protocolScheduler) Route(string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return &gwscheduler.RouteDecision{ModelID: "model-1", ModelName: "upstream-model"}, nil
}
func (protocolScheduler) Failover(string, string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return nil, errors.New("unused")
}
func (protocolScheduler) FailoverExcluding(string, []string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return nil, errors.New("unused")
}

func (protocolScheduler) FailoverExcludingVideo(string, []string, bool, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return nil, errors.New("unused")
}
func (protocolScheduler) GetGroupConfig(string) *gwscheduler.GroupRoutingConfig { return nil }
func (protocolScheduler) NotifyFailure(string)                                  {}
func (protocolScheduler) SetCooldown(string, time.Duration)                     {}
func (protocolScheduler) PersistSticky(context.Context, string, string, *gwscheduler.RouteDecision) {
}
func (protocolScheduler) UpdateSticky(context.Context, string, string, string, string) bool {
	return false
}

func TestRealHandlers_EnforceAuthorizationBudgetQuotaBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	protocols := []struct {
		name string
		path string
		body string
	}{
		{"chat", "/v1/chat/completions", `{"model":"premium","messages":[{"role":"user","content":"hi"}]}`},
		{"responses", "/v1/responses", `{"model":"premium","input":"hi"}`},
		{"messages", "/v1/messages", `{"model":"premium","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`},
		{"embeddings", "/v1/embeddings", `{"model":"premium","input":"hi"}`},
		{"rerank", "/v1/rerank", `{"model":"premium","query":"q","documents":["d"]}`},
		{"rerank-vl", "/v1/rerank?model_type=rerank_vl", `{"model":"premium","query":{"text":"q"},"documents":[{"text":"d"}]}`},
	}
	stages := []struct {
		name       string
		usable     []string
		tierLimit  string
		blockQuota bool
		wantStatus int
	}{
		{"authorization", []string{"other"}, "0", true, http.StatusForbidden},
		{"budget", []string{"premium"}, "0", true, http.StatusPaymentRequired},
		{"quota", []string{"premium"}, "10", true, http.StatusTooManyRequests},
	}
	for _, protocol := range protocols {
		for _, stage := range stages {
			t.Run(protocol.name+"/"+stage.name, func(t *testing.T) {
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
				tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
				budgetHolder := budget.NewSnapshotHolder()
				budgetHolder.Set(&configsync.BudgetTierSnapshot{
					SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
					PositiveEnforcementEnabled: true, ZeroBlockEnabled: true,
					Timezone: "Asia/Shanghai", WeekStart: "monday",
					GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
						"premium": {TierID: tierID, TierName: "高级", Enabled: true},
					},
				})
				budgetGate := budget.NewGate(budgetHolder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
				quotaHolder := quota.NewSnapshotHolder()
				quotaHolder.Set(&corequota.QuotaSnapshot{Version: 1, DefaultUserRPM: 1})
				quotaGate := quota.NewMiddleware(quotaHolder, quota.NewRedisStore(rdb))
				if stage.blockQuota {
					require.NoError(t, rdb.ZAdd(context.Background(), quota.RPMKey("user", "u-1", "", ""),
						redis.Z{Score: float64(time.Now().UnixMilli()), Member: "existing"}).Err())
				}
				nameCache := cache.NewGroupNameCache("")
				nameCache.SetNames([]string{"premium"})
				dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(
					protocolScheduler{}, nameCache, nil, budget.NewTierResolver(budgetHolder),
				)
				upstream := &protocolOrchestrator{}
				handle := realProtocolHandler(protocol.name, upstream, dispatcher, budgetGate, quotaGate)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("identity", &gwmodel.UserIdentity{
						KeyID: "key-1", UserID: "u-1", KeyType: "personal", AccessAllGroups: true,
						UsableGroups: stage.usable,
						BudgetChain: []gwmodel.BudgetScope{{
							ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
							TierLimits: []gwmodel.TierLimit{{
								TierID: tierID.String(), PeriodType: "monthly", EffectiveLimitUSD: stage.tierLimit,
								Enabled: true, EffectiveAt: now.Add(-time.Hour),
							}},
						}},
					})
					c.Next()
				})
				router.POST(strings.Split(protocol.path, "?")[0], handle)
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, protocol.path, strings.NewReader(protocol.body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				require.Equal(t, stage.wantStatus, recorder.Code, recorder.Body.String())
				require.Zero(t, upstream.calls.Load())
			})
		}
	}
}

// TestRealHandlers_DirectModelRejected 镜像 exploit 路径：非组名模型（如上游渠道 ID）
// 经真实 handler 必须返回 403 direct_model_not_allowed，而不能直连上游绕过档位预算。
func TestRealHandlers_DirectModelRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	budgetHolder := budget.NewSnapshotHolder()
	budgetHolder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, ZeroBlockEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"premium": {TierID: tierID, TierName: "高级", Enabled: true},
		},
	})
	budgetGate := budget.NewGate(budgetHolder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	quotaHolder := quota.NewSnapshotHolder()
	quotaHolder.Set(&corequota.QuotaSnapshot{Version: 1, DefaultUserRPM: 1})
	quotaGate := quota.NewMiddleware(quotaHolder, quota.NewRedisStore(rdb))
	nameCache := cache.NewGroupNameCache("")
	nameCache.SetNames([]string{"premium"})
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(
		protocolScheduler{}, nameCache, nil, budget.NewTierResolver(budgetHolder),
	)
	upstream := &protocolOrchestrator{}
	handle := realProtocolHandler("chat", upstream, dispatcher, budgetGate, quotaGate)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("identity", &gwmodel.UserIdentity{
			KeyID: "key-1", UserID: "u-1", KeyType: "personal", AccessAllGroups: true,
			UsableGroups: []string{"premium"},
			BudgetChain: []gwmodel.BudgetScope{{
				ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
				TierLimits: []gwmodel.TierLimit{{
					TierID: tierID.String(), PeriodType: "monthly", EffectiveLimitUSD: "100",
					Enabled: true, EffectiveAt: now.Add(-time.Hour),
				}},
			}},
		})
		c.Next()
	})
	router.POST("/v1/chat/completions", handle)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"direct-model","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "direct_model_not_allowed")
	require.Zero(t, upstream.calls.Load())
}

func realProtocolHandler(
	protocol string,
	upstream *protocolOrchestrator,
	dispatcher *orchestrator.GroupAwareDispatcher,
	budgetGate *budget.Gate,
	quotaGate *quota.QuotaMiddleware,
) gin.HandlerFunc {
	switch protocol {
	case "chat":
		h := handler.NewChatHandlerWithGroup(upstream, dispatcher)
		h.SetBudgetGate(budgetGate)
		h.SetRequestQuotaGate(quotaGate)
		return h.Handle
	case "responses":
		h := handler.NewResponsesHandlerWithGroup(upstream, dispatcher)
		h.SetBudgetGate(budgetGate)
		h.SetRequestQuotaGate(quotaGate)
		return h.Handle
	case "messages":
		h := handler.NewMessagesHandlerWithGroup(upstream, dispatcher)
		h.SetBudgetGate(budgetGate)
		h.SetRequestQuotaGate(quotaGate)
		return h.HandleMessages
	case "embeddings":
		h := handler.NewEmbeddingsHandlerWithGroup(upstream, dispatcher)
		h.SetBudgetGate(budgetGate)
		h.SetRequestQuotaGate(quotaGate)
		return h.Handle
	default:
		h := handler.NewRerankHandlerWithGroup(upstream, dispatcher)
		h.SetBudgetGate(budgetGate)
		h.SetRequestQuotaGate(quotaGate)
		return h.Handle
	}
}
