package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return client, mr
}

func TestStickyStore_GetSet(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Test Set and Get
	err := store.Set(ctx, "test-group", "session-123", "model-42", 10*time.Second)
	require.NoError(t, err)

	modelID, err := store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-42", modelID)
}

func TestStickyStore_GetNonExistent(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Get non-existent key should return empty string, no error
	modelID, err := store.Get(ctx, "test-group", "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "", modelID)
}

func TestStickyStore_TTL(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set with 1 second TTL
	err := store.Set(ctx, "test-group", "session-123", "model-42", 1*time.Second)
	require.NoError(t, err)

	// Should exist immediately
	modelID, err := store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-42", modelID)

	// Fast-forward time in miniredis
	mr.FastForward(2 * time.Second)

	// Should be expired
	modelID, err = store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "", modelID)
}

func TestStickyStore_Delete(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set a value
	err := store.Set(ctx, "test-group", "session-123", "model-42", 10*time.Second)
	require.NoError(t, err)

	// Verify it exists
	modelID, err := store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-42", modelID)

	// Delete it
	err = store.Delete(ctx, "test-group", "session-123")
	require.NoError(t, err)

	// Should be gone
	modelID, err = store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "", modelID)
}

func TestStickyStore_EmptySessionKey(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set with empty session key should be no-op
	err := store.Set(ctx, "test-group", "", "model-42", 10*time.Second)
	require.NoError(t, err)

	// Get with empty session key should return empty
	modelID, err := store.Get(ctx, "test-group", "")
	require.NoError(t, err)
	assert.Equal(t, "", modelID)

	// Delete with empty session key should be no-op
	err = store.Delete(ctx, "test-group", "")
	require.NoError(t, err)
}

func TestStickyStore_KeyFormat(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set a value
	err := store.Set(ctx, "gpt-4-group", "conv_abc123", "42", 10*time.Second)
	require.NoError(t, err)

	// Verify the key format in Redis
	key := "llm:sticky:gpt-4-group:conv_abc123"
	val, err := client.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, "42", val)
}

func TestStickyStore_NilClient(t *testing.T) {
	// Create store with nil client (should be no-op)
	store := NewStickyStore(nil)
	ctx := context.Background()

	// All operations should be safe no-ops
	err := store.Set(ctx, "test-group", "session-123", "model-42", 10*time.Second)
	assert.NoError(t, err)

	modelID, err := store.Get(ctx, "test-group", "session-123")
	assert.NoError(t, err)
	assert.Equal(t, "", modelID)

	err = store.Delete(ctx, "test-group", "session-123")
	assert.NoError(t, err)
}

func TestStickyStore_MultipleGroups(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set different models for same session in different groups
	err := store.Set(ctx, "group-a", "session-123", "model-1", 10*time.Second)
	require.NoError(t, err)

	err = store.Set(ctx, "group-b", "session-123", "model-2", 10*time.Second)
	require.NoError(t, err)

	// Each group should have its own mapping
	modelID, err := store.Get(ctx, "group-a", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-1", modelID)

	modelID, err = store.Get(ctx, "group-b", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-2", modelID)
}

func TestStickyStore_OverwriteValue(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	store := NewStickyStore(client)
	ctx := context.Background()

	// Set initial value
	err := store.Set(ctx, "test-group", "session-123", "model-1", 10*time.Second)
	require.NoError(t, err)

	// Overwrite with new value (simulating failover)
	err = store.Set(ctx, "test-group", "session-123", "model-2", 10*time.Second)
	require.NoError(t, err)

	// Should return the new value
	modelID, err := store.Get(ctx, "test-group", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "model-2", modelID)
}
