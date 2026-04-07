package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

var (
	ErrKeyNotFound = errors.New("key not found")
)

type KeyItem struct {
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	MaskedKey       string           `json:"masked_key"`
	KeyHint         string           `json:"key_hint"`
	Enabled         bool             `json:"enabled"`
	LogEnabled      bool             `json:"log_enabled"`
	AccessAllGroups bool             `json:"access_all_groups"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty"`
	BoundGroups     []BoundGroupItem `json:"bound_groups,omitempty"`
}

type KeyDetail struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	MaskedKey string    `json:"masked_key"`
	KeyHint   string    `json:"key_hint"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type VerifyResultUser struct {
	UserID       uuid.UUID `json:"user_id"`
	Name         string    `json:"name"`
	DepartmentID string    `json:"department_id,omitempty"` // SSO dept_id 原始字符串，不做 UUID 解析（避免大小写被标准化）
	DeptName     string    `json:"dept_name,omitempty"`
	StudioID     string    `json:"studio_id,omitempty"`
	StudioName   string    `json:"studio_name,omitempty"`
}

type VerifyResult struct {
	KeyID           uuid.UUID        `json:"key_id"`
	KeyName         string           `json:"key_name,omitempty"`
	Status          string           `json:"status"`
	AccessAllGroups bool             `json:"access_all_groups"`
	BypassOrgCheck  bool             `json:"bypass_org_check"`
	LogEnabled      bool             `json:"log_enabled"`
	User            VerifyResultUser `json:"user"`
	// v0.7: 模型组授权快照。public ∪ 本部门已开通 controlled 的组名集合。
	UsableGroups []string `json:"usable_groups,omitempty"`
	// v0.7: Key 类型（personal/service）与预算链（scope 列表，不含实时 used）。
	KeyType     string        `json:"key_type,omitempty"`
	BudgetChain []BudgetScope `json:"budget_chain,omitempty"`
	// v0.8: IP 白名单（JSON 数组字符串）
	IPAllowlist string `json:"ip_allowlist,omitempty"`
}

// BudgetScope 预算链中单层 scope 的标识与元数据（不含实时 used，used 实时从 Redis 读）。
type BudgetScope struct {
	ScopeType       string                 `json:"scope_type"` // company|studio|dept_daily|dept_service|member
	ScopeID         string                 `json:"scope_id"`
	AmountUSD       float64                `json:"amount_usd"`        // 月限额（-1 表示未配置/不限额）
	DailyAmountUSD  float64                `json:"daily_amount_usd"`  // 日限额（-1 表示不限）
	WeeklyAmountUSD float64                `json:"weekly_amount_usd"` // 周限额（-1 表示不限）
	Label           string                 `json:"label"`             // 展示用标签，如「研发部·成员日常预算」
	TierLimits      []BudgetScopeTierLimit `json:"tier_limits,omitempty"`
}

type BudgetScopeTierLimit struct {
	TierID            uuid.UUID               `json:"tier_id"`
	TierName          string                  `json:"tier_name"`
	PeriodType        budgetdomain.PeriodType `json:"period_type"`
	BaseLimitUSD      budgetdomain.Amount     `json:"base_limit_usd"`
	EffectiveLimitUSD budgetdomain.Amount     `json:"effective_limit_usd"`
	Enabled           bool                    `json:"enabled"`
	EffectiveAt       time.Time               `json:"effective_at"`
	ExpiresAt         *time.Time              `json:"expires_at,omitempty"`
}

type keyBudgetReader interface {
	GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error)
}

type keyBudgetTierReader interface {
	ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error)
	ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error)
}

type EventPublisher interface {
	Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID)
	InvalidateKeyCache(keyHash string)
}

type publisherAdapter struct {
	p *event.Publisher
}

func (a *publisherAdapter) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	if a.p == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID, 0)
	if err := a.p.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish event", "type", eventType, "resource_id", resourceID, "error", err)
	}
}

func (a *publisherAdapter) InvalidateKeyCache(keyHash string) {
	if a.p == nil {
		return
	}
	a.p.InvalidateKeyAuthCache(context.Background(), keyHash)
}

func NewEventPublisherAdapter(p *event.Publisher) EventPublisher {
	if p == nil {
		return nil
	}
	return &publisherAdapter{p: p}
}

type KeyService struct {
	repo           *repository.KeyRepo
	keyGroupSvc    *KeyGroupService
	publisher      EventPublisher
	entitlementSvc EntitlementComputer // v0.8: 授权计算接口
	budgetRepo     keyBudgetReader     // v0.7: 预算链构造，可选
	budgetTierRepo keyBudgetTierReader
	clock          func() time.Time
}

func (s *KeyService) WithBudgetTierRepo(repo keyBudgetTierReader) *KeyService {
	s.budgetTierRepo = repo
	return s
}

func NewKeyService(repo *repository.KeyRepo, keyGroupSvc *KeyGroupService, publisher EventPublisher) *KeyService {
	return &KeyService{repo: repo, keyGroupSvc: keyGroupSvc, publisher: publisher, clock: time.Now}
}

// SetEntitlementService 注入授权服务，用于 verify-key 下发 usable_groups 快照。
func (s *KeyService) SetEntitlementService(svc EntitlementComputer) {
	s.entitlementSvc = svc
}

// WithBudgetRepo 注入预算 repo（v0.7 新增），用于 verify-key 下发 BudgetChain。
func (s *KeyService) WithBudgetRepo(repo keyBudgetReader) *KeyService {
	s.budgetRepo = repo
	return s
}

func (s *KeyService) WithClock(clock func() time.Time) *KeyService {
	if clock != nil {
		s.clock = clock
	}
	return s
}

func (s *KeyService) CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time, groupIDs []uint) (*model.ApiKey, string, []BoundGroupItem, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", nil, fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]

	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])

	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encrypt key: %w", err)
	}

	// 冗余写入用户/部门名称，供 verify-key 直接返回，无需 JOIN
	info, infoErr := s.repo.GetUserDeptInfo(userID)
	if infoErr != nil {
		slog.Warn("create key: failed to fetch user/dept info", "user_id", userID, "error", infoErr)
	}

	k := &model.ApiKey{
		UserID:       userID,
		Name:         name,
		KeyHash:      keyHash,
		EncryptedKey: encryptedKey,
		KeyPrefix:    "sk-",
		KeyHint:      keyHint,
		Enabled:      true,
		ExpiresAt:    expiresAt,
		UserName:     info.UserName,
		DeptID:       info.DeptID,
		DeptName:     info.DeptName,
		StudioID:     info.StudioID,
		StudioName:   info.StudioName,
	}

	if err := s.repo.Create(k); err != nil {
		return nil, "", nil, fmt.Errorf("create key: %w", err)
	}

	var boundGroups []BoundGroupItem
	if len(groupIDs) > 0 && s.keyGroupSvc != nil {
		if err := s.keyGroupSvc.BindGroups(ctx, nil, k.ID, groupIDs); err != nil {
			slog.Warn("create key: bind groups failed", "key_id", k.ID, "error", err)
		}
	}
	if s.keyGroupSvc != nil {
		groups, err := s.keyGroupSvc.GetKeyGroups(ctx, k.ID)
		if err != nil {
			slog.Warn("create key: get key groups failed", "key_id", k.ID, "error", err)
		} else {
			boundGroups = groups
		}
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "api_key", k.ID)
	}

	return k, keyStr, boundGroups, nil
}

func (s *KeyService) ListKeys(ctx context.Context, userID uuid.UUID, page, size int) ([]KeyItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	offset := (page - 1) * size

	keys, total, err := s.repo.ListByUserID(userID, offset, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list keys: %w", err)
	}

	items := make([]KeyItem, 0, len(keys))
	for _, k := range keys {
		var boundGroups []BoundGroupItem
		if s.keyGroupSvc != nil {
			groups, err := s.keyGroupSvc.GetKeyGroups(ctx, k.ID)
			if err != nil {
				slog.Warn("list keys: get key groups failed", "key_id", k.ID, "error", err)
			} else {
				boundGroups = groups
			}
		}
		items = append(items, KeyItem{
			ID:              k.ID,
			Name:            k.Name,
			MaskedKey:       k.KeyPrefix + "****" + k.KeyHint,
			KeyHint:         k.KeyHint,
			Enabled:         k.Enabled,
			LogEnabled:      k.LogEnabled,
			AccessAllGroups: k.AccessAllGroups,
			CreatedAt:       k.CreatedAt,
			UpdatedAt:       k.UpdatedAt,
			LastUsedAt:      k.LastUsedAt,
			ExpiresAt:       k.ExpiresAt,
			BoundGroups:     boundGroups,
		})
	}

	return items, total, nil
}

func (s *KeyService) GetKey(ctx context.Context, userID, keyID uuid.UUID) (*KeyDetail, []BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, nil, ErrKeyNotFound
	}

	var boundGroups []BoundGroupItem
	if s.keyGroupSvc != nil {
		groups, err := s.keyGroupSvc.GetKeyGroups(ctx, keyID)
		if err != nil {
			slog.Warn("get key: get key groups failed", "key_id", keyID, "error", err)
		} else {
			boundGroups = groups
		}
	}

	return &KeyDetail{
		ID:        k.ID,
		Name:      k.Name,
		MaskedKey: k.KeyPrefix + "****" + k.KeyHint,
		KeyHint:   k.KeyHint,
		Enabled:   k.Enabled,
		CreatedAt: k.CreatedAt,
		UpdatedAt: k.UpdatedAt,
	}, boundGroups, nil
}

func (s *KeyService) UpdateKeyStatus(ctx context.Context, userID, keyID uuid.UUID, enabled bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}

	if err := s.repo.UpdateStatus(keyID, enabled); err != nil {
		return nil, fmt.Errorf("update key status: %w", err)
	}
	k.Enabled = enabled

	if s.publisher != nil {
		// FIX(v0.7): resource_type 统一为 "api_key"，与 KeyCache.HandleChangeEvent 期望一致；
		// 同时补调 InvalidateKeyCache（Redis Del + pub/sub），保证「即时生效」承诺不落空。
		s.publisher.InvalidateKeyCache(k.KeyHash)
		if enabled {
			s.publisher.Publish(event.KeyEnabled, "api_key", keyID)
		} else {
			s.publisher.Publish(event.KeyDisabled, "api_key", keyID)
		}
	}

	return k, nil
}

func (s *KeyService) DeleteKey(ctx context.Context, userID, keyID uuid.UUID) error {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return ErrKeyNotFound
	}
	if k.UserID != userID {
		return ErrKeyNotFound
	}

	if err := s.repo.Delete(keyID); err != nil {
		return fmt.Errorf("delete key: %w", err)
	}

	if s.publisher != nil {
		// FIX(v0.7): 统一 resource_type="api_key" + 主动失效缓存
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyDeleted, "api_key", keyID)
	}

	return nil
}

func (s *KeyService) RevealKey(ctx context.Context, userID, keyID uuid.UUID) (string, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return "", ErrKeyNotFound
	}
	if k.UserID != userID {
		return "", ErrKeyNotFound
	}

	plaintext, err := crypto.Decrypt(k.EncryptedKey)
	if err != nil {
		return "", fmt.Errorf("decrypt key: %w", err)
	}
	return plaintext, nil
}

func (s *KeyService) ResetKey(ctx context.Context, userID, keyID uuid.UUID) (*model.ApiKey, string, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, "", ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, "", ErrKeyNotFound
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]
	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])
	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt key: %w", err)
	}

	k.KeyHash = keyHash
	k.EncryptedKey = encryptedKey
	k.KeyHint = keyHint
	if err := s.repo.UpdateHashAndKey(k.ID, keyHash, encryptedKey, keyHint); err != nil {
		return nil, "", fmt.Errorf("update key: %w", err)
	}

	if s.publisher != nil {
		// FIX(v0.7): 统一 resource_type="api_key" + 失效旧 hash 缓存（重置后旧 hash 作废）
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyCreated, "api_key", keyID)
	}

	return k, keyStr, nil
}

func (s *KeyService) ListKeyGroups(ctx context.Context, userID, keyID uuid.UUID) ([]BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}
	if s.keyGroupSvc == nil {
		return nil, nil
	}
	return s.keyGroupSvc.GetKeyGroups(ctx, keyID)
}

func (s *KeyService) UpdateKeyGroups(ctx context.Context, userID, keyID uuid.UUID, add, remove []uint) ([]BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}
	if s.keyGroupSvc == nil {
		return nil, fmt.Errorf("key group service not available")
	}
	return s.keyGroupSvc.UpdateBindings(ctx, keyID, add, remove)
}

func (s *KeyService) ListAvailableGroups(ctx context.Context) ([]AvailableGroupItem, error) {
	if s.keyGroupSvc == nil {
		return nil, nil
	}
	return s.keyGroupSvc.ListAvailableGroups(ctx)
}

func (s *KeyService) UpdateAccessAllGroups(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if err := s.repo.UpdateAccessAllGroups(keyID, value); err != nil {
		return nil, fmt.Errorf("update access_all_groups: %w", err)
	}
	k.AccessAllGroups = value
	return k, nil
}

func (s *KeyService) UpdateLogEnabled(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if err := s.repo.UpdateLogEnabled(keyID, value); err != nil {
		return nil, fmt.Errorf("update log_enabled: %w", err)
	}
	k.LogEnabled = value
	if s.publisher != nil {
		// 立即删除两层 Redis 缓存（auth service + gateway KeyCache），无需等 TTL
		s.publisher.InvalidateKeyCache(k.KeyHash)
		// 通知 gateway 订阅者清除本地内存缓存
		s.publisher.Publish(event.KeyLogChanged, "api_key", keyID)
	}
	return k, nil
}

func (s *KeyService) VerifyKey(ctx context.Context, keyRaw string) (*VerifyResult, error) {
	hash := sha256.Sum256([]byte(keyRaw))
	keyHash := hex.EncodeToString(hash[:])

	k, err := s.repo.FindByHash(keyHash)
	if err != nil {
		return nil, ErrKeyNotFound
	}

	status := "active"
	if !k.Enabled {
		status = "disabled"
	}
	now := s.clock()
	if k.ExpiresAt != nil && k.ExpiresAt.Before(now) {
		status = "disabled"
	}

	s.repo.TouchLastUsed(keyHash) // best-effort update

	// 懒补全：如果 key 的 org 快照字段缺失，同步补齐后再返回，
	// 确保本次请求就能拿到正确的 studio_id，不再需要等第二次请求。
	// 跳过条件：UserName 已有值且 DeptID 为空，说明该用户没有部门，查了也没用。
	needBackfill := k.UserName == "" || k.DeptName == "" || k.DeptID == "" || k.StudioName == "" || k.StudioID == ""
	hasNoDept := k.UserName != "" && k.DeptID == "" // 用户存在但确实没有部门，不查
	if needBackfill && !hasNoDept {
		info, err := s.repo.GetUserDeptInfo(k.UserID)
		if err == nil && info.UserName != "" {
			k.UserName = info.UserName
			k.DeptID = info.DeptID
			k.DeptName = info.DeptName
			k.StudioID = info.StudioID
			k.StudioName = info.StudioName
			// 异步写回 DB + 发失效事件（不阻塞本次响应）
			go func() {
				if err := s.repo.BackfillUserDeptNames(k.ID, info.UserName, info.DeptID, info.DeptName, info.StudioID, info.StudioName); err == nil {
					if s.publisher != nil {
						s.publisher.InvalidateKeyCache(k.KeyHash)
						s.publisher.Publish(event.KeyUpdated, "api_key", k.ID)
					}
				}
			}()
		}
	}

	// v0.7: 构造预算链（scope 列表 + 各层额度，不含实时 used）
	var budgetChain []BudgetScope
	if s.budgetRepo != nil {
		budgetChain = s.buildBudgetChain(ctx, k, now)
	}

	// v0.7: key_type（personal/service 默认 personal）
	keyType := k.KeyType
	if keyType == "" {
		keyType = model.KeyTypePersonal
	}

	result := &VerifyResult{
		KeyID:           k.ID,
		KeyName:         k.Name,
		Status:          status,
		AccessAllGroups: k.AccessAllGroups,
		BypassOrgCheck:  k.BypassOrgCheck,
		LogEnabled:      k.LogEnabled,
		KeyType:         keyType,
		BudgetChain:     budgetChain,
		IPAllowlist:     k.IPAllowlist,
		User: VerifyResultUser{
			UserID:       k.UserID,
			Name:         k.UserName,
			DepartmentID: k.DeptID,
			DeptName:     k.DeptName,
			StudioID:     k.StudioID,
			StudioName:   k.StudioName,
		},
	}
	if s.entitlementSvc != nil {
		if usableGroups, err := s.entitlementSvc.ComputeUsableGroups(ctx, k.DeptID, k.StudioID); err == nil {
			result.UsableGroups = usableGroups
		} else if k.DeptID != "" {
			slog.Warn("verify-key: failed to compute usable_groups, fallback to empty",
				"key_id", k.ID, "dept_id", k.DeptID, "error", err)
		}
	}
	return result, nil
}

// parseDeptID 将字符串形式的 dept_id 转换为 *uuid.UUID，空字符串返回 nil。
func parseDeptID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

// ─────────────────────────────────────────────────────────────────────────────
// v0.7 Service Key
// ─────────────────────────────────────────────────────────────────────────────

// ServiceKeyItem Service Key 列表项（不含明文 key）。
// ServiceKeyItem Service Key 列表项（不含明文 key）。
type ServiceKeyItem struct {
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	MaskedKey       string           `json:"masked_key"`
	KeyType         string           `json:"key_type"`
	Purpose         string           `json:"purpose"`
	DeptID          string           `json:"dept_id"`
	DeptName        string           `json:"dept_name"`
	Enabled         bool             `json:"enabled"`
	AccessAllGroups bool             `json:"access_all_groups"`
	BoundGroups     []BoundGroupItem `json:"bound_groups,omitempty"`
	IPAllowlist     string           `json:"ip_allowlist,omitempty"`
	CreatedBy       *uuid.UUID       `json:"created_by,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
}

// CreateServiceKey 创建 Service Key（仅部门负责人）。
// actorID：操作人 user_id（部门负责人）；deptID：归属部门 SSO dept_id。
// groupIDs 非空时绑定指定模型组；accessAllGroups=true 表示可用 usable_groups 全集（与个人 Key 一致）。
func (s *KeyService) CreateServiceKey(ctx context.Context, actorID uuid.UUID, deptID, name, purpose, ipAllowlist string, groupIDs []uint, accessAllGroups bool) (*model.ApiKey, string, []BoundGroupItem, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", nil, fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]
	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])
	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encrypt key: %w", err)
	}

	// 取部门/工作室名称（通过 actorID 找 userDeptInfo，再用 deptID 覆盖）
	info, _ := s.repo.GetUserDeptInfo(actorID)
	// deptID 是指定的归属部门，不一定和 actor 所在部门相同；名称通过 repo 补全
	deptInfo, _ := s.repo.GetDeptInfoByDeptID(deptID)

	k := &model.ApiKey{
		UserID:       actorID, // service key 的 user_id = 创建者（仅作归属/审计，不影响预算链）
		Name:         name,
		KeyHash:      keyHash,
		EncryptedKey: encryptedKey,
		KeyPrefix:    "sk-",
		KeyHint:      keyHint,
		Enabled:      true,
		KeyType:      model.KeyTypeService,
		Purpose:      purpose,
		CreatedBy:    &actorID,
		// 归属部门（决定预算链和授权范围）
		DeptID:     deptID,
		DeptName:   deptInfo.DeptName,
		StudioID:   deptInfo.StudioID,
		StudioName: deptInfo.StudioName,
		// v0.8: IP 白名单
		IPAllowlist: ipAllowlist,
		// 创建者信息（审计用）
		UserName: info.UserName,
	}

	if err := s.repo.Create(k); err != nil {
		return nil, "", nil, fmt.Errorf("create service key: %w", err)
	}

	if accessAllGroups {
		if err := s.repo.UpdateAccessAllGroups(k.ID, true); err != nil {
			slog.Warn("create service key: set access_all_groups failed", "key_id", k.ID, "error", err)
		} else {
			k.AccessAllGroups = true
		}
	} else if len(groupIDs) > 0 && s.keyGroupSvc != nil {
		if err := s.keyGroupSvc.BindGroups(ctx, nil, k.ID, groupIDs); err != nil {
			slog.Warn("create service key: bind groups failed", "key_id", k.ID, "error", err)
		}
	}

	var boundGroups []BoundGroupItem
	if s.keyGroupSvc != nil {
		groups, err := s.keyGroupSvc.GetKeyGroups(ctx, k.ID)
		if err != nil {
			slog.Warn("create service key: get key groups failed", "key_id", k.ID, "error", err)
		} else {
			boundGroups = groups
		}
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "api_key", k.ID)
		s.publisher.InvalidateKeyCache(k.KeyHash)
	}
	return k, keyStr, boundGroups, nil
}

// ListServiceKeys 列出某部门的所有 Service Key（不含明文）。
func (s *KeyService) ListServiceKeys(ctx context.Context, deptID string) ([]ServiceKeyItem, error) {
	keys, err := s.repo.ListByDeptAndType(deptID, model.KeyTypeService)
	if err != nil {
		return nil, err
	}
	return toServiceKeyItems(ctx, s, keys), nil
}

// ListServiceKeysPaged 分页列出 Service Key。
// allowedDepts 非 nil 时限制可见部门范围（用于非全局管理员）；deptID 非空时再细化到单部门。
func (s *KeyService) ListServiceKeysPaged(ctx context.Context, deptID string, allowedDepts []string, page, pageSize int) ([]ServiceKeyItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	keys, total, err := s.repo.ListByDeptAndTypePaged(deptID, allowedDepts, model.KeyTypeService, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return toServiceKeyItems(ctx, s, keys), total, nil
}

func toServiceKeyItems(ctx context.Context, svc *KeyService, keys []model.ApiKey) []ServiceKeyItem {
	items := make([]ServiceKeyItem, 0, len(keys))
	for _, k := range keys {
		var boundGroups []BoundGroupItem
		if svc.keyGroupSvc != nil {
			groups, err := svc.keyGroupSvc.GetKeyGroups(ctx, k.ID)
			if err != nil {
				slog.Warn("list service keys: get key groups failed", "key_id", k.ID, "error", err)
			} else {
				boundGroups = groups
			}
		}
		items = append(items, ServiceKeyItem{
			ID:              k.ID,
			Name:            k.Name,
			MaskedKey:       k.KeyPrefix + "****" + k.KeyHint,
			KeyType:         k.KeyType,
			Purpose:         k.Purpose,
			DeptID:          k.DeptID,
			DeptName:        k.DeptName,
			Enabled:         k.Enabled,
			AccessAllGroups: k.AccessAllGroups,
			BoundGroups:     boundGroups,
			IPAllowlist:     k.IPAllowlist,
			CreatedBy:       k.CreatedBy,
			CreatedAt:       k.CreatedAt,
			LastUsedAt:      k.LastUsedAt,
		})
	}
	return items
}

// UpdateServiceKeyGroups 更新 Service Key 的模型组绑定。
func (s *KeyService) UpdateServiceKeyGroups(ctx context.Context, keyID uuid.UUID, add, remove []uint) ([]BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil || k.KeyType != model.KeyTypeService {
		return nil, ErrKeyNotFound
	}
	if k.AccessAllGroups {
		return nil, fmt.Errorf("access_all_groups enabled, disable it before binding specific groups")
	}
	if s.keyGroupSvc == nil {
		return nil, fmt.Errorf("key group service not available")
	}
	groups, err := s.keyGroupSvc.UpdateBindings(ctx, keyID, add, remove)
	if err != nil {
		return nil, err
	}
	if s.publisher != nil {
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyUpdated, "api_key", keyID)
	}
	return groups, nil
}

// UpdateServiceKeyAccessAllGroups 切换 Service Key 的 access_all_groups。
func (s *KeyService) UpdateServiceKeyAccessAllGroups(ctx context.Context, keyID uuid.UUID, value bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil || k.KeyType != model.KeyTypeService {
		return nil, ErrKeyNotFound
	}
	if err := s.repo.UpdateAccessAllGroups(keyID, value); err != nil {
		return nil, fmt.Errorf("update access_all_groups: %w", err)
	}
	k.AccessAllGroups = value
	if s.publisher != nil {
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyUpdated, "api_key", keyID)
	}
	return k, nil
}

// DeleteServiceKey 吊销 Service Key（部门负责人或 admin）。
// 权限校验由 handler 层完成。
func (s *KeyService) DeleteServiceKey(ctx context.Context, keyID uuid.UUID) error {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return ErrKeyNotFound
	}
	if k.KeyType != model.KeyTypeService {
		return ErrKeyNotFound // 不允许用此接口删个人 Key
	}
	if err := s.repo.Delete(keyID); err != nil {
		return fmt.Errorf("delete service key: %w", err)
	}
	if s.publisher != nil {
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyDeleted, "api_key", keyID)
	}
	return nil
}

// ToggleServiceKey 启用或停用 Service Key。
// 权限校验由 handler 层完成。
func (s *KeyService) ToggleServiceKey(ctx context.Context, keyID uuid.UUID, enabled bool) error {
	k, err := s.repo.FindByID(keyID)
	if err != nil || k.KeyType != model.KeyTypeService {
		return ErrKeyNotFound
	}
	if err := s.repo.UpdateStatus(keyID, enabled); err != nil {
		return fmt.Errorf("toggle service key: %w", err)
	}
	if s.publisher != nil {
		s.publisher.InvalidateKeyCache(k.KeyHash)
	}
	return nil
}

// FindServiceKey 查找 Service Key（用于 handler 做 RBAC 前置校验）。
func (s *KeyService) FindServiceKey(ctx context.Context, keyID uuid.UUID) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil || k.KeyType != model.KeyTypeService {
		return nil, ErrKeyNotFound
	}
	return k, nil
}

// UpdateIPAllowlist 更新 Service Key 的 IP 白名单。
// allowlistJSON 必须是合法 JSON 数组字符串（如 `["1.2.3.4","10.0.0.0/8"]`）或空字符串。
// 权限校验由 handler 层完成。
func (s *KeyService) UpdateIPAllowlist(ctx context.Context, keyID uuid.UUID, allowlistJSON string) error {
	k, err := s.repo.FindByID(keyID)
	if err != nil || k.KeyType != model.KeyTypeService {
		return ErrKeyNotFound
	}
	if err := s.repo.UpdateIPAllowlist(keyID, allowlistJSON); err != nil {
		return fmt.Errorf("update ip allowlist: %w", err)
	}
	if s.publisher != nil {
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyUpdated, "api_key", keyID)
	}
	return nil
}

// buildBudgetChain 根据 key 类型构造预算链（各层 scope + 已配额度）。
// personal: member(有则含) → dept_daily → studio → company
// service:  dept_service → studio → company
func (s *KeyService) buildBudgetChain(ctx context.Context, k *model.ApiKey, now time.Time) []BudgetScope {
	type scopeDef struct {
		scopeType string
		scopeID   string
		label     string
	}
	var defs []scopeDef

	if k.KeyType == model.KeyTypeService {
		defs = []scopeDef{
			{model.BudgetScopeDeptService, k.DeptID, k.DeptName + "·程序服务预算"},
			{model.BudgetScopeStudio, k.StudioID, k.StudioName},
			{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"},
		}
	} else {
		// personal：member 单独设额时包含
		defs = []scopeDef{
			{model.BudgetScopeMember, k.UserID.String(), "我的额度"},
			{model.BudgetScopeDeptDaily, k.DeptID, k.DeptName + "·成员日常预算"},
			{model.BudgetScopeStudio, k.StudioID, k.StudioName},
			{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"},
		}
	}

	chain := make([]BudgetScope, 0, len(defs))
	for _, d := range defs {
		if d.scopeID == "" {
			continue
		}
		amount := float64(-1)       // -1 = 未配置/不限额
		dailyAmount := float64(-1)  // -1 = 不限
		weeklyAmount := float64(-1) // -1 = 不限
		alloc, _ := s.budgetRepo.GetAllocation(d.scopeType, d.scopeID)
		var tierLimits []BudgetScopeTierLimit
		if alloc != nil && alloc.Enabled {
			amount = alloc.AmountUSD
			dailyAmount = alloc.DailyAmountUSD
			weeklyAmount = alloc.WeeklyAmountUSD
			amount, dailyAmount, weeklyAmount, tierLimits = s.effectiveBudgetLimits(ctx, alloc, amount, dailyAmount, weeklyAmount, now)
		} else if d.scopeType == model.BudgetScopeMember {
			// 成员未单独设额则跳过（走 dept_daily 共享池）
			continue
		}
		chain = append(chain, BudgetScope{
			ScopeType:       d.scopeType,
			ScopeID:         d.scopeID,
			AmountUSD:       amount,
			DailyAmountUSD:  dailyAmount,
			WeeklyAmountUSD: weeklyAmount,
			Label:           d.label,
			TierLimits:      tierLimits,
		})
	}
	return chain
}

func (s *KeyService) effectiveBudgetLimits(ctx context.Context, allocation *model.BudgetAllocation, monthly, daily, weekly float64, now time.Time) (float64, float64, float64, []BudgetScopeTierLimit) {
	if s.budgetTierRepo == nil {
		return monthly, daily, weekly, nil
	}
	overrides, overrideErr := s.budgetTierRepo.ListActiveOverrides(ctx, allocation.ID, now)
	if overrideErr != nil {
		slog.WarnContext(ctx, "verify-key: failed to query budget overrides; using total base limits",
			"budget_allocation_id", allocation.ID, "error", overrideErr)
		overrides = nil
	}
	for _, override := range overrides {
		if override.DimensionType != budgetdomain.DimensionTotal {
			continue
		}
		value, _ := override.TargetLimitUSD.Decimal.Float64()
		switch override.PeriodType {
		case budgetdomain.PeriodDaily:
			if value > daily {
				daily = value
			}
		case budgetdomain.PeriodWeekly:
			if value > weekly {
				weekly = value
			}
		case budgetdomain.PeriodMonthly:
			if value > monthly {
				monthly = value
			}
		}
	}
	limits, tierErr := s.budgetTierRepo.ListTierLimits(ctx, allocation.ID)
	if tierErr != nil {
		slog.WarnContext(ctx, "verify-key: failed to query tier limits; retaining total budget",
			"budget_allocation_id", allocation.ID, "error", tierErr)
		return monthly, daily, weekly, nil
	}
	result := make([]BudgetScopeTierLimit, 0, len(limits))
	for _, limit := range limits {
		effective := limit.LimitUSD
		var expiresAt *time.Time
		for _, override := range overrides {
			if override.DimensionType == budgetdomain.DimensionTier && override.ModelTierID != nil &&
				*override.ModelTierID == limit.ModelTierID && override.PeriodType == limit.PeriodType &&
				override.TargetLimitUSD.Decimal.GreaterThan(effective.Decimal) {
				effective = override.TargetLimitUSD
				expires := override.ExpiresAt
				expiresAt = &expires
			}
		}
		result = append(result, BudgetScopeTierLimit{
			TierID: limit.ModelTierID, TierName: limit.ModelTier.Name, PeriodType: limit.PeriodType,
			BaseLimitUSD: limit.LimitUSD, EffectiveLimitUSD: effective, Enabled: limit.Enabled,
			EffectiveAt: limit.EffectiveAt, ExpiresAt: expiresAt,
		})
	}
	return monthly, daily, weekly, result
}
