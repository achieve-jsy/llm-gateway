package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChannelMonitor 渠道监控配置
type ChannelMonitor struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string         `gorm:"type:varchar(200);not null" json:"name"`
	Provider         string         `gorm:"type:varchar(50);not null" json:"provider"`
	APIMode          string         `gorm:"type:varchar(50);not null;default:chat_completions" json:"api_mode"`
	Endpoint         string         `gorm:"type:text;not null" json:"endpoint"`
	APIKeyEncrypted  string         `gorm:"type:text;not null" json:"-"`
	PrimaryModel     string         `gorm:"type:varchar(200);not null" json:"primary_model"`
	ExtraModels      StringArray    `gorm:"type:text[];not null;default:'{}'" json:"extra_models"`
	GroupName        string         `gorm:"type:varchar(200);not null;default:''" json:"group_name"`
	Enabled          bool           `gorm:"not null;default:true" json:"enabled"`
	IntervalSeconds  int            `gorm:"not null;default:60" json:"interval_seconds"`
	LastCheckedAt    *time.Time     `json:"last_checked_at"`
	CreatedBy        string         `gorm:"type:varchar(100);not null;default:''" json:"created_by"`
	ExtraHeaders     JSONMap        `gorm:"type:jsonb;not null;default:'{}'" json:"extra_headers"`
	BodyOverrideMode string         `gorm:"type:varchar(20);not null;default:off" json:"body_override_mode"`
	BodyOverride     JSONRaw        `gorm:"type:jsonb" json:"body_override,omitempty"`
	TemplateID       *int64         `json:"template_id,omitempty"`
	CreatedAt        time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"not null;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChannelMonitor) TableName() string { return "channel_monitors" }

// ChannelMonitorHistory 渠道监控检测历史
type ChannelMonitorHistory struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MonitorID     int64     `gorm:"not null;index:idx_channel_monitor_histories_lookup" json:"monitor_id"`
	Model         string    `gorm:"type:varchar(200);not null;index:idx_channel_monitor_histories_lookup" json:"model"`
	Status        string    `gorm:"type:varchar(20);not null" json:"status"`
	LatencyMs     *int      `json:"latency_ms"`
	PingLatencyMs *int      `json:"ping_latency_ms"`
	Message       string    `gorm:"type:varchar(500);not null;default:''" json:"message"`
	CheckedAt     time.Time `gorm:"not null;index:idx_channel_monitor_histories_lookup,priority:3,sort:desc;index" json:"checked_at"`
}

func (ChannelMonitorHistory) TableName() string { return "channel_monitor_histories" }

// ChannelMonitorDailyRollup 渠道监控日聚合
type ChannelMonitorDailyRollup struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	MonitorID        int64          `gorm:"not null;uniqueIndex:idx_channel_monitor_daily_rollups_uniq" json:"monitor_id"`
	Model            string         `gorm:"type:varchar(200);not null;uniqueIndex:idx_channel_monitor_daily_rollups_uniq" json:"model"`
	BucketDate       string         `gorm:"type:date;not null;uniqueIndex:idx_channel_monitor_daily_rollups_uniq" json:"bucket_date"`
	TotalChecks      int            `gorm:"not null;default:0" json:"total_checks"`
	OkCount          int            `gorm:"not null;default:0" json:"ok_count"`
	OperationalCount int            `gorm:"not null;default:0" json:"operational_count"`
	DegradedCount    int            `gorm:"not null;default:0" json:"degraded_count"`
	FailedCount      int            `gorm:"not null;default:0" json:"failed_count"`
	ErrorCount       int            `gorm:"not null;default:0" json:"error_count"`
	SumLatencyMs     int64          `gorm:"not null;default:0" json:"sum_latency_ms"`
	CountLatency     int            `gorm:"not null;default:0" json:"count_latency"`
	SumPingLatencyMs int64          `gorm:"not null;default:0" json:"sum_ping_latency_ms"`
	CountPingLatency int            `gorm:"not null;default:0" json:"count_ping_latency"`
	ComputedAt       time.Time      `gorm:"not null;autoCreateTime" json:"computed_at"`
	CreatedAt        time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChannelMonitorDailyRollup) TableName() string { return "channel_monitor_daily_rollups" }

// ChannelMonitorRequestTemplate 渠道监控请求模板
type ChannelMonitorRequestTemplate struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string         `gorm:"type:varchar(200);not null" json:"name"`
	Provider         string         `gorm:"type:varchar(50);not null" json:"provider"`
	APIMode          string         `gorm:"type:varchar(50);not null;default:chat_completions" json:"api_mode"`
	Description      string         `gorm:"type:text;not null;default:''" json:"description"`
	ExtraHeaders     JSONMap        `gorm:"type:jsonb;not null;default:'{}'" json:"extra_headers"`
	BodyOverrideMode string         `gorm:"type:varchar(20);not null;default:off" json:"body_override_mode"`
	BodyOverride     JSONRaw        `gorm:"type:jsonb" json:"body_override,omitempty"`
	CreatedAt        time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"not null;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChannelMonitorRequestTemplate) TableName() string { return "channel_monitor_request_templates" }

// ChannelMonitorAggregationWatermark 聚合水位标记（单行，id=1）
type ChannelMonitorAggregationWatermark struct {
	ID                 int       `gorm:"primaryKey;autoIncrement:false;default:1" json:"id"`
	LastAggregatedDate string    `gorm:"type:date;not null" json:"last_aggregated_date"`
	UpdatedAt          time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ChannelMonitorAggregationWatermark) TableName() string {
	return "channel_monitor_aggregation_watermark"
}

// ── Custom GORM types ──

// StringArray is a text[] column type for GORM.
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return fmt.Sprintf("{%s}", joinQuoted(a)), nil
}

func (a *StringArray) Scan(src interface{}) error {
	if src == nil {
		*a = StringArray{}
		return nil
	}
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into StringArray", src)
	}
	if s == "" || s == "{}" {
		*a = StringArray{}
		return nil
	}
	// PostgreSQL text[] format: {elem1,elem2,...}
	s = s[1 : len(s)-1] // strip { }
	if s == "" {
		*a = StringArray{}
		return nil
	}
	parts := splitArray(s)
	*a = StringArray(parts)
	return nil
}

func joinQuoted(ss []string) string {
	out := make([]byte, 0)
	for i, s := range ss {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = append(out, s...)
		out = append(out, '"')
	}
	return string(out)
}

func splitArray(s string) []string {
	parts := make([]string, 0)
	current := ""
	inQuote := false
	for _, c := range s {
		switch c {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				parts = append(parts, current)
				current = ""
			} else {
				current += string(c)
			}
		default:
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// JSONMap is a map[string]string for JSONB columns.
type JSONMap map[string]string

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (m *JSONMap) Scan(src interface{}) error {
	if src == nil {
		*m = JSONMap{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONMap", src)
	}
	if len(b) == 0 || string(b) == "null" {
		*m = JSONMap{}
		return nil
	}
	return json.Unmarshal(b, m)
}

// JSONRaw is a raw JSON type for GORM (stores arbitrary JSONB).
type JSONRaw map[string]interface{}

func (r JSONRaw) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (r *JSONRaw) Scan(src interface{}) error {
	if src == nil {
		*r = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONRaw", src)
	}
	if len(b) == 0 || string(b) == "null" {
		*r = nil
		return nil
	}
	return json.Unmarshal(b, r)
}
