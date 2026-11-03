package protocol_test

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/protocol"
)

func readAllString(rc io.ReadCloser) string {
	defer rc.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

func oaiToClaudeSession() *protocol.Session {
	return protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"test-model",
		"upstream-model",
	)
}

func mockUpstreamSSE(chunks []string) *io.PipeReader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for _, chunk := range chunks {
			pw.Write([]byte("data: " + chunk + "\n\n"))
		}
	}()
	return pr
}

func chunkJSON(s string) string {
	return s
}

func TestStreamAdapter_BasicText(t *testing.T) {
	chunks := []string{
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello \"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}"),
	}
	upstream := mockUpstreamSSE(chunks)
	s := oaiToClaudeSession()
	adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	if !strings.Contains(output, "event: message_start") {
		t.Fatal("missing message_start event")
	}
	if !strings.Contains(output, "event: content_block_start") {
		t.Fatal("missing content_block_start event")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Fatal("missing content_block_delta event")
	}
	if !strings.Contains(output, "event: content_block_stop") {
		t.Fatal("missing content_block_stop event")
	}
	if !strings.Contains(output, "event: message_delta") {
		t.Fatal("missing message_delta event")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("missing message_stop event")
	}
	if !strings.Contains(output, "text_delta") {
		t.Fatal("missing text_delta content")
	}
}

func TestStreamAdapter_ToolUse(t *testing.T) {
	chunks := []string{
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}]},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}"),
	}
	upstream := mockUpstreamSSE(chunks)
	s := testSession()
	adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	if !strings.Contains(output, "tool_use") {
		t.Fatal("missing tool_use in output")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("missing message_stop event")
	}
}

func TestStreamAdapter_UpstreamReadError(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		pw.CloseWithError(io.ErrUnexpectedEOF)
	}()
	s := testSession()
	adapter := protocol.NewStreamAdapter(pr, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	if !strings.Contains(output, "text_delta") {
		t.Fatal("expected text_delta for the first chunk")
	}
	if strings.Contains(output, "event: message_stop") {
		t.Fatal("should not emit message_stop on upstream error")
	}
}

func TestStreamAdapter_ClientDisconnect(t *testing.T) {
	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pw.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\n"))
		for i := 0; i < 100; i++ {
			_, err := pw.Write([]byte("data: {\"id\":\"2\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"y\"}}]}\n\n"))
			if err != nil {
				return
			}
		}
	}()
	s := testSession()
	adapter := protocol.NewStreamAdapter(pr, s, "test-model", 10, "resp-1")
	buf := make([]byte, 200)
	n, err := adapter.Read(buf)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n == 0 {
		t.Fatal("expected data")
	}
	adapter.Close()
	wg.Wait()
}

func TestStreamAdapter_FinishBeforeUsage(t *testing.T) {
	chunks := []string{
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"text\"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}"),
	}
	upstream := mockUpstreamSSE(chunks)
	s := testSession()
	adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("missing message_stop event")
	}
	if !strings.Contains(output, "output_tokens") {
		t.Fatal("missing output_tokens in usage")
	}
}

func TestStreamAdapter_NoDoneMarker(t *testing.T) {
	chunks := []string{
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"text\"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}"),
	}
	upstream := mockUpstreamSSE(chunks)
	s := testSession()
	adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("missing message_stop event on EOF without [DONE]")
	}
}

func TestStreamAdapter_OutputFormat(t *testing.T) {
	chunks := []string{
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}"),
		chunkJSON("{\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}"),
	}
	upstream := mockUpstreamSSE(chunks)
	s := testSession()
	adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
	output := readAllString(adapter)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "" {
				continue
			}
			if !json.Valid([]byte(payload)) {
				t.Fatalf("invalid JSON in data line: %s", line)
			}
		}
	}
}
