package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	obsrepo "github.com/example/llm-gateway/internal/observability/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// upstream_protocol 的存储侧契约（gateway 写 / 控制面读，共享同一个 obsmodel.CallLog）：
//   - 只有原生直发成功才写值：native_chat / native_responses / native_claude / native_gemini
//   - 降级到 relaykit 转换路径不写 → 落库空串，**空串即「转换」**
//   - 跨进程 JSON 字段名固定为 upstream_protocol（Redis spool payload）

func newCallLogDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:call_log_upstream_protocol_%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	return db
}

// 写库 → 读回：UpstreamProtocol 必须原样保留（空串代表转换路径，不能被默认值吞成 NULL/丢失）。
func TestCallLogRepositoryPersistsUpstreamProtocol(t *testing.T) {
	db := newCallLogDB(t)
	repo := obsrepo.NewCallLogRepository(db)
	ts := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	rows := []obsmodel.CallLog{
		{RequestID: "native-chat", TS: ts, Model: "gpt-4o", UpstreamProtocol: "native_chat"},
		{RequestID: "native-claude", TS: ts, Model: "claude-sonnet-4", UpstreamProtocol: "native_claude"},
		{RequestID: "converted", TS: ts, Model: "gpt-4o", UpstreamProtocol: ""},
	}
	require.NoError(t, repo.BatchInsert(context.Background(), rows))

	for _, want := range rows {
		var got obsmodel.CallLog
		require.NoError(t, db.Where("request_id = ?", want.RequestID).First(&got).Error)
		require.Equal(t, want.UpstreamProtocol, got.UpstreamProtocol, "request_id=%s", want.RequestID)
	}

	// 落库列名必须是 upstream_protocol（GORM 从字段名推导，别被手写 column tag 改走）
	require.True(t, db.Migrator().HasColumn(&obsmodel.CallLog{}, "upstream_protocol"))
}

// spool 投递契约的**反序列化**方向：控制面从 Redis Stream 读到的 payload 要能解析出该字段，
// 缺失时保持空串（= 转换路径），不报错。
func TestCallLogUpstreamProtocolSpoolPayloadDecodes(t *testing.T) {
	var got obsmodel.CallLog
	require.NoError(t, json.Unmarshal(
		[]byte(`{"request_id":"spool-1","upstream_protocol":"native_responses"}`), &got))
	require.Equal(t, "native_responses", got.UpstreamProtocol)

	// 老网关（本次改造前）投递的 payload 没有该字段：解析不报错，落库为空串
	var legacy obsmodel.CallLog
	require.NoError(t, json.Unmarshal([]byte(`{"request_id":"spool-legacy"}`), &legacy))
	require.Equal(t, "", legacy.UpstreamProtocol)

	// 空串时 omitempty 让 payload 不带该键，保持与改造前一致（不额外增加 spool 体积）
	payload, err := json.Marshal(obsmodel.CallLog{RequestID: "spool-2"})
	require.NoError(t, err)
	var keys map[string]any
	require.NoError(t, json.Unmarshal(payload, &keys))
	require.NotContains(t, keys, "upstream_protocol")
}
