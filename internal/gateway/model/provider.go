package model

// ProviderEntry 厂商通道元数据（来自 control-plane）
type ProviderEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Endpoint    string   `json:"endpoint"`
	Credentials string   `json:"credentials"`
	Enabled     bool     `json:"enabled"`
	Models      []string `json:"models"`
}
