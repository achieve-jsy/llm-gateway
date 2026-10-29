package event_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/redis/go-redis/v9"
)

// 缓存键前缀契约：网关 KeyCache 当前写入 gateway:keycache:v2:<sha256>，
// 旧版本写入 gateway:keycache:<sha256>。吊销时必须把两代前缀都删掉，
// 否则新网关会继续用旧条目（泄漏 5 分钟），这在"吊销一个泄漏的 key"场景下是安全问题。
func TestInvalidateKeyAuthCacheDeletesAllGatewayPrefixes(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	hash := strings.Repeat("a", 64)
	keys := []string{
		"key:auth:verify:" + hash,   // 内嵌 auth service 缓存
		"gateway:keycache:" + hash,  // 旧版网关缓存
		"gateway:keycache:v2:" + hash, // 当前网关缓存
	}
	for _, k := range keys {
		mr.Set(k, "cached")
	}

	event.NewPublisher(rdb).InvalidateKeyAuthCache(context.Background(), hash)

	for _, k := range keys {
		if mr.Exists(k) {
			t.Fatalf("吊销后仍残留缓存条目 %q", k)
		}
	}
}
