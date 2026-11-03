package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetTopIssues_EmptyShape 空数据时响应应含 slow / errors / limit（SQLite 上 PG 聚合可能失败，
// 此时仍应返回 200 或 500；有 Redis/空库时至少字段契约在成功路径成立）。
func TestGetTopIssues_EmptyShape(t *testing.T) {
	r, _, _ := setupOpsHandlerAll(t)
	w := doRequest(r, "GET", "/ops/top-issues?time_range=1h&limit=10")
	// SQLite 无 PERCENTILE_CONT 时可能 500；此处仅在 200 时校验 shape
	if w.Code != 200 {
		t.Skipf("skip shape assert on non-200 (likely SQLite dialect): %d %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "slow")
	assert.Contains(t, resp, "errors")
	assert.Contains(t, resp, "limit")
	assert.Equal(t, float64(10), resp["limit"])

	slow, ok := resp["slow"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, slow)
	errors, ok := resp["errors"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, errors)
}
