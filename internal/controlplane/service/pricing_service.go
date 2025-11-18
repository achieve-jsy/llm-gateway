package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/example/llm-gateway/internal/core"
)

// PricingService 定价查询服务
type PricingService struct {
	table *pricing.PricingTable
	db    *gorm.DB
}

// NewPricingService 创建 PricingService，并初始化加载价表
func NewPricingService(db *gorm.DB) *PricingService {
	table := pricing.NewPricingTable()
	return &PricingService{table: table, db: db}
}

// GetAllPricing 返回全量定价（从内存价表）
func (s *PricingService) GetAllPricing(ctx context.Context) (map[string]core.ModelPricing, error) {
	return s.table.GetAll(), nil
}

// Refresh 刷新内存价表
func (s *PricingService) Refresh(ctx context.Context) error {
	if err := s.table.Refresh(ctx, s.db); err != nil {
		return fmt.Errorf("refresh pricing table: %w", err)
	}
	return nil
}

// Resolve 查询单个模型定价（实现 PricingResolver 接口）
func (s *PricingService) Resolve(modelKey string) (core.ModelPricing, bool) {
	return s.table.Resolve(modelKey)
}