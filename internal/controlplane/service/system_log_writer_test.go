package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

const testFlushInterval = 50 * time.Millisecond

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Auto-migrate system_logs table
	if err := db.AutoMigrate(&model.SystemLog{}); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return db
}

func newTestWriter(t *testing.T, db *gorm.DB) *SystemLogWriter {
	t.Helper()
	return NewSystemLogWriterWithConfig(db, defaultSystemLogBufferSize, defaultSystemLogBatchSize, testFlushInterval)
}

func TestSystemLogWriter_WriteAndFlush(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()
	defer w.Stop()

	// Write a single entry
	entry := &model.SystemLog{
		Level:   "error",
		Message: "test error message",
	}
	w.Write(context.Background(), entry)

	// Wait for flush (ticker interval + buffer)
	time.Sleep(200 * time.Millisecond)

	// Verify it was written to DB
	var logs []model.SystemLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Level != "error" {
		t.Errorf("expected level 'error', got %q", logs[0].Level)
	}
	if logs[0].Message != "test error message" {
		t.Errorf("expected message 'test error message', got %q", logs[0].Message)
	}

	// Verify health counters
	if w.WrittenCount() != 1 {
		t.Errorf("expected written_count=1, got %d", w.WrittenCount())
	}
	if w.WriteFailures() != 0 {
		t.Errorf("expected write_failures=0, got %d", w.WriteFailures())
	}
	if w.DroppedCount() != 0 {
		t.Errorf("expected dropped_count=0, got %d", w.DroppedCount())
	}
}

func TestSystemLogWriter_BatchWrite(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()
	defer w.Stop()

	n := 10
	for i := 0; i < n; i++ {
		w.Write(context.Background(), &model.SystemLog{
			Level:   "info",
			Message: "batch message",
		})
	}

	time.Sleep(300 * time.Millisecond)

	var logs []model.SystemLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if len(logs) != n {
		t.Errorf("expected %d log entries, got %d", n, len(logs))
	}
	if w.WrittenCount() != int64(n) {
		t.Errorf("expected written_count=%d, got %d", n, w.WrittenCount())
	}
}

func TestSystemLogWriter_NonBlocking(t *testing.T) {
	// Fill the queue to verify writes are non-blocking
	// Use a writer that flushes very slowly to ensure queue fills up
	db := newTestDB(t)
	w := &SystemLogWriter{
		db:            db,
		queue:         make(chan *model.SystemLog, 2), // tiny buffer
		batchSize:     defaultSystemLogBatchSize,
		flushInterval: 1 * time.Hour, // never flush during test
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.lastError.Store("")
	w.running.Store(true)
	w.wg.Add(1)
	go w.run()
	defer func() {
		w.cancel()
		w.wg.Wait()
	}()

	// Write more entries than the buffer size
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			w.Write(context.Background(), &model.SystemLog{
				Level:   "info",
				Message: "non-blocking test",
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Pass - writes completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked - expected non-blocking behavior")
	}

	// Should have dropped some entries
	if w.DroppedCount() == 0 {
		t.Error("expected some dropped entries when queue is full")
	}
}

func TestSystemLogWriter_DroppedCount(t *testing.T) {
	db := newTestDB(t)
	w := &SystemLogWriter{
		db:            db,
		queue:         make(chan *model.SystemLog, 1),
		batchSize:     defaultSystemLogBatchSize,
		flushInterval: 1 * time.Hour,
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.lastError.Store("")
	w.running.Store(true)
	w.wg.Add(1)
	go w.run()
	defer func() {
		w.cancel()
		w.wg.Wait()
	}()

	// Fill the queue
	w.Write(context.Background(), &model.SystemLog{Level: "info", Message: "first"})
	w.Write(context.Background(), &model.SystemLog{Level: "info", Message: "second"})
	w.Write(context.Background(), &model.SystemLog{Level: "info", Message: "third"})

	time.Sleep(100 * time.Millisecond)

	if w.DroppedCount() < 1 {
		t.Errorf("expected dropped_count >= 1, got %d", w.DroppedCount())
	}
}

func TestSystemLogWriter_SinkHealthMethods(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()
	defer w.Stop()

	// Write some entries
	for i := 0; i < 5; i++ {
		w.Write(context.Background(), &model.SystemLog{
			Level:   "warn",
			Message: "health test",
		})
	}

	time.Sleep(300 * time.Millisecond)

	// QueueDepth should be 0 after flush
	if qd := w.QueueDepth(); qd != 0 {
		t.Errorf("expected queue_depth=0 after flush, got %d", qd)
	}

	// QueueCapacity
	if qc := w.QueueCapacity(); qc != defaultSystemLogBufferSize {
		t.Errorf("expected queue_capacity=%d, got %d", defaultSystemLogBufferSize, qc)
	}

	// WrittenCount
	if wc := w.WrittenCount(); wc != 5 {
		t.Errorf("expected written_count=5, got %d", wc)
	}

	// WriteFailures
	if wf := w.WriteFailures(); wf != 0 {
		t.Errorf("expected write_failures=0, got %d", wf)
	}

	// DroppedCount
	if dc := w.DroppedCount(); dc != 0 {
		t.Errorf("expected dropped_count=0, got %d", dc)
	}

	// AvgWriteDelay
	if delay := w.AvgWriteDelay(); delay == 0 {
		t.Error("expected avg_write_delay > 0 after writes")
	}

	// IsRunning
	if !w.IsRunning() {
		t.Error("expected IsRunning=true")
	}

	// LastError should be empty
	if le := w.LastError(); le != "" {
		t.Errorf("expected empty last_error, got %q", le)
	}
}

func TestSystemLogWriter_NilSafety(t *testing.T) {
	var w *SystemLogWriter

	// All public methods should be nil-safe
	w.Write(context.Background(), &model.SystemLog{Level: "info"})
	w.Stop()
	_ = w.QueueDepth()
	_ = w.QueueCapacity()
	_ = w.DroppedCount()
	_ = w.WriteFailures()
	_ = w.WrittenCount()
	_ = w.AvgWriteDelay()
	_ = w.LastError()
	_ = w.IsRunning()
}

func TestSystemLogWriter_GlobalAccessor(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()
	defer w.Stop()

	// Set global
	SetSystemLogWriter(w)

	// Get global
	got := GetSystemLogWriter()
	if got != w {
		t.Fatal("GetSystemLogWriter returned different instance")
	}

	// Verify health methods work through global
	got.Write(context.Background(), &model.SystemLog{
		Level:   "error",
		Message: "global test",
	})

	time.Sleep(300 * time.Millisecond)

	if got.WrittenCount() != 1 {
		t.Errorf("expected written_count=1, got %d", got.WrittenCount())
	}

	// Reset global
	SetSystemLogWriter(nil)
	if GetSystemLogWriter() != nil {
		t.Error("expected nil after reset")
	}
}

func TestSystemLogWriter_AllFields(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()
	defer w.Stop()

	now := time.Now()
	reqID := "req-123"
	userID := "user-456"
	provider := "openai"
	modelName := "gpt-4"
	group := "test-group"
	durationMs := 1500
	status := 500
	errMsg := "upstream timeout"

	entry := &model.SystemLog{
		Ts:           now,
		Level:        "error",
		Component:    "gateway",
		Message:      "upstream request failed",
		RequestID:    &reqID,
		UserID:       &userID,
		Provider:     &provider,
		Model:        &modelName,
		Group:        &group, // group_name column
		DurationMs:   &durationMs,
		Status:       &status,
		ErrorMessage: &errMsg,
	}

	w.Write(context.Background(), entry)

	time.Sleep(300 * time.Millisecond)

	var logs []model.SystemLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	l := logs[0]
	if l.Level != "error" {
		t.Errorf("level: expected 'error', got %q", l.Level)
	}
	if l.Component != "gateway" {
		t.Errorf("component: expected 'gateway', got %q", l.Component)
	}
	if l.Message != "upstream request failed" {
		t.Errorf("message mismatch")
	}
	if l.RequestID == nil || *l.RequestID != "req-123" {
		t.Errorf("request_id mismatch")
	}
	if l.UserID == nil || *l.UserID != "user-456" {
		t.Errorf("user_id mismatch")
	}
	if l.Provider == nil || *l.Provider != "openai" {
		t.Errorf("provider mismatch")
	}
	if l.Model == nil || *l.Model != "gpt-4" {
		t.Errorf("model mismatch")
	}
	if l.Group == nil || *l.Group != "test-group" {
		t.Errorf("group mismatch")
	}
	if l.DurationMs == nil || *l.DurationMs != 1500 {
		t.Errorf("duration_ms mismatch")
	}
	if l.Status == nil || *l.Status != 500 {
		t.Errorf("status mismatch")
	}
	if l.ErrorMessage == nil || *l.ErrorMessage != "upstream timeout" {
		t.Errorf("error_message mismatch")
	}
}

func TestSystemLogWriter_StopDrains(t *testing.T) {
	db := newTestDB(t)
	w := newTestWriter(t, db)
	w.Start()

	// Write entries then stop
	for i := 0; i < 5; i++ {
		w.Write(context.Background(), &model.SystemLog{
			Level:   "info",
			Message: "drain test",
		})
	}
	w.Stop()

	// All entries should be drained
	var logs []model.SystemLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if len(logs) != 5 {
		t.Errorf("expected 5 log entries after drain, got %d", len(logs))
	}
	if w.IsRunning() {
		t.Error("expected IsRunning=false after Stop")
	}
}