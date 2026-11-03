package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testInternalToken = "internal-token"

	// 假控制面返回的合法组名载荷（真实接口形如 {"data":[{"name":...}]}）
	validGroupsPayload = `{"data":[{"name":"gpt-5.6-terra"},{"name":"gpt-5.6-luna"}]}`
)

// 假控制面的响应模式；cpModeOK 必须为 0 以匹配 atomic.Int32 的零值。
const (
	cpModeOK int32 = iota
	cpModeServerError
	cpModeBadJSON
)

// fakeControlPlane 是可切换响应的假控制面，用于覆盖刷新成功/失败/非法 JSON 等场景。
type fakeControlPlane struct {
	srv       *httptest.Server
	mode      atomic.Int32
	failFirst atomic.Int64 // 前 N 个请求返回 500（用于「先失败 N 次再成功」）
	requests  atomic.Int64
	lastToken atomic.Value // string
}

func newFakeControlPlane() *fakeControlPlane {
	f := &fakeControlPlane{}
	f.lastToken.Store("")
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeControlPlane) handle(w http.ResponseWriter, r *http.Request) {
	n := f.requests.Add(1)
	f.lastToken.Store(r.Header.Get("X-Auth-Token"))

	if r.URL.Path != groupNameEndpoint {
		http.NotFound(w, r)
		return
	}

	mode := f.mode.Load()
	if mode == cpModeOK && n <= f.failFirst.Load() {
		mode = cpModeServerError
	}

	switch mode {
	case cpModeServerError:
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"control plane not ready"}`))
	case cpModeBadJSON:
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [`))
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validGroupsPayload))
	}
}

func (f *fakeControlPlane) URL() string          { return f.srv.URL }
func (f *fakeControlPlane) Close()               { f.srv.Close() }
func (f *fakeControlPlane) Requests() int64      { return f.requests.Load() }
func (f *fakeControlPlane) SetFailFirst(n int64) { f.failFirst.Store(n) }
func (f *fakeControlPlane) SetMode(m int32)      { f.mode.Store(m) }
func (f *fakeControlPlane) LastToken() string {
	v, _ := f.lastToken.Load().(string)
	return v
}

// assertReadyWithNames 校验缓存处于「已就绪且组名完整」的状态。
func assertReadyWithNames(t *testing.T, gc *GroupNameCache) {
	t.Helper()
	if !gc.IsInitialized() {
		t.Fatal("已成功刷新的缓存必须保持 initialized=true")
	}
	for _, name := range []string{"gpt-5.6-terra", "gpt-5.6-luna"} {
		if !gc.IsGroupName(name) {
			t.Fatalf("刷新失败后已有的组名 %q 不应丢失", name)
		}
	}
	if gc.IsGroupName("gpt-5.6-not-a-group") {
		t.Fatal("不应出现未下发过的组名")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// ① Refresh 成功：写入组名、标记 initialized，并携带内部 token。
func TestRefreshSuccessSetsNamesAndInitialized(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()

	gc := NewGroupNameCache(testInternalToken)
	if gc.IsInitialized() {
		t.Fatal("新建的缓存不应处于 initialized 状态")
	}
	if gc.IsGroupName("gpt-5.6-terra") {
		t.Fatal("新建的缓存不应包含任何组名")
	}

	if err := gc.Refresh(context.Background(), cp.URL()); err != nil {
		t.Fatalf("Refresh 应成功，实际报错: %v", err)
	}

	assertReadyWithNames(t, gc)
	if got := cp.LastToken(); got != testInternalToken {
		t.Fatalf("刷新请求应携带内部 token，实际 X-Auth-Token=%q", got)
	}
	if got := cp.Requests(); got != 1 {
		t.Fatalf("刷新一次应只发起一个请求，实际 %d", got)
	}
}

// ② 「先失败 N 次再成功」以及后续失败，都不得清空已就绪状态与已写入组名。
func TestRefreshFailureKeepsReadyStateAndNames(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()

	gc := NewGroupNameCache(testInternalToken)
	if err := gc.Refresh(context.Background(), cp.URL()); err != nil {
		t.Fatalf("首次 Refresh 应成功，实际报错: %v", err)
	}
	assertReadyWithNames(t, gc)

	// 连续 3 次 HTTP 500：每次都返回错误，但不得清空状态
	cp.SetFailFirst(1 << 30)
	for i := 1; i <= 3; i++ {
		if err := gc.Refresh(context.Background(), cp.URL()); err == nil {
			t.Fatalf("第 %d 次刷新收到 500 应返回错误", i)
		}
		assertReadyWithNames(t, gc)
	}

	// 非法 JSON
	cp.SetMode(cpModeBadJSON)
	if err := gc.Refresh(context.Background(), cp.URL()); err == nil {
		t.Fatal("刷新收到非法 JSON 应返回错误")
	}
	assertReadyWithNames(t, gc)
	cp.SetMode(cpModeOK)

	// 控制面整体不可达（连接被拒绝）
	dead := newFakeControlPlane()
	deadURL := dead.URL()
	dead.Close()
	if err := gc.Refresh(context.Background(), deadURL); err == nil {
		t.Fatal("控制面不可达时应返回错误")
	}
	assertReadyWithNames(t, gc)
}

// ③ 未就绪的缓存收到非 200 / 非法 JSON 时返回错误，且不污染 names 与 initialized。
func TestRefreshFailureDoesNotPolluteFreshCache(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()

	gc := NewGroupNameCache(testInternalToken)

	cp.SetMode(cpModeServerError)
	if err := gc.Refresh(context.Background(), cp.URL()); err == nil {
		t.Fatal("非 200 响应应返回错误")
	}
	if gc.IsInitialized() {
		t.Fatal("失败的刷新不得把 initialized 置为 true")
	}
	if gc.IsGroupName("gpt-5.6-terra") {
		t.Fatal("失败的刷新不得写入组名")
	}

	cp.SetMode(cpModeBadJSON)
	if err := gc.Refresh(context.Background(), cp.URL()); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	if gc.IsInitialized() {
		t.Fatal("非法 JSON 的刷新不得把 initialized 置为 true")
	}
	if gc.IsGroupName("gpt-5.6-terra") {
		t.Fatal("非法 JSON 的刷新不得写入组名")
	}

	// 控制面恢复后必须能正常就绪
	cp.SetMode(cpModeOK)
	if err := gc.Refresh(context.Background(), cp.URL()); err != nil {
		t.Fatalf("控制面恢复后 Refresh 应成功，实际报错: %v", err)
	}
	assertReadyWithNames(t, gc)
}

// 快速重试语义：首次控制面刷新成功前按 fast 间隔重试，成功后回落到 steady 间隔。
func TestRefreshLoopRetriesFastUntilFirstControlPlaneSuccess(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()
	cp.SetFailFirst(3) // 前 3 个请求 500，第 4 个成功

	gc := NewGroupNameCache(testInternalToken)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		gc.RefreshLoop(ctx, cp.URL(), 10*time.Millisecond, time.Hour)
	}()

	waitFor(t, 3*time.Second, gc.IsInitialized, "首次控制面刷新成功后缓存应就绪")
	assertReadyWithNames(t, gc)

	// 3 次失败 + 1 次成功
	if got := cp.Requests(); got != 4 {
		t.Fatalf("首次成功前的请求数应为 4（3 次失败 + 1 次成功），实际 %d", got)
	}

	// 已切换为 steady 间隔（1h），短时间内不应再有请求
	time.Sleep(200 * time.Millisecond)
	if got := cp.Requests(); got != 4 {
		t.Fatalf("成功后应回落到 steady 间隔，请求数不应继续增长，实际 %d", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 RefreshLoop 应退出")
	}
}

// 快速重试期间也必须能被 ctx 取消，且失败不清空预热/已就绪状态。
func TestRefreshLoopStopsOnContextCancelAndKeepsState(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()
	cp.SetMode(cpModeServerError)

	gc := NewGroupNameCache(testInternalToken)
	// 模拟 Redis 预热：已就绪但控制面尚未确认
	gc.SetNames([]string{"gpt-5.6-preheated"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		gc.RefreshLoop(ctx, cp.URL(), 5*time.Millisecond, time.Hour)
	}()

	waitFor(t, 3*time.Second, func() bool { return cp.Requests() >= 3 }, "应持续快速重试")
	if !gc.IsInitialized() || !gc.IsGroupName("gpt-5.6-preheated") {
		t.Fatal("控制面刷新失败不得清空预热状态")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 RefreshLoop 应退出")
	}

	// 退出后不再产生新请求
	after := cp.Requests()
	time.Sleep(100 * time.Millisecond)
	if got := cp.Requests(); got != after {
		t.Fatalf("RefreshLoop 退出后不应继续刷新，请求数 %d → %d", after, got)
	}
}

// SetNames（Redis 预热路径）应过滤空串并标记就绪。
func TestSetNamesFiltersEmptyAndMarksInitialized(t *testing.T) {
	gc := NewGroupNameCache(testInternalToken)
	gc.SetNames([]string{"gpt-5.6-terra", "", "gpt-5.6-luna"})

	if !gc.IsInitialized() {
		t.Fatal("SetNames 后应处于 initialized 状态")
	}
	if !gc.IsGroupName("gpt-5.6-terra") || !gc.IsGroupName("gpt-5.6-luna") {
		t.Fatal("SetNames 应写入有效组名")
	}
	if gc.IsGroupName("") {
		t.Fatal("SetNames 应过滤空串")
	}
}

// 退避判据（循环级）：首次控制面成功前连续失败时，重试间隔必须逐次递增（R8），
// 而不是永远按 fast 间隔重试（永久性错误会变成每 fast 一条 Warn 的噪声源）。
//
// fast=50ms/steady=1h 时理论节奏：r1@0、r2@50ms、r3@150ms、r4@350ms（第 3 次失败后退避到 200ms）。
// 线性重试下 r4 在 200ms 就会出现，因此「观察到第 3 次请求后再等 100ms 仍无第 4 次」可判别。
func TestRefreshLoopBacksOffOnConsecutiveFailures(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()
	cp.SetMode(cpModeServerError)

	gc := NewGroupNameCache(testInternalToken)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		gc.RefreshLoop(ctx, cp.URL(), 50*time.Millisecond, time.Hour)
	}()

	waitFor(t, 3*time.Second, func() bool { return cp.Requests() >= 3 }, "应至少完成 3 次失败重试")
	time.Sleep(100 * time.Millisecond)
	if got := cp.Requests(); got != 3 {
		t.Fatalf("第 3 次失败后应退避到 200ms，100ms 内不应出现新请求，实际请求数 %d", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 RefreshLoop 应退出")
	}
}

// R2：provider 快照回流路径（Redis 快照里的启用组名）。
// 契约：非空 → 写入并标记就绪、来源=redis-snapshot；空列表（旧控制面不写该字段，
// 或全部组被停用）→ 保持现有缓存不动，绝不能用空列表覆盖。
func TestApplySnapshotGroupNamesFromProviderSnapshot(t *testing.T) {
	gc := NewGroupNameCache(testInternalToken)

	// ① 首次回流：非空 → 就绪、来源为 redis-snapshot
	if !gc.ApplySnapshotGroupNames([]string{"gpt-5.6-terra", "gpt-5.6-luna"}, 7) {
		t.Fatal("非空组名必须被写入缓存")
	}
	if !gc.IsInitialized() || !gc.IsGroupName("gpt-5.6-terra") || !gc.IsGroupName("gpt-5.6-luna") {
		t.Fatal("快照回流后缓存必须就绪且包含全部组名")
	}
	if got := gc.Len(); got != 2 {
		t.Fatalf("组名数量应为 2，实际 %d", got)
	}
	if got := gc.Source(); got != GroupNameCacheSourceRedisSnapshot {
		t.Fatalf("快照回流的来源应为 %q，实际 %q", GroupNameCacheSourceRedisSnapshot, got)
	}

	// ② 空列表（旧控制面 / 全部组停用）：保持现有缓存不动
	if gc.ApplySnapshotGroupNames(nil, 8) {
		t.Fatal("空组名列表必须被忽略（不得用空列表覆盖现有缓存）")
	}
	if gc.ApplySnapshotGroupNames([]string{}, 9) {
		t.Fatal("空组名列表必须被忽略（不得用空列表覆盖现有缓存）")
	}
	if !gc.IsInitialized() || gc.Len() != 2 || !gc.IsGroupName("gpt-5.6-terra") || !gc.IsGroupName("gpt-5.6-luna") {
		t.Fatal("空列表回流不得清空已就绪的组名")
	}

	// ③ 新快照替换组名（改名 / 停用后的最新事实）
	if !gc.ApplySnapshotGroupNames([]string{"gpt-5.6-nova"}, 10) {
		t.Fatal("非空组名必须被写入缓存")
	}
	if gc.IsGroupName("gpt-5.6-terra") || !gc.IsGroupName("gpt-5.6-nova") || gc.Len() != 1 {
		t.Fatal("回流必须整集合替换，旧组名不得残留")
	}

	// ④ nil 接收者 / 全空串列表必须安全
	var nilCache *GroupNameCache
	if nilCache.ApplySnapshotGroupNames([]string{"x"}, 1) {
		t.Fatal("nil 接收者必须直接返回 false")
	}
	if gc.ApplySnapshotGroupNames([]string{"", ""}, 11) {
		t.Fatal("全空串等价于空列表，必须被忽略")
	}
	if gc.Len() != 1 || !gc.IsGroupName("gpt-5.6-nova") {
		t.Fatal("空串列表不得清空已就绪的组名")
	}
}

// SetNames / Refresh 两条写入路径的来源契约：
// SetNames（预热/快照回流）= redis-snapshot；Refresh（控制面 HTTP 轮询）= control-plane。
func TestGroupNameCacheSourceAndLenContracts(t *testing.T) {
	cp := newFakeControlPlane()
	defer cp.Close()

	gc := NewGroupNameCache(testInternalToken)
	if gc.Source() != "" || gc.Len() != 0 {
		t.Fatal("未写入过任何组名时来源必须为空、数量必须为 0")
	}

	gc.SetNames([]string{"gpt-5.6-terra", "", "gpt-5.6-luna"})
	if got := gc.Source(); got != GroupNameCacheSourceRedisSnapshot {
		t.Fatalf("SetNames 的来源应为 %q，实际 %q", GroupNameCacheSourceRedisSnapshot, got)
	}
	if got := gc.Len(); got != 2 {
		t.Fatalf("SetNames 应过滤空串，数量应为 2，实际 %d", got)
	}

	if err := gc.Refresh(context.Background(), cp.URL()); err != nil {
		t.Fatalf("Refresh 应成功，实际报错: %v", err)
	}
	if got := gc.Source(); got != GroupNameCacheSourceControlPlane {
		t.Fatalf("Refresh 的来源应为 %q，实际 %q", GroupNameCacheSourceControlPlane, got)
	}
	if got := gc.Len(); got != 2 {
		t.Fatalf("Refresh 后数量应为 2，实际 %d", got)
	}
}

// 退避序列（纯函数判据，不依赖时钟）：生产默认 2s/30s 下的序列必须是
// 2s→4s→8s→16s→30s，并被 30s 封顶（R8）。
func TestNextFailureIntervalBackoffSequence(t *testing.T) {
	fast := DefaultGroupNameFastRefreshInterval
	steady := DefaultGroupNameSteadyRefreshInterval
	want := []time.Duration{
		2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		30 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	for i, expected := range want {
		if got := nextFailureInterval(fast, steady, i+1); got != expected {
			t.Fatalf("连续失败 %d 次后的重试间隔应为 %v，实际 %v", i+1, expected, got)
		}
	}

	// 非法/零值入参回落到默认间隔；failures<=0 视为首次失败
	if got := nextFailureInterval(0, 0, 1); got != fast {
		t.Fatalf("零值间隔应回落到默认 fast=%v，实际 %v", fast, got)
	}
	if got := nextFailureInterval(fast, steady, 0); got != fast {
		t.Fatalf("failures<=0 应视为首次失败（%v），实际 %v", fast, got)
	}
	// steady 小于 fast 时不放大：直接封顶在 steady
	if got := nextFailureInterval(time.Hour, time.Second, 3); got != time.Second {
		t.Fatalf("steady 小于 fast 时应封顶在 steady=1s，实际 %v", got)
	}
}
