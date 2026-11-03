package model

import (
	"time"
)

// 媒体任务平台的状态词表（C1）。
//
// 存储层词表 = core 的对外四态（queued/in_progress/completed/failed）
// ∪ {expired}。`expired` 只由控制面超时 sweeper 写入，是**存储层**状态：
// 对调用方（网关 / 客户端）一律归一为 core.MediaStatusFailed
// （见 core/media_task.go 的 MediaStatus 注释与下面的 CallerMediaTaskStatus）。
//
// 历史遗留：`video_tasks` 表在 Seedance 链路里用的是 submitted/running/succeeded/failed
// 这套旧词表。存量行与旧链路**继续使用旧词表**，本文件的状态机只约束媒体词表，
// 遇到旧词表一律不施加前进限制（保证向后兼容），详见 repository.CanAdvanceMediaStatus。
//
// ⚠️ 这里刻意**不 import internal/core**：`internal/core` 会被 `tools/atlas-loader` 间接
// 依赖，而 core 的依赖闭包（gjson 等）不在 atlas-loader 的 go.sum 里 —— import 它会让
// `cd tools/atlas-loader && go run main.go` 直接编译失败（Atlas 的期望 schema 就出不来）。
// 因此取值以字面量复制，并由 tests/controlplane/model 的跨包断言测试锁定与 core 一致。
const (
	// MediaTaskStatusQueued 已受理、尚未开始（对应旧词表 submitted）。
	MediaTaskStatusQueued = "queued"
	// MediaTaskStatusInProgress 上游处理中（对应旧词表 running）。
	MediaTaskStatusInProgress = "in_progress"
	// MediaTaskStatusCompleted 成功终态（对应旧词表 succeeded）。
	MediaTaskStatusCompleted = "completed"
	// MediaTaskStatusFailed 失败终态。
	MediaTaskStatusFailed = "failed"
	// MediaTaskStatusExpired 超时终态（存储层专用；对调用方归一为 failed，且不记账）。
	MediaTaskStatusExpired = "expired"
)

// MediaTaskKind* 与 core.MediaKind 取值一致（存库字符串）。
const (
	MediaTaskKindImage = "image"
	MediaTaskKindVideo = "video"
)

// IsMediaTaskStatus 报告状态是否属于媒体词表（含存储层专有的 expired）。
func IsMediaTaskStatus(status string) bool {
	switch status {
	case MediaTaskStatusQueued, MediaTaskStatusInProgress,
		MediaTaskStatusCompleted, MediaTaskStatusFailed, MediaTaskStatusExpired:
		return true
	default:
		return false
	}
}

// IsMediaTaskTerminalStatus 报告状态是否为终态（completed/failed/expired）。
func IsMediaTaskTerminalStatus(status string) bool {
	switch status {
	case MediaTaskStatusCompleted, MediaTaskStatusFailed, MediaTaskStatusExpired:
		return true
	default:
		return false
	}
}

// IsValidMediaTaskKind 报告 kind 是否在受支持枚举内（image|video）。
func IsValidMediaTaskKind(kind string) bool {
	switch kind {
	case MediaTaskKindImage, MediaTaskKindVideo:
		return true
	default:
		return false
	}
}

// CallerMediaTaskStatus 把**存储层**状态归一为**对外词表**：expired → failed。
//
// 依据 core/media_task.go：`expired` 是存储层专有状态（超时 sweeper 置位，视同失败但不记账），
// 对外响应必须归一为 failed。仅用于「面向调用方（网关/客户端）」的响应；
// **管理面（看板/详情）返回原始值**，因为运维需要区分「超时」与「上游失败」。
// legacy 词表（submitted/running/succeeded）原样返回，不做映射（旧链路自行展示）。
func CallerMediaTaskStatus(status string) string {
	if status == MediaTaskStatusExpired {
		return MediaTaskStatusFailed
	}
	return status
}

// VideoTask 媒体任务记录（用量统计用，非 task_id 映射表）。
// 主键为对外 task_id（网关生成）；provider_type 用于查询时路由到正确适配器。
//
// ⚠️ 表名已由 `video_tasks` 改名为 `media_tasks`（决策 D：扩列 + 改名，单表承载
// 图像/视频任务）。结构体名保持 VideoTask 以避免全仓重命名 churn，
// TableName() 是唯一的改名触点（另见 repository.applyScope 里的裸 SQL 表名限定）。
//
// 具名对象：二级索引保留历史名 `idx_video_tasks_*` / `idx_video_task_events_*`（GORM tag 不动），
// NOT NULL 约束保留 Postgres 侧旧名（Atlas 不比对二者）；**主键**与外键由迁移显式 RENAME 到
// Atlas 的期望名（media_tasks_pkey / fk_media_tasks_events）—— 实测 Atlas 会比对这两类名字。
type VideoTask struct {
	TaskID       string           `gorm:"type:varchar(128);primaryKey" json:"task_id"`
	APIKeyID     string           `gorm:"type:varchar(64);not null;index:idx_video_tasks_api_key;uniqueIndex:uniq_media_tasks_idem" json:"api_key_id"`
	APIKeyName   string           `gorm:"type:varchar(128)" json:"api_key_name,omitempty"`
	APIKeyType   string           `gorm:"type:varchar(16)" json:"api_key_type,omitempty"`
	UserID       string           `gorm:"type:text" json:"user_id,omitempty"`
	UserName     string           `gorm:"type:varchar(128)" json:"user_name,omitempty"`
	Model        string           `gorm:"type:text;not null;index:idx_video_tasks_model" json:"model"`
	ProviderName string           `gorm:"type:varchar(64);not null" json:"provider_name"`
	ProviderType string           `gorm:"type:varchar(64);not null;default:''" json:"provider_type"`
	Status       string           `gorm:"type:varchar(32);not null;default:submitted;index:idx_video_tasks_status" json:"status"`
	VideoURL     string           `gorm:"type:text" json:"video_url,omitempty"`
	RequestBody  string           `gorm:"type:text" json:"request_body,omitempty"`
	ResponseBody string           `gorm:"type:text" json:"response_body,omitempty"`
	ErrorMessage string           `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt    time.Time        `gorm:"not null;default:now();index:idx_video_tasks_ts" json:"created_at"`
	UpdatedAt    time.Time        `gorm:"not null;default:now()" json:"updated_at"`
	Events       []VideoTaskEvent `gorm:"foreignKey:TaskID;references:TaskID;constraint:OnDelete:CASCADE" json:"events,omitempty"`

	// ─────────────────────── C1 delta：媒体任务平台新增列 ───────────────────────

	// Kind 任务类别 image|video（旧行与旧链路为空串）。旧行不回溯填充是刻意的：
	// 旧行都是视频任务，但改历史数据会污染「存量 112 行」的审计口径。
	Kind string `gorm:"type:varchar(16);not null;default:''" json:"kind,omitempty"`
	// Action 渠道无关的归一动作（core.MediaAction，如 text_to_video）。
	Action string `gorm:"type:varchar(40);not null;default:''" json:"action,omitempty"`
	// VendorTaskID 上游任务 id（对外 task_id 与上游 id 分离，禁止对外暴露）。
	VendorTaskID string `gorm:"type:varchar(191);not null;default:''" json:"vendor_task_id,omitempty"`
	// Progress 上游进度透传（"0-100" 或阶段名，语义由上游决定）。
	Progress string `gorm:"type:varchar(20);not null;default:''" json:"progress,omitempty"`
	// Usage 上游实际用量（credits/units/duration/images…）。
	// 可空是刻意的：NULL = 上游未提供用量（对齐 core.MediaTaskStatus.Usage 的 nil 语义），
	// 与「给了用量但为 0」区分开；同时避免 JSONRaw 的 nil 被写成 NULL 而撞 NOT NULL。
	Usage JSONRaw `gorm:"type:jsonb" json:"usage,omitempty"`
	// CostMicroUSD 成本（整数微美元，避免浮点误差；1 USD = 1e6 微美元）。
	CostMicroUSD int64 `gorm:"not null;default:0" json:"cost_micro_usd"`
	// BilledAt 记账时间（只写一次；失败/超时不记账）。
	BilledAt *time.Time `json:"billed_at,omitempty"`
	// StartedAt 上游开始处理的时刻（超时判定优先取它）。
	StartedAt *time.Time `json:"started_at,omitempty"`
	// FinishedAt 进入终态的时刻。
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// PollFailures 轮询失败计数（C7 失败分类累加，达到上限由网关侧放弃）。
	PollFailures int `gorm:"not null;default:0" json:"poll_failures"`
	// IdempotencyKey 幂等键（与 APIKeyID 组成唯一索引，命中即返回既有任务且不打上游）。
	//
	// ⚠️ 必须是 **指针**：GORM 对 string 零值会写入 `''` 而不是 NULL，而 PG 的唯一索引里
	// NULL 互不相等、`''` 却彼此相等 ⇒ 同一个 Key 下两个「不带幂等键」的任务会在
	// (api_key_id, idempotency_key) 上撞唯一键，把正常提交打成 500。
	// 用 *string 让「无幂等键」落到 NULL，索引语义才与设计一致（唯一索引只拦显式重复提交）。
	IdempotencyKey *string `gorm:"type:varchar(191);uniqueIndex:uniq_media_tasks_idem" json:"idempotency_key,omitempty"`
	// PrivateData 渠道/凭证引用等**禁止对外返回**的数据。
	// json:"-" 是刻意的：任何 HTTP 出口（管理面与内部面）都不会序列化它。
	PrivateData JSONRaw `gorm:"type:jsonb" json:"-"`
	// DeptID / StudioID 归属快照（供按部门/工作室查询，避免每次回表 users/departments）。
	DeptID   string `gorm:"type:varchar(64);not null;default:''" json:"dept_id,omitempty"`
	StudioID string `gorm:"type:varchar(64);not null;default:''" json:"studio_id,omitempty"`
}

func (VideoTask) TableName() string {
	return "media_tasks"
}

// VideoTaskEvent 记录任务创建和状态更新事件，供详情页时间线展示。
type VideoTaskEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"type:varchar(128);not null;index:idx_video_task_events_task_id" json:"task_id"`
	EventType string    `gorm:"type:varchar(64);not null" json:"event_type"`
	EventData string    `gorm:"type:text" json:"event_data,omitempty"`
	CreatedAt time.Time `gorm:"not null;default:now();index:idx_video_task_events_created_at" json:"created_at"`
}

func (VideoTaskEvent) TableName() string {
	return "media_task_events"
}
