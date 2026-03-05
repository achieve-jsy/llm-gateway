package ehrclient

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter(100)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !rl.Wait(ctx.Done()) {
		t.Fatal("Wait should return true when token available")
	}
}

func TestRateLimiter_WaitTimeout(t *testing.T) {
	rl := NewRateLimiter(1)
	rl.Wait(context.Background().Done())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rl.Wait(ctx.Done()) {
		t.Fatal("Wait should return false when context canceled")
	}
}

func TestRateLimiter_SetRate(t *testing.T) {
	rl := NewRateLimiter(100)
	rl.SetRate(10)
	if rl.rate != 10 {
		t.Fatalf("expected rate 10, got %f", rl.rate)
	}
	if rl.maxTokens != 10 {
		t.Fatalf("expected maxTokens 10, got %f", rl.maxTokens)
	}
}

func TestRateLimiter_Burst(t *testing.T) {
	rl := NewRateLimiter(1000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	for i := 0; i < 100; i++ {
		if !rl.Wait(ctx.Done()) {
			t.Fatalf("Wait returned false at iteration %d", i)
		}
	}
	elapsed := time.Since(start)
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("burst of 100 at rate=1000 took %v, want < 100ms", elapsed)
	}
}
