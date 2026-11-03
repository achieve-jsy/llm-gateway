package core

import (
	"bytes"
	"encoding/json"
	"testing"
)

func ptrF(v float64) *float64 { return &v }

func TestContentPartVideoURL(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantType   string
		wantURL    string
		wantFileID string
		wantFPS    *float64
		wantErr    bool
	}{
		{
			name:     "object form",
			in:       `{"type":"video_url","video_url":{"url":"https://a/b.mp4","fps":2}}`,
			wantType: "video_url",
			wantURL:  "https://a/b.mp4",
			wantFPS:  ptrF(2),
		},
		{
			name:     "string form",
			in:       `{"type":"video_url","video_url":"data:video/mp4;base64,AAA"}`,
			wantType: "video_url",
			wantURL:  "data:video/mp4;base64,AAA",
		},
		{
			name:       "input_video alias",
			in:         `{"type":"input_video","video_url":{"file_id":"file-1"}}`,
			wantType:   "video_url",
			wantFileID: "file-1",
		},
		{
			name:     "input_video top-level fps",
			in:       `{"type":"input_video","video_url":"https://a/b.mp4","fps":1}`,
			wantType: "video_url",
			wantURL:  "https://a/b.mp4",
			wantFPS:  ptrF(1),
		},
		{
			name:    "missing url and file_id",
			in:      `{"type":"video_url","video_url":{}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var part ContentPart
			err := json.Unmarshal([]byte(tc.in), &part)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (part=%+v)", part)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if part.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", part.Type, tc.wantType)
			}
			if part.VideoURL == nil {
				t.Fatal("VideoURL is nil")
			}
			if part.VideoURL.URL != tc.wantURL {
				t.Fatalf("url = %q, want %q", part.VideoURL.URL, tc.wantURL)
			}
			if part.VideoURL.FileID != tc.wantFileID {
				t.Fatalf("file_id = %q, want %q", part.VideoURL.FileID, tc.wantFileID)
			}
			if tc.wantFPS == nil {
				if part.VideoURL.FPS != nil {
					t.Fatalf("fps = %v, want nil", *part.VideoURL.FPS)
				}
			} else if part.VideoURL.FPS == nil || *part.VideoURL.FPS != *tc.wantFPS {
				t.Fatalf("fps = %v, want %v", part.VideoURL.FPS, *tc.wantFPS)
			}
		})
	}
}

func TestContentPartVideoURLRoundTrip(t *testing.T) {
	const in = `{"type":"video_url","video_url":{"url":"https://a/b.mp4","fps":2}}`
	var part ContentPart
	if err := json.Unmarshal([]byte(in), &part); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var again ContentPart
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	out2, err := json.Marshal(again)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("round trip drifted:\n first=%s\nsecond=%s", out, out2)
	}
	if again.VideoURL == nil || again.VideoURL.FPS == nil || *again.VideoURL.FPS != 2 {
		t.Fatalf("fps lost in round trip: %+v", again.VideoURL)
	}
}

func TestHasVideoContent(t *testing.T) {
	videoMsg := Message{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "what is in this video?"},
		map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://a/b.mp4"}},
	}}
	textMsg := Message{Role: "user", Content: "hello"}
	imageMsg := Message{Role: "user", Content: []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://a/b.png"}},
	}}

	if !HasVideoContent([]Message{textMsg, videoMsg}) {
		t.Fatal("expected true for messages containing video part")
	}
	if HasVideoContent([]Message{textMsg, imageMsg}) {
		t.Fatal("expected false for text/image only")
	}
	if HasVideoContent(nil) {
		t.Fatal("expected false for empty messages")
	}
}

func TestParseVideoURLContent(t *testing.T) {
	obj := map[string]any{"url": "https://a/b.mp4", "fps": float64(2)}
	v, ok := ParseVideoURLContent(obj)
	if !ok || v == nil || v.URL != "https://a/b.mp4" || v.FPS == nil || *v.FPS != 2 {
		t.Fatalf("object parse failed: ok=%v v=%+v", ok, v)
	}
	v, ok = ParseVideoURLContent("data:video/mp4;base64,AAA")
	if !ok || v == nil || v.URL != "data:video/mp4;base64,AAA" {
		t.Fatalf("string parse failed: ok=%v v=%+v", ok, v)
	}
	if _, ok := ParseVideoURLContent(42); ok {
		t.Fatal("expected false for unsupported type")
	}
}
