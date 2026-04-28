package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogsSeedanceDefaultsDateRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage-logs?seedance=true&page=1&page_size=50", nil)
	from, to, ok := parseDateRangeWithDefault(c, true)
	if !ok {
		t.Fatalf("seedance date range should default")
	}
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		t.Fatalf("invalid default range: from=%s to=%s", from, to)
	}

	w := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage-logs?page=1&page_size=50", nil)
	_, _, ok = parseDateRangeWithDefault(c, false)
	if ok || w.Code != http.StatusBadRequest {
		t.Fatalf("plain logs without dates ok=%v status=%d body=%s", ok, w.Code, w.Body.String())
	}
}
