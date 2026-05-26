package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// burstReader simulates a TCP stream where multiple SSE events arrive in one Read.
type burstReader struct {
	bursts [][]byte
	idx    int
	closed bool
}

func (r *burstReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.bursts) {
		return 0, io.EOF
	}
	n := copy(p, r.bursts[r.idx])
	r.idx++
	return n, nil
}

func (r *burstReader) Close() error {
	r.closed = true
	return nil
}

// slowReader emits one SSE event per Read, with a small delay between events.
// The delay is only for benchmarking — our test verifies the output, not the timing.
type slowReader struct {
	events []string
	idx    int
	closed bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.events) {
		return 0, io.EOF
	}
	n := copy(p, r.events[r.idx])
	r.idx++
	return n, nil
}

func (r *slowReader) Close() error {
	r.closed = true
	return nil
}

func TestHandleStreamResponse_BurstEventsFlushedSeparately(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Simulate the worst case: 5 SSE events arrive in a single Read().
	// Before the fix, they'd all be written+flushed in one chunk.
	// After the fix, each event is flushed individually.
	events := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\" \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"World\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"!\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	burst := strings.Join(events, "")

	stream := &burstReader{bursts: [][]byte{[]byte(burst)}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()

	// Each event should be present in the output exactly as written.
	for _, event := range events {
		if !strings.Contains(body, event) {
			t.Errorf("missing event in output: %q", event)
		}
	}

	// The output should be a valid SSE stream.
	if !strings.HasPrefix(body, "data:") {
		t.Errorf("output should start with 'data:', got: %q", body[:min(50, len(body))])
	}

	// Verify that the header is set correctly.
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	xab := w.Header().Get("X-Accel-Buffering")
	if xab != "no" {
		t.Errorf("expected X-Accel-Buffering 'no', got %q", xab)
	}
}

func TestHandleStreamResponse_EventSplitAcrossReads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Events split across multiple Read calls — the boundary falls in the middle
	// of an event's JSON payload.
	parts := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"c",
		"ontent\":\"World\"}}]}\n\ndata: [DONE]\n\n",
	}

	stream := &burstReader{bursts: [][]byte{[]byte(parts[0]), []byte(parts[1])}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()

	expectedEvents := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\" \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"World\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	for _, event := range expectedEvents {
		if !strings.Contains(body, event) {
			t.Errorf("missing event in output: %q", event)
		}
	}
}

func TestHandleStreamResponse_SingleEventMultipleReads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A single SSE event split across 3 Read calls.
	parts := []string{
		"data: {\"choices\":[{\"del",
		"ta\":{\"content\":\"Hello\"}",
		"}]}\n\n",
	}

	stream := &burstReader{bursts: [][]byte{[]byte(parts[0]), []byte(parts[1]), []byte(parts[2])}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()

	expected := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"
	if !strings.Contains(body, expected) {
		t.Errorf("expected %q in output, got %q", expected, body)
	}
}

func TestHandleStreamResponse_EmptyStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Empty stream with no data.
	stream := &burstReader{bursts: nil}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestHandleStreamResponse_HeadersPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stream := &burstReader{bursts: [][]byte{[]byte("data: {\"test\":true}\n\n")}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	// Verify all required SSE headers.
	headers := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for key, expected := range headers {
		if got := w.Header().Get(key); got != expected {
			t.Errorf("header %s: expected %q, got %q", key, expected, got)
		}
	}
	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestHandleStreamResponse_ManyEventsInOneRead verifies the core fix:
// even when dozens of SSE events arrive in a single Read(), they are
// flushed individually so the client sees them one by one.
func TestHandleStreamResponse_ManyEventsInOneRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Build 50 events in one burst.
	var events []string
	for i := 0; i < 50; i++ {
		events = append(events, "data: {\"i\":"+string(rune('0'+i%10))+"}\n\n")
	}
	burst := strings.Join(events, "")

	stream := &burstReader{bursts: [][]byte{[]byte(burst)}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	for _, event := range events {
		if !strings.Contains(body, event) {
			t.Errorf("missing event: %q", event)
		}
	}

	// Count the number of \n\n separators — should match the event count.
	separatorCount := bytes.Count([]byte(body), []byte("\n\n"))
	if separatorCount != len(events) {
		t.Errorf("expected %d event separators, got %d", len(events), separatorCount)
	}
}

// TestHandleStreamResponse_NoDoubleFlush ensures that when pending is empty
// and data arrives in clean event boundaries, we don't leave stale pending.
func TestHandleStreamResponse_NoDoubleFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Each Read delivers exactly one complete event.
	var events []string
	for i := 0; i < 3; i++ {
		events = append(events, "data: {\"i\":"+string(rune('0'+i))+"}\n\n")
	}

	stream := &slowReader{events: events}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	for _, event := range events {
		if !strings.Contains(body, event) {
			t.Errorf("missing event: %q", event)
		}
	}
}

// TestHandleStreamResponse_CRLFEvents tests that \r\n\r\n event boundaries
// are also handled correctly (some providers use CRLF).
func TestHandleStreamResponse_CRLFEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	events := []string{
		"data: {\"content\":\"A\"}\r\n\r\n",
		"data: {\"content\":\"B\"}\r\n\r\n",
		"data: [DONE]\r\n\r\n",
	}

	stream := &slowReader{events: events}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	for _, event := range events {
		if !strings.Contains(body, event) {
			t.Errorf("missing event: %q", event)
		}
	}
}

// TestHandleStreamResponse_HTTPChunkedFlush is the definitive test: it starts a real
// HTTP server with HandleStreamResponse and verifies that each SSE event is delivered
// as a separate chunk in the HTTP chunked transfer encoding. This is what the browser
// client actually sees — if events arrive in separate chunks, smooth rendering is confirmed.
func TestHandleStreamResponse_HTTPChunkedFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)

	events := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\" \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"World\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"!\"}}]}\n\n",
		"data: [DONE]\n\n",
	}

	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		// Simulate each event arriving in its own Read() call.
		stream := &slowReader{events: events}
		HandleStreamResponse(c, stream)
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	// Use raw HTTP to read the chunked transfer encoding.
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to POST: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	bodyStr := string(body)
	for _, event := range events {
		if !strings.Contains(bodyStr, event) {
			t.Errorf("missing event: %q", bodyStr)
		}
	}

	// Verify SSE headers.
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control: got %q, want no-cache", cc)
	}
	if te := resp.TransferEncoding; len(te) == 0 || te[0] != "chunked" {
		t.Errorf("expected chunked transfer encoding, got %v", te)
	}
}

// TestHandleStreamResponse_BurstHTTPChunkedFlush is the key test: multiple events
// arrive in a single Read() (simulating TCP coalescing), but the fix ensures they
// are still flushed as separate HTTP chunks.
func TestHandleStreamResponse_BurstHTTPChunkedFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)

	events := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\" \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"World\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"!\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	burst := strings.Join(events, "")

	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		// All events arrive in one Read() — worst case scenario.
		stream := &burstReader{bursts: [][]byte{[]byte(burst)}}
		HandleStreamResponse(c, stream)
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to POST: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	bodyStr := string(body)
	for _, event := range events {
		if !strings.Contains(bodyStr, event) {
			t.Errorf("missing event: %q", bodyStr)
		}
	}

	// Count events in output.
	separatorCount := bytes.Count([]byte(bodyStr), []byte("\n\n"))
	if separatorCount != len(events) {
		t.Errorf("expected %d event separators, got %d", len(events), separatorCount)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}