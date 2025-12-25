package model

// UserIdentity represents the authenticated user identity
type UserIdentity struct {
	UserID          string `json:"user_id"`
	KeyID           string `json:"key_id"`
	DepartmentID    string `json:"department_id"`
	StudioID        string `json:"studio_id,omitempty"`
	AccessAllGroups bool   `json:"access_all_groups"`
	LogEnabled      bool   `json:"log_enabled"`
	// v0.7: 可用模型组名称集合（public ∪ 本部门已开通 controlled），由 verify-key 快照下发。
	// group_dispatcher 据此判定授权而非依赖 key_group_bindings。
	UsableGroups []string `json:"usable_groups,omitempty"`
	// v0.7: Key 类型（personal/service）
	KeyType string `json:"key_type,omitempty"`
}

// MeModelItem represents a single model in /v1/me/models response
type MeModelItem struct {
	ID                  string `json:"id"`
	Object              string `json:"object"`
	OwnedBy             string `json:"owned_by"`
	Type                string `json:"type"`
	Enabled             bool   `json:"enabled"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastCheckError      string `json:"last_check_error,omitempty"`
	ResponseTime        int    `json:"response_time"`
}

// MeModelsResponse is the response for /v1/me/models endpoint
type MeModelsResponse struct {
	Object string        `json:"object"`
	Data   []MeModelItem `json:"data"`
}
