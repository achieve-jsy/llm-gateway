package admin

import (
	"strings"
	"testing"
)

// envFunc 用 map 伪造环境变量读取函数，便于表驱动测试配置收敛逻辑。
func envFunc(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestLoadConfigDefaultsToLoopback(t *testing.T) {
	cfg := loadConfigFrom(envFunc(nil))

	if !cfg.Enabled {
		t.Error("配置缺失时观测端点应默认开启")
	}
	if cfg.Addr != DefaultAddr {
		t.Errorf("默认监听地址 = %q, 期望 %q", cfg.Addr, DefaultAddr)
	}
	if !isLoopbackHost(strings.Split(DefaultAddr, ":")[0]) {
		t.Errorf("默认监听地址 %q 必须是回环地址", DefaultAddr)
	}
	if cfg.AllowNonLoopback {
		t.Error("默认不应允许非回环绑定")
	}
	if cfg.Adjustment != "" {
		t.Errorf("默认配置不应有收敛告警，实际: %q", cfg.Adjustment)
	}
}

func TestLoadConfigNormalizesAddr(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantAddr     string
		wantEnabled  bool
		wantAllow    bool
		wantAdjusted bool
	}{
		{
			name:     "显式回环地址原样使用",
			env:      map[string]string{EnvAddr: "127.0.0.1:19091"},
			wantAddr: "127.0.0.1:19091", wantEnabled: true,
		},
		{
			name:     "地址两端空白被裁剪",
			env:      map[string]string{EnvAddr: "  127.0.0.1:9092  "},
			wantAddr: "127.0.0.1:9092", wantEnabled: true,
		},
		{
			name:     "只写端口的简写形式收敛到回环",
			env:      map[string]string{EnvAddr: ":19091"},
			wantAddr: "127.0.0.1:19091", wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "纯端口数字收敛到回环",
			env:      map[string]string{EnvAddr: "19091"},
			wantAddr: "127.0.0.1:19091", wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "0.0.0.0 被收敛到回环",
			env:      map[string]string{EnvAddr: "0.0.0.0:9091"},
			wantAddr: "127.0.0.1:9091", wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "显式 opt-in 后保留非回环绑定",
			env:      map[string]string{EnvAddr: "0.0.0.0:9091", EnvAllowNonLoopback: "true"},
			wantAddr: "0.0.0.0:9091", wantEnabled: true, wantAllow: true,
		},
		{
			name:     "IPv6 回环保留",
			env:      map[string]string{EnvAddr: "[::1]:9091"},
			wantAddr: "[::1]:9091", wantEnabled: true,
		},
		{
			name:     "localhost 视为回环",
			env:      map[string]string{EnvAddr: "localhost:9091"},
			wantAddr: "localhost:9091", wantEnabled: true,
		},
		{
			name:     "无法解析的地址回退安全默认",
			env:      map[string]string{EnvAddr: "not-an-address"},
			wantAddr: DefaultAddr, wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "端口非法回退安全默认",
			env:      map[string]string{EnvAddr: "127.0.0.1:70000"},
			wantAddr: DefaultAddr, wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "端口非数字回退安全默认",
			env:      map[string]string{EnvAddr: "127.0.0.1:abc"},
			wantAddr: DefaultAddr, wantEnabled: true, wantAdjusted: true,
		},
		{
			name:     "端口 0 允许（测试用随机端口）",
			env:      map[string]string{EnvAddr: "127.0.0.1:0"},
			wantAddr: "127.0.0.1:0", wantEnabled: true,
		},
		{
			name:        "开关关闭",
			env:         map[string]string{EnvEnabled: "false"},
			wantAddr:    DefaultAddr,
			wantEnabled: false,
		},
		{
			name:        "开关 0 视为关闭",
			env:         map[string]string{EnvEnabled: "0"},
			wantAddr:    DefaultAddr,
			wantEnabled: false,
		},
		{
			name:     "开关大写 TRUE 视为开启",
			env:      map[string]string{EnvEnabled: "TRUE"},
			wantAddr: DefaultAddr, wantEnabled: true,
		},
		{
			name:     "开关取值无法识别时保持默认开启",
			env:      map[string]string{EnvEnabled: "banana"},
			wantAddr: DefaultAddr, wantEnabled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadConfigFrom(envFunc(tc.env))
			if cfg.Addr != tc.wantAddr {
				t.Errorf("Addr = %q, 期望 %q", cfg.Addr, tc.wantAddr)
			}
			if cfg.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, 期望 %v", cfg.Enabled, tc.wantEnabled)
			}
			if cfg.AllowNonLoopback != tc.wantAllow {
				t.Errorf("AllowNonLoopback = %v, 期望 %v", cfg.AllowNonLoopback, tc.wantAllow)
			}
			if (cfg.Adjustment != "") != tc.wantAdjusted {
				t.Errorf("Adjustment = %q, 期望是否收敛 = %v", cfg.Adjustment, tc.wantAdjusted)
			}
			if tc.wantEnabled && !isLoopbackHost(hostOf(t, cfg.Addr)) && !tc.wantAllow {
				t.Errorf("收敛后的地址 %q 必须仍是回环地址", cfg.Addr)
			}
		})
	}
}

func TestLoadConfigFromProcessEnv(t *testing.T) {
	t.Setenv(EnvAddr, "127.0.0.1:19092")
	t.Setenv(EnvEnabled, "false")
	t.Setenv(EnvAllowNonLoopback, "yes")

	cfg := LoadConfig()
	if cfg.Addr != "127.0.0.1:19092" {
		t.Errorf("Addr = %q, 期望 127.0.0.1:19092", cfg.Addr)
	}
	if cfg.Enabled {
		t.Error("Enabled 应为 false")
	}
	if !cfg.AllowNonLoopback {
		t.Error("AllowNonLoopback 应为 true")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1":      true,
		"127.8.8.8":      true,
		"::1":            true,
		"localhost":      true,
		"LOCALHOST":      true,
		"":               false,
		"0.0.0.0":        false,
		"::":             false,
		"192.0.2.10": false,
		"cp.internal":    false,
	}
	for host, want := range tests {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, 期望 %v", host, got, want)
		}
	}
}

// hostOf 从监听地址中取出 host 部分，取不到时直接失败。
func hostOf(t *testing.T, addr string) string {
	t.Helper()
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		t.Fatalf("地址 %q 缺少端口", addr)
	}
	return strings.TrimSuffix(strings.TrimPrefix(addr[:idx], "["), "]")
}
