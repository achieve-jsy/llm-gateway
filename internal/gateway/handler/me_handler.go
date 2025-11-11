package handler

import (
	"log/slog"
	"net/http"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
)

// MeHandler handles the /v1/me/models endpoint for authenticated employees
type MeHandler struct {
	router          ModelsRouterInterface
	state           *provider.RuntimeState
	controlPlaneURL string
	internalToken   string
}

// NewMeHandler creates a new MeHandler instance
func NewMeHandler(router ModelsRouterInterface, state *provider.RuntimeState, controlPlaneURL, internalToken string) *MeHandler {
	return &MeHandler{
		router:          router,
		state:           state,
		controlPlaneURL: controlPlaneURL,
		internalToken:   internalToken,
	}
}

// Handle handles GET /v1/me/models requests
func (h *MeHandler) Handle(c *gin.Context) {
	identityVal, exists := c.Get("identity")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "not authenticated", "code": "unauthorized"}})
		return
	}
	_ = identityVal

	var names []string
	if h.router != nil {
		resp, err := h.router.ListModels(c.Request.Context())
		if err == nil && resp != nil {
			for _, m := range resp.Data {
				names = append(names, m.ID)
			}
		}
	}

	items := make([]model.MeModelItem, 0, len(names))
	healthMap := h.fetchHealth(names)

	for _, name := range names {
		item := model.MeModelItem{ID: name, Object: "model", OwnedBy: "llm-gateway", Type: "chat", Enabled: true}
		if hEntry, ok := healthMap[name]; ok {
			if hEntry.Enabled != nil {
				item.Enabled = *hEntry.Enabled
			}
			item.ConsecutiveFailures = hEntry.ConsecutiveFailures
			item.LastCheckError = hEntry.LastCheckError
			item.ResponseTime = hEntry.ResponseTime
			if hEntry.ModelType != "" {
				item.Type = hEntry.ModelType
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, model.MeModelsResponse{Object: "list", Data: items})
}

func (h *MeHandler) fetchHealth(names []string) map[string]infra.ModelHealthEntry {
	if h.controlPlaneURL == "" || h.internalToken == "" {
		return nil
	}
	healthMap, err := infra.LoadModelHealth(h.controlPlaneURL, h.internalToken, names)
	if err != nil {
		slog.Warn("failed to fetch model health from control-plane", "error", err)
		return nil
	}
	return healthMap
}
