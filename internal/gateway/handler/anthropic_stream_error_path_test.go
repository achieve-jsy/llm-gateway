package handler

// 流式错误路径的回归：上游读流中途失败 / 客户端提前关闭。
//
// 背景：convert() 里注释记录过一次真实事故——修复前忽略 scanner.Err() 并照样
// flushAndFinish()，等于给客户端发了一个"正常结束、内容为空"的 message_stop，
// Claude Code 报 "Streaming response ended before any complete data was received"
// 并自动做一次非流式重试。因此这里钉住两条契约：
//  1. 上游读失败 → 错误沿 pipe 传给调用方（由上层写 event: error），**不补发**
//     content_block_stop / message_delta / message_stop；
//  2. 客户端提前 Close → 转换 goroutine 必须退出（不泄漏），且不 panic。

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// failingUpstream 先吐一段合法 SSE，然后把 |err| 作为 Read 错误返回，模拟上游连接中断
// （Bedrock 把 validationException 作为读错误抛出是同一种形态）。
type failingUpstream struct {
	body []byte
	done bool
	err  error
}

func (f *failingUpstream) Read(p []byte) (int, error) {
	if !f.done && len(f.body) > 0 {
		if len(p) >= len(f.body) {
			n := copy(p, f.body)
			f.done = true
			f.body = nil
			return n, nil
		}
		n := copy(p, f.body)
		f.body = f.body[n:]
		return n, nil
	}
	return 0, f.err
}

func (f *failingUpstream) Close() error { return nil }

// TestAnthropicStream_ErrorPath_UpstreamReadError_NoNormalTermination 断言上游读失败时
// 不伪装成正常收尾：错误要传给调用方，且不补发 stop / message_delta / message_stop。
func TestAnthropicStream_ErrorPath_UpstreamReadError_NoNormalTermination(t *testing.T) {
	const partial = `{"comm`
	sse := openAISSE(t,
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "part"}}},
		},
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", partial)),
	)
	sse = strings.TrimSuffix(sse, "data: [DONE]\n\n") // 去掉 [DONE]，让流以错误结束

	upstreamErr := errors.New("connection reset by peer")
	adapter := newAnthropicStreamAdapter(
		&failingUpstream{body: []byte(sse), err: upstreamErr},
		"req_err_path", "claude-3-5-sonnet-20241022", 50,
	)
	out, err := io.ReadAll(adapter)
	if err == nil {
		t.Fatalf("读流应返回上游错误，实际 nil；输出 %q", string(out))
	}
	if !errors.Is(err, upstreamErr) {
		t.Errorf("错误链里应包含上游错误，实际 %v", err)
	}

	body := string(out)
	if !strings.Contains(body, "content_block_start") {
		t.Fatalf("错误发生前已收到的事件应原样保留，实际输出 %q", body)
	}
	// message_delta / message_stop 是"收尾"信号，错误路径绝不能补发它们。
	for _, forbidden := range []string{"message_delta", "message_stop"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("上游读失败后不应再发 %q（否则客户端会把半截工具调用当完整结果）；输出 %q",
				forbidden, body)
		}
	}
	// 错误发生时仍打开的工具块也不能被 stop 掉：它后面不会再有内容，stop 等于宣告
	// "这个 tool_use 的参数到此为止"，客户端会拿半个 JSON 去解析。
	blocks := collectToolUseBlocks(t, parseAnthropicEvents(t, body))
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1；输出 %q", len(blocks), body)
	}
	if blocks[0].StopPos >= 0 {
		t.Errorf("错误路径不应给仍在打开的工具块补 content_block_stop；输出 %q", body)
	}
}

// TestAnthropicStream_ErrorPath_ClientCloseMidStream 断言客户端提前关闭时转换 goroutine
// 会退出（不泄漏）、不 panic，且关闭后的 Read 直接返回错误。
func TestAnthropicStream_ErrorPath_ClientCloseMidStream(t *testing.T) {
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", `{"command":"ls"}`)),
	)
	adapter := newAnthropicStreamAdapter(
		io.NopCloser(strings.NewReader(sse)),
		"req_close_path", "claude-3-5-sonnet-20241022", 50,
	)

	buf := make([]byte, 64)
	if _, err := adapter.Read(buf); err != nil {
		t.Fatalf("首次 Read 应成功：%v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close 返回错误：%v", err)
	}
	if _, err := adapter.Read(buf); err == nil {
		t.Error("Close 之后的 Read 应返回错误")
	}

	concrete, ok := adapter.(*anthropicStreamAdapter)
	if !ok {
		t.Fatalf("adapter 类型不符：%T", adapter)
	}
	done := make(chan struct{})
	go func() {
		concrete.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端关闭后转换 goroutine 未退出（goroutine 泄漏）")
	}
}

// TestAnthropicStream_ErrorPath_ReadErrorBeforeAnyChunk 断言"一个 chunk 都没读出来就失败"
// 时不发 message_start 之外的空壳收尾（否则客户端会先收到 message_start 再等到 EOF）。
func TestAnthropicStream_ErrorPath_ReadErrorBeforeAnyChunk(t *testing.T) {
	upstreamErr := errors.New("dial tcp: connection refused")
	adapter := newAnthropicStreamAdapter(
		&failingUpstream{err: upstreamErr},
		"req_err_empty", "claude-3-5-sonnet-20241022", 50,
	)
	out, err := io.ReadAll(adapter)
	if err == nil {
		t.Fatal("读流应返回上游错误，实际 nil")
	}
	if bytes.Contains(out, []byte("message_stop")) {
		t.Errorf("上游一帧都没给出时不应补发 message_stop；输出 %q", string(out))
	}
}
