package service

import (
	"context"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// CatalogService 是 handler 接到的"门面"，把 search / lookup / sync / diff 集中暴露。
//
// 它内部组合 ModelCatalogRepo（读写） + CatalogSyncService（远程同步 + diff 计算），
// 让 handler 只依赖一个接口、一个对象。
type CatalogService struct {
	repo     *repository.ModelCatalogRepo
	syncSvc  *CatalogSyncService
}

// NewCatalogService 组合构造。
func NewCatalogService(repo *repository.ModelCatalogRepo, syncSvc *CatalogSyncService) *CatalogService {
	return &CatalogService{repo: repo, syncSvc: syncSvc}
}

// Search 透传 repo.Search。注意签名是 (results, total, error)：
// total 是匹配条件的全表总数（不受 opts.Limit 影响），handler 直接把它当作 JSON
// 的 "total" 字段返回给前端，避免前端把 len(results) 误当作总数。
func (s *CatalogService) Search(opts repository.CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
	return s.repo.Search(opts)
}

func (s *CatalogService) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	return s.repo.GetByID(id)
}

func (s *CatalogService) GetBySourceModelID(source, id string) (*model.ModelCatalog, error) {
	return s.repo.GetBySourceModelID(source, id)
}

// TriggerAsyncSync 在后台 goroutine 触发 SyncFromRemote + ApplyAutoUpdates；
// handler 立即拿到 202，前端轮询状态即可。
func (s *CatalogService) TriggerAsyncSync(_ context.Context) {
	go func() {
		ctx := context.Background()
		if _, err := s.syncSvc.SyncFromRemote(ctx); err != nil {
			slog.Error("catalog sync failed", "error", err)
			return
		}
		if _, err := s.syncSvc.ApplyAutoUpdates(ctx); err != nil {
			slog.Warn("catalog auto-update failed", "error", err)
		}
	}()
}

func (s *CatalogService) IsSyncing() bool {
	return s.syncSvc.IsSyncing()
}

// SyncStatus 返回最近一次同步的全部观察值（运行中？上次开始/结束时间、耗时、来源、
// 条目数、错误信息、目录总数），供前端轮询展示同步状态。
func (s *CatalogService) SyncStatus(ctx context.Context) (*SyncStatus, error) {
	return s.syncSvc.SyncStatus(ctx)
}

func (s *CatalogService) ComputePriceDiff(ctx context.Context) ([]PriceDiffEntry, error) {
	return s.syncSvc.ComputePriceDiff(ctx)
}

func (s *CatalogService) ApplyPriceDiff(ctx context.Context, ids []uuid.UUID) (int, error) {
	return s.syncSvc.ApplyPriceDiff(ctx, ids)
}

// AutoBind 将本地未绑定 model 按 name/upstream_model_id 严格同名匹配到 catalog 条目，
// 唯一命中直接绑定，多重命中作为候选返回让 admin 选。
func (s *CatalogService) AutoBind(ctx context.Context) (*AutoBindResult, error) {
	return s.syncSvc.AutoBind(ctx)
}

// BindingStats 返回 catalog 条目总数与本地模型的绑定分布，供前端 CatalogSyncPage
// 在 diff 为空时做空状态语义判定（目录空 / 本地无 model / 未绑定 / 全部绑定）。
func (s *CatalogService) BindingStats(ctx context.Context) (*BindingStats, error) {
	return s.syncSvc.BindingStats(ctx)
}
