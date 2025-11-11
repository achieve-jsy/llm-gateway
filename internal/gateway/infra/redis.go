package infra

import (
	"context"
	"fmt"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/redis/go-redis/v9"
)

// InitRedis 初始化 Redis 连接（T007）
func InitRedis(cfg config.RedisConfig) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: 30,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return rdb, nil
}
