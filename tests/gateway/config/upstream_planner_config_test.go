package config_test

import (
	"testing"

	gwconfig "github.com/example/llm-gateway/internal/gateway/config"
	"github.com/stretchr/testify/require"
)

func TestLoad_UpstreamPlannerDefaultsDisabled(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-upstream-planner-config-test")

	cfg := gwconfig.Load()

	require.False(t, cfg.UpstreamPlanner.Enabled)
	require.False(t, cfg.UpstreamPlanner.ShadowEnabled)
	require.False(t, cfg.UpstreamPlanner.DebugHeaderEnabled)
}

func TestLoad_UpstreamPlannerEnvironmentOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-upstream-planner-config-test")
	t.Setenv("UPSTREAM_PLANNER_ENABLED", "true")
	t.Setenv("UPSTREAM_PLANNER_SHADOW_ENABLED", "true")
	t.Setenv("UPSTREAM_PLANNER_DEBUG_HEADER_ENABLED", "true")

	cfg := gwconfig.Load()

	require.True(t, cfg.UpstreamPlanner.Enabled)
	require.True(t, cfg.UpstreamPlanner.ShadowEnabled)
	require.True(t, cfg.UpstreamPlanner.DebugHeaderEnabled)
}
