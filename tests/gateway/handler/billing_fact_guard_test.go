package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rejectingBillingReadiness struct{}

func (rejectingBillingReadiness) CanAcceptBillableRequest() bool { return false }

func TestBillingFactGuard_Returns503OnlyForBillableRequestsAtHighWatermark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(server.BillingFactGuard(rejectingBillingReadiness{}))
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	billable := httptest.NewRecorder()
	router.ServeHTTP(billable, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusServiceUnavailable, billable.Code)
	require.Contains(t, billable.Body.String(), "billing_fact_store_backpressure")

	readOnly := httptest.NewRecorder()
	router.ServeHTTP(readOnly, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	require.Equal(t, http.StatusNoContent, readOnly.Code)
}
