//go:build darwin

package admin

import "syscall"

// readProcessRSS Darwin 下用 getrusage 取「最大常驻集」（Maxrss，字节）。
// 注意：Maxrss 是峰值而非瞬时值，只能作为当前 RSS 的上界近似（Darwin 无 /proc）。
// 生产部署在 Linux 容器里，取到的是 /proc/self/statm 的即时值。
func readProcessRSS() (uint64, string, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, "", false
	}
	if ru.Maxrss <= 0 {
		return 0, "", false
	}
	return uint64(ru.Maxrss), "maxrss", true
}

// containerMemory Darwin 无 cgroup 数据。
func containerMemory() (uint64, uint64, bool) { return 0, 0, false }
