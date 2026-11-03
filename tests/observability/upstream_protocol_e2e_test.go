package observability_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 原生旁路（upstream_protocol）可见性端到端：
// 写库 → 用生产查询函数读回 → 断言能按该维度筛出原生行、且不混入转换行。
//
// 契约（三支 agent 共同遵守，改动前先看这里）：
//   - 只有原生直发成功才写值：native_chat / native_responses / native_claude / native_gemini
//   - 降级到 relaykit 转换路径不写 → 落库为空串，**空串即「转换」**
//   - 筛选参数为空 = 不筛选（与 inbound_endpoint 一致）

func newUpstreamProtocolDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每个用例独立的内存库，避免共享缓存串数据
	dsn := fmt.Sprintf("file:upstream_protocol_%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	return db
}

func seedUpstreamProtocolRows(t *testing.T, db *gorm.DB) time.Time {
	t.Helper()
	ts := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	rows := []obsmodel.CallLog{
		{RequestID: "e2e-native-chat", TS: ts, Model: "gpt-4o", KeyType: obsmodel.KeyTypePersonal,
			UpstreamProtocol: "native_chat", Status: 200, PromptTokens: 100, CompletionTokens: 20, EstCostUSD: 0.10},
		{RequestID: "e2e-native-gemini", TS: ts, Model: "gemini-2.5-pro", KeyType: obsmodel.KeyTypePersonal,
			UpstreamProtocol: "native_gemini", Status: 200, PromptTokens: 50, CompletionTokens: 10, EstCostUSD: 0.05},
		// 降级/老路径：不打标，落库空串
		{RequestID: "e2e-relay-converted", TS: ts, Model: "gpt-4o", KeyType: obsmodel.KeyTypePersonal,
			UpstreamProtocol: "", Status: 200, PromptTokens: 300, CompletionTokens: 30, EstCostUSD: 0.30},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	return ts
}

// 聚合维度：原生协议各自成桶，转换行落在空串桶 —— 运维据此算原生占比。
func TestUpstreamProtocolBreakdownSplitsNativeAndConverted(t *testing.T) {
	db := newUpstreamProtocolDB(t)
	ts := seedUpstreamProtocolRows(t, db)

	// 空筛选参数 = 不筛选，拿到全部桶
	got, err := obshandler.QueryUpstreamProtocolBreakdown(db, ts.Add(-time.Hour), ts.Add(time.Hour), "", nil)
	require.NoError(t, err)
	require.Len(t, got, 3)

	byProtocol := make(map[string]obshandler.UpstreamProtocolStat, len(got))
	for _, stat := range got {
		byProtocol[stat.UpstreamProtocol] = stat
	}
	require.Equal(t, int64(1), byProtocol["native_chat"].Calls)
	require.Equal(t, int64(120), byProtocol["native_chat"].TotalTokens)
	require.Equal(t, int64(1), byProtocol["native_gemini"].Calls)
	require.Equal(t, int64(1), byProtocol[""].Calls, "空串桶必须存在且代表转换路径")
	require.Equal(t, int64(330), byProtocol[""].TotalTokens)
}

// 筛「原生」时不能把转换行算进来；这是本次改造要回答的问题本身。
func TestUpstreamProtocolFilterExcludesConvertedRows(t *testing.T) {
	db := newUpstreamProtocolDB(t)
	ts := seedUpstreamProtocolRows(t, db)

	// 复用 GetDashboard 的筛选片段（参数按 (protocol, protocol) 传两次）
	native, err := obshandler.QueryUpstreamProtocolBreakdown(db, ts.Add(-time.Hour), ts.Add(time.Hour),
		obshandler.UpstreamProtocolClause, []interface{}{"native_chat", "native_chat"})
	require.NoError(t, err)
	require.Len(t, native, 1, "只应剩下 native_chat 一个桶")
	require.Equal(t, "native_chat", native[0].UpstreamProtocol)
	require.Equal(t, int64(1), native[0].Calls)

	// 空参数语义是「不筛选」，不是「只看转换」—— 防止有人把它改成等值匹配
	all, err := obshandler.QueryUpstreamProtocolBreakdown(db, ts.Add(-time.Hour), ts.Add(time.Hour),
		obshandler.UpstreamProtocolClause, []interface{}{"", ""})
	require.NoError(t, err)
	require.Len(t, all, 3)

	// 真要只看转换行，直接等值查空串
	converted, err := obshandler.QueryUpstreamProtocolBreakdown(db, ts.Add(-time.Hour), ts.Add(time.Hour),
		"AND upstream_protocol = ?", []interface{}{""})
	require.NoError(t, err)
	require.Len(t, converted, 1)
	require.Equal(t, int64(1), converted[0].Calls)
}

// 投递契约：gateway → 控制面 spool 的 JSON 字段名必须是 upstream_protocol。
func TestUpstreamProtocolSpoolJSONFieldName(t *testing.T) {
	payload, err := json.Marshal(obsmodel.CallLog{RequestID: "e2e-json", UpstreamProtocol: "native_claude"})
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Equal(t, "native_claude", decoded["upstream_protocol"])
	require.Equal(t, "upstream_protocol", obshandler.UpstreamProtocolParam)
}
