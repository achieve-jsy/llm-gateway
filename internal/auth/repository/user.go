package repository

import (
	"github.com/example/llm-gateway/internal/auth/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) FindBySub(sub string) (*model.User, error) {
	var user model.User
	if err := r.db.Where("sub = ?", sub).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindBySubUnscoped 连软删除的行一起查。SSO 登录用它判断"这个人是离职被软删的"，
// 否则 FindBySub 查不到 → 走进新建分支 → 删了又建，离职人员原地复活。
func (r *UserRepository) FindBySubUnscoped(sub string) (*model.User, error) {
	var user model.User
	if err := r.db.Unscoped().Where("sub = ?", sub).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByEmail(email string) (*model.User, error) {
	var user model.User
	if err := r.db.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByID(id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := r.db.First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByIDs 批量按 ID 查询用户，返回 id->User 映射，缺失的 ID 不报错。
func (r *UserRepository) FindByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.User, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]*model.User{}, nil
	}
	var users []model.User
	if err := r.db.Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]*model.User, len(users))
	for i := range users {
		m[users[i].ID] = &users[i]
	}
	return m, nil
}

func (r *UserRepository) Create(user *model.User) error {
	return r.db.Create(user).Error
}

func (r *UserRepository) Update(user *model.User) error {
	return r.db.Save(user).Error
}
