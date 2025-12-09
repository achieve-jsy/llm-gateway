package service

import (
	"context"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KeyOrgSyncer 在 org 变动后异步重算受影响用户的 api_keys org 快照字段，
// 并发 key_updated 事件让 gateway KeyCache 主动失效。
//
// 使用方式：在各 org 变动操作（SetDeptStudio、ChangeDepartment、FollowSSO、
// upsertUser、ImportMappings）完成后调用对应方法（均为异步，不阻塞响应）。
type KeyOrgSyncer struct {
	db        *gorm.DB
	keyRepo   *KeyOrgSyncerKeyRepo
	publisher EventPublisher
}

// KeyOrgSyncerKeyRepo 仅暴露 KeyOrgSyncer 需要的最小 DB 操作
type KeyOrgSyncerKeyRepo struct {
	db *gorm.DB
}

func NewKeyOrgSyncerRepo(db *gorm.DB) *KeyOrgSyncerKeyRepo {
	return &KeyOrgSyncerKeyRepo{db: db}
}

// userOrgRow 查询结果行
type userOrgRow struct {
	UserID     string `gorm:"column:user_id"`
	UserName   string `gorm:"column:user_name"`
	DeptID     string `gorm:"column:dept_id"`
	DeptName   string `gorm:"column:dept_name"`
	StudioID   string `gorm:"column:studio_id"`
	StudioName string `gorm:"column:studio_name"`
}

// getUsersOrgByDeptSSO 按 SSO dept_id 查询属于该部门的所有用户及其当前 org 信息
func (r *KeyOrgSyncerKeyRepo) getUsersOrgByDeptSSO(deptSSO string) ([]userOrgRow, error) {
	var rows []userOrgRow
	err := r.db.Raw(`
		SELECT u.id::text AS user_id,
		       u.name AS user_name,
		       COALESCE(d.id::text, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(dsm.studio_id::text, '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE d.dept_id = ?
	`, deptSSO).Scan(&rows).Error
	return rows, err
}

// getUserOrgByUserID 按 user_id 查询单个用户当前 org 信息
func (r *KeyOrgSyncerKeyRepo) getUserOrgByUserID(userID string) (*userOrgRow, error) {
	var row userOrgRow
	err := r.db.Raw(`
		SELECT u.id::text AS user_id,
		       u.name AS user_name,
		       COALESCE(d.id::text, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(dsm.studio_id::text, '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE u.id::text = ?
	`, userID).Scan(&row).Error
	return &row, err
}

// getUsersOrgByStudioID 按 studio_id 查询工作室下所有用户（工作室删除时清空 studio 字段用）
func (r *KeyOrgSyncerKeyRepo) getUsersOrgByStudioID(studioID string) ([]userOrgRow, error) {
	var rows []userOrgRow
	err := r.db.Raw(`
		SELECT u.id::text AS user_id,
		       u.name AS user_name,
		       COALESCE(d.id::text, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       '' AS studio_id,
		       '' AS studio_name
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		WHERE dsm.studio_id::text = ?
	`, studioID).Scan(&rows).Error
	return rows, err
}

// syncKeysForUsers 批量更新一批用户的 api_keys org 字段并发失效事件
func (r *KeyOrgSyncerKeyRepo) syncKeysForUsers(rows []userOrgRow) []syncedKey {
	type keyRow struct {
		ID      string `gorm:"column:id"`
		KeyHash string `gorm:"column:key_hash"`
		UserID  string `gorm:"column:user_id"`
	}
	if len(rows) == 0 {
		return nil
	}
	userIDs := make([]string, 0, len(rows))
	infoByUser := make(map[string]userOrgRow, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
		infoByUser[row.UserID] = row
	}

	var keys []keyRow
	r.db.Raw(`SELECT id::text, key_hash, user_id::text FROM api_keys WHERE user_id::text IN ?`, userIDs).Scan(&keys)

	var synced []syncedKey
	for _, k := range keys {
		info, ok := infoByUser[k.UserID]
		if !ok {
			continue
		}
		keyUUID, err := uuid.Parse(k.ID)
		if err != nil {
			continue
		}
		if err := r.db.Model(&struct{ ID uuid.UUID }{ID: keyUUID}).
			Table("api_keys").
			Where("id = ?", keyUUID).
			Updates(map[string]any{
				"user_name":   info.UserName,
				"dept_id":     info.DeptID,
				"dept_name":   info.DeptName,
				"studio_id":   info.StudioID,
				"studio_name": info.StudioName,
			}).Error; err == nil {
			synced = append(synced, syncedKey{id: keyUUID, hash: k.KeyHash})
		}
	}
	return synced
}

type syncedKey struct {
	id   uuid.UUID
	hash string
}

// NewKeyOrgSyncer 创建 KeyOrgSyncer
func NewKeyOrgSyncer(db *gorm.DB, publisher EventPublisher) *KeyOrgSyncer {
	return &KeyOrgSyncer{
		db:        db,
		keyRepo:   NewKeyOrgSyncerRepo(db),
		publisher: publisher,
	}
}

// publish 对所有同步的 key 发失效事件
func (s *KeyOrgSyncer) publish(synced []syncedKey) {
	if s.publisher == nil {
		return
	}
	for _, k := range synced {
		s.publisher.InvalidateKeyCache(k.hash)
		s.publisher.Publish(event.KeyUpdated, "api_key", k.id)
	}
}

// OnDeptStudioChanged 部门→工作室映射变更后调用（SetDeptStudio / ImportMappings）
// deptSSOs: 受影响的 SSO dept_id 列表
func (s *KeyOrgSyncer) OnDeptStudioChanged(ctx context.Context, deptSSOs []string) {
	go func() {
		for _, deptSSO := range deptSSOs {
			rows, err := s.keyRepo.getUsersOrgByDeptSSO(deptSSO)
			if err != nil {
				slog.Warn("KeyOrgSyncer: getUsersOrgByDeptSSO failed", "dept_sso", deptSSO, "error", err)
				continue
			}
			synced := s.keyRepo.syncKeysForUsers(rows)
			s.publish(synced)
			slog.Info("KeyOrgSyncer: synced keys for dept", "dept_sso", deptSSO, "keys", len(synced))
		}
	}()
}

// OnUserDeptChanged 用户部门变更后调用（ChangeDepartment / FollowSSO / SSO 登录迁移）
func (s *KeyOrgSyncer) OnUserDeptChanged(ctx context.Context, userID string) {
	go func() {
		row, err := s.keyRepo.getUserOrgByUserID(userID)
		if err != nil || row.UserID == "" {
			slog.Warn("KeyOrgSyncer: getUserOrgByUserID failed", "user_id", userID, "error", err)
			return
		}
		synced := s.keyRepo.syncKeysForUsers([]userOrgRow{*row})
		s.publish(synced)
		slog.Info("KeyOrgSyncer: synced keys for user", "user_id", userID, "keys", len(synced))
	}()
}

// OnStudioDeleted 工作室删除后调用，清空该工作室下所有用户 key 的 studio 字段
func (s *KeyOrgSyncer) OnStudioDeleted(ctx context.Context, studioID string) {
	go func() {
		rows, err := s.keyRepo.getUsersOrgByStudioID(studioID)
		if err != nil {
			slog.Warn("KeyOrgSyncer: getUsersOrgByStudioID failed", "studio_id", studioID, "error", err)
			return
		}
		synced := s.keyRepo.syncKeysForUsers(rows)
		s.publish(synced)
		slog.Info("KeyOrgSyncer: synced keys after studio deleted", "studio_id", studioID, "keys", len(synced))
	}()
}
