package service

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// SystemMetrics 系统资源指标快照
type SystemMetrics struct {
	CPUUsagePercent      float64 `json:"cpu_usage_percent"`
	MemoryUsedMB         float64 `json:"memory_used_mb"`
	MemoryTotalMB        float64 `json:"memory_total_mb"`
	MemoryUsagePercent   float64 `json:"memory_usage_percent"`
	GoroutineCount       int     `json:"goroutine_count"`
	DBActiveCount        int     `json:"db_active_count"`
	DBIdleCount          int     `json:"db_idle_count"`
	DBWaitingCount       int64   `json:"db_waiting_count"`
	DBMaxOpenConns       int     `json:"db_max_open_conns"`
	RedisTotalConns      int     `json:"redis_total_conns"`
	RedisIdleConns       int     `json:"redis_idle_conns"`
	RedisPoolSize        int     `json:"redis_pool_size"`
	ConcurrencyQueueDepth int    `json:"concurrency_queue_depth"`
	DBOk                 bool    `json:"db_ok"`
	RedisOk              bool    `json:"redis_ok"`
	CollectedAt          time.Time `json:"collected_at"`
}

// SystemMetricsCollector 定期采集系统资源指标
type SystemMetricsCollector struct {
	db  *gorm.DB
	rdb *redis.Client

	mu      sync.RWMutex
	current SystemMetrics
	history []SystemMetrics // 最近 10 个快照（5 分钟历史，30s 间隔）
}

// NewSystemMetricsCollector 创建采集器
func NewSystemMetricsCollector(db *gorm.DB, rdb *redis.Client) *SystemMetricsCollector {
	return &SystemMetricsCollector{
		db:      db,
		rdb:     rdb,
		history: make([]SystemMetrics, 0, 10),
	}
}

// Start 启动后台采集 goroutine
func (c *SystemMetricsCollector) Start(ctx context.Context) {
	// 立即采集一次
	c.collect()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.collect()
			}
		}
	}()
}

// Get 获取最新的系统指标快照（线程安全）
func (c *SystemMetricsCollector) Get() SystemMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// GetHistory 获取最近的历史快照
func (c *SystemMetricsCollector) GetHistory() []SystemMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]SystemMetrics, len(c.history))
	copy(result, c.history)
	return result
}

func (c *SystemMetricsCollector) collect() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	allocMB := float64(memStats.Alloc) / 1024 / 1024
	sysMB := float64(memStats.Sys) / 1024 / 1024
	memPct := 0.0
	if sysMB > 0 {
		memPct = allocMB / sysMB * 100
	}

	// CPU 近似值：Goroutine 数 / GOMAXPROCS，上限 100%
	goroutines := runtime.NumGoroutine()
	maxProcs := runtime.GOMAXPROCS(0)
	cpuPct := float64(goroutines) / float64(maxProcs) * 10
	if cpuPct > 100 {
		cpuPct = 100
	}

	// DB 连接池
	sqlDB, err := c.db.DB()
	var dbActive, dbIdle int
	var dbWaiting int64
	var dbMaxOpen int
	dbOk := false
	if err == nil {
		stats := sqlDB.Stats()
		dbActive = stats.InUse
		dbIdle = stats.Idle
		dbWaiting = stats.WaitCount
		dbMaxOpen = stats.MaxOpenConnections
		// 健康检查：SELECT 1
		dbOk = sqlDB.PingContext(context.Background()) == nil
	}

	// Redis 连接池 + 健康检查
	var redisTotal, redisIdle, redisPool int
	redisOk := false
	if c.rdb != nil {
		poolStats := c.rdb.PoolStats()
		redisTotal = int(poolStats.TotalConns)
		redisIdle = int(poolStats.IdleConns)
		// go-redis PoolStats 没有 MaxConns 字段，使用 0 表示未知
		redisPool = 0
		redisOk = c.rdb.Ping(context.Background()).Err() == nil
	}

	// 并发队列深度：统计所有 provider 当前并发总数
	queueDepth := 0
	if c.rdb != nil {
		iter := c.rdb.Scan(context.Background(), 0, "concurrency:provider:*", 100).Iterator()
		for iter.Next(context.Background()) {
			key := iter.Val()
			val, _ := c.rdb.Get(context.Background(), key).Int()
			queueDepth += val
		}
	}

	snapshot := SystemMetrics{
		CPUUsagePercent:       cpuPct,
		MemoryUsedMB:          allocMB,
		MemoryTotalMB:         sysMB,
		MemoryUsagePercent:    memPct,
		GoroutineCount:        goroutines,
		DBActiveCount:         dbActive,
		DBIdleCount:           dbIdle,
		DBWaitingCount:        dbWaiting,
		DBMaxOpenConns:        dbMaxOpen,
		RedisTotalConns:       redisTotal,
		RedisIdleConns:        redisIdle,
		RedisPoolSize:         redisPool,
		ConcurrencyQueueDepth: queueDepth,
		DBOk:                  dbOk,
		RedisOk:               redisOk,
		CollectedAt:           time.Now(),
	}

	c.mu.Lock()
	c.current = snapshot
	// 维护历史队列（保留最近 10 条）
	c.history = append(c.history, snapshot)
	if len(c.history) > 10 {
		c.history = c.history[1:]
	}
	c.mu.Unlock()

	slog.Debug("system metrics collected",
		"cpu_pct", cpuPct,
		"mem_mb", allocMB,
		"goroutines", goroutines,
		"db_active", dbActive,
		"redis_total", redisTotal,
		"queue_depth", queueDepth,
		"db_ok", dbOk,
		"redis_ok", redisOk,
	)
}
