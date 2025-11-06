package service

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

const (
	SettingKeySiteName   = "site_name"
	SettingKeyAPIBaseURL = "api_base_url"
)

type SettingsRepository interface {
	GetValue(key string) (string, error)
	GetMultiple(keys []string) (map[string]string, error)
	SetMultiple(settings map[string]string) error
}

type SettingsService struct {
	repo SettingsRepository
}

func NewSettingsService(repo SettingsRepository) *SettingsService {
	s := &SettingsService{repo: repo}
	s.ensureDefaults()
	return s
}

func (s *SettingsService) ensureDefaults() {
	defaults := map[string]string{
		SettingKeySiteName: "LLM Gateway",
	}
	existing, _ := s.repo.GetMultiple([]string{SettingKeySiteName, SettingKeyAPIBaseURL})
	for k, v := range defaults {
		if _, ok := existing[k]; !ok {
			_ = s.repo.SetMultiple(map[string]string{k: v})
		}
	}
}

func (s *SettingsService) GetSystemSettings(ctx context.Context) (*model.SystemSettings, error) {
	vals, err := s.repo.GetMultiple([]string{SettingKeySiteName, SettingKeyAPIBaseURL})
	if err != nil {
		return nil, err
	}
	return &model.SystemSettings{
		SiteName:   valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		APIBaseURL: valOrDefault(vals, SettingKeyAPIBaseURL, ""),
	}, nil
}

func (s *SettingsService) GetPublicSettings(ctx context.Context) (*model.PublicSettings, error) {
	vals, err := s.repo.GetMultiple([]string{SettingKeySiteName, SettingKeyAPIBaseURL})
	if err != nil {
		return nil, err
	}
	return &model.PublicSettings{
		SiteName:   valOrDefault(vals, SettingKeySiteName, "LLM Gateway"),
		APIBaseURL: valOrDefault(vals, SettingKeyAPIBaseURL, ""),
	}, nil
}

func (s *SettingsService) UpdateSettings(ctx context.Context, settings *model.SystemSettings) error {
	updates := make(map[string]string)
	if settings.SiteName != "" {
		updates[SettingKeySiteName] = settings.SiteName
	}
	updates[SettingKeyAPIBaseURL] = settings.APIBaseURL
	return s.repo.SetMultiple(updates)
}

func valOrDefault(vals map[string]string, key, def string) string {
	if v, ok := vals[key]; ok && v != "" {
		return v
	}
	return def
}