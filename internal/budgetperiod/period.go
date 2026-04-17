package budgetperiod

import (
	"errors"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
)

const (
	DefaultTimezone    = "Asia/Shanghai"
	CanonicalWeekStart = "monday"
)

var ErrUnsupportedWeekStart = errors.New("week_start must be monday")

type Config struct {
	Timezone  string `json:"timezone"`
	WeekStart string `json:"week_start"`
}

type Period struct {
	Key     string
	StartAt time.Time
	ResetAt time.Time
}

func ValidateConfig(config Config) error {
	if config.Timezone == "" {
		return errors.New("timezone is required")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	if config.WeekStart != CanonicalWeekStart {
		return ErrUnsupportedWeekStart
	}
	return nil
}

func Resolve(now time.Time, periodType budgetdomain.PeriodType, config Config) (Period, error) {
	if err := ValidateConfig(config); err != nil {
		return Period{}, err
	}
	if !periodType.Valid() {
		return Period{}, fmt.Errorf("invalid period_type %q", periodType)
	}
	location, _ := time.LoadLocation(config.Timezone)
	local := now.In(location)

	switch periodType {
	case budgetdomain.PeriodDaily:
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		return Period{Key: start.Format("2006-01-02"), StartAt: start, ResetAt: start.AddDate(0, 0, 1)}, nil
	case budgetdomain.PeriodWeekly:
		weekdayOffset := (int(local.Weekday()) + 6) % 7
		start := time.Date(local.Year(), local.Month(), local.Day()-weekdayOffset, 0, 0, 0, 0, location)
		year, week := local.ISOWeek()
		return Period{
			Key:     fmt.Sprintf("%04d-W%02d", year, week),
			StartAt: start,
			ResetAt: start.AddDate(0, 0, 7),
		}, nil
	case budgetdomain.PeriodMonthly:
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		return Period{Key: start.Format("2006-01"), StartAt: start, ResetAt: start.AddDate(0, 1, 0)}, nil
	default:
		panic("validated period type is exhaustive")
	}
}
