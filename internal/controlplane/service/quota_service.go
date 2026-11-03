package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	authmodel "github.com/example/llm-gateway/internal/auth/model"
	"github.com/example/llm-gateway/internal/controlplane/event"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const QuotaSnapshotKey = "quota:snapshot:v1"

// QuotaService 管理四层配额体系的 CRUD 与 snapshot 发布
type QuotaService struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewQuotaService(db *gorm.DB, rdb *redis.Client) *QuotaService {
	return &QuotaService{db: db, rdb: rdb}
}

// SetUserQuota 设置用户配额（nil = 清除覆盖 → 继承 default_user）
func (s *QuotaService) SetUserQuota(ctx context.Context, userID string, rpm, maxConc *int) error {
	if err := validateQuotaValues(rpm, maxConc); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&authmodel.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"quota_rpm":             rpm,
			"quota_max_concurrency": maxConc,
		})
	if result.Error != nil {
		return fmt.Errorf("update user quota: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found")
	}
	go s.refreshSnapshotAsync()
	return nil
}

// GetUserEffectiveQuota 获取用户有效配额（含继承来源）
func (s *QuotaService) GetUserEffectiveQuota(ctx context.Context, userID string) (*QuotaEffective, error) {
	var user authmodel.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户不在 users 表里（如 admin 等平台角色），返回默认配额
			settings, settingsErr := s.getSettings(ctx)
			if settingsErr != nil {
				return nil, settingsErr
			}
			return &QuotaEffective{
				RPM: settings.DefaultUserRPM, MaxConcurrency: settings.DefaultUserMaxConcurrency,
				Source: "default_user", SubjectType: "user", SubjectID: userID,
				DefaultRPM: settings.DefaultUserRPM, DefaultMaxConc: settings.DefaultUserMaxConcurrency,
				RPMSource: "default_user", MaxConcurrencySource: "default_user",
			}, nil
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	rpm := settings.DefaultUserRPM
	maxConc := settings.DefaultUserMaxConcurrency
	source := "default_user"
	if user.QuotaRPM != nil {
		rpm = *user.QuotaRPM
		source = "override"
	}
	if user.QuotaMaxConcurrency != nil {
		maxConc = *user.QuotaMaxConcurrency
		source = "override"
	}
	return &QuotaEffective{
		RPM: rpm, MaxConcurrency: maxConc, Source: source,
		SubjectType: "user", SubjectID: user.ID,
		DefaultRPM: settings.DefaultUserRPM, DefaultMaxConc: settings.DefaultUserMaxConcurrency,
		ConfiguredRPM: user.QuotaRPM, ConfiguredMaxConc: user.QuotaMaxConcurrency,
		RPMSource:            quotaFieldSource(user.QuotaRPM, "default_user"),
		MaxConcurrencySource: quotaFieldSource(user.QuotaMaxConcurrency, "default_user"),
	}, nil
}

// SetServiceKeyQuota 设置 Service Key 配额
func (s *QuotaService) SetServiceKeyQuota(ctx context.Context, keyID string, rpm, maxConc *int) error {
	if err := validateQuotaValues(rpm, maxConc); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&cpmodel.ApiKey{}).
		Where("id = ? AND key_type = ?", keyID, cpmodel.KeyTypeService).
		Updates(map[string]interface{}{
			"quota_rpm":             rpm,
			"quota_max_concurrency": maxConc,
		})
	if result.Error != nil {
		return fmt.Errorf("update service key quota: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("service key not found")
	}
	go s.refreshSnapshotAsync()
	return nil
}

// GetServiceKeyEffectiveQuota 获取 Service Key 有效配额
func (s *QuotaService) GetServiceKeyEffectiveQuota(ctx context.Context, keyID string) (*QuotaEffective, error) {
	var key cpmodel.ApiKey
	if err := s.db.WithContext(ctx).Model(&cpmodel.ApiKey{}).
		Where("id = ? AND key_type = ?", keyID, cpmodel.KeyTypeService).
		First(&key).Error; err != nil {
		return nil, fmt.Errorf("find service key: %w", err)
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	rpm := settings.DefaultServiceKeyRPM
	maxConc := settings.DefaultServiceKeyMaxConcurrency
	source := "default_service_key"
	if key.QuotaRPM != nil {
		rpm = *key.QuotaRPM
		source = "override"
	}
	if key.QuotaMaxConcurrency != nil {
		maxConc = *key.QuotaMaxConcurrency
		source = "override"
	}
	return &QuotaEffective{
		RPM: rpm, MaxConcurrency: maxConc, Source: source,
		SubjectType: "service_key", SubjectID: key.ID,
		DefaultRPM: settings.DefaultServiceKeyRPM, DefaultMaxConc: settings.DefaultServiceKeyMaxConcurrency,
		ConfiguredRPM: key.QuotaRPM, ConfiguredMaxConc: key.QuotaMaxConcurrency,
		RPMSource:            quotaFieldSource(key.QuotaRPM, "default_service_key"),
		MaxConcurrencySource: quotaFieldSource(key.QuotaMaxConcurrency, "default_service_key"),
	}, nil
}

// SetGroupQuota 设置模型组配额
func (s *QuotaService) SetGroupQuota(ctx context.Context, groupID uint, rpm, maxConc *int) error {
	if err := validateQuotaValues(rpm, maxConc); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&cpmodel.Group{}).
		Where("id = ?", groupID).
		Updates(map[string]interface{}{
			"quota_rpm":             rpm,
			"quota_max_concurrency": maxConc,
		})
	if result.Error != nil {
		return fmt.Errorf("update group quota: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("group not found")
	}
	go s.refreshSnapshotAsync()
	return nil
}

// GetGroupEffectiveQuota 获取模型组有效配额
func (s *QuotaService) GetGroupEffectiveQuota(ctx context.Context, groupID uint) (*QuotaEffective, error) {
	var group cpmodel.Group
	if err := s.db.WithContext(ctx).Where("id = ?", groupID).First(&group).Error; err != nil {
		return nil, fmt.Errorf("find group: %w", err)
	}
	rpm := 0
	maxConc := 0
	source := "unset"
	if group.QuotaRPM != nil {
		rpm = *group.QuotaRPM
		source = "override"
	}
	if group.QuotaMaxConcurrency != nil {
		maxConc = *group.QuotaMaxConcurrency
		source = "override"
	}
	return &QuotaEffective{
		RPM: rpm, MaxConcurrency: maxConc, Source: source,
		SubjectType: "group", SubjectID: fmt.Sprintf("%d", groupID),
		ConfiguredRPM: group.QuotaRPM, ConfiguredMaxConc: group.QuotaMaxConcurrency,
		RPMSource:            quotaFieldSource(group.QuotaRPM, "unset"),
		MaxConcurrencySource: quotaFieldSource(group.QuotaMaxConcurrency, "unset"),
	}, nil
}

// SetModelQuota 设置模型配额
func (s *QuotaService) SetModelQuota(ctx context.Context, modelID string, rpm, maxConc *int) error {
	if err := validateQuotaValues(rpm, maxConc); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&cpmodel.Model{}).
		Where("id = ?", modelID).
		Updates(map[string]interface{}{
			"quota_rpm":             rpm,
			"quota_max_concurrency": maxConc,
		})
	if result.Error != nil {
		return fmt.Errorf("update model quota: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("model not found")
	}
	go s.refreshSnapshotAsync()
	return nil
}

// GetModelEffectiveQuota 获取模型有效配额
func (s *QuotaService) GetModelEffectiveQuota(ctx context.Context, modelID string) (*QuotaEffective, error) {
	var m cpmodel.Model
	if err := s.db.WithContext(ctx).Where("id = ?", modelID).First(&m).Error; err != nil {
		return nil, fmt.Errorf("find model: %w", err)
	}
	rpm := 0
	maxConc := 0
	source := "unset"
	if m.QuotaRPM != nil {
		rpm = *m.QuotaRPM
		source = "override"
	}
	if m.QuotaMaxConcurrency != nil {
		maxConc = *m.QuotaMaxConcurrency
		source = "override"
	}
	return &QuotaEffective{
		RPM: rpm, MaxConcurrency: maxConc, Source: source,
		SubjectType: "model", SubjectID: m.ID.String(),
		ConfiguredRPM: m.QuotaRPM, ConfiguredMaxConc: m.QuotaMaxConcurrency,
		RPMSource:            quotaFieldSource(m.QuotaRPM, "unset"),
		MaxConcurrencySource: quotaFieldSource(m.QuotaMaxConcurrency, "unset"),
	}, nil
}

// RefreshSnapshot 手动触发 snapshot 刷新（供外部调用）
func (s *QuotaService) RefreshSnapshot(ctx context.Context) error {
	return s.refreshSnapshot(ctx)
}

// StartSnapshotReconciler 同步执行首次刷新，并周期性从数据库重建配额快照。
// 返回的通道会在 ctx 取消、协调器退出后关闭。
func (s *QuotaService) StartSnapshotReconciler(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if err := s.refreshSnapshot(ctx); err != nil {
		slog.Warn("initial quota snapshot refresh failed", "error", err)
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.refreshSnapshot(ctx); err != nil {
					slog.Warn("periodic quota snapshot refresh failed", "error", err)
				}
			}
		}
	}()
	return done
}

// refreshSnapshot 构建并发布 quota snapshot 到 Redis
func (s *QuotaService) refreshSnapshot(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	snap, err := s.buildSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("build snapshot: %w", err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := s.rdb.Set(ctx, QuotaSnapshotKey, data, 0).Err(); err != nil {
		return fmt.Errorf("set quota snapshot: %w", err)
	}
	// 必须发 JSON ChangeEvent：gateway provider 订阅器会 json.Unmarshal；
	// 裸字符串会刷 ERROR，且配额 ConfigLoader 也约定识别 type=quota_snapshot_updated。
	ev := event.NewChangeEvent(event.QuotaSnapshotUpdated, "quota", uuid.Nil, 0)
	payload, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("quota snapshot event marshal failed", "error", err)
	} else if err := s.rdb.Publish(ctx, "config:change", string(payload)).Err(); err != nil {
		slog.Warn("quota snapshot publish failed", "error", err)
	}
	slog.Info("quota snapshot refreshed")
	return nil
}

// refreshSnapshotAsync 异步刷新（写操作后调用）
func (s *QuotaService) refreshSnapshotAsync() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.refreshSnapshot(ctx); err != nil {
		slog.Warn("async quota snapshot refresh failed", "error", err)
	}
}

func (s *QuotaService) buildSnapshot(ctx context.Context) (*QuotaSnapshotData, error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	snap := &QuotaSnapshotData{
		DefaultUserRPM:                  settings.DefaultUserRPM,
		DefaultUserMaxConcurrency:       settings.DefaultUserMaxConcurrency,
		DefaultServiceKeyRPM:            settings.DefaultServiceKeyRPM,
		DefaultServiceKeyMaxConcurrency: settings.DefaultServiceKeyMaxConcurrency,
		Users:                           make(map[string]QuotaEntry),
		ServiceKeys:                     make(map[string]QuotaEntry),
		Groups:                          make(map[string]QuotaEntry),
		Models:                          make(map[string]QuotaEntry),
		UpdatedAt:                       time.Now().UTC(),
	}
	var users []authmodel.User
	if err := s.db.WithContext(ctx).Where("quota_rpm IS NOT NULL OR quota_max_concurrency IS NOT NULL").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("load user quota overrides: %w", err)
	}
	for _, u := range users {
		snap.Users[u.ID.String()] = quotaEntryWithDefaults(
			u.QuotaRPM,
			u.QuotaMaxConcurrency,
			settings.DefaultUserRPM,
			settings.DefaultUserMaxConcurrency,
		)
	}
	var keys []cpmodel.ApiKey
	if err := s.db.WithContext(ctx).Where("key_type = ? AND (quota_rpm IS NOT NULL OR quota_max_concurrency IS NOT NULL)", cpmodel.KeyTypeService).Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("load key quota overrides: %w", err)
	}
	for _, k := range keys {
		snap.ServiceKeys[k.ID.String()] = quotaEntryWithDefaults(
			k.QuotaRPM,
			k.QuotaMaxConcurrency,
			settings.DefaultServiceKeyRPM,
			settings.DefaultServiceKeyMaxConcurrency,
		)
	}
	var groups []cpmodel.Group
	if err := s.db.WithContext(ctx).Where("quota_rpm IS NOT NULL OR quota_max_concurrency IS NOT NULL").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("load group quota overrides: %w", err)
	}
	for _, g := range groups {
		snap.Groups[g.Name] = QuotaEntry{RPM: g.QuotaRPM, MaxConcurrency: g.QuotaMaxConcurrency}
	}
	var models []cpmodel.Model
	if err := s.db.WithContext(ctx).Where("quota_rpm IS NOT NULL OR quota_max_concurrency IS NOT NULL").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("load model quota overrides: %w", err)
	}
	for _, m := range models {
		snap.Models[m.ID.String()] = QuotaEntry{RPM: m.QuotaRPM, MaxConcurrency: m.QuotaMaxConcurrency}
	}
	// 加载当前生效的豁免
	var exemptions []corequota.QuotaExemption
	if err := s.db.WithContext(ctx).Where("expires_at > ?", time.Now().UTC()).Find(&exemptions).Error; err != nil {
		return nil, fmt.Errorf("load exemptions: %w", err)
	}
	snap.Exemptions = make([]QuotaExemptionEntry, 0, len(exemptions))
	for _, e := range exemptions {
		snap.Exemptions = append(snap.Exemptions, QuotaExemptionEntry{
			SubjectType: e.SubjectType, SubjectID: e.SubjectID, Reason: e.Reason, ExpiresAt: e.ExpiresAt,
		})
	}
	return snap, nil
}

func quotaEntryWithDefaults(rpm, maxConc *int, defaultRPM, defaultMaxConc int) QuotaEntry {
	if rpm == nil {
		rpm = &defaultRPM
	}
	if maxConc == nil {
		maxConc = &defaultMaxConc
	}
	return QuotaEntry{RPM: rpm, MaxConcurrency: maxConc}
}

func (s *QuotaService) getSettings(ctx context.Context) (*cpmodel.SystemSettings, error) {
	settingsRepo := repository.NewSettingsRepo(s.db)
	svc := NewSettingsService(settingsRepo)
	return svc.GetSystemSettings(ctx)
}

func validateQuotaValues(rpm, maxConc *int) error {
	if rpm != nil && *rpm < 0 {
		return fmt.Errorf("quota_rpm must be >= 0 (0 = unlimited, null = inherit)")
	}
	if maxConc != nil && *maxConc < 0 {
		return fmt.Errorf("quota_max_concurrency must be >= 0 (0 = unlimited, null = inherit)")
	}
	return nil
}

func quotaFieldSource(configured *int, inheritedSource string) string {
	if configured != nil {
		return "override"
	}
	return inheritedSource
}

// QuotaEffective 有效配额（含继承来源）
type QuotaEffective struct {
	RPM                  int    `json:"rpm"`
	MaxConcurrency       int    `json:"max_concurrency"`
	Source               string `json:"source"`
	RPMSource            string `json:"rpm_source"`
	MaxConcurrencySource string `json:"max_concurrency_source"`
	SubjectType          string `json:"subject_type"`
	SubjectID            any    `json:"subject_id"`
	DefaultRPM           int    `json:"default_rpm,omitempty"`
	DefaultMaxConc       int    `json:"default_max_concurrency,omitempty"`
	ConfiguredRPM        *int   `json:"configured_rpm"`
	ConfiguredMaxConc    *int   `json:"configured_max_concurrency"`
}

// QuotaSnapshotData Redis 中存储的 quota snapshot 结构
type QuotaSnapshotData struct {
	DefaultUserRPM                  int                   `json:"default_user_rpm"`
	DefaultUserMaxConcurrency       int                   `json:"default_user_max_concurrency"`
	DefaultServiceKeyRPM            int                   `json:"default_service_key_rpm"`
	DefaultServiceKeyMaxConcurrency int                   `json:"default_service_key_max_concurrency"`
	Users                           map[string]QuotaEntry `json:"users"`
	ServiceKeys                     map[string]QuotaEntry `json:"service_keys"`
	Groups                          map[string]QuotaEntry `json:"groups"`
	Models                          map[string]QuotaEntry `json:"models"`
	Exemptions                      []QuotaExemptionEntry `json:"exemptions,omitempty"`
	UpdatedAt                       time.Time             `json:"updated_at"`
}

type QuotaEntry struct {
	RPM            *int `json:"rpm,omitempty"`
	MaxConcurrency *int `json:"max_concurrency,omitempty"`
}

type QuotaExemptionEntry struct {
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	Reason      string    `json:"reason"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// QuotaCurrentUsage 当前使用量
type QuotaCurrentUsage struct {
	RPM            int `json:"rpm"`
	MaxConcurrency int `json:"max_concurrency"`
}

// currentConcKey 返回用户的并发租约键。
//
// 键格式来自**唯一权威定义** corequota.ConcurrencyKey（internal/core/quota），
// 不得在本文件硬编码字面量：网关侧的并发键曾从 quota:conc: 迁移为
// quota:conc:lease:（ZSET 租约），本读取端当时未同步，导致
// 「用户当前并发使用量」恒为 0，并持续输出 "quota usage: GET failed" 告警，
// 把正常的键不存在误报成 Redis 故障。
func currentConcKey(userID string) string {
	return corequota.ConcurrencyKeySubject("user", userID)
}

// getUserConcurrencyUsage 统计用户当前**在飞**并发数（只读、非破坏性）。
//
// 并发键是租约式 ZSET：member = 租约 id，score = 该租约的到期时间戳（毫秒）。
// 因此「当前在飞」= score > now 的成员数，用 ZCOUNT (now +inf 统计：
//   - 不用 ZCARD：它会把「已过期但尚未被清理」的成员一起算进来，向上虚报占用；
//   - 不做 ZREMRANGEBYSCORE：本路径是控制面只读展示，绝不能顺手清理数据；
//   - 边界与写入侧对齐：网关获取脚本按 `-inf now` 清理，故 score == now 视为已到期，
//     区间用左开括号（由 corequota.ConcurrencyInFlightRange 统一给出）。
//
// 键不存在时 ZCOUNT 返回 0（不产生 redis.Nil），属正常情况——用户当前没有在途请求——
// 返回 0 且不告警。
func getUserConcurrencyUsage(ctx context.Context, rdb *redis.Client, userID string) int {
	concKey := currentConcKey(userID)
	min, max := corequota.ConcurrencyInFlightRange(time.Now())
	cnt, err := rdb.ZCount(ctx, concKey, min, max).Result()
	if err != nil {
		// ZCOUNT 在键不存在时返回 0 而非 redis.Nil，故此处 err 只可能是真实故障
		// （连接失败 / WRONGTYPE 等），需要告警；对外仍降级返回 0，不影响接口可用性。
		slog.Warn("quota usage: concurrency ZCOUNT failed",
			"key", concKey, "error", err, "user_id", userID)
		return 0
	}
	return int(cnt)
}

// GetUserCurrentUsage 查询用户当前 RPM 和并发使用量（只读，不改动任何 Redis 数据）
func (s *QuotaService) GetUserCurrentUsage(ctx context.Context, userID string) (*QuotaCurrentUsage, error) {
	if s.rdb == nil {
		return &QuotaCurrentUsage{}, nil
	}

	// RPM 键也是 ZSET：窗口内 = score 严格大于 (now - RPMWindowMS) 的成员。
	// 同样不用 ZCARD —— 窗口外但尚未过期/尚未被写入侧清理的残留成员会被算进来而虚报。
	// 键不存在时 ZCOUNT 返回 0（不产生 redis.Nil），属正常情况，不告警。
	rpmKey := corequota.RPMKey("user", userID, "", "")
	rpmMin, rpmMax := corequota.RPMInFlightRange(time.Now())
	rpmVal, err := s.rdb.ZCount(ctx, rpmKey, rpmMin, rpmMax).Result()
	if err != nil {
		slog.Warn("quota usage: rpm ZCOUNT failed", "key", rpmKey, "error", err, "user_id", userID)
	}

	return &QuotaCurrentUsage{
		RPM:            int(rpmVal),
		MaxConcurrency: getUserConcurrencyUsage(ctx, s.rdb, userID),
	}, nil
}

// GetQuotaSnapshot 获取当前 quota snapshot（供 gateway 拉取）
func (s *QuotaService) GetQuotaSnapshot(ctx context.Context) (*QuotaSnapshotData, error) {
	return s.buildSnapshot(ctx)
}
