package infra_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const floatTolerance = 0.0001

// p 是 float64 取指针的快捷函数，用于构造 ModelPricing 中的可选价格字段。
func p(v float64) *float64 { return &v }

// setupTestDB 创建内存 SQLite 数据库并建 call_logs 表，
// 表结构匹配 batch_writer.go 中 INSERT 语句的 26 列。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		// 跳过 gorm 默认的外键/事务行为，保持测试干净
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	err = db.Exec(`CREATE TABLE IF NOT EXISTS call_logs (
		request_id          TEXT    PRIMARY KEY,
		key_id              TEXT    NOT NULL DEFAULT '',
		user_id             TEXT    NOT NULL DEFAULT '',
		dept_id             TEXT,
		studio_id           TEXT,
		model               TEXT    NOT NULL DEFAULT '',
		stat_date           TIMESTAMP NOT NULL,
		prompt_tokens       INTEGER NOT NULL DEFAULT 0,
		completion_tokens   INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens    INTEGER NOT NULL DEFAULT 0,
		cached_tokens       INTEGER NOT NULL DEFAULT 0,
		ttft_ms             INTEGER,
		duration_ms         INTEGER,
		provider_duration_ms INTEGER,
		provider_type       TEXT,
		provider_model_id   TEXT,
		is_stream           BOOLEAN NOT NULL DEFAULT false,
		session_id          TEXT,
		status              TEXT    NOT NULL DEFAULT '',
		usage_missing       BOOLEAN NOT NULL DEFAULT false,
		prompt_est_cost     REAL    NOT NULL DEFAULT 0,
		completion_est_cost REAL    NOT NULL DEFAULT 0,
		reasoning_est_cost  REAL    NOT NULL DEFAULT 0,
		cached_est_cost     REAL    NOT NULL DEFAULT 0,
		total_est_cost      REAL    NOT NULL DEFAULT 0,
		pricing_missing     BOOLEAN NOT NULL DEFAULT false
	)`).Error
	require.NoError(t, err)
	return db
}

// countRows 返回 call_logs 表中的总行数。
func countRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	err := db.Table("call_logs").Count(&n).Error
	require.NoError(t, err)
	return n
}

// waitForRows 轮询等待 call_logs 表行数达到 want，超时返回 false。
func waitForRows(t *testing.T, db *gorm.DB, want int64, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if countRows(t, db) >= want {
			return true
		}
	}
	return false
}

// verifyCostColumns 查询 request_id 对应行的成本列，并与期望值比较。
func verifyCostColumns(t *testing.T, db *gorm.DB, requestID string,
	wantPrompt, wantCompletion, wantReasoning, wantCached, wantTotal float64,
) {
	t.Helper()
	var (
		gotPrompt     float64
		gotCompletion float64
		gotReasoning  float64
		gotCached     float64
		gotTotal      float64
	)
	q := `SELECT prompt_est_cost, completion_est_cost, reasoning_est_cost, cached_est_cost, total_est_cost
		FROM call_logs WHERE request_id = ?`
	err := db.Raw(q, requestID).Row().Scan(&gotPrompt, &gotCompletion, &gotReasoning, &gotCached, &gotTotal)
	require.NoError(t, err)
	if !floatEqual(gotPrompt, wantPrompt) {
		t.Errorf("request_id=%s prompt_est_cost = %v, want %v", requestID, gotPrompt, wantPrompt)
	}
	if !floatEqual(gotCompletion, wantCompletion) {
		t.Errorf("request_id=%s completion_est_cost = %v, want %v", requestID, gotCompletion, wantCompletion)
	}
	if !floatEqual(gotReasoning, wantReasoning) {
		t.Errorf("request_id=%s reasoning_est_cost = %v, want %v", requestID, gotReasoning, wantReasoning)
	}
	if !floatEqual(gotCached, wantCached) {
		t.Errorf("request_id=%s cached_est_cost = %v, want %v", requestID, gotCached, wantCached)
	}
	if !floatEqual(gotTotal, wantTotal) {
		t.Errorf("request_id=%s total_est_cost = %v, want %v", requestID, gotTotal, wantTotal)
	}
}

// now 返回当前时间，用于填充 UsageEvent.StatDate。
var now = time.Now().UTC().Truncate(time.Second)

// makeEvent 构造一个完整的 UsageEvent，仅需指定差异字段。
func makeEvent(requestID, model string, promptTokens, completionTokens int) infra.UsageEvent {
	return infra.UsageEvent{
		RequestID:        requestID,
		KeyID:            "test-key",
		UserID:           "test-user",
		Model:            model,
		ProviderType:     "test-provider",
		StatDate:         now,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Status:           "success",
	}
}

// makeFullEvent 构造一个包含所有 token 桶的 UsageEvent。
func makeFullEvent(requestID, model string, promptTokens, completionTokens, reasoningTokens, cachedTokens int) infra.UsageEvent {
	return infra.UsageEvent{
		RequestID:        requestID,
		KeyID:            "test-key",
		UserID:           "test-user",
		Model:            model,
		ProviderType:     "test-provider",
		StatDate:         now,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		ReasoningTokens:  reasoningTokens,
		CachedTokens:     cachedTokens,
		Status:           "success",
	}
}

// ---------------------------------------------------------------------------
// 测试用例
// ---------------------------------------------------------------------------

func TestBatchWriter_BatchWrite(t *testing.T) {
	const (
		batchSize    = 200 // 与 BatchWriter 默认 batchSize 一致
		testModel    = "test-model"
	)

	// 预置定价：PromptPrice=2.0, CompletionPrice=8.0, ReasoningPrice=12.0, CachedPrice=1.0
	basePricing := infra.ModelPricing{
		PromptPrice:     2.0,
		CompletionPrice: 8.0,
		ReasoningPrice:  p(12.0),
		CachedPrice:     p(1.0),
		PricingMissing:  false,
	}

	t.Run("批量写入正常", func(t *testing.T) {
		// 测试目的：构造 3 个事件通过 BatchWriter 写入 DB，验证行数和各成本列计算正确。
		db := setupTestDB(t)
		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		// 事件 1：所有 token 桶都有值
		e1 := makeFullEvent("req-1", testModel, 1000, 500, 200, 300)
		// 事件 2：仅 prompt 和 completion
		e2 := makeFullEvent("req-2", testModel, 2000, 1000, 0, 0)
		// 事件 3：所有 token 都为 0（成本应为 0）
		e3 := makeFullEvent("req-3", testModel, 0, 0, 0, 0)

		ch <- e1
		ch <- e2
		ch <- e3
		close(ch) // 关闭 channel → goroutine 排干缓冲区后 flush 并退出
		time.Sleep(100 * time.Millisecond)

		bw.Stop()

		// 验证行数
		require.EqualValues(t, 3, countRows(t, db), "应有 3 行数据")

		// 验证 e1 成本：P=1000*2/1M=0.002, C=500*8/1M=0.004, R=200*12/1M=0.0024, Cache=300*1/1M=0.0003
		verifyCostColumns(t, db, "req-1", 0.002, 0.004, 0.0024, 0.0003, 0.0087)

		// 验证 e2 成本：P=2000*2/1M=0.004, C=1000*8/1M=0.008, R=0, Cache=0
		verifyCostColumns(t, db, "req-2", 0.004, 0.008, 0, 0, 0.012)

		// 验证 e3 成本：全 0
		verifyCostColumns(t, db, "req-3", 0, 0, 0, 0, 0)
	})

	t.Run("ON CONFLICT幂等", func(t *testing.T) {
		// 测试目的：DB 中已有 request_id="dup-1" 的记录，通过 BatchWriter 写入同
		// request_id 的事件时，ON CONFLICT DO NOTHING 应阻止重复插入，最终只有 1 行。
		db := setupTestDB(t)
		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		// 预插入一条记录
		err := db.Exec(`INSERT INTO call_logs (request_id, key_id, user_id, model, stat_date, status)
			VALUES ('dup-1', 'pre-key', 'pre-user', 'pre-model', ?, 'pre-status')`, now).Error
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		ch <- makeEvent("dup-1", testModel, 100, 50)
		close(ch)

		bw.Stop()

		// 验证只有 1 行
		var count int64
		err = db.Table("call_logs").Where("request_id = ?", "dup-1").Count(&count).Error
		require.NoError(t, err)
		assert.EqualValues(t, 1, count, "ON CONFLICT 应阻止重复插入")

		// 验证数据仍是预插入的原始值（成本列应为默认 0）
		var status string
		err = db.Raw("SELECT status FROM call_logs WHERE request_id = ?", "dup-1").Scan(&status).Error
		require.NoError(t, err)
		assert.Equal(t, "pre-status", status, "ON CONFLICT DO NOTHING 应保留原始行")
	})

	t.Run("写入失败丢弃当批", func(t *testing.T) {
		// 测试目的：关闭 SQLite 连接后 flush 应失败但不应 panic，
		// 且 UsageMetricsInstance.BatchWriteErrors 计数器应递增。
		infra.UsageMetricsInstance.Reset()

		db := setupTestDB(t)

		// 关闭底层连接使后续 Exec 失败
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.Close()

		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		ch <- makeEvent("err-1", testModel, 100, 50)
		ch <- makeEvent("err-2", testModel, 200, 100)
		ch <- makeEvent("err-3", testModel, 300, 150)
		close(ch)

		time.Sleep(150 * time.Millisecond) // 给 goroutine 时间排干 channel 并尝试 flush
		bw.Stop()

		// 验证错误计数器递增（flush 失败会调用 UsageMetricsInstance.BatchWriteErrors.Inc()）
		assert.Greater(t, infra.UsageMetricsInstance.BatchWriteErrors.Value(), int64(0),
			"flush 失败后 BatchWriteErrors 应 > 0")
	})

	t.Run("ticker触发flush", func(t *testing.T) {
		// 测试目的：即使事件数未达到 batchSize（200），1s 的 ticker 也应触发
		// flush 将事件写入 DB。发送 1 个事件后轮询 DB 确认。
		db := setupTestDB(t)
		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		ch <- makeEvent("ticker-1", testModel, 100, 50)

		// 轮询等待 ticker 触发 flush（ticker 间隔 1s，这里给 2.5s 余量）
		flushed := waitForRows(t, db, 1, 2500*time.Millisecond)
		assert.True(t, flushed, "ticker 应在 1s 内触发 flush")

		bw.Stop()
	})

	t.Run("批量大小触发flush", func(t *testing.T) {
		// 测试目的：发送 batchSize（200）个事件后，BatchWriter 应立即 flush 而
		// 无需等待 ticker。发送后轮询 DB 在短时间内确认。
		db := setupTestDB(t)
		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		for i := 0; i < batchSize; i++ {
			reqID := "batch-" + string(rune('0'+i%10)) + string(rune('A'+i/10))
			ch <- makeEvent(reqID, testModel, 100, 50)
		}
		close(ch)

		// 给 goroutine 时间从已关闭通道中排干 200 个事件并触发 batch-size flush
		time.Sleep(200 * time.Millisecond)
		bw.Stop()
		require.EqualValues(t, batchSize, countRows(t, db), "发送 %d 个事件后应全部写入", batchSize)
	})

	t.Run("stop时flush残留", func(t *testing.T) {
		// 测试目的：发送不足 batchSize 的事件后立即调用 Stop()，
		// 确保残留事件在 shutdown 时被 flush 而非丢弃。
		db := setupTestDB(t)
		ch := make(chan infra.UsageEvent, batchSize)
		bw := infra.NewBatchWriter(ch, db)
		bw.SetPricing(testModel, basePricing)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bw.Start(ctx)

		ch <- makeEvent("stop-1", testModel, 100, 50)
		ch <- makeEvent("stop-2", testModel, 200, 100)
		ch <- makeEvent("stop-3", testModel, 300, 150)

		// 给 goroutine 一点时间以从 channel 读取事件到 batch 中
		time.Sleep(100 * time.Millisecond)
		bw.Stop()

		// 停止后所有残留事件应已被 flush
		require.EqualValues(t, 3, countRows(t, db), "Stop 时应 flush 残留事件")

		// 验证请求确实已写入（检查 key_id）
		var keyID string
		err := db.Raw("SELECT key_id FROM call_logs WHERE request_id = ?", "stop-2").Scan(&keyID).Error
		require.NoError(t, err)
		assert.Equal(t, "test-key", keyID)
	})
}
