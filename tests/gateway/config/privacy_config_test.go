package config_test

import (
	"testing"

	gwconfig "github.com/example/llm-gateway/internal/gateway/config"
	"github.com/stretchr/testify/require"
)

// 默认必须是 off：升级即脱敏会把线上请求打挂，也会让"没显式开启"变成隐式改写。
func TestLoad_PrivacyDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-privacy-config-test")
	cfg := gwconfig.Load()
	require.Equal(t, "off", cfg.Privacy.Mode)
	require.True(t, cfg.Privacy.RulesOnly)
	require.Equal(t, 8388608, cfg.Privacy.MaxInputBytes)
	require.Empty(t, cfg.Privacy.CacheDir)
}

// 环境变量优先于默认值（viper AutomaticEnv：privacy.mode → PRIVACY_MODE）。
func TestLoad_PrivacyEnvOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-privacy-config-test")
	t.Setenv("PRIVACY_MODE", "mask")
	t.Setenv("PRIVACY_RULES_ONLY", "false")
	t.Setenv("PRIVACY_MAX_INPUT_BYTES", "1024")
	t.Setenv("PRIVACY_CACHE_DIR", "/tmp/privacy-cache")

	cfg := gwconfig.Load()
	require.Equal(t, "mask", cfg.Privacy.Mode)
	require.False(t, cfg.Privacy.RulesOnly)
	require.Equal(t, 1024, cfg.Privacy.MaxInputBytes)
	require.Equal(t, "/tmp/privacy-cache", cfg.Privacy.CacheDir)
}
