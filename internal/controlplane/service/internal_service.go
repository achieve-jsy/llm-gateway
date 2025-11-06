package service

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/google/uuid"
)

// InternalService 内部接口服务
type InternalService struct {
	modelRepo    ModelRepoInterface
	providerRepo ProviderRepoInterface
}

// NewInternalService 创建 InternalService 实例
func NewInternalService(modelRepo ModelRepoInterface, providerRepo ProviderRepoInterface) *InternalService {
	return &InternalService{
		modelRepo:    modelRepo,
		providerRepo: providerRepo,
	}
}

// AvailableModel 可用模型信息（含渠道详情，供 gateway-api 消费）
type AvailableModel struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	ModelType           string    `json:"model_type"`
	ProviderID          uuid.UUID `json:"provider_id"`
	ProviderName        string    `json:"provider_name"`
	ProviderType        string    `json:"provider_type"`
	ProviderEndpoint    string    `json:"provider_endpoint"`
	ProviderCredentials string    `json:"provider_credentials"`
}

// ListAvailableModels 查询所有可用模型（T045）
func (s *InternalService) ListAvailableModels() ([]AvailableModel, error) {
	models, err := s.modelRepo.ListAvailable()
	if err != nil {
		return nil, err
	}

	result := make([]AvailableModel, 0, len(models))
	for _, m := range models {
		// 解密凭证供 gateway-api 使用
		credentials, _ := crypto.Decrypt(m.Provider.EncryptedCredentials)

		result = append(result, AvailableModel{
			ID:                  m.ID,
			Name:                m.Name,
			ModelType:           m.ModelType,
			ProviderID:          m.ProviderID,
			ProviderName:        m.Provider.Name,
			ProviderType:        m.Provider.Type,
			ProviderEndpoint:    m.Provider.Endpoint,
			ProviderCredentials: credentials,
		})
	}

	return result, nil
}

// CheckModelNameExists 检查模型名称是否已存在（供前端实时校验）
func (s *InternalService) CheckModelNameExists(name string, excludeID *uuid.UUID) (bool, error) {
	if excludeID != nil {
		return s.modelRepo.ExistsByNameExcluding(name, *excludeID)
	}
	return s.modelRepo.ExistsByName(name)
}

// GetProviderWithDecryptedCredentials 获取渠道含解密凭证（内部使用）
func (s *InternalService) GetProviderWithDecryptedCredentials(id uuid.UUID) (*model.Provider, string, error) {
	provider, err := s.providerRepo.GetByID(id)
	if err != nil {
		return nil, "", err
	}
	credentials, err := crypto.Decrypt(provider.EncryptedCredentials)
	if err != nil {
		return provider, "", err
	}
	return provider, credentials, nil
}
