package quota

import (
	"testing"

	"github.com/example/llm-gateway/internal/core/quota"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
)

func TestResolveSubject_PersonalKey(t *testing.T) {
	id := &gwmodel.UserIdentity{UserID: "user-123", KeyID: "key-456", KeyType: "personal"}
	stype, sid := ResolveSubject(id)
	if stype != "user" {
		t.Errorf("expected subject_type=user, got %s", stype)
	}
	if sid != "user-123" {
		t.Errorf("expected subject_id=user-123, got %s", sid)
	}
}

func TestResolveSubject_ServiceKey(t *testing.T) {
	id := &gwmodel.UserIdentity{UserID: "user-123", KeyID: "key-456", KeyType: "service"}
	stype, sid := ResolveSubject(id)
	if stype != "service_key" {
		t.Errorf("expected subject_type=service_key, got %s", stype)
	}
	if sid != "key-456" {
		t.Errorf("expected subject_id=key-456, got %s", sid)
	}
}

func TestResolveSubjectLimits_NilSnapshot_User(t *testing.T) {
	r := ResolveSubjectLimits(nil, "user", "user-123")
	if r.RPM != 60 || r.MaxConcurrency != 10 || r.Source != "default_user" {
		t.Errorf("expected fallback 60/10/default_user, got %+v", r)
	}
}

func TestResolveSubjectLimits_NilSnapshot_ServiceKey(t *testing.T) {
	r := ResolveSubjectLimits(nil, "service_key", "key-456")
	if r.RPM != 120 || r.MaxConcurrency != 20 || r.Source != "default_service_key" {
		t.Errorf("expected fallback 120/20/default_service_key, got %+v", r)
	}
}

func TestResolveSubjectLimits_UserDefault(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		DefaultUser:       quota.LayerLimits{RPM: 30, MaxConcurrency: 5},
		DefaultServiceKey: quota.LayerLimits{RPM: 60, MaxConcurrency: 10},
		Users:             map[string]quota.LayerLimits{},
	}
	r := ResolveSubjectLimits(snap, "user", "user-123")
	if r.RPM != 30 || r.MaxConcurrency != 5 || r.Source != "default_user" {
		t.Errorf("expected 30/5/default_user, got %+v", r)
	}
}

func TestResolveSubjectLimits_UserOverride(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 30, MaxConcurrency: 5},
		Users: map[string]quota.LayerLimits{
			"user-123": {RPM: 100, MaxConcurrency: 20},
		},
	}
	r := ResolveSubjectLimits(snap, "user", "user-123")
	if r.RPM != 100 || r.MaxConcurrency != 20 || r.Source != "override" {
		t.Errorf("expected 100/20/override, got %+v", r)
	}
}

func TestResolveSubjectLimits_ServiceKeyOverride(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		DefaultServiceKey: quota.LayerLimits{RPM: 120, MaxConcurrency: 20},
		ServiceKeys: map[string]quota.LayerLimits{
			"key-456": {RPM: 50, MaxConcurrency: 5},
		},
	}
	r := ResolveSubjectLimits(snap, "service_key", "key-456")
	if r.RPM != 50 || r.MaxConcurrency != 5 || r.Source != "override" {
		t.Errorf("expected 50/5/override, got %+v", r)
	}
}

func TestResolveSubjectLimits_ServiceKeyDefault(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		DefaultServiceKey: quota.LayerLimits{RPM: 120, MaxConcurrency: 20},
		ServiceKeys:       map[string]quota.LayerLimits{},
	}
	r := ResolveSubjectLimits(snap, "service_key", "key-unknown")
	if r.RPM != 120 || r.MaxConcurrency != 20 || r.Source != "default_service_key" {
		t.Errorf("expected 120/20/default_service_key, got %+v", r)
	}
}

func TestResolveGroupLimits_NilSnapshot(t *testing.T) {
	r := ResolveGroupLimits(nil, "gpt-4")
	if r.RPM != 0 || r.MaxConcurrency != 0 || r.Source != "unset" {
		t.Errorf("expected 0/0/unset, got %+v", r)
	}
}

func TestResolveGroupLimits_Unset(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		Groups: map[string]quota.LayerLimits{},
	}
	r := ResolveGroupLimits(snap, "gpt-4")
	if r.RPM != 0 || r.MaxConcurrency != 0 || r.Source != "unset" {
		t.Errorf("expected 0/0/unset, got %+v", r)
	}
}

func TestResolveGroupLimits_Override(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		Groups: map[string]quota.LayerLimits{
			"gpt-4": {RPM: 30, MaxConcurrency: 5},
		},
	}
	r := ResolveGroupLimits(snap, "gpt-4")
	if r.RPM != 30 || r.MaxConcurrency != 5 || r.Source != "override" {
		t.Errorf("expected 30/5/override, got %+v", r)
	}
}

func TestResolveModelLimits_NilSnapshot(t *testing.T) {
	r := ResolveModelLimits(nil, "model-uuid")
	if r.RPM != 0 || r.MaxConcurrency != 0 || r.Source != "unset" {
		t.Errorf("expected 0/0/unset, got %+v", r)
	}
}

func TestResolveModelLimits_Override(t *testing.T) {
	snap := &quota.QuotaSnapshot{
		Models: map[string]quota.LayerLimits{
			"model-uuid": {RPM: 10, MaxConcurrency: 2},
		},
	}
	r := ResolveModelLimits(snap, "model-uuid")
	if r.RPM != 10 || r.MaxConcurrency != 2 || r.Source != "override" {
		t.Errorf("expected 10/2/override, got %+v", r)
	}
}

func TestSnapshotHolder_SetAndGet(t *testing.T) {
	h := NewSnapshotHolder()
	if h.Get() != nil {
		t.Error("expected nil snapshot initially")
	}

	snap := &quota.QuotaSnapshot{
		Version:     1,
		DefaultUser: quota.LayerLimits{RPM: 60, MaxConcurrency: 10},
	}
	h.Set(snap)

	got := h.Get()
	if got == nil || got.Version != 1 || got.DefaultUser.RPM != 60 {
		t.Errorf("unexpected snapshot: %+v", got)
	}

	// Replace
	snap2 := &quota.QuotaSnapshot{Version: 2}
	h.Set(snap2)
	got = h.Get()
	if got == nil || got.Version != 2 {
		t.Errorf("expected version 2, got %+v", got)
	}
}
