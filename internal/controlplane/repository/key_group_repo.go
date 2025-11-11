package repository

import (
	"context"
	"fmt"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type KeyGroupRepo struct {
	db *gorm.DB
}

func NewKeyGroupRepo(db *gorm.DB) *KeyGroupRepo {
	return &KeyGroupRepo{db: db}
}

type KeyGroupInfo struct {
	GroupID   uint   `json:"id"`
	GroupName string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Strategy  string `json:"strategy"`
}

func (r *KeyGroupRepo) BatchInsert(ctx context.Context, tx *gorm.DB, keyID uuid.UUID, groupIDs []uint) error {
	if len(groupIDs) == 0 {
		return nil
	}
	bindings := make([]model.KeyGroupBinding, 0, len(groupIDs))
	for _, gid := range groupIDs {
		bindings = append(bindings, model.KeyGroupBinding{
			KeyID:   keyID,
			GroupID: gid,
		})
	}
	db := tx
	if db == nil {
		db = r.db
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key_id"}, {Name: "group_id"}},
		DoNothing: true,
	}).Create(&bindings).Error
}

func (r *KeyGroupRepo) BatchDelete(ctx context.Context, tx *gorm.DB, keyID uuid.UUID, groupIDs []uint) error {
	if len(groupIDs) == 0 {
		return nil
	}
	db := tx
	if db == nil {
		db = r.db
	}
	return db.WithContext(ctx).Where("key_id = ? AND group_id IN ?", keyID, groupIDs).Delete(&model.KeyGroupBinding{}).Error
}

func (r *KeyGroupRepo) FindByKeyID(ctx context.Context, keyID uuid.UUID) ([]KeyGroupInfo, error) {
	var results []KeyGroupInfo
	err := r.db.WithContext(ctx).
		Table("key_group_bindings AS kbg").
		Select("g.id as group_id, g.name as group_name, g.enabled, g.strategy").
		Joins("JOIN model_groups g ON g.id = kbg.group_id").
		Where("kbg.key_id = ? AND g.deleted_at IS NULL", keyID).
		Order("g.name ASC").
		Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("query key groups: %w", err)
	}
	return results, nil
}

func (r *KeyGroupRepo) ExistsByKeyAndGroup(ctx context.Context, keyID uuid.UUID, groupID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.KeyGroupBinding{}).
		Where("key_id = ? AND group_id = ?", keyID, groupID).
		Count(&count).Error
	return count > 0, err
}

func (r *KeyGroupRepo) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}