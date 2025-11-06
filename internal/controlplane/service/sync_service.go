package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// SyncProviderRepo SyncService 依赖的 ProviderRepo 子集
type SyncProviderRepo interface {
	GetByID(id uuid.UUID) (*model.Provider, error)
}

// SyncModelRepo SyncService 依赖的 ModelRepo 子集
type SyncModelRepo interface {
	List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error)
	ExistsByName(name string) (bool, error)
	Create(m *model.Model) error
	BatchCreate(models []*model.Model) error
}

// SyncService 模型同步服务
type SyncService struct {
	providerRepo SyncProviderRepo
	modelRepo    SyncModelRepo
	publisher    *event.Publisher
	snapshotter  *ProviderSnapshotRefresher
}

// NewSyncService 创建 SyncService
func NewSyncService(providerRepo SyncProviderRepo, modelRepo SyncModelRepo, publisher *event.Publisher) *SyncService {
	return &SyncService{providerRepo: providerRepo, modelRepo: modelRepo, publisher: publisher}
}

// SetSnapshotRefresher injects the shared provider snapshot refresher.
func (s *SyncService) SetSnapshotRefresher(snapshotter *ProviderSnapshotRefresher) {
	s.snapshotter = snapshotter
}

// SyncModelItem 同步结果中的模型条目
type SyncModelItem struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// SyncResult 同步对比结果
type SyncResult struct {
	Added         []SyncModelItem `json:"added"`
	Existing      []SyncModelItem `json:"existing"`
	Removed       []SyncModelItem `json:"removed"`
	TotalUpstream int             `json:"total_upstream"`
	TotalLocal    int             `json:"total_local"`
	Unsupported   bool            `json:"unsupported"`
}

// ConfirmSyncInput 确认同步请求
type ConfirmSyncInput struct {
	ModelsToAdd []string `json:"models_to_add" binding:"required"`
}

// ConfirmSyncResult 确认同步结果
type ConfirmSyncResult struct {
	Registered int `json:"registered"`
	Skipped    int `json:"skipped"`
	Disabled   int `json:"disabled"`
}

// SyncModels 从厂商拉取模型列表并与本地对比
func (s *SyncService) SyncModels(providerID uuid.UUID) (*SyncResult, error) {
	// 获取渠道
	provider, err := s.providerRepo.GetByID(providerID)
	if err != nil || provider == nil {
		return nil, fmt.Errorf("provider not found")
	}

	if !provider.Enabled {
		return nil, fmt.Errorf("provider is disabled")
	}

	// 检查是否支持同步（Anthropic/Bedrock 不支持 /models 接口）
	if isUnsupportedAdapter(provider.DefinitionID) {
		return &SyncResult{Unsupported: true}, nil
	}

	// 解密凭证
	credJSON, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials")
	}

	apiKey := extractAPIKey(credJSON)

	// 调用厂商 GET /models
	upstreamModels, err := fetchUpstreamModels(provider.Endpoint, apiKey)
	if err != nil {
		return nil, fmt.Errorf("upstream error: %v", err)
	}

	// 获取本地已注册模型
	localModels, _, err := s.modelRepo.List(repository.ModelListFilter{ProviderID: &providerID}, 0, 10000)
	if err != nil {
		return nil, fmt.Errorf("failed to query local models")
	}

	// 对比分类
	localMap := make(map[string]bool)
	for _, m := range localModels {
		localMap[m.Name] = true
	}

	upstreamMap := make(map[string]bool)
	added := make([]SyncModelItem, 0)
	existing := make([]SyncModelItem, 0)
	for _, um := range upstreamModels {
		upstreamMap[um.ID] = true
		item := SyncModelItem{ID: um.ID, OwnedBy: um.OwnedBy}
		if localMap[um.ID] {
			existing = append(existing, item)
		} else {
			added = append(added, item)
		}
	}

	// 找出已下架的（本地有但上游没有）
	removed := make([]SyncModelItem, 0)
	if len(upstreamModels) > 0 { // 空列表不做下架检测
		for _, m := range localModels {
			if !upstreamMap[m.Name] {
				removed = append(removed, SyncModelItem{ID: m.Name, OwnedBy: ""})
			}
		}
	}

	return &SyncResult{
		Added:         added,
		Existing:      existing,
		Removed:       removed,
		TotalUpstream: len(upstreamModels),
		TotalLocal:    len(localModels),
	}, nil
}

// ConfirmSync 确认批量注册 + 禁用已下架
func (s *SyncService) ConfirmSync(providerID uuid.UUID, input ConfirmSyncInput) (*ConfirmSyncResult, error) {
	// 1. 查询已存在的模型名（一次性查询，避免 N+1）
	existingNames := make(map[string]bool)
	localModels, _, _ := s.modelRepo.List(repository.ModelListFilter{ProviderID: &providerID}, 0, 10000)
	for _, m := range localModels {
		existingNames[m.Name] = true
	}

	// 2. 过滤出真正需要新增的模型
	var toCreate []*model.Model
	skipped := 0
	for _, name := range input.ModelsToAdd {
		if existingNames[name] {
			skipped++
			continue
		}
		enabled := false
		toCreate = append(toCreate, &model.Model{
			Name:       name,
			ProviderID: providerID,
			ModelType:  inferModelType(name),
			Enabled:    &enabled,
		})
	}

	// 3. 批量 INSERT（一次数据库操作）
	registered := 0
	if len(toCreate) > 0 {
		if err := s.modelRepo.BatchCreate(toCreate); err != nil {
			return nil, fmt.Errorf("batch create models: %v", err)
		}
		registered = len(toCreate)
	}

	// 批量注册完成后发布一次事件通知
	if registered > 0 {
		s.publishEvent(event.ModelCreated, "model", providerID)
	}

	return &ConfirmSyncResult{
		Registered: registered,
		Skipped:    skipped,
		Disabled:   0,
	}, nil
}

// publishEvent 安全发布事件（publisher 为 nil 或发布失败不影响业务）
func (s *SyncService) publishEvent(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	snapshotVersion := int64(0)
	if s.snapshotter != nil {
		version, err := s.snapshotter.RefreshAvailableSnapshot(context.Background())
		if err != nil {
			slog.Error("failed to refresh provider snapshot", "type", eventType, "resource_id", resourceID, "error", err)
		} else {
			snapshotVersion = version
		}
	}
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID, snapshotVersion)
	if err := s.publisher.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish event", "type", eventType, "resource_id", resourceID, "error", err)
	}
}

// isUnsupportedAdapter 判断渠道定义是否支持 /models 接口
func isUnsupportedAdapter(definitionID string) bool {
	unsupported := []string{"anthropic", "bedrock"}
	for _, u := range unsupported {
		if definitionID == u {
			return true
		}
	}
	return false
}

// upstreamModel 上游模型信息
type upstreamModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// fetchUpstreamModels 调用厂商 GET /models 接口
func fetchUpstreamModels(endpoint, apiKey string) ([]upstreamModel, error) {
	url := strings.TrimRight(endpoint, "/") + "/models"

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []upstreamModel `json:"data"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %v", err)
	}

	return result.Data, nil
}

// inferModelType 从模型名推断类型
func inferModelType(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "embed") {
		return "embedding"
	}
	return "chat"
}
