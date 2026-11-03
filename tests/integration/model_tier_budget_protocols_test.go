//go:build integration

package integration_test

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

type integrationProtocolOrchestrator struct{ calls atomic.Int32 }

func (o *integrationProtocolOrchestrator) Chat(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	o.calls.Add(1)
	return &core.ChatResponse{}, nil
}
func (o *integrationProtocolOrchestrator) ChatStream(context.Context, *core.ChatRequest) (io.ReadCloser, error) {
	o.calls.Add(1)
	return io.NopCloser(strings.NewReader("data: [DONE]\n\n")), nil
}
func (o *integrationProtocolOrchestrator) Responses(context.Context, *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	o.calls.Add(1)
	return &core.ResponsesResponse{}, nil
}
func (o *integrationProtocolOrchestrator) ResponsesStream(context.Context, *core.ResponsesRequest) (io.ReadCloser, error) {
	o.calls.Add(1)
	return io.NopCloser(strings.NewReader("data: [DONE]\n\n")), nil
}
func (o *integrationProtocolOrchestrator) Embeddings(context.Context, *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	o.calls.Add(1)
	return &core.EmbeddingResponse{}, nil
}
func (o *integrationProtocolOrchestrator) Rerank(context.Context, *core.RerankRequest) (*core.RerankResponse, error) {
	o.calls.Add(1)
	return &core.RerankResponse{}, nil
}

type integrationProtocolScheduler struct{}

func (integrationProtocolScheduler) Route(string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return &gwscheduler.RouteDecision{ModelID: "model-1", ModelName: "upstream-model"}, nil
}
func (integrationProtocolScheduler) Failover(string, string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return nil, errors.New("unused")
}
func (integrationProtocolScheduler) FailoverExcluding(string, []string, *core.ChatRequest) (*gwscheduler.RouteDecision, error) {
	return nil, errors.New("unused")
}
func (integrationProtocolScheduler) GetGroupConfig(string) *gwscheduler.GroupRoutingConfig {
	return nil
}
func (integrationProtocolScheduler) NotifyFailure(string)              {}
func (integrationProtocolScheduler) SetCooldown(string, time.Duration) {}
func (integrationProtocolScheduler) PersistSticky(context.Context, string, string, *gwscheduler.RouteDecision) {
}
func (integrationProtocolScheduler) UpdateSticky(context.Context, string, string, string, string) bool {
	return false
}

func TestModelTierBudgetIntegration_RealHandlersEnforceGateOrderForSixProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	infra := startIntegrationInfra(t)
	protocols := []struct{ name, path, body string }{
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
				require.NoError(t, infra.Redis.FlushDB(context.Background()).Err())
				now := time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC)
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
				budgetGate := budget.NewGate(budgetHolder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now })
				quotaHolder := quota.NewSnapshotHolder()
				quotaHolder.Set(&corequota.QuotaSnapshot{Version: 1, DefaultUserRPM: 1})
				quotaGate := quota.NewMiddleware(quotaHolder, quota.NewRedisStore(infra.Redis))
				if stage.blockQuota {
					require.NoError(t, infra.Redis.ZAdd(context.Background(), quota.RPMKey("user", "u-1", "", ""),
						redis.Z{Score: float64(time.Now().UnixMilli()), Member: "existing"}).Err())
				}
				nameCache := cache.NewGroupNameCache("")
				nameCache.SetNames([]string{"premium"})
				dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(
					integrationProtocolScheduler{}, nameCache, nil, budget.NewTierResolver(budgetHolder),
				)
				upstream := &integrationProtocolOrchestrator{}
				handle := integrationRealProtocolHandler(protocol.name, upstream, dispatcher, budgetGate, quotaGate)
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
				require.Zero(t, upstream.calls.Load(), "upstream must run after authorization, budget and quota")
			})
		}
	}
}

func integrationRealProtocolHandler(
	protocol string,
	upstream *integrationProtocolOrchestrator,
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
