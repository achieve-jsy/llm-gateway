package keyactivity

// KeyActivityConfig 类型化配置项：扫描、EWMA、唤醒判定、冷启动
// 由 service.GetKeyActivityConfig() 填充并注入各组件。
type KeyActivityConfig struct {
	Enabled         bool
	ScanIntervalSec int
	IPAsSource      bool
	DormantDays     int
	Alpha           float64
	K               float64
	MinBaseline     float64
	MinDev          float64
	BootstrapDays   int
}
