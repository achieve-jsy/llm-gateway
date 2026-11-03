package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VideoTaskRepo struct {
	db *gorm.DB
}

type VideoTaskScope struct {
	IsGlobal  bool
	UserID    string
	StudioIDs []string
	DeptIDs   []string
}

type TrendRow struct {
	Date      string `json:"date"`
	Tasks     int64  `json:"tasks"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
	Submitted int64  `json:"submitted"`
	Running   int64  `json:"running"`
}

type ProviderDistributionRow struct {
	ProviderName string `json:"provider_name"`
	Count        int64  `json:"count"`
}

type StatusDistributionRow struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type ModelDistributionRow struct {
	Model     string `json:"model"`
	Count     int64  `json:"count"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
}

type DistributionRows struct {
	Providers []ProviderDistributionRow `json:"providers"`
	Statuses  []StatusDistributionRow   `json:"statuses"`
	Models    []ModelDistributionRow    `json:"models"`
}

func NewVideoTaskRepo(db *gorm.DB) *VideoTaskRepo {
	return &VideoTaskRepo{db: db}
}

type VideoTaskUpdateFields struct {
	Status       string
	VideoURL     string
	ResponseBody string
	ErrorMessage string
}

// List 分页查询
func (r *VideoTaskRepo) List(page, pageSize int, providerName, modelName, status, userID string) ([]model.VideoTask, int64, error) {
	return r.ListScoped(page, pageSize, providerName, modelName, status, userID, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) ListScoped(page, pageSize int, providerName, modelName, status, userID string, scope VideoTaskScope) ([]model.VideoTask, int64, error) {
	var tasks []model.VideoTask
	var total int64

	query := r.db.Model(&model.VideoTask{})
	query = r.applyScope(query, scope)

	if providerName != "" {
		query = query.Where("provider_name = ?", providerName)
	}
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetByID 按 task_id 查询
func (r *VideoTaskRepo) GetByID(taskID string) (*model.VideoTask, error) {
	return r.GetByIDScoped(taskID, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) GetByIDScoped(taskID string, scope VideoTaskScope) (*model.VideoTask, error) {
	var task model.VideoTask
	query := r.applyScope(r.db.Model(&model.VideoTask{}), scope)
	if err := query.
		Preload("Events", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC").Order("id ASC")
		}).
		Where("task_id = ?", taskID).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetByIDAndUserID 按 task_id + user_id 查询，供个人中心详情隔离使用。
func (r *VideoTaskRepo) GetByIDAndUserID(taskID, userID string) (*model.VideoTask, error) {
	var task model.VideoTask
	if err := r.db.
		Preload("Events", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC").Order("id ASC")
		}).
		Where("task_id = ? AND user_id = ?", taskID, userID).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// Create 创建记录（task_id 冲突时忽略，保证幂等）
func (r *VideoTaskRepo) Create(task *model.VideoTask) error {
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(task)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	return r.createEvent(task.TaskID, task.Status, "任务已提交")
}

// UpdateStatus 更新状态并刷新 updated_at（用于平均生成耗时统计）
func (r *VideoTaskRepo) UpdateStatus(taskID, status string) error {
	return r.UpdateFields(taskID, VideoTaskUpdateFields{Status: status})
}

// UpdateFields 按字段更新任务详情，并在状态变化时写入事件。
func (r *VideoTaskRepo) UpdateFields(taskID string, fields VideoTaskUpdateFields) error {
	updates := map[string]interface{}{}
	if fields.Status != "" {
		updates["status"] = fields.Status
	}
	if fields.VideoURL != "" {
		updates["video_url"] = fields.VideoURL
	}
	if fields.ResponseBody != "" {
		updates["response_body"] = fields.ResponseBody
	}
	if fields.ErrorMessage != "" {
		updates["error_message"] = fields.ErrorMessage
	}
	if len(updates) == 0 {
		return nil
	}

	var previousStatus string
	if fields.Status != "" {
		_ = r.db.Model(&model.VideoTask{}).
			Select("status").
			Where("task_id = ?", taskID).
			Scan(&previousStatus).Error
	}

	// updated_at 用于生成耗时统计；同一状态下轮询只刷新响应体，不延长耗时。
	if fields.Status == "" || fields.Status != previousStatus {
		updates["updated_at"] = time.Now().UTC()
	}
	result := r.db.Model(&model.VideoTask{}).
		Where("task_id = ?", taskID).
		Updates(updates)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	if fields.Status != "" && fields.Status != previousStatus {
		return r.createEvent(taskID, fields.Status, "状态更新为 "+fields.Status)
	}
	return nil
}

func (r *VideoTaskRepo) createEvent(taskID, eventType, eventData string) error {
	if eventType == "" {
		eventType = "updated"
	}
	return r.db.Create(&model.VideoTaskEvent{
		TaskID:    taskID,
		EventType: eventType,
		EventData: eventData,
		CreatedAt: time.Now().UTC(),
	}).Error
}

// GetProviderType 按 task_id 查 provider_type（供 gateway 查询路由）
func (r *VideoTaskRepo) GetProviderType(taskID string) (string, error) {
	meta, err := r.GetTaskMeta(taskID)
	if err != nil {
		return "", err
	}
	return meta.ProviderType, nil
}

// VideoTaskMeta 供 gateway 路由与 call_logs 反查的最小字段集。
type VideoTaskMeta struct {
	Model        string
	ProviderType string
	ProviderName string
}

// GetTaskMeta 按 task_id 查 model + provider_type + provider_name。
func (r *VideoTaskRepo) GetTaskMeta(taskID string) (*VideoTaskMeta, error) {
	var task model.VideoTask
	if err := r.db.Select("model", "provider_type", "provider_name").Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	return &VideoTaskMeta{
		Model:        task.Model,
		ProviderType: task.ProviderType,
		ProviderName: task.ProviderName,
	}, nil
}

func (r *VideoTaskRepo) statsQuery(start, end, userID, providerName, modelName string) *gorm.DB {
	return r.statsQueryScoped(start, end, userID, providerName, modelName, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) statsQueryScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) *gorm.DB {
	query := r.applyScope(r.db.Model(&model.VideoTask{}), scope)
	if start != "" {
		query = query.Where("created_at >= ?", start)
	}
	if end != "" {
		query = query.Where("created_at < (?::date + INTERVAL '1 day')", end)
	}
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if providerName != "" {
		query = query.Where("provider_name = ?", providerName)
	}
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}
	return query
}

func (r *VideoTaskRepo) applyScope(query *gorm.DB, scope VideoTaskScope) *gorm.DB {
	if scope.IsGlobal {
		return query
	}
	if scope.UserID != "" {
		return query.Where("media_tasks.user_id = ?", scope.UserID)
	}
	if len(scope.StudioIDs) > 0 {
		return query.Where(`media_tasks.user_id IN (
			SELECT CAST(u.id AS TEXT)
			FROM users u
			JOIN departments d ON d.id = u.dept_id
			WHERE CAST(d.studio_id AS TEXT) IN ?
		)`, scope.StudioIDs)
	}
	if len(scope.DeptIDs) > 0 {
		return query.Where(`media_tasks.user_id IN (
			SELECT CAST(u.id AS TEXT)
			FROM users u
			JOIN departments d ON d.id = u.dept_id
			WHERE CAST(d.id AS TEXT) IN ? OR d.dept_id IN ?
		)`, scope.DeptIDs, scope.DeptIDs)
	}
	return query.Where("1 = 0")
}

// Stats 统计
func (r *VideoTaskRepo) Stats(start, end, userID, providerName, modelName string) (map[string]interface{}, error) {
	return r.StatsScoped(start, end, userID, providerName, modelName, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) StatsScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) (map[string]interface{}, error) {
	var total, submitted, running, succeeded, failed int64
	var avgDurationSeconds float64

	base := r.statsQueryScoped(start, end, userID, providerName, modelName, scope)
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "submitted").Count(&submitted).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "running").Count(&running).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "succeeded").Count(&succeeded).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "failed").Count(&failed).Error; err != nil {
		return nil, err
	}

	// 计算已成功任务的平均耗时（秒）
	if err := base.Session(&gorm.Session{}).
		Where("status = ? AND updated_at > created_at", "succeeded").
		Select("COALESCE(AVG(EXTRACT(EPOCH FROM (updated_at - created_at))), 0)").
		Scan(&avgDurationSeconds).Error; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":                total,
		"submitted":            submitted,
		"running":              running,
		"succeeded":            succeeded,
		"failed":               failed,
		"avg_duration_seconds": avgDurationSeconds,
	}, nil
}

// StatsTrend 按天统计任务趋势。
func (r *VideoTaskRepo) StatsTrend(start, end, userID, providerName, modelName string) ([]TrendRow, error) {
	return r.StatsTrendScoped(start, end, userID, providerName, modelName, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) StatsTrendScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) ([]TrendRow, error) {
	var rows []TrendRow
	err := r.statsQueryScoped(start, end, userID, providerName, modelName, scope).
		Select(`
			TO_CHAR(created_at::date, 'YYYY-MM-DD') AS date,
			COUNT(*) AS tasks,
			COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed,
			COUNT(*) FILTER (WHERE status = 'submitted') AS submitted,
			COUNT(*) FILTER (WHERE status = 'running') AS running
		`).
		Group("created_at::date").
		Order("created_at::date ASC").
		Scan(&rows).Error
	return rows, err
}

// StatsDistribution 统计渠道、状态、模型分布。
func (r *VideoTaskRepo) StatsDistribution(start, end, userID, providerName, modelName string) (DistributionRows, error) {
	return r.StatsDistributionScoped(start, end, userID, providerName, modelName, VideoTaskScope{IsGlobal: true})
}

func (r *VideoTaskRepo) StatsDistributionScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) (DistributionRows, error) {
	var result DistributionRows

	if err := r.statsQueryScoped(start, end, userID, providerName, modelName, scope).
		Select("provider_name, COUNT(*) AS count").
		Group("provider_name").
		Order("count DESC").
		Scan(&result.Providers).Error; err != nil {
		return result, err
	}
	if err := r.statsQueryScoped(start, end, userID, providerName, modelName, scope).
		Select("status, COUNT(*) AS count").
		Group("status").
		Order("count DESC").
		Scan(&result.Statuses).Error; err != nil {
		return result, err
	}
	if err := r.statsQueryScoped(start, end, userID, providerName, modelName, scope).
		Select(`
			model,
			COUNT(*) AS count,
			COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed
		`).
		Group("model").
		Order("count DESC").
		Limit(20).
		Scan(&result.Models).Error; err != nil {
		return result, err
	}
	return result, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// C2：媒体任务平台（media_tasks）的仓储增量
//
// 与上面 legacy Seedance 路径（video_url / submitted→running→succeeded）**并存**：
// 本节的 CAS + 状态机只约束媒体词表（queued/in_progress/completed/failed/expired），
// 旧词表行与旧链路一律不施加前进限制（见 CanAdvanceMediaStatus），保证向后兼容。
// ═══════════════════════════════════════════════════════════════════════════════

// mediaTaskIdempotencyKey 取任务上「有效」的幂等键（nil 或空串 = 不带幂等键）。
func mediaTaskIdempotencyKey(task *model.VideoTask) string {
	if task == nil || task.IdempotencyKey == nil {
		return ""
	}
	return strings.TrimSpace(*task.IdempotencyKey)
}

// mediaTaskCASMaxAttempts 回读重试次数（乐观锁：并发改动导致 RowsAffected=0 时重读一次再判断）。
const mediaTaskCASMaxAttempts = 3

// mediaTaskStatusRank 媒体词表的前进序（终态同秩：completed/failed/expired 互不可转换）。
var mediaTaskStatusRank = map[string]int{
	model.MediaTaskStatusQueued:     0,
	model.MediaTaskStatusInProgress: 1,
	model.MediaTaskStatusCompleted:  2,
	model.MediaTaskStatusFailed:     2,
	model.MediaTaskStatusExpired:    2,
}

// MediaTaskTerminalStatuses 返回全部终态（含存储层专有的 expired）。
func MediaTaskTerminalStatuses() []string {
	return []string{
		model.MediaTaskStatusCompleted,
		model.MediaTaskStatusFailed,
		model.MediaTaskStatusExpired,
	}
}

// MediaTaskActiveStatuses 返回全部非终态（sweeper 的扫描范围）。
func MediaTaskActiveStatuses() []string {
	return []string{model.MediaTaskStatusQueued, model.MediaTaskStatusInProgress}
}

// CanAdvanceMediaStatus 判定状态迁移是否被允许（状态机只前进、终态不回退）。
//
// 规则（三条，逐条都有测试）：
//  1. 当前状态不是媒体词表（legacy 行，或旧链路刚创建的 submitted/running/succeeded 行）
//     → **放行**。这是向后兼容的核心：Seedance 旧链路的 submitted→running→succeeded
//     （以及任何 legacy 状态之间的迁移）必须原样工作，不能被新状态机拦下。
//  2. 当前是媒体词表、目标不是 → **拒绝**。媒体任务不得被写回 legacy 词表，
//     否则看板与新状态机会出现两套口径（例如把 completed 改回 succeeded）。
//  3. 两侧都是媒体词表 → 只允许 rank 增大；rank 相同仅允许「同状态重放」
//     （用于幂等刷新 progress/usage），不得在 completed/failed/expired 之间横跳。
func CanAdvanceMediaStatus(from, to string) bool {
	fromRank, fromIsMedia := mediaTaskStatusRank[from]
	if !fromIsMedia {
		return true
	}
	toRank, toIsMedia := mediaTaskStatusRank[to]
	if !toIsMedia {
		return false
	}
	if from == to {
		return true
	}
	return toRank > fromRank
}

// MediaTaskUpdateFields 一次媒体任务增量更新。
//
// 零值语义（与既有 UpdateFields 保持一致，均为「不修改」）：
//   - ExpectedStatus 非空 → 启用 CAS：当前状态必须等于它才更新；
//   - 字符串字段空串 = 不修改；指针字段 nil = 不修改（因此 cost_micro_usd=0 可被显式写入）。
type MediaTaskUpdateFields struct {
	ExpectedStatus string

	Status       string
	Progress     string
	Usage        *model.JSONRaw
	CostMicroUSD *int64
	BilledAt     *time.Time
	PollFailures *int

	// VendorTaskID 上游任务 id（提交后回填）。
	VendorTaskID string

	// 旧链路字段（PATCH /:id 一并承载，保持向后兼容）。
	VideoURL     string
	ResponseBody string
	ErrorMessage string
}

// getByTaskID 读单条（不含 events）。
func (r *VideoTaskRepo) getByTaskID(taskID string) (*model.VideoTask, error) {
	var task model.VideoTask
	if err := r.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetMediaTask 按 task_id 读单条全量（内部 API 用；不做归属过滤，调用方必须已过 InternalAuth）。
func (r *VideoTaskRepo) GetMediaTask(taskID string) (*model.VideoTask, error) {
	return r.getByTaskID(taskID)
}

// GetByIdempotencyKey 按 (api_key_id, idempotency_key) 查既有任务。
//
// 命中即「重复提交」，调用方必须直接返回既有任务、**不新建、不打上游**
// （唯一索引 uniq_media_tasks_idem 是并发下的最终防线，见 CreateMediaTask）。
// 任一参数为空时返回 gorm.ErrRecordNotFound（空幂等键不参与幂等）。
func (r *VideoTaskRepo) GetByIdempotencyKey(apiKeyID, idempotencyKey string) (*model.VideoTask, error) {
	if apiKeyID == "" || idempotencyKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var task model.VideoTask
	if err := r.db.
		Where("api_key_id = ? AND idempotency_key = ?", apiKeyID, idempotencyKey).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// CreateMediaTask 创建媒体任务并报告是否真的新建（幂等，绝不重复出图/出视频）。
//
// 返回 (created=false, existing, nil) 的两种情况：
//  1. (api_key_id, idempotency_key) 已存在 → 直接返回既有任务（快路径，不发 INSERT）；
//  2. 并发下唯一索引被抢（task_id 或幂等键）→ 回读既有任务返回。
//
// 返回 (created=true, task, nil) 时会顺带写一条事件（与既有 Create 行为一致）。
// 宁可返回错误也不静默吞掉冲突：找不到既有行时返回 error，由调用方返 5xx。
func (r *VideoTaskRepo) CreateMediaTask(task *model.VideoTask) (bool, *model.VideoTask, error) {
	if task == nil {
		return false, nil, fmt.Errorf("repository: media task is nil")
	}
	if key := mediaTaskIdempotencyKey(task); key != "" {
		existing, err := r.GetByIdempotencyKey(task.APIKeyID, key)
		switch {
		case err == nil:
			return false, existing, nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return false, nil, err
		}
	}

	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(task)
	if result.Error != nil {
		return false, nil, result.Error
	}
	if result.RowsAffected == 1 {
		if err := r.createEvent(task.TaskID, task.Status, "任务已提交"); err != nil {
			return true, task, err
		}
		return true, task, nil
	}

	// 冲突（task_id 或幂等唯一键）：回读既有行。
	if existing, err := r.getByTaskID(task.TaskID); err == nil {
		return false, existing, nil
	}
	if key := mediaTaskIdempotencyKey(task); key != "" {
		if existing, err := r.GetByIdempotencyKey(task.APIKeyID, key); err == nil {
			return false, existing, nil
		}
	}
	return false, nil, fmt.Errorf("repository: media task conflict without existing row: task_id=%s", task.TaskID)
}

// UpdateMediaTaskCAS 以 CAS 语义增量更新媒体任务。
//
// 返回 (updated, task, err)：
//   - updated=true  → 本次写入生效，task 为写入后的最新快照；
//   - updated=false → 未写入，task 为当前快照（CAS 不匹配 / 状态机拒绝 / 无可写字段 / 并发抢输）。
//     **不是错误**：调用方应读 task 决定下一步；
//   - err != nil → 记录不存在（gorm.ErrRecordNotFound，调用方转 404）或数据库故障（调用方转 5xx）。
//
// 并发安全：读出当前状态后用 `WHERE task_id = ? AND status = <读到的状态>` 做条件更新
// （乐观锁），因此即使有并发写入也不会互相覆盖；抢输时最多重读重试 mediaTaskCASMaxAttempts 次。
//
// 时间字段由服务端维护，不由调用方传入：
//   - 首次进入 in_progress → started_at（用于超时判定，不被轮询刷新）；
//   - 首次进入任一终态 → finished_at；
//   - updated_at **只在状态真正变化时**刷新（保持既有「同一状态轮询不延长耗时」的语义，
//     并使 updated_at 成为可信的「停留时长」基准，超时扫描依赖它）。
func (r *VideoTaskRepo) UpdateMediaTaskCAS(taskID string, fields MediaTaskUpdateFields) (bool, *model.VideoTask, error) {
	if taskID == "" {
		return false, nil, fmt.Errorf("repository: media task id required")
	}
	next := strings.TrimSpace(fields.Status)
	expected := strings.TrimSpace(fields.ExpectedStatus)

	for attempt := 0; attempt < mediaTaskCASMaxAttempts; attempt++ {
		current, err := r.getByTaskID(taskID)
		if err != nil {
			return false, nil, err
		}

		if expected != "" && !mediaTaskExpectedStatusMatch(expected, current.Status) {
			return false, current, nil
		}
		if next != "" && !CanAdvanceMediaStatus(current.Status, next) {
			return false, current, nil
		}

		now := time.Now().UTC()
		updates := map[string]interface{}{}
		if next != "" {
			updates["status"] = next
		}
		if fields.Progress != "" {
			updates["progress"] = fields.Progress
		}
		if fields.Usage != nil {
			updates["usage"] = *fields.Usage
		}
		if fields.CostMicroUSD != nil {
			updates["cost_micro_usd"] = *fields.CostMicroUSD
		}
		if fields.BilledAt != nil {
			updates["billed_at"] = fields.BilledAt.UTC()
		}
		if fields.PollFailures != nil {
			updates["poll_failures"] = *fields.PollFailures
		}
		if fields.VendorTaskID != "" {
			updates["vendor_task_id"] = fields.VendorTaskID
		}
		if fields.VideoURL != "" {
			updates["video_url"] = fields.VideoURL
		}
		if fields.ResponseBody != "" {
			updates["response_body"] = fields.ResponseBody
		}
		if fields.ErrorMessage != "" {
			updates["error_message"] = fields.ErrorMessage
		}
		if next == model.MediaTaskStatusInProgress && current.StartedAt == nil {
			updates["started_at"] = now
		}
		if model.IsMediaTaskTerminalStatus(next) && current.FinishedAt == nil {
			updates["finished_at"] = now
		}
		statusChanged := next != "" && next != current.Status
		if statusChanged {
			updates["updated_at"] = now
		}
		if len(updates) == 0 {
			return false, current, nil
		}

		var updated bool
		err = r.db.Transaction(func(tx *gorm.DB) error {
			// UpdateColumns（而非 Updates）：GORM 的 Updates 会**自动**追加 updated_at，
			// 那会让同状态轮询不断续命、使超时扫描永远不命中。这里由本方法显式决定
			// updated_at 是否出现在 updates 里（只在状态真正变化时写）。
			result := tx.Model(&model.VideoTask{}).
				Where("task_id = ? AND status = ?", taskID, current.Status).
				UpdateColumns(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil // 乐观锁失败，交给外层重读重试
			}
			updated = true
			if !statusChanged {
				return nil
			}
			return tx.Create(&model.VideoTaskEvent{
				TaskID:    taskID,
				EventType: next,
				EventData: "状态更新为 " + next,
				CreatedAt: now,
			}).Error
		})
		if err != nil {
			return false, nil, err
		}
		if updated {
			fresh, readErr := r.getByTaskID(taskID)
			if readErr != nil {
				return true, nil, readErr
			}
			return true, fresh, nil
		}
	}

	// 多次抢输：返回最新快照（不报错，调用方下轮重试）。
	fresh, err := r.getByTaskID(taskID)
	if err != nil {
		return false, nil, err
	}
	return false, fresh, nil
}

// mediaTaskExpectedStatusMatch 判定 CAS 的期望状态是否匹配当前状态。
//
// 除精确相等外，额外接受「调用方看到的对外状态」：调用方从内部 API 读到的 status 已被
// 归一（expired → failed），因此 expected_status=failed 也允许匹配 DB 里的 expired，
// 避免「读到 failed、CAS 却永远失败」的死循环。反向（expected=expired）保持精确匹配。
func mediaTaskExpectedStatusMatch(expected, current string) bool {
	if expected == current {
		return true
	}
	return expected == model.MediaTaskStatusFailed && current == model.MediaTaskStatusExpired
}

// ListTimedOut 扫描超过 cutoff 仍未进入终态的媒体任务（sweeper 用）。
//
// 判定口径：status ∈ {queued, in_progress} 且 COALESCE(started_at, updated_at) < cutoff。
//   - 已开始的取 started_at（首次进入 in_progress 的时刻）；
//   - 仍在排队的取 updated_at（= 创建后未发生状态变化，同 created_at）。
//
// ⚠️ 刻意不用 updated_at 作为已开始任务的起算点：它的语义是「最后一次状态变化」，
// 若改成「最后一次写入」，长期轮询会被不断续命、永远不超时 —— 那正是要消灭的「任务卡死」。
// legacy 词表行（submitted/running/succeeded）**不在扫描范围**：它们由旧链路自己收敛。
func (r *VideoTaskRepo) ListTimedOut(cutoff time.Time, limit int) ([]model.VideoTask, error) {
	query := r.db.Model(&model.VideoTask{}).
		Where("status IN ?", MediaTaskActiveStatuses()).
		Where("COALESCE(started_at, updated_at) < ?", cutoff.UTC()).
		Order("COALESCE(started_at, updated_at) ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var tasks []model.VideoTask
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListEvents 返回任务的事件时间线（按时间与 id 升序，与 GetByIDScoped 的 Preload 一致）。
func (r *VideoTaskRepo) ListEvents(taskID string) ([]model.VideoTaskEvent, error) {
	var events []model.VideoTaskEvent
	if err := r.db.
		Where("task_id = ?", taskID).
		Order("created_at ASC").
		Order("id ASC").
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}
