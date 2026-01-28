package keyactivity

import "time"

const (
	EventTypeNewSource = "new_source"
	EventTypeFirstUse  = "first_use"
	EventTypeWakeup    = "wakeup"
	EventTypeSpike     = "usage_spike"
)

// KeySourceSeen 已见来源表（E1/E2）
type KeySourceSeen struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	KeyID     string    `gorm:"column:key_id;not null;index:idx_kss_key;uniqueIndex:uniq_key_source,priority:1" json:"key_id"`
	SourceFP  string    `gorm:"column:source_fp;not null;uniqueIndex:uniq_key_source,priority:2" json:"source_fp"`
	UAFp      string    `gorm:"column:ua_fp;not null" json:"ua_fp"`
	UARaw     string    `gorm:"column:ua_raw;not null;default:''" json:"ua_raw"`
	IP        string    `gorm:"column:ip;not null;default:''" json:"ip"`
	FirstSeen time.Time `gorm:"column:first_seen;not null" json:"first_seen"`
	LastSeen  time.Time `gorm:"column:last_seen;not null" json:"last_seen"`
	HitCount  int64     `gorm:"column:hit_count;not null;default:1" json:"hit_count"`
}

func (KeySourceSeen) TableName() string { return "key_source_seen" }

// KeyActivityState 每 Key 状态（EWMA 基线 + E3 唤醒记录）
type KeyActivityState struct {
	KeyID         string     `gorm:"column:key_id;primaryKey" json:"key_id"`
	EwmaMean      float64    `gorm:"column:ewma_mean;not null;default:0" json:"ewma_mean"`
	EwmaDev       float64    `gorm:"column:ewma_dev;not null;default:0" json:"ewma_dev"`
	LastSeenAt    *time.Time `gorm:"column:last_seen_at" json:"last_seen_at,omitempty"`
	LastSpikeHour *time.Time `gorm:"column:last_spike_hour" json:"last_spike_hour,omitempty"`
	Samples       int64      `gorm:"column:samples;not null;default:0" json:"samples"`
	BaselineReady bool       `gorm:"column:baseline_ready;not null;default:false" json:"baseline_ready"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (KeyActivityState) TableName() string { return "key_activity_state" }

// KeyActivityAlert 提醒记录 + 去重（唯一键 key_id+event_type+dedup_key）
type KeyActivityAlert struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	KeyID      string    `gorm:"column:key_id;not null;uniqueIndex:uniq_activity_alert,priority:1" json:"key_id"`
	UserID     string    `gorm:"column:user_id;not null" json:"user_id"`
	EventType  string    `gorm:"column:event_type;not null;uniqueIndex:uniq_activity_alert,priority:2" json:"event_type"`
	DedupKey   string    `gorm:"column:dedup_key;not null;uniqueIndex:uniq_activity_alert,priority:3" json:"dedup_key"`
	UARaw      string    `gorm:"column:ua_raw;not null;default:''" json:"ua_raw"`
	IP         string    `gorm:"column:ip;not null;default:''" json:"ip"`
	Detail     string    `gorm:"column:detail;type:jsonb" json:"detail,omitempty"`
	NotifiedAt time.Time `gorm:"column:notified_at;not null" json:"notified_at"`
}

func (KeyActivityAlert) TableName() string { return "key_activity_alert" }

// CallLogRow call_logs 增量扫描行
type CallLogRow struct {
	RequestID string
	TS        time.Time
	KeyID     string
	KeyName   string
	UserID    string
	UserAgent string
	IPAddress string
}

// Cursor 扫描游标（复合水位，防同秒漏扫）
type Cursor struct {
	TS        time.Time `json:"ts"`
	RequestID string    `json:"request_id"`
}
