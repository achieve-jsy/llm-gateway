package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewUsageEvent_ReadsUpstreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	// 模拟 orchestrator 错误路径写入（与 chat.go/stream.go/response.go 一致）
	c.Set("upstream_status", 429)

	event := NewCollector(CollectorConfig{}, nil).NewUsageEvent(c)
	if event.UpstreamStatus != 429 {
		t.Fatalf("UpstreamStatus = %d, want 429", event.UpstreamStatus)
	}
}

func TestNewUsageEvent_UpstreamStatusMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	// 成功请求（orchestrator 成功路径 delete）或网关侧错误（无上游调用）→ 无该 key
	event := NewCollector(CollectorConfig{}, nil).NewUsageEvent(c)
	if event.UpstreamStatus != 0 {
		t.Fatalf("UpstreamStatus = %d, want 0", event.UpstreamStatus)
	}
}
