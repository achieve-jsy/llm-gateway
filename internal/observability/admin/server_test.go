package admin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testAdminServer 构造一个用于测试的 admin server（随机端口的本机地址）。
func testAdminServer() *Server {
	return New(Config{Addr: "127.0.0.1:0", Enabled: true})
}

// promLineRE 校验 Prometheus 文本格式的单行采样。
var promLineRE = regexp.MustCompile("^[a-zA-Z_:][a-zA-Z0-9_:]*(\\{[^}]*\\})? -?(\\d+(\\.\\d+)?([eE][-+]?\\d+)?|NaN|\\+Inf|-Inf)$")

func TestAdminHandlerPprofEndpoints(t *testing.T) {
	srv := httptest.NewServer(testAdminServer().Handler())
	defer srv.Close()

	tests := []struct {
		path     string
		wantBody string
	}{
		{path: "/debug/pprof/", wantBody: "goroutine"},
		{path: "/debug/pprof/", wantBody: "heap"},
		{path: "/debug/pprof/heap", wantBody: ""},
		{path: "/debug/pprof/heap?debug=1", wantBody: "heap profile"},
		{path: "/debug/pprof/goroutine?debug=1", wantBody: "goroutine profile"},
		{path: "/debug/pprof/allocs?debug=1", wantBody: ""},
		{path: "/debug/pprof/block?debug=1", wantBody: ""},
		{path: "/debug/pprof/mutex?debug=1", wantBody: ""},
		{path: "/debug/pprof/threadcreate?debug=1", wantBody: ""},
		{path: "/debug/pprof/cmdline", wantBody: ""},
		{path: "/debug/pprof/symbol", wantBody: "num_symbols"},
		{path: "/healthz", wantBody: "ok"},
	}

	for _, tc := range tests {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s 失败: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s 状态码 = %d, 期望 200", tc.path, resp.StatusCode)
			continue
		}
		if tc.wantBody != "" && !strings.Contains(string(body), tc.wantBody) {
			t.Errorf("GET %s 响应体未包含 %q, 前 200 字节: %q", tc.path, tc.wantBody, truncate(string(body), 200))
		}
	}
}

func TestAdminHandlerMetrics(t *testing.T) {
	srv := httptest.NewServer(testAdminServer().Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics 失败: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics 状态码 = %d, 期望 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, 期望 text/plain 开头", ct)
	}

	text := string(body)
	values := validatePrometheusExposition(t, text)

	// 任务要求的必备指标：堆 alloc/inuse、GC 次数、goroutine、RSS、启动时长
	for _, name := range []string{
		"go_goroutines",
		"go_memstats_alloc_bytes",
		"go_memstats_heap_alloc_bytes",
		"go_memstats_heap_inuse_bytes",
		"go_memstats_sys_bytes",
		"go_gc_cycles_total",
		"go_memstats_gc_pause_total_seconds",
		"go_runtime_metric",
	} {
		if _, ok := values[name]; !ok {
			t.Errorf("缺少必备指标 %q", name)
		}
	}

	if got, err := strconv.ParseFloat(values["go_goroutines"], 64); err != nil || got < 1 {
		t.Errorf("go_goroutines = %q, 期望 >= 1（err=%v）", values["go_goroutines"], err)
	}
	if got, err := strconv.ParseFloat(values["process_uptime_seconds"], 64); err != nil || got <= 0 {
		t.Errorf("process_uptime_seconds = %q, 期望 > 0（err=%v）", values["process_uptime_seconds"], err)
	}
	if _, ok := values["process_start_time_seconds"]; !ok {
		t.Error("缺少 process_start_time_seconds")
	}

	// GC 周期数必须能从 runtime/metrics 里读到
	if !strings.Contains(text, "go_runtime_metric{name=\"/gc/cycles/total:gc-cycles\"}") {
		t.Error("缺少 runtime/metrics 的 /gc/cycles/total:gc-cycles 采样")
	}
	if !strings.Contains(text, "go_runtime_metric{name=\"/gc/gomemlimit:bytes\"}") {
		t.Error("缺少 runtime/metrics 的 /gc/gomemlimit:bytes 采样（OOM 排查关键）")
	}

	// RSS：当前平台可读时必须输出且大于 0
	if rss, _, ok := readProcessRSS(); ok {
		if rss == 0 {
			t.Error("readProcessRSS 返回 0")
		}
		if _, exists := values["process_resident_memory_bytes"]; !exists {
			t.Error("平台支持 RSS 时缺少 process_resident_memory_bytes")
		}
	}
}

func TestAdminHandlerMetricsStartDuration(t *testing.T) {
	// 模拟已启动：写入门监听完成时间
	adminListenDoneAt.Store(time.Now().UnixNano())
	defer adminListenDoneAt.Store(0)

	s := testAdminServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1"
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "admin_start_duration_seconds") {
		t.Error("已启动时应输出 admin_start_duration_seconds")
	}
}

func TestAdminHandlerExpvarDebugVars(t *testing.T) {
	srv := httptest.NewServer(testAdminServer().Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/vars")
	if err != nil {
		t.Fatalf("GET /debug/vars 失败: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("/debug/vars 不是合法 JSON: %v", err)
	}
	for _, key := range []string{"cmdline", "memstats", adminExpvarName} {
		if _, ok := payload[key]; !ok {
			t.Errorf("/debug/vars 缺少 %q", key)
		}
	}
}

// TestAdminHandlerRejectsNonLoopbackClient 验证第二道防线：
// 即使监听地址被误配成 0.0.0.0，非回环来源也会被 403 拒绝。
func TestAdminHandlerRejectsNonLoopbackClient(t *testing.T) {
	h := testAdminServer().Handler()

	tests := []struct {
		remote string
		want   int
	}{
		{remote: "127.0.0.1:54321", want: http.StatusOK},
		{remote: "127.9.9.9:54321", want: http.StatusOK},
		{remote: "[::1]:54321", want: http.StatusOK},
		{remote: "192.0.2.10:54321", want: http.StatusForbidden},
		{remote: "10.0.0.1:54321", want: http.StatusForbidden},
		{remote: "172.17.0.1:54321", want: http.StatusForbidden},
		{remote: "", want: http.StatusForbidden},
		{remote: "not-an-address", want: http.StatusForbidden},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = tc.remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Errorf("RemoteAddr=%q 状态码 = %d, 期望 %d", tc.remote, rec.Code, tc.want)
		}
	}
}

func TestServerStartShutdownAndGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	s := testAdminServer()
	errCh := make(chan error, 1)
	go func() { errCh <- s.Start() }()

	addr := waitForListenAddr(t, s)
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("实际监听地址 = %q, 期望绑定 127.0.0.1", addr)
	}

	// 重复 Start 必须报错而不是再起一个监听
	if err := s.Start(); err == nil {
		t.Error("重复 Start 应返回错误")
	}

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	defer client.CloseIdleConnections()

	for _, path := range []string{"/debug/pprof/", "/metrics"} {
		resp, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET http://%s%s 失败: %v", addr, path, err)
		}
		code := resp.StatusCode
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if code != http.StatusOK {
			t.Fatalf("GET http://%s%s 状态码 = %d, 期望 200", addr, path, code)
		}
		if path == "/debug/pprof/" && !strings.Contains(string(body), "heap") {
			t.Errorf("pprof 首页未列出 heap profile: %q", truncate(string(body), 200))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Shutdown 后 Start 应返回 nil, 实际: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown 后 Start 未返回（goroutine 泄露）")
	}

	// 关闭后端口不可再访问
	if resp, err := client.Get("http://" + addr + "/metrics"); err == nil {
		resp.Body.Close()
		t.Fatalf("Shutdown 后 http://%s/metrics 仍可访问（状态码 %d）", addr, resp.StatusCode)
	}

	// Shutdown 幂等
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("重复 Shutdown 应返回 nil, 实际: %v", err)
	}
	// 关闭后不允许再次启动
	if err := s.Start(); err == nil {
		t.Error("Shutdown 之后再次 Start 应返回错误")
	}

	// goroutine 数量应回落到基线附近（允许少量运行时/测试框架抖动）
	deadline := time.Now().Add(3 * time.Second)
	for {
		if now := runtime.NumGoroutine(); now <= before+5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine 未回落: before=%d, now=%d", before, runtime.NumGoroutine())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestServerDisabledDoesNotListen(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0", Enabled: false})
	if s.Enabled() {
		t.Fatal("Enabled() 应为 false")
	}

	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("禁用时 Start 应返回 nil, 实际: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("禁用时 Start 不应阻塞")
	}

	if got := s.Addr(); got != "127.0.0.1:0" {
		t.Errorf("禁用时不应监听, Addr = %q", got)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("禁用时 Shutdown 应返回 nil, 实际: %v", err)
	}
}

// TestServerStartListenFailureIsReturned 端口被占用时必须返回错误而不是 panic。
func TestServerStartListenFailureIsReturned(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("准备占用端口失败: %v", err)
	}
	defer occupied.Close()

	s := New(Config{Addr: occupied.Addr().String(), Enabled: true})
	if err := s.Start(); err == nil {
		t.Fatal("端口被占用时 Start 应返回错误")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("监听失败后 Shutdown 应返回 nil, 实际: %v", err)
	}
}

// validatePrometheusExposition 逐行校验 Prometheus 文本格式，返回「指标名 -> 最后一个采样值」映射。
func validatePrometheusExposition(t *testing.T, body string) map[string]string {
	t.Helper()

	declared := map[string]bool{}
	values := map[string]string{}
	lineNo := 0
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		lineNo++
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			fields := strings.SplitN(line, " ", 4)
			if len(fields) < 3 {
				t.Errorf("第 %d 行注释缺字段: %q", lineNo, line)
				continue
			}
			switch fields[1] {
			case "HELP":
				if len(fields) < 4 {
					t.Errorf("第 %d 行 HELP 缺少说明文案: %q", lineNo, line)
				}
			case "TYPE":
				if len(fields) < 4 {
					t.Errorf("第 %d 行 TYPE 缺少类型: %q", lineNo, line)
					continue
				}
				declared[fields[2]] = true
			default:
				t.Errorf("第 %d 行出现未支持的注释类型: %q", lineNo, line)
			}
			continue
		}
		if !promLineRE.MatchString(line) {
			t.Errorf("第 %d 行不是合法 Prometheus 采样: %q", lineNo, line)
			continue
		}
		name := line[:strings.IndexAny(line, " {")]
		if !declared[name] {
			t.Errorf("第 %d 行指标 %q 缺少前置的 # TYPE 声明", lineNo, name)
		}
		values[name] = line[strings.LastIndex(line, " ")+1:]
	}
	return values
}

// waitForListenAddr 轮询等待 Start 完成监听并返回真实地址。
func waitForListenAddr(t *testing.T, s *Server) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if addr := s.Addr(); !strings.HasSuffix(addr, ":0") {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 admin server 监听超时, Addr = %q", s.Addr())
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
