package handler

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type GroupServiceInterface interface {
	Create(req service.CreateGroupRequest) (*model.Group, error)
	List(search string, page, pageSize int) ([]model.Group, int64, error)
	GetByID(id uint) (*model.Group, error)
	Update(id uint, req service.UpdateGroupRequest) (*model.Group, error)
	Delete(id uint) error
	AddModelsToGroup(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int, tiers map[uuid.UUID]string) error
	ListGroupModels(groupID uint) ([]model.GroupModel, error)
	RemoveModelFromGroup(groupID uint, modelID uuid.UUID) error
	ListEnabledGroupsWithModels() ([]model.Group, error)
	CountGroupModels(groupID uint) int
	GetFailoverDefaults() service.FailoverDefaults
}

type GroupHandler struct {
	svc        GroupServiceInterface
	modelRepo  ModelRepoInterface
	providerRepo ProviderRepoInterface
}

type ModelRepoInterface interface {
	GetByID(id uuid.UUID) (*model.Model, error)
}

type ProviderRepoInterface interface {
	GetByID(id uuid.UUID) (*model.Provider, error)
}

func NewGroupHandler(svc GroupServiceInterface, modelRepo ModelRepoInterface, providerRepo ProviderRepoInterface) *GroupHandler {
	return &GroupHandler{svc: svc, modelRepo: modelRepo, providerRepo: providerRepo}
}

type addModelsRequest struct {
	ModelIDs        []uuid.UUID         `json:"model_ids" binding:"required,min=1"`
	Priorities      map[uuid.UUID]int   `json:"priorities,omitempty"`
	Weights         map[uuid.UUID]int   `json:"weights,omitempty"`
	ComplexityTiers map[uuid.UUID]string `json:"complexity_tiers,omitempty"`
}

func parseGroupID(c *gin.Context) (uint, error) {
	v, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

func parseModelID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("model_id"))
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (h *GroupHandler) Create(c *gin.Context) {
	var req service.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	group, err := h.svc.Create(req)
	if err != nil {
		if errors.Is(err, service.ErrGroupNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "group name already exists", "code": CodeGroupNameConflict},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": group})
}

func (h *GroupHandler) List(c *gin.Context) {
	search := c.DefaultQuery("search", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	groups, total, err := h.svc.List(search, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list groups", "code": CodeInternalError},
		})
		return
	}

	type groupWithCount struct {
		ID                      uint   `json:"id"`
		Name                    string `json:"name"`
		Description             string `json:"description"`
		Strategy                string `json:"strategy"`
		Enabled                 bool   `json:"enabled"`
		MaxSwitches             int    `json:"max_switches"`
		SameModelRetries        int    `json:"same_model_retries"`
		FailoverCooldownSeconds int    `json:"failover_cooldown_seconds"`
		ModelCount              int    `json:"model_count"`
		OpenMode                string `json:"open_mode"`
		CreatedAt               string `json:"created_at"`
		UpdatedAt               string `json:"updated_at"`
	}

	items := make([]groupWithCount, len(groups))
	for i, g := range groups {
		count := h.svc.CountGroupModels(g.ID)
		items[i] = groupWithCount{
			ID:                      g.ID,
			Name:                    g.Name,
			Description:             g.Description,
			Strategy:                g.Strategy,
			Enabled:                 g.Enabled,
			MaxSwitches:             g.MaxSwitches,
			SameModelRetries:        g.SameModelRetries,
			FailoverCooldownSeconds: g.FailoverCooldownSeconds,
			ModelCount:              count,
			OpenMode:                g.OpenMode,
			CreatedAt:               g.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:               g.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *GroupHandler) GetByID(c *gin.Context) {
	if c.Param("id") == "defaults" {
		h.GetDefaults(c)
		return
	}
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	group, err := h.svc.GetByID(id)
	if err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to get group", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": group})
}

func (h *GroupHandler) Update(c *gin.Context) {
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	var req service.UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	group, err := h.svc.Update(id, req)
	if err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		if errors.Is(err, service.ErrGroupNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "group name already exists", "code": CodeGroupNameConflict},
			})
			return
		}
		if errors.Is(err, service.ErrGroupDeleted) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "group has been deleted", "code": CodeBadRequest},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": group})
}

func (h *GroupHandler) Delete(c *gin.Context) {
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.Delete(id); err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to delete group", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "group deleted"})
}

func (h *GroupHandler) AddModels(c *gin.Context) {
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	var req addModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.AddModelsToGroup(id, req.ModelIDs, req.Priorities, req.Weights, req.ComplexityTiers); err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "models added to group"})
}

func (h *GroupHandler) ListModels(c *gin.Context) {
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	models, err := h.svc.ListGroupModels(id)
	if err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list group models", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": models})
}

func (h *GroupHandler) RemoveModel(c *gin.Context) {
	groupID, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	modelID, err := parseModelID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.RemoveModelFromGroup(groupID, modelID); err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "model removed from group"})
}

func (h *GroupHandler) GetDefaults(c *gin.Context) {
	d := h.svc.GetFailoverDefaults()
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"max_switches":              d.MaxSwitches,
			"same_model_retries":        d.SameModelRetries,
			"failover_cooldown_seconds": d.CooldownSeconds,
		},
	})
}

func (h *GroupHandler) ListEnabledGroups(c *gin.Context) {
	token := c.GetHeader("X-Auth-Token")
	if token == "" {
		token = c.GetHeader("X-Internal-Token")
	}
	expected := os.Getenv("INTERNAL_AUTH_TOKEN")
	if expected == "" {
		expected = "internal-shared-secret"
	}
	if token != expected {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{"type": "authorization_error", "message": "invalid auth token", "code": "forbidden"},
		})
		return
	}

	groups, err := h.svc.ListEnabledGroupsWithModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list enabled groups", "code": CodeInternalError},
		})
		return
	}

	type modelInfo struct {
		ID             uuid.UUID `json:"id"`
		Name           string    `json:"name"`
		ProviderID     uuid.UUID `json:"provider_id,omitempty"`
		ProviderType   string    `json:"provider_type,omitempty"`
		ProviderName   string    `json:"provider_name,omitempty"`
		Endpoint       string    `json:"endpoint,omitempty"`
		Priority       *int      `json:"priority"`
		Weight         *int      `json:"weight"`
		ComplexityTier string    `json:"complexity_tier"`
	}

	type groupInfo struct {
		ID             uint        `json:"id"`
		Name           string      `json:"name"`
		Description    string      `json:"description"`
		Strategy       string      `json:"strategy"`
		Enabled        bool        `json:"enabled"`
		TimeoutSeconds int         `json:"timeout_seconds"`
		Models         []modelInfo `json:"models"`
	}

	result := make([]groupInfo, 0, len(groups))
	for _, g := range groups {
		gi := groupInfo{
			ID:             g.ID,
			Name:           g.Name,
			Description:    g.Description,
			Strategy:       g.Strategy,
			Enabled:        g.Enabled,
			TimeoutSeconds: g.TimeoutSeconds,
			Models:         make([]modelInfo, 0, len(g.Models)),
		}
		for _, gm := range g.Models {
			mi := modelInfo{
				ID:             gm.ModelID,
				Priority:       gm.Priority,
				Weight:         gm.Weight,
				ComplexityTier: gm.ComplexityTier,
			}
			if h.modelRepo != nil {
				m, err := h.modelRepo.GetByID(gm.ModelID)
				if err == nil && m != nil {
					// model 本身必须启用，否则跳过（不下发到 gateway snapshot）
					if m.Enabled == nil || !*m.Enabled {
						continue
					}
				mi.Name = m.UpstreamName()
				mi.ProviderID = m.ProviderID
				providerEnabled := false
				if h.providerRepo != nil {
					p, err := h.providerRepo.GetByID(m.ProviderID)
					if err == nil && p != nil && p.Enabled {
						mi.ProviderType = p.DefinitionID
						mi.ProviderName = p.Name
						mi.Endpoint = p.Endpoint
						providerEnabled = true
					}
				}
					if !providerEnabled {
						continue
					}
				} else {
					continue
				}
			}
			gi.Models = append(gi.Models, mi)
		}
		result = append(result, gi)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// upstreamModelName 返回推送给 Gateway 的实际 model ID。
// 优先使用 UpstreamModelID（明确指定的上游 ID），否则直接使用 Name。
// 注意：不再自动剥离 "{provider}/" 前缀。某些上游（如 ZHIPU）期望收到
// 带前缀的完整模型名（"ZHIPU/GLM-5.2"）；需要剥前缀的 provider（如 Bedrock、
// Anthropic）在各自的 adapter 中处理。
func upstreamModelName(m *model.Model) string {
	if m.UpstreamModelID != "" {
		return m.UpstreamModelID
	}
	return m.Name
}