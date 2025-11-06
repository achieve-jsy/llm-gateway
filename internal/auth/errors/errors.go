package errors

var (
	ErrKeyNotFound  = &AuthError{Code: "E4015", Message: "key not found", Status: 401}
	ErrKeyDisabled  = &AuthError{Code: "E4016", Message: "key disabled", Status: 401}
	ErrKeyInvalid   = &AuthError{Code: "E4017", Message: "invalid key format", Status: 401}
	ErrUserNotFound = &AuthError{Code: "E4043", Message: "user not found", Status: 404}
	ErrUpstreamDown = &AuthError{Code: "E5031", Message: "upstream unavailable", Status: 503}
)

type AuthError struct {
	Code    string
	Message string
	Status  int
}

func (e *AuthError) Error() string { return e.Message }
