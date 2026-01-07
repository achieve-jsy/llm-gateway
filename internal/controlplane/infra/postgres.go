package infra

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitPostgres 初始化 PostgreSQL 连接
// 优先使用传入的 dsn 参数；为空时回退读环境变量 DATABASE_URL / DB_* 系列。
func InitPostgres(dsn ...string) (*gorm.DB, error) {
	connStr := ""
	if len(dsn) > 0 {
		connStr = dsn[0]
	}
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		host := getEnvOrDefault("DB_HOST", "localhost")
		port := getEnvOrDefault("DB_PORT", "5432")
		user := getEnvOrDefault("DB_USER", "postgres")
		pass := getEnvOrDefault("DB_PASSWORD", "postgres")
		dbname := getEnvOrDefault("DB_NAME", "llm_gateway")
		sslmode := getEnvOrDefault("DB_SSLMODE", "disable")
		connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			host, port, user, pass, dbname, sslmode)
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: connStr,
		// PreferSimpleProtocol 禁用 pgx prepared statement 缓存，
		// 避免迁移后 "cached plan must not change result type" (SQLSTATE 0A000) 500 错误。
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	// 连接池配置
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)

	return db, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
