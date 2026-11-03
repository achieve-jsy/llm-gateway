package service

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// wantCategoryTypes 分类归属表（唯一事实源，与 notification_registry.go 的字段标注保持一致）。
var wantCategoryTypes = map[string][]string{
	"budget": {
		model.NotificationTypeBudgetAlert,
		model.NotificationTypeBudgetOwnerAttention,
		model.NotificationTypeBudgetScheduleApplied,
		model.NotificationTypeBudgetScheduleFailed,
	},
	"expansion": {
		model.NotificationTypeExpansionSubmitted,
		model.NotificationTypeExpansionApproved,
		model.NotificationTypeExpansionRejected,
		model.NotificationTypeExpansionDecision,
		model.NotificationTypeExpansionApplyFailed,
		model.NotificationTypeExpansionExpiring,
		model.NotificationTypeExpansionRestored,
		model.NotificationTypeApprovalPoolEmpty,
	},
	"key": {
		model.NotificationTypeKeyActivity,
		model.NotificationTypeKeyRequestApproved,
		model.NotificationTypeKeyApprovalPending,
	},
	"platform_risk": {
		model.NotificationTypePlatformRiskCounterIntegrity,
		model.NotificationTypePlatformRiskPeriodHealth,
		model.NotificationTypePlatformRiskRedisHealth,
	},
	"ops": {
		model.NotificationTypeOpsChannelDown,
		model.NotificationTypeOpsChannelRecovered,
	},
	"notification": {
		model.NotificationTypeOpsDeliveryFailed,
	},
	"system": {
		model.NotificationTypeSystemStartup,
	},
}

func TestNotificationRegistry_EveryTypeHasCategory(t *testing.T) {
	for _, def := range GetNotificationTypeRegistry() {
		if def.Category == "" {
			t.Errorf("type %q missing Category", def.Name)
		}
		if def.CategoryName == "" {
			t.Errorf("type %q missing CategoryName", def.Name)
		}
	}
}

func TestNotificationRegistry_CategoryMembership(t *testing.T) {
	reg := GetNotificationTypeRegistry()
	got := map[string][]string{}
	gotCount := 0
	for _, def := range reg {
		got[def.Category] = append(got[def.Category], def.Name)
		gotCount++
	}

	if gotCount != 22 {
		t.Errorf("registry has %d types, want 22", gotCount)
	}

	if len(got) != len(wantCategoryTypes) {
		t.Errorf("registry has %d categories, want %d: %v", len(got), len(wantCategoryTypes), got)
	}
	for cat, wantTypes := range wantCategoryTypes {
		gotTypes, ok := got[cat]
		if !ok {
			t.Errorf("category %q missing from registry", cat)
			continue
		}
		if len(gotTypes) != len(wantTypes) {
			t.Errorf("category %q has %d types %v, want %d %v", cat, len(gotTypes), gotTypes, len(wantTypes), wantTypes)
			continue
		}
		for _, wt := range wantTypes {
			found := false
			for _, gt := range gotTypes {
				if gt == wt {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("category %q missing type %q (got %v)", cat, wt, gotTypes)
			}
		}
	}
}

func TestNotificationMetadata_CategoriesConsistent(t *testing.T) {
	resp := GetNotificationMetadata()

	// 分类列表去重、稳定顺序，且与类型上的 category/categoryName 一致。
	seen := map[string]string{}
	for _, cat := range resp.Categories {
		if cat.Name == "" || cat.DisplayName == "" {
			t.Errorf("category entry incomplete: %+v", cat)
		}
		if prev, dup := seen[cat.Name]; dup {
			t.Errorf("duplicate category %q (display %q and %q)", cat.Name, prev, cat.DisplayName)
		}
		seen[cat.Name] = cat.DisplayName
	}

	for _, typ := range resp.Types {
		display, ok := seen[typ.Category]
		if !ok {
			t.Errorf("type %q category %q not in categories list", typ.Name, typ.Category)
			continue
		}
		if typ.CategoryName != display {
			t.Errorf("type %q categoryName %q != categories displayName %q", typ.Name, typ.CategoryName, display)
		}
	}

	// 分类列表与注册表分类集合一一对应。
	if len(seen) != len(wantCategoryTypes) {
		t.Errorf("categories count %d, want %d", len(seen), len(wantCategoryTypes))
	}
}

// TestNotificationMetadata_JSONContract 锁定前端消费的 wire 格式：
// types[].category/categoryName、顶层 categories[]（name/displayName）必须按此 JSON 键名输出。
func TestNotificationMetadata_JSONContract(t *testing.T) {
	raw, err := json.Marshal(GetNotificationMetadata())
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	var body struct {
		Types []struct {
			Name         string `json:"name"`
			Category     string `json:"category"`
			CategoryName string `json:"categoryName"`
		} `json:"types"`
		Categories []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if len(body.Categories) != len(wantCategoryTypes) {
		t.Errorf("json categories count %d, want %d", len(body.Categories), len(wantCategoryTypes))
	}
	for _, c := range body.Categories {
		if c.Name == "" || c.DisplayName == "" {
			t.Errorf("json category entry incomplete: %+v", c)
		}
	}
	if len(body.Types) != 22 {
		t.Errorf("json types count %d, want 22", len(body.Types))
	}
	for _, typ := range body.Types {
		if typ.Name == "" || typ.Category == "" || typ.CategoryName == "" {
			t.Errorf("json type entry missing category fields: %+v", typ)
		}
	}
}
