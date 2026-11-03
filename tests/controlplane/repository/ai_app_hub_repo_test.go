package repository_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAIHubRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE ai_app_studios (
		id text PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		department_id text UNIQUE,
		name text NOT NULL DEFAULT '',
		leader_name text NOT NULL DEFAULT '',
		leader_passport text NOT NULL DEFAULT '',
		leader_employee_id text NOT NULL DEFAULT '',
		source text NOT NULL DEFAULT 'ehr',
		visible boolean NOT NULL DEFAULT 1,
		sort_order integer NOT NULL DEFAULT 0,
		created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE ai_app_departments (
		department_id text PRIMARY KEY,
		name text NOT NULL DEFAULT '',
		parent_id text NOT NULL DEFAULT '',
		department_type integer NOT NULL DEFAULT 0,
		department_path text NOT NULL DEFAULT '',
		studio_id text,
		leader_name text NOT NULL DEFAULT '',
		leader_passport text NOT NULL DEFAULT '',
		synced_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error)
	return db
}

func TestInsertStudioIfAbsentDoesNotOverwrite(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	dept := "0B3CF753-9D6D-46E8-89C2-8E752D2A460F"

	inserted, err := repo.InsertStudioIfAbsent(context.Background(), &model.AIAppStudio{
		Name: "公共关系事业部", DepartmentID: &dept, Visible: false, Source: "ehr",
	})
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.InsertStudioIfAbsent(context.Background(), &model.AIAppStudio{
		Name: "改名了", DepartmentID: &dept, Visible: true, Source: "ehr",
	})
	require.NoError(t, err)
	require.False(t, inserted)

	got, err := repo.GetStudioByDepartmentID(context.Background(), dept)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "公共关系事业部", got.Name)
	require.False(t, got.Visible)
}

func TestUpdateStudioEHRFieldsLeavesVisibility(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	dept := "53E83585-6CE3-48EF-8255-520BBAFCB156"

	inserted, err := repo.InsertStudioIfAbsent(context.Background(), &model.AIAppStudio{
		Name: "战略发展中心", DepartmentID: &dept, Visible: false,
		LeaderPassport: "old", Source: "ehr",
	})
	require.NoError(t, err)
	require.True(t, inserted)

	before, err := repo.GetStudioByDepartmentID(context.Background(), dept)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateStudioEHRFields(context.Background(), before.ID,
		"技术研究工作室", "何治国", "hezhiguo", "emp-1"))

	after, err := repo.GetStudioByDepartmentID(context.Background(), dept)
	require.NoError(t, err)
	require.Equal(t, "技术研究工作室", after.Name)
	require.Equal(t, "何治国", after.LeaderName)
	require.Equal(t, "hezhiguo", after.LeaderPassport)
	require.Equal(t, "emp-1", after.LeaderEmployeeID)
	require.False(t, after.Visible, "可见性属运营字段，同步不得覆盖")
}

func TestHideStudiosNotInDepartmentsHidesOnlyVanishedEHRRows(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	keepDept, goneDept, manualDept := "keep-1", "gone-1", "gone-manual"
	for _, s := range []model.AIAppStudio{
		{DepartmentID: &keepDept, Name: "保留", Source: "ehr", Visible: true},
		{DepartmentID: &goneDept, Name: "撤销", Source: "ehr", Visible: true},
		{DepartmentID: &manualDept, Name: "手工", Source: "manual", Visible: true},
	} {
		_, err := repo.InsertStudioIfAbsent(ctx, &s)
		require.NoError(t, err)
	}

	n, err := repo.HideStudiosNotInDepartments(ctx, []string{keepDept})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "只隐藏 source=ehr 且不在 keep 里的行")

	kept, err := repo.GetStudioByDepartmentID(ctx, keepDept)
	require.NoError(t, err)
	require.True(t, kept.Visible)

	gone, err := repo.GetStudioByDepartmentID(ctx, goneDept)
	require.NoError(t, err)
	require.False(t, gone.Visible)

	manual, err := repo.GetStudioByDepartmentID(ctx, manualDept)
	require.NoError(t, err)
	require.True(t, manual.Visible, "manual 行永不被同步改动")
}

func TestHideStudiosNotInDepartmentsIgnoresEmptyDepartmentID(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	_, err := repo.InsertStudioIfAbsent(ctx, &model.AIAppStudio{Name: "手工工作室", Source: "ehr", Visible: true})
	require.NoError(t, err)

	n, err := repo.HideStudiosNotInDepartments(ctx, []string{"any"})
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "department_id IS NULL 的行不参与对账")
}
