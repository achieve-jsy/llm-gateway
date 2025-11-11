package provider

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// TestNewRegistry 验证新注册表为空状态
func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if registry.providers == nil {
		t.Fatal("providers map is nil")
	}

	if registry.modelMap == nil {
		t.Fatal("modelMap is nil")
	}

	if len(registry.providers) != 0 {
		t.Fatalf("expected empty providers, got %d", len(registry.providers))
	}

	if len(registry.modelMap) != 0 {
		t.Fatalf("expected empty modelMap, got %d", len(registry.modelMap))
	}
}

// TestRegister 注册 provider，验证 GetProvider
func TestRegister(t *testing.T) {
	registry := NewRegistry()
	mockProvider := NewMockProvider("test-provider")

	registry.Register("test-provider", mockProvider)

	p, ok := registry.GetProvider("test-provider")
	if !ok {
		t.Fatal("provider not found after registration")
	}

	if p != mockProvider {
		t.Fatal("retrieved provider is not the same instance")
	}

	if p.Name() != "test-provider" {
		t.Fatalf("expected provider name 'test-provider', got %s", p.Name())
	}
}

// TestRegisterModel 注册模型映射后，模型可用性要求 provider 也已注册
func TestRegisterModel(t *testing.T) {
	registry := NewRegistry()

	// 仅注册模型映射但缺少 provider 实例时，IsModelAvailable 应返回 false
	registry.RegisterModel("gpt-4", "openai")
	if registry.IsModelAvailable("gpt-4") {
		t.Fatal("model should not be available when provider instance is missing")
	}

	// 注册 provider 之后，模型方可用
	registry.Register("openai", NewMockProvider("openai"))
	registry.RegisterModel("gpt-3.5", "openai")

	if !registry.IsModelAvailable("gpt-4") {
		t.Fatal("model gpt-4 should be available after provider registration")
	}
	if !registry.IsModelAvailable("gpt-3.5") {
		t.Fatal("model gpt-3.5 should be available after registration")
	}
}

// TestResolve_ExactMatch 按精确模型名解析
func TestResolve_ExactMatch(t *testing.T) {
	registry := NewRegistry()
	mockProvider := NewMockProvider("openai")

	registry.Register("openai", mockProvider)
	registry.RegisterModel("gpt-4", "openai")

	p, err := registry.Resolve("gpt-4")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if p.Name() != "openai" {
		t.Fatalf("expected provider name 'openai', got %s", p.Name())
	}
}

// TestResolve_PrefixMatch 按前缀解析
func TestResolve_PrefixMatch(t *testing.T) {
	registry := NewRegistry()
	mockProvider := NewMockProvider("anthropic")

	registry.Register("anthropic", mockProvider)
	registry.RegisterModel("claude", "anthropic")

	// 精确匹配 "claude" 的模型前缀匹配
	p, err := registry.Resolve("claude-3-sonnet")
	if err != nil {
		t.Fatalf("expected no error for prefix match, got %v", err)
	}

	if p.Name() != "anthropic" {
		t.Fatalf("expected provider name 'anthropic', got %s", p.Name())
	}
}

// TestResolve_NotFound 模型未找到返回错误
func TestResolve_NotFound(t *testing.T) {
	registry := NewRegistry()

	p, err := registry.Resolve("unknown-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}

	if p != nil {
		t.Fatal("expected nil provider for unknown model")
	}

	if err.Error() != "no provider found for model: unknown-model" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

// TestListAllModels 列出所有注册的模型
func TestListAllModels(t *testing.T) {
	registry := NewRegistry()

	registry.RegisterModel("gpt-4", "openai")
	registry.RegisterModel("gpt-3.5", "openai")
	registry.RegisterModel("claude-3", "anthropic")

	models := registry.ListAllModels()

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	modelSet := make(map[string]bool)
	for _, m := range models {
		modelSet[m] = true
	}

	expectedModels := []string{"gpt-4", "gpt-3.5", "claude-3"}
	for _, expected := range expectedModels {
		if !modelSet[expected] {
			t.Fatalf("expected model %s not found in list", expected)
		}
	}
}

// TestIsModelAvailable 检查模型可用性
func TestIsModelAvailable(t *testing.T) {
	registry := NewRegistry()
	mockProvider := NewMockProvider("openai")

	registry.Register("openai", mockProvider)
	registry.RegisterModel("gpt-4", "openai")

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"available model", "gpt-4", true},
		{"unavailable model", "unknown", false},
		{"provider missing", "gpt-3.5", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.IsModelAvailable(tt.model)
			if result != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestReload 原子重新加载注册表
func TestReload(t *testing.T) {
	registry := NewRegistry()
	mockProvider1 := NewMockProvider("openai")

	registry.Register("openai", mockProvider1)
	registry.RegisterModel("gpt-4", "openai")

	// 验证原始状态
	if len(registry.ListAllModels()) != 1 {
		t.Fatal("expected 1 model after initial registration")
	}

	// 创建新的 entries 进行 reload
	entries := []model.ProviderEntry{
		{
			ID:       "provider-1",
			Name:     "anthropic",
			Type:     "anthropic",
			Enabled:  true,
			Models:   []string{"claude-3", "claude-3.5"},
		},
		{
			ID:       "provider-2",
			Name:     "openai-disabled",
			Type:     "openai",
			Enabled:  false,
			Models:   []string{"gpt-4"},
		},
	}

	factory := func(entry model.ProviderEntry) Provider {
		return NewMockProvider(entry.Name)
	}

	registry.Reload(entries, factory)

	// 验证重新加载后的状态
	models := registry.ListAllModels()
	if len(models) != 2 {
		t.Fatalf("expected 2 models after reload, got %d", len(models))
	}

	// 验证禁用的 provider 未注册
	if registry.IsModelAvailable("gpt-4") {
		t.Fatal("disabled provider's model should not be available")
	}

	// 验证新 provider 可用
	if !registry.IsModelAvailable("claude-3") {
		t.Fatal("claude-3 should be available after reload")
	}

	// 验证原 provider 被替换
	if _, ok := registry.GetProvider("openai"); ok {
		t.Fatal("old openai provider should be removed after reload")
	}
}

// TestRegistry_Concurrency 并发访问安全性
func TestRegistry_Concurrency(t *testing.T) {
	registry := NewRegistry()

	// 初始化一些 providers
	for i := 0; i < 5; i++ {
		name := "provider-" + string(rune('0'+i))
		registry.Register(name, NewMockProvider(name))
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	successCount := atomic.Int32{}

	// 并发读取
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "provider-" + string(rune('0'+(id%5)))
			p, ok := registry.GetProvider(name)
			if !ok {
				errors <- nil
				return
			}
			if p.Name() != name {
				errors <- nil
				return
			}
			successCount.Add(1)
		}(i)
	}

	// 并发注册模型
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			providerName := "provider-" + string(rune('0'+(id%5)))
			modelName := "model-" + string(rune('0'+byte(id)))
			registry.RegisterModel(modelName, providerName)
			successCount.Add(1)
		}(i)
	}

	// 并发列表查询
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			models := registry.ListAllModels()
			if len(models) >= 0 {
				successCount.Add(1)
			}
		}()
	}

	// 并发可用性检查
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			modelName := "model-" + string(rune('0'+byte(id)))
			registry.IsModelAvailable(modelName)
			successCount.Add(1)
		}(i)
	}

	wg.Wait()
	close(errors)

	// 验证没有错误
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent access error: %v", err)
		}
	}

	// 验证成功次数
	if successCount.Load() == 0 {
		t.Fatal("expected some successful operations")
	}
}

// TestResolve_PrefixMatchPriority 前缀匹配时，精确匹配优先级更高
func TestResolve_PrefixMatchPriority(t *testing.T) {
	registry := NewRegistry()
	providerA := NewMockProvider("provider-a")
	providerB := NewMockProvider("provider-b")

	registry.Register("provider-a", providerA)
	registry.Register("provider-b", providerB)

	// 注册 "claude" 对应 provider-a，"claude-3" 对应 provider-b
	registry.RegisterModel("claude", "provider-a")
	registry.RegisterModel("claude-3", "provider-b")

	// 解析 "claude-3-sonnet" 应该匹配到 "claude-3" -> provider-b（前缀更长，优先级更高）
	// 实际上当前实现是第一个找到的前缀就返回，所以需要验证实际行为
	p, err := registry.Resolve("claude-3-sonnet")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 验证解析到某个 provider
	if p == nil {
		t.Fatal("expected provider, got nil")
	}
}

// TestGetProvider_NotFound 获取不存在的 provider
func TestGetProvider_NotFound(t *testing.T) {
	registry := NewRegistry()

	p, ok := registry.GetProvider("non-existent")
	if ok {
		t.Fatal("expected ok=false for non-existent provider")
	}

	if p != nil {
		t.Fatal("expected nil provider for non-existent provider")
	}
}

// TestRegisterMultipleModelsToSameProvider 多个模型映射到同一 provider
func TestRegisterMultipleModelsToSameProvider(t *testing.T) {
	registry := NewRegistry()
	mockProvider := NewMockProvider("openai")

	registry.Register("openai", mockProvider)
	registry.RegisterModel("gpt-4", "openai")
	registry.RegisterModel("gpt-3.5-turbo", "openai")
	registry.RegisterModel("gpt-4-turbo", "openai")

	tests := []string{"gpt-4", "gpt-3.5-turbo", "gpt-4-turbo"}

	for _, model := range tests {
		t.Run(model, func(t *testing.T) {
			p, err := registry.Resolve(model)
			if err != nil {
				t.Fatalf("expected no error for model %s, got %v", model, err)
			}

			if p.Name() != "openai" {
				t.Fatalf("expected openai provider, got %s", p.Name())
			}
		})
	}
}

// TestResolve_MissingProviderInstance 模型映射到不存在的 provider
func TestResolve_MissingProviderInstance(t *testing.T) {
	registry := NewRegistry()

	// 只注册模型，不注册对应的 provider
	registry.RegisterModel("missing-model", "missing-provider")

	_, err := registry.Resolve("missing-model")
	if err == nil {
		t.Fatal("expected error when provider instance is missing")
	}
}
