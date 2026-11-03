package config

import (
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
	Archive      ArchiveConfig
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

type BudgetTierConfig struct {
	MasterEnabled              bool
	AccountingEnabled          bool
	PositiveEnforcementEnabled bool
	ZeroBlockEnabled           bool
	ShadowEnabled              bool
	V2TotalEnforcementEnabled  bool
	LegacyTotalWriteEnabled    bool
}

// ArchiveConfig 调用链路正文归档 v3 配置
// 契约：docs/trace-archive-v3-implementation-contract.md §2.1
type ArchiveConfig struct {
	// —— 开关（v3 §5.1 拆分；旧 archive.enabled 作兼容读取，映射到 WriteEnabled）——
	WriteEnabled bool // archive.write.enabled / ARCHIVE_WRITE_ENABLED
	ReadEnabled  bool // archive.read.enabled  / ARCHIVE_READ_ENABLED

	// —— 基础 ——
	Root string // archive.root
	TZ   string // archive.tz

	// —— 热窗口与节流 ——
	HotWindowDays   int    // archive.hot_window_days    默认 3
	DaysPerNight    int    // archive.days_per_night     默认 2（仅常规归档，不含回填）
	ClosureLagHours int    // archive.closure_lag_hours  默认 24
	CronSchedule    string // archive.cron_schedule

	// —— 分块与压缩（v3 §4.1 / §E4）——
	ChunkMB            int    // archive.chunk_mb            默认 256（未压缩目标）
	ZstdLevel          string // archive.zstd_level          默认 "best"
	ZstdWindowMB       int    // archive.zstd_window_mb      默认 128（不可省！）
	EncoderConcurrency int    // archive.encoder_concurrency 默认 1
	MaxConcurrentReads int    // archive.max_concurrent_reads 默认 4

	// —— 导出 ——
	BatchRows int // archive.batch_rows 默认 1000
	MaxRetry  int // archive.max_retry  默认 5

	// —— 校验与巡检（v3 §3.2/§3.4/§3.5）——
	VerifySampleRows      int // archive.verify_sample_rows       默认 20
	InspectFreshnessHours int // archive.inspect_freshness_hours  默认 168
	InspectPerCycle       int // archive.inspect_per_cycle        默认 3

	// —— 回填（v3 §7.3，独立通道）——
	BackfillEnabled bool // archive.backfill.enabled  默认 false
	BackfillMaxDays int  // archive.backfill.max_days 默认 30

	// —— 离机副本闸 ——
	RequireOffhostCopy          bool   // archive.require_offhost_copy 默认 true
	OffhostCopyMarker           string // archive.offhost_copy_marker
	OffhostMarkerFreshnessHours int    // archive.offhost_marker_freshness_hours 默认 24

	// —— 磁盘与保留（v3 §9）——
	DiskWatermarkPercent int // archive.disk_watermark_percent 默认 80（env 键沿用 ARCHIVE_DISK_WATERMARK_MB）
	MaxDiskGB            int // archive.max_disk_gb           默认 0
	RetentionDays        int // archive.retention_days        默认 180

	// —— 暂存 ——
	StagingRetentionHours int // archive.staging_retention_hours 默认 336
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
	v.SetDefault("budget.timezone", "Asia/Shanghai")
	v.SetDefault("budget.week_start", "monday")

	// —— trace 正文归档 v3（ARCHIVE_* 环境变量经 AutomaticEnv 覆盖）——
	v.SetDefault("archive.write.enabled", false)
	v.SetDefault("archive.read.enabled", false)
	v.SetDefault("archive.root", "/datalog/llm-gateway-archive")
	v.SetDefault("archive.tz", "Asia/Shanghai")
	v.SetDefault("archive.hot_window_days", 3)
	v.SetDefault("archive.days_per_night", 2)
	v.SetDefault("archive.closure_lag_hours", 24)
	v.SetDefault("archive.cron_schedule", "0 3 * * *")
	v.SetDefault("archive.chunk_mb", 256)
	v.SetDefault("archive.zstd_level", "best")
	v.SetDefault("archive.zstd_window_mb", 128)
	v.SetDefault("archive.encoder_concurrency", 1)
	v.SetDefault("archive.max_concurrent_reads", 4)
	v.SetDefault("archive.batch_rows", 1000)
	v.SetDefault("archive.max_retry", 5)
	v.SetDefault("archive.verify_sample_rows", 20)
	v.SetDefault("archive.inspect_freshness_hours", 168)
	v.SetDefault("archive.inspect_per_cycle", 3)
	v.SetDefault("archive.backfill.enabled", false)
	v.SetDefault("archive.backfill.max_days", 30)
	v.SetDefault("archive.require_offhost_copy", true)
	v.SetDefault("archive.offhost_copy_marker", "")
	v.SetDefault("archive.offhost_marker_freshness_hours", 24)
	v.SetDefault("archive.disk_watermark_percent", 80)
	v.SetDefault("archive.max_disk_gb", 0)
	v.SetDefault("archive.retention_days", 180)
	v.SetDefault("archive.staging_retention_hours", 336)

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

	// 归档 v3：磁盘水位键名为历史遗留（ARCHIVE_DISK_WATERMARK_MB），语义为使用率百分比
	_ = v.BindEnv("archive.disk_watermark_percent", "ARCHIVE_DISK_WATERMARK_MB")

	// 归档 v3：开关拆分后旧总开关 archive.enabled 作兼容读取，语义映射到 WriteEnabled（v3 §5.1）
	archiveWriteEnabled := v.GetBool("archive.write.enabled")
	if v.IsSet("archive.enabled") && v.GetBool("archive.enabled") {
		archiveWriteEnabled = true
	}

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
		Archive: ArchiveConfig{
			WriteEnabled: archiveWriteEnabled,
			ReadEnabled:  v.GetBool("archive.read.enabled"),

			Root: v.GetString("archive.root"),
			TZ:   v.GetString("archive.tz"),

			HotWindowDays:   v.GetInt("archive.hot_window_days"),
			DaysPerNight:    v.GetInt("archive.days_per_night"),
			ClosureLagHours: v.GetInt("archive.closure_lag_hours"),
			CronSchedule:    v.GetString("archive.cron_schedule"),

			ChunkMB:            v.GetInt("archive.chunk_mb"),
			ZstdLevel:          v.GetString("archive.zstd_level"),
			ZstdWindowMB:       v.GetInt("archive.zstd_window_mb"),
			EncoderConcurrency: v.GetInt("archive.encoder_concurrency"),
			MaxConcurrentReads: v.GetInt("archive.max_concurrent_reads"),

			BatchRows: v.GetInt("archive.batch_rows"),
			MaxRetry:  v.GetInt("archive.max_retry"),

			VerifySampleRows:      v.GetInt("archive.verify_sample_rows"),
			InspectFreshnessHours: v.GetInt("archive.inspect_freshness_hours"),
			InspectPerCycle:       v.GetInt("archive.inspect_per_cycle"),

			BackfillEnabled: v.GetBool("archive.backfill.enabled"),
			BackfillMaxDays: v.GetInt("archive.backfill.max_days"),

			RequireOffhostCopy:          v.GetBool("archive.require_offhost_copy"),
			OffhostCopyMarker:           v.GetString("archive.offhost_copy_marker"),
			OffhostMarkerFreshnessHours: v.GetInt("archive.offhost_marker_freshness_hours"),

			DiskWatermarkPercent: v.GetInt("archive.disk_watermark_percent"),
			MaxDiskGB:            v.GetInt("archive.max_disk_gb"),
			RetentionDays:        v.GetInt("archive.retention_days"),

			StagingRetentionHours: v.GetInt("archive.staging_retention_hours"),
		},
	}
}
