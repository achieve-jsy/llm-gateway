package protocol_test

import (
	"io"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/protocol"
)

func TestReadSSEFrame_SingleLine(t *testing.T) {
	input := "data: {\"key\":\"value\"}\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(frame.Data) != `{"key":"value"}` {
		t.Fatalf("unexpected data: %q", string(frame.Data))
	}
	if frame.Event != "" {
		t.Fatalf("unexpected event: %q", frame.Event)
	}
}

func TestReadSSEFrame_MultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(frame.Data) != "line1\nline2\nline3" {
		t.Fatalf("unexpected data: %q", string(frame.Data))
	}
}

func TestReadSSEFrame_WithEvent(t *testing.T) {
	input := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Event != "ping" {
		t.Fatalf("unexpected event: %q", frame.Event)
	}
	if string(frame.Data) != `{"type":"ping"}` {
		t.Fatalf("unexpected data: %q", string(frame.Data))
	}
}

func TestReadSSEFrame_PartialRead(t *testing.T) {
	input := "data: {\"k"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	_, err := r.ReadFrame()
	if err != io.EOF {
		t.Fatalf("expected EOF on partial frame, got: %v", err)
	}
}

func TestReadSSEFrame_ExceedsLimit(t *testing.T) {
	payload := strings.Repeat("x", 90)
	input := "data: " + payload + "\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 50)
	_, err := r.ReadFrame()
	if err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func TestReadSSEFrame_DONE(t *testing.T) {
	input := "data: [DONE]\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	_, err := r.ReadFrame()
	if err != io.EOF {
		t.Fatalf("expected io.EOF for [DONE], got: %v", err)
	}
}

func TestReadSSEFrame_Comments(t *testing.T) {
	input := ": this is a comment\ndata: {\"key\":\"value\"}\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(frame.Data) != `{"key":"value"}` {
		t.Fatalf("unexpected data: %q", string(frame.Data))
	}
}

func TestReadSSEFrame_MultipleFrames(t *testing.T) {
	input := "data: first\n\ndata: second\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	f1, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error on first frame: %v", err)
	}
	if string(f1.Data) != "first" {
		t.Fatalf("unexpected first data: %q", string(f1.Data))
	}
	f2, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error on second frame: %v", err)
	}
	if string(f2.Data) != "second" {
		t.Fatalf("unexpected second data: %q", string(f2.Data))
	}
	_, err = r.ReadFrame()
	if err != io.EOF {
		t.Fatalf("expected EOF after second frame, got: %v", err)
	}
}

func TestReadSSEFrame_EmptyLines(t *testing.T) {
	input := "\n\n\ndata: {\"key\":\"value\"}\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(frame.Data) != `{"key":"value"}` {
		t.Fatalf("unexpected data: %q", string(frame.Data))
	}
}

func TestReadSSEFrame_OnlyEvent(t *testing.T) {
	input := "event: heartbeat\n\n"
	r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Event != "heartbeat" {
		t.Fatalf("unexpected event: %q", frame.Event)
	}
	if len(frame.Data) > 0 {
		t.Fatalf("expected empty data, got: %q", string(frame.Data))
	}
}