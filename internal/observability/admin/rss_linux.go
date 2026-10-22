//go:build linux

package admin

import (
	"os"
	"strconv"
	"strings"
)

// readProcessRSS 返回当前进程常驻内存（字节）与数据来源。
// Linux 下取 /proc/self/statm 的第 2 列（resident pages）× 页大小。
func readProcessRSS() (uint64, string, bool) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, "", false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return pages * uint64(os.Getpagesize()), "statm", true
}

// cgroup 内存文件路径：优先 cgroup v2，其次 v1。
var (
	cgroupV2MemoryCurrent = "/sys/fs/cgroup/memory.current"
	cgroupV2MemoryMax     = "/sys/fs/cgroup/memory.max"
	cgroupV1MemoryCurrent = "/sys/fs/cgroup/memory/memory.usage_in_bytes"
	cgroupV1MemoryMax     = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	// dockerEnvFile 用于判断进程是否运行在容器内；
	// 只有容器内才能把 cgroup 数据当作「本进程所属容器」的用量（宿主机上读到的是全机根 cgroup）。
	dockerEnvFile = "/.dockerenv"
)

// containerMemory 读取容器 cgroup 的内存用量与上限（字节）。
// 非容器环境返回 ok=false（指标不会输出），避免把宿主机全机内存误标成容器内存。
func containerMemory() (usage uint64, limit uint64, ok bool) {
	if _, err := os.Stat(dockerEnvFile); err != nil {
		return 0, 0, false
	}

	usage, ok = readUintFile(cgroupV2MemoryCurrent)
	if !ok {
		usage, ok = readUintFile(cgroupV1MemoryCurrent)
	}
	if !ok {
		return 0, 0, false
	}

	if v, okMax := readUintFile(cgroupV2MemoryMax); okMax {
		limit = v
	} else if v, okMax := readUintFile(cgroupV1MemoryMax); okMax && v < 1<<60 {
		// cgroup v1 用接近 uint64 上限的极大值表示「无限制」
		limit = v
	}
	return usage, limit, true
}

// readUintFile 读取一个十进制无符号整数文件；"max"（cgroup v2 无限制）与读取失败都返回 false。
func readUintFile(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(data))
	if s == "" || s == "max" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
