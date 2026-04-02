package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/example/llm-gateway/internal/controlplane/infra"
	"github.com/example/llm-gateway/internal/controlplane/router"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func main() {
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
	r := router.Setup(db, rdb)

	// 启动服务
	port := os.Getenv("CONTROL_PLANE_PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("control-plane starting on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
