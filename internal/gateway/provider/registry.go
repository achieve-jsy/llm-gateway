package provider

import (
	"fmt"
	"strings"
	"sync"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// Registry Provider 注册表，按模型名前缀分发
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider   // name → Provider 实例
	modelMap  map[string]string     // model name → provider name
	entries   []model.ProviderEntry // 原始配置
}

// NewRegistry 创建空注册表
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		modelMap:  make(map[string]string),
	}
}

// Register 注册一个 Provider 实例
func (r *Registry) Register(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// RegisterModel 注册模型到 Provider 的映射
func (r *Registry) RegisterModel(modelName, providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelMap[modelName] = providerName
}

// Resolve 根据模型名查找对应 Provider
func (r *Registry) Resolve(modelName string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 精确匹配
	if providerName, ok := r.modelMap[modelName]; ok {
		if p, exists := r.providers[providerName]; exists {
			return p, nil
		}
	}

	// 前缀匹配
	for registeredModel, providerName := range r.modelMap {
		if strings.HasPrefix(modelName, registeredModel) {
			if p, exists := r.providers[providerName]; exists {
				return p, nil
			}
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", modelName)
}

// ListAllModels 返回所有已注册模型
func (r *Registry) ListAllModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.modelMap))
	for m := range r.modelMap {
		models = append(models, m)
	}
	return models
}

// IsModelAvailable 检查模型是否可用
func (r *Registry) IsModelAvailable(modelName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if providerName, ok := r.modelMap[modelName]; ok {
		_, exists := r.providers[providerName]
		return exists
	}
	return false
}

// Reload 原子替换注册表内容（配置变更时调用）
func (r *Registry) Reload(entries []model.ProviderEntry, factory func(entry model.ProviderEntry) Provider) {
	newProviders := make(map[string]Provider)
	newModelMap := make(map[string]string)

	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		p := factory(entry)
		newProviders[entry.Name] = p
		for _, m := range entry.Models {
			newModelMap[m] = entry.Name
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = newProviders
	r.modelMap = newModelMap
	r.entries = entries
}

// GetProvider 按名称获取 Provider
func (r *Registry) GetProvider(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}
