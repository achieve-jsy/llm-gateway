package dto

type VerifyKeyRequest struct {
	Key string `json:"key" binding:"required"`
}

type VerifyKeyResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Data      struct {
		Status string `json:"status"`
		KeyID  string `json:"key_id,omitempty"`
		User   *struct {
			UserID       string `json:"user_id"`
			Name         string `json:"name"`
			DepartmentID string `json:"department_id,omitempty"`
		} `json:"user,omitempty"`
	} `json:"data"`
}
