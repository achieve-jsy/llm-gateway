package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/authz"
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
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}
	id, ok := identityVal.(*model.UserIdentity)
	if !ok || id.KeyID == "" {
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}

	names, err := authz.ResolveVisibleGroupNames(c.Request.Context(), id, h.client)
	if err != nil {
		slog.Warn("bound models: failed to resolve visible groups", "key_id", id.KeyID, "error", err)
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}

	c.JSON(http.StatusOK, core.ModelsResponse{
		Object: "list",
		Data:   stringSliceToModels(names, time.Now().Unix()),
	})
}

func stringSliceToModels(names []string, now int64) []core.Model {
	items := make([]core.Model, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		items = append(items, core.Model{
			ID:      name,
			Object:  "model",
			Created: now,
			OwnedBy: "organization",
		})
	}
	return items
}
