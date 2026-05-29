package observability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// nulEscape 是 Go json.Marshal 对 NUL 字节产生的转义序列字面量（6 字节：）。
func nulEscape() string {
	return string([]byte{'\\', 'u', '0', '0', '0', '0'})
}

func TestScrubNULEscapes(t *testing.T) {
	esc := nulEscape()
	repl := string([]byte{'\\', 'u', 'f', 'f', 'f', 'd'})

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no NUL", `{"content":"hello"}`, `{"content":"hello"}`},
		{"escaped NUL in string", `{"content":"a` + esc + `b"}`, `{"content":"a` + repl + `b"}`},
		{"NUL as only content", `"` + esc + `"`, `"` + repl + `"`},
		{"multiple NULs", `{"a":"` + esc + esc + `","b":1}`, `{"a":"` + repl + repl + `","b":1}`},
		{"unrelated unicode untouched", `{"s":"é你"}`, `{"s":"é你"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scrubNULEscapes([]byte(c.in))
			assert.Equal(t, c.want, string(got))
		})
	}
}

// TestScrubNULEscapes_MarshalOutput 模拟生产路径：Go json.Marshal 把 NUL 字节
// 编码成 NUL 转义，验证 scrub 能将其还原为合法 JSON 且不含 NUL 转义。
func TestScrubNULEscapes_MarshalOutput(t *testing.T) {
	orig, err := json.Marshal(map[string]string{"content": "a\x00b"})
	require.NoError(t, err)
	require.Contains(t, string(orig), nulEscape()) // 确认触发条件存在

	got := scrubNULEscapes(orig)
	assert.NotContains(t, string(got), nulEscape())
	assert.Contains(t, string(got), string([]byte{'\\', 'u', 'f', 'f', 'f', 'd'}))

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(got, &decoded)) // scrub 后仍是合法 JSON
	assert.Equal(t, "a�b", decoded["content"])
}

func TestTraceEventToModel_ScrubsNULFromAllBodies(t *testing.T) {
	// 构造与生产一致的输入：json.Marshal 编码含 NUL 的结构
	mk := func(v any) []byte {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return b
	}
	e := TraceEvent{
		RequestID:         "req-1",
		TS:                time.Now(),
		RequestHeadersRaw: mk(map[string]string{"x-key": "\x00hdr"}),
		RequestBodyRaw:    mk(map[string]any{"messages": []map[string]string{{"role": "user", "content": "a\x00b"}}}),
		ResponseBodyRaw:   mk(map[string]string{"content": "ok\x00done"}),
	}
	m := traceEventToModel(e)
	require.NotEmpty(t, m.RequestHeaders)
	require.NotEmpty(t, m.RequestBody)
	require.NotEmpty(t, m.ResponseBody)

	assert.NotContains(t, string(m.RequestHeaders), nulEscape())
	assert.NotContains(t, string(m.RequestBody), nulEscape())
	assert.NotContains(t, string(m.ResponseBody), nulEscape())

	var reqBody, respBody map[string]any
	require.NoError(t, json.Unmarshal(m.RequestBody, &reqBody))
	require.NoError(t, json.Unmarshal(m.ResponseBody, &respBody))
	assert.Equal(t, "a�b", reqBody["messages"].([]any)[0].(map[string]any)["content"])
	assert.Equal(t, "ok�done", respBody["content"])
}

// batchFailingDB 注册一个 BeforeCreate 钩子：当目标是批量 slice 时返回错误，
// 模拟"单条坏记录导致整批回滚"的 PostgreSQL 行为；单条插入放行。
func batchFailingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_batch", func(d *gorm.DB) {
		if d.Statement.Schema == nil {
			return
		}
		if _, ok := d.Statement.Dest.(*[]obsmodel.CallTrace); ok {
			d.AddError(gorm.ErrInvalidData)
		}
	}))
	return db
}

func TestTraceWriter_FallsBackToPerRecordOnBatchError(t *testing.T) {
	db := batchFailingDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	tw := NewTraceWriter(db, 64, 4, time.Hour)
	go tw.Run(ctx)

	now := time.Now()
	for i := 0; i < 4; i++ {
		tw.Emit(TraceEvent{
			RequestID:       "req-" + string(rune('a'+i)),
			TS:              now,
			RequestBodyRaw:  []byte(`{"ok":true}`),
			ResponseBodyRaw: []byte(`{"content":"fine"}`),
		})
	}
	// flushN=4 触发批量写入 → 批量失败 → 逐条插入全部成功
	time.Sleep(200 * time.Millisecond)
	cancel()

	var n int64
	require.NoError(t, db.Model(&obsmodel.CallTrace{}).Count(&n).Error)
	assert.Equal(t, int64(4), n)
}
