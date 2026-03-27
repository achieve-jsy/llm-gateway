package handler

import (
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core/video"
	"github.com/gin-gonic/gin"
)

// AssetAdapterRegistry maps provider type strings to AssetProvider instances.
type AssetAdapterRegistry struct {
	relay      video.AssetProvider
	volcengine video.AssetProvider
}

// NewAssetAdapterRegistry creates a registry with the given adapters.
func NewAssetAdapterRegistry(relay, volcengine video.AssetProvider) *AssetAdapterRegistry {
	return &AssetAdapterRegistry{relay: relay, volcengine: volcengine}
}

// Get returns the adapter for the given provider type.
func (r *AssetAdapterRegistry) Get(providerType string) video.AssetProvider {
	switch providerType {
	case "seedance-volcengine":
		if r.volcengine != nil {
			return r.volcengine
		}
	default:
		if r.relay != nil {
			return r.relay
		}
	}
	if r.relay != nil {
		return r.relay
	}
	return r.volcengine
}

// AssetHandler 素材库 Handler
type AssetHandler struct {
	relayAdapter      video.AssetProvider
	volcengineAdapter video.AssetProvider
	adapterReg        *AssetAdapterRegistry
	providerType      string
}

// AssetHandlerOption is a functional option for AssetHandler.
type AssetHandlerOption func(*AssetHandler)

// WithAssetProviderType sets the default provider type for asset operations.
func WithAssetProviderType(pt string) AssetHandlerOption {
	return func(h *AssetHandler) { h.providerType = pt }
}

// NewAssetHandler 创建素材 Handler
func NewAssetHandler(relayAdapter, volcengineAdapter video.AssetProvider, opts ...AssetHandlerOption) *AssetHandler {
	h := &AssetHandler{
		relayAdapter:      relayAdapter,
		volcengineAdapter: volcengineAdapter,
		adapterReg:        NewAssetAdapterRegistry(relayAdapter, volcengineAdapter),
		providerType:      "seedance-relay",
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Handle 统一入口，按 Action 分发
func (h *AssetHandler) Handle(c *gin.Context) {
	action := c.Param("action")
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	adapter := h.adapterReg.Get(h.providerType)
	result, err := adapter.DoAction(c.Request.Context(), action, body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}
