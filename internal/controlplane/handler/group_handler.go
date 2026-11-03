package handler

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/middleware"
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
	ResetCapability(id uint) (*model.Group, error)
}

type GroupHandler struct {
	svc          GroupServiceInterface
	modelRepo    ModelRepoInterface
	providerRepo ProviderRepoInterface
	settingsSvc  SettingsServiceInterface
}

type ModelRepoInterface interface {
	GetByID(id uuid.UUID) (*model.Model, error)
	// GetByIDsWithCatalog 批量取模型并预加载 Catalog：路由 payload 的能力位
	// （supports_video_input）需要读 catalog 声明，且逐模型 GetByID 会 N+1。
	GetByIDsWithCatalog(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
}

type ProviderRepoInterface interface {
	GetByID(id uuid.UUID) (*model.Provider, error)
}

func NewGroupHandler(svc GroupServiceInterface, modelRepo ModelRepoInterface, providerRepo ProviderRepoInterface) *GroupHandler {
	return &GroupHandler{svc: svc, modelRepo: modelRepo, providerRepo: providerRepo}
}

// WithSettingsService 注入设置服务，用于解析组级 sticky 的全局默认值。
func (h *GroupHandler) WithSettingsService(svc SettingsServiceInterface) *GroupHandler {
	h.settingsSvc = svc
	return h
}

type addModelsRequest struct {
	ModelIDs        []uuid.UUID          `json:"model_ids" binding:"required,min=1"`
	Priorities      map[uuid.UUID]int    `json:"priorities,omitempty"`
	Weights         map[uuid.UUID]int    `json:"weights,omitempty"`
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
		ModelType               string `json:"model_type"`
		Strategy                string `json:"strategy"`
		Enabled                 bool   `json:"enabled"`
		MaxSwitches             int    `json:"max_switches"`
		SameModelRetries        int    `json:"same_model_retries"`
		FailoverCooldownSeconds int    `json:"failover_cooldown_seconds"`
		ModelCount              int    `json:"model_count"`
		OpenMode                string `json:"open_mode"`
		IsCapabilityOverride    bool   `json:"is_capability_override"`
		ModelTierID             string `json:"model_tier_id,omitempty"`
		ModelTierName           string `json:"model_tier_name,omitempty"`
		CreatedAt               string `json:"created_at"`
		UpdatedAt               string `json:"updated_at"`
	}

	items := make([]groupWithCount, len(groups))
	for i, g := range groups {
		count := h.svc.CountGroupModels(g.ID)
		mt := g.ModelType
		if mt == "" {
			mt = "chat"
		}
		var tierIDStr string
		var tierName string
		if g.BudgetTierID != nil {
			tierIDStr = g.BudgetTierID.String()
		}
		if g.BudgetTier != nil {
			tierName = g.BudgetTier.Name
		}
		items[i] = groupWithCount{
			ID:                      g.ID,
			Name:                    g.Name,
			Description:             g.Description,
			ModelType:               mt,
			Strategy:                g.Strategy,
			Enabled:                 g.Enabled,
			MaxSwitches:             g.MaxSwitches,
			SameModelRetries:        g.SameModelRetries,
			FailoverCooldownSeconds: g.FailoverCooldownSeconds,
			ModelCount:              count,
			OpenMode:                g.OpenMode,
			IsCapabilityOverride:    g.IsCapabilityOverride,
			ModelTierID:             tierIDStr,
			ModelTierName:           tierName,
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

func (h *GroupHandler) ResetCapability(c *gin.Context) {
	id, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid group id", "code": CodeBadRequest},
		})
		return
	}

	group, err := h.svc.ResetCapability(id)
	if err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "group not found", "code": CodeGroupNotFound},
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

func (h *GroupHandler) ListEnabledGroups(c *gin.Context) {
	token := c.GetHeader("X-Auth-Token")
	if token == "" {
		token = c.GetHeader("X-Internal-Token")
	}
	// 与 middleware.InternalAuth 共用同一解析后的内部 Token（env INTERNAL_TOKEN，
	// 兼容历史部署变量名 INTERNAL_AUTH_TOKEN），常量时间比较，未配置/默认值/不匹配
	// 一律 403（fail-closed）。
	expected := middleware.ResolveInternalToken()
	if expected == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
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

	// 获取全局 sticky 默认值，用于组级未显式设置时的 fallback
	stickyDefaultEnabled := true
	stickyDefaultTTL := 300
	if h.settingsSvc != nil {
		if sysSettings, err := h.settingsSvc.GetSystemSettings(c.Request.Context()); err == nil && sysSettings != nil {
			stickyDefaultEnabled = sysSettings.StickyDefaultEnabled
			stickyDefaultTTL = sysSettings.StickyDefaultTTLSeconds
		}
	}

	type modelInfo struct {
		ID             uuid.UUID `json:"id"`
		Name           string    `json:"name"`
		ModelType      string    `json:"model_type,omitempty"`
		ProviderID     uuid.UUID `json:"provider_id,omitempty"`
		ProviderType   string    `json:"provider_type,omitempty"`
		ProviderName   string    `json:"provider_name,omitempty"`
		Endpoint       string    `json:"endpoint,omitempty"`
		Priority       *int      `json:"priority"`
		Weight         *int      `json:"weight"`
		ComplexityTier string    `json:"complexity_tier"`
		// SupportsVideoInput 供 gateway 逐成员过滤（组级聚合是保守 AND，
		// 混合组必须能在路由期挑出真正支持视频的成员）。
		SupportsVideoInput bool `json:"supports_video_input"`
	}

	type groupInfo struct {
		ID               uint        `json:"id"`
		Name             string      `json:"name"`
		Description      string      `json:"description"`
		Strategy         string      `json:"strategy"`
		Enabled          bool        `json:"enabled"`
		TimeoutSeconds   int         `json:"timeout_seconds"`
		StickyEnabled    bool        `json:"sticky_enabled"`
		StickyTTLSeconds int         `json:"sticky_ttl_seconds"`
		Models           []modelInfo `json:"models"`
	}

	// 一次性批量取模型（含 catalog）。能力位必须能读到 catalog 声明，
	// 否则"catalog 已声明支持视频"的模型在路由期会被判成不支持。
	modelsByID := make(map[uuid.UUID]*model.Model)
	if h.modelRepo != nil {
		ids := make([]uuid.UUID, 0, len(groups)*4)
		for _, g := range groups {
			for _, gm := range g.Models {
				ids = append(ids, gm.ModelID)
			}
		}
		if len(ids) > 0 {
			byID, err := h.modelRepo.GetByIDsWithCatalog(ids)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{"type": "internal_error", "message": "failed to load models", "code": CodeInternalError},
				})
				return
			}
			modelsByID = byID
		}
	}

	result := make([]groupInfo, 0, len(groups))
	for _, g := range groups {
		// 解析 effective sticky：组级显式设置优先，否则用全局默认
		effStickyEnabled := stickyDefaultEnabled
		if g.StickyEnabled != nil {
			effStickyEnabled = *g.StickyEnabled
		}
		effStickyTTL := stickyDefaultTTL
		if g.StickyTTLSeconds != nil {
			effStickyTTL = *g.StickyTTLSeconds
		}

		gi := groupInfo{
			ID:               g.ID,
			Name:             g.Name,
			Description:      g.Description,
			Strategy:         g.Strategy,
			Enabled:          g.Enabled,
			TimeoutSeconds:   g.TimeoutSeconds,
			StickyEnabled:    effStickyEnabled,
			StickyTTLSeconds: effStickyTTL,
			Models:           make([]modelInfo, 0, len(g.Models)),
		}
		for _, gm := range g.Models {
			mi := modelInfo{
				ID:             gm.ModelID,
				Priority:       gm.Priority,
				Weight:         gm.Weight,
				ComplexityTier: gm.ComplexityTier,
			}
			if h.modelRepo != nil {
				m := modelsByID[gm.ModelID]
				if m != nil {
					// model 本身必须启用，否则跳过（不下发到 gateway snapshot）
					if m.Enabled == nil || !*m.Enabled {
						continue
					}
					mi.Name = m.UpstreamName()
					mi.ModelType = m.ModelType
					mi.ProviderID = m.ProviderID
					mi.SupportsVideoInput = service.EffectiveSupportsVideoInput(m)
					providerEnabled := false
					if h.providerRepo != nil {
						p, err := h.providerRepo.GetByID(m.ProviderID)
						if err == nil && p != nil && p.Enabled {
							mi.ProviderType = p.DefinitionID
							if mi.ProviderType == "" {
								mi.ProviderType = p.Type
							}
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
