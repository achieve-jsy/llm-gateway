package ehrsync

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestScheduler(t *testing.T) (*Scheduler, *redis.Client) {
	t.Helper()
	rdb, _ := newTestRedis(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := &Scheduler{
		rdb:    rdb,
		logger: logger,
		locks:  NewLocker(rdb, 10*time.Second),
	}
	return s, rdb
}

func TestScheduler_StartStop(t *testing.T) {
	s, _ := newTestScheduler(t)

	cfg := SchedulerConfig{
		CronOrg:        1 * time.Hour,
		CronCompany:    1 * time.Hour,
		CronDepartment: 1 * time.Hour,
		CronEmployee:   1 * time.Hour,
		CronReconcile:  12 * time.Hour,
		LockTTL:        10 * time.Second,
		Enabled:        true,
	}

	if err := s.Start(cfg); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !s.IsRunning() {
		t.Fatal("expected running after start")
	}

	s.Stop()
	if s.IsRunning() {
		t.Fatal("expected not running after stop")
	}
}

func TestScheduler_Disabled(t *testing.T) {
	s, _ := newTestScheduler(t)

	cfg := SchedulerConfig{Enabled: false}
	if err := s.Start(cfg); err != nil {
		t.Fatalf("start disabled: %v", err)
	}
	if s.IsRunning() {
		t.Fatal("expected not running when disabled")
	}
}

func TestScheduler_Locker_TryAcquireRelease(t *testing.T) {
	rdb, _ := newTestRedis(t)
	locker := NewLocker(rdb, 10*time.Second)
	ctx := context.Background()

	acquired, err := locker.TryAcquire(ctx, "test:lock")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true")
	}

	acquired2, _ := locker.TryAcquire(ctx, "test:lock")
	if acquired2 {
		t.Fatal("expected second acquire to fail while held")
	}

	if err := locker.Release(ctx, "test:lock"); err != nil {
		t.Fatalf("release: %v", err)
	}

	acquired3, _ := locker.TryAcquire(ctx, "test:lock")
	if !acquired3 {
		t.Fatal("expected acquired after release")
	}
}

func TestScheduler_Locker_Concurrent(t *testing.T) {
	rdb, _ := newTestRedis(t)
	locker := NewLocker(rdb, 10*time.Second)
	ctx := context.Background()

	var mu sync.Mutex
	results := make([]bool, 0, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := locker.TryAcquire(ctx, "concurrent:lock")
			if err != nil {
				t.Errorf("acquire err: %v", err)
				return
			}
			mu.Lock()
			results = append(results, ok)
			mu.Unlock()
		}()
	}
	wg.Wait()

	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Fatalf("expected exactly 1 goroutine to acquire, got %d (results=%v)", trueCount, results)
	}
}

func TestScheduler_TriggerManual_Incremental(t *testing.T) {
	orch, _, cleanup := newTestOrchestrator(t, healthyHandler())
	defer cleanup()

	s, _ := newTestScheduler(t)
	s.orch = orch

	ctx := context.Background()
	batchID, err := s.TriggerManual(ctx, "incremental")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if batchID == "" {
		t.Fatal("expected non-empty batchID")
	}
}

func TestScheduler_TriggerManual_Full(t *testing.T) {
	orch, _, cleanup := newTestOrchestrator(t, healthyHandler())
	defer cleanup()

	s, _ := newTestScheduler(t)
	s.orch = orch

	ctx := context.Background()
	batchID, err := s.TriggerManual(ctx, "full")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if batchID == "" {
		t.Fatal("expected non-empty batchID")
	}
}

func TestScheduler_TriggerManual_InvalidType(t *testing.T) {
	s, _ := newTestScheduler(t)
	s.orch = &Orchestrator{}

	ctx := context.Background()
	_, err := s.TriggerManual(ctx, "bogus")
	if err == nil {
		t.Fatal("expected error for invalid sync type")
	}
}

func TestAcquireAndRun_Succeeds(t *testing.T) {
	s, _ := newTestScheduler(t)
	ctx := context.Background()

	acquired, err := s.acquireAndRun(ctx, "batch-1", "free:lock", func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true for free lock")
	}
}

func TestAcquireAndRun_AlreadyLocked(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := &Scheduler{
		rdb:    rdb,
		logger: logger,
		locks:  NewLocker(rdb, 10*time.Second),
	}

	ctx := context.Background()
	if err := rdb.Set(ctx, "taken:lock", "1", 10*time.Second).Err(); err != nil {
		t.Fatalf("pre-set lock: %v", err)
	}

	acquired, err := s.acquireAndRun(ctx, "batch-2", "taken:lock", func() error {
		t.Fatal("should not be called")
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Fatal("expected acquired=false for already-held lock")
	}
}

func TestEnvDuration_ParseSeconds(t *testing.T) {
	t.Setenv("EHR_TEST_DURATION", "30")
	got := envDuration("EHR_TEST_DURATION", 10*time.Second)
	if got != 30*time.Second {
		t.Fatalf("expected 30s, got %v", got)
	}
}

func TestEnvDuration_ParseGoFormat(t *testing.T) {
	t.Setenv("EHR_TEST_DURATION", "5m")
	got := envDuration("EHR_TEST_DURATION", 10*time.Second)
	if got != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", got)
	}
}

func TestEnvDuration_Default(t *testing.T) {
	got := envDuration("EHR_NONEXISTENT_XXX", 10*time.Second)
	if got != 10*time.Second {
		t.Fatalf("expected 10s default, got %v", got)
	}
}

func TestEnvBool_True(t *testing.T) {
	t.Setenv("EHR_TEST_BOOL", "true")
	got := envBool("EHR_TEST_BOOL", false)
	if !got {
		t.Fatal("expected true")
	}
}

func TestEnvBool_False(t *testing.T) {
	t.Setenv("EHR_TEST_BOOL", "false")
	got := envBool("EHR_TEST_BOOL", true)
	if got {
		t.Fatal("expected false")
	}
}

func TestEnvBool_Invalid(t *testing.T) {
	t.Setenv("EHR_TEST_BOOL", "maybe")
	got := envBool("EHR_TEST_BOOL", true)
	if !got {
		t.Fatal("expected default=true for invalid value")
	}
}
