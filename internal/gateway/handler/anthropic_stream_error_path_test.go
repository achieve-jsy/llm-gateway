package handler

// 流式错误路径的回归：上游读流中途失败 / 客户端提前关闭。
//
// 背景：convert() 里注释记录过一次真实事故——修复前忽略 scanner.Err() 并照样
// flushAndFinish()，等于给客户端发了一个"正常结束、内容为空"的 message_stop，
// Claude Code 报 "Streaming response ended before any complete data was received"
// 并自动做一次非流式重试。因此这里钉住两条契约：
//  1. 上游读失败 → 错误沿 pipe 传给调用方（由上层写 event: error），**不补发**
//     content_block_stop / message_delta / message_stop；
//     （注："错误传到客户端且不发 message_stop"这一点 stream_error_test.go 的
//     UpstreamErrorPropagates 已覆盖；本文件的增量是**已打开的 tool_use 块不得被 stop**——
//     否则客户端会把半截 JSON 当完整参数执行。）
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
//
// 上游特意用 io.Pipe：只写一个 chunk 就**停住不关写端**，于是转换 goroutine 必然阻塞在
// 读取上游上，只有 adapter.Close() 真的关掉上游读端才能被唤醒。删掉 Close() 里的
// a.upstream.Close() 会让这个用例超时红——用 io.NopCloser + 整段 body 的写法没有这种
// 敏感性（流会被自然读完，wg 照样 done）。
func TestAnthropicStream_ErrorPath_ClientCloseMidStream(t *testing.T) {
	// 只发一个 chunk，且刻意不带 [DONE]：流「还有后续」但永远不来。
	first := "data: " + `{"id":"chatcmpl-partial","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_ls","type":"function","function":{"name":"Bash","arguments":"{\"command\":"}}]}}]}` + "\n\n"

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() {
		_, _ = pw.Write([]byte(first))
		// 写完就停：pipe 写端保持打开，上游一直「等着发后续」。
	}()

	adapter := newAnthropicStreamAdapter(pr, "req_close_path", "claude-3-5-sonnet-20241022", 50)
	buf := make([]byte, 64)
	type readResult struct {
		n   int
		err error
	}
	firstRead := make(chan readResult, 1)
	go func() {
		n, err := adapter.Read(buf)
		firstRead <- readResult{n: n, err: err}
	}()
	select {
	case res := <-firstRead:
		if res.err != nil {
			t.Fatalf("首次 Read 应成功：%v", res.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("首次 Read 超时：上游首个 chunk 没有被解析出任何事件")
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
		t.Fatal("客户端关闭后转换 goroutine 未退出：Close() 很可能没有关闭上游读端")
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
