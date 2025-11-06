package dto

type OIDCCallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

type UserInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	DepartmentID   string `json:"departmentId,omitempty"`
	DepartmentName string `json:"departmentName,omitempty"`
}

type LoginResponse struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	User         UserInfo `json:"user"`
	Role         string   `json:"role"`
}
