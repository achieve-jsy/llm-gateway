package service

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// TestApplyVideoParams_NonVideoModel_ReturnsValidJSON verifies that when a
// non-video model has its video fields cleared, the result serializes to
// a valid JSON array ('[]') rather than an empty string (''), which would
// cause a PostgreSQL jsonb "invalid input syntax" error.
func TestApplyVideoParams_NonVideoModel_ReturnsValidJSON(t *testing.T) {
	enabled := true
	m := &model.Model{
		ID:                         uuid.New(),
		Name:                       "gpt-4o",
		ProviderID:                 uuid.New(),
		ModelType:                  "chat",
		Enabled:                    &enabled,
		VideoSupportedResolutions:  []string{"720p", "1080p"}, // simulate dirty data
		VideoSupportedAspectRatios: []string{"16:9"},
		VideoSupportedActions:      []string{"preview"},
	}

	applyVideoParams(m, nil, nil, nil, nil, nil)

	// After clearing, fields must be empty slices, NOT nil
	if m.VideoSupportedResolutions == nil {
		t.Fatal("VideoSupportedResolutions should be empty slice, got nil")
	}
	if m.VideoSupportedAspectRatios == nil {
		t.Fatal("VideoSupportedAspectRatios should be empty slice, got nil")
	}
	if m.VideoSupportedActions == nil {
		t.Fatal("VideoSupportedActions should be empty slice, got nil")
	}

	// Verify JSON serialization produces valid JSON arrays, not empty strings
	for name, field := range map[string]any{
		"video_supported_resolutions":   m.VideoSupportedResolutions,
		"video_supported_aspect_ratios": m.VideoSupportedAspectRatios,
		"video_supported_actions":       m.VideoSupportedActions,
	} {
		b, err := json.Marshal(field)
		if err != nil {
			t.Fatalf("json.Marshal(%s) failed: %v", name, err)
		}
		got := string(b)
		if got != "[]" {
			t.Fatalf("%s: expected '[]', got %q — PostgreSQL jsonb would reject this", name, got)
		}
	}
}

// TestApplyVideoParams_VideoModel_PreservesValues verifies that for video models
// the provided values are kept and not cleared.
func TestApplyVideoParams_VideoModel_PreservesValues(t *testing.T) {
	enabled := true
	m := &model.Model{
		ID:         uuid.New(),
		Name:       "seedance-v1",
		ProviderID: uuid.New(),
		ModelType:  "video",
		Enabled:    &enabled,
	}

	minDur := 5
	maxDur := 60
	resolutions := []string{"720p", "1080p"}
	ratios := []string{"16:9", "9:16"}
	actions := []string{"preview", "generate"}

	applyVideoParams(m, resolutions, ratios, &minDur, &maxDur, actions)

	if len(m.VideoSupportedResolutions) != 2 || m.VideoSupportedResolutions[0] != "720p" {
		t.Fatalf("VideoSupportedResolutions not set correctly: %v", m.VideoSupportedResolutions)
	}
	if len(m.VideoSupportedAspectRatios) != 2 {
		t.Fatalf("VideoSupportedAspectRatios not set correctly: %v", m.VideoSupportedAspectRatios)
	}
	if m.VideoMinDurationSeconds != 5 {
		t.Fatalf("VideoMinDurationSeconds: got %d, want 5", m.VideoMinDurationSeconds)
	}
	if m.VideoMaxDurationSeconds != 60 {
		t.Fatalf("VideoMaxDurationSeconds: got %d, want 60", m.VideoMaxDurationSeconds)
	}
	if len(m.VideoSupportedActions) != 2 || m.VideoSupportedActions[0] != "preview" {
		t.Fatalf("VideoSupportedActions not set correctly: %v", m.VideoSupportedActions)
	}
}

// TestApplyVideoParams_NilModel_NoPanic verifies nil model doesn't panic.
func TestApplyVideoParams_NilModel_NoPanic(t *testing.T) {
	applyVideoParams(nil, []string{"720p"}, nil, nil, nil, nil)
	// Should not panic
}
