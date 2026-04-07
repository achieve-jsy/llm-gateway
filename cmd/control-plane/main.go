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
	select {
	case err := <-serverErr:
		stop()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control-plane server stopped unexpectedly: %v", err)
		}
	case <-lifecycleCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("control-plane graceful shutdown failed: %v", err)
		}
	}
}
