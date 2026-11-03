package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config control-plane 全局配置
type Config struct {
	Server       ServerConfig
	Controlplane ControlplaneConfig
	Redis        RedisConfig
	Database     DatabaseConfig
	Provider     ProviderConfig
	Auth         AuthConfig
	JWT          JWTConfig
	Gateway      GatewayConfig
	Budget       BudgetConfig
	BudgetTier   BudgetTierConfig
	// MediaTask 媒体任务平台（C7）：超时 sweeper 的参数。全部为**非密钥**配置，
	// 有安全默认值 ⇒ 不需要 GitLab CI 变量（遵守 test-env-config skill：只有密钥才进 TEST_*）。
	MediaTask MediaTaskConfig
	// TraceBodyRead 是读侧（点读服务）的点读配置：只含 root 与两个解码参数。
	TraceBodyRead TraceBodyReadConfig
}

type ServerConfig struct {
	Port string
}

type ControlplaneConfig struct {
	Port string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type DatabaseConfig struct {
	URL string
}

type ProviderConfig struct {
	InternalToken   string `mapstructure:"internal_token"`
	ControlPlaneURL string `mapstructure:"control_plane_url"`
}

type AuthConfig struct {
	URL   string
	Token string
}

type JWTConfig struct {
	Secret string
}

// GatewayConfig gateway 服务地址（供 control-plane 主动调用 gateway）
type GatewayConfig struct {
	URL string
}

type BudgetConfig struct {
	Timezone  string
	WeekStart string
}

// MediaTaskConfig 媒体任务平台配置（C7 超时 sweeper）。
//
// 环境变量（优先级最高，覆盖 YAML 与代码默认值）：
//   - MEDIA_TASK_TIMEOUT_MINUTES        非终态任务存活上限（分钟），默认 30
//   - MEDIA_TASK_SWEEP_INTERVAL_SECONDS 扫描周期（秒），默认 60
//   - MEDIA_TASK_SWEEP_BATCH            单轮最多处理条数，默认 200
//
// 全部为**非密钥**配置且有安全默认值 ⇒ 按 test-env-config skill 第 1 步的判定，
// 无需新增 GitLab `TEST_*` 变量（仓库内零密钥即可生效）。
type MediaTaskConfig struct {
	TimeoutMinutes       int
	SweepIntervalSeconds int
	SweepBatch           int
}

type BudgetTierConfig struct {
	MasterEnabled              bool
	AccountingEnabled          bool
	PositiveEnforcementEnabled bool
	ZeroBlockEnabled           bool
	ShadowEnabled              bool
	V2TotalEnforcementEnabled  bool
	LegacyTotalWriteEnabled    bool
}

// Load 加载配置：viper 读取 YAML + 环境变量覆盖
//
// 优先级（从高到低）：
//  1. 环境变量
//  2. config/{APP_ENV}.yaml（默认 dev）
//  3. 代码默认值
func Load() *Config {
	v := viper.New()

	// 默认值
	v.SetDefault("server.port", "8080")
	v.SetDefault("controlplane.port", "8081")
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("database.url", "")
	v.SetDefault("provider.internal_token", "CHANGE_ME_INTERNAL_TOKEN")
	v.SetDefault("provider.control_plane_url", "http://localhost:8081")
	v.SetDefault("auth.url", "http://localhost:8081")
	v.SetDefault("auth.token", "CHANGE_ME_INTERNAL_TOKEN")
	v.SetDefault("jwt.secret", "CHANGE_ME_JWT_SECRET")
	v.SetDefault("gateway.url", "http://localhost:8080")
	v.SetDefault("budget_tier.master_enabled", false)
	v.SetDefault("budget_tier.accounting_enabled", false)
	v.SetDefault("budget_tier.positive_enforcement_enabled", false)
	v.SetDefault("budget_tier.zero_block_enabled", false)
	v.SetDefault("budget_tier.shadow_enabled", false)
	v.SetDefault("budget_tier.v2_total_enforcement_enabled", false)
	v.SetDefault("budget_tier.legacy_total_write_enabled", true)
	v.SetDefault("media_task.timeout_minutes", 30)
	v.SetDefault("media_task.sweep_interval_seconds", 60)
	v.SetDefault("media_task.sweep_batch", 200)
	v.SetDefault("budget.timezone", "Asia/Shanghai")
	v.SetDefault("budget.week_start", "monday")
	// 读侧点读配置（与网关侧同名同前缀：两侧读的是同一个目录）
	v.SetDefault("trace_body.root", "")
	v.SetDefault("trace_body.read_max_concurrent", 2)
	v.SetDefault("trace_body.read_zstd_window_mb", 512)

	// 读取环境对应的 YAML
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}
	v.SetConfigName(env)
	v.SetConfigType("yaml")
	v.AddConfigPath("./config")
	v.AddConfigPath("../config")
	v.AddConfigPath("../../config")

	if err := v.ReadInConfig(); err != nil {
		log.Printf("[controlplane/config] warning: config file not found for env=%s, using defaults + env vars: %v", env, err)
	}

	// 环境变量自动覆盖
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 额外绑定历史环境变量名
	_ = v.BindEnv("controlplane.port", "CONTROL_PLANE_PORT")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("database.url", "DATABASE_URL")
	_ = v.BindEnv("provider.internal_token", "INTERNAL_TOKEN")
	_ = v.BindEnv("jwt.secret", "JWT_SECRET")
	_ = v.BindEnv("gateway.url", "GATEWAY_URL")
	_ = v.BindEnv("media_task.timeout_minutes", "MEDIA_TASK_TIMEOUT_MINUTES")
	_ = v.BindEnv("media_task.sweep_interval_seconds", "MEDIA_TASK_SWEEP_INTERVAL_SECONDS")
	_ = v.BindEnv("media_task.sweep_batch", "MEDIA_TASK_SWEEP_BATCH")

	return &Config{
		Server: ServerConfig{
			Port: v.GetString("server.port"),
		},
		Controlplane: ControlplaneConfig{
			Port: v.GetString("controlplane.port"),
		},
		Redis: RedisConfig{
			Addr:     v.GetString("redis.addr"),
			Password: v.GetString("redis.password"),
			DB:       v.GetInt("redis.db"),
		},
		Database: DatabaseConfig{
			URL: v.GetString("database.url"),
		},
		Provider: ProviderConfig{
			InternalToken:   v.GetString("provider.internal_token"),
			ControlPlaneURL: v.GetString("provider.control_plane_url"),
		},
		Auth: AuthConfig{
			URL:   v.GetString("auth.url"),
			Token: v.GetString("auth.token"),
		},
		JWT: JWTConfig{
			Secret: v.GetString("jwt.secret"),
		},
		Gateway: GatewayConfig{
			URL: v.GetString("gateway.url"),
		},
		Budget: BudgetConfig{
			Timezone:  v.GetString("budget.timezone"),
			WeekStart: v.GetString("budget.week_start"),
		},
		BudgetTier: BudgetTierConfig{
			MasterEnabled:              v.GetBool("budget_tier.master_enabled"),
			AccountingEnabled:          v.GetBool("budget_tier.accounting_enabled"),
			PositiveEnforcementEnabled: v.GetBool("budget_tier.positive_enforcement_enabled"),
			ZeroBlockEnabled:           v.GetBool("budget_tier.zero_block_enabled"),
			ShadowEnabled:              v.GetBool("budget_tier.shadow_enabled"),
			V2TotalEnforcementEnabled:  v.GetBool("budget_tier.v2_total_enforcement_enabled"),
			LegacyTotalWriteEnabled:    v.GetBool("budget_tier.legacy_total_write_enabled"),
		},
		MediaTask: MediaTaskConfig{
			TimeoutMinutes:       v.GetInt("media_task.timeout_minutes"),
			SweepIntervalSeconds: v.GetInt("media_task.sweep_interval_seconds"),
			SweepBatch:           v.GetInt("media_task.sweep_batch"),
		},
		TraceBodyRead: func() TraceBodyReadConfig {
			tbRead, err := LoadTraceBodyReadConfig(v)
			if err != nil {
				// 配置错误必须在启动时响亮失败，而不是让 control-plane 带着非法
				// 读侧配置跑起来（与网关侧 TraceBody 的处理同一判据）。
				// Load() 的签名不返回 error（既有风格），因此用 panic 表达启动失败。
				panic(fmt.Errorf("trace_body 读侧配置非法: %w", err))
			}
			return tbRead
		}(),
	}
}
