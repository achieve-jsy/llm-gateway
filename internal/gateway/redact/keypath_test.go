package redact

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// 固定 KeyPath 时，同一凭证在不同请求（不同 Session）里必须得到同一占位符：
// 这是上游前缀缓存不被脱敏打散的前提。
//
// 注意：占位符的「还原」依赖产生它的那个 Session 的映射表，
// 跨 Session 只需保证字符串一致，不保证能互相还原（网关是单请求内闭环）。
func TestKeyPathKeepsPlaceholdersStableAcrossSessions(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "hmac.key")
	e, err := New(Config{Mode: ModeMask, RulesOnly: true, KeyPath: keyPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	text := "token=" + synthToken

	maskOnce := func() (string, *Session) {
		s := e.NewSession()
		out, err := s.ProtectText(context.Background(), text)
		if err != nil {
			s.Close()
			t.Fatalf("ProtectText: %v", err)
		}
		return out, s
	}

	first, s1 := maskOnce()
	defer s1.Close()
	second, s2 := maskOnce()
	defer s2.Close()

	if first != second {
		t.Fatalf("两次请求占位符不一致（前缀缓存会被打散）: %q vs %q", first, second)
	}
	if !strings.Contains(first, "{{") {
		t.Fatalf("未产生占位符: %q", first)
	}
	// 同一会话内必须能还原
	if got := s1.RestoreText(first); !strings.Contains(got, synthToken) {
		t.Fatalf("同一会话内未还原: %q", got)
	}
}
