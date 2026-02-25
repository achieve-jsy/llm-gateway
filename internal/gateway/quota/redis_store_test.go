package quota

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *RedisStore) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, NewRedisStore(rdb)
}

func TestCheckRPM_Limit0_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.CheckRPM(context.Background(), "test:key", 0)
	if err != nil {
		t.Errorf("limit=0 should pass, got %v", err)
	}
}

func TestCheckRPM_UnderLimit_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.CheckRPM(context.Background(), "test:key", 10)
	if err != nil {
		t.Errorf("under limit should pass, got %v", err)
	}
}

func TestCheckRPM_AtLimit_Fails(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:key"
	// Fill up to limit
	for i := 0; i < 5; i++ {
		store.CheckRPM(context.Background(), key, 5)
	}
	err := store.CheckRPM(context.Background(), key, 5)
	if err == nil {
		t.Error("at limit should fail")
	}
}

func TestAcquireConcurrency_Limit0_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.AcquireConcurrency(context.Background(), "test:conc", 0)
	if err != nil {
		t.Errorf("limit=0 should pass, got %v", err)
	}
}

func TestAcquireConcurrency_UnderLimit_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.AcquireConcurrency(context.Background(), "test:conc", 5)
	if err != nil {
		t.Errorf("under limit should pass, got %v", err)
	}
}

func TestAcquireConcurrency_AtLimit_Fails(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc"
	// Fill up to limit
	for i := 0; i < 3; i++ {
		store.AcquireConcurrency(context.Background(), key, 3)
	}
	err := store.AcquireConcurrency(context.Background(), key, 3)
	if err == nil {
		t.Error("at limit should fail")
	}
}

func TestAcquireConcurrency_ReleaseThenAcquire(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc"
	// Fill to limit
	for i := 0; i < 2; i++ {
		store.AcquireConcurrency(context.Background(), key, 2)
	}
	// At limit, should fail
	err := store.AcquireConcurrency(context.Background(), key, 2)
	if err == nil {
		t.Error("at limit should fail")
	}
	// Release one
	store.ReleaseConcurrency(context.Background(), key)
	// Now should pass
	err = store.AcquireConcurrency(context.Background(), key, 2)
	if err != nil {
		t.Errorf("after release should pass, got %v", err)
	}
}

func TestRPMKey(t *testing.T) {
	tests := []struct {
		subjectType, subjectID, layer, resourceID, expected string
	}{
		{"user", "u1", "", "", "quota:rpm:user:u1"},
		{"service_key", "k1", "", "", "quota:rpm:service_key:k1"},
		{"user", "u1", "group", "gpt-4", "quota:rpm:user:u1:group:gpt-4"},
		{"user", "u1", "model", "model-uuid", "quota:rpm:user:u1:model:model-uuid"},
	}
	for _, tt := range tests {
		got := RPMKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
		if got != tt.expected {
			t.Errorf("RPMKey(%s,%s,%s,%s) = %s, want %s", tt.subjectType, tt.subjectID, tt.layer, tt.resourceID, got, tt.expected)
		}
	}
}

func TestConcurrencyKey(t *testing.T) {
	tests := []struct {
		subjectType, subjectID, layer, resourceID, expected string
	}{
		{"user", "u1", "", "", "quota:conc:user:u1"},
		{"service_key", "k1", "group", "gpt-4", "quota:conc:service_key:k1:group:gpt-4"},
	}
	for _, tt := range tests {
		got := ConcurrencyKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
		if got != tt.expected {
			t.Errorf("ConcurrencyKey(%s,%s,%s,%s) = %s, want %s", tt.subjectType, tt.subjectID, tt.layer, tt.resourceID, got, tt.expected)
		}
	}
}
