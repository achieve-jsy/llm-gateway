package config

import (
	"os"
	"strconv"
)

// Config 网关全局配置
type Config struct {
	Server   ServerConfig
	Redis    RedisConfig
	OTLP     OTLPConfig
	Provider ProviderConfig
}

// ServerConfig 服务配置
type ServerConfig struct {
	Port string
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// OTLPConfig OpenTelemetry 导出配置
type OTLPConfig struct {
	Endpoint string
	Enabled  bool
}

// ProviderConfig 厂商通道配置
type ProviderConfig struct {
	InternalToken   string
	ControlPlaneURL string
}

// Load 从环境变量加载配置
func Load() *Config {
	redisDB, _ := strconv.Atoi(getEnvOrDefault("REDIS_DB", "0"))

	return &Config{
		Server: ServerConfig{
			Port: getEnvOrDefault("GATEWAY_PORT", "8080"),
		},
		Redis: RedisConfig{
			Addr:     getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       redisDB,
		},
		OTLP: OTLPConfig{
			Endpoint: getEnvOrDefault("OTLP_ENDPOINT", "localhost:4317"),
			Enabled:  os.Getenv("OTLP_ENABLED") == "true",
		},
		Provider: ProviderConfig{
			InternalToken:   os.Getenv("INTERNAL_TOKEN"),
			ControlPlaneURL: getEnvOrDefault("CONTROL_PLANE_URL", "http://localhost:8081"),
		},
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
