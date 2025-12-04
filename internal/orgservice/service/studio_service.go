package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
)

var (
	ErrStudioHasDepartments = errors.New("工作室下存在部门，无法删除")
	ErrStudioDuplicate      = errors.New("工作室名称已存在")
	ErrStudioNotFound       = errors.New("工作室不存在")
)

// StudioService 工作室管理业务逻辑
type StudioService struct {
	studioRepo *repository.StudioRepo
}

func NewStudioService(studioRepo *repository.StudioRepo) *StudioService {
	return &StudioService{studioRepo: studioRepo}
}

// Create 创建工作室
func (s *StudioService) Create(ctx context.Context, studio *model.Studio) error {
	return s.studioRepo.Create(studio)
}

// List 分页查询工作室
func (s *StudioService) List(ctx context.Context, page, pageSize int) ([]model.Studio, int64, error) {
	return s.studioRepo.List(page, pageSize)
}

// GetByID 查询工作室详情
func (s *StudioService) GetByID(ctx context.Context, id string) (*model.Studio, error) {
	return s.studioRepo.GetByID(id)
}

// Delete 删除工作室（有关联部门时拒绝）
func (s *StudioService) Delete(ctx context.Context, id string) error {
	has, err := s.studioRepo.HasDepartments(id)
	if err != nil {
		return fmt.Errorf("check departments: %w", err)
	}
	if has {
		return ErrStudioHasDepartments
	}
	return s.studioRepo.Delete(id)
}
