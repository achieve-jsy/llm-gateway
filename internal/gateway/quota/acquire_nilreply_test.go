package quota

// redis.Nil 空回复路径的**顺序无关**用例。
//
// 背景：`concurrencyAcquireScript` 与 `rpmCheckScript` 恒定返回整数，绝不会产生空回复；
// 历史上「用 Lua false 表示超限」正好踩中 RESP2 把 false 编码成空回复、go-redis 解码为
// redis.Nil 的坑，于是「超限」被 fail-open 分支当成 Redis 故障而放行，限额静默失效。
//
// 为了不依赖脚本内容、也不受用例执行顺序影响，这里在 **客户端层**用 hook 把 EVALSHA 的
// 返回值直接替换成 redis.Nil，从而确定性地触发该路径。断言：必须**拒绝**（返回非 nil
// error），绝不允许 fail-open 放行（返回 ("", nil)）。
import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

// nilReplyHook 把匹配 cmd 的命令返回值替换成 redis.Nil（空回复）。
type nilReplyHook struct {
	cmd string
}

func (h nilReplyHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h nilReplyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == h.cmd {
			return redis.Nil
		}
		return next(ctx, cmd)
	}
}

func (h nilReplyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestAcquireConcurrency_NilReply_RejectsInsteadOfFailOpen
// 空回复（redis.Nil）必须被判为**协议异常并拒绝**，而不是 fail-open 放行。
func TestAcquireConcurrency_NilReply_RejectsInsteadOfFailOpen(t *testing.T) {
	mr, rdb := newStoreWithClient(t)
	_ = mr
	key := "test:conc:nil-reply"
	ctx := context.Background()

	rdb.AddHook(nilReplyHook{cmd: "evalsha"})
	store := NewRedisStore(rdb)

	lease, err := store.AcquireConcurrency(ctx, key, 5)
	if err == nil {
		t.Fatalf("空回复必须被拒绝，绝不能 fail-open 放行：实际 lease=%q err=nil", lease)
	}
	if lease != "" {
		t.Fatalf("异常路径不得返回 lease id，实际 %q", lease)
	}
	// 必须是"协议异常"而不是"超限"，以便运维区分脚本异常与业务拒绝
	if !contains(err.Error(), "protocol_error") {
		t.Fatalf("应报协议异常（便于区分业务超限），实际 %v", err)
	}
	// 不得留下成员
	if n := concVal(t, rdb, key); n != 0 {
		t.Fatalf("异常路径不得写入成员，实际 ZCARD=%d", n)
	}
}

// TestCheckRPM_NilReply_RejectsInsteadOfFailOpen RPM 侧同理。
func TestCheckRPM_NilReply_RejectsInsteadOfFailOpen(t *testing.T) {
	_, rdb := newStoreWithClient(t)
	key := "test:rpm:nil-reply"
	ctx := context.Background()

	rdb.AddHook(nilReplyHook{cmd: "evalsha"})
	store := NewRedisStore(rdb)

	err := store.CheckRPM(ctx, key, 10)
	if err == nil {
		t.Fatal("空回复必须被拒绝，绝不能 fail-open 放行")
	}
	if !contains(err.Error(), "protocol_error") {
		t.Fatalf("应报协议异常，实际 %v", err)
	}
}

// contains 避免额外引入 strings 依赖时的可读性包装。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
