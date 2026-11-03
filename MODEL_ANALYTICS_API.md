# Model Analytics API Implementation

## Overview
Implemented two new backend API endpoints for model analytics dashboard, following existing codebase patterns.

## Files Modified

### 1. `/internal/observability/handler/model_analytics.go` (NEW)
Complete implementation of two API handlers with Go structs, request/response types, and raw SQL queries.

### 2. `/internal/controlplane/router/router.go` (MODIFIED)
Registered two new routes under adminGroup with proper RBAC guards.

## API Endpoints

### API 1: GET /api/v1/usage/dashboard/model-hourly
**Purpose**: Hourly token/cost breakdown with daily cumulative totals

**Parameters**:
- `start`, `end`: date range (required, max 90 days)
- `model_name`: optional filter
- `tier_name`: optional filter
- `group_by`: "model" (default) | "tier"

**Response Structure**:
```go
type ModelHourlyResponse struct {
    Start   string                 `json:"start"`
    End     string                 `json:"end"`
    GroupBy string                 `json:"group_by"`
    Hourly  []ModelHourlyPoint     `json:"hourly"`      // hourly buckets
    Daily   []ModelDailyCumulative `json:"daily"`       // cumulative totals
}
```

**Key Features**:
- When `group_by=tier`, uses `budget_tier_name_snapshot` with fallback to "unclassified"
- Hourly data grouped by `date_trunc('hour', ts)` with Asia/Shanghai timezone
- Daily cumulative totals calculated in Go
- Excludes service keys via `COALESCE(NULLIF(key_type, ''), 'personal') != 'service'`

### API 2: GET /api/v1/usage/dashboard/model-users
**Purpose**: User/dept usage analytics with three modes

**Parameters**:
- `start`, `end`: date range (required, max 90 days)
- `model_name`, `tier_name`, `dept_name`: optional filters
- `group_by`: "dept" (default) | "user"
- `period`: "day" (default) | "month"

**Response Structure**:
```go
type ModelUsersResponse struct {
    Start       string                `json:"start"`
    End         string                `json:"end"`
    GroupBy     string                `json:"group_by"`
    Period      string                `json:"period"`
    Summary     ModelUsersSummary     `json:"summary"`
    DeptRows    []ModelDeptSummaryRow `json:"dept_rows,omitempty"`
    UserDayRows []ModelUserDayRow     `json:"user_day_rows,omitempty"`
    MonthRows   []ModelMonthlyRow     `json:"month_rows,omitempty"`
}
```

**Three Modes**:

1. **Dept Summary** (`group_by=dept, period=day`):
   - Aggregates by department
   - Includes `user_count` for per-capita cost calculation
   - `cost_per_user` = total_cost / user_count
   - `cost_pct` calculated in Go

2. **User-Per-Day Breakdown** (`group_by=user, period=day`):
   - Groups by (date, user_id, user_name, dept_name)
   - Each row represents one user's activity on one day
   - `cost_pct` = user_day_cost / grand_total_cost

3. **Monthly Aggregation** (`period=month`):
   - Groups by month + group_key (dept or user)
   - Includes user_count per month
   - `cost_pct` calculated in Go

## Implementation Details

### Query Patterns
- Uses `parseDateRangeFromTo()` with 90-day max validation
- RBAC via `rbac.GetActor(c)` + `policy.UsagePolicy{}.QueryScope(actor)`
- Service key exclusion: `COALESCE(NULLIF(key_type, ''), 'personal') != 'service'`
- Timezone-aware grouping: `ts AT TIME ZONE 'Asia/Shanghai'`

### Cost Percentage Calculation
```go
// Calculate grand total first
var grandTotal struct {
    Cost float64 `gorm:"column:cost"`
}
db.Raw(`SELECT COALESCE(SUM(est_cost_usd), 0) AS cost FROM call_logs WHERE ...`).Scan(&grandTotal)

// Then calculate cost_pct for each row
costPct := 0.0
if grandTotal.Cost > 0 {
    costPct = math.Round(rowCost/grandTotal.Cost*1e4) / 1e2 // percentage with 2 decimals
}
```

### SQL Query Examples

**Hourly Breakdown**:
```sql
SELECT
    to_char(date_trunc('hour', ts) AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD HH24:00') AS hour,
    COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') AS group_key,
    COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
    COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
    COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
    COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
    COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
    COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd,
    COUNT(*) AS call_count
FROM call_logs
WHERE ts >= ? AND ts < ?
    AND COALESCE(NULLIF(key_type, ''), 'personal') != 'service'
    AND (? = '' OR model ILIKE ?)
    AND (? = '' OR COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = ?)
GROUP BY date_trunc('hour', ts), group_key
ORDER BY hour ASC, est_cost_usd DESC
```

**Dept Summary**:
```sql
SELECT
    COALESCE(NULLIF(dept_name, ''), 'unknown') AS dept_name,
    COUNT(DISTINCT COALESCE(user_id, '')) AS user_count,
    COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
    COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
    COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
    COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
    COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd,
    COUNT(*) AS call_count
FROM call_logs
WHERE ...
GROUP BY COALESCE(NULLIF(dept_name, ''), 'unknown')
ORDER BY est_cost_usd DESC
```

## Token Fields
All queries include these token breakdowns from `call_logs`:
- `prompt_tokens`
- `completion_tokens`
- `reasoning_tokens`
- `cached_tokens`
- `cache_write_tokens`

## Verification
- ✅ Code compiles cleanly: `go build ./internal/observability/handler/`
- ✅ Router compiles cleanly: `go build ./internal/controlplane/router/`
- ✅ No vet warnings: `go vet ./internal/observability/handler/`
- ✅ Follows existing patterns from `dashboard_enhanced.go` and `usage_handler.go`

## Routes Registered
```go
adminGroup.GET("/usage/dashboard/model-hourly", guard(rbac.PermUsageViewScoped), usageH2.ModelHourly)
adminGroup.GET("/usage/dashboard/model-users", guard(rbac.PermUsageViewScoped), usageH2.ModelUsers)
```

## Next Steps
1. Run integration tests to verify query performance
2. Add index on `(ts, key_type, model, budget_tier_name_snapshot)` if needed
3. Consider caching for frequently accessed date ranges
4. Document API in Swagger/OpenAPI spec
