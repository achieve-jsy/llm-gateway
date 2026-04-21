package scheduler

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestExtractSessionKey_ConversationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// Create ChatRequest with conversation_id in ExtraFields
	conversationID := "conv_abc123"
	extraFields := map[string]json.RawMessage{
		"conversation_id": json.RawMessage(`"` + conversationID + `"`),
	}
	req := &core.ChatRequest{
		ExtraFields: core.UnknownJSONFieldsFromMap(extraFields),
	}

	sessionKey := ExtractSessionKey(c, req)
	assert.Equal(t, conversationID, sessionKey)
}

func TestExtractSessionKey_KeyIDFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// Set UserIdentity with KeyID
	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	// ChatRequest without conversation_id
	req := &core.ChatRequest{}

	sessionKey := ExtractSessionKey(c, req)

	// Should return hash of KeyID (32 hex chars)
	assert.Len(t, sessionKey, 32)
	assert.NotEqual(t, "", sessionKey)
	assert.NotEqual(t, "test-key-id-123", sessionKey) // Should be hashed
}

func TestExtractSessionKey_ConversationIDTakesPrecedence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// Set both UserIdentity and conversation_id
	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	conversationID := "conv_xyz789"
	extraFields := map[string]json.RawMessage{
		"conversation_id": json.RawMessage(`"` + conversationID + `"`),
	}
	req := &core.ChatRequest{
		ExtraFields: core.UnknownJSONFieldsFromMap(extraFields),
	}

	sessionKey := ExtractSessionKey(c, req)

	// conversation_id should take precedence
	assert.Equal(t, conversationID, sessionKey)
}

func TestExtractSessionKey_NoIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// No identity in context, no conversation_id
	req := &core.ChatRequest{}

	sessionKey := ExtractSessionKey(c, req)
	assert.Equal(t, "", sessionKey)
}

func TestExtractSessionKey_EmptyKeyID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// Identity with empty KeyID
	identity := &model.UserIdentity{
		KeyID: "",
	}
	c.Set("identity", identity)

	req := &core.ChatRequest{}

	sessionKey := ExtractSessionKey(c, req)
	assert.Equal(t, "", sessionKey)
}

func TestExtractSessionKey_EmptyExtraFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	// ChatRequest with empty ExtraFields
	req := &core.ChatRequest{
		ExtraFields: core.UnknownJSONFields{},
	}

	sessionKey := ExtractSessionKey(c, req)

	// Should fall back to KeyID hash
	assert.Len(t, sessionKey, 32)
	assert.NotEqual(t, "", sessionKey)
}

func TestExtractSessionKey_NilRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	sessionKey := ExtractSessionKey(c, nil)

	// Should fall back to KeyID hash
	assert.Len(t, sessionKey, 32)
	assert.NotEqual(t, "", sessionKey)
}

func TestExtractSessionKey_InvalidConversationIDJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	// Invalid JSON for conversation_id
	extraFields := map[string]json.RawMessage{
		"conversation_id": json.RawMessage(`{invalid}`),
	}
	req := &core.ChatRequest{
		ExtraFields: core.UnknownJSONFieldsFromMap(extraFields),
	}

	sessionKey := ExtractSessionKey(c, req)

	// Should fall back to KeyID hash
	assert.Len(t, sessionKey, 32)
	assert.NotEqual(t, "", sessionKey)
}

func TestExtractSessionKey_EmptyConversationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	identity := &model.UserIdentity{
		KeyID: "test-key-id-123",
	}
	c.Set("identity", identity)

	// Empty conversation_id
	extraFields := map[string]json.RawMessage{
		"conversation_id": json.RawMessage(`""`),
	}
	req := &core.ChatRequest{
		ExtraFields: core.UnknownJSONFieldsFromMap(extraFields),
	}

	sessionKey := ExtractSessionKey(c, req)

	// Should fall back to KeyID hash
	assert.Len(t, sessionKey, 32)
	assert.NotEqual(t, "", sessionKey)
}

func TestExtractSessionKey_ConsistentHashing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c1, _ := gin.CreateTestContext(nil)
	c2, _ := gin.CreateTestContext(nil)

	keyID := "test-key-id-123"

	identity1 := &model.UserIdentity{KeyID: keyID}
	c1.Set("identity", identity1)

	identity2 := &model.UserIdentity{KeyID: keyID}
	c2.Set("identity", identity2)

	req1 := &core.ChatRequest{}
	req2 := &core.ChatRequest{}

	sessionKey1 := ExtractSessionKey(c1, req1)
	sessionKey2 := ExtractSessionKey(c2, req2)

	// Same KeyID should produce same hash
	assert.Equal(t, sessionKey1, sessionKey2)
}

func TestExtractSessionKey_DifferentKeyIDsDifferentHashes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c1, _ := gin.CreateTestContext(nil)
	c2, _ := gin.CreateTestContext(nil)

	identity1 := &model.UserIdentity{KeyID: "key-1"}
	c1.Set("identity", identity1)

	identity2 := &model.UserIdentity{KeyID: "key-2"}
	c2.Set("identity", identity2)

	req1 := &core.ChatRequest{}
	req2 := &core.ChatRequest{}

	sessionKey1 := ExtractSessionKey(c1, req1)
	sessionKey2 := ExtractSessionKey(c2, req2)

	// Different KeyIDs should produce different hashes
	assert.NotEqual(t, sessionKey1, sessionKey2)
}

func TestExtractSessionKey_HashLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	identity := &model.UserIdentity{
		KeyID: "test-key-id-with-a-very-long-name-to-test-hash-truncation",
	}
	c.Set("identity", identity)

	req := &core.ChatRequest{}

	sessionKey := ExtractSessionKey(c, req)

	// Hash should always be 32 hex chars (16 bytes * 2)
	assert.Len(t, sessionKey, 32)
	assert.Regexp(t, "^[0-9a-f]{32}$", sessionKey)
}
