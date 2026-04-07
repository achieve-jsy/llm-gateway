package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/stretchr/testify/require"
)

func TestBudgetAlertClient_ReturnsErrorForNon2xxResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "internal-token", r.Header.Get("X-Internal-Token"))
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	client := server.NewBudgetAlertClient(&http.Client{Timeout: time.Second}, upstream.URL, "internal-token")

	err := client.Send(context.Background(), server.BudgetAlertRequest{
		ScopeType: "member", ScopeID: "u-1", PeriodType: "monthly", DimensionType: "tier", ModelTierID: "t-1",
	})

	require.ErrorContains(t, err, "503")
}
