package core

import (
	"encoding/json"
	"testing"
)

func TestChatRequestWithExtraFieldsObject(t *testing.T) {
	// Test that extra_fields object is captured
	jsonData := `{
		"model": "test-model",
		"messages": [{"role": "user", "content": "test"}],
		"extra_fields": {"conversation_id": "test-conv-456"}
	}`

	var req ChatRequest
	if err := json.Unmarshal([]byte(jsonData), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.ExtraFields.IsEmpty() {
		t.Fatal("ExtraFields should not be empty")
	}

	// Try to find extra_fields object
	extraFieldsRaw := req.ExtraFields.Lookup("extra_fields")
	t.Logf("extra_fields raw: %s", string(extraFieldsRaw))

	// Try to find conversation_id directly
	convID := req.ExtraFields.Lookup("conversation_id")
	t.Logf("conversation_id raw: %s", string(convID))

	if len(extraFieldsRaw) > 0 {
		// extra_fields is an object, parse it
		var extraObj map[string]json.RawMessage
		if err := json.Unmarshal(extraFieldsRaw, &extraObj); err == nil {
			if convIDRaw, ok := extraObj["conversation_id"]; ok {
				var convIDStr string
				if err := json.Unmarshal(convIDRaw, &convIDStr); err == nil {
					t.Logf("✅ conversation_id from extra_fields object: %s", convIDStr)
					return
				}
			}
		}
	}

	if len(convID) > 0 {
		var convIDStr string
		if err := json.Unmarshal(convID, &convIDStr); err == nil {
			t.Logf("✅ conversation_id directly in ExtraFields: %s", convIDStr)
			return
		}
	}

	t.Error("❌ Could not extract conversation_id from any location")
}
