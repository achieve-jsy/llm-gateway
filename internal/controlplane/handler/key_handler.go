package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type KeyServiceInterface interface {
	VerifyKey(ctx context.Context, key string) (*service.VerifyResult, error)
	CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*model.ApiKey, string, error)
	ListKeys(ctx context.Context, userID uuid.UUID, page, size int) ([]service.KeyItem, int64, error)
	GetKey(ctx context.Context, userID, keyID uuid.UUID) (*service.KeyDetail, error)
	UpdateKeyStatus(ctx context.Context, userID, keyID uuid.UUID, enabled bool) (*model.ApiKey, error)
	DeleteKey(ctx context.Context, userID, keyID uuid.UUID) error
	ResetKey(ctx context.Context, userID, keyID uuid.UUID) (*model.ApiKey, string, error)
}

type CreateKeyResponse struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Key       string     `json:"key"`
	KeyHint   string     `json:"key_hint"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type KeyHandler struct {
	svc KeyServiceInterface
}

func NewKeyHandler(svc KeyServiceInterface) *KeyHandler {
	return &KeyHandler{svc: svc}
}

func (h *KeyHandler) CreateKey(c *gin.Context) {
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
		Name      string  `json:"name" binding:"required,max=128"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
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

	k, plaintext, err := h.svc.CreateKey(c.Request.Context(), userID, input.Name, input.ExpiresAt)
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

	c.JSON(http.StatusCreated, gin.H{"data": CreateKeyResponse{
		ID:        k.ID,
		Name:      k.Name,
		Key:       plaintext,
		KeyHint:   k.KeyHint,
		Enabled:   k.Enabled,
		CreatedAt: k.CreatedAt,
		UpdatedAt: k.UpdatedAt,
		ExpiresAt: k.ExpiresAt,
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

	key, err := h.svc.GetKey(c.Request.Context(), userID, keyID)
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

	c.JSON(http.StatusOK, gin.H{"data": key})
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
