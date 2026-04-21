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

func NewVideoTaskService(repo *repository.VideoTaskRepo) *VideoTaskService {
	return &VideoTaskService{repo: repo}
}

func (s *VideoTaskService) List(page, pageSize int, providerName, modelName, status, userID string) ([]model.VideoTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.List(page, pageSize, providerName, modelName, status, userID)
}

func (s *VideoTaskService) GetByID(taskID string) (*model.VideoTask, error) {
	return s.repo.GetByID(taskID)
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

func (s *VideoTaskService) StatsTrend(start, end, userID, providerName, modelName string) ([]repository.TrendRow, error) {
	return s.repo.StatsTrend(start, end, userID, providerName, modelName)
}

func (s *VideoTaskService) StatsDistribution(start, end, userID, providerName, modelName string) (repository.DistributionRows, error) {
	return s.repo.StatsDistribution(start, end, userID, providerName, modelName)
}
