package budgetdomain

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrAmountMustBeString = errors.New("amount must be a decimal string")
	ErrAmountNegative     = errors.New("amount must be greater than or equal to zero")
	ErrTierIDRequired     = errors.New("model_tier_id is required for tier dimension")
	ErrTierIDForbidden    = errors.New("model_tier_id must be null for total dimension")
)

type Amount struct {
	decimal.Decimal
}

func ParseAmount(raw string) (Amount, error) {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return Amount{}, ErrAmountMustBeString
	}
	if value.IsNegative() {
		return Amount{}, ErrAmountNegative
	}
	return Amount{Decimal: value.Round(6)}, nil
}

func (a Amount) String() string {
	return a.Decimal.StringFixed(6)
}

func (a Amount) Value() (driver.Value, error) {
	return a.String(), nil
}

func (a *Amount) Scan(value any) error {
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		raw = fmt.Sprint(typed)
	}
	parsed, err := ParseAmount(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

func (a Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || data[0] != '"' || bytes.Equal(data, []byte("null")) {
		return ErrAmountMustBeString
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return ErrAmountMustBeString
	}
	parsed, err := ParseAmount(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

type PeriodType string

const (
	PeriodDaily   PeriodType = "daily"
	PeriodWeekly  PeriodType = "weekly"
	PeriodMonthly PeriodType = "monthly"
)

func (p PeriodType) Valid() bool {
	switch p {
	case PeriodDaily, PeriodWeekly, PeriodMonthly:
		return true
	default:
		return false
	}
}

type DimensionType string

const (
	DimensionTotal DimensionType = "total"
	DimensionTier  DimensionType = "tier"
)

func (d DimensionType) Valid() bool {
	return d == DimensionTotal || d == DimensionTier
}

func ValidateDimension(d DimensionType, tierID *uuid.UUID) error {
	switch d {
	case DimensionTotal:
		if tierID != nil {
			return ErrTierIDForbidden
		}
	case DimensionTier:
		if tierID == nil || *tierID == uuid.Nil {
			return ErrTierIDRequired
		}
	default:
		return fmt.Errorf("invalid dimension_type %q", d)
	}
	return nil
}

func DimensionKey(d DimensionType, tierID *uuid.UUID) (string, error) {
	if err := ValidateDimension(d, tierID); err != nil {
		return "", err
	}
	if d == DimensionTotal {
		return "total", nil
	}
	return "tier:" + tierID.String(), nil
}

type DurationType string

const (
	DurationPermanent     DurationType = "permanent"
	DurationCurrentPeriod DurationType = "current_period"
)

func (d DurationType) Valid() bool {
	return d == DurationPermanent || d == DurationCurrentPeriod
}

type OverrideStatus string

const (
	OverrideActive    OverrideStatus = "active"
	OverrideExpired   OverrideStatus = "expired"
	OverrideCancelled OverrideStatus = "cancelled"
	OverrideFailed    OverrideStatus = "failed"
)

func (s OverrideStatus) Valid() bool {
	switch s {
	case OverrideActive, OverrideExpired, OverrideCancelled, OverrideFailed:
		return true
	default:
		return false
	}
}

type ScheduleStatus string

const (
	SchedulePending   ScheduleStatus = "pending"
	ScheduleApplied   ScheduleStatus = "applied"
	ScheduleReplaced  ScheduleStatus = "replaced"
	ScheduleCancelled ScheduleStatus = "cancelled"
	ScheduleFailed    ScheduleStatus = "failed"
)

func (s ScheduleStatus) Valid() bool {
	switch s {
	case SchedulePending, ScheduleApplied, ScheduleReplaced, ScheduleCancelled, ScheduleFailed:
		return true
	default:
		return false
	}
}
