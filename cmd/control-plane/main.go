package main

import (
	"log"
	"os"

	"github.com/example/llm-gateway/internal/controlplane/infra"
	"github.com/example/llm-gateway/internal/controlplane/router"
)

func main() {
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
