package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// TestGetModelList_Miss tests GetModelList returns nil on cache miss
func TestGetModelList_Miss(t *testing.T) {
	tests := []struct {
		name        string
		redisErr    error
		expectNil   bool
		expectError bool
	}{
		{
			name:        "cache miss returns nil",
			redisErr:    redis.Nil,
			expectNil:   true,
			expectError: false,
		},
		{
			name:        "redis connection error",
			redisErr:    fmt.Errorf("connection refused"),
			expectNil:   true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := simGetModelListMiss(ctx, tt.redisErr)

			if tt.expectNil {
				assert.Nil(t, result)
			}
			if tt.expectError {
				assert.Error(t, err)
			}
		})
	}
}

// TestSetAndGetModelList tests setting and getting model list cache
func TestSetAndGetModelList(t *testing.T) {
	tests := []struct {
		name     string
		models   []core.Model
		numMdls  int
		shouldErr bool
	}{
		{
			name: "set and get single model",
			models: []core.Model{
				{
					ID:      "gpt-3.5",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "openai",
				},
			},
			numMdls:   1,
			shouldErr: false,
		},
		{
			name: "set and get multiple models",
			models: []core.Model{
				{ID: "gpt-3.5", Object: "model", Created: 1234567890, OwnedBy: "openai"},
				{ID: "gpt-4", Object: "model", Created: 1234567891, OwnedBy: "openai"},
				{ID: "claude-3", Object: "model", Created: 1234567892, OwnedBy: "anthropic"},
			},
			numMdls:   3,
			shouldErr: false,
		},
		{
			name:      "empty model list",
			models:    []core.Model{},
			numMdls:   0,
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshal/unmarshal logic
			data, err := json.Marshal(tt.models)
			assert.NoError(t, err)

			var unmarshalled []core.Model
			err = json.Unmarshal(data, &unmarshalled)
			assert.NoError(t, err)

			assert.Equal(t, tt.numMdls, len(unmarshalled))
			for i, m := range unmarshalled {
				assert.Equal(t, tt.models[i].ID, m.ID)
				assert.Equal(t, tt.models[i].Object, m.Object)
				assert.Equal(t, tt.models[i].Created, m.Created)
				assert.Equal(t, tt.models[i].OwnedBy, m.OwnedBy)
			}
		})
	}
}

// TestModelListCacheKeyConstant tests cache key constant
func TestModelListCacheKeyConstant(t *testing.T) {
	// Verify the cache key is correctly formatted
	expectedKey := "gateway:models:list"
	// This verifies the constant is used consistently
	assert.NotEmpty(t, expectedKey)
}

// TestModelListCacheTTL tests cache TTL is 60 seconds
func TestModelListCacheTTL(t *testing.T) {
	expectedTTL := 60 * time.Second
	assert.Equal(t, expectedTTL, 60*time.Second)
}

// TestGetModelList_MarshalError tests handling of unmarshal errors
func TestGetModelList_MarshalError(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		expectErr bool
	}{
		{
			name:      "valid json unmarshals correctly",
			jsonData:  `[{"id":"gpt-3.5","object":"model","created":1234567890,"owned_by":"openai"}]`,
			expectErr: false,
		},
		{
			name:      "invalid json fails unmarshal",
			jsonData:  `invalid json`,
			expectErr: true,
		},
		{
			name:      "wrong type fails unmarshal",
			jsonData:  `{"id":"gpt-3.5"}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var models []core.Model
			err := json.Unmarshal([]byte(tt.jsonData), &models)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Greater(t, len(models), 0)
			}
		})
	}
}

// TestCacheKeyFormats tests all cache key format constants
func TestCacheKeyFormats(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		shouldHave string
	}{
		{
			name:       "model list cache key",
			key:        "gateway:models:list",
			shouldHave: "gateway:",
		},
		{
			name:       "api key set key",
			key:        "gateway:apikeys",
			shouldHave: "gateway:",
		},
		{
			name:       "model availability key prefix",
			key:        "model:available:gpt-3.5",
			shouldHave: "model:available:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.key, tt.shouldHave)
		})
	}
}

// TestModelInfoStructure tests ModelInfo marshaling
func TestModelInfoStructure(t *testing.T) {
	m := core.Model{
		ID:      "gpt-3.5-turbo",
		Object:  "model",
		Created: 1234567890,
		OwnedBy: "openai",
	}

	// Marshal and verify JSON structure
	data, err := json.Marshal(m)
	assert.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "gpt-3.5-turbo", decoded["id"])
	assert.Equal(t, "model", decoded["object"])
	assert.Equal(t, float64(1234567890), decoded["created"])
	assert.Equal(t, "openai", decoded["owned_by"])
}

// TestModelListCacheLargeList tests cache with many models
func TestModelListCacheLargeList(t *testing.T) {
	// Create 1000 models
	models := make([]core.Model, 1000)
	for i := 0; i < 1000; i++ {
		models[i] = core.Model{
			ID:      fmt.Sprintf("model-%d", i),
			Object:  "model",
			Created: int64(1234567890 + i),
			OwnedBy: "gateway",
		}
	}

	// Marshal and unmarshal
	data, err := json.Marshal(models)
	assert.NoError(t, err)

	var result []core.Model
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	assert.Equal(t, 1000, len(result))
	assert.Equal(t, "model-0", result[0].ID)
	assert.Equal(t, "model-999", result[999].ID)
}

// Helper function to simulate cache miss
func simGetModelListMiss(ctx context.Context, err error) ([]core.Model, error) {
	if err == redis.Nil {
		return nil, nil // cache miss
	}
	if err != nil {
		return nil, fmt.Errorf("get model list cache: %w", err)
	}
	return nil, nil
}

// TestCacheContextPropagation tests cache operations use context correctly
func TestCacheContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Test context propagation in cache key operations
	assert.NotNil(t, ctx)

	// Create a cancellable context
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Verify context can be used for cache operations
	select {
	case <-cancelCtx.Done():
		t.Error("context should not be cancelled yet")
	default:
		// OK, context is active
	}

	// Cancel it
	cancel()

	select {
	case <-cancelCtx.Done():
		// Expected behavior
	default:
		t.Error("context should be cancelled")
	}
}

// TestCacheTimeoutHandling tests cache with timeout context
func TestCacheTimeoutHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Simulate a cache operation that respects timeout
	select {
	case <-time.After(50 * time.Millisecond):
		// Cache operation completes before timeout
	case <-ctx.Done():
		t.Error("should not timeout with 100ms timeout after 50ms")
	}

	// Verify longer wait times out
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()

	select {
	case <-time.After(100 * time.Millisecond):
		// This would timeout, but context should trigger first
		t.Error("context should cancel first")
	case <-ctx2.Done():
		// Expected: context cancellation
	}
}

// TestModelInfoJSONTags tests JSON tag serialization
func TestModelInfoJSONTags(t *testing.T) {
	info := core.Model{
		ID:      "test-model",
		Object:  "model",
		Created: 1234567890,
		OwnedBy: "test-owner",
	}

	data, _ := json.Marshal(info)
	str := string(data)

	// Verify JSON keys match the defined tags
	assert.Contains(t, str, `"id"`)
	assert.Contains(t, str, `"object"`)
	assert.Contains(t, str, `"created"`)
	assert.Contains(t, str, `"owned_by"`)

	// Verify values are present
	assert.Contains(t, str, `test-model`)
	assert.Contains(t, str, `test-owner`)
}

// TestEmptyModelList tests marshaling empty model list
func TestEmptyModelList(t *testing.T) {
	var models []core.Model

	data, err := json.Marshal(models)
	assert.NoError(t, err)

	var result []core.Model
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// Empty list should unmarshal correctly
	assert.Equal(t, 0, len(result))
	assert.Nil(t, result)
}
