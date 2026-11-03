package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const defaultMaxSSEFrameSize = 2 << 20

// SSEFrame 表示一个解析完成的 SSE 事件帧。
type SSEFrame struct {
	Event string
	Data  []byte
}

// SSEReader 从 io.Reader 中逐帧读取 SSE 事件。
type SSEReader struct {
	scanner      *bufio.Scanner
	maxFrameSize int
}

// NewSSEReader 创建一个新的 SSE 读取器。
func NewSSEReader(r io.Reader, maxFrameSize int) *SSEReader {
	if maxFrameSize <= 0 {
		maxFrameSize = defaultMaxSSEFrameSize
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	return &SSEReader{
		scanner:      scanner,
		maxFrameSize: maxFrameSize,
	}
}

// ReadFrame 读取下一个完整的 SSE 事件帧。
func (r *SSEReader) ReadFrame() (*SSEFrame, error) {
	frame := &SSEFrame{}
	var dataLines []string
	var totalDataLen int
	hasContent := false

	for r.scanner.Scan() {
		line := r.scanner.Text()

		if line == "" {
			if !hasContent {
				continue
			}
			frame.Data = []byte(strings.Join(dataLines, "\n"))
			return frame, nil
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			frame.Event = strings.TrimSpace(line[6:])
			hasContent = true
			continue
		}

		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[5:])
			if payload == "[DONE]" {
				return nil, io.EOF
			}
			totalDataLen += len(payload)
			if totalDataLen > r.maxFrameSize {
				return nil, fmt.Errorf("SSE frame exceeds maximum size of %d bytes", r.maxFrameSize)
			}
			dataLines = append(dataLines, payload)
			hasContent = true
			continue
		}
	}

	if err := r.scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE scanner error: %w", err)
	}

	// EOF without complete frame: return io.EOF
	if hasContent {
		return nil, io.EOF
	}
	return nil, io.EOF
}