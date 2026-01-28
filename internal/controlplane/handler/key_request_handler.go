package handler

import (
	"net/http"
	"strconv"
	"time"

	authrepo "github.com/example/llm-gateway/internal/auth/repository"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func parseExpiresAtOptional(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}

// KeyRequestHandler Key 创建申请 + 审批的 API 入口。
type KeyRequestHandler struct {
	svc      *service.KeyRequestService
	rbac     *rbac.Checker
	userRepo *authrepo.UserRepository
}

func NewKeyRequestHandler(
	svc *service.KeyRequestService,
	rbacChecker *rbac.Checker,
	userRepo *authrepo.UserRepository,
) *KeyRequestHandler {
	return &KeyRequestHandler{svc: svc, rbac: rbacChecker, userRepo: userRepo}
}

// CheckPolicy GET /api/v1/key-requests/policy?dept_id=xxx
// 查询当前用户的 Key 创建策略（free/approval_required）。
// 不传 dept_id 则使用 JWT claim 中的 dept_id（与 dept_policies.dept_id 同为 SSO varchar 格式）。
func (h *KeyRequestHandler) CheckPolicy(c *gin.Context) {
	deptID := c.Query("dept_id")
	if deptID == "" {
		// 优先从 JWT claim 中取 SSO dept_id（string 格式，与 dept_policies.dept_id 一致）
		deptID = c.GetString("dept_id")
	}
	requiresApproval, err := h.svc.RequiresApproval(c.Request.Context(), deptID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	policy := model.KeyCreationPolicyFree
	if requiresApproval {
		policy = model.KeyCreationPolicyApproval
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"requires_approval": requiresApproval,
			"policy":            policy,
		},
	})
}

// Create POST /api/v1/key-requests
// 发起 Key 创建申请（可附带预算）。
func (h *KeyRequestHandler) Create(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)

	userID, _ := uuid.Parse(c.GetString("user_id"))

	// 直接从 JWT claims 取 SSO dept_id / dept_name（与 dept_policies.dept_id 格式一致）
	deptID := c.GetString("dept_id")
	deptName := c.GetString("dept_name")

	// 仍需要 user.Name 用于冗余
	userName := c.GetString("user_name")
	if userName == "" {
		if u, err := h.userRepo.FindByID(userID); err == nil && u != nil {
			userName = u.Name
		}
	}

	var in struct {
		KeyName       string  `json:"key_name" binding:"required,max=128"`
		Purpose       string  `json:"purpose"`
		GroupIDs      []uint  `json:"group_ids"`
		ExpiresAt     *string `json:"expires_at"`
		BudgetMonthly float64 `json:"budget_monthly"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req, err := h.svc.CreateRequest(
		c.Request.Context(),
		userID, userName, deptID, deptName,
		in.KeyName, in.Purpose, in.GroupIDs,
		parseExpiresAtOptional(in.ExpiresAt),
		in.BudgetMonthly,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": req})
}

// Decide POST /api/v1/key-requests/:id/decision
// 审批（approve/reject/cancel）。
func (h *KeyRequestHandler) Decide(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		Action  string `json:"action" binding:"required"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	requestID := c.Param("id")
	approverID, _ := uuid.Parse(c.GetString("user_id"))

	if err := h.svc.Decide(c.Request.Context(), requestID, approverID, approverID, req.Action, req.Comment, actor.IsGlobal, actor.DeptIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListMine GET /api/v1/key-requests?status=pending
func (h *KeyRequestHandler) ListMine(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")
	items, err := h.svc.ListMine(c.Request.Context(), userID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListPending GET /api/v1/key-requests/pending
// 审批人查询待审批的申请（按管辖部门过滤）。
func (h *KeyRequestHandler) ListPending(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	requesterName := c.Query("name")
	items, err := h.svc.ListPending(c.Request.Context(), actor.IsGlobal, actor.DeptIDs, requesterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CountMine GET /api/v1/key-requests/summary
// 当前用户的申请数量统计。
func (h *KeyRequestHandler) CountMine(c *gin.Context) {
	userID := c.GetString("user_id")
	pending, approved, rejected, err := h.svc.CountMine(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"pending":  pending,
			"approved": approved,
			"rejected": rejected,
		},
	})
}

// CountPendingForDept GET /api/v1/key-requests/pending/count
// 部门 pending 数量（审批人用）。
func (h *KeyRequestHandler) CountPendingForDept(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"count": 0}})
		return
	}
	count, _ := h.svc.CountPendingForDept(c.Request.Context(), actor.IsGlobal, actor.DeptIDs)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"count": count}})
}

// ListHistory GET /api/v1/key-requests/history
// 审批人查询已处理的申请记录（approved/rejected）。
func (h *KeyRequestHandler) ListHistory(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	items, err := h.svc.ListHistoryForApprover(c.Request.Context(), actor.IsGlobal, actor.DeptIDs, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": items}})
}

// GetDeptPolicy GET /api/v1/dept-policies/:dept_id/key-creation
func (h *KeyRequestHandler) GetDeptPolicy(c *gin.Context) {
	deptID := c.Param("dept_id")
	val, err := h.svc.GetDeptPolicy(c.Request.Context(), deptID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if val == "" {
		val = model.KeyCreationPolicyFree
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"dept_id": deptID, "key_creation_policy": val}})
}

// SetDeptPolicy PUT /api/v1/dept-policies/:dept_id/key-creation
func (h *KeyRequestHandler) SetDeptPolicy(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	deptID := c.Param("dept_id")
	if !actor.IsGlobal && !actor.HasDept(deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改该部门策略"})
		return
	}

	var in struct {
		Policy string `json:"policy" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedBy, _ := uuid.Parse(c.GetString("user_id"))
	autoApproved, err := h.svc.SetDeptPolicy(c.Request.Context(), deptID, in.Policy, updatedBy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "auto_approved": autoApproved})
}
