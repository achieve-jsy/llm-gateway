package service

import (
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// ModelStatusLogService 模型状态变更日志服务
type ModelStatusLogService struct {
	logRepo *repository.ModelStatusLogRepo
}

func NewModelStatusLogService(logRepo *repository.ModelStatusLogRepo) *ModelStatusLogService {
	return &ModelStatusLogService{logRepo: logRepo}
}

// LogStatusChange 记录模型状态变更日志
func (s *ModelStatusLogService) LogStatusChange(
	modelID, providerID uuid.UUID,
	modelName, providerName string,
	changeType string,
	reason, errorMessage string,
	operatorID *uuid.UUID,
	operatorName string,
) {
	log := &model.ModelStatusLog{
		ModelID:      modelID,
		ModelName:    modelName,
		ProviderID:   providerID,
		ProviderName: providerName,
		ChangeType:   changeType,
		Reason:       reason,
		ErrorMessage: errorMessage,
		OperatorID:   operatorID,
		OperatorName: operatorName,
	}
	if err := s.logRepo.Create(log); err != nil {
		slog.Error("failed to create model status log", "model_id", modelID, "change_type", changeType, "error", err)
	}
}