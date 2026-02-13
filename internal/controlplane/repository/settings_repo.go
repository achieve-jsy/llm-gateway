package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SettingsRepo struct {
	db *gorm.DB
}

func NewSettingsRepo(db *gorm.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

func (r *SettingsRepo) GetValue(key string) (string, error) {
	var cfg model.SystemConfig
	err := r.db.First(&cfg, "key = ?", key).Error
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}

func (r *SettingsRepo) GetMultiple(keys []string) (map[string]string, error) {
	var cfgs []model.SystemConfig
	err := r.db.Where("key IN ?", keys).Find(&cfgs).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(cfgs))
	for _, c := range cfgs {
		result[c.Key] = c.Value
	}
	return result, nil
}

func (r *SettingsRepo) SetMultiple(settings map[string]string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cfgs := make([]model.SystemConfig, 0, len(settings))
	for k, v := range settings {
		cfgs = append(cfgs, model.SystemConfig{Key: k, Value: v, CreatedAt: now, UpdatedAt: now})
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&cfgs).Error
}

func (r *SettingsRepo) GetAll() ([]model.SystemConfig, error) {
	var cfgs []model.SystemConfig
	err := r.db.Order("key").Find(&cfgs).Error
	return cfgs, err
}

// DB 暴露底层 gorm.DB，供其他服务查询轻量数据（如用户邮箱），
// 避免大范围注入依赖。
func (r *SettingsRepo) DB() *gorm.DB {
	return r.db
}