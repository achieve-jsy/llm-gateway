package orchestrator

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────
// B3: stream_usage_observer — usage 解析
//     BEFORE: json.Marshal(raw) + json.Unmarshal(b, &u)
//     AFTER:  直接从 map[string]any 提取字段
// ─────────────────────────────────────────────────────────────

var sampleUsagePayload = map[string]any{
	"prompt_tokens":     float64(1024),
	"completion_tokens": float64(512),
	"total_tokens":      float64(1536),
}

func usageParseBefore(raw any) *core.Usage {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var u core.Usage
	if err := json.Unmarshal(b, &u); err != nil {
		return nil
	}
	if u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 {
		return &u
	}
	return nil
}

func usageParseAfter(raw any) *core.Usage {
	usageMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	u := core.Usage{
		PromptTokens:     toInt(usageMap["prompt_tokens"]),
		CompletionTokens: toInt(usageMap["completion_tokens"]),
		TotalTokens:      toInt(usageMap["total_tokens"]),
	}
	if u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 {
		return &u
	}
	return nil
}

func BenchmarkUsageParse_Before(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usageParseBefore(sampleUsagePayload)
	}
}

func BenchmarkUsageParse_After(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usageParseAfter(sampleUsagePayload)
	}
}

// ─────────────────────────────────────────────────────────────
// B4: contentBuf 预分配 vs 零初始化
//     模拟 1000 tokens 的流式输出（每 token 约 4 字节）
// ─────────────────────────────────────────────────────────────

func BenchmarkContentBuf_NoPregrow(b *testing.B) {
	gin.SetMode(gin.TestMode)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		obs := &streamUsageObserver{gc: c, start: time.Now()}
		// 模拟 1000 个 token，每个 4 字节
		for j := 0; j < 1000; j++ {
			obs.contentBuf.WriteString("好的! ")
		}
		_ = obs.contentBuf.String()
	}
}

func BenchmarkContentBuf_Pregrow2048(b *testing.B) {
	gin.SetMode(gin.TestMode)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		obs := &streamUsageObserver{gc: c, start: time.Now()}
		obs.contentBuf.Grow(2048)
		for j := 0; j < 1000; j++ {
			obs.contentBuf.WriteString("好的! ")
		}
		_ = obs.contentBuf.String()
	}
}
