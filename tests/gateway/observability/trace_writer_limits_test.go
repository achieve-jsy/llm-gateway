package observability_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTraceLimitDB 建一个内存 sqlite 的 call_traces 表，用于观察 TraceWriter 的 flush 行为。
func newTraceLimitDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	return db
}

// traceBatchRecorder 通过 GORM create 回调记录每一次批量 INSERT 的行数与正文字节数，
// 用于断言 D5 的按字节限批把一次 flush 拆成了多批。
type traceBatchRecorder struct {
	mu    sync.Mutex
	rows  []int
	bytes []int64
}

// snapshot 返回已记录批次的快照（拷贝，避免与 Run 协程竞争）。
func (r *traceBatchRecorder) snapshot() ([]int, []int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.rows...), append([]int64(nil), r.bytes...)
}

// registerTraceBatchRecorder 注册 create 前置回调，统计每次 Create 的行数与正文字节。
func registerTraceBatchRecorder(db *gorm.DB, rec *traceBatchRecorder) {
	db.Callback().Create().Before("gorm:create").Register("test:trace_batch_recorder", func(tx *gorm.DB) {
		rows := traceRowsFromDest(tx)
		var n int64
		for i := range rows {
			n += int64(len(rows[i].RequestBody) + len(rows[i].ResponseBody))
		}
		rec.mu.Lock()
		rec.rows = append(rec.rows, len(rows))
		rec.bytes = append(rec.bytes, n)
		rec.mu.Unlock()
	})
}

// traceRowsFromDest 从 GORM statement 取出本次 Create 的行（批量 slice 或降级后的单条）。
func traceRowsFromDest(tx *gorm.DB) []obsmodel.CallTrace {
	switch dest := tx.Statement.Dest.(type) {
	case *[]obsmodel.CallTrace:
		return *dest
	case []obsmodel.CallTrace:
		return dest
	case *obsmodel.CallTrace:
		return []obsmodel.CallTrace{*dest}
	}
	return nil
}

// traceEventOfSize 构造一条正文恰好 n 字节、且为合法 JSON 的 TraceEvent
// （json.Valid 通过时 traceEventToModel 保留原始长度，便于按字节断言）。
func traceEventOfSize(requestID string, n int) gwobs.TraceEvent {
	const prefix, suffix = `{"p":"`, `"}`
	return gwobs.TraceEvent{
		RequestID:      requestID,
		RequestBodyRaw: []byte(prefix + strings.Repeat("x", n-len(prefix)-len(suffix)) + suffix),
	}
}

// traceRowCount 返回 call_traces 行数；出错返回 -1。
// 该函数会被 require.Eventually 的轮询协程调用，故只能返回值、不能调用 FailNow。
func traceRowCount(db *gorm.DB) int64 {
	var n int64
	if err := db.Model(&obsmodel.CallTrace{}).Count(&n).Error; err != nil {
		return -1
	}
	return n
}

// TestTraceWriter_MaxBatchBytesSplitsFlush 验收标准 5（D5）：
// maxBatchBytes 生效——按字节累计达到上限即 flush，把事件序列拆成多批，
// 从而给单次批量 INSERT 的正文长度（pgx 简单协议下会内联进 SQL 文本）设上界。
func TestTraceWriter_MaxBatchBytesSplitsFlush(t *testing.T) {
	const (
		eventBytes = 400
		eventCount = 6
	)

	tests := []struct {
		name        string
		description string
		flushN      int
		maxBatch    int64
		wantBatches []int
	}{
		{
			name:        "默认 8MB 上限不拆分",
			description: "flushN=6 时按行数触发一次 flush；6 条事件共 2400B 远小于 8MB 上限，应在同一批写入",
			flushN:      eventCount,
			maxBatch:    gwobs.DefaultTraceMaxBatchBytes,
			wantBatches: []int{6},
		},
		{
			name:        "1KB 上限按字节拆成两批",
			description: "flushN 远大于事件数、maxBatchBytes=1000 时，累计 1200B ≥ 1000B 即 flush，6 条 400B 事件应拆成 3+3 两批",
			flushN:      1000,
			maxBatch:    1000,
			wantBatches: []int{3, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTraceLimitDB(t)
			rec := &traceBatchRecorder{}
			registerTraceBatchRecorder(db, rec)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// interval 取 1 小时：不按时间触发；flushN 与 maxBatchBytes 由用例指定，
			// 于是 flush 只会由这两个条件之一与 ctx 取消触发。
			w := gwobs.NewTraceWriterWithLimits(db, 64, tt.flushN, time.Hour, gwobs.TraceWriterLimits{
				MaxBatchBytes: tt.maxBatch,
				MaxQueueBytes: 1 << 20,
			})
			done := make(chan struct{})
			go func() {
				w.Run(ctx)
				close(done)
			}()

			for i := 0; i < eventCount; i++ {
				w.Emit(traceEventOfSize(fmt.Sprintf("batch-%d", i), eventBytes))
			}

			// 两批（或一批）都会由 flushN / maxBatchBytes 自动触发，无需依赖取消时机
			wantFlushes := len(tt.wantBatches)
			require.Eventually(t, func() bool {
				rows, _ := rec.snapshot()
				return len(rows) >= wantFlushes
			}, 3*time.Second, 10*time.Millisecond, tt.description)

			cancel()
			<-done

			rows, bytesPerBatch := rec.snapshot()
			assert.Equal(t, tt.wantBatches, rows, tt.description)
			// 每批的正文长度都必须有上界：< maxBatchBytes + 单条最大正文
			for i, b := range bytesPerBatch {
				assert.LessOrEqual(t, b, tt.maxBatch+eventBytes, "第 %d 批正文长度必须有上界", i)
			}
			// 全部事件都落库（限批只影响分几批，不丢数据）
			assert.Equal(t, int64(eventCount), traceRowCount(db))
		})
	}
}

// TestTraceWriter_QueueByteLimitDropsAndRecycles 验收标准 6（D6）：
//  1. 队列字节超限时 Emit 立即丢弃，而不是无界堆积——channel 槽位充足，
//     唯一可能的丢弃原因就是 maxQueueBytes 字节闸门；
//  2. 被消费的事件必须归还字节额度（连续 Emit + 消费后计数回到 0），
//     否则残留计数会让后续 trace 被永久丢弃。
func TestTraceWriter_QueueByteLimitDropsAndRecycles(t *testing.T) {
	const (
		eventBytes = 900
		maxQueue   = int64(1000) // 只装得下 1 条 900B 事件
		dropCount  = 5
	)

	db := newTraceLimitDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// bufSize=64 且消费协程尚未启动：槽位远多于事件数，
	// 因此唯一可能丢弃事件的原因就是字节闸门，而不是 channel 满。
	w := gwobs.NewTraceWriterWithLimits(db, 64, 1, 10*time.Millisecond, gwobs.TraceWriterLimits{
		MaxBatchBytes: 1 << 20,
		MaxQueueBytes: maxQueue,
	})

	// 阶段一：Run 未启动，900B 事件在 1000B 上限下只可能放行 1 条，其余必须被丢弃
	w.Emit(traceEventOfSize("queue-keep-0", eventBytes))
	for i := 0; i < dropCount; i++ {
		w.Emit(traceEventOfSize(fmt.Sprintf("queue-drop-%d", i), eventBytes))
	}

	// 阶段二：启动消费，只有 1 条能落库（证明字节闸门丢弃了其余 5 条）
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool { return traceRowCount(db) == 1 }, 3*time.Second, 20*time.Millisecond,
		"队列字节闸门应只放行 1 条 900B 事件（上限 1000B），其余立即丢弃")

	// 阶段三：额度回收。事件出队即递减计数；若计数泄漏（残留 900B），
	// 这条 900B 事件会因 900+900 > 1000 被丢弃，断言随即失败。
	w.Emit(traceEventOfSize("queue-recycle-1", eventBytes))
	require.Eventually(t, func() bool { return traceRowCount(db) == 2 }, 3*time.Second, 20*time.Millisecond,
		"队列字节计数未随消费回收，导致后续 trace 被永久丢弃")

	// 阶段四：连续 Emit + 消费可以反复通过（计数稳定回到 0，不累积泄漏）
	for i := 2; i <= 4; i++ {
		w.Emit(traceEventOfSize(fmt.Sprintf("queue-recycle-%d", i), eventBytes))
		want := int64(i + 1)
		require.Eventually(t, func() bool { return traceRowCount(db) == want }, 3*time.Second, 20*time.Millisecond,
			"第 %d 次回收后仍应能通过", i)
	}

	cancel()
	<-done
}
