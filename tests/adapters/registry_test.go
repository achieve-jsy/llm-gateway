package adapters_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/adapters"
)

type fakeAdapter struct{ name string }

func (a fakeAdapter) Name() string { return a.name }

func TestRegistryRegisterAndLookupByProviderTypeAndProtocolMode(t *testing.T) {
	registry := adapters.NewRegistry()
	adapter := fakeAdapter{name: "openai-chat"}

	if err := registry.Register("OpenAI", adapters.ProtocolOpenAIChat, adapter); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got, ok := registry.Lookup(" openai ", adapters.ProtocolOpenAIChat)
	if !ok {
		t.Fatal("Lookup did not find registered adapter")
	}
	if got.Name() != "openai-chat" {
		t.Fatalf("adapter name = %q, want openai-chat", got.Name())
	}

	if _, ok := registry.Lookup("openai", adapters.ProtocolOpenAICompatible); ok {
		t.Fatal("Lookup should distinguish protocol_mode")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	registry := adapters.NewRegistry()
	if err := registry.Register("azure", adapters.ProtocolOpenAIChat, fakeAdapter{name: "a"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register("azure", adapters.ProtocolOpenAIChat, fakeAdapter{name: "b"}); err == nil {
		t.Fatal("duplicate Register should return error")
	}
}
