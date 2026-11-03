package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type KeyServiceInterface interface {
	VerifyKey(ctx context.Context, key string) (*service.VerifyResult, error)
	CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time, groupIDs []uint) (*model.ApiKey, string, []service.BoundGroupItem, error)
	ListKeys(ctx context.Context, userID uuid.UUID, page, size int) ([]service.KeyItem, int64, error)
	GetKey(ctx context.Context, userID, keyID uuid.UUID) (*service.KeyDetail, []service.BoundGroupItem, error)
	UpdateKeyStatus(ctx context.Context, userID, keyID uuid.UUID, enabled bool) (*model.ApiKey, error)
	DeleteKey(ctx context.Context, userID, keyID uuid.UUID) error
	ResetKey(ctx context.Context, userID, keyID uuid.UUID) (*model.ApiKey, string, error)
	RevealKey(ctx context.Context, userID, keyID uuid.UUID) (string, error)
	ListKeyGroups(ctx context.Context, userID, keyID uuid.UUID) ([]service.BoundGroupItem, error)
	UpdateKeyGroups(ctx context.Context, userID, keyID uuid.UUID, add, remove []uint) ([]service.BoundGroupItem, error)
	ListAvailableGroups(ctx context.Context) ([]service.AvailableGroupItem, error)
	UpdateAccessAllGroups(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error)
	UpdateLogEnabled(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error)
}

// KeyCreationPolicyChecker 提供部门 Key 创建审批策略。
// 直建 Key 的授权必须在服务端执行，前端的策略查询只用于引导交互。
type KeyCreationPolicyChecker interface {
	RequiresApproval(ctx context.Context, deptID string) (bool, error)
}

type CreateKeyResponse struct {
	ID           uuid.UUID               `json:"id"`
	Name         string                  `json:"name"`
	Key          string                  `json:"key"`
	KeyHint      string                  `json:"key_hint"`
	Enabled      bool                    `json:"enabled"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	ExpiresAt    *time.Time              `json:"expires_at,omitempty"`
	BoundGroups  []BoundGroupItem        `json:"bound_groups,omitempty"`
}

type BoundGroupItem struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type KeyListItem struct {
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	MaskedKey       string           `json:"masked_key"`
	KeyHint         string           `json:"key_hint"`
	Enabled         bool             `json:"enabled"`
	AccessAllGroups bool             `json:"access_all_groups"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty"`
	BoundGroups     []BoundGroupItem `json:"bound_groups,omitempty"`
}

type KeyDetailResponse struct {
	ID          uuid.UUID        `json:"id"`
	Name        string           `json:"name"`
	MaskedKey   string           `json:"masked_key"`
	KeyHint     string           `json:"key_hint"`
	Enabled     bool             `json:"enabled"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	BoundGroups []BoundGroupItem `json:"bound_groups,omitempty"`
}

type KeyHandler struct {
	svc           KeyServiceInterface
	policyChecker KeyCreationPolicyChecker
	rbac          *rbac.Checker
}

func NewKeyHandler(svc KeyServiceInterface, policyChecker KeyCreationPolicyChecker, rbacChecker *rbac.Checker) *KeyHandler {
	return &KeyHandler{svc: svc, policyChecker: policyChecker, rbac: rbacChecker}
}

func (h *KeyHandler) CreateKey(c *gin.Context) {
	if h.rbac != nil {
		h.rbac.BuildAndInjectActor(c)
		actor := rbac.GetActor(c)
		if rbac.RejectScopeUnavailable(c, actor) {
			return
		}
		if h.policyChecker != nil {
			requiresApproval, err := h.policyChecker.RequiresApproval(c.Request.Context(), c.GetString("dept_id"))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"type":    "internal_error",
						"message": "failed to check key creation policy",
						"code":    CodeInternalError,
					},
				})
				return
			}
			if requiresApproval && !actor.IsManager() {
				c.JSON(http.StatusForbidden, gin.H{
					"error": gin.H{
						"type":    "forbidden",
						"message": "当前部门创建 Key 需要审批，请通过 Key 申请提交",
						"code":    "key_creation_requires_approval",
					},
				})
				return
			}
		}
	}

	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	var input struct {
		Name      string     `json:"name" binding:"required,max=128"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		GroupIDs  []uint     `json:"group_ids,omitempty"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	k, plaintext, boundGroups, err := h.svc.CreateKey(c.Request.Context(), userID, input.Name, input.ExpiresAt, input.GroupIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": err.Error(),
				"code":    CodeInternalError,
			},
		})
		return
	}

	bgItems := convertBoundGroups(boundGroups)
	c.JSON(http.StatusCreated, gin.H{"data": CreateKeyResponse{
		ID:          k.ID,
		Name:        k.Name,
		Key:         plaintext,
		KeyHint:     k.KeyHint,
		Enabled:     k.Enabled,
		CreatedAt:   k.CreatedAt,
		UpdatedAt:   k.UpdatedAt,
		ExpiresAt:   k.ExpiresAt,
		BoundGroups: bgItems,
	}})
}

func (h *KeyHandler) ListKeys(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	keys, total, err := h.svc.ListKeys(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to list keys",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"items":     keys,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
		"request_id": c.GetString("request_id"),
	})
}

// MyKeys GET /api/v1/my/keys — 当前登录用户的 Key 列表（轻量，仅返回 id/name）
func (h *KeyHandler) MyKeys(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "unauthorized", "message": "not authenticated"}})
		return
	}
	keys, _, err := h.svc.ListKeys(c.Request.Context(), userID, 1, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "internal_error", "message": "failed to list keys"}})
		return
	}
	type item struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		KeyHint string `json:"key_hint"`
		Enabled bool   `json:"enabled"`
	}
	items := make([]item, 0, len(keys))
	for _, k := range keys {
		items = append(items, item{ID: k.ID.String(), Name: k.Name, KeyHint: k.KeyHint, Enabled: k.Enabled})
	}
	c.JSON(http.StatusOK, gin.H{"keys": items})
}

func (h *KeyHandler) GetKey(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid key id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	key, boundGroups, err := h.svc.GetKey(c.Request.Context(), userID, keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found",
				"message": "key not found",
				"code":    CodeKeyNotFound,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": KeyDetailResponse{
		ID:          key.ID,
		Name:        key.Name,
		MaskedKey:   key.MaskedKey,
		KeyHint:     key.KeyHint,
		Enabled:     key.Enabled,
		CreatedAt:   key.CreatedAt,
		UpdatedAt:   key.UpdatedAt,
		BoundGroups: convertBoundGroups(boundGroups),
	}})
}

func (h *KeyHandler) DisableKey(c *gin.Context) {
	h.toggleKeyStatus(c, false)
}

func (h *KeyHandler) EnableKey(c *gin.Context) {
	h.toggleKeyStatus(c, true)
}

func (h *KeyHandler) toggleKeyStatus(c *gin.Context, enabled bool) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid key id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	k, err := h.svc.UpdateKeyStatus(c.Request.Context(), userID, keyID, enabled)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found",
				"message": "key not found",
				"code":    CodeKeyNotFound,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": k})
}

func (h *KeyHandler) DeleteKey(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid key id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	if err := h.svc.DeleteKey(c.Request.Context(), userID, keyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found",
				"message": "key not found",
				"code":    CodeKeyNotFound,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "key deleted"})
}

func (h *KeyHandler) ResetKey(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid user_id","code":CodeBadRequest}})
		return
	}
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid key id","code":CodeBadRequest}})
		return
	}
	k, plaintext, err := h.svc.ResetKey(c.Request.Context(), userID, keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type":"not_found","message":"key not found","code":CodeKeyNotFound}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": CreateKeyResponse{
		ID: k.ID, Name: k.Name, Key: plaintext, KeyHint: k.KeyHint, Enabled: k.Enabled, CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt, ExpiresAt: k.ExpiresAt,
	}})
}

func (h *KeyHandler) RevealKey(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid user_id in context",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid key id", "code": CodeBadRequest}})
		return
	}

	plaintext, err := h.svc.RevealKey(c.Request.Context(), userID, keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found", "message": "key not found", "code": CodeKeyNotFound}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"key": plaintext}})
}

func (h *KeyHandler) VerifyKey(c *gin.Context) {
	var input struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	result, err := h.svc.VerifyKey(c.Request.Context(), input.Key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found",
				"message": "key not found",
				"code":    CodeKeyNotFound,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *KeyHandler) ListKeyGroups(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid user_id","code":CodeBadRequest}})
		return
	}
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid key id","code":CodeBadRequest}})
		return
	}
	groups, err := h.svc.ListKeyGroups(c.Request.Context(), userID, keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type":"not_found","message":"key not found","code":CodeKeyNotFound}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"key_id": keyID, "bound_groups": groups}})
}

func (h *KeyHandler) UpdateKeyGroups(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid user_id","code":CodeBadRequest}})
		return
	}
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"invalid key id","code":CodeBadRequest}})
		return
	}
	var input struct {
		Add    []uint `json:"add,omitempty"`
		Remove []uint `json:"remove,omitempty"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":err.Error(),"code":CodeBadRequest}})
		return
	}
	if len(input.Add) == 0 && len(input.Remove) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":"add or remove is required","code":CodeBadRequest}})
		return
	}
	groups, err := h.svc.UpdateKeyGroups(c.Request.Context(), userID, keyID, input.Add, input.Remove)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type":"invalid_request","message":err.Error(),"code":CodeBadRequest}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"key_id": keyID, "bound_groups": groups}})
}

func (h *KeyHandler) ListAvailableGroups(c *gin.Context) {
	groups, err := h.svc.ListAvailableGroups(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type":"internal_error","message":err.Error(),"code":CodeInternalError}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"groups": groups}})
}

func convertBoundGroups(items []service.BoundGroupItem) []BoundGroupItem {
	result := make([]BoundGroupItem, 0, len(items))
	for _, item := range items {
		result = append(result, BoundGroupItem{
			ID:   item.ID,
			Name: item.Name,
		})
	}
	return result
}

// ToggleAccessAllGroups POST /api/v1/keys/:id/access-all-groups
// ToggleLogEnabled PATCH /api/v1/keys/:id/log-enabled
func (h *KeyHandler) ToggleLogEnabled(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid user_id", "code": CodeBadRequest}})
		return
	}
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid key id", "code": CodeBadRequest}})
		return
	}
	var input struct {
		LogEnabled bool `json:"log_enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest}})
		return
	}
	k, err := h.svc.UpdateLogEnabled(c.Request.Context(), userID, keyID, input.LogEnabled)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found", "message": "key not found", "code": CodeKeyNotFound}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": k.ID, "log_enabled": k.LogEnabled}})
}

func (h *KeyHandler) ToggleAccessAllGroups(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid user_id", "code": CodeBadRequest}})
		return
	}
	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid key id", "code": CodeBadRequest}})
		return
	}
	var input struct {
		AccessAllGroups bool `json:"access_all_groups"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest}})
		return
	}
	k, err := h.svc.UpdateAccessAllGroups(c.Request.Context(), userID, keyID, input.AccessAllGroups)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found", "message": "key not found", "code": CodeKeyNotFound}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": k.ID, "access_all_groups": k.AccessAllGroups}})
}
