package ehrclient

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestTokenManager_Acquire(t *testing.T) {
	tm := NewTokenManager("initial-token")
	tok, err := tm.Acquire(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "initial-token" {
		t.Fatalf("expected 'initial-token', got %q", tok)
	}
}

func TestTokenManager_AcquireEmpty(t *testing.T) {
	tm := NewTokenManager("")
	_, err := tm.Acquire(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var cfgErr *ErrConfig
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ErrConfig, got %T: %v", err, err)
	}
}

func TestTokenManager_ReloadFromConfig(t *testing.T) {
	tm := NewTokenManager("old")
	tm.SetReloadHook(func() string { return "new" })
	tm.ReloadFromConfig()
	tok, err := tm.Acquire(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "new" {
		t.Fatalf("expected 'new', got %q", tok)
	}
}

func TestTokenManager_OnRotatedHook(t *testing.T) {
	tm := NewTokenManager("old")
	var rotated string
	tm.SetOnRotatedHook(func(newToken string) {
		rotated = newToken
	})
	tm.SetReloadHook(func() string { return "new" })
	tm.ReloadFromConfig()
	if rotated != "new" {
		t.Fatalf("expected onRotated hook to fire with 'new', got %q", rotated)
	}
}

func TestTokenManager_ConcurrentAcquireReload(t *testing.T) {
	tm := NewTokenManager("initial")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%5 == 0 {
				tm.Replace("replaced")
			} else {
				tm.Acquire(context.Background())
			}
		}(i)
	}
	wg.Wait()
}
