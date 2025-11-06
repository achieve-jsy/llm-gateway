package config

import (
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	HTTP     HTTPConfig
	OIDC     OIDCConfig
}

type ServerConfig struct {
	Port string
}

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	URL      string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type HTTPConfig struct {
	TimeoutSeconds int
}

type OIDCConfig struct {
	IssuerURL         string
	ClientID          string
	ClientSecret      string
	RedirectURL       string
	FrontendURL       string
	Scopes            []string
	RoleClaimPath     string
	AdminMatchValues  []string
	DefaultRole       string
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

func (c PostgresConfig) DSN() string {
	if c.URL != "" {
		return c.URL
	}
	return "host=" + c.Host + " port=" + c.Port + " user=" + c.User + " password=" + c.Password + " dbname=" + c.DBName + " sslmode=" + c.SSLMode
}

func Load() *Config {
	v := viper.New()
	v.SetDefault("server.port", "8082")
	v.SetDefault("postgres.host", "localhost")
	v.SetDefault("postgres.port", "5432")
	v.SetDefault("postgres.user", "postgres")
	v.SetDefault("postgres.password", "postgres")
	v.SetDefault("postgres.dbname", "llm_gateway")
	v.SetDefault("postgres.sslmode", "disable")
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("http.timeout_seconds", 5)
	v.SetDefault("oidc.issuer_url", "")
	v.SetDefault("oidc.client_id", "")
	v.SetDefault("oidc.client_secret", "")
	v.SetDefault("oidc.redirect_url", "http://localhost:8082/auth/oidc/callback")
	v.SetDefault("oidc.frontend_url", "http://localhost:5173")
	v.SetDefault("oidc.scopes", "openid profile email")
	v.SetDefault("oidc.role_claim_path", "groups")
	v.SetDefault("oidc.admin_match_values", "llm-gateway-admins")
	v.SetDefault("oidc.default_role", "user")

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
		log.Printf("[auth-config] warning: config file not found for env=%s, using defaults + env vars: %v", env, err)
	}

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	_ = v.BindEnv("server.port", "AUTH_PORT")
	_ = v.BindEnv("postgres.url", "DATABASE_URL")
	_ = v.BindEnv("postgres.host", "DB_HOST")
	_ = v.BindEnv("postgres.port", "DB_PORT")
	_ = v.BindEnv("postgres.user", "DB_USER")
	_ = v.BindEnv("postgres.password", "DB_PASSWORD")
	_ = v.BindEnv("postgres.dbname", "DB_NAME")
	_ = v.BindEnv("postgres.sslmode", "DB_SSLMODE")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("http.timeout_seconds", "AUTH_HTTP_TIMEOUT_SECONDS")
	_ = v.BindEnv("oidc.issuer_url", "OAUTH_ISSUER_URL")
	_ = v.BindEnv("oidc.client_id", "OAUTH_CLIENT_ID")
	_ = v.BindEnv("oidc.client_secret", "OAUTH_CLIENT_SECRET")
	_ = v.BindEnv("oidc.redirect_url", "OAUTH_REDIRECT_URI")
	_ = v.BindEnv("oidc.frontend_url", "FRONTEND_URL")
	_ = v.BindEnv("oidc.scopes", "OAUTH_SCOPES")
	_ = v.BindEnv("oidc.role_claim_path", "OIDC_ROLE_CLAIM_PATH")
	_ = v.BindEnv("oidc.admin_match_values", "OIDC_ADMIN_MATCH_VALUES")
	_ = v.BindEnv("oidc.default_role", "OIDC_DEFAULT_ROLE")

	return &Config{
		Server: ServerConfig{
			Port: v.GetString("server.port"),
		},
		Postgres: PostgresConfig{
			Host:     v.GetString("postgres.host"),
			Port:     v.GetString("postgres.port"),
			User:     v.GetString("postgres.user"),
			Password: v.GetString("postgres.password"),
			DBName:   v.GetString("postgres.dbname"),
			SSLMode:  v.GetString("postgres.sslmode"),
			URL:      v.GetString("postgres.url"),
		},
		Redis: RedisConfig{
			Addr:     v.GetString("redis.addr"),
			Password: v.GetString("redis.password"),
			DB:       v.GetInt("redis.db"),
		},
		HTTP: HTTPConfig{
			TimeoutSeconds: v.GetInt("http.timeout_seconds"),
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
}
