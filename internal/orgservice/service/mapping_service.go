package service

import (
	"context"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
)

// MappingService 部门↔工作室映射的只读查询。
// 映射现在只由 EHR 同步写入（repository.EHRProjectionRepo），没有手工入口。
type MappingService struct {
	mappingRepo *repository.MappingRepo
}

func NewMappingService(mappingRepo *repository.MappingRepo) *MappingService {
	return &MappingService{mappingRepo: mappingRepo}
}

// List 查询映射列表
func (s *MappingService) List(_ context.Context, page, pageSize int) ([]model.DeptStudioMap, int64, error) {
	return s.mappingRepo.List(page, pageSize)
}
