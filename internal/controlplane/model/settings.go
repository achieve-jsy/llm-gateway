package model

type SystemConfig struct {
	Key         string `gorm:"primaryKey;column:key;type:varchar(100)" json:"key"`
	Value       string `gorm:"column:value;type:text" json:"value"`
	Encrypted   bool   `gorm:"column:encrypted" json:"encrypted"`
	Description string `gorm:"column:description;type:varchar(500)" json:"description"`
	CreatedAt   string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   string `gorm:"column:updated_at" json:"updated_at"`
}

func (SystemConfig) TableName() string {
	return "system_configs"
}

type SystemSettings struct {
	SiteName                       string  `json:"site_name"`
	SiteLogo                       string  `json:"site_logo"`
	SiteSubtitle                   string  `json:"site_subtitle"`
	APIBaseURL                     string  `json:"api_base_url"`
	RateLimitPerMinute             int     `json:"rate_limit_per_minute"`
	MaxConcurrencyPerKey           int     `json:"max_concurrency_per_key"`
	OIDCIssuerURL                  string  `json:"oidc_issuer_url"`
	OIDCClientID                   string  `json:"oidc_client_id"`
	OIDCClientSecret               string  `json:"oidc_client_secret"`
	OIDCRedirectURL                string  `json:"oidc_redirect_url"`
	OIDCScopes                     string  `json:"oidc_scopes"`
	OIDCEnabled                    bool    `json:"oidc_enabled"`
	WebhookEnabled                 bool    `json:"webhook_enabled"`
	WebhookURL                     string  `json:"webhook_url"`
	WebhookSecret                  string  `json:"webhook_secret"`
	WebhookEvents                  string  `json:"webhook_events"`
	DefaultMaxSwitches             int     `json:"default_max_switches"`
	DefaultSameModelRetries        int     `json:"default_same_model_retries"`
	DefaultFailoverCooldownSeconds int     `json:"default_failover_cooldown_seconds"`
	SupplierName                   string  `json:"supplier_name"`
	PricingUsdToCnyRate            float64 `json:"pricing_usd_to_cny_rate"`
	// v0.7 分层限速：0 = 不限
	GlobalRPM int `json:"global_rpm"`
	StudioRPM int `json:"studio_rpm"`
	DeptRPM   int `json:"dept_rpm"`
	KeyRPM    int `json:"key_rpm"`
}

type PublicSettings struct {
	SiteName              string  `json:"site_name"`
	SiteLogo              string  `json:"site_logo"`
	SiteSubtitle          string  `json:"site_subtitle"`
	APIBaseURL            string  `json:"api_base_url"`
	RateLimitPerMinute    int     `json:"rate_limit_per_minute"`
	MaxConcurrencyPerKey  int     `json:"max_concurrency_per_key"`
	SupplierName          string  `json:"supplier_name"`
	PricingUsdToCnyRate   float64 `json:"pricing_usd_to_cny_rate"`
	// v0.7 分层限速：0 = 不限
	GlobalRPM             int     `json:"global_rpm"`
	StudioRPM             int     `json:"studio_rpm"`
	DeptRPM               int     `json:"dept_rpm"`
	KeyRPM                int     `json:"key_rpm"`
}