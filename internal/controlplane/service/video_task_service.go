package service

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
)

type VideoTaskService struct {
	repo *repository.VideoTaskRepo
}

type VideoTaskUpdateFields struct {
	Status       string
	VideoURL     string
	ResponseBody string
	ErrorMessage string
}

type VideoTaskScope = repository.VideoTaskScope

func NewVideoTaskService(repo *repository.VideoTaskRepo) *VideoTaskService {
	return &VideoTaskService{repo: repo}
}

func (s *VideoTaskService) List(page, pageSize int, providerName, modelName, status, userID string) ([]model.VideoTask, int64, error) {
	return s.ListScoped(page, pageSize, providerName, modelName, status, userID, VideoTaskScope{IsGlobal: true})
}

func (s *VideoTaskService) ListScoped(page, pageSize int, providerName, modelName, status, userID string, scope VideoTaskScope) ([]model.VideoTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.ListScoped(page, pageSize, providerName, modelName, status, userID, scope)
}

func (s *VideoTaskService) GetByID(taskID string) (*model.VideoTask, error) {
	return s.repo.GetByID(taskID)
}

func (s *VideoTaskService) GetByIDScoped(taskID string, scope VideoTaskScope) (*model.VideoTask, error) {
	return s.repo.GetByIDScoped(taskID, scope)
}

func (s *VideoTaskService) GetByIDAndUserID(taskID, userID string) (*model.VideoTask, error) {
	return s.repo.GetByIDAndUserID(taskID, userID)
}

func (s *VideoTaskService) Create(task *model.VideoTask) error {
	return s.repo.Create(task)
}

func (s *VideoTaskService) UpdateStatus(taskID, status string) error {
	return s.repo.UpdateStatus(taskID, status)
}

func (s *VideoTaskService) Update(taskID string, fields VideoTaskUpdateFields) error {
	return s.repo.UpdateFields(taskID, repository.VideoTaskUpdateFields{
		Status:       fields.Status,
		VideoURL:     fields.VideoURL,
		ResponseBody: fields.ResponseBody,
		ErrorMessage: fields.ErrorMessage,
	})
}

func (s *VideoTaskService) GetProviderType(taskID string) (string, error) {
	return s.repo.GetProviderType(taskID)
}

func (s *VideoTaskService) GetTaskMeta(taskID string) (*repository.VideoTaskMeta, error) {
	return s.repo.GetTaskMeta(taskID)
}

func (s *VideoTaskService) Stats(start, end, userID, providerName, modelName string) (map[string]interface{}, error) {
	return s.repo.Stats(start, end, userID, providerName, modelName)
}

func (s *VideoTaskService) StatsScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) (map[string]interface{}, error) {
	return s.repo.StatsScoped(start, end, userID, providerName, modelName, scope)
}

func (s *VideoTaskService) StatsTrend(start, end, userID, providerName, modelName string) ([]repository.TrendRow, error) {
	return s.repo.StatsTrend(start, end, userID, providerName, modelName)
}

func (s *VideoTaskService) StatsTrendScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) ([]repository.TrendRow, error) {
	return s.repo.StatsTrendScoped(start, end, userID, providerName, modelName, scope)
}

func (s *VideoTaskService) StatsDistribution(start, end, userID, providerName, modelName string) (repository.DistributionRows, error) {
	return s.repo.StatsDistribution(start, end, userID, providerName, modelName)
}

func (s *VideoTaskService) StatsDistributionScoped(start, end, userID, providerName, modelName string, scope VideoTaskScope) (repository.DistributionRows, error) {
	return s.repo.StatsDistributionScoped(start, end, userID, providerName, modelName, scope)
}

// ═══════════════════════════════════════════════════════════════════════════════
// C2/C3：媒体任务平台（media_tasks）的服务增量
//
// 与上面 legacy Seedance 方法**并存**，互不改变语义：
//   - InternalCreate / InternalUpdate 走下面的媒体方法（新增字段 + CAS + 幂等）；
//   - InternalUpdateStatus（PATCH /:id/status）继续走 legacy Update，行为逐字不变。
// ═══════════════════════════════════════════════════════════════════════════════

// MediaTaskUpdateFields 是媒体任务增量更新的服务层入参（= repository 同名字段 + 校验）。
type MediaTaskUpdateFields = repository.MediaTaskUpdateFields

// CreateMediaTask 创建媒体任务并报告是否真的新建（幂等）。
//
// 幂等语义由 repository.CreateMediaTask 保证：
// (api_key_id, idempotency_key) 命中 → (false, 既有任务, nil)，**不新建、不打上游**。
func (s *VideoTaskService) CreateMediaTask(task *model.VideoTask) (bool, *model.VideoTask, error) {
	return s.repo.CreateMediaTask(task)
}

// UpdateMediaTaskCAS 以 CAS 语义更新媒体任务（状态只前进、终态不回退）。
//
// 返回的 task 状态已是**存储层原值**；面向调用方时用 model.CallerMediaTaskStatus 归一
// （expired → failed），由 handler 负责。
func (s *VideoTaskService) UpdateMediaTaskCAS(taskID string, fields MediaTaskUpdateFields) (bool, *model.VideoTask, error) {
	return s.repo.UpdateMediaTaskCAS(taskID, fields)
}

// GetMediaTask 按 task_id 读单条全量（内部 API 用）。
func (s *VideoTaskService) GetMediaTask(taskID string) (*model.VideoTask, error) {
	return s.repo.GetMediaTask(taskID)
}

// GetByIdempotencyKey 按 (api_key_id, idempotency_key) 查既有任务（幂等查询）。
func (s *VideoTaskService) GetByIdempotencyKey(apiKeyID, idempotencyKey string) (*model.VideoTask, error) {
	return s.repo.GetByIdempotencyKey(apiKeyID, idempotencyKey)
}

// ListEvents 任务事件时间线。
func (s *VideoTaskService) ListEvents(taskID string) ([]model.VideoTaskEvent, error) {
	return s.repo.ListEvents(taskID)
}

// ListTimedOut 扫描超时未终态任务（sweeper 用）。
func (s *VideoTaskService) ListTimedOut(cutoff time.Time, limit int) ([]model.VideoTask, error) {
	return s.repo.ListTimedOut(cutoff, limit)
}
