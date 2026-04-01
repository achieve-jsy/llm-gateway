package model

// ModelEntry 模型元数据
type ModelEntry struct {
	Name            string `json:"name"`
	UpstreamModelID string `json:"upstream_model_id,omitempty"`
}

// ProviderEntry 厂商通道元数据（来自 control-plane）
type ProviderEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Endpoint    string   `json:"endpoint"`
	Credentials string   `json:"credentials"`
	Enabled     bool     `json:"enabled"`
	Models      []string `json:"models"` // 保留以支持旧版本反序列化
	ModelsV2    []ModelEntry `json:"models_v2,omitempty"` // 新版本字段，包含 upstream_model_id
}
