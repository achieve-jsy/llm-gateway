package dto

type VerifyKeyRequest struct {
	Key string `json:"key" binding:"required"`
}

type VerifyKeyResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Data      struct {
		Status          string `json:"status"`
		KeyID           string `json:"key_id,omitempty"`
		KeyName         string `json:"key_name,omitempty"`
		AccessAllGroups bool   `json:"access_all_groups"`
		LogEnabled      *bool  `json:"log_enabled,omitempty"` // nil = 旧缓存无此字段，默认 true
		User            *struct {
			UserID       string `json:"user_id"`
			Name         string `json:"name"`
			DepartmentID string `json:"department_id,omitempty"`
			DeptName     string `json:"dept_name,omitempty"`
			StudioID     string `json:"studio_id,omitempty"`
			StudioName   string `json:"studio_name,omitempty"`
		} `json:"user,omitempty"`
	} `json:"data"`
}
