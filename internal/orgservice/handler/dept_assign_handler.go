package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
)

// DeptAssignHandler 成员部门归属修正
type DeptAssignHandler struct {
	userOrgRepo *repository.UserOrgRepo
	deptRepo    *repository.DeptRepo
	deptPolicy  policy.OrgPolicy
}

func NewDeptAssignHandler(
	userOrgRepo *repository.UserOrgRepo,
	deptRepo *repository.DeptRepo,
) *DeptAssignHandler {
	return &DeptAssignHandler{userOrgRepo: userOrgRepo, deptRepo: deptRepo, deptPolicy: policy.OrgPolicy{}}
}

// ChangeDepartment PUT /api/v1/org/users/:id/department
// Body: { "dept_id": "<departments.dept_id（varchar SSO ID）>" }
func (h *DeptAssignHandler) ChangeDepartment(c *gin.Context) {
	userID := c.Param("id")
	opID := c.GetString("user_id")

	var body struct {
		DeptID string `json:"dept_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errorResp("invalid_request", "缺少 dept_id", "bad_request"))
		return
	}

	actor := rbac.GetActor(c)

	dept, err := h.deptRepo.GetByDeptID(body.DeptID)
	if err != nil || dept == nil {
		c.JSON(http.StatusBadRequest, errorResp("dept_not_found", "目标部门不存在", "not_found"))
		return
	}
	targetStudioID := ""
	if dept.StudioID != nil {
		targetStudioID = *dept.StudioID
	}
	if targetStudioID == "" {
		c.JSON(http.StatusUnprocessableEntity, errorResp("dept_not_mapped",
			"目标部门尚未归属工作室，请先完成映射后再调整成员", "dept_not_mapped"))
		return
	}

	currentDeptID, _ := h.userOrgRepo.GetUserDeptIDByUserID(userID)
	currentStudioID := ""
	if currentDeptID != "" {
		currentStudioID, _ = h.userOrgRepo.GetStudioIDByDeptIDStr(currentDeptID)
	}

	if !h.deptPolicy.CanChangeMemberDept(actor, targetStudioID, currentStudioID) {
		c.JSON(http.StatusForbidden, errorResp("forbidden",
			"目标部门或成员当前工作室不在您的管辖范围内", "forbidden"))
		return
	}

	if err := h.userOrgRepo.UpdateUserDepartment(userID, dept.ID, opID); err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", "更新失败: "+err.Error(), "internal_error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":     "已手动修正部门归属",
		"dept_id":     body.DeptID,
		"dept_name":   dept.DeptName,
		"studio_id":   targetStudioID,
		"dept_source": "manual",
	})
}

// FollowSSO PUT /api/v1/org/users/:id/department/follow-sso
// 恢复跟随 SSO；Guard 已确保调用者至少具备 studio_admin 角色，无需在此重复判断
func (h *DeptAssignHandler) FollowSSO(c *gin.Context) {
	userID := c.Param("id")
	if err := h.userOrgRepo.FollowSSO(userID); err != nil {
		c.JSON(http.StatusInternalServerError, errorResp("internal_error", "更新失败: "+err.Error(), "internal_error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已恢复跟随 SSO，下次登录时将按 SSO 部门自动归集"})
}
