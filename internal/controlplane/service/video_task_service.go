package service

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
)

type VideoTaskService struct {
	repo *repository.VideoTaskRepo
}

func NewVideoTaskService(repo *repository.VideoTaskRepo) *VideoTaskService {
	return &VideoTaskService{repo: repo}
}

func (s *VideoTaskService) List(page, pageSize int, providerName, modelName, status string) ([]model.VideoTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.List(page, pageSize, providerName, modelName, status)
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

func (s *VideoTaskService) Stats() (map[string]interface{}, error) {
	return s.repo.Stats()
}
