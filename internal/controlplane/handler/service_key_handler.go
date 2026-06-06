package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ServiceKeyHandler 处理程序服务 Key（Service Key）的创建、列表、吊销、IP 白名单管理。
type ServiceKeyHandler struct {
	keySvc   *service.KeyService
	recorder audit.Recorder
	rbac     *rbac.Checker
}

func NewServiceKeyHandler(keySvc *service.KeyService, rbacChecker *rbac.Checker) *ServiceKeyHandler {
	return &ServiceKeyHandler{keySvc: keySvc, rbac: rbacChecker}
}

// SetAuditRecorder 注入审计记录器（可传 audit.Nop{} 表示禁用审计）
func (h *ServiceKeyHandler) SetAuditRecorder(r audit.Recorder) {
	h.recorder = audit.Wrap(r)
}

func (h *ServiceKeyHandler) getClientIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	return c.ClientIP()
}

// validateAndNormalizeIPAllowlist 校验并规范化 IP 白名单（空/`"[]"`=不限制）
func validateAndNormalizeIPAllowlist(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "[]" {
		return "", nil
	}
	var list []string
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", nil
	}
	normalized := make([]string, 0, len(list))
	for _, raw := range list {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if net.ParseIP(entry) == nil {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return "", &invalidIPError{entry: entry}
			}
		}
		normalized = append(normalized, entry)
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type invalidIPError struct{ entry string }

func (e *invalidIPError) Error() string { return "invalid ip/cidr entry: " + e.entry }

// Create POST /api/v1/service-keys
func (h *ServiceKeyHandler) Create(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		DeptID          string `json:"dept_id" binding:"required"`
		Name            string `json:"name" binding:"required"`
		Purpose         string `json:"purpose"`
		IPAllowlist     string `json:"ip_allowlist"`
		GroupIDs        []uint `json:"group_ids,omitempty"`
		AccessAllGroups bool   `json:"access_all_groups,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(req.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权为该部门创建 Service Key"})
		return
	}

	normalized, err := validateAndNormalizeIPAllowlist(req.IPAllowlist)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	k, plainKey, boundGroups, err := h.keySvc.CreateServiceKey(c.Request.Context(), actorID, req.DeptID, req.Name, req.Purpose, normalized, req.GroupIDs, req.AccessAllGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, audit.Input{
		UserID:       actorID,
		UserName:     c.GetString("user_name"),
		Action:       audit.ActionServiceKeyCreate,
		ResourceType: audit.ResourceServiceKey,
		ResourceID:   k.ID.String(),
		ResourceName: k.Name,
		DeptID:       k.DeptID,
		DeptName:     k.DeptName,
		ClientIP:     h.getClientIP(c),
		UserAgent:    c.GetHeader("User-Agent"),
		Details: map[string]any{
			"purpose":      k.Purpose,
			"masked_key":   k.KeyPrefix + "****" + k.KeyHint,
			"ip_allowlist": normalized,
		},
	})

	c.JSON(http.StatusCreated, gin.H{
		"id":                k.ID,
		"key":               plainKey,
		"key_type":          k.KeyType,
		"masked_key":        k.KeyPrefix + "****" + k.KeyHint,
		"dept_id":           k.DeptID,
		"dept_name":         k.DeptName,
		"purpose":           k.Purpose,
		"ip_allowlist":      k.IPAllowlist,
		"access_all_groups": k.AccessAllGroups,
		"bound_groups":      boundGroups,
		"created_by":        k.CreatedBy,
	})
}

// List GET /api/v1/service-keys?dept_id=xxx
func (h *ServiceKeyHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	deptID := c.Query("dept_id")
	keyword := c.Query("keyword")

	var allowedDepts []string
	if !actor.IsGlobal {
		allowedDepts = actor.DeptIDs
		if len(allowedDepts) == 0 {
			c.JSON(http.StatusOK, gin.H{"items": []struct{}{}, "total": 0, "page": 1, "page_size": 20})
			return
		}
		if deptID != "" {
			found := false
			for _, d := range allowedDepts {
				if d == deptID {
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门的 Service Key"})
				return
			}
		}
	}

	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	items, total, err := h.keySvc.ListServiceKeysPaged(c.Request.Context(), deptID, allowedDepts, page, pageSize, keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// Toggle PATCH /api/v1/service-keys/:id/status
func (h *ServiceKeyHandler) Toggle(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该 Service Key"})
		return
	}

	if err := h.keySvc.ToggleServiceKey(c.Request.Context(), keyID, req.Enabled); err != nil {
		if err == service.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	h.recordAudit(c, audit.Input{
		UserID:       actorID,
		UserName:     c.GetString("user_name"),
		Action:       audit.ActionServiceKeyToggle,
		ResourceType: audit.ResourceServiceKey,
		ResourceID:   k.ID.String(),
		ResourceName: k.Name,
		DeptID:       k.DeptID,
		DeptName:     k.DeptName,
		ClientIP:     h.getClientIP(c),
		UserAgent:    c.GetHeader("User-Agent"),
		Details:      map[string]any{"enabled": req.Enabled},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": req.Enabled})
}

// UpdateIPAllowlist PATCH /api/v1/service-keys/:id/ip-allowlist
func (h *ServiceKeyHandler) UpdateIPAllowlist(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	var req struct {
		IPAllowlist any `json:"ip_allowlist"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var raw string
	switch v := req.IPAllowlist.(type) {
	case string:
		raw = v
	case []any:
		list := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "ip_allowlist array must contain only strings"})
				return
			} else {
				list = append(list, s)
			}
		}
		b, _ := json.Marshal(list)
		raw = string(b)
	case nil:
		raw = ""
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip_allowlist must be string or array"})
		return
	}

	normalized, err := validateAndNormalizeIPAllowlist(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该 Service Key"})
		return
	}

	oldAllowlist := k.IPAllowlist
	if err := h.keySvc.UpdateIPAllowlist(c.Request.Context(), keyID, normalized); err != nil {
		if err == service.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	h.recordAudit(c, audit.Input{
		UserID:       actorID,
		UserName:     c.GetString("user_name"),
		Action:       audit.ActionServiceKeyIPChanged,
		ResourceType: audit.ResourceServiceKey,
		ResourceID:   k.ID.String(),
		ResourceName: k.Name,
		DeptID:       k.DeptID,
		DeptName:     k.DeptName,
		ClientIP:     h.getClientIP(c),
		UserAgent:    c.GetHeader("User-Agent"),
		Details: map[string]any{
			"old_ip_allowlist": oldAllowlist,
			"new_ip_allowlist": normalized,
		},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true, "ip_allowlist": normalized})
}

// Delete DELETE /api/v1/service-keys/:id
func (h *ServiceKeyHandler) Delete(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权吊销该 Service Key"})
		return
	}

	if err := h.keySvc.DeleteServiceKey(c.Request.Context(), keyID); err != nil {
		if err == service.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	h.recordAudit(c, audit.Input{
		UserID:       actorID,
		UserName:     c.GetString("user_name"),
		Action:       audit.ActionServiceKeyDelete,
		ResourceType: audit.ResourceServiceKey,
		ResourceID:   k.ID.String(),
		ResourceName: k.Name,
		DeptID:       k.DeptID,
		DeptName:     k.DeptName,
		ClientIP:     h.getClientIP(c),
		UserAgent:    c.GetHeader("User-Agent"),
		Details: map[string]any{
			"masked_key": k.KeyPrefix + "****" + k.KeyHint,
			"purpose":    k.Purpose,
		},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ServiceKeyHandler) assertServiceKeyAccess(c *gin.Context, keyID uuid.UUID) error {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	k, findErr := h.keySvc.FindServiceKey(c.Request.Context(), keyID)
	if findErr != nil {
		return findErr
	}
	if !actor.IsGlobal && !actor.HasDept(k.DeptID) {
		return service.ErrKeyNotFound
	}
	return nil
}

// UpdateGroups POST /api/v1/service-keys/:id/groups
func (h *ServiceKeyHandler) UpdateGroups(c *gin.Context) {
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	if err := h.assertServiceKeyAccess(c, keyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}

	var req struct {
		Add    []uint `json:"add,omitempty"`
		Remove []uint `json:"remove,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Add) == 0 && len(req.Remove) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "add or remove is required"})
		return
	}

	groups, err := h.keySvc.UpdateServiceKeyGroups(c.Request.Context(), keyID, req.Add, req.Remove)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"key_id": keyID, "bound_groups": groups}})
}

// ToggleAccessAllGroups POST /api/v1/service-keys/:id/access-all-groups
func (h *ServiceKeyHandler) ToggleAccessAllGroups(c *gin.Context) {
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	if err := h.assertServiceKeyAccess(c, keyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}

	var req struct {
		AccessAllGroups bool `json:"access_all_groups"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	k, err := h.keySvc.UpdateServiceKeyAccessAllGroups(c.Request.Context(), keyID, req.AccessAllGroups)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service key not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": k.ID, "access_all_groups": k.AccessAllGroups}})
}

// recordAudit 内部辅助：通过接口写审计，自动标记跳过全局中间件重复记录
func (h *ServiceKeyHandler) recordAudit(c *gin.Context, in audit.Input) {
	if h.recorder != nil {
		h.recorder.Record(in)
	}
	audit.MarkRecorded(c)
}
