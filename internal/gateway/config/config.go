package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 网关全局配置
type Config struct {
	Server        ServerConfig
	Redis         RedisConfig
	Database      DatabaseConfig
	OTLP          OTLPConfig
	Provider      ProviderConfig
	Auth          AuthConfig
	OIDC          OIDCConfig
	Observability ObservabilityConfig
	TraceBody     TraceBodyConfig
	Media         MediaConfig
}

// MediaConfig 媒体链路（图像 / 视频）配置。
type MediaConfig struct {
	// RequirePricing 命中「媒体请求 + 无价」时是否拒绝准入（402 media_pricing_missing）。
	//
	// 默认 true（保守）：model_catalog 的媒体价列默认 NULL 表示「无价」，
	// 放行等于静默记 0 元。关闭后放行，但采集侧会强制 usage_missing=true 并打
	// Error 级强告警（见 docs/media-channels-integration-design.md §8.3 / D3）。
	RequirePricing bool

	// TaskTimeoutMinutes 异步媒体任务的超时阈值（分钟），来自 MEDIA_TASK_TIMEOUT_MINUTES，默认 30。
	//
	// 语义（C7）：**网关只在查询请求内做前台判定，不做后台扫描**——
	// 「超时扫描的 cron 在控制面」。超过该阈值仍未终态的任务，网关在
	// GET /v1/videos/:task_id 里把它 CAS 推进到 expired（对外归一为 failed、不记账），
	// 保证即使控制面 sweeper 未部署，客户端也不会无限轮询。
	TaskTimeoutMinutes int

	// PollMaxFailures 单任务允许的连续轮询失败上限，来自 MEDIA_POLL_MAX_FAILURES，默认 20。
	//
	// 语义（C7）：上游查询失败按 not_found / auth / transient / unrecognized 分类；
	// transient / unrecognized 计入 poll_failures，达到本上限后放弃轮询并把任务置为
	// failed（不记账），避免上游抖动变成无休止轮询。
	PollMaxFailures int
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

// DatabaseConfig 数据库连接配置
type DatabaseConfig struct {
	URL string
}

type ObservabilityConfig struct {
	SpoolCapacity          int64
	SpoolHighWatermark     int64
	SpoolClaimMinIdle      time.Duration
	BillableReservationTTL time.Duration
	// TraceMaxStringLen 写入 call_traces 的单个文本字段字符上限（0 表示使用默认 500_000，
	// 即默认不截断长上下文，供用量/语义分析报告使用完整内容）。
	TraceMaxStringLen int
	// TraceMaxBase64Len base64 data-URI 保留字符上限（图片仍以占位符代替）。
	TraceMaxBase64Len int
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

// AuthConfig auth 服务配置
type AuthConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

// OIDCConfig OIDC 登录配置
type OIDCConfig struct {
	IssuerURL        string   `mapstructure:"issuer_url"`
	ClientID         string   `mapstructure:"client_id"`
	ClientSecret     string   `mapstructure:"client_secret"`
	RedirectURL      string   `mapstructure:"redirect_url"`
	FrontendURL      string   `mapstructure:"frontend_url"`
	Scopes           []string `mapstructure:"scopes"`
	RoleClaimPath    string   `mapstructure:"role_claim_path"`
	AdminMatchValues []string `mapstructure:"admin_match_values"`
	DefaultRole      string   `mapstructure:"default_role"`
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
	v.SetDefault("database.url", "")
	v.SetDefault("observability.spool_capacity", 100_000)
	v.SetDefault("observability.spool_high_watermark", 80_000)
	v.SetDefault("observability.spool_claim_min_idle", "30s")
	v.SetDefault("observability.billable_reservation_ttl", "10m")
	v.SetDefault("observability.trace_max_string_len", 500_000)
	v.SetDefault("observability.trace_max_base64_len", 64)
	// 正文直写 /datalog（规格 §11.1）。root 默认为**空** ⇒ 功能关闭，
	// 网关走既有路径（正文仍写 PG）。计划 4 的部署动作是
	// 「加 /datalog 挂载 + 设 TRACE_BODY_ROOT」同时发生。
	//
	// 【键必须在顶层 trace_body.*】放进 observability.* 会得到
	// OBSERVABILITY_TRACE_BODY_ROOT，与规格 §11.1 的环境变量名不符。
	v.SetDefault("trace_body.root", "")
	v.SetDefault("trace_body.inst", "")
	v.SetDefault("trace_body.block_mb", int64(1536))
	v.SetDefault("trace_body.flush_interval", "1m")
	v.SetDefault("trace_body.roll_interval", "1h")
	v.SetDefault("trace_body.disk_watermark_percent", 90)
	v.SetDefault("otlp.endpoint", "localhost:4317")
	v.SetDefault("otlp.enabled", false)
	// 媒体准入计价闸门（A2）：无价即拒绝，默认 true（保守）。
	// 注：AutomaticEnv + envKeyReplacer 已能把 MEDIA_REQUIRE_PRICING 映射到
	// media.require_pricing，下面的 BindEnv 是显式登记（与历史变量写法保持一致）；
	// 空字符串的 env 会被 viper 判为"未设置"（getEnv 要求 val != ""），
	// 因此 MEDIA_REQUIRE_PRICING= 不会把开关意外翻成 false。
	v.SetDefault("media.require_pricing", true)
	// 异步媒体任务平台的运行策略（C7）：超时阈值与轮询失败上限。
	// 与 New API 的 TASK_TIMEOUT_MINUTES / TASK_POLL_MAX_FAILURES 同义
	// （见 docs/media-channels-integration-design.md §4.1 / §6.4）。
	v.SetDefault("media.task_timeout_minutes", 30)
	v.SetDefault("media.poll_max_failures", 20)
	v.SetDefault("provider.internal_token", "")
	v.SetDefault("provider.control_plane_url", "http://localhost:8081")
	v.SetDefault("auth.url", "http://localhost:8082")
	v.SetDefault("auth.token", "")
	v.SetDefault("oidc.issuer_url", "")
	v.SetDefault("oidc.client_id", "")
	v.SetDefault("oidc.client_secret", "")
	v.SetDefault("oidc.redirect_url", "http://localhost:8080/auth/oidc/callback")
	v.SetDefault("oidc.frontend_url", "http://localhost:5173")
	v.SetDefault("oidc.scopes", "openid profile email")
	v.SetDefault("oidc.role_claim_path", "groups")
	v.SetDefault("oidc.admin_match_values", "llm-gateway-admins")
	v.SetDefault("oidc.default_role", "employee")

	// 确定环境：APP_ENV 可选 dev / test / prod
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
	_ = v.BindEnv("media.require_pricing", "MEDIA_REQUIRE_PRICING")
	_ = v.BindEnv("media.task_timeout_minutes", "MEDIA_TASK_TIMEOUT_MINUTES")
	_ = v.BindEnv("media.poll_max_failures", "MEDIA_POLL_MAX_FAILURES")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("database.url", "DATABASE_URL")
	_ = v.BindEnv("otlp.endpoint", "OTLP_ENDPOINT")
	_ = v.BindEnv("otlp.enabled", "OTLP_ENABLED")
	_ = v.BindEnv("provider.internal_token", "INTERNAL_TOKEN")
	_ = v.BindEnv("provider.control_plane_url", "CONTROL_PLANE_URL")
	_ = v.BindEnv("auth.url", "AUTH_URL")
	_ = v.BindEnv("auth.token", "AUTH_INTERNAL_TOKEN")
	_ = v.BindEnv("oidc.issuer_url", "OAUTH_ISSUER_URL")
	_ = v.BindEnv("oidc.client_id", "OAUTH_CLIENT_ID")
	_ = v.BindEnv("oidc.client_secret", "OAUTH_CLIENT_SECRET")
	_ = v.BindEnv("oidc.redirect_url", "OAUTH_REDIRECT_URI")
	_ = v.BindEnv("oidc.frontend_url", "FRONTEND_URL")
	_ = v.BindEnv("oidc.scopes", "OAUTH_SCOPES")
	_ = v.BindEnv("oidc.role_claim_path", "OIDC_ROLE_CLAIM_PATH")
	_ = v.BindEnv("oidc.admin_match_values", "OIDC_ADMIN_MATCH_VALUES")
	_ = v.BindEnv("oidc.default_role", "OIDC_DEFAULT_ROLE")

	cfg := &Config{
		Server: ServerConfig{
			Port: v.GetString("server.port"),
		},
		Redis: RedisConfig{
			Addr:     v.GetString("redis.addr"),
			Password: v.GetString("redis.password"),
			DB:       v.GetInt("redis.db"),
		},
		Database: DatabaseConfig{
			URL: v.GetString("database.url"),
		},
		Observability: ObservabilityConfig{
			SpoolCapacity:          v.GetInt64("observability.spool_capacity"),
			SpoolHighWatermark:     v.GetInt64("observability.spool_high_watermark"),
			SpoolClaimMinIdle:      v.GetDuration("observability.spool_claim_min_idle"),
			BillableReservationTTL: v.GetDuration("observability.billable_reservation_ttl"),
			TraceMaxStringLen:      v.GetInt("observability.trace_max_string_len"),
			TraceMaxBase64Len:      v.GetInt("observability.trace_max_base64_len"),
		},
		TraceBody: func() TraceBodyConfig {
			tb, err := LoadTraceBodyConfig(v)
			if err != nil {
				// 配置错误必须在启动时响亮失败，而不是让网关带着非法 inst 跑起来
				// （规格 §5.1：inst 是路径分量，非法值会让正文落到错误的目录）。
				// Load() 的签名不返回 error（既有风格），因此用 panic 表达启动失败。
				panic(fmt.Errorf("trace_body 配置非法: %w", err))
			}
			return tb
		}(),
		Media: MediaConfig{
			RequirePricing:     v.GetBool("media.require_pricing"),
			TaskTimeoutMinutes: normalizePositiveInt(v.GetInt("media.task_timeout_minutes"), defaultMediaTaskTimeoutMinutes),
			PollMaxFailures:    normalizePositiveInt(v.GetInt("media.poll_max_failures"), defaultMediaPollMaxFailures),
		},
		OTLP: OTLPConfig{
			Endpoint: v.GetString("otlp.endpoint"),
			Enabled:  v.GetBool("otlp.enabled"),
		},
		Provider: ProviderConfig{
			InternalToken:   v.GetString("provider.internal_token"),
			ControlPlaneURL: v.GetString("provider.control_plane_url"),
		},
		Auth: AuthConfig{
			URL:   v.GetString("auth.url"),
			Token: v.GetString("auth.token"),
		},
		OIDC: OIDCConfig{
			IssuerURL:        v.GetString("oidc.issuer_url"),
			ClientID:         v.GetString("oidc.client_id"),
			ClientSecret:     v.GetString("oidc.client_secret"),
			RedirectURL:      v.GetString("oidc.redirect_url"),
			FrontendURL:      v.GetString("oidc.frontend_url"),
			Scopes:           splitList(v.GetString("oidc.scopes")),
			RoleClaimPath:    v.GetString("oidc.role_claim_path"),
			AdminMatchValues: splitList(v.GetString("oidc.admin_match_values")),
			DefaultRole:      v.GetString("oidc.default_role"),
		},
	}

	return cfg
}

// 媒体任务平台的兜底默认值（C7）。与 SetDefault 保持一致：即使 SetDefault 被
// 绕过（例如未来改用别的加载方式），非法/缺失值也落回这两个安全值，
// 而不是 0（0 会让「超时 = 立刻过期」「失败上限 = 首次失败即放弃」）。
const (
	defaultMediaTaskTimeoutMinutes = 30
	defaultMediaPollMaxFailures    = 20
)

// normalizePositiveInt 把非正数（含未配置时的 0、负数、非法值）归一为兜底值。
func normalizePositiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' })
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
