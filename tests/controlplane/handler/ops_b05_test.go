package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveRequestError_Degraded409 request-errors resolve 应返回 409（B05b 降级方案）
func TestResolveRequestError_Degraded409(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/request-errors/req-xxx/resolve", `{"resolved":true}`)
	assertStatus(t, w, 409)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "error")
}

// TestResolveUpstreamError_Degraded409 upstream-errors resolve 应返回 409（B05b 降级方案）
func TestResolveUpstreamError_Degraded409(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/upstream-errors/req-yyy/resolve", `{"resolved":true}`)
	assertStatus(t, w, 409)
}
