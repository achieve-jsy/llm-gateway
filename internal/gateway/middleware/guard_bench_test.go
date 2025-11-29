package middleware

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────
// B6: GatewayGuard — 单锁 vs 双锁，高并发场景
// ─────────────────────────────────────────────────────────────

// BEFORE：单 mu 保护两个数据结构
type guardBefore struct {
	mu            sync.Mutex
	rateData      map[string]*rateCounter
	limitPerMin   int32
	inFlight      map[string]int32
	maxConcurrent int32
}

func (g *guardBefore) acquireAndCheck(key string) (bool, bool) {
	g.mu.Lock()
	current := g.inFlight[key]
	if current >= g.maxConcurrent {
		g.mu.Unlock()
		return false, false
	}
	g.inFlight[key] = current + 1
	// 限流检查（持同一把锁）
	now := time.Now()
	counter, ok := g.rateData[key]
	if !ok || now.After(counter.resetAt) {
		counter = &rateCounter{count: 0, resetAt: now.Add(time.Minute)}
		g.rateData[key] = counter
	}
	counter.count++
	allowed := counter.count <= int(g.limitPerMin)
	if !allowed {
		g.inFlight[key]--
	}
	g.mu.Unlock()
	return true, allowed
}

func (g *guardBefore) release(key string) {
	g.mu.Lock()
	current := g.inFlight[key]
	if current <= 1 {
		delete(g.inFlight, key)
	} else {
		g.inFlight[key] = current - 1
	}
	g.mu.Unlock()
}

// AFTER：双锁，time.Now() 在锁外
type guardAfter struct {
	inFlightMu    sync.Mutex
	inFlight      map[string]int32
	maxConcurrent int32
	rateMu        sync.Mutex
	rateData      map[string]*rateCounter
	limitPerMin   int32
}

func (g *guardAfter) acquireAndCheck(key string) (bool, bool) {
	g.inFlightMu.Lock()
	current := g.inFlight[key]
	if current >= g.maxConcurrent {
		g.inFlightMu.Unlock()
		return false, false
	}
	g.inFlight[key] = current + 1
	g.inFlightMu.Unlock()

	now := time.Now() // 锁外获取时间
	g.rateMu.Lock()
	counter, ok := g.rateData[key]
	if !ok || now.After(counter.resetAt) {
		counter = &rateCounter{count: 0, resetAt: now.Add(time.Minute)}
		g.rateData[key] = counter
	}
	counter.count++
	allowed := counter.count <= int(g.limitPerMin)
	g.rateMu.Unlock()

	if !allowed {
		g.inFlightMu.Lock()
		c := g.inFlight[key]
		if c <= 1 {
			delete(g.inFlight, key)
		} else {
			g.inFlight[key] = c - 1
		}
		g.inFlightMu.Unlock()
	}
	return true, allowed
}

func (g *guardAfter) release(key string) {
	g.inFlightMu.Lock()
	current := g.inFlight[key]
	if current <= 1 {
		delete(g.inFlight, key)
	} else {
		g.inFlight[key] = current - 1
	}
	g.inFlightMu.Unlock()
}

// 串行基准
func BenchmarkGuard_Before_Serial(b *testing.B) {
	g := &guardBefore{
		rateData: make(map[string]*rateCounter), limitPerMin: 1000,
		inFlight: make(map[string]int32), maxConcurrent: 100,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.acquireAndCheck("user-1")
		g.release("user-1")
	}
}

func BenchmarkGuard_After_Serial(b *testing.B) {
	g := &guardAfter{
		rateData: make(map[string]*rateCounter), limitPerMin: 1000,
		inFlight: make(map[string]int32), maxConcurrent: 100,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.acquireAndCheck("user-1")
		g.release("user-1")
	}
}

// 并发基准：10 个 goroutine 同时竞争同一个 key
func BenchmarkGuard_Before_Parallel(b *testing.B) {
	g := &guardBefore{
		rateData: make(map[string]*rateCounter), limitPerMin: 1000000,
		inFlight: make(map[string]int32), maxConcurrent: 100,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ok, _ := g.acquireAndCheck("user-shared")
			if ok {
				g.release("user-shared")
			}
		}
	})
}

func BenchmarkGuard_After_Parallel(b *testing.B) {
	g := &guardAfter{
		rateData: make(map[string]*rateCounter), limitPerMin: 1000000,
		inFlight: make(map[string]int32), maxConcurrent: 100,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ok, _ := g.acquireAndCheck("user-shared")
			if ok {
				g.release("user-shared")
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────
// B7: slog.Info vs slog.Debug（INFO 级别下）
//     模拟路由决策时的日志调用开销
// ─────────────────────────────────────────────────────────────

func BenchmarkSlog_InfoOnInfoLevel(b *testing.B) {
	// 默认 slog 级别是 INFO，slog.Info 会真正执行
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 模拟 router.go 里的两行日志
		infoNoop("router: group found", "group", "claude-sonnet-4-6", "models", 3, "strategy", "priority")
		infoNoop("router: healthy models", "group", "claude-sonnet-4-6", "healthy", 2)
	}
}

func BenchmarkSlog_DebugOnInfoLevel(b *testing.B) {
	// slog.Debug 在 INFO 级别下是真正的 noop（不进入 handler）
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		debugNoop("router: group found", "group", "claude-sonnet-4-6", "models", 3, "strategy", "priority")
		debugNoop("router: healthy models", "group", "claude-sonnet-4-6", "healthy", 2)
	}
}

// 用真实的 slog 包调用
func infoNoop(msg string, args ...any) {
	// 直接调用会影响全局 logger，用 handler 模拟
	w := httptest.NewRecorder()
	_ = w // 仅防止优化掉
	// 实际 benchmark 用真正的 slog
	benchSlogInfo(msg, args...)
}

func debugNoop(msg string, args ...any) {
	benchSlogDebug(msg, args...)
}
