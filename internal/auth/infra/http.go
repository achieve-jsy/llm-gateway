package infra

import (
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/auth/config"
)

func NewHTTPClient(cfg config.HTTPConfig) (*http.Client, error) {
	return &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}, nil
}
