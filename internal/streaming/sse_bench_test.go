package streaming_test

import (
	"bytes"
	"encoding/json"
	"testing"
)

// ─────────────────────────────────────────────────────────────
// B5: processEvent — SSE chunk 解析
//     BEFORE: bytes.Split + make(payloadLines) + bytes.Join
//     AFTER:  逐字节扫描，单行路径零额外分配
// ─────────────────────────────────────────────────────────────

// 典型单行 SSE event（OpenAI/Bedrock 格式）
var singleLineEvent = []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"delta":{"content":"Hello"},"finish_reason":null,"index":0}]}`)

var donePayload = []byte("[DONE]")

func parseDataLine(line []byte) ([]byte, bool) {
	line = bytes.TrimRight(line, "\r")
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return nil, false
	}
	return line[6:], true
}

// BEFORE 实现
func processEventBefore(event []byte) map[string]any {
	lines := bytes.Split(event, []byte("\n"))
	payloadLines := make([][]byte, 0, len(lines))
	for _, line := range lines {
		jsonData, ok := parseDataLine(line)
		if !ok {
			continue
		}
		payloadLines = append(payloadLines, jsonData)
	}
	if len(payloadLines) == 0 {
		return nil
	}
	jsonData := bytes.Join(payloadLines, []byte("\n"))
	if bytes.Equal(jsonData, donePayload) {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return nil
	}
	return payload
}

// AFTER 实现（与实际代码一致）
func processEventAfter(event []byte) map[string]any {
	var firstPayload []byte
	var extraPayloads [][]byte

	remaining := event
	for len(remaining) > 0 {
		var line []byte
		if idx := bytes.IndexByte(remaining, '\n'); idx >= 0 {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			line = remaining
			remaining = nil
		}
		jsonData, ok := parseDataLine(line)
		if !ok {
			continue
		}
		if firstPayload == nil {
			firstPayload = jsonData
		} else {
			extraPayloads = append(extraPayloads, jsonData)
		}
	}
	if firstPayload == nil {
		return nil
	}
	var jsonData []byte
	if len(extraPayloads) == 0 {
		jsonData = firstPayload
	} else {
		all := make([][]byte, 0, 1+len(extraPayloads))
		all = append(all, firstPayload)
		all = append(all, extraPayloads...)
		jsonData = bytes.Join(all, []byte("\n"))
	}
	if bytes.Equal(jsonData, donePayload) {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return nil
	}
	return payload
}

func BenchmarkProcessEvent_Before(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processEventBefore(singleLineEvent)
	}
}

func BenchmarkProcessEvent_After(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processEventAfter(singleLineEvent)
	}
}

// 整体流：模拟 200 个 chunk 的流式响应
func BenchmarkFullStream_Before(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 200; j++ {
			processEventBefore(singleLineEvent)
		}
	}
}

func BenchmarkFullStream_After(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 200; j++ {
			processEventAfter(singleLineEvent)
		}
	}
}
