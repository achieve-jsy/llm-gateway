package config_test

import (
	"testing"
	"time"

	gwconfig "github.com/example/llm-gateway/internal/gateway/config"
	"github.com/stretchr/testify/require"
)

func TestLoad_ObservabilityAdmissionDefaultsAndOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "nonexistent-observability-config-test")
	defaults := gwconfig.Load()
	require.Equal(t, int64(100_000), defaults.Observability.SpoolCapacity)
	require.Equal(t, int64(80_000), defaults.Observability.SpoolHighWatermark)
	require.Equal(t, 30*time.Second, defaults.Observability.SpoolClaimMinIdle)
	require.Equal(t, 10*time.Minute, defaults.Observability.BillableReservationTTL)

	t.Setenv("OBSERVABILITY_SPOOL_CAPACITY", "321")
	t.Setenv("OBSERVABILITY_SPOOL_HIGH_WATERMARK", "250")
	t.Setenv("OBSERVABILITY_SPOOL_CLAIM_MIN_IDLE", "45s")
	t.Setenv("OBSERVABILITY_BILLABLE_RESERVATION_TTL", "20m")
	overridden := gwconfig.Load()
	require.Equal(t, int64(321), overridden.Observability.SpoolCapacity)
	require.Equal(t, int64(250), overridden.Observability.SpoolHighWatermark)
	require.Equal(t, 45*time.Second, overridden.Observability.SpoolClaimMinIdle)
	require.Equal(t, 20*time.Minute, overridden.Observability.BillableReservationTTL)
}
