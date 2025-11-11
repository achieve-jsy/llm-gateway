package model

// UserIdentity represents the authenticated user identity
type UserIdentity struct {
	UserID       string `json:"user_id"`
	KeyID        string `json:"key_id"`
	DepartmentID string `json:"department_id"`
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
