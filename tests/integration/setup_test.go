package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 连接远程 PostgreSQL 测试库
// 当 RUN_INTEGRATION_TESTS != "1" 或 PG 不可达时自动 Skip，确保 CI/本地无 DB 也能通过编译/测试。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("skipping integration test: set RUN_INTEGRATION_TESTS=1 to enable")
	}

	host := getTestEnv("TEST_DB_HOST", "115.190.242.137")
	port := getTestEnv("TEST_DB_PORT", "5433")
	user := getTestEnv("TEST_DB_USER", "llm_gateway")
	pass := getTestEnv("TEST_DB_PASSWORD", "llm_gateway_dev_2026")
	dbname := getTestEnv("TEST_DB_NAME", "llm_gateway")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=3",
		host, port, user, pass, dbname)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to postgres %s:%s (%v)", host, port, err)
	}
	if sqlDB, perr := db.DB(); perr == nil {
		if perr := sqlDB.Ping(); perr != nil {
			t.Skipf("skipping integration test: ping failed (%v)", perr)
		}
	}

	// 每次测试前清表并重建（注意顺序：先删有外键的子表）
	db.Migrator().DropTable(&model.ModelTagRelation{})
	db.Migrator().DropTable(&model.ModelTag{})
	db.Migrator().DropTable(&model.Model{})
	db.Migrator().DropTable(&model.Provider{})
	if err := db.AutoMigrate(&model.Provider{}, &model.Model{}, &model.ModelTag{}, &model.ModelTagRelation{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func getTestEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// boolPtr 返回 bool 指针（用于 GORM *bool 字段赋值）
func boolPtr(b bool) *bool {
	return &b
}
