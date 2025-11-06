package infra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── getEnvOrDefault ───────────────────────────────────────────────────────

func TestGetEnvOrDefault_ReturnsDefault_WhenVarEmpty(t *testing.T) {
	t.Setenv("CP_INFRA_TEST_EMPTY", "")
	assert.Equal(t, "fallback", getEnvOrDefault("CP_INFRA_TEST_EMPTY", "fallback"))
}

func TestGetEnvOrDefault_ReturnsEnvValue_WhenSet(t *testing.T) {
	t.Setenv("CP_INFRA_TEST_PRESENT", "thevalue")
	assert.Equal(t, "thevalue", getEnvOrDefault("CP_INFRA_TEST_PRESENT", "fallback"))
}

func TestGetEnvOrDefault_ReturnsDefault_WhenVarUnset(t *testing.T) {
	// Rely on key being absent in CI; t.Setenv would set it to empty which
	// also triggers the default branch, so this is equivalent.
	val := getEnvOrDefault("CP_INFRA_TEST_DEFINITELY_ABSENT_XYZ987", "mydefault")
	assert.Equal(t, "mydefault", val)
}

// ─── InitPostgres (negative / config-building paths) ─────────────────────
//
// We never connect to a real DB. Port 1 causes an immediate "connection
// refused" from the pgx driver so there is no long network timeout.

func TestInitPostgres_FailsWithBadPort_ViaDATABASE_URL(t *testing.T) {
	t.Setenv("DATABASE_URL",
		"host=localhost port=1 user=x password=x dbname=x sslmode=disable")

	db, err := InitPostgres()
	assert.Nil(t, db)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "connect postgres:"),
		"error should be wrapped with 'connect postgres:': %s", err)
}

func TestInitPostgres_FailsWithBadPort_ViaComponentEnvVars(t *testing.T) {
	// Clear DATABASE_URL so that component vars are assembled into a DSN.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "1")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpass")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_SSLMODE", "disable")

	db, err := InitPostgres()
	assert.Nil(t, db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect postgres")
}

func TestInitPostgres_DefaultComponentValues_StillFail(t *testing.T) {
	// With all component vars cleared, defaults (localhost:5432) are used.
	// Unless a local Postgres is running on port 5432, this should fail.
	// We use a non-existent port to guarantee failure without a network wait.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "1") // override only port so defaults kick in for others
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "")
	t.Setenv("DB_SSLMODE", "")

	_, err := InitPostgres()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect postgres")
}

// ─── InitRedis (negative / config-building paths) ─────────────────────────
//
// Port 1 causes "connection refused" immediately during Ping().

func TestInitRedis_FailsWithUnreachableAddr(t *testing.T) {
	t.Setenv("REDIS_ADDR", "localhost:1")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")

	rdb, err := InitRedis()
	assert.Nil(t, rdb)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "ping redis:"),
		"error should be wrapped with 'ping redis:': %s", err)
}

func TestInitRedis_ParsesDBIndex_AndFails(t *testing.T) {
	// Verify that a non-zero DB index is parsed without panicking.
	t.Setenv("REDIS_ADDR", "localhost:1")
	t.Setenv("REDIS_DB", "5")

	_, err := InitRedis()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping redis")
}

func TestInitRedis_InvalidDBIndex_DefaultsToZero(t *testing.T) {
	// Non-numeric DB index should default to 0 via fmt.Sscanf.
	t.Setenv("REDIS_ADDR", "localhost:1")
	t.Setenv("REDIS_DB", "notanumber")

	_, err := InitRedis()
	require.Error(t, err) // still fails on Ping, but shouldn't panic
	assert.Contains(t, err.Error(), "ping redis")
}

func TestInitRedis_DefaultAddr_IsLocalhostPort6379(t *testing.T) {
	// Verify getEnvOrDefault supplies "localhost:6379" when REDIS_ADDR is empty.
	// We exercise the path via getEnvOrDefault directly (package-internal).
	t.Setenv("REDIS_ADDR", "")
	addr := getEnvOrDefault("REDIS_ADDR", "localhost:6379")
	assert.Equal(t, "localhost:6379", addr)
}
