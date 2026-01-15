package audit

import (
	"testing"

	"github.com/google/uuid"
)

// ── 接口契约测试 ───────────────────────────────────────────

func TestNop_ImplementsRecorder(t *testing.T) {
	var _ Recorder = Nop{}
}

func TestLogRecorder_ImplementsRecorder(t *testing.T) {
	var _ Recorder = LogRecorder{}
}

// ── Wrap 行为 ───────────────────────────────────────────

func TestWrap_NilReturnsNop(t *testing.T) {
	rec := Wrap(nil)
	if _, ok := rec.(Nop); !ok {
		t.Errorf("Wrap(nil) should return Nop, got %T", rec)
	}
}

func TestWrap_NonNilReturnsUnchanged(t *testing.T) {
	original := LogRecorder{}
	rec := Wrap(original)
	if _, ok := rec.(LogRecorder); !ok {
		t.Errorf("Wrap(LogRecorder) should return LogRecorder, got %T", rec)
	}
}

func TestNop_RecordDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Nop.Record should not panic: %v", r)
		}
	}()
	var nop Nop
	nop.Record(Input{
		Action:       ActionProviderCreate,
		ResourceType: ResourceProvider,
		ResourceID:   "p-1",
	})
}

func TestLogRecorder_RecordDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogRecorder.Record should not panic: %v", r)
		}
	}()
	var log LogRecorder
	log.Record(Input{
		Action:       ActionServiceKeyCreate,
		ResourceType: ResourceServiceKey,
		ResourceID:   "sk-1",
		UserID:       uuid.New(),
		UserName:     "alice",
		ClientIP:     "10.0.0.1",
		StatusCode:   200,
		Details:      map[string]any{"key": "value"},
	})
}

// ── context helpers ─────────────────────────────────────

func TestMarkRecorded_And_IsRecorded(t *testing.T) {
	// 注意：需要 gin.Context 才能测试，这里用 mock 测试常量
	// 完整流程在 middleware/audit_middleware_test.go 中验证
	if contextKeyRecorded == "" {
		t.Error("contextKeyRecorded should not be empty")
	}
	if contextKeyResourceName == "" {
		t.Error("contextKeyResourceName should not be empty")
	}
}

// ── 类型系统测试（编译期）────────────────────────────────

func TestActionTypes_AreDistinct(t *testing.T) {
	actions := []Action{
		ActionServiceKeyCreate,
		ActionServiceKeyDelete,
		ActionServiceKeyToggle,
		ActionServiceKeyIPChanged,
		ActionServiceKeyReset,
		ActionProviderCreate,
		ActionProviderUpdate,
		ActionProviderDelete,
		ActionProviderToggle,
		ActionProviderTest,
		ActionProviderSyncModels,
		ActionModelCreate,
		ActionModelUpdate,
		ActionModelDelete,
		ActionModelBatchUpdateStatus,
		ActionGroupCreate,
		ActionGroupUpdate,
		ActionGroupDelete,
		ActionGroupAddModels,
		ActionGroupRemoveModel,
		ActionGroupEntitlementGrant,
		ActionGroupEntitlementRevoke,
		ActionBudgetSet,
		ActionBudgetDelete,
		ActionExpansionCreate,
		ActionExpansionDecision,
		ActionKeyCreate,
		ActionKeyDisable,
		ActionKeyEnable,
		ActionKeyDelete,
		ActionKeyReset,
		ActionKeyToggleLog,
		ActionKeyToggleAccessAll,
		ActionKeyBindGroups,
		ActionUserLogin,
		ActionUserLogout,
		ActionSettingsUpdate,
		ActionAnnouncementPublish,
		ActionAnnouncementDelete,
		ActionHealthCheckRun,
		ActionEmployeeImport,
	}

	seen := make(map[Action]struct{}, len(actions))
	for _, a := range actions {
		if a == "" {
			t.Errorf("Action should not be empty")
		}
		if _, exists := seen[a]; exists {
			t.Errorf("Duplicate action: %s", a)
		}
		seen[a] = struct{}{}
	}
}

func TestResourceTypes_AreDistinct(t *testing.T) {
	resources := []Resource{
		ResourceServiceKey,
		ResourceProvider,
		ResourceModel,
		ResourceGroup,
		ResourceBudget,
		ResourceExpansion,
		ResourceKey,
		ResourceUser,
		ResourceSettings,
		ResourceAnnouncement,
		ResourceHealthCheck,
		ResourceEmployee,
		ResourceUnknown,
	}

	seen := make(map[Resource]struct{}, len(resources))
	for _, r := range resources {
		if r == "" && r != ResourceUnknown {
			t.Errorf("Resource should not be empty (except ResourceUnknown)")
		}
		if _, exists := seen[r]; exists {
			t.Errorf("Duplicate resource: %s", r)
		}
		seen[r] = struct{}{}
	}
}

// ── Action 命名规范验证 ─────────────────────────────────

func TestActions_ContainDot(t *testing.T) {
	// 所有 Action 必须符合 "resource.action" 格式
	actions := []Action{
		ActionServiceKeyCreate, ActionServiceKeyDelete,
		ActionProviderCreate, ActionProviderDelete,
		ActionModelCreate, ActionModelUpdate,
		ActionGroupCreate, ActionKeyCreate,
		ActionUserLogin, ActionUserLogout,
	}
	for _, a := range actions {
		s := string(a)
		if len(s) < 3 {
			t.Errorf("action too short: %q", s)
			continue
		}
		if s[0] == '.' || s[len(s)-1] == '.' {
			t.Errorf("action starts or ends with dot: %q", s)
			continue
		}
		hasMiddleDot := false
		for _, c := range s[1 : len(s)-1] {
			if c == '.' {
				hasMiddleDot = true
				break
			}
		}
		if !hasMiddleDot {
			t.Errorf("action must be in form 'resource.action': %q", s)
		}
	}
}
