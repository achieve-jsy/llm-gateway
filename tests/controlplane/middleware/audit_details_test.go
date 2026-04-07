package middleware_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type auditCapture struct {
	input audit.Input
}

func (c *auditCapture) Record(input audit.Input) {
	c.input = input
}

func TestAuditRecordsReasonAndBeforeAfterState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capture := &auditCapture{}
	router := gin.New()
	router.Use(middleware.AuditMiddleware(capture))
	router.PUT("/api/v1/model-tiers/:id", func(c *gin.Context) {
		audit.SetBeforeAfter(c, map[string]any{"name": "旧等级"}, map[string]any{"name": "新等级"})
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": c.Param("id")}})
	})

	request := httptest.NewRequest(http.MethodPut, "/api/v1/model-tiers/tier-1", bytes.NewBufferString(`{"name":"新等级","reason":"重新分层"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	details, ok := capture.input.Details.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "重新分层", details["reason"])
	require.Equal(t, map[string]any{"name": "旧等级"}, details["before"])
	require.Equal(t, map[string]any{"name": "新等级"}, details["after"])
}
