package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KeyRepo struct {
	db *gorm.DB
}

func NewKeyRepo(db *gorm.DB) *KeyRepo {
	return &KeyRepo{db: db}
}

func (r *KeyRepo) Create(key *model.ApiKey) error {
	return r.db.Create(key).Error
}

func (r *KeyRepo) FindByHash(hash string) (*model.ApiKey, error) {
	var k model.ApiKey
	err := r.db.Where("key_hash = ?", hash).First(&k).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *KeyRepo) FindByID(id uuid.UUID) (*model.ApiKey, error) {
	var k model.ApiKey
	err := r.db.First(&k, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *KeyRepo) ListByUserID(userID uuid.UUID, offset, limit int) ([]model.ApiKey, int64, error) {
	var keys []model.ApiKey
	var total int64

	query := r.db.Model(&model.ApiKey{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&keys).Error
	if err != nil {
		return nil, 0, err
	}

	return keys, total, nil
}

func (r *KeyRepo) CountByUserID(userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&model.ApiKey{}).Where("user_id = ? AND enabled = ?", userID, true).Count(&count).Error
	return count, err
}

func (r *KeyRepo) UpdateStatus(id uuid.UUID, enabled bool) error {
	return r.db.Model(&model.ApiKey{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func (r *KeyRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.ApiKey{}, "id = ?", id).Error
}

func (r *KeyRepo) TouchLastUsed(hash string) {
	now := time.Now()
	_ = r.db.Model(&model.ApiKey{}).Where("key_hash = ?", hash).Update("last_used_at", now).Error
}

func (r *KeyRepo) UpdateHashAndKey(id uuid.UUID, keyHash, encryptedKey, keyHint string) error {
	return r.db.Model(&model.ApiKey{}).Where("id = ?", id).Updates(map[string]any{
		"key_hash": keyHash, "encrypted_key": encryptedKey, "key_hint": keyHint,
	}).Error
}

func (r *KeyRepo) UpdateAccessAllGroups(id uuid.UUID, value bool) error {
	return r.db.Model(&model.ApiKey{}).Where("id = ?", id).Update("access_all_groups", value).Error
}
