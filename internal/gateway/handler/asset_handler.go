package handler

import (
	"io"
	"net/http"
	"sync"

	"github.com/example/llm-gateway/internal/core/video"
	"github.com/gin-gonic/gin"
)

// AssetAdapterRegistry maps provider type strings to AssetProvider instances.
// Swap 支持配置热更新。
type AssetAdapterRegistry struct {
	mu         sync.RWMutex
	relay      video.AssetProvider
	volcengine video.AssetProvider
}

// NewAssetAdapterRegistry creates a registry with the given adapters.
func NewAssetAdapterRegistry(relay, volcengine video.AssetProvider) *AssetAdapterRegistry {
	return &AssetAdapterRegistry{relay: relay, volcengine: volcengine}
}

// Swap 原子替换素材适配器。
func (r *AssetAdapterRegistry) Swap(relay, volcengine video.AssetProvider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if relay != nil {
		r.relay = relay
	}
	if volcengine != nil {
		r.volcengine = volcengine
	}
}

// Get returns the adapter for the given provider type.
func (r *AssetAdapterRegistry) Get(providerType string) video.AssetProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	adapterReg   *AssetAdapterRegistry
	providerType string
}

// AdapterRegistry 返回适配器注册表（供热加载 Swap）。
func (h *AssetHandler) AdapterRegistry() *AssetAdapterRegistry {
	if h == nil {
		return nil
	}
	return h.adapterReg
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
		adapterReg:   NewAssetAdapterRegistry(relayAdapter, volcengineAdapter),
		providerType: "seedance-relay",
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
