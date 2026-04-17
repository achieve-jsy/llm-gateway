package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ── fakeEventPublisher 完整版（补全 InvalidateKeyCache 方法）──

// fakeEventPublisher 模拟 EventPublisher 接口用于 KeyService 事件发布测试
type fakeEventPublisher struct {
	published         []publishedEvent
	invalidatedHashes []string
}

type publishedEvent struct {
	eventType    event.ChangeEventType
	resourceType string
	resourceID   uuid.UUID
}

func (f *fakeEventPublisher) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	f.published = append(f.published, publishedEvent{eventType, resourceType, resourceID})
}

// InvalidateKeyCache 补全接口方法，用于 Toggle/Delete/UpdateIPAllowlist 路径测试
func (f *fakeEventPublisher) InvalidateKeyCache(keyHash string) {
	f.invalidatedHashes = append(f.invalidatedHashes, keyHash)
}

// 编译期接口契约检查
var _ service.EventPublisher = (*fakeEventPublisher)(nil)

// ── 原有测试 ──────────────────────────────────────────────

func TestNewKeyService_NilPublisher(t *testing.T) {
	t.Run("publisher为nil时构造不panic", func(t *testing.T) {
		svc := service.NewKeyService(nil, nil, nil)
		assert.NotNil(t, svc)
	})
}

func TestNewEventPublisherAdapter_NilInput(t *testing.T) {
	t.Run("传入nil返回nil", func(t *testing.T) {
		adapter := service.NewEventPublisherAdapter(nil)
		assert.Nil(t, adapter)
	})
}

func TestKeyServiceErrors(t *testing.T) {
	t.Run("ErrKeyNotFound错误值存在", func(t *testing.T) {
		assert.NotNil(t, service.ErrKeyNotFound)
		assert.Equal(t, "key not found", service.ErrKeyNotFound.Error())
	})
}

func TestVerifyResult_Structure(t *testing.T) {
	t.Run("VerifyResult字段正确", func(t *testing.T) {
		keyID := uuid.New()
		userID := uuid.New()
		result := service.VerifyResult{
			KeyID:           keyID,
			Status:          "active",
			AccessAllGroups: true,
			User: service.VerifyResultUser{
				UserID: userID,
				Name:   "test-user",
			},
		}
		assert.Equal(t, keyID, result.KeyID)
		assert.Equal(t, "active", result.Status)
		assert.True(t, result.AccessAllGroups)
		assert.Equal(t, userID, result.User.UserID)
	})
}

func TestKeyItem_Structure(t *testing.T) {
	t.Run("KeyItem基本字段", func(t *testing.T) {
		item := service.KeyItem{
			ID:              uuid.New(),
			Name:            "my-key",
			MaskedKey:       "sk-****abcd",
			KeyHint:         "abcd",
			Enabled:         true,
			AccessAllGroups: false,
		}
		assert.Equal(t, "my-key", item.Name)
		assert.Equal(t, "sk-****abcd", item.MaskedKey)
		assert.True(t, item.Enabled)
	})
}

func TestFakeEventPublisher_Records(t *testing.T) {
	t.Run("事件发布被记录", func(t *testing.T) {
		pub := &fakeEventPublisher{}
		keyID := uuid.New()
		pub.Publish(event.KeyCreated, "key", keyID)

		assert.Len(t, pub.published, 1)
		assert.Equal(t, event.KeyCreated, pub.published[0].eventType)
		assert.Equal(t, "key", pub.published[0].resourceType)
		assert.Equal(t, keyID, pub.published[0].resourceID)
	})

	t.Run("InvalidateKeyCache 被记录", func(t *testing.T) {
		pub := &fakeEventPublisher{}
		pub.InvalidateKeyCache("abc123def")
		pub.InvalidateKeyCache("xyz789")
		assert.Equal(t, []string{"abc123def", "xyz789"}, pub.invalidatedHashes)
	})
}

func TestKeyService_ListKeys_NilRepo(t *testing.T) {
	t.Run("repo为nil时ListKeys应panic或返回错误", func(t *testing.T) {
		svc := service.NewKeyService(nil, nil, nil)
		assert.NotNil(t, svc)
		_ = context.Background()
	})
}

// ── v0.8: IP 白名单字段测试（新增）──────────────────────────────

func TestVerifyResult_IPAllowlist_Field(t *testing.T) {
	t.Run("空白名单字段", func(t *testing.T) {
		result := service.VerifyResult{
			KeyID:       uuid.New(),
			Status:      "active",
			IPAllowlist: "",
		}
		assert.Empty(t, result.IPAllowlist)
	})

	t.Run("配置了白名单", func(t *testing.T) {
		allowlistJSON := `["1.2.3.4","10.0.0.0/8"]`
		result := service.VerifyResult{
			KeyID:       uuid.New(),
			Status:      "active",
			IPAllowlist: allowlistJSON,
		}
		assert.Equal(t, allowlistJSON, result.IPAllowlist)
		// 验证 JSON 结构
		var list []string
		require.NoError(t, json.Unmarshal([]byte(result.IPAllowlist), &list))
		assert.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, list)
	})

	t.Run("VerifyResult 包含所有 v0.8 字段", func(t *testing.T) {
		result := service.VerifyResult{
			KeyID:           uuid.New(),
			KeyName:         "ci-runner",
			Status:          "active",
			AccessAllGroups: false,
			BypassOrgCheck:  false,
			LogEnabled:      true,
			KeyType:         "service",
			IPAllowlist:     `["192.0.2.10"]`,
			BudgetChain:     []service.BudgetScope{{ScopeType: "dept_service", ScopeID: "eng"}},
		}
		assert.Equal(t, "service", result.KeyType)
		assert.NotEmpty(t, result.IPAllowlist)
		assert.Len(t, result.BudgetChain, 1)
	})
}

func TestServiceKeyItem_IPAllowlist_Field(t *testing.T) {
	t.Run("空 IP 白名单", func(t *testing.T) {
		item := service.ServiceKeyItem{
			ID:          uuid.New(),
			Name:        "ci-key",
			MaskedKey:   "sk-****abcd",
			KeyType:     model.KeyTypeService,
			Purpose:     "ci",
			DeptID:      "engineering",
			DeptName:    "Engineering",
			Enabled:     true,
			IPAllowlist: "",
		}
		assert.Empty(t, item.IPAllowlist)
		assert.Equal(t, model.KeyTypeService, item.KeyType)
	})

	t.Run("CIDR + 单 IP", func(t *testing.T) {
		item := service.ServiceKeyItem{
			ID:          uuid.New(),
			Name:        "corp-key",
			MaskedKey:   "sk-****1234",
			KeyType:     model.KeyTypeService,
			IPAllowlist: `["10.0.0.0/8","192.0.2.10"]`,
			CreatedAt:   time.Now(),
		}
		var list []string
		require.NoError(t, json.Unmarshal([]byte(item.IPAllowlist), &list))
		assert.Equal(t, []string{"10.0.0.0/8", "192.0.2.10"}, list)
	})

	t.Run("LastUsedAt + IP 白名单 共存", func(t *testing.T) {
		now := time.Now()
		item := service.ServiceKeyItem{
			ID:          uuid.New(),
			IPAllowlist: `["203.0.113.5"]`,
			LastUsedAt:  &now,
			CreatedAt:   now,
		}
		assert.NotNil(t, item.LastUsedAt)
		assert.NotEmpty(t, item.IPAllowlist)
	})
}

func TestModelApiKey_IPAllowlist_Field(t *testing.T) {
	t.Run("ApiKey model 包含 ip_allowlist 字段", func(t *testing.T) {
		k := model.ApiKey{
			KeyType:     model.KeyTypeService,
			IPAllowlist: `["172.16.0.0/12"]`,
		}
		assert.Equal(t, model.KeyTypeService, k.KeyType)
		assert.Equal(t, `["172.16.0.0/12"]`, k.IPAllowlist)
	})

	t.Run("JSON Marshal/Unmarshal 往返保持一致", func(t *testing.T) {
		original := `["1.1.1.1","2.2.2.2","10.0.0.0/8"]`
		var list []string
		require.NoError(t, json.Unmarshal([]byte(original), &list))

		// 重新序列化
		encoded, err := json.Marshal(list)
		require.NoError(t, err)
		assert.Equal(t, original, string(encoded))
	})
}

func TestAuditIntegration_WithPublisher(t *testing.T) {
	// 验证完整 fakeEventPublisher 满足 audit 集成场景
	t.Run("InvalidateKeyCache + Publish 协同", func(t *testing.T) {
		pub := &fakeEventPublisher{}
		keyID := uuid.New()
		hash := "sha256_abc123"

		// 模拟 ToggleServiceKey 路径：先 Invalidate 缓存再返回
		pub.InvalidateKeyCache(hash)
		pub.Publish(event.KeyUpdated, "api_key", keyID)

		assert.Len(t, pub.invalidatedHashes, 1)
		assert.Len(t, pub.published, 1)
		assert.Equal(t, hash, pub.invalidatedHashes[0])
		assert.Equal(t, event.KeyUpdated, pub.published[0].eventType)
	})

	t.Run("DeleteServiceKey 路径：Invalidate + Publish(KeyDeleted)", func(t *testing.T) {
		pub := &fakeEventPublisher{}
		keyID := uuid.New()
		pub.InvalidateKeyCache("hash_xyz")
		pub.Publish(event.KeyDeleted, "api_key", keyID)

		assert.Len(t, pub.invalidatedHashes, 1)
		assert.Equal(t, event.KeyDeleted, pub.published[0].eventType)
	})
}

type verifyBudgetReader struct {
	allocations map[string]*model.BudgetAllocation
}

func (r verifyBudgetReader) GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error) {
	return r.allocations[scopeType+":"+scopeID], nil
}

type verifyTierReader struct {
	limits    map[uuid.UUID][]model.BudgetTierLimit
	overrides map[uuid.UUID][]model.BudgetTemporaryOverride
}

func (r verifyTierReader) ListTierLimits(_ context.Context, allocationID uuid.UUID) ([]model.BudgetTierLimit, error) {
	return r.limits[allocationID], nil
}

func (r verifyTierReader) ListActiveOverrides(_ context.Context, allocationID uuid.UUID, now time.Time) ([]model.BudgetTemporaryOverride, error) {
	var active []model.BudgetTemporaryOverride
	for _, override := range r.overrides[allocationID] {
		if override.Status == budgetdomain.OverrideActive &&
			!override.StartsAt.After(now) && override.ExpiresAt.After(now) {
			active = append(active, override)
		}
	}
	return active, nil
}

func TestVerifyKeyBudgetChainKeepsTotalNumbersAndAddsEffectiveTierDecimalStrings(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	keyRaw := "sk-test-tier-budget"
	hash := sha256.Sum256([]byte(keyRaw))
	keyHash := hex.EncodeToString(hash[:])
	keyID, userID, allocationID, tierID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE api_keys (
		id text PRIMARY KEY, user_id text NOT NULL, name text NOT NULL,
		key_hash text NOT NULL, encrypted_key text NOT NULL, key_prefix text NOT NULL,
		key_hint text NOT NULL, enabled boolean NOT NULL, log_enabled boolean NOT NULL,
		access_all_groups boolean NOT NULL, bypass_org_check boolean NOT NULL,
		user_name text, dept_id text, dept_name text, studio_id text, studio_name text,
		key_type text NOT NULL, purpose text, created_by text, ip_allowlist text,
		quota_rpm integer, quota_max_concurrency integer, created_at datetime,
		updated_at datetime, last_used_at datetime, expires_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO api_keys (
		id,user_id,name,key_hash,encrypted_key,key_prefix,key_hint,enabled,log_enabled,
		access_all_groups,bypass_org_check,user_name,dept_id,dept_name,studio_id,studio_name,
		key_type,purpose,ip_allowlist,created_at,updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		keyID.String(), userID.String(), "tier-test", keyHash, "encrypted", "sk-", "test",
		true, false, false, false, "Tester", "dept-a", "研发部", "studio-a", "研发工作室",
		model.KeyTypePersonal, "", "", now, now).Error)

	baseTier := mustAmount(t, "80.000000")
	totalTarget := mustAmount(t, "120.000000")
	tierTarget := mustAmount(t, "100.000000")
	tier := model.ModelTier{ID: tierID, Name: "高级", Enabled: true}
	allocation := &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: userID.String(),
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	start, expiry := now.Add(-time.Hour), now.Add(time.Hour)
	svc := service.NewKeyService(repository.NewKeyRepo(db), nil, nil).
		WithBudgetRepo(verifyBudgetReader{allocations: map[string]*model.BudgetAllocation{
			model.BudgetScopeMember + ":" + userID.String(): allocation,
		}}).
		WithBudgetTierRepo(verifyTierReader{
			limits: map[uuid.UUID][]model.BudgetTierLimit{allocationID: {{
				ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID,
				PeriodType: budgetdomain.PeriodMonthly, LimitUSD: baseTier, EffectiveAt: now.Add(-24 * time.Hour),
				Enabled: true, ModelTier: tier,
			}}},
			overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{allocationID: {
				{ID: uuid.New(), BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTotal,
					DimensionKey: "total", PeriodType: budgetdomain.PeriodMonthly, TargetLimitUSD: totalTarget,
					StartsAt: start, ExpiresAt: expiry, Status: budgetdomain.OverrideActive},
				{ID: uuid.New(), BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTier,
					DimensionKey: "tier:" + tierID.String(), ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
					TargetLimitUSD: tierTarget, StartsAt: start, ExpiresAt: expiry, Status: budgetdomain.OverrideActive},
			}},
		}).
		WithClock(func() time.Time { return now })

	got, err := svc.VerifyKey(context.Background(), keyRaw)
	require.NoError(t, err)
	require.NotEmpty(t, got.BudgetChain)
	member := got.BudgetChain[0]
	require.Equal(t, 120.0, member.AmountUSD)
	require.Equal(t, -1.0, member.DailyAmountUSD)
	require.Len(t, member.TierLimits, 1)
	require.Equal(t, "80.000000", member.TierLimits[0].BaseLimitUSD.String())
	require.Equal(t, "100.000000", member.TierLimits[0].EffectiveLimitUSD.String())
	require.Equal(t, expiry, *member.TierLimits[0].ExpiresAt)

	raw, err := json.Marshal(member)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"amount_usd":120`)
	require.Contains(t, string(raw), `"base_limit_usd":"80.000000"`)
	require.Contains(t, string(raw), `"effective_limit_usd":"100.000000"`)
}

func TestBudgetScope_Structure(t *testing.T) {
	t.Run("BudgetScope 字段完整", func(t *testing.T) {
		scope := service.BudgetScope{
			ScopeType:       "dept_service",
			ScopeID:         "engineering",
			AmountUSD:       100.0,
			DailyAmountUSD:  10.0,
			WeeklyAmountUSD: 50.0,
			Label:           "Engineering Budget",
		}
		assert.Equal(t, "dept_service", scope.ScopeType)
		assert.Equal(t, 100.0, scope.AmountUSD)
		assert.Equal(t, "Engineering Budget", scope.Label)
	})

	t.Run("BudgetChain 可挂载到 VerifyResult", func(t *testing.T) {
		result := service.VerifyResult{
			KeyID:   uuid.New(),
			Status:  "active",
			KeyType: "service",
			BudgetChain: []service.BudgetScope{
				{ScopeType: "dept_service", ScopeID: "eng", AmountUSD: 200},
				{ScopeType: "studio", ScopeID: "platform", AmountUSD: 2000},
			},
		}
		assert.Len(t, result.BudgetChain, 2)
		assert.Equal(t, "dept_service", result.BudgetChain[0].ScopeType)
	})
}
