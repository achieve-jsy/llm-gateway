package service

import (
	"context"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
)

// StudioService 工作室只读查询（工作室由 EHR 同步维护，不再手工增删）。
type StudioService struct {
	studioRepo *repository.StudioRepo
}

func NewStudioService(studioRepo *repository.StudioRepo) *StudioService {
	return &StudioService{studioRepo: studioRepo}
}

// List 分页查询工作室
func (s *StudioService) List(_ context.Context, page, pageSize int) ([]model.Studio, int64, error) {
	return s.studioRepo.List(page, pageSize)
}

// GetByID 查询工作室详情
func (s *StudioService) GetByID(_ context.Context, id string) (*model.Studio, error) {
	return s.studioRepo.GetByID(id)
}
