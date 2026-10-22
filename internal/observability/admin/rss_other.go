//go:build !linux && !darwin

package admin

// readProcessRSS 其它平台没有稳定的 RSS 读取方式，返回不可用（对应指标会被跳过）。
func readProcessRSS() (uint64, string, bool) { return 0, "", false }

// containerMemory 其它平台不采集 cgroup 数据。
func containerMemory() (uint64, uint64, bool) { return 0, 0, false }
