package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
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

	now := time.Now().Unix()

	if len(id.UsableGroups) > 0 {
		var names []string
		if id.AccessAllGroups {
			names = id.UsableGroups
		} else if h.client != nil {
			groups, err := h.client.GetKeyGroups(c.Request.Context(), id.KeyID)
			if err != nil {
				slog.Warn("bound models: failed to get key groups", "key_id", id.KeyID, "error", err)
				c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
				return
			}
			names = intersectStrings(id.UsableGroups, boundGroupNames(groups))
		}
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: stringSliceToModels(names, now)})
		return
	}

	if id.AccessAllGroups {
		names, err := h.client.ListAllGroups(c.Request.Context())
		if err != nil {
			slog.Error("bound models: failed to list all groups for access-all key", "key_id", id.KeyID, "error", err)
			c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
			return
		}
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: stringSliceToModels(names, now)})
		return
	}

	groups, err := h.client.GetKeyGroups(c.Request.Context(), id.KeyID)
	if err != nil {
		slog.Warn("bound models: failed to get key groups, returning empty", "key_id", id.KeyID, "error", err)
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}

	items := make([]core.Model, 0, len(groups))
	for _, g := range groups {
		if !g.Enabled || g.Name == "" {
			continue
		}
		items = append(items, core.Model{
			ID:      g.Name,
			Object:  "model",
			Created: now,
			OwnedBy: "organization",
		})
	}

	c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: items})
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

func boundGroupNames(groups []binding.BoundGroup) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Enabled && g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return names
}

func intersectStrings(ceiling []string, subset []string) []string {
	ceilSet := make(map[string]struct{}, len(ceiling))
	for _, s := range ceiling {
		ceilSet[s] = struct{}{}
	}
	out := make([]string, 0, min(len(subset), len(ceiling)))
	for _, s := range subset {
		if _, ok := ceilSet[s]; ok {
			out = append(out, s)
		}
	}
	return out
}
