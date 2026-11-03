package protocol_test

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/protocol"
)

var benchBridgeJSON = []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hello, how are you today?"}],"max_tokens":1024}`)

func benchBridgeToolsJSONBytes() []byte {
	tools := make([]string, 5)
	for i := 0; i < 5; i++ {
		tools[i] = fmt.Sprintf(`{
			"name": "tool_%d",
			"description": "Tool number %d",
			"input_schema": {
				"type": "object",
				"properties": {
					"param": {"type": "string", "description": "A parameter"}
				},
				"required": ["param"]
			}
		}`, i, i)
	}
	return []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Use tools please"}],"max_tokens":1024,"tools":[%s]}`,
		strings.Join(tools, ",")))
}

func benchSession() *protocol.Session {
	return protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)
}

// BenchmarkBridge_ClaudeToChat benchmarks the full decode → convert → bridge pipeline.
func BenchmarkBridge_ClaudeToChat(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		claudeReq, err := protocol.DecodeClaudeRequest(benchBridgeJSON)
		if err != nil {
			b.Fatal(err)
		}
		s := benchSession()
		result, err := protocol.ConvertRequest(s, claudeReq)
		if err != nil {
			b.Fatal(err)
		}
		_, err = protocol.BridgeToChatRequest(s, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBridge_ClaudeToChat_WithTools benchmarks the pipeline with 5 tool definitions.
func BenchmarkBridge_ClaudeToChat_WithTools(b *testing.B) {
	b.ReportAllocs()
	benchToolsJSON := benchBridgeToolsJSONBytes()
	for b.Loop() {
		claudeReq, err := protocol.DecodeClaudeRequest(benchToolsJSON)
		if err != nil {
			b.Fatal(err)
		}
		s := benchSession()
		result, err := protocol.ConvertRequest(s, claudeReq)
		if err != nil {
			b.Fatal(err)
		}
		_, err = protocol.BridgeToChatRequest(s, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSSEReader_SingleFrame benchmarks reading one SSE frame.
func BenchmarkSSEReader_SingleFrame(b *testing.B) {
	input := "data: {\"key\":\"value\"}\n\n"
	b.ReportAllocs()
	for b.Loop() {
		r := protocol.NewSSEReader(strings.NewReader(input), 2<<20)
		_, err := r.ReadFrame()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func buildSSEString(frameCount int) string {
	var sb strings.Builder
	for i := 0; i < frameCount; i++ {
		payload, _ := json.Marshal(map[string]any{"id": i, "content": "test data for frame", "count": i})
		sb.WriteString("data: ")
		sb.Write(payload)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// BenchmarkSSEReader_100Frames benchmarks reading 100 consecutive SSE frames.
func BenchmarkSSEReader_100Frames(b *testing.B) {
	frameCount := 100
	bulkInput := buildSSEString(frameCount)

	b.ReportAllocs()
	for b.Loop() {
		r := protocol.NewSSEReader(strings.NewReader(bulkInput), 2<<20)
		for i := 0; i < frameCount; i++ {
			_, err := r.ReadFrame()
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// mockUpstreamSSEBench creates a pipe reader with the given number of chunks.
func mockUpstreamSSEBench(chunkCount int) *io.PipeReader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < chunkCount; i++ {
			chunk := fmt.Sprintf(
				`{"id":"chatcmpl-%d","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"token %d "},"finish_reason":null}]}`,
				i, i,
			)
			if i == chunkCount-1 {
				chunk = fmt.Sprintf(
					`{"id":"chatcmpl-%d","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
					i,
				)
			}
			pw.Write([]byte("data: " + chunk + "\n\n"))
		}
	}()
	return pr
}

// readAllDiscard reads and discards all output.
func readAllDiscard(rc io.ReadCloser) {
	defer rc.Close()
	buf := make([]byte, 8192)
	for {
		_, err := rc.Read(buf)
		if err != nil {
			break
		}
	}
}

// BenchmarkStreamAdapter_10Chunks benchmarks streaming 10 chunks through the adapter.
func BenchmarkStreamAdapter_10Chunks(b *testing.B) {
	s := benchSession()
	b.ReportAllocs()
	for b.Loop() {
		upstream := mockUpstreamSSEBench(10)
		adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
		readAllDiscard(adapter)
	}
}

// BenchmarkStreamAdapter_100Chunks benchmarks streaming 100 chunks through the adapter.
func BenchmarkStreamAdapter_100Chunks(b *testing.B) {
	s := benchSession()
	b.ReportAllocs()
	for b.Loop() {
		upstream := mockUpstreamSSEBench(100)
		adapter := protocol.NewStreamAdapter(upstream, s, "test-model", 10, "resp-1")
		readAllDiscard(adapter)
	}
}

// BenchmarkCollectDiagnostics_50Diags benchmarks merging 50 diagnostics.
func BenchmarkCollectDiagnostics_50Diags(b *testing.B) {
	relayDiags := make([]types.ConversionDiagnostic, 25)
	for i := 0; i < 25; i++ {
		relayDiags[i] = types.ConversionDiagnostic{
			Code:     fmt.Sprintf("relay_code_%d", i),
			Message:  fmt.Sprintf("relay diagnostic %d", i),
			Severity: types.ConversionDiagnosticWarning,
		}
	}
	bridgeDiags := make([]types.ConversionDiagnostic, 25)
	for i := 0; i < 25; i++ {
		bridgeDiags[i] = types.ConversionDiagnostic{
			Code:     fmt.Sprintf("bridge_code_%d", i),
			Message:  fmt.Sprintf("bridge diagnostic %d", i),
			Severity: types.ConversionDiagnosticWarning,
		}
	}
	s := benchSession()

	b.ReportAllocs()
	for b.Loop() {
		diags := protocol.CollectDiagnostics(s, relayDiags, bridgeDiags)
		if len(diags) != 50 {
			b.Fatalf("expected 50 diagnostics, got %d", len(diags))
		}
	}
}
