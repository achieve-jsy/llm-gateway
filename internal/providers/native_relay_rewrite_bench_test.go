package providers

// rewriteNativeBodyModel 的性能基准。
//
// 背景：端到端对比发现大 body 下原生旁路比转换路径慢 4 倍（1.16ms vs 290µs）。
// 原生路径唯一的额外开销就是「改写 body.model」，所以怀疑瓶颈在这里。
//
// 该函数目前用 map[string]json.RawMessage 往返：整个 body 解进 map、
// 换掉 model、再整个序列化回去。map 的 hashing 与 Marshal 时的按 key 排序
// 让它在长上下文上比完整的协议转换还贵。

import (
	"strings"
	"testing"
)

func benchBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"model":"client-group","max_tokens":512,"messages":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"role":"user","content":[{"type":"text","text":"`)
		b.WriteString(strings.Repeat("lorem ipsum dolor sit amet ", 12))
		b.WriteString(`"}]}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func BenchmarkRewriteNativeBodyModel_Small(b *testing.B) {
	body := []byte(`{"model":"client-group","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rewriteNativeBodyModel(body, "upstream-model"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRewriteNativeBodyModel_Large(b *testing.B) {
	body := benchBody(60)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rewriteNativeBodyModel(body, "upstream-model"); err != nil {
			b.Fatal(err)
		}
	}
}
