package admin

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"
)

// Server control-plane 的 admin/观测端点服务。
// 使用独立的 http.Server + ServeMux，与对外暴露的 8081 gin 路由完全隔离。
type Server struct {
	cfg Config
	log *slog.Logger

	handler http.Handler
	httpSrv *http.Server

	mu      sync.Mutex
	ln      net.Listener
	started bool
	stopped bool
}

// New 创建 admin server（此时不监听，监听动作在 Start 里做）。
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, log: slog.Default().With("component", "control-plane-admin")}
	s.handler = s.buildHandler()
	s.httpSrv = &http.Server{
		Addr:    cfg.Addr,
		Handler: s.handler,
		// 只做读取侧防护；不设 WriteTimeout：/debug/pprof/profile 与 /debug/pprof/trace
		// 需要在一次响应里持续输出数十秒，写超时会打断抓取。
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	return s
}

// Enabled 报告观测端点是否启用。
func (s *Server) Enabled() bool { return s.cfg.Enabled }

// Handler 返回观测端点的 http.Handler（供测试直接挂 httptest 使用）。
func (s *Server) Handler() http.Handler { return s.handler }

// Addr 返回实际监听地址；未启动时返回配置值（例如 "127.0.0.1:0"）。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.cfg.Addr
}

// Start 监听并阻塞服务，直到 Shutdown（返回 nil）或服务异常退出（返回 err）。
// 配置禁用时直接返回 nil，不监听任何端口。
func (s *Server) Start() error {
	if !s.cfg.Enabled {
		s.log.Info("control-plane admin 观测端点已禁用", "env", EnvEnabled)
		return nil
	}
	if s.cfg.Adjustment != "" {
		s.log.Warn("control-plane admin 观测端点配置已安全收敛", "detail", s.cfg.Adjustment)
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errors.New("admin server 已关闭，无法再次启动")
	}
	if s.started {
		s.mu.Unlock()
		return errors.New("admin server 已启动")
	}
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("admin 观测端点监听 %s 失败: %w", s.cfg.Addr, err)
	}
	s.ln = ln
	s.started = true
	s.mu.Unlock()

	adminListenDoneAt.Store(time.Now().UnixNano())
	s.log.Info("control-plane admin 观测端点已启动",
		"addr", ln.Addr().String(), "pprof", "/debug/pprof/", "metrics", "/metrics")

	if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("admin 观测端点异常退出: %w", err)
	}
	return nil
}

// Shutdown 优雅关闭观测端点；幂等，未启动时为 no-op，不会 panic。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	alreadyStopped := s.stopped
	started := s.started
	s.stopped = true
	s.mu.Unlock()

	if alreadyStopped || !started {
		return nil
	}
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("admin 观测端点优雅关闭失败: %w", err)
	}
	return nil
}

// buildHandler 组装观测端点路由。
// 所有 handler 都显式注册在自己的 mux 上（不使用 import _ "net/http/pprof"），
// 避免 pprof 被挂到 http.DefaultServeMux 上后从其它服务外泄。
func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// pprof（含任务要求的 /debug/pprof/heap 与 /debug/pprof/goroutine）
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	// metrics：Prometheus 文本格式（标准库实现，未引入 client_golang）
	mux.HandleFunc("/metrics", s.handleMetrics)
	// expvar：JSON 指标（含 memstats / cmdline / admin 概览）
	mux.Handle("/debug/vars", expvar.Handler())
	// 存活探测：便于容器 healthcheck 与端口自检
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})

	return loopbackOnly(s.log, mux)
}

// loopbackOnly 只放行来自回环地址的请求。
// 这是绑定地址之外的第二道防线：即使被误配成 0.0.0.0（或经 NAT/端口转发抵达），
// 也能保证指标与 pprof 不被非本机来源读到。
func loopbackOnly(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			log.Warn("admin 观测端点拒绝来源不明的请求", "remote_addr", r.RemoteAddr, "path", r.URL.Path)
			http.Error(w, "forbidden: admin endpoints are loopback-only", http.StatusForbidden)
			return
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			log.Warn("admin 观测端点拒绝非本机访问", "remote_addr", r.RemoteAddr, "path", r.URL.Path)
			http.Error(w, "forbidden: admin endpoints are loopback-only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
