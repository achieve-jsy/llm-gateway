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
	SettingKeyWebhookEvents        = "webhook_events"
)

const (
	defaultRateLimitPerMin      = 60
	defaultMaxConcurrencyPerKey = 10
	defaultOIDCScopes           = "openid profile email"
	defaultWebhookEvents        = "health_check_failed"
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
	}
}

func (s *SettingsService) ensureDefaults() {
	defaults := map[string]string{
		SettingKeySiteName:             "LLM Gateway",
		SettingKeySiteSubtitle:         "LLM API 统一网关",
		SettingKeyRateLimitPerMin:      fmt.Sprintf("%d", defaultRateLimitPerMin),
		SettingKeyMaxConcurrencyPerKey: fmt.Sprintf("%d", defaultMaxConcurrencyPerKey),
		SettingKeyOIDCScopes:           defaultOIDCScopes,
		SettingKeyOIDCEnabled:          "false",
		SettingKeyWebhookEnabled:       "false",
		SettingKeyWebhookEvents:        defaultWebhookEvents,
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
		SiteName:             valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		SiteLogo:             valOrDefault(vals, SettingKeySiteLogo, ""),
		SiteSubtitle:         valOrDefault(vals, SettingKeySiteSubtitle, ""),
		APIBaseURL:           valOrDefault(vals, SettingKeyAPIBaseURL, ""),
		RateLimitPerMinute:   intOrDefault(vals, SettingKeyRateLimitPerMin, defaultRateLimitPerMin),
		MaxConcurrencyPerKey: intOrDefault(vals, SettingKeyMaxConcurrencyPerKey, defaultMaxConcurrencyPerKey),
		OIDCIssuerURL:        valOrDefault(vals, SettingKeyOIDCIssuerURL, ""),
		OIDCClientID:         valOrDefault(vals, SettingKeyOIDCClientID, ""),
		OIDCClientSecret:     valOrDefault(vals, SettingKeyOIDCClientSecret, ""),
		OIDCRedirectURL:      valOrDefault(vals, SettingKeyOIDCRedirectURL, ""),
		OIDCScopes:           valOrDefault(vals, SettingKeyOIDCScopes, defaultOIDCScopes),
		OIDCEnabled:          boolOrDefault(vals, SettingKeyOIDCEnabled, false),
		WebhookEnabled:       boolOrDefault(vals, SettingKeyWebhookEnabled, false),
		WebhookURL:           valOrDefault(vals, SettingKeyWebhookURL, ""),
		WebhookSecret:        valOrDefault(vals, SettingKeyWebhookSecret, ""),
		WebhookEvents:        valOrDefault(vals, SettingKeyWebhookEvents, defaultWebhookEvents),
	}, nil
}

func (s *SettingsService) GetPublicSettings(ctx context.Context) (*model.PublicSettings, error) {
	vals, err := s.repo.GetMultiple([]string{
		SettingKeySiteName, SettingKeySiteLogo, SettingKeySiteSubtitle, SettingKeyAPIBaseURL,
		SettingKeyRateLimitPerMin, SettingKeyMaxConcurrencyPerKey,
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