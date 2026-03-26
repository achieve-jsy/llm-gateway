package video

import (
	"context"
	"encoding/json"
)

// VideoProvider 视频生成能力接口
type VideoProvider interface {
	// CreateTask 创建视频生成任务
	// req: 原始请求 JSON（直通，不解析）
	// 返回: 上游返回的 task_id
	CreateTask(ctx context.Context, req json.RawMessage) (taskID string, err error)

	// GetTask 查询任务状态
	// taskID: 上游返回的 task_id
	// 返回: 原始响应 JSON（直通，不解析）
	GetTask(ctx context.Context, taskID string) (json.RawMessage, error)
}

// AssetProvider 素材库能力接口
type AssetProvider interface {
	// DoAction 执行素材操作
	// action: CreateAssetGroup / CreateAsset / GetAsset / ListAssets / etc.
	// req: 原始请求 JSON（直通）
	// 返回: 原始响应 JSON（直通）
	DoAction(ctx context.Context, action string, req json.RawMessage) (json.RawMessage, error)
}
