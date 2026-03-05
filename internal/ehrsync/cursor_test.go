package ehrsync

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

func TestCursorManager_GetSet(t *testing.T) {
	rdb, _ := newTestRedis(t)
	cm := NewCursorManager(rdb)
	ctx := context.Background()

	got, err := cm.Get(ctx, "department", "org1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	if err := cm.Set(ctx, "department", "org1", "2025-01-01"); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err = cm.Get(ctx, "department", "org1")
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if got != "2025-01-01" {
		t.Fatalf("expected 2025-01-01, got %q", got)
	}
}

func TestCursorManager_Reset(t *testing.T) {
	rdb, _ := newTestRedis(t)
	cm := NewCursorManager(rdb)
	ctx := context.Background()

	_ = cm.Set(ctx, "employee", "org1", "2025-03-01")
	if err := cm.Reset(ctx, "employee", "org1"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	got, _ := cm.Get(ctx, "employee", "org1")
	if got != "" {
		t.Fatalf("expected empty after reset, got %q", got)
	}
}

func TestCursorManager_UpdateIfGreater(t *testing.T) {
	rdb, _ := newTestRedis(t)
	cm := NewCursorManager(rdb)
	ctx := context.Background()

	_ = cm.Set(ctx, "department", "org1", "2025-01-01")

	if err := cm.UpdateIfGreater(ctx, "department", "org1", "2025-02-01"); err != nil {
		t.Fatalf("update greater: %v", err)
	}
	got, _ := cm.Get(ctx, "department", "org1")
	if got != "2025-02-01" {
		t.Fatalf("expected 2025-02-01, got %q", got)
	}

	if err := cm.UpdateIfGreater(ctx, "department", "org1", "2025-01-01"); err != nil {
		t.Fatalf("update lesser: %v", err)
	}
	got, _ = cm.Get(ctx, "department", "org1")
	if got != "2025-02-01" {
		t.Fatalf("expected still 2025-02-01, got %q", got)
	}
}

func TestCursorManager_Concurrent(t *testing.T) {
	rdb, _ := newTestRedis(t)
	cm := NewCursorManager(rdb)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			val := "2025-01-01"
			if n%2 == 0 {
				val = "2025-06-01"
			}
			_ = cm.UpdateIfGreater(ctx, "employee", "org1", val)
		}(i)
	}
	wg.Wait()

	got, err := cm.Get(ctx, "employee", "org1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "2025-06-01" {
		t.Fatalf("expected 2025-06-01, got %q", got)
	}
}
