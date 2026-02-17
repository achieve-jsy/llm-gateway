# Task: Phase A Batch 1 — 新建预聚合表 + dept_id 数据清洗

## Background
LLM Gateway 管理层用量看板升级项目。当前 Dashboard API 直查 call_logs 原始表（百万级数据性能差），需要新建两张预聚合表（model_daily_stats、user_daily_stats）并清洗 call_logs.dept_id 历史脏数据。

## Working Directory
`/home/hgf/llm_gateway-backend-dashboard`（Git worktree on branch `feat/dashboard-backend-upgrade`）

## Task 1: A1 — Create `model_daily_stats` table

### 1a. Migration files
- **Up**: `migrations/054_model_daily_stats.up.sql`
- **Down**: `migrations/054_model_daily_stats.down.sql`

**Up SQL:**
```sql
CREATE TABLE IF NOT EXISTS model_daily_stats (
    stat_date           DATE        NOT NULL,
    model               TEXT        NOT NULL,
    studio_id           TEXT        NOT NULL DEFAULT '',
    req_count           INT         NOT NULL DEFAULT 0,
    success_count       INT         NOT NULL DEFAULT 0,
    user_count          INT         NOT NULL DEFAULT 0,
    dept_count          INT         NOT NULL DEFAULT 0,
    active_days         INT         NOT NULL DEFAULT 0,
    prompt_tokens       BIGINT      NOT NULL DEFAULT 0,
    cached_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    completion_tokens   BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens    BIGINT      NOT NULL DEFAULT 0,
    est_cost_usd        DECIMAL(18,6) NOT NULL DEFAULT 0,
    avg_duration_ms     INT         NOT NULL DEFAULT 0,
    p50_duration_ms     INT         NOT NULL DEFAULT 0,
    p90_duration_ms     INT         NOT NULL DEFAULT 0,
    avg_first_token_ms  INT         NOT NULL DEFAULT 0,
    PRIMARY KEY (stat_date, model, studio_id)
);

CREATE INDEX idx_model_daily_stats_model ON model_daily_stats(model);
CREATE INDEX idx_model_daily_stats_date ON model_daily_stats(stat_date);
```

**Down SQL:**
```sql
DROP TABLE IF EXISTS model_daily_stats;
```

### 1b. Go struct
- **File**: `internal/observability/model/model_daily_stats.go`
- **Package**: `model` (same as `internal/observability/model/usage_daily.go`)

```go
package model

import "time"

// ModelDailyStats 模型维度每日预聚合统计
type ModelDailyStats struct {
	StatDate          time.Time `json:"stat_date" gorm:"type:date;primaryKey"`
	Model             string    `json:"model" gorm:"type:text;primaryKey"`
	StudioID          string    `json:"studio_id" gorm:"type:text;primaryKey;default:''"`
	ReqCount          int       `json:"req_count" gorm:"not null;default:0"`
	SuccessCount      int       `json:"success_count" gorm:"not null;default:0"`
	UserCount         int       `json:"user_count" gorm:"not null;default:0"`
	DeptCount         int       `json:"dept_count" gorm:"not null;default:0"`
	ActiveDays        int       `json:"active_days" gorm:"not null;default:0"`
	PromptTokens      int64     `json:"prompt_tokens" gorm:"not null;default:0"`
	CachedTokens      int64     `json:"cached_tokens" gorm:"not null;default:0"`
	CacheWriteTokens  int64     `json:"cache_write_tokens" gorm:"not null;default:0"`
	CompletionTokens  int64     `json:"completion_tokens" gorm:"not null;default:0"`
	ReasoningTokens   int64     `json:"reasoning_tokens" gorm:"not null;default:0"`
	EstCostUSD        float64   `json:"est_cost_usd" gorm:"type:decimal(18,6);not null;default:0"`
	AvgDurationMs     int       `json:"avg_duration_ms" gorm:"not null;default:0"`
	P50DurationMs     int       `json:"p50_duration_ms" gorm:"not null;default:0"`
	P90DurationMs     int       `json:"p90_duration_ms" gorm:"not null;default:0"`
	AvgFirstTokenMs   int       `json:"avg_first_token_ms" gorm:"not null;default:0"`
}

func (ModelDailyStats) TableName() string {
	return "model_daily_stats"
}
```

---

## Task 2: A2 — Create `user_daily_stats` table

### 2a. Migration files
- **Up**: `migrations/055_user_daily_stats.up.sql`
- **Down**: `migrations/055_user_daily_stats.down.sql`

**Up SQL:**
```sql
CREATE TABLE IF NOT EXISTS user_daily_stats (
    stat_date           DATE        NOT NULL,
    user_id             TEXT        NOT NULL,
    studio_id           TEXT        NOT NULL DEFAULT '',
    dept_id             TEXT        NOT NULL DEFAULT '',
    req_count           INT         NOT NULL DEFAULT 0,
    success_count       INT         NOT NULL DEFAULT 0,
    error_count         INT         NOT NULL DEFAULT 0,
    model_count         INT         NOT NULL DEFAULT 0,
    prompt_tokens       BIGINT      NOT NULL DEFAULT 0,
    cached_tokens       BIGINT      NOT NULL DEFAULT 0,
    completion_tokens   BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens    BIGINT      NOT NULL DEFAULT 0,
    est_cost_usd        DECIMAL(18,6) NOT NULL DEFAULT 0,
    hourly_distribution JSONB       NOT NULL DEFAULT '{}',
    PRIMARY KEY (stat_date, user_id)
);

CREATE INDEX idx_user_daily_stats_user ON user_daily_stats(user_id);
CREATE INDEX idx_user_daily_stats_date ON user_daily_stats(stat_date);
CREATE INDEX idx_user_daily_stats_dept ON user_daily_stats(dept_id);
CREATE INDEX idx_user_daily_stats_studio ON user_daily_stats(studio_id);
```

**Down SQL:**
```sql
DROP TABLE IF EXISTS user_daily_stats;
```

### 2b. Go struct
- **File**: `internal/observability/model/user_daily_stats.go`

```go
package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// HourlyDistribution 小时分布 JSONB
type HourlyDistribution map[string]int

func (h HourlyDistribution) Value() (driver.Value, error) {
	if h == nil {
		return "{}", nil
	}
	b, err := json.Marshal(h)
	return string(b), err
}

func (h *HourlyDistribution) Scan(src interface{}) error {
	if src == nil {
		*h = make(HourlyDistribution)
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return nil
	}
	return json.Unmarshal(b, h)
}

// UserDailyStats 用户维度每日预聚合统计
type UserDailyStats struct {
	StatDate           time.Time          `json:"stat_date" gorm:"type:date;primaryKey"`
	UserID             string             `json:"user_id" gorm:"type:text;primaryKey"`
	StudioID           string             `json:"studio_id" gorm:"type:text;not null;default:''"`
	DeptID             string             `json:"dept_id" gorm:"type:text;not null;default:''"`
	ReqCount           int                `json:"req_count" gorm:"not null;default:0"`
	SuccessCount       int                `json:"success_count" gorm:"not null;default:0"`
	ErrorCount         int                `json:"error_count" gorm:"not null;default:0"`
	ModelCount         int                `json:"model_count" gorm:"not null;default:0"`
	PromptTokens       int64              `json:"prompt_tokens" gorm:"not null;default:0"`
	CachedTokens       int64              `json:"cached_tokens" gorm:"not null;default:0"`
	CompletionTokens   int64              `json:"completion_tokens" gorm:"not null;default:0"`
	ReasoningTokens    int64              `json:"reasoning_tokens" gorm:"not null;default:0"`
	EstCostUSD         float64            `json:"est_cost_usd" gorm:"type:decimal(18,6);not null;default:0"`
	HourlyDistribution HourlyDistribution `json:"hourly_distribution" gorm:"type:jsonb;not null;default:'{}'"`
}

func (UserDailyStats) TableName() string {
	return "user_daily_stats"
}
```

---

## Task 3: A5 — call_logs.dept_id Data Cleaning Migration

### 3a. Migration files
- **Up**: `migrations/056_clean_dept_id.up.sql`
- **Down**: `migrations/056_clean_dept_id.down.sql`

**Up SQL** — clean `dept:xxx` prefix format to pure UUID:
```sql
-- Step 1: Clean call_logs.dept_id — remove "dept:" prefix
UPDATE call_logs
SET dept_id = SUBSTRING(dept_id FROM 6)
WHERE dept_id LIKE 'dept:%';

-- Step 2: Clean usage_daily.dept_id — same format issue
UPDATE usage_daily
SET dept_id = SUBSTRING(dept_id FROM 6)
WHERE dept_id LIKE 'dept:%';
```

**Down SQL** — cannot perfectly reverse (we don't know which were originally prefixed), so just document:
```sql
-- NOTE: This migration is NOT perfectly reversible.
-- The "dept:" prefix was a legacy format. Once cleaned, we cannot
-- distinguish which records originally had the prefix.
-- For rollback, re-run with prefix on known-affected records only.
-- Affected count can be checked before running UP:
--   SELECT COUNT(*) FROM call_logs WHERE dept_id LIKE 'dept:%';
```

---

## Verification
After all changes:
1. `go build ./...` must pass with zero errors
2. `go vet ./...` must pass
3. All 6 new files exist (4 migrations + 2 Go structs)
4. `git diff --stat` should show exactly these new files

## Constraints
- Do NOT modify any existing files except adding imports if needed
- Do NOT modify `usage_daily.go` or any handler/service files
- Do NOT run `go mod tidy` (no new dependencies needed)
- Migration numbering: 054, 055, 056 (current max is 053)
- All Go files go in `internal/observability/model/` package
