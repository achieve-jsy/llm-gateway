package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type BoundModelsHandler struct {
	client *binding.Client
}

func NewBoundModelsHandler(controlPlaneURL, internalToken string) *BoundModelsHandler {
	return &BoundModelsHandler{
		client: binding.NewClient(controlPlaneURL, internalToken),
	}
}

func (h *BoundModelsHandler) Handle(c *gin.Context) {
	identityVal, exists := c.Get("identity")
	if !exists {
		c.JSON(http.StatusOK, model.ModelsResponse{
			Object: "list",
			Data:   []model.ModelInfo{},
		})
		return
	}
	id, ok := identityVal.(*model.UserIdentity)
	if !ok || id.KeyID == "" {
		c.JSON(http.StatusOK, model.ModelsResponse{
			Object: "list",
			Data:   []model.ModelInfo{},
		})
		return
	}
	keyID := id.KeyID

	groups, err := h.client.GetKeyGroups(c.Request.Context(), keyID)
	if err != nil {
		slog.Warn("bound models: failed to get key groups, returning empty", "key_id", keyID, "error", err)
		c.JSON(http.StatusOK, model.ModelsResponse{
			Object: "list",
			Data:   []model.ModelInfo{},
		})
		return
	}

	items := make([]model.ModelInfo, 0, len(groups))
	for _, g := range groups {
		items = append(items, model.ModelInfo{
			ID:      g.Name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "organization",
		})
	}

	c.JSON(http.StatusOK, model.ModelsResponse{
		Object: "list",
		Data:   items,
	})
}