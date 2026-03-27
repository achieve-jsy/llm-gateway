package handler

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/infra"
)

// CPVideoTaskStoreAdapter 将 infra.VideoTaskClient 适配为 ControlPlaneVideoTaskWriter。
type CPVideoTaskStoreAdapter struct {
	Client *infra.VideoTaskClient
}

func (a *CPVideoTaskStoreAdapter) Create(ctx context.Context, rec VideoTaskRecord) error {
	if a == nil || a.Client == nil {
		return nil
	}
	return a.Client.Create(ctx, infra.CreateVideoTaskRequest{
		TaskID:       rec.TaskID,
		APIKeyID:     rec.APIKeyID,
		APIKeyName:   rec.APIKeyName,
		APIKeyType:   rec.APIKeyType,
		UserID:       rec.UserID,
		UserName:     rec.UserName,
		Model:        rec.Model,
		ProviderName: rec.ProviderName,
		ProviderType: rec.ProviderType,
		Status:       rec.Status,
		RequestBody:  rec.RequestBody,
	})
}

func (a *CPVideoTaskStoreAdapter) Update(ctx context.Context, taskID string, update VideoTaskUpdate) error {
	if a == nil || a.Client == nil {
		return nil
	}
	return a.Client.Update(ctx, taskID, infra.UpdateVideoTaskRequest{
		Status:       update.Status,
		VideoURL:     update.VideoURL,
		ResponseBody: update.ResponseBody,
		ErrorMessage: update.ErrorMessage,
	})
}

func (a *CPVideoTaskStoreAdapter) UpdateStatus(ctx context.Context, taskID, status string) error {
	if a == nil || a.Client == nil {
		return nil
	}
	return a.Update(ctx, taskID, VideoTaskUpdate{Status: status})
}

func (a *CPVideoTaskStoreAdapter) LookupProviderType(ctx context.Context, taskID string) (string, error) {
	if a == nil || a.Client == nil {
		return "", nil
	}
	return a.Client.LookupProviderType(ctx, taskID)
}
