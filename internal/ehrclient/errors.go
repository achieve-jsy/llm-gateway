package ehrclient

import "fmt"

type ErrHTTP struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *ErrHTTP) Error() string {
	return fmt.Sprintf("ehr api %s %s => %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

func (e *ErrHTTP) IsAuthError() bool    { return e.StatusCode == 401 }
func (e *ErrHTTP) IsForbidden() bool    { return e.StatusCode == 403 }
func (e *ErrHTTP) IsRateLimited() bool  { return e.StatusCode == 429 }
func (e *ErrHTTP) IsServerError() bool  { return e.StatusCode >= 500 && e.StatusCode < 600 }
func (e *ErrHTTP) IsRetryable() bool    { return e.IsAuthError() || e.IsServerError() }

type ErrConfig struct{}

func (e *ErrConfig) Error() string { return "ehr api token not configured" }
