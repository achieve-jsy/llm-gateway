//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type integrationInfra struct {
	Redis            *redis.Client
	DB               *gorm.DB
	PostgresDSN      string
	RedisContainerID string
}

func startIntegrationInfra(t *testing.T) integrationInfra {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("Docker is required for integration tests: %v", err)
	}
	redisID := runContainer(t, "redis:7-alpine")
	postgresID := runContainer(t, "postgres:16-alpine",
		"-e", "POSTGRES_PASSWORD=gateway",
		"-e", "POSTGRES_USER=gateway",
		"-e", "POSTGRES_DB=gateway_test",
	)
	redisAddr := mappedAddress(t, redisID, "6379/tcp")
	postgresAddr := mappedAddress(t, postgresID, "5432/tcp")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	require.Eventually(t, func() bool {
		return rdb.Ping(context.Background()).Err() == nil
	}, 30*time.Second, 100*time.Millisecond)
	var db *gorm.DB
	var postgresDSN string
	require.Eventually(t, func() bool {
		host, port, err := net.SplitHostPort(postgresAddr)
		if err != nil {
			return false
		}
		dsn := fmt.Sprintf("host=%s port=%s user=gateway password=gateway dbname=gateway_test sslmode=disable", host, port)
		candidate, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			return false
		}
		sqlDB, err := candidate.DB()
		if err != nil || sqlDB.Ping() != nil {
			return false
		}
		db = candidate
		postgresDSN = dsn
		return true
	}, 30*time.Second, 100*time.Millisecond)
	applyAtlasSchema(t, postgresAddr, "gateway", "gateway", "gateway_test")
	return integrationInfra{
		Redis: rdb, DB: db, PostgresDSN: postgresDSN, RedisContainerID: redisID,
	}
}

func applyAtlasSchema(t *testing.T, postgresAddr, user, password, database string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	url := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", user, password, postgresAddr, database)
	output, err := exec.Command(
		"atlas", "migrate", "apply",
		"--dir", "file://"+filepath.Join(root, "atlas-migrations"),
		"--url", url,
	).CombinedOutput()
	require.NoError(t, err, string(output))
}

func runContainer(t *testing.T, image string, args ...string) string {
	t.Helper()
	command := []string{"run", "-d", "--rm", "-P"}
	command = append(command, args...)
	command = append(command, image)
	output, err := exec.Command("docker", command...).CombinedOutput()
	require.NoError(t, err, string(output))
	id := strings.TrimSpace(string(output))
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", id).Run()
	})
	return id
}

func mappedAddress(t *testing.T, containerID, port string) string {
	t.Helper()
	require.Eventually(t, func() bool {
		return exec.Command("docker", "port", containerID, port).Run() == nil
	}, 30*time.Second, 100*time.Millisecond)
	output, err := exec.Command("docker", "port", containerID, port).Output()
	require.NoError(t, err)
	line := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	host, mappedPort, err := net.SplitHostPort(line)
	require.NoError(t, err)
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, mappedPort)
}
