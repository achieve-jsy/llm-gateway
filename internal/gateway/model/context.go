package model

import "time"

// RequestContext 请求运行时上下文
type RequestContext struct {
	RequestID string
	APIKey    string
	Model     string
	Provider  string
	Stream    bool
	StartTime time.Time
}
