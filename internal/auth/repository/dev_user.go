package repository

import (
	"github.com/example/llm-gateway/internal/auth/model"
	"gorm.io/gorm"
)

type DevUserRepository struct{ db *gorm.DB }

func NewDevUserRepository(db *gorm.DB) *DevUserRepository { return &DevUserRepository{db: db} }

func (r *DevUserRepository) FindByUsername(username string) (*model.DevUser, error) {
	var user model.DevUser
	if err := r.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *DevUserRepository) Create(user *model.DevUser) error {
	return r.db.Create(user).Error
}
