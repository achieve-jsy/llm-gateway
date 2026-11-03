// Package admin 提供 control-plane 的「仅本机可访问」的观测端点（pprof + metrics）。
//
// 背景：2026-10-16 的 OOM 事故只能靠宿主机 dmesg + docker logs 定位，control-plane
// 既没有 pprof 也没有 /metrics，缺少内存/goroutine 的可观测性。本包补齐这些能力，
// 并遵守以下铁律：
//
//  1. 独立监听地址（默认 127.0.0.1:9091），绝不注册进对外暴露的 8081 gin 路由
//     （nginx 会把 8081 暴露到外网）；
//  2. 配置缺失或非法时回退到安全默认值，任何情况下都不 panic；
//  3. 非回环地址必须显式 opt-in（CONTROL_PLANE_ADMIN_ALLOW_NON_LOOPBACK=true），
//     否则强制收敛到回环地址；
//  4. 请求侧再叠加一道回环校验（RemoteAddr），即使被误绑到 0.0.0.0 也不会对外泄露。
//
// 依赖取舍：仓库未引入 github.com/prometheus/client_golang（go.mod / go.sum / vendor
// 均无该模块），且本仓库使用 vendor 模式，新增依赖需要重新 vendor。因此这里只用标准库
// runtime.ReadMemStats + runtime/metrics + expvar，手工输出 Prometheus 文本格式。
package admin

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	// EnvAddr 观测端点监听地址，默认 127.0.0.1:9091。
	EnvAddr = "CONTROL_PLANE_ADMIN_ADDR"
	// EnvEnabled 观测端点开关；默认开启（因为默认只绑本机，开启是安全的）。
	EnvEnabled = "CONTROL_PLANE_ADMIN_ENABLED"
	// EnvAllowNonLoopback 允许绑定非回环地址的显式开关，默认关闭。
	EnvAllowNonLoopback = "CONTROL_PLANE_ADMIN_ALLOW_NON_LOOPBACK"

	// DefaultAddr 安全默认监听地址：仅本机、独立端口，不占用对外的 8081。
	DefaultAddr = "127.0.0.1:9091"
)

// Config 观测端点配置。
type Config struct {
	// Addr 采样后的实际监听地址（保证带 host，且默认为回环地址）。
	Addr string
	// Enabled 是否启用观测端点。
	Enabled bool
	// AllowNonLoopback 用户是否显式允许非回环绑定。
	AllowNonLoopback bool
	// Adjustment 非空表示用户配置被安全收敛（或回退），内容为原因说明；
	// 由调用方在启动时记录告警，避免本函数产生日志副作用。
	Adjustment string
}

// LoadConfig 从环境变量加载配置，缺失/非法时回退到安全默认，且不 panic。
func LoadConfig() Config {
	return loadConfigFrom(os.Getenv)
}

// loadConfigFrom 便于测试注入环境变量读取函数。
func loadConfigFrom(getenv func(string) string) Config {
	cfg := Config{Addr: DefaultAddr, Enabled: true}
	cfg.AllowNonLoopback = isTruthy(getenv(EnvAllowNonLoopback))

	if raw := strings.TrimSpace(getenv(EnvAddr)); raw != "" {
		addr, reason := normalizeAddr(raw, cfg.AllowNonLoopback)
		cfg.Addr = addr
		cfg.Adjustment = reason
	}
	if v := strings.TrimSpace(getenv(EnvEnabled)); v != "" && isFalsey(v) {
		cfg.Enabled = false
	}
	return cfg
}

// normalizeAddr 把用户配置收敛成安全的监听地址。
// 返回收敛后的地址；若与请求不一致，reason 说明原因（空字符串表示按原样使用）。
func normalizeAddr(raw string, allowNonLoopback bool) (addr string, reason string) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		// 兼容只写端口的简写形式（"9091" / ":9091"）。
		p, perr := strconv.Atoi(strings.TrimPrefix(raw, ":"))
		if perr != nil || p < 0 || p > 65535 {
			return DefaultAddr, fmt.Sprintf("%s=%q 无法解析，已回退安全默认 %s", EnvAddr, raw, DefaultAddr)
		}
		host, port = "", strconv.Itoa(p)
	} else if p, perr := strconv.Atoi(port); perr != nil || p < 0 || p > 65535 {
		return DefaultAddr, fmt.Sprintf("%s=%q 的端口非法，已回退安全默认 %s", EnvAddr, raw, DefaultAddr)
	}

	if !isLoopbackHost(host) && !allowNonLoopback {
		safe := net.JoinHostPort("127.0.0.1", port)
		return safe, fmt.Sprintf(
			"%s=%q 会绑定非回环地址（pprof/metrics 将被外网访问），已收敛到 %s；确需非回环绑定请显式设置 %s=true",
			EnvAddr, raw, safe, EnvAllowNonLoopback)
	}
	// 回环地址，或已显式 opt-in 的非回环地址：按用户配置使用。
	return net.JoinHostPort(host, port), ""
}

// isLoopbackHost 判断监听地址的 host 是否是本机回环（含 localhost 与 127.0.0.0/8、::1）。
// 空 host（如 ":9091"、":0"）表示监听全部网卡，不算回环。
// 无法解析的主机名按非回环处理（保守策略，需要显式 opt-in）。
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if h == "" {
		return false
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// isTruthy 识别常见的「真」值（大小写不敏感）；其余取值一律视为假。
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// isFalsey 识别常见的「假」值（大小写不敏感）；未知取值不会关闭开关。
func isFalsey(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}
