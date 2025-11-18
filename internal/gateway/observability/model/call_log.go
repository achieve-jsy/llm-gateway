// Package model re-exports shared observability data models.
package model

import obsmodel "github.com/example/llm-gateway/internal/observability/model"

// CallLog is the shared call log model
type CallLog = obsmodel.CallLog