package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/example/llm-gateway/internal/controlplane/model"
)

const (
	SettingKeySiteName             = "site_name"
	SettingKeySiteLogo             = "site_logo"
	SettingKeySiteSubtitle         = "site_subtitle"
	SettingKeyAPIBaseURL           = "api_base_url"
	SettingKeyRateLimitPerMin      = "gateway_rate_limit_per_min"
	SettingKeyMaxConcurrencyPerKey = "gateway_max_concurrency_per_key"
	SettingKeyOIDCIssuerURL        = "oidc_issuer_url"
	SettingKeyOIDCClientID         = "oidc_client_id"
	SettingKeyOIDCClientSecret     = "oidc_client_secret"
	SettingKeyOIDCRedirectURL      = "oidc_redirect_url"
	SettingKeyOIDCScopes           = "oidc_scopes"
	SettingKeyOIDCEnabled          = "oidc_enabled"
	SettingKeyWebhookEnabled       = "webhook_enabled"
	SettingKeyWebhookURL           = "webhook_url"
	SettingKeyWebhookSecret        = "webhook_secret"
	SettingKeyWebhookEvents               = "webhook_events"
	SettingKeyDefaultMaxSwitches          = "fault_default_max_switches"
	SettingKeyDefaultSameModelRetries     = "fault_default_same_model_retires"
	SettingKeyDefaultFailoverCooldown     = "fault_default_cooldown_seconds"
	SettingKeySupplierName                = "supplier_name"
	SettingKeyPricingUsdToCnyRate         = "pricing_usd_to_cny_rate"
	// v0.7 分层限速
	SettingKeyGlobalRPM = "global_rpm"
	SettingKeyStudioRPM = "studio_rpm"
	SettingKeyDeptRPM   = "dept_rpm"
	SettingKeyKeyRPM    = "key_rpm"

	// Key 活动提醒（后台离线扫描，不碰 gateway 热路径）
	SettingKeyKeyActivityEnabled         = "key_activity.enabled"
	SettingKeyKeyActivityScanIntervalSec = "key_activity.scan_interval_sec"
	SettingKeyKeyActivityIPAsSource      = "key_activity.ip_as_source"
	SettingKeyKeyActivityDormantDays     = "key_activity.dormant_days"
	SettingKeyKeyActivityEWMAAlpha       = "key_activity.ewma_alpha"
	SettingKeyKeyActivityEWMAK           = "key_activity.ewma_k"
	SettingKeyKeyActivityMinBaseline     = "key_activity.min_baseline"
	SettingKeyKeyActivityMinDev          = "key_activity.min_dev"
	SettingKeyKeyActivityBootstrapDays   = "key_activity.bootstrap_days"
	SettingKeyKeyActivityBootstrapDone   = "key_activity.bootstrap_done"
	SettingKeyKeyActivityScanCursor      = "key_activity.scan_cursor"

	// 邮件通知
	SettingKeyEmailEnabled      = "email_enabled"
	SettingKeySMTPHost          = "smtp_host"
	SettingKeySMTPPort          = "smtp_port"
	SettingKeySMTPFrom          = "smtp_from"
	SettingKeySMTPPassword      = "smtp_password"
	SettingKeySMTPTLSSkipVerify = "smtp_tls_skip_verify"
)

const (
	defaultRateLimitPerMin              = 60
	defaultMaxConcurrencyPerKey         = 10
	defaultOIDCScopes                   = "openid profile email"
	defaultWebhookEvents                = "health_check_failed"
	defaultMaxSwitches                  = 2
	defaultSameModelRetries             = 1
	defaultFailoverCooldownSeconds      = 60
	defaultSupplierName                 = "某公司"
	defaultPricingUsdToCnyRate          = 7.2
	defaultKAEnabled                    = false
	defaultKAScanIntervalSec            = 60
	defaultKAIPAsSource                 = true
	defaultKADormantDays                = 30
	defaultKAEWMAAlpha                  = 0.3
	defaultKAEWMAK                      = 3.0
	defaultKAMinBaseline                = 20.0
	defaultKAMinDev                     = 5.0
	defaultKABootstrapDays              = 90
)

type SettingsRepository interface {
	GetValue(key string) (string, error)
	GetMultiple(keys []string) (map[string]string, error)
	SetMultiple(settings map[string]string) error
	GetAll() ([]model.SystemConfig, error)
}

type SettingsService struct {
	repo SettingsRepository
}

func NewSettingsService(repo SettingsRepository) *SettingsService {
	s := &SettingsService{repo: repo}
	s.ensureDefaults()
	return s
}

func allSettingKeys() []string {
	return []string{
		SettingKeySiteName, SettingKeySiteLogo, SettingKeySiteSubtitle, SettingKeyAPIBaseURL,
		SettingKeyRateLimitPerMin, SettingKeyMaxConcurrencyPerKey,
		SettingKeyOIDCIssuerURL, SettingKeyOIDCClientID, SettingKeyOIDCClientSecret,
		SettingKeyOIDCRedirectURL, SettingKeyOIDCScopes, SettingKeyOIDCEnabled,
		SettingKeyWebhookEnabled, SettingKeyWebhookURL, SettingKeyWebhookSecret, SettingKeyWebhookEvents,
		SettingKeyDefaultMaxSwitches, SettingKeyDefaultSameModelRetries, SettingKeyDefaultFailoverCooldown,
		SettingKeySupplierName,
		SettingKeyPricingUsdToCnyRate,
		SettingKeyGlobalRPM, SettingKeyStudioRPM, SettingKeyDeptRPM, SettingKeyKeyRPM,
		SettingKeyKeyActivityEnabled, SettingKeyKeyActivityScanIntervalSec,
		SettingKeyKeyActivityIPAsSource, SettingKeyKeyActivityDormantDays,
		SettingKeyKeyActivityEWMAAlpha, SettingKeyKeyActivityEWMAK,
		SettingKeyKeyActivityMinBaseline, SettingKeyKeyActivityMinDev,
		SettingKeyKeyActivityBootstrapDays,
		SettingKeyKeyActivityBootstrapDone, SettingKeyKeyActivityScanCursor,
		SettingKeyEmailEnabled, SettingKeySMTPHost, SettingKeySMTPPort,
		SettingKeySMTPFrom, SettingKeySMTPPassword, SettingKeySMTPTLSSkipVerify,
	}
}

func (s *SettingsService) ensureDefaults() {
	defaults := map[string]string{
		SettingKeySiteName:                  "LLM Gateway",
		SettingKeySiteSubtitle:              "LLM API 统一网关",
		SettingKeyRateLimitPerMin:           fmt.Sprintf("%d", defaultRateLimitPerMin),
		SettingKeyMaxConcurrencyPerKey:      fmt.Sprintf("%d", defaultMaxConcurrencyPerKey),
		SettingKeyOIDCScopes:                defaultOIDCScopes,
		SettingKeyOIDCEnabled:               "false",
		SettingKeyWebhookEnabled:            "false",
		SettingKeyWebhookEvents:             defaultWebhookEvents,
		SettingKeyDefaultMaxSwitches:        fmt.Sprintf("%d", defaultMaxSwitches),
		SettingKeyDefaultSameModelRetries:   fmt.Sprintf("%d", defaultSameModelRetries),
		SettingKeyDefaultFailoverCooldown:   fmt.Sprintf("%d", defaultFailoverCooldownSeconds),
		SettingKeySupplierName:              defaultSupplierName,
		SettingKeyPricingUsdToCnyRate:       fmt.Sprintf("%f", defaultPricingUsdToCnyRate),
		SettingKeyKeyActivityEnabled:         fmt.Sprintf("%t", defaultKAEnabled),
		SettingKeyKeyActivityScanIntervalSec: fmt.Sprintf("%d", defaultKAScanIntervalSec),
		SettingKeyKeyActivityIPAsSource:      fmt.Sprintf("%t", defaultKAIPAsSource),
		SettingKeyKeyActivityDormantDays:     fmt.Sprintf("%d", defaultKADormantDays),
		SettingKeyKeyActivityEWMAAlpha:       fmt.Sprintf("%f", defaultKAEWMAAlpha),
		SettingKeyKeyActivityEWMAK:           fmt.Sprintf("%f", defaultKAEWMAK),
		SettingKeyKeyActivityMinBaseline:     fmt.Sprintf("%.0f", defaultKAMinBaseline),
		SettingKeyKeyActivityMinDev:          fmt.Sprintf("%.0f", defaultKAMinDev),
		SettingKeyKeyActivityBootstrapDays:   fmt.Sprintf("%d", defaultKABootstrapDays),
		SettingKeyEmailEnabled:              "false",
		SettingKeySMTPHost:                  "mail.example.com",
		SettingKeySMTPPort:                  "587",
		SettingKeySMTPFrom:                  "llm-gateway@example.com",
		SettingKeySMTPPassword:      "JHk#7wt#3v)R@W2jP)",
		SettingKeySMTPTLSSkipVerify: "false",
	}
	existing, _ := s.repo.GetMultiple(allSettingKeys())
	for k, v := range defaults {
		if _, ok := existing[k]; !ok {
			_ = s.repo.SetMultiple(map[string]string{k: v})
		}
	}
}

func (s *SettingsService) GetSystemSettings(ctx context.Context) (*model.SystemSettings, error) {
	vals, err := s.repo.GetMultiple(allSettingKeys())
	if err != nil {
		return nil, err
	}
	return &model.SystemSettings{
		SiteName:                       valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		SiteLogo:                       valOrDefault(vals, SettingKeySiteLogo, ""),
		SiteSubtitle:                   valOrDefault(vals, SettingKeySiteSubtitle, ""),
		APIBaseURL:                     valOrDefault(vals, SettingKeyAPIBaseURL, ""),
		RateLimitPerMinute:             intOrDefault(vals, SettingKeyRateLimitPerMin, defaultRateLimitPerMin),
		MaxConcurrencyPerKey:           intOrDefault(vals, SettingKeyMaxConcurrencyPerKey, defaultMaxConcurrencyPerKey),
		OIDCIssuerURL:                  valOrDefault(vals, SettingKeyOIDCIssuerURL, ""),
		OIDCClientID:                   valOrDefault(vals, SettingKeyOIDCClientID, ""),
		OIDCClientSecret:               valOrDefault(vals, SettingKeyOIDCClientSecret, ""),
		OIDCRedirectURL:                valOrDefault(vals, SettingKeyOIDCRedirectURL, ""),
		OIDCScopes:                     valOrDefault(vals, SettingKeyOIDCScopes, defaultOIDCScopes),
		OIDCEnabled:                    boolOrDefault(vals, SettingKeyOIDCEnabled, false),
		WebhookEnabled:                 boolOrDefault(vals, SettingKeyWebhookEnabled, false),
		WebhookURL:                     valOrDefault(vals, SettingKeyWebhookURL, ""),
		WebhookSecret:                  valOrDefault(vals, SettingKeyWebhookSecret, ""),
		WebhookEvents:                  valOrDefault(vals, SettingKeyWebhookEvents, defaultWebhookEvents),
		DefaultMaxSwitches:             intOrDefault(vals, SettingKeyDefaultMaxSwitches, defaultMaxSwitches),
		DefaultSameModelRetries:        intOrDefault(vals, SettingKeyDefaultSameModelRetries, defaultSameModelRetries),
		DefaultFailoverCooldownSeconds: intOrDefault(vals, SettingKeyDefaultFailoverCooldown, defaultFailoverCooldownSeconds),
		SupplierName:                  valOrDefault(vals, SettingKeySupplierName, defaultSupplierName),
		PricingUsdToCnyRate:           floatOrDefault(vals, SettingKeyPricingUsdToCnyRate, defaultPricingUsdToCnyRate),
		// v0.7 分层限速（默认 0 = 不限）
		GlobalRPM: intOrDefault(vals, SettingKeyGlobalRPM, 0),
		StudioRPM: intOrDefault(vals, SettingKeyStudioRPM, 0),
		DeptRPM:   intOrDefault(vals, SettingKeyDeptRPM, 0),
		KeyRPM:    intOrDefault(vals, SettingKeyKeyRPM, 0),
		// 邮件通知
		EmailEnabled:      boolOrDefault(vals, SettingKeyEmailEnabled, false),
		SMTPHost:          valOrDefault(vals, SettingKeySMTPHost, "mail.example.com"),
		SMTPPort:          intOrDefault(vals, SettingKeySMTPPort, 587),
		SMTPFrom:          valOrDefault(vals, SettingKeySMTPFrom, "llm-gateway@example.com"),
		SMTPPassword:      valOrDefault(vals, SettingKeySMTPPassword, ""),
		SMTPTLSSkipVerify: boolOrDefault(vals, SettingKeySMTPTLSSkipVerify, false),
	}, nil
}

func (s *SettingsService) GetPublicSettings(ctx context.Context) (*model.PublicSettings, error) {
	vals, err := s.repo.GetMultiple([]string{
		SettingKeySiteName, SettingKeySiteLogo, SettingKeySiteSubtitle, SettingKeyAPIBaseURL,
		SettingKeyRateLimitPerMin, SettingKeyMaxConcurrencyPerKey,
		SettingKeySupplierName,
		SettingKeyPricingUsdToCnyRate,
		SettingKeyGlobalRPM, SettingKeyStudioRPM, SettingKeyDeptRPM, SettingKeyKeyRPM,
	})
	if err != nil {
		return nil, err
	}
	return &model.PublicSettings{
		SiteName:              valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		SiteLogo:              valOrDefault(vals, SettingKeySiteLogo, ""),
		SiteSubtitle:          valOrDefault(vals, SettingKeySiteSubtitle, ""),
		APIBaseURL:            valOrDefault(vals, SettingKeyAPIBaseURL, ""),
		RateLimitPerMinute:    intOrDefault(vals, SettingKeyRateLimitPerMin, defaultRateLimitPerMin),
		MaxConcurrencyPerKey:  intOrDefault(vals, SettingKeyMaxConcurrencyPerKey, defaultMaxConcurrencyPerKey),
		SupplierName:          valOrDefault(vals, SettingKeySupplierName, defaultSupplierName),
		PricingUsdToCnyRate:   floatOrDefault(vals, SettingKeyPricingUsdToCnyRate, defaultPricingUsdToCnyRate),
		GlobalRPM:             intOrDefault(vals, SettingKeyGlobalRPM, 0),
		StudioRPM:             intOrDefault(vals, SettingKeyStudioRPM, 0),
		DeptRPM:               intOrDefault(vals, SettingKeyDeptRPM, 0),
		KeyRPM:                intOrDefault(vals, SettingKeyKeyRPM, 0),
	}, nil
}

func (s *SettingsService) UpdateSettings(ctx context.Context, settings *model.SystemSettings) error {
	updates := make(map[string]string)
	if settings.SiteName != "" {
		updates[SettingKeySiteName] = settings.SiteName
	}
	updates[SettingKeySiteLogo] = settings.SiteLogo
	updates[SettingKeySiteSubtitle] = settings.SiteSubtitle
	updates[SettingKeyAPIBaseURL] = settings.APIBaseURL
	if settings.RateLimitPerMinute > 0 {
		updates[SettingKeyRateLimitPerMin] = fmt.Sprintf("%d", settings.RateLimitPerMinute)
	}
	if settings.MaxConcurrencyPerKey > 0 {
		updates[SettingKeyMaxConcurrencyPerKey] = fmt.Sprintf("%d", settings.MaxConcurrencyPerKey)
	}
	updates[SettingKeyOIDCIssuerURL] = settings.OIDCIssuerURL
	updates[SettingKeyOIDCClientID] = settings.OIDCClientID
	if settings.OIDCClientSecret != "" {
		updates[SettingKeyOIDCClientSecret] = settings.OIDCClientSecret
	}
	updates[SettingKeyOIDCRedirectURL] = settings.OIDCRedirectURL
	updates[SettingKeyOIDCScopes] = settings.OIDCScopes
	updates[SettingKeyOIDCEnabled] = fmt.Sprintf("%t", settings.OIDCEnabled)
	updates[SettingKeyWebhookEnabled] = fmt.Sprintf("%t", settings.WebhookEnabled)
	updates[SettingKeyWebhookURL] = settings.WebhookURL
	if settings.WebhookSecret != "" {
		updates[SettingKeyWebhookSecret] = settings.WebhookSecret
	}
	updates[SettingKeyWebhookEvents] = settings.WebhookEvents
	if settings.DefaultMaxSwitches > 0 {
		updates[SettingKeyDefaultMaxSwitches] = fmt.Sprintf("%d", settings.DefaultMaxSwitches)
	}
	if settings.DefaultSameModelRetries >= 0 {
		updates[SettingKeyDefaultSameModelRetries] = fmt.Sprintf("%d", settings.DefaultSameModelRetries)
	}
	if settings.DefaultFailoverCooldownSeconds > 0 {
		updates[SettingKeyDefaultFailoverCooldown] = fmt.Sprintf("%d", settings.DefaultFailoverCooldownSeconds)
	}
	updates[SettingKeySupplierName] = settings.SupplierName
	if settings.PricingUsdToCnyRate > 0 {
		updates[SettingKeyPricingUsdToCnyRate] = fmt.Sprintf("%f", settings.PricingUsdToCnyRate)
	}
	// v0.7 分层限速：0 = 不限，始终写入（确保显式设置 0 也能覆盖旧值）
	updates[SettingKeyGlobalRPM] = fmt.Sprintf("%d", settings.GlobalRPM)
	updates[SettingKeyStudioRPM] = fmt.Sprintf("%d", settings.StudioRPM)
	updates[SettingKeyDeptRPM] = fmt.Sprintf("%d", settings.DeptRPM)
	updates[SettingKeyKeyRPM] = fmt.Sprintf("%d", settings.KeyRPM)
	// 邮件通知
	updates[SettingKeyEmailEnabled] = fmt.Sprintf("%t", settings.EmailEnabled)
	if settings.SMTPHost != "" {
		updates[SettingKeySMTPHost] = settings.SMTPHost
	}
	if settings.SMTPPort > 0 {
		updates[SettingKeySMTPPort] = fmt.Sprintf("%d", settings.SMTPPort)
	}
	if settings.SMTPFrom != "" {
		updates[SettingKeySMTPFrom] = settings.SMTPFrom
	}
	if settings.SMTPPassword != "" {
		updates[SettingKeySMTPPassword] = settings.SMTPPassword
	}
	updates[SettingKeySMTPTLSSkipVerify] = fmt.Sprintf("%t", settings.SMTPTLSSkipVerify)
	return s.repo.SetMultiple(updates)
}

func (s *SettingsService) GetGatewayRateLimitPerMinute(ctx context.Context) int {
	v, err := s.repo.GetValue(SettingKeyRateLimitPerMin)
	if err != nil {
		return defaultRateLimitPerMin
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultRateLimitPerMin
	}
	return n
}

func (s *SettingsService) GetGatewayMaxConcurrencyPerKey(ctx context.Context) int {
	v, err := s.repo.GetValue(SettingKeyMaxConcurrencyPerKey)
	if err != nil {
		return defaultMaxConcurrencyPerKey
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultMaxConcurrencyPerKey
	}
	return n
}

func (s *SettingsService) GetDefaultFailoverCooldownSeconds(ctx context.Context) int {
	v, err := s.repo.GetValue(SettingKeyDefaultFailoverCooldown)
	if err != nil {
		return defaultFailoverCooldownSeconds
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultFailoverCooldownSeconds
	}
	return n
}

func (s *SettingsService) GetDefaultMaxSwitches(ctx context.Context) int {
	return s.getFailoverDefault(SettingKeyDefaultMaxSwitches, defaultMaxSwitches)
}

func (s *SettingsService) GetDefaultSameModelRetries(ctx context.Context) int {
	return s.getFailoverDefault(SettingKeyDefaultSameModelRetries, defaultSameModelRetries)
}

func (s *SettingsService) getFailoverDefault(key string, def int) int {
	v, err := s.repo.GetValue(key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func valOrDefault(vals map[string]string, key, def string) string {
	if v, ok := vals[key]; ok && v != "" {
		return v
	}
	return def
}

func intOrDefault(vals map[string]string, key string, def int) int {
	v, ok := vals[key]
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func boolOrDefault(vals map[string]string, key string, def bool) bool {
	v, ok := vals[key]
	if !ok || v == "" {
		return def
	}
	return v == "true"
}

func floatOrDefault(vals map[string]string, key string, def float64) float64 {
	v, ok := vals[key]
	if !ok || v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func (s *SettingsService) GetSupplierName(ctx context.Context) string {
	v, err := s.repo.GetValue(SettingKeySupplierName)
	if err != nil || v == "" {
		return defaultSupplierName
	}
	return v
}

func (s *SettingsService) GetPricingUsdToCnyRate(ctx context.Context) float64 {
	v, err := s.repo.GetValue(SettingKeyPricingUsdToCnyRate)
	if err != nil || v == "" {
		return defaultPricingUsdToCnyRate
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return defaultPricingUsdToCnyRate
	}
	return n
}

func (s *SettingsService) ExportAll(ctx context.Context) (map[string]string, error) {
	cfgs, err := s.repo.GetAll()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(cfgs))
	for _, c := range cfgs {
		result[c.Key] = c.Value
	}
	return result, nil
}

func (s *SettingsService) ImportSettings(ctx context.Context, settings map[string]string) error {
	return s.repo.SetMultiple(settings)
}

// GetKeyActivityConfig 从 system_configs 解析 Key 活动提醒的全部配置
func (s *SettingsService) GetKeyActivityConfig(ctx context.Context) *keyactivity.KeyActivityConfig {
	enabled, _ := strconv.ParseBool(getStringSafe(s.repo, SettingKeyKeyActivityEnabled))
	interval, _ := strconv.Atoi(getStringSafe(s.repo, SettingKeyKeyActivityScanIntervalSec))
	ipAsSource, _ := strconv.ParseBool(getStringSafe(s.repo, SettingKeyKeyActivityIPAsSource))
	dormantDays, _ := strconv.Atoi(getStringSafe(s.repo, SettingKeyKeyActivityDormantDays))
	alpha, _ := strconv.ParseFloat(getStringSafe(s.repo, SettingKeyKeyActivityEWMAAlpha), 64)
	k, _ := strconv.ParseFloat(getStringSafe(s.repo, SettingKeyKeyActivityEWMAK), 64)
	minBaseline, _ := strconv.ParseFloat(getStringSafe(s.repo, SettingKeyKeyActivityMinBaseline), 64)
	minDev, _ := strconv.ParseFloat(getStringSafe(s.repo, SettingKeyKeyActivityMinDev), 64)
	bootstrapDays, _ := strconv.Atoi(getStringSafe(s.repo, SettingKeyKeyActivityBootstrapDays))
	return &keyactivity.KeyActivityConfig{
		Enabled: enabled, ScanIntervalSec: interval, IPAsSource: ipAsSource,
		DormantDays: dormantDays, Alpha: alpha, K: k,
		MinBaseline: minBaseline, MinDev: minDev, BootstrapDays: bootstrapDays,
	}
}

func getStringSafe(repo SettingsRepository, key string) string {
	v, _ := repo.GetValue(key)
	return v
}