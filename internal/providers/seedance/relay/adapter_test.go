package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdapter_CreateTask(t *testing.T) {
	// Mock upstream server that returns a task ID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		if r.URL.Path != "/seedance/api/v3/contents/generations/tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "cgt-test-123"})
	}))
	defer server.Close()

	adapter := NewAdapter("test-relay", server.URL, "test-api-key")

	taskID, err := adapter.CreateTask(context.Background(), []byte(`{"model":"test","content":[]}`))
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if taskID != "cgt-test-123" {
		t.Errorf("expected taskID 'cgt-test-123', got '%s'", taskID)
	}
}

func TestAdapter_GetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "cgt-test-123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "cgt-test-123",
			"status": "succeeded",
			"content": map[string]string{
				"video_url": "https://example.com/video.mp4",
			},
		})
	}))
	defer server.Close()

	adapter := NewAdapter("test-relay", server.URL, "test-api-key")

	result, err := adapter.GetTask(context.Background(), "cgt-test-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(result, &resp)
	if resp["status"] != "succeeded" {
		t.Errorf("expected status 'succeeded', got '%v'", resp["status"])
	}
}

func TestAdapter_DoAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "CreateAssetGroup") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ResponseMetadata": map[string]string{"RequestId": "req-123"},
			"Result": map[string]string{
				"Id": "group-test-456",
			},
		})
	}))
	defer server.Close()

	adapter := NewAdapter("test-relay", server.URL, "test-api-key")

	result, err := adapter.DoAction(context.Background(), "CreateAssetGroup", []byte(`{"Name":"test-group"}`))
	if err != nil {
		t.Fatalf("DoAction failed: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(result, &resp)
	if resp["Result"] == nil {
		t.Error("expected Result field in response")
	}
}

func TestAdapter_RequestPassthrough(t *testing.T) {
	// Verify that the adapter passes the request body through unchanged
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		// Verify all fields are passed through
		if body["model"] != "doubao-seedance-2-0-260128" {
			t.Errorf("model not passed through correctly: %v", body["model"])
		}
		if body["resolution"] != "720p" {
			t.Errorf("resolution not passed through: %v", body["resolution"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "cgt-test-789"})
	}))
	defer server.Close()

	adapter := NewAdapter("test-relay", server.URL, "test-api-key")

	requestBody := `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}],"resolution":"720p","ratio":"16:9","duration":5}`
	taskID, err := adapter.CreateTask(context.Background(), []byte(requestBody))
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if taskID != "cgt-test-789" {
		t.Errorf("expected taskID 'cgt-test-789', got '%s'", taskID)
	}
}