package core

import (
	"encoding/json"
	"testing"
)

func TestChatRequestExtraFieldsConversationID(t *testing.T) {
	// Test that conversation_id in top-level fields is captured in ExtraFields
	jsonData := `{
		"model": "test-model",
		"messages": [{"role": "user", "content": "test"}],
		"conversation_id": "test-conv-123",
		"custom_field": "custom_value"
	}`

	var req ChatRequest
	if err := json.Unmarshal([]byte(jsonData), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.ExtraFields.IsEmpty() {
		t.Fatal("ExtraFields should not be empty")
	}

	convID := req.ExtraFields.Lookup("conversation_id")
	if len(convID) == 0 {
		t.Fatal("conversation_id not found in ExtraFields")
	}

	var convIDStr string
	if err := json.Unmarshal(convID, &convIDStr); err != nil {
		t.Fatalf("Failed to parse conversation_id: %v", err)
	}

	if convIDStr != "test-conv-123" {
		t.Errorf("Expected 'test-conv-123', got '%s'", convIDStr)
	}

	t.Logf("✅ conversation_id correctly extracted: %s", convIDStr)
}
