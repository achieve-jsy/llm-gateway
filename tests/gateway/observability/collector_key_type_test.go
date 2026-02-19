package observability_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

func TestCollector_NewUsageEvent_KeyType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		ctxKeyType string
		want       string
	}{
		{name: "service key", ctxKeyType: "service", want: "service"},
		{name: "personal key", ctxKeyType: "personal", want: "personal"},
		{name: "empty defaults personal", ctxKeyType: "", want: "personal"},
		{name: "unknown defaults personal", ctxKeyType: "other", want: "personal"},
	}

	col := observability.NewCollector(observability.CollectorConfig{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Set("key_type", tt.ctxKeyType)
			c.Set("request_id", "req-1")

			ev := col.NewUsageEvent(c)
			assert.Equal(t, tt.want, ev.KeyType)
		})
	}
}

func TestSanitizeCallLogForWrite_ServiceKeyClearsUser(t *testing.T) {
	cl := obsmodel.CallLog{
		KeyType:  obsmodel.KeyTypeService,
		UserID:   "user-1",
		UserName: "admin",
	}
	observability.SanitizeCallLogForWrite(&cl)
	assert.Empty(t, cl.UserID)
	assert.Empty(t, cl.UserName)

	cl2 := obsmodel.CallLog{
		KeyType:  obsmodel.KeyTypePersonal,
		UserID:   "user-1",
		UserName: "alice",
	}
	observability.SanitizeCallLogForWrite(&cl2)
	assert.Equal(t, "user-1", cl2.UserID)
	assert.Equal(t, "alice", cl2.UserName)
}
