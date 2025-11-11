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

// setupTestDB 连接测试专用数据库（llm_gateway_test），不存在则自动创建
// 强制规则：禁止使用生产数据库 llm_gateway 运行测试
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("skipping integration test: set RUN_INTEGRATION_TESTS=1 to enable")
	}

	host := getTestEnv("TEST_DB_HOST", "192.0.2.10")
	port := getTestEnv("TEST_DB_PORT", "5432")
	user := getTestEnv("TEST_DB_USER", "llm_gateway")
	pass := getTestEnv("TEST_DB_PASSWORD", "CHANGE_ME_PASSWORD")
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

	// 每次测试前清表并重建。
	// 这里直接用 CASCADE 清理，避免 PG 因残留类型/外键导致 DropTable/AutoMigrate 不稳定。
	cleanupSQL := []string{
		"DROP TABLE IF EXISTS model_tag_relations CASCADE",
		"DROP TABLE IF EXISTS model_tags CASCADE",
		"DROP TABLE IF EXISTS health_check_logs CASCADE",
		"DROP TABLE IF EXISTS health_check_configs CASCADE",
		"DROP TABLE IF EXISTS models CASCADE",
		"DROP TABLE IF EXISTS providers CASCADE",
		"DROP TABLE IF EXISTS provider_definitions CASCADE",
	}
	for _, sql := range cleanupSQL {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("清理测试表失败 (%s): %v", sql, err)
		}
	}

	if err := db.AutoMigrate(
		&model.ProviderDefinition{},
		&model.Provider{},
		&model.Model{},
		&model.ModelTag{},
		&model.ModelTagRelation{},
		&model.HealthCheckConfig{},
		&model.HealthCheckLog{},
	); err != nil {
		t.Fatalf("迁移表结构失败: %v", err)
	}

	seedProviderDefinitions(t, db)

	return db
}

func seedProviderDefinitions(t *testing.T, db *gorm.DB) {
	t.Helper()
	defs := []model.ProviderDefinition{
		{ID: "dashscope", DisplayName: "阿里百炼", Adapter: "openai-compatible", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Builtin: true, Enabled: true},
		{ID: "deepseek", DisplayName: "DeepSeek", Adapter: "openai-compatible", BaseURL: "https://api.deepseek.com/v1", Builtin: true, Enabled: true},
		{ID: "anthropic", DisplayName: "Anthropic", Adapter: "anthropic", BaseURL: "https://api.anthropic.com/v1", Builtin: true, Enabled: true},
		{ID: "bedrock", DisplayName: "AWS Bedrock", Adapter: "bedrock", BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", Builtin: true, Enabled: true},
	}
	for _, def := range defs {
		if err := db.Create(&def).Error; err != nil {
			t.Fatalf("seed provider definition %s 失败: %v", def.ID, err)
		}
	}

	cfg := model.HealthCheckConfig{
		Enabled:                    true,
		IntervalSeconds:            300,
		FailureThreshold:           3,
		FailureAction:              "auto_disable",
		CheckProviders:             true,
		CheckModels:                true,
		CheckMode:                  "connectivity",
		TimeoutSeconds:             15,
		ModelCheckEnabled:          true,
		ModelCheckIntervalSeconds:  300,
		ModelCheckFailureThreshold: 3,
	}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatalf("seed health_check_config 失败: %v", err)
	}
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
