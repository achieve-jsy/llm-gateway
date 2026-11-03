package repository_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// 软删的数据只能从"历史用量"里看到：组织树 / 工作室列表 / 清单类接口一律过滤。
func TestStudioAndDeptListHideSoftDeleted(t *testing.T) {
	db := openProjectionTestDB(t)
	studioRepo := repository.NewStudioRepo(db)
	deptRepo := repository.NewDeptRepo(db)

	visibleStudio := &model.Studio{ID: uuid.NewString(), Name: "在营工作室", Visible: true, Source: "ehr"}
	hiddenStudio := &model.Studio{ID: uuid.NewString(), Name: "已撤并工作室", Visible: true, Source: "ehr"}
	require.NoError(t, db.Create(visibleStudio).Error)
	require.NoError(t, db.Create(hiddenStudio).Error)
	// visible 列带 default true：GORM 建行时会跳过零值，false 必须单独写（生产代码同理）。
	require.NoError(t, db.Model(&model.Studio{}).Where("id = ?", hiddenStudio.ID).Update("visible", false).Error)

	require.NoError(t, db.Create(&model.OrgDepartment{
		ID: uuid.NewString(), DeptID: "d-active", DeptName: "在营部门", StudioID: &visibleStudio.ID,
		Status: model.DeptStatusActive,
	}).Error)
	require.NoError(t, db.Create(&model.OrgDepartment{
		ID: uuid.NewString(), DeptID: "d-deleted", DeptName: "已撤并部门", StudioID: &hiddenStudio.ID,
		Status: model.DeptStatusDeleted,
	}).Error)

	studios, total, err := studioRepo.List(1, 50)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "软删的工作室不计入总数")
	require.Len(t, studios, 1)
	require.Equal(t, "在营工作室", studios[0].Name)

	depts, deptTotal, err := deptRepo.List("", 1, 50)
	require.NoError(t, err)
	require.Equal(t, int64(1), deptTotal, "软删的部门不计入总数")
	require.Len(t, depts, 1)
	require.Equal(t, "d-active", depts[0].DeptID)

	// 显式查 deleted 时仍要能查到（排查问题用）
	deleted, _, err := deptRepo.List(string(model.DeptStatusDeleted), 1, 50)
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Equal(t, "d-deleted", deleted[0].DeptID)
}
