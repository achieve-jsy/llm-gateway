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

func (r *KeyRepo) UpdateLogEnabled(id uuid.UUID, value bool) error {
	return r.db.Model(&model.ApiKey{}).Where("id = ?", id).Update("log_enabled", value).Error
}

func (r *KeyRepo) UpdateAccessAllGroups(id uuid.UUID, value bool) error {
	return r.db.Model(&model.ApiKey{}).Where("id = ?", id).Update("access_all_groups", value).Error
}

// UserDeptInfo 用于创建 Key 时冗余写入用户/部门/工作室名称。
type UserDeptInfo struct {
	UserName   string
	DeptID     string
	DeptName   string
	StudioID   string
	StudioName string
}

// BackfillUserDeptNames 补全 api_keys 表中缺失的 user_name / dept_id / dept_name / studio_id / studio_name（懒补全，异步调用）。
func (r *KeyRepo) BackfillUserDeptNames(keyID uuid.UUID, userName, deptID, deptName, studioID, studioName string) error {
	return r.db.Model(&model.ApiKey{}).
		Where("id = ?", keyID).
		Updates(map[string]any{
			"user_name":   userName,
			"dept_id":     deptID,
			"dept_name":   deptName,
			"studio_id":   studioID,
			"studio_name": studioName,
		}).Error
}

// GetUserDeptInfo 通过 user_id 查询用户姓名及所属部门、工作室信息。
// 优先查 users（OIDC 用户），查不到时回退到 dev_users（本地账号）。
func (r *KeyRepo) GetUserDeptInfo(userID uuid.UUID) (UserDeptInfo, error) {
	var info UserDeptInfo
	err := r.db.Raw(`
		SELECT u.name AS user_name,
		       COALESCE(d.dept_id, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(dsm.studio_id::text, '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE u.id = ?
	`, userID).Scan(&info).Error
	if err != nil {
		return info, err
	}
	// 未在 OIDC users 表中找到（本地 dev 账号），从 dev_users 取 username
	if info.UserName == "" {
		_ = r.db.Raw(`SELECT username AS user_name FROM dev_users WHERE id = ?`, userID).Scan(&info).Error
	}
	return info, nil
}

// ListByDeptAndType 按部门 SSO dept_id 和 key_type 列出 Key（v0.7 Service Key 列表）。
func (r *KeyRepo) ListByDeptAndType(deptID, keyType string) ([]model.ApiKey, error) {
	var keys []model.ApiKey
	err := r.db.Where("dept_id = ? AND key_type = ?", deptID, keyType).
		Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// GetDeptInfoByDeptID 通过 SSO dept_id 获取部门名称和所属工作室信息。
func (r *KeyRepo) GetDeptInfoByDeptID(deptID string) (UserDeptInfo, error) {
	var info UserDeptInfo
	if deptID == "" {
		return info, nil
	}
	r.db.Raw(`
		SELECT COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(dsm.studio_id::text, '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE d.dept_id = ?
	`, deptID).Scan(&info)
	return info, nil
}
