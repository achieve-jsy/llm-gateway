package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/example/llm-gateway/internal/controlplane/infra"
	"github.com/example/llm-gateway/internal/controlplane/router"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/observability/admin"
)

func main() {
	lifecycleCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 加载 .env 文件（开发环境，文件不存在时不报错）
	_ = godotenv.Load()

	// 初始化 PostgreSQL
	db, err := infra.InitPostgres()
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL: %v", err)
	}

	// 初始化 Redis
	rdb, err := infra.InitRedis()
	if err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}

	// 启动前同步重建配额快照，并在运行期间定期修复 Redis 丢失或陈旧数据。
	reconcilerCtx, stopReconciler := context.WithCancel(context.Background())
	quotaSvc := service.NewQuotaService(db, rdb)
	reconcilerDone := quotaSvc.StartSnapshotReconciler(reconcilerCtx, 30*time.Second)
	defer func() {
		stopReconciler()
		<-reconcilerDone
	}()

	// 初始化路由（注入依赖）
	r := router.Setup(lifecycleCtx, db, rdb)

	// 启动服务
	port := os.Getenv("CONTROL_PLANE_PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("control-plane starting on :%s", port)
	server := &http.Server{Addr: ":" + port, Handler: r}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	// 本机观测端点（pprof + /metrics + expvar）：独立监听 127.0.0.1:9091，
	// 刻意不注册进对外暴露的 8081 路由（nginx 会把 8081 暴露到外网）。
	// 见 2026-10-16 OOM 复盘：control-plane 当时既无 pprof 也无 /metrics。
	adminSrv := admin.New(admin.LoadConfig())
	var adminErrCh <-chan error
	if adminSrv.Enabled() {
		ch := make(chan error, 1)
		adminErrCh = ch
		go func() { ch <- adminSrv.Start() }()
	} else {
		log.Printf("control-plane admin endpoints disabled by %s", admin.EnvEnabled)
	}

	select {
	case err := <-serverErr:
		stop()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control-plane server stopped unexpectedly: %v", err)
		}
	case err := <-adminErrCh:
		// admin 端点只是辅助观测能力：启动/运行失败（例如端口被占用）不应拖垮管理面主服务
		log.Printf("control-plane admin server stopped unexpectedly: %v", err)
		<-lifecycleCtx.Done()
		shutdownControlPlane(server, adminSrv)
	case <-lifecycleCtx.Done():
		shutdownControlPlane(server, adminSrv)
	}
}

// shutdownControlPlane 优雅关闭主服务与本机观测端点（共享同一个 10s 超时预算）。
func shutdownControlPlane(server *http.Server, adminSrv *admin.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := adminSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("control-plane admin server graceful shutdown failed: %v", err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("control-plane graceful shutdown failed: %v", err)
	}
}
