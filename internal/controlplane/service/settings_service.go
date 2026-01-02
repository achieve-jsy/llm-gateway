package service

import (
	"context"
	"fmt"
	"strconv"

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
	// v0.7 分层限速
	SettingKeyGlobalRPM = "global_rpm"
	SettingKeyStudioRPM = "studio_rpm"
	SettingKeyDeptRPM   = "dept_rpm"
	SettingKeyKeyRPM    = "key_rpm"
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
		SettingKeyGlobalRPM, SettingKeyStudioRPM, SettingKeyDeptRPM, SettingKeyKeyRPM,
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
		// v0.7 分层限速（默认 0 = 不限）
		GlobalRPM: intOrDefault(vals, SettingKeyGlobalRPM, 0),
		StudioRPM: intOrDefault(vals, SettingKeyStudioRPM, 0),
		DeptRPM:   intOrDefault(vals, SettingKeyDeptRPM, 0),
		KeyRPM:    intOrDefault(vals, SettingKeyKeyRPM, 0),
	}, nil
}

func (s *SettingsService) GetPublicSettings(ctx context.Context) (*model.PublicSettings, error) {
	vals, err := s.repo.GetMultiple([]string{
		SettingKeySiteName, SettingKeySiteLogo, SettingKeySiteSubtitle, SettingKeyAPIBaseURL,
		SettingKeyRateLimitPerMin, SettingKeyMaxConcurrencyPerKey,
		SettingKeySupplierName,
		SettingKeyGlobalRPM, SettingKeyStudioRPM, SettingKeyDeptRPM, SettingKeyKeyRPM,
	})
	if err != nil {
		return nil, err
	}
	return &model.PublicSettings{
		SiteName:             valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		SiteLogo:             valOrDefault(vals, SettingKeySiteLogo, ""),
		SiteSubtitle:         valOrDefault(vals, SettingKeySiteSubtitle, ""),
		APIBaseURL:           valOrDefault(vals, SettingKeyAPIBaseURL, ""),
		RateLimitPerMinute:   intOrDefault(vals, SettingKeyRateLimitPerMin, defaultRateLimitPerMin),
		MaxConcurrencyPerKey: intOrDefault(vals, SettingKeyMaxConcurrencyPerKey, defaultMaxConcurrencyPerKey),
		SupplierName:        valOrDefault(vals, SettingKeySupplierName, defaultSupplierName),
		GlobalRPM:           intOrDefault(vals, SettingKeyGlobalRPM, 0),
		StudioRPM:           intOrDefault(vals, SettingKeyStudioRPM, 0),
		DeptRPM:             intOrDefault(vals, SettingKeyDeptRPM, 0),
		KeyRPM:              intOrDefault(vals, SettingKeyKeyRPM, 0),
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
	// v0.7 分层限速：0 = 不限，始终写入（确保显式设置 0 也能覆盖旧值）
	updates[SettingKeyGlobalRPM] = fmt.Sprintf("%d", settings.GlobalRPM)
	updates[SettingKeyStudioRPM] = fmt.Sprintf("%d", settings.StudioRPM)
	updates[SettingKeyDeptRPM] = fmt.Sprintf("%d", settings.DeptRPM)
	updates[SettingKeyKeyRPM] = fmt.Sprintf("%d", settings.KeyRPM)
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

func (s *SettingsService) GetSupplierName(ctx context.Context) string {
	v, err := s.repo.GetValue(SettingKeySupplierName)
	if err != nil || v == "" {
		return defaultSupplierName
	}
	return v
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