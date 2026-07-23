package quota

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// LayerLimits 单层配额限额（RPM + 并发）。
// 值为 0 表示该层显式不限（跳过检查）。
type LayerLimits struct {
	RPM            int `json:"rpm"`
	MaxConcurrency int `json:"max_concurrency"`
}

// QuotaSnapshot 四层配额快照，由控制面发布到 Redis，gateway 热加载。
// JSON 字段名与控制面 QuotaSnapshotData 严格一致，确保反序列化正确。
type QuotaSnapshot struct {
	Version                         int                    `json:"version"`
	DefaultUserRPM                  int                    `json:"default_user_rpm"`
	DefaultUserMaxConcurrency       int                    `json:"default_user_max_concurrency"`
	DefaultServiceKeyRPM            int                    `json:"default_service_key_rpm"`
	DefaultServiceKeyMaxConcurrency int                    `json:"default_service_key_max_concurrency"`
	Users                           map[string]LayerLimits `json:"users"`        // userUUID
	ServiceKeys                     map[string]LayerLimits `json:"service_keys"` // keyUUID
	Groups                          map[string]LayerLimits `json:"groups"`       // group name
	Models                          map[string]LayerLimits `json:"models"`       // model UUID
	Exemptions                      []QuotaExemption       `json:"exemptions"`   // 紧急豁免列表
}

// QuotaViolation 配额违规详情，用于 429 响应与结构化日志。
type QuotaViolation struct {
	SubjectType string `json:"subject_type"` // user | service_key
	SubjectID   string `json:"subject_id"`
	Layer       string `json:"layer"` // default | subject | group | model
	Gate        string `json:"gate"`  // rpm | concurrency
	Limit       int    `json:"limit"`
}

// QuotaExemption 紧急豁免记录
type QuotaExemption struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primarykey;default:gen_random_uuid()"`
	SubjectType string    `json:"subject_type" gorm:"type:varchar(32);not null"` // user | service_key
	SubjectID   string    `json:"subject_id" gorm:"type:varchar(255);not null"`  // userID or keyID
	Reason      string    `json:"reason" gorm:"type:text"`                       // 豁免原因
	ExpiresAt   time.Time `json:"expires_at" gorm:"not null"`                    // 过期时间
	CreatedBy   string    `json:"created_by" gorm:"type:varchar(255);not null"`  // 创建人 userID
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (QuotaExemption) TableName() string { return "quota_exemptions" }

// fallbackDefaultUser 快照未就绪时用户默认配额
var fallbackDefaultUser = LayerLimits{RPM: 60, MaxConcurrency: 10}

// fallbackDefaultServiceKey 快照未就绪时 Service Key 默认配额
var fallbackDefaultServiceKey = LayerLimits{RPM: 120, MaxConcurrency: 20}

// FallbackDefaultUser returns the default user LayerLimits used when no snapshot is available.
func FallbackDefaultUser() LayerLimits { return fallbackDefaultUser }

// FallbackDefaultServiceKey returns the default service-key LayerLimits used when no snapshot is available.
func FallbackDefaultServiceKey() LayerLimits { return fallbackDefaultServiceKey }

// ──────────────────────────────────────────────────────────────────────────────
// 配额 Redis 键格式（跨进程共享）
//
// gateway（写入方）与控制面（只读展示方）必须使用同一套键格式。这些常量放在本包
// ——它本来就是两个进程共享的领域类型包——以避免任一侧硬编码字符串字面量而再次漂移：
// 历史上并发键前缀从 quota:conc: 迁移到 quota:conc:lease: 时，控制面读路径未同步，
// 导致「用户当前并发使用量」恒为 0 并持续打故障告警（观测盲区）。
// ──────────────────────────────────────────────────────────────────────────────

const (
	// RPMKeyPrefix RPM 滑动窗口键前缀（ZSET，member 为请求时间戳）
	RPMKeyPrefix = "quota:rpm:"
	// RPMWindowMS RPM 滑动窗口大小（毫秒），跨进程共享：网关写入/判定与实际限流口径一致，
	// 控制面只读展示也用同一窗口，避免管理面读数与实际口径不符。
	RPMWindowMS = 60 * 1000
	// ConcurrencyKeyPrefix 并发租约键前缀（ZSET，member 为租约 id，score 为到期时间戳）
	//
	// 注意它同时匹配更宽的 quota:conc:* 模式：运维按旧前缀巡检/清理旧键时
	// 必须显式排除本前缀，避免误删在途租约。
	ConcurrencyKeyPrefix = "quota:conc:lease:"
)

// LegacyConcurrencyKeySubject 返回**旧的**并发键形态（裸 INCR/DECR 时代的
// `quota:conc:<subjectType>:<subjectID>`，例如 `quota:conc:user:u1`）。
//
// 为什么保留这个函数而不是让各处直接写字符串字面量：
//  1. 它是**历史/外部数据**的键形态，不由本包构造——灰度期由旧代码写入，
//     清理时由运维读取，因此无法用 ConcurrencyKeyPrefix 推导出来，字面量无法避免；
//  2. 把它收敛到一处并说明用途，能使「新格式的期望值」在测试里**零字面量**
//     （新格式一律走 ConcurrencyKey/ConcurrencyKeySubject 等 helper），
//     从而让"两侧不得硬编码新键前缀"这条约束可被 grep 机械校验；
//  3. 需要断言"新旧不冲突"（灰度共存、WRONGTYPE 防护、旧键钉死对照）的测试，
//     必须有一个明确命名的旧形态来源，否则会被误读成"漏改的旧代码"。
//
// 生产代码**不得**用它构造当前生效的并发键；仅限灰度对照与运维清理场景。
func LegacyConcurrencyKeySubject(subjectType, subjectID string) string {
	return "quota:conc:" + subjectType + ":" + subjectID
}

// RPMInFlightRange 返回统计「当前 RPM 窗口内请求数」应传给 ZCOUNT 的 min/max 参数。
//
// 与写入侧对齐：网关判定脚本按 `ZREMRANGEBYSCORE key -inf (now-window)` 丢弃窗口外条目，
// 因此窗口内 = score 严格大于 (now - RPMWindowMS) 的成员，用左开区间排除恰好落在边界的条目。
// 读侧据此统计即可，**不要**用 ZCARD（会把窗口外但尚未过期/尚未被清理的残留成员算进来）。
func RPMInFlightRange(now time.Time) (min, max string) {
	return fmt.Sprintf("(%d", now.Add(-time.Duration(RPMWindowMS)*time.Millisecond).UnixMilli()), "+inf"
}

// RPMKey 构造 RPM 滑动窗口键。
//
// 键类型：ZSET；member = 请求标识/时间戳，score = 请求发生时刻（毫秒时间戳）。
// 判'窗口内是否超限'由网关用 ZREMRANGEBYSCORE 清理窗口外条目后 ZCARD 统计。
// resourceID 为空表示主体层（user/service_key 级）；否则为 group/model 层。
func RPMKey(subjectType, subjectID, layer, resourceID string) string {
	if resourceID == "" {
		return RPMKeyPrefix + subjectType + ":" + subjectID
	}
	return RPMKeyPrefix + subjectType + ":" + subjectID + ":" + layer + ":" + resourceID
}

// ConcurrencyKey 构造并发租约键（本函数是并发键格式的**唯一权威定义**，两侧不得另有实现）。
//
// 数据结构与语义：
//   - 键类型：**ZSET**（不是字符串计数器）。旧实现曾是字符串 INCR/DECR，
//     迁移后仍沿用旧格式会与灰度期的旧代码互撞，因此必须使用 ConcurrencyKeyPrefix。
//   - member：**租约 id**，每个在途请求一条，全局唯一（释放时按它精确 ZREM）。
//   - score：**该租约的到期时间戳**（毫秒）。逐条目到期是实现自愈的唯一权威：
//     丢失归还（进程被强杀、优雅退出被截断）的条目会在到期后自动失效，
//     新请求的获取脚本用 ZREMRANGEBYSCORE key -inf now 顺手清理，
//     因此不会再出现'丢一次 DECR 就被永久钉死在限额上'。
//
// 读取方（控制面只读展示等）必须使用**非破坏性**统计：
//
//	ZCOUNT key (now +inf     // now = 当前毫秒时间戳，严格大于 now = 尚未到期的在途请求数
//
// 明确禁止：
//   - 不要用 ZCARD：会把'已过期但尚未被清理'的成员一起算进来，向上虚报占用；
//   - 不要为了读数执行 ZREMRANGEBYSCORE：读路径必须无副作用。
//
// 多段格式（带 layer/resourceID）与主体层是**互相独立的作用域**，各自有独立限额，
// 不能互相替代或合并统计。lease 粒度 = (request, key)：
// 同一请求内对同一 key 重复获取复用同一租约；failover 换到不同 key 时各 key 独立持有。
func ConcurrencyKey(subjectType, subjectID, layer, resourceID string) string {
	if resourceID == "" {
		return ConcurrencyKeyPrefix + subjectType + ":" + subjectID
	}
	return ConcurrencyKeyPrefix + subjectType + ":" + subjectID + ":" + layer + ":" + resourceID
}

// ConcurrencyKeySubject 构造主体层（user / service_key 级）并发键的便捷写法。
// 等价于 ConcurrencyKey(subjectType, subjectID, "", "")。
func ConcurrencyKeySubject(subjectType, subjectID string) string {
	return ConcurrencyKey(subjectType, subjectID, "", "")
}

// ConcurrencyInFlightRange 返回统计'当前在飞并发数'应传给 ZCOUNT 的 min/max 参数。
//
// 与写入侧彻底对齐：获取脚本用 `ZREMRANGEBYSCORE key -inf now` 清理，
// 因此 score 恰好等于 now 的条目视为**已到期**，这里用左开区间 `(now` 严格排除它们，
// 保证读到的数字不会把'刚好卡在边界'的条目算成占用。
//
// 调用方只需：rdb.ZCount(ctx, ConcurrencyKey(...), min, max)
// 返回顺序与 go-redis 的 ZCount(ctx, key, min, max) 参数顺序一致。
func ConcurrencyInFlightRange(now time.Time) (min, max string) {
	return fmt.Sprintf("(%d", now.UnixMilli()), "+inf"
}
