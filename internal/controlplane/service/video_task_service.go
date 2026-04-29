package service

import (
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
