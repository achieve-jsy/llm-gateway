package protocol_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/protocol"
)

func TestLoadPolicy_DefaultLegacy(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "",
	}
	policy, err := protocol.LoadPolicy(cfg)
	if err != nil {
		t.Fatalf("LoadPolicy should not error for empty config: %v", err)
	}
	if policy == nil {
		t.Fatal("policy must not be nil")
	}

	// 空配置默认所有入口走 legacy
	mode := policy.ModeFor("any-key", "messages")
	if mode != protocol.ModeLegacy {
		t.Errorf("default mode should be legacy, got %s", mode)
	}
}

func TestLoadPolicy_ActiveForKey(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "legacy",
		PerKey: map[string]string{
			"key-alpha": "active",
		},
	}
	policy, err := protocol.LoadPolicy(cfg)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	// key-alpha 被列入白名单 → active
	if mode := policy.ModeFor("key-alpha", "messages"); mode != protocol.ModeActive {
		t.Errorf("key-alpha should be active, got %s", mode)
	}

	// key-beta 不在白名单 → 回退 default
	if mode := policy.ModeFor("key-beta", "messages"); mode != protocol.ModeLegacy {
		t.Errorf("key-beta should fall back to legacy, got %s", mode)
	}
}

func TestLoadPolicy_ShadowForEntry(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "legacy",
		PerEntry: map[string]string{
			"messages": "shadow",
		},
	}
	policy, err := protocol.LoadPolicy(cfg)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	// messages 入口灰度 shadow
	if mode := policy.ModeFor("any-key", "messages"); mode != protocol.ModeShadow {
		t.Errorf("messages entry should be shadow, got %s", mode)
	}

	// responses 入口未灰度 → 回退 default
	if mode := policy.ModeFor("any-key", "responses"); mode != protocol.ModeLegacy {
		t.Errorf("responses entry should fall back to legacy, got %s", mode)
	}
}

func TestLoadPolicy_InvalidMode(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "supercharged",
	}
	_, err := protocol.LoadPolicy(cfg)
	if err == nil {
		t.Fatal("expected error for invalid default mode")
	}
}

func TestModeFor_KeyOverridesEntry(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "legacy",
		PerEntry: map[string]string{
			"messages": "shadow",
		},
		PerKey: map[string]string{
			"key-vip": "active",
		},
	}
	policy, err := protocol.LoadPolicy(cfg)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	// key-vip 指向 active，即使入口是 shadow → key 优先
	if mode := policy.ModeFor("key-vip", "messages"); mode != protocol.ModeActive {
		t.Errorf("key-vip should win over entry, got %s", mode)
	}

	// 无 key 匹配时入口 shadow 生效
	if mode := policy.ModeFor("key-regular", "messages"); mode != protocol.ModeShadow {
		t.Errorf("regular key should use entry shadow, got %s", mode)
	}
}
