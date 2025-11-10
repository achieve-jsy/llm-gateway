package service

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockSettingsRepo struct {
	getValueFn    func(key string) (string, error)
	getMultipleFn func(keys []string) (map[string]string, error)
	setMultipleFn func(settings map[string]string) error
	getAllFn      func() ([]model.SystemConfig, error)
}

func (m *MockSettingsRepo) GetValue(key string) (string, error) {
	if m.getValueFn != nil {
		return m.getValueFn(key)
	}
	return "", nil
}

func (m *MockSettingsRepo) GetMultiple(keys []string) (map[string]string, error) {
	if m.getMultipleFn != nil {
		return m.getMultipleFn(keys)
	}
	return map[string]string{}, nil
}

func (m *MockSettingsRepo) SetMultiple(settings map[string]string) error {
	if m.setMultipleFn != nil {
		return m.setMultipleFn(settings)
	}
	return nil
}

func (m *MockSettingsRepo) GetAll() ([]model.SystemConfig, error) {
	if m.getAllFn != nil {
		return m.getAllFn()
	}
	return nil, nil
}

var _ SettingsRepository = (*MockSettingsRepo)(nil)

func TestGetDefaultMaxSwitches_UsesDefault(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			return "", errors.New("not found")
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultMaxSwitches(context.Background())
	assert.Equal(t, 2, result)
}

func TestGetDefaultMaxSwitches_UsesCustomValue(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			if key == SettingKeyDefaultMaxSwitches {
				return "5", nil
			}
			return "", nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultMaxSwitches(context.Background())
	assert.Equal(t, 5, result)
}

func TestGetDefaultSameModelRetries_UsesDefault(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			return "", errors.New("not found")
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultSameModelRetries(context.Background())
	assert.Equal(t, 1, result)
}

func TestGetDefaultSameModelRetries_UsesCustomValue(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			if key == SettingKeyDefaultSameModelRetries {
				return "3", nil
			}
			return "", nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultSameModelRetries(context.Background())
	assert.Equal(t, 3, result)
}

func TestGetDefaultSameModelRetries_ZeroIsValid(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			if key == SettingKeyDefaultSameModelRetries {
				return "0", nil
			}
			return "", nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultSameModelRetries(context.Background())
	assert.Equal(t, 0, result)
}

func TestGetDefaultFailoverCooldownSeconds_UsesDefault(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			return "", errors.New("not found")
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultFailoverCooldownSeconds(context.Background())
	assert.Equal(t, 60, result)
}

func TestGetDefaultFailoverCooldownSeconds_UsesCustomValue(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			if key == SettingKeyDefaultFailoverCooldown {
				return "300", nil
			}
			return "", nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultFailoverCooldownSeconds(context.Background())
	assert.Equal(t, 300, result)
}

func TestGetDefaultFailoverCooldown_InvalidValueReturnsDefault(t *testing.T) {
	repo := &MockSettingsRepo{
		getValueFn: func(key string) (string, error) {
			if key == SettingKeyDefaultFailoverCooldown {
				return "-10", nil
			}
			return "", nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	result := svc.GetDefaultFailoverCooldownSeconds(context.Background())
	assert.Equal(t, defaultFailoverCooldownSeconds, result)
}

func TestGetSystemSettings_IncludesFailoverDefaults(t *testing.T) {
	repo := &MockSettingsRepo{
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)
	settings, err := svc.GetSystemSettings(context.Background())

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, 2, settings.DefaultMaxSwitches)
	assert.Equal(t, 1, settings.DefaultSameModelRetries)
	assert.Equal(t, 60, settings.DefaultFailoverCooldownSeconds)
}

func TestGetSystemSettings_IncludesCustomFailoverDefaults(t *testing.T) {
	repo := &MockSettingsRepo{
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{
				"site_name":                         "TestGateway",
				SettingKeyDefaultMaxSwitches:        "4",
				SettingKeyDefaultSameModelRetries:   "2",
				SettingKeyDefaultFailoverCooldown:   "180",
			}, nil
		},
	}
	svc := NewSettingsService(repo)
	settings, err := svc.GetSystemSettings(context.Background())

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, 4, settings.DefaultMaxSwitches)
	assert.Equal(t, 2, settings.DefaultSameModelRetries)
	assert.Equal(t, 180, settings.DefaultFailoverCooldownSeconds)
}

func TestUpdateSettings_PersistsFailoverDefaults(t *testing.T) {
	var captured map[string]string
	repo := &MockSettingsRepo{
		setMultipleFn: func(settings map[string]string) error {
			captured = settings
			return nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)

	err := svc.UpdateSettings(context.Background(), &model.SystemSettings{
		SiteName:                       "Test",
		DefaultMaxSwitches:             5,
		DefaultSameModelRetries:        2,
		DefaultFailoverCooldownSeconds: 240,
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "5", captured[SettingKeyDefaultMaxSwitches])
	assert.Equal(t, "2", captured[SettingKeyDefaultSameModelRetries])
	assert.Equal(t, "240", captured[SettingKeyDefaultFailoverCooldown])
}

func TestUpdateSettings_ZeroValuesNotPersisted(t *testing.T) {
	var captured map[string]string
	repo := &MockSettingsRepo{
		setMultipleFn: func(settings map[string]string) error {
			captured = settings
			return nil
		},
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	svc := NewSettingsService(repo)

	err := svc.UpdateSettings(context.Background(), &model.SystemSettings{
		SiteName:                       "Test",
		DefaultMaxSwitches:             0,
		DefaultFailoverCooldownSeconds: 0,
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	_, hasMaxSwitches := captured[SettingKeyDefaultMaxSwitches]
	assert.False(t, hasMaxSwitches, "zero max_switches should not be persisted")
	_, hasCooldown := captured[SettingKeyDefaultFailoverCooldown]
	assert.False(t, hasCooldown, "zero cooldown should not be persisted")
}

func TestEnsureDefaults_SeedsFailoverDefaults(t *testing.T) {
	var seededKeys []string
	repo := &MockSettingsRepo{
		getMultipleFn: func(keys []string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		setMultipleFn: func(settings map[string]string) error {
			for k := range settings {
				seededKeys = append(seededKeys, k)
			}
			return nil
		},
	}
	_ = NewSettingsService(repo)

	assert.Contains(t, seededKeys, SettingKeyDefaultMaxSwitches)
	assert.Contains(t, seededKeys, SettingKeyDefaultSameModelRetries)
	assert.Contains(t, seededKeys, SettingKeyDefaultFailoverCooldown)
}

func TestAllSettingKeys_IncludesFailoverKeys(t *testing.T) {
	keys := allSettingKeys()
	assert.Contains(t, keys, SettingKeyDefaultMaxSwitches)
	assert.Contains(t, keys, SettingKeyDefaultSameModelRetries)
	assert.Contains(t, keys, SettingKeyDefaultFailoverCooldown)
}