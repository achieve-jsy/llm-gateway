package observability

import "time"

// CollectorConfig Collector 配置
type CollectorConfig struct {
	Enabled                  bool          // 埋点总开关，默认 true
	BufferSize               int           // channel 缓冲容量，默认 10000
	FlushN                   int           // 批量写条数阈值，默认 200
	FlushInterval            time.Duration // 批量写时间阈值，默认 1s
	EnqueueTimeout           time.Duration // 队列满时有界等待，默认 25ms
	SyncFallbackTimeout      time.Duration // 同步兜底写超时，默认 2s
	DBRetryCount             int           // DB 批量写失败后的同步重试次数
	DBRetryBackoff           time.Duration // DB 重试初始退避
	SpoolReplayInterval      time.Duration // Redis Stream 后台回放间隔
	DBConnectRetryInterval   time.Duration // PostgreSQL 启动/断线重连间隔
	PriceRefreshInterval     time.Duration // 数据库价格表全量刷新间隔
	SpoolCapacity            int64         // Redis Stream 规划容量（不使用 MAXLEN 丢数据）
	SpoolHighWatermark       int64         // 拒绝新可计费请求的高水位
	SpoolHealthCheckInterval time.Duration // Stream 容量健康检查间隔
	SpoolMaxRetries          int           // 单条 poison 隔离前的最大回放次数
	SpoolClaimMinIdle        time.Duration // 多实例认领 pending 消息的最小空闲时间
	BillableReservationTTL   time.Duration // 可计费请求 inflight reservation 过期时间
}

// DefaultCollectorConfig 返回默认配置
func DefaultCollectorConfig() CollectorConfig {
	return CollectorConfig{
		Enabled:                  true,
		BufferSize:               10000,
		FlushN:                   200,
		FlushInterval:            time.Second,
		EnqueueTimeout:           25 * time.Millisecond,
		SyncFallbackTimeout:      2 * time.Second,
		DBRetryCount:             3,
		DBRetryBackoff:           50 * time.Millisecond,
		SpoolReplayInterval:      time.Second,
		DBConnectRetryInterval:   5 * time.Second,
		PriceRefreshInterval:     5 * time.Minute,
		SpoolCapacity:            100_000,
		SpoolHighWatermark:       80_000,
		SpoolHealthCheckInterval: time.Second,
		SpoolMaxRetries:          3,
		SpoolClaimMinIdle:        30 * time.Second,
		BillableReservationTTL:   10 * time.Minute,
	}
}
