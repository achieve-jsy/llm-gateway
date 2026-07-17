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

func (h *KeyRequestHandler) actorName(c *gin.Context, userID uuid.UUID) string {
	if name := c.GetString("user_name"); name != "" {
		return name
	}
	return h.userDisplayName(userID)
}

func (h *KeyRequestHandler) userDisplayName(userID uuid.UUID) string {
	if userID == uuid.Nil {
		return ""
	}
	if u, err := h.userRepo.FindByID(userID); err == nil && u != nil && u.Name != "" {
		return u.Name
	}
	return ""
}

// enrichApproverNames 列表回显：approver_name 为空时按 approver_id 查用户表补齐。
func (h *KeyRequestHandler) enrichApproverNames(items []model.KeyRequest) {
	if len(items) == 0 {
		return
	}
	need := make([]uuid.UUID, 0)
	seen := make(map[uuid.UUID]struct{})
	for i := range items {
		if items[i].ApproverName != "" || items[i].ApproverID == nil {
			continue
		}
		id := *items[i].ApproverID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		need = append(need, id)
	}
	if len(need) == 0 {
		return
	}
	users, err := h.userRepo.FindByIDs(need)
	if err != nil {
		return
	}
	for i := range items {
		if items[i].ApproverName != "" || items[i].ApproverID == nil {
			continue
		}
		if u := users[*items[i].ApproverID]; u != nil && u.Name != "" {
			items[i].ApproverName = u.Name
		}
	}
}

// CheckPolicy 查询当前用户是否需要走申请；负责人豁免时 requires_approval=false，policy 仍反映部门策略。
func (h *KeyRequestHandler) CheckPolicy(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	deptID := c.Query("dept_id")
	if deptID == "" {
		// 优先从 JWT claim 中取 SSO dept_id（string 格式，与 dept_policies.dept_id 一致）
		deptID = c.GetString("dept_id")
	}
	deptRequiresApproval, err := h.svc.RequiresApproval(c.Request.Context(), deptID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	requiresApproval := deptRequiresApproval && !actor.IsManager()
	policy := model.KeyCreationPolicyFree
	if deptRequiresApproval {
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
	// cancel 允许申请人本人操作；approve/reject 仅负责人（cancel 推荐走 /cancel 接口）
	if req.Action != model.ExpansionActionCancel && rbac.RejectScopeUnavailable(c, actor) {
		return
	}
	if req.Action != model.ExpansionActionCancel && !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	requestID := c.Param("id")
	approverID, _ := uuid.Parse(c.GetString("user_id"))
	approverName := h.actorName(c, approverID)

	if err := h.svc.Decide(c.Request.Context(), requestID, approverID, approverName, approverID, req.Action, req.Comment, actor.IsGlobal, actor.ApprovalDeptIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// CancelMine POST /api/v1/key-requests/:id/cancel
// 申请人取消自己的待审批 Key 申请（无需负责人权限）。
func (h *KeyRequestHandler) CancelMine(c *gin.Context) {
	requestID := c.Param("id")
	userID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	if err := h.svc.CancelMine(c.Request.Context(), requestID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListMine GET /api/v1/key-requests?status=pending&page=1&page_size=20
func (h *KeyRequestHandler) ListMine(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := h.svc.ListMine(c.Request.Context(), userID, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []model.KeyRequest{}
	}
	h.enrichApproverNames(items)
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ListPending GET /api/v1/key-requests/pending
// 审批人查询待审批的申请（按管辖部门过滤）。
func (h *KeyRequestHandler) ListPending(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if rbac.RejectScopeUnavailable(c, actor) {
		return
	}
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	requesterName := c.Query("name")
	hasBudget := c.Query("has_budget") == "true"
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := h.svc.ListPending(c.Request.Context(), actor.IsGlobal, actor.ApprovalDeptIDs, requesterName, hasBudget, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []model.KeyRequest{}
	}
	h.enrichApproverNames(items)
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
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
	if rbac.RejectScopeUnavailable(c, actor) {
		return
	}
	if !actor.IsManager() {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"count": 0, "budget_count": 0}})
		return
	}
	count, budgetCount, err := h.svc.CountPendingForDept(c.Request.Context(), actor.IsGlobal, actor.ApprovalDeptIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"count":        count,
		"budget_count": budgetCount,
	}})
}

// ListHistory GET /api/v1/key-requests/history
// 审批人查询已处理的申请记录（approved/rejected）。
func (h *KeyRequestHandler) ListHistory(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if rbac.RejectScopeUnavailable(c, actor) {
		return
	}
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
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	// limit 兼容旧客户端：未传 page_size 时沿用 limit
	if c.Query("page_size") == "" && c.Query("limit") != "" {
		pageSize = limit
	}
	requesterName := c.Query("name")
	status := c.Query("status")
	if status != "" && status != model.KeyRequestStatusApproved && status != model.KeyRequestStatusRejected {
		status = ""
	}
	items, total, err := h.svc.ListHistoryForApprover(c.Request.Context(), actor.IsGlobal, actor.ApprovalDeptIDs, requesterName, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []model.KeyRequest{}
	}
	h.enrichApproverNames(items)
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetDeptPolicy GET /api/v1/dept-policies/:dept_id/key-creation
func (h *KeyRequestHandler) GetDeptPolicy(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if rbac.RejectScopeUnavailable(c, actor) {
		return
	}
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}
	deptID := c.Param("dept_id")
	if !actor.HasDirectDept(deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门策略"})
		return
	}
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
	if rbac.RejectScopeUnavailable(c, actor) {
		return
	}
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	deptID := c.Param("dept_id")
	if !actor.HasDirectDept(deptID) {
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
	result, err := h.svc.SetDeptPolicy(c.Request.Context(), deptID, in.Policy, updatedBy, h.actorName(c, updatedBy))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
