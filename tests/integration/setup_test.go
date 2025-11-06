package integration

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 连接测试专用数据库（llm_gateway_test），不存在则自动创建
// 强制规则：禁止使用生产数据库 llm_gateway 运行测试
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("skipping integration test: set RUN_INTEGRATION_TESTS=1 to enable")
	}

	host := getTestEnv("TEST_DB_HOST", "115.190.242.137")
	port := getTestEnv("TEST_DB_PORT", "5433")
	user := getTestEnv("TEST_DB_USER", "llm_gateway")
	pass := getTestEnv("TEST_DB_PASSWORD", "llm_gateway_dev_2026")
	dbname := getTestEnv("TEST_DB_NAME", "llm_gateway_test")

	// 安全检查：禁止使用生产数据库
	if dbname == "llm_gateway" {
		t.Fatal("禁止使用生产数据库 'llm_gateway' 运行测试！请设置 TEST_DB_NAME=llm_gateway_test")
	}

	// 先尝试连接测试数据库
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbname)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		// 数据库不存在，尝试创建
		t.Logf("测试数据库 %s 不存在，正在创建...", dbname)
		if createErr := createTestDatabase(host, port, user, pass, dbname); createErr != nil {
			t.Fatalf("创建测试数据库失败: %v (原始错误: %v)", createErr, err)
		}
		// 重新连接
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			t.Fatalf("连接测试数据库失败: %v", err)
		}
	}

	// 验证连接
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 sql.DB 失败: %v", err)
	}
	if perr := sqlDB.Ping(); perr != nil {
		t.Fatalf("ping 测试数据库失败: %v", perr)
	}

	// 每次测试前清表并重建（注意顺序：先删有外键的子表）
	db.Migrator().DropTable(&model.ModelTagRelation{})
	db.Migrator().DropTable(&model.ModelTag{})
	db.Migrator().DropTable(&model.Model{})
	db.Migrator().DropTable(&model.Provider{})
	if err := db.AutoMigrate(&model.Provider{}, &model.Model{}, &model.ModelTag{}, &model.ModelTagRelation{}); err != nil {
		t.Fatalf("迁移表结构失败: %v", err)
	}

	return db
}

// createTestDatabase 连接默认库创建测试数据库
func createTestDatabase(host, port, user, pass, dbname string) error {
	// 连接已有的 llm_gateway 库来执行 CREATE DATABASE
	adminDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=llm_gateway sslmode=disable",
		host, port, user, pass)

	adminDB, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("无法连接管理数据库: %w", err)
	}

	sqlDB, _ := adminDB.DB()
	defer sqlDB.Close()

	// 创建测试数据库
	result := adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbname))
	if result.Error != nil {
		// 忽略"已存在"错误
		if result.Error.Error() != fmt.Sprintf(`ERROR: database "%s" already exists (SQLSTATE 42P04)`, dbname) {
			return fmt.Errorf("创建数据库失败: %w", result.Error)
		}
	}

	return nil
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
