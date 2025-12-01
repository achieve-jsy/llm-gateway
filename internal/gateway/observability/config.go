package observability

import "time"

// CollectorConfig Collector 配置
type CollectorConfig struct {
	Enabled       bool          // 埋点总开关，默认 true
	BufferSize    int           // channel 缓冲容量，默认 10000
	FlushN        int           // 批量写条数阈值，默认 200
	FlushInterval time.Duration // 批量写时间阈值，默认 1s
}

// DefaultCollectorConfig 返回默认配置
func DefaultCollectorConfig() CollectorConfig {
	return CollectorConfig{
		Enabled:       true,
		BufferSize:    10000,
		FlushN:        200,
		FlushInterval: time.Second,
	}
}