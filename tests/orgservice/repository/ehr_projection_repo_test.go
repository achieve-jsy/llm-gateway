package repository_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 建表语句逐字对齐真实列（atlas-migrations/20261104090000_studios_ehr_identity.sql），
// 避免 sqlite 与生产 schema 漂移导致测试假绿。
func openProjectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE studios (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		owner_id TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ehr_department_id TEXT,
		visible BOOLEAN NOT NULL DEFAULT 1,
		source TEXT NOT NULL DEFAULT 'manual',
		leader_name TEXT NOT NULL DEFAULT '',
		leader_passport TEXT NOT NULL DEFAULT '',
		leader_employee_id TEXT NOT NULL DEFAULT ''
	)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX uniq_studios_ehr_dept ON studios (ehr_department_id)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE departments (
		id TEXT PRIMARY KEY,
		dept_id TEXT NOT NULL,
		dept_name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		studio_id TEXT,
		status TEXT NOT NULL DEFAULT 'pending_studio',
		owner_id TEXT
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE dept_studio_map (
		dept_id TEXT PRIMARY KEY,
		studio_id TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual',
		updated_by TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error)
	return db
}

func studioIn(ehrID, name string) repository.EHRStudioInput {
	return repository.EHRStudioInput{EHRDepartmentID: ehrID, Name: name, LeaderName: name + "-负责人"}
}

// 1) 首次投影 → inserted，visible=true / source=ehr
func TestProjectionStudioInsertThenStable(t *testing.T) {
	db := openProjectionTestDB(t)
	repo := repository.NewEHRProjectionRepo(db)
	ctx := context.Background()

	outcome, err := repo.UpsertStudio(ctx, studioIn("ehr-1", "战龙工作室"))
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionInserted, outcome)

	var s model.Studio
	require.NoError(t, db.Where("ehr_department_id = ?", "ehr-1").Take(&s).Error)
	require.Equal(t, "战龙工作室", s.Name)
	require.True(t, s.Visible)
	require.Equal(t, "ehr", s.Source)
	firstID := s.ID

	// 2) 同输入再跑 → unchanged（差量为 0 的保证）
	outcome, err = repo.UpsertStudio(ctx, studioIn("ehr-1", "战龙工作室"))
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUnchanged, outcome)

	// 3) 改名 → updated，且 id 不变（api_keys/call_logs 快照不能断）
	outcome, err = repo.UpsertStudio(ctx, studioIn("ehr-1", "战龙科技"))
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUpdated, outcome)
	require.NoError(t, db.Where("ehr_department_id = ?", "ehr-1").Take(&s).Error)
	require.Equal(t, "战龙科技", s.Name)
	require.Equal(t, firstID, s.ID, "改名不能换 id")
}

// 4) source=manual 的行同步不覆盖
func TestProjectionSkipsManualStudio(t *testing.T) {
	db := openProjectionTestDB(t)
	repo := repository.NewEHRProjectionRepo(db)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO studios (id, name, ehr_department_id, source) VALUES (?, ?, ?, 'manual')`,
		uuid.NewString(), "旧名字", "ehr-9").Error)

	outcome, err := repo.UpsertStudio(ctx, studioIn("ehr-9", "EHR 新名字"))
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUnchanged, outcome)

	var s model.Studio
	require.NoError(t, db.Where("ehr_department_id = ?", "ehr-9").Take(&s).Error)
	require.Equal(t, "旧名字", s.Name)
}

// 5) visible 单向：人工隐藏后同步只改 name，不复原 visible
func TestProjectionNeverRevealsHiddenStudio(t *testing.T) {
	db := openProjectionTestDB(t)
	repo := repository.NewEHRProjectionRepo(db)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO studios (id, name, ehr_department_id, source, visible) VALUES (?, ?, ?, 'ehr', 0)`,
		uuid.NewString(), "元气工作室", "ehr-2").Error)

	outcome, err := repo.UpsertStudio(ctx, studioIn("ehr-2", "元气工作室改名"))
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUpdated, outcome)

	var s model.Studio
	require.NoError(t, db.Where("ehr_department_id = ?", "ehr-2").Take(&s).Error)
	require.Equal(t, "元气工作室改名", s.Name)
	require.False(t, s.Visible, "同步不能把隐藏的工作室复活")
}

func TestProjectionDepartmentAndMapping(t *testing.T) {
	db := openProjectionTestDB(t)
	repo := repository.NewEHRProjectionRepo(db)
	ctx := context.Background()
	studioID := uuid.New()

	outcome, err := repo.UpsertDepartment(ctx, repository.EHRDepartmentInput{
		DeptID: "dept-1", DeptName: "技术部", StudioID: &studioID, Status: string(model.DeptStatusActive),
	})
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionInserted, outcome)

	outcome, err = repo.UpsertDepartment(ctx, repository.EHRDepartmentInput{
		DeptID: "dept-1", DeptName: "技术部改名", StudioID: &studioID, Status: string(model.DeptStatusActive),
	})
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUpdated, outcome)

	// status=deleted 的行被 EHR 重新看到 → 复活为 active
	require.NoError(t, db.Exec(`UPDATE departments SET status = 'deleted' WHERE dept_id = 'dept-1'`).Error)
	outcome, err = repo.UpsertDepartment(ctx, repository.EHRDepartmentInput{
		DeptID: "dept-1", DeptName: "技术部改名", StudioID: &studioID, Status: string(model.DeptStatusActive),
	})
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUpdated, outcome)

	var d model.OrgDepartment
	require.NoError(t, db.Where("dept_id = ?", "dept-1").Take(&d).Error)
	require.Equal(t, string(model.DeptStatusActive), string(d.Status))

	// 映射：新增 → inserted，同值 → unchanged，换工作室 → updated
	outcome, err = repo.UpsertMapping(ctx, "dept-1", studioID, "ehr-sync")
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionInserted, outcome)

	outcome, err = repo.UpsertMapping(ctx, "dept-1", studioID, "ehr-sync")
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUnchanged, outcome)

	outcome, err = repo.UpsertMapping(ctx, "dept-1", uuid.New(), "ehr-sync")
	require.NoError(t, err)
	require.Equal(t, repository.ProjectionUpdated, outcome)
}

func TestProjectionReconcile(t *testing.T) {
	db := openProjectionTestDB(t)
	repo := repository.NewEHRProjectionRepo(db)
	ctx := context.Background()

	_, err := repo.UpsertStudio(ctx, studioIn("ehr-a", "A 工作室"))
	require.NoError(t, err)
	_, err = repo.UpsertStudio(ctx, studioIn("ehr-b", "B 工作室"))
	require.NoError(t, err)
	manualID := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios (id, name, source) VALUES (?, '手工残留', 'manual')`, manualID).Error)
	_, err = repo.UpsertDepartment(ctx, repository.EHRDepartmentInput{DeptID: "d-a", DeptName: "部门A", Status: string(model.DeptStatusPendingStudio)})
	require.NoError(t, err)

	// 只有 ehr-a 还在；ehr-b 隐藏，manual 行不受影响
	hidden, err := repo.HideStudiosNotIn(ctx, []string{"ehr-a"})
	require.NoError(t, err)
	require.Equal(t, 1, hidden)

	var visible int64
	require.NoError(t, db.Model(&model.Studio{}).Where("visible = ?", true).Count(&visible).Error)
	require.Equal(t, int64(2), visible, "A 工作室 + manual 残留 可见，B 被隐藏")

	// 部门对账：d-a 还在，d-b 标 deleted 但行保留
	_, err = repo.UpsertDepartment(ctx, repository.EHRDepartmentInput{DeptID: "d-b", DeptName: "部门B", Status: string(model.DeptStatusActive)})
	require.NoError(t, err)
	archived, err := repo.ArchiveDepartmentsNotIn(ctx, []string{"d-a"})
	require.NoError(t, err)
	require.Equal(t, 1, archived)

	var d model.OrgDepartment
	require.NoError(t, db.Where("dept_id = ?", "d-b").Take(&d).Error)
	require.Equal(t, string(model.DeptStatusDeleted), string(d.Status), "软删：行还在，只是状态变了")

	// keep 为空（EHR 异常返回空）→ 一条都不动
	hidden, err = repo.HideStudiosNotIn(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 0, hidden)
	archived, err = repo.ArchiveDepartmentsNotIn(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 0, archived)
}
