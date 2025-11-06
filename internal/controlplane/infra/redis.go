package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

// InitRedis 初始化 Redis 连接
func InitRedis() (*redis.Client, error) {
	addr := getEnvOrDefault("REDIS_ADDR", "localhost:6379")
	password := os.Getenv("REDIS_PASSWORD")
	dbStr := getEnvOrDefault("REDIS_DB", "0")

	db := 0
	fmt.Sscanf(dbStr, "%d", &db)

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		PoolSize: 20,
	})

	// 验证连接
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return rdb, nil
}
