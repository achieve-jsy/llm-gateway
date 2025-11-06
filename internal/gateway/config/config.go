package config

import (
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
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
	InternalToken   string `mapstructure:"internal_token"`
	ControlPlaneURL string `mapstructure:"control_plane_url"`
}

// Load 加载配置：Viper 读取 YAML + 环境变量覆盖
//
// 优先级（从高到低）：
//  1. 环境变量（自动映射，如 REDIS_ADDR → redis.addr）
//  2. config/{env}.yaml（env 由 APP_ENV 决定，默认 dev）
//  3. 代码中 SetDefault 的默认值
func Load() *Config {
	v := viper.New()

	// 默认值
	v.SetDefault("server.port", "8080")
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("otlp.endpoint", "localhost:4317")
	v.SetDefault("otlp.enabled", false)
	v.SetDefault("provider.internal_token", "")
	v.SetDefault("provider.control_plane_url", "http://localhost:8081")

	// 确定环境：APP_ENV 可选 dev / staging / prod
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}

	// 读取对应环境的 YAML
	v.SetConfigName(env)
	v.SetConfigType("yaml")
	v.AddConfigPath("./config")
	v.AddConfigPath("../config")
	v.AddConfigPath("../../config")

	if err := v.ReadInConfig(); err != nil {
		// 配置文件找不到不是致命错误，可由环境变量兜底
		log.Printf("[config] warning: config file not found for env=%s, using defaults + env vars: %v", env, err)
	}

	// 环境变量自动覆盖：将 key 中的 "." 映射为 "_"
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 额外绑定一些与 YAML key 命名不一致的历史环境变量
	_ = v.BindEnv("server.port", "GATEWAY_PORT")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("otlp.endpoint", "OTLP_ENDPOINT")
	_ = v.BindEnv("otlp.enabled", "OTLP_ENABLED")
	_ = v.BindEnv("provider.internal_token", "INTERNAL_TOKEN")
	_ = v.BindEnv("provider.control_plane_url", "CONTROL_PLANE_URL")

	cfg := &Config{
		Server: ServerConfig{
			Port: v.GetString("server.port"),
		},
		Redis: RedisConfig{
			Addr:     v.GetString("redis.addr"),
			Password: v.GetString("redis.password"),
			DB:       v.GetInt("redis.db"),
		},
		OTLP: OTLPConfig{
			Endpoint: v.GetString("otlp.endpoint"),
			Enabled:  v.GetBool("otlp.enabled"),
		},
		Provider: ProviderConfig{
			InternalToken:   v.GetString("provider.internal_token"),
			ControlPlaneURL: v.GetString("provider.control_plane_url"),
		},
	}

	return cfg
}
