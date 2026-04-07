package accounting_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixedPricing struct{}

func (fixedPricing) Resolve(string) (core.ModelPricing, bool) {
	price := 1_000_000.0
	return core.ModelPricing{InputPerMtok: &price, OutputPerMtok: &price}, true
}

func TestUsageCollector_AccountsBeforeBuildingCallLogEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:account_before_log?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	collector := gwobs.NewCollector(gwobs.CollectorConfig{
		Enabled: true, BufferSize: 8, FlushN: 1, FlushInterval: 10 * time.Millisecond,
	}, fixedPricing{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))
	hookStarted := make(chan struct{})
	releaseHook := make(chan struct{})
	hook := func(_ *gin.Context, _ float64, _ int) {
		close(hookStarted)
		<-releaseHook
	}
	router := gin.New()
	router.Use(middleware.UsageCollector(collector, nil, hook))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("request_id", "req-account-order")
		c.Set("model", "premium")
		c.Set("usage", core.Usage{PromptTokens: 1, TotalTokens: 1})
		c.Set("stream", false)
		c.Set(budget.CtxKeyAccountingPlan, &budget.AccountingPlan{RedisCheckSucceeded: true})
		c.Status(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		close(requestDone)
	}()
	<-hookStarted
	time.Sleep(30 * time.Millisecond)
	var count int64
	require.NoError(t, db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "req-account-order").Count(&count).Error)
	require.Zero(t, count, "call log must not emit before synchronous accounting finishes")
	close(releaseHook)
	<-requestDone
	require.Equal(t, http.StatusOK, recorder.Code)

	require.Eventually(t, func() bool {
		var row obsmodel.CallLog
		if db.First(&row, "request_id = ?", "req-account-order").Error != nil {
			return false
		}
		return row.RequestID == "req-account-order"
	}, time.Second, 10*time.Millisecond)
}

func TestUsageCollector_StreamingAccountsExactlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	collector := gwobs.NewCollector(gwobs.CollectorConfig{
		Enabled: true, BufferSize: 8, FlushN: 8, FlushInterval: time.Second,
	}, fixedPricing{})
	router := gin.New()
	router.Use(middleware.UsageCollector(collector, nil, func(*gin.Context, float64, int) {
		calls.Add(1)
	}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("model", "premium")
		c.Set("stream", true)
		c.Set("usage", core.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
		budget.SetAccountingPlan(c, &budget.AccountingPlan{RedisCheckSucceeded: true})
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: done\n\n")
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, int32(1), calls.Load())
}

func TestUsageCollector_DoesNotInventCostWithoutEstimator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	var capturedCost atomic.Uint64
	router := gin.New()
	router.Use(middleware.UsageCollector(nil, nil, func(_ *gin.Context, cost float64, _ int) {
		calls.Add(1)
		capturedCost.Store(math.Float64bits(cost))
	}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("model", "premium")
		c.Set("usage", core.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
		budget.SetAccountingPlan(c, &budget.AccountingPlan{RedisCheckSucceeded: true})
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Zero(t, calls.Load())
	require.Zero(t, math.Float64frombits(capturedCost.Load()))
}

func TestUsageCollector_StreamingSettlementKeepsRequestPlanAcrossHotUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	oldTier := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	newTier := uuid.MustParse("7534a819-ceb7-4ce8-a62f-bf78627eabb7")
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"premium": {TierID: oldTier, TierName: "旧等级", Enabled: true},
		},
	})
	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), fixedPricing{})
	var settlements atomic.Int32
	router := gin.New()
	router.Use(middleware.UsageCollector(collector, nil, func(c *gin.Context, cost float64, _ int) {
		settlements.Add(1)
		plan, ok := budget.GetAccountingPlan(c)
		require.True(t, ok)
		micro, err := budget.MicroUSDFromFloat64(cost)
		require.NoError(t, err)
		require.NoError(t, gate.Account(context.Background(), plan, micro).V2Error)
	}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
			TierLimits: []gwmodel.TierLimit{
				{TierID: oldTier.String(), PeriodType: "monthly", EffectiveLimitUSD: "10", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
				{TierID: newTier.String(), PeriodType: "monthly", EffectiveLimitUSD: "10", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			},
		}}})
		tier, ok := budget.NewTierResolver(holder).Resolve("premium")
		require.True(t, ok)
		budget.SetTierContext(c, tier)
		_, err := gate.Check(c)
		require.NoError(t, err)
		holder.Set(&configsync.BudgetTierSnapshot{
			SchemaVersion: 1, Revision: 2, MasterEnabled: true, AccountingEnabled: true,
			Timezone: "Asia/Shanghai", WeekStart: "monday",
			GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
				"premium": {TierID: newTier, TierName: "新等级", Enabled: true},
			},
		})
		c.Set("model", "premium")
		c.Set("stream", true)
		c.Set("usage", core.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: [DONE]\n\n")
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	require.Equal(t, int32(1), settlements.Load())
	require.Equal(t, int64(1), rdb.Exists(context.Background(),
		budget.TierUsedKey("member", "u-1", oldTier.String(), "2026-07")).Val())
	require.Equal(t, int64(0), rdb.Exists(context.Background(),
		budget.TierUsedKey("member", "u-1", newTier.String(), "2026-07")).Val())
}
