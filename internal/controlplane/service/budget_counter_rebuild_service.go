package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/budgetrebuild"
	"github.com/example/llm-gateway/internal/calllogstream"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	rebuildLeaseTTL       = 10 * time.Second
	rebuildWaitInterval   = 10 * time.Millisecond
	rebuildPageSize       = 200
	rebuildSwitchBatch    = 100
	rebuildClaimMinIdle   = 30 * time.Second
	rebuildTempExpiration = time.Hour
	maxReportedDiffs      = 1000
)

type CounterDiff struct {
	Key                 string  `json:"key"`
	ExpectedMicroUSD    int64   `json:"expected_micro_usd"`
	ActualMicroUSD      int64   `json:"actual_micro_usd"`
	AbsDiffMicroUSD     int64   `json:"abs_diff_micro_usd"`
	DiffRatio           float64 `json:"diff_ratio"`
	AllowedDiffMicroUSD int64   `json:"allowed_diff_micro_usd"`
	WithinThreshold     bool    `json:"within_threshold"`
}

type RebuildV2Result struct {
	KeysWritten             int           `json:"keys_written"`
	SourceRows              int           `json:"source_rows"`
	Diffs                   []CounterDiff `json:"diffs"`
	LegacyV2Diffs           []CounterDiff `json:"legacy_v2_diffs"`
	SwitchBatches           int           `json:"switch_batches"`
	AllocationPages         int           `json:"allocation_pages"`
	CallLogPages            int           `json:"call_log_pages"`
	DiffsTruncated          bool          `json:"diffs_truncated"`
	AllDiffsWithinThreshold bool          `json:"all_diffs_within_threshold"`
	PreviousGeneration      string        `json:"previous_generation"`
	ActiveGeneration        string        `json:"active_generation"`
	CompletedAt             time.Time     `json:"completed_at"`
}

func (r *RebuildV2Result) CutoverReady() bool {
	if r == nil || r.SourceRows == 0 || r.KeysWritten == 0 || !r.AllDiffsWithinThreshold {
		return false
	}
	for _, diff := range append(r.Diffs, r.LegacyV2Diffs...) {
		if !diff.WithinThreshold {
			return false
		}
	}
	return true
}

type BudgetCounterRebuildService struct {
	db           *gorm.DB
	rdb          *redis.Client
	periodConfig budgetperiod.Config
	cfg          BudgetCounterRebuildConfig
}

type BudgetCounterRebuildConfig struct {
	LeaseTTL       time.Duration
	WaitInterval   time.Duration
	BeforeActivate func(string)
}

func NewBudgetCounterRebuildService(
	db *gorm.DB,
	rdb *redis.Client,
	periodConfig budgetperiod.Config,
) *BudgetCounterRebuildService {
	return NewBudgetCounterRebuildServiceWithConfig(db, rdb, periodConfig, BudgetCounterRebuildConfig{})
}

func NewBudgetCounterRebuildServiceWithConfig(
	db *gorm.DB,
	rdb *redis.Client,
	periodConfig budgetperiod.Config,
	cfg BudgetCounterRebuildConfig,
) *BudgetCounterRebuildService {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = rebuildLeaseTTL
	}
	if cfg.WaitInterval <= 0 {
		cfg.WaitInterval = rebuildWaitInterval
	}
	return &BudgetCounterRebuildService{db: db, rdb: rdb, periodConfig: periodConfig, cfg: cfg}
}

type rebuildLease struct {
	rdb    *redis.Client
	token  string
	ttl    time.Duration
	lost   atomic.Bool
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

var renewLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('PEXPIRE', KEYS[1], ARGV[2])
`)

var releaseLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('DEL', KEYS[1])
`)

func acquireRebuildLease(ctx context.Context, rdb *redis.Client, ttl time.Duration) (*rebuildLease, error) {
	token := uuid.NewString()
	acquired, err := rdb.SetNX(ctx, budgetrebuild.LockKey, token, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire budget rebuild lease: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("another budget counter rebuild is active")
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	lease := &rebuildLease{
		rdb: rdb, token: token, ttl: ttl,
		cancel: cancel, done: make(chan struct{}),
	}
	go lease.renewLoop(leaseCtx)
	return lease, nil
}

func (l *rebuildLease) renewLoop(ctx context.Context) {
	defer close(l.done)
	ticker := time.NewTicker(l.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := renewLeaseScript.Run(ctx, l.rdb,
				[]string{budgetrebuild.LockKey}, l.token, l.ttl.Milliseconds(),
			).Int()
			if err != nil || ok != 1 {
				l.lost.Store(true)
				return
			}
		}
	}
}

func (l *rebuildLease) check(ctx context.Context) error {
	if l == nil || l.lost.Load() {
		return errors.New("budget rebuild lease lost")
	}
	value, err := l.rdb.Get(ctx, budgetrebuild.LockKey).Result()
	if err != nil || value != l.token {
		l.lost.Store(true)
		return errors.New("budget rebuild lease lost")
	}
	return nil
}

func (l *rebuildLease) close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.cancel()
		<-l.done
		_ = releaseLeaseScript.Run(context.Background(), l.rdb,
			[]string{budgetrebuild.LockKey}, l.token,
		).Err()
	})
}

type allocationFact struct {
	ID        string `json:"id"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	Monthly   bool   `json:"monthly"`
	Weekly    bool   `json:"weekly"`
	Daily     bool   `json:"daily"`
}

type counterMeta struct {
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	ResetAt    time.Time               `json:"reset_at"`
}

type rebuildRun struct {
	id              string
	generation      string
	valuesKey       string
	metaKey         string
	allocByScopeKey string
	allocByIDKey    string
}

func newRebuildRun() rebuildRun {
	id := uuid.NewString()
	prefix := "budget:v2:rebuild:run:" + id
	return rebuildRun{
		id: id, generation: "g-" + id, valuesKey: prefix + ":values", metaKey: prefix + ":meta",
		allocByScopeKey: prefix + ":alloc_scope", allocByIDKey: prefix + ":alloc_id",
	}
}

func (r rebuildRun) keys() []string {
	return []string{r.valuesKey, r.metaKey, r.allocByScopeKey, r.allocByIDKey}
}

func (s *BudgetCounterRebuildService) RebuildCurrentPeriods(
	ctx context.Context,
	now time.Time,
) (*RebuildV2Result, error) {
	if s.db == nil || s.rdb == nil {
		return nil, fmt.Errorf("budget counter rebuild dependencies are not initialized")
	}
	lease, err := acquireRebuildLease(ctx, s.rdb, s.cfg.LeaseTTL)
	if err != nil {
		return nil, err
	}
	defer lease.close()
	if err := s.waitForInflight(ctx, lease); err != nil {
		return nil, err
	}
	if err := s.drainCallLogSpool(ctx, lease); err != nil {
		return nil, err
	}
	if err := lease.check(ctx); err != nil {
		return nil, err
	}
	windows, earliestStart, err := s.currentWindows(now)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now()
	run := newRebuildRun()
	defer s.rdb.Del(context.Background(), run.keys()...)
	previousGeneration, err := s.rdb.Get(ctx, budgetrebuild.ActiveGenerationKey).Result()
	if err == redis.Nil {
		previousGeneration = budgetrebuild.LegacyGeneration
	} else if err != nil {
		return nil, fmt.Errorf("read active budget generation: %w", err)
	}
	switched := false
	defer func() {
		if !switched {
			_ = s.cleanupGeneration(context.Background(), run)
		}
	}()
	result := &RebuildV2Result{
		Diffs:                   make([]CounterDiff, 0),
		LegacyV2Diffs:           make([]CounterDiff, 0),
		AllDiffsWithinThreshold: true,
		PreviousGeneration:      previousGeneration,
		ActiveGeneration:        run.generation,
	}
	if err := s.buildAllocationFacts(ctx, lease, run, windows, result); err != nil {
		return nil, err
	}
	if err := s.buildTierLimitFacts(ctx, lease, run, windows); err != nil {
		return nil, err
	}
	if err := s.addExistingTierKeys(ctx, lease, run, windows, previousGeneration); err != nil {
		return nil, err
	}
	sourceRows, err := s.aggregateCallLogs(ctx, lease, run, windows, earliestStart, cutoff, result)
	if err != nil {
		return nil, err
	}
	result.SourceRows = sourceRows
	if err := s.validateAndCollectDiffs(ctx, lease, run, result, previousGeneration); err != nil {
		return nil, err
	}
	if err := s.buildGeneration(ctx, lease, run, result, now); err != nil {
		return nil, err
	}
	if s.cfg.BeforeActivate != nil {
		s.cfg.BeforeActivate(run.generation)
	}
	if err := s.activateGeneration(ctx, lease, previousGeneration, run.generation); err != nil {
		return nil, err
	}
	switched = true
	result.CompletedAt = time.Now()
	return result, nil
}

func (s *BudgetCounterRebuildService) currentWindows(
	now time.Time,
) (map[budgetdomain.PeriodType]budgetperiod.Period, time.Time, error) {
	windows := make(map[budgetdomain.PeriodType]budgetperiod.Period, 3)
	var earliest time.Time
	for _, periodType := range []budgetdomain.PeriodType{
		budgetdomain.PeriodDaily, budgetdomain.PeriodWeekly, budgetdomain.PeriodMonthly,
	} {
		window, err := budgetperiod.Resolve(now, periodType, s.periodConfig)
		if err != nil {
			return nil, time.Time{}, err
		}
		windows[periodType] = window
		if earliest.IsZero() || window.StartAt.Before(earliest) {
			earliest = window.StartAt
		}
	}
	return windows, earliest, nil
}

func (s *BudgetCounterRebuildService) waitForInflight(ctx context.Context, lease *rebuildLease) error {
	ticker := time.NewTicker(s.cfg.WaitInterval)
	defer ticker.Stop()
	for {
		if lease != nil {
			if err := lease.check(ctx); err != nil {
				return err
			}
		}
		now := time.Now().UnixMilli()
		if err := s.rdb.ZRemRangeByScore(ctx, budgetrebuild.BillableInflightKey, "-inf", strconv.FormatInt(now, 10)).Err(); err != nil {
			return fmt.Errorf("clean expired billable reservations: %w", err)
		}
		count, err := s.rdb.ZCard(ctx, budgetrebuild.BillableInflightKey).Result()
		if err != nil {
			return fmt.Errorf("count billable inflight requests: %w", err)
		}
		if count == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
		}
	}
}

func (s *BudgetCounterRebuildService) drainCallLogSpool(ctx context.Context, lease *rebuildLease) error {
	group := calllogstream.ConsumerGroup
	err := s.rdb.XGroupCreateMkStream(ctx, calllogstream.Spool, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create rebuild spool consumer group: %w", err)
	}
	consumer := "budget-rebuild-" + lease.token
	for {
		if err := lease.check(ctx); err != nil {
			return err
		}
		messages, _, err := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: calllogstream.Spool, Group: group, Consumer: consumer,
			MinIdle: rebuildClaimMinIdle, Start: "0-0", Count: rebuildSwitchBatch,
		}).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("claim pending call logs for rebuild: %w", err)
		}
		if len(messages) == 0 {
			streams, readErr := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group: group, Consumer: consumer,
				Streams: []string{calllogstream.Spool, ">"},
				Count:   rebuildSwitchBatch, Block: -1,
			}).Result()
			if readErr != nil && readErr != redis.Nil {
				return fmt.Errorf("read call logs for rebuild drain: %w", readErr)
			}
			for _, stream := range streams {
				messages = append(messages, stream.Messages...)
			}
		}
		if len(messages) > 0 {
			if err := s.persistDrainBatch(ctx, group, messages); err != nil {
				return err
			}
			continue
		}
		pending, err := s.rdb.XPending(ctx, calllogstream.Spool, group).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("inspect rebuild spool pending: %w", err)
		}
		length, err := s.rdb.XLen(ctx, calllogstream.Spool).Result()
		if err != nil {
			return fmt.Errorf("inspect rebuild spool length: %w", err)
		}
		if length == 0 && (pending == nil || pending.Count == 0) {
			groups, infoErr := s.rdb.XInfoGroups(ctx, calllogstream.Spool).Result()
			if infoErr != nil {
				return fmt.Errorf("inspect rebuild spool group lag: %w", infoErr)
			}
			for _, info := range groups {
				if info.Name == group && info.Pending == 0 && info.Lag == 0 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(s.cfg.WaitInterval):
		}
	}
}

func (s *BudgetCounterRebuildService) persistDrainBatch(
	ctx context.Context,
	group string,
	messages []redis.XMessage,
) error {
	logs := make([]obsmodel.CallLog, 0, len(messages))
	validIDs := make([]string, 0, len(messages))
	poison := make([]redis.XMessage, 0)
	for _, message := range messages {
		raw, ok := message.Values["payload"]
		var log obsmodel.CallLog
		if !ok || json.Unmarshal([]byte(fmt.Sprint(raw)), &log) != nil || log.RequestID == "" {
			poison = append(poison, message)
			continue
		}
		logs = append(logs, log)
		validIDs = append(validIDs, message.ID)
	}
	if len(logs) > 0 {
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(logs, rebuildSwitchBatch).Error; err != nil {
			return fmt.Errorf("persist drained call logs: %w", err)
		}
	}
	pipe := s.rdb.TxPipeline()
	if len(validIDs) > 0 {
		pipe.XAck(ctx, calllogstream.Spool, group, validIDs...)
		pipe.XDel(ctx, calllogstream.Spool, validIDs...)
	}
	for _, message := range poison {
		values := make(map[string]any, len(message.Values)+3)
		for key, value := range message.Values {
			values[key] = value
		}
		values["source_id"] = message.ID
		values["error"] = "invalid call-log payload during rebuild drain"
		values["retries"] = 1
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: calllogstream.DLQ, Values: values})
		pipe.XAck(ctx, calllogstream.Spool, group, message.ID)
		pipe.XDel(ctx, calllogstream.Spool, message.ID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ack drained call logs: %w", err)
	}
	return nil
}

func (s *BudgetCounterRebuildService) buildAllocationFacts(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	windows map[budgetdomain.PeriodType]budgetperiod.Period,
	result *RebuildV2Result,
) error {
	cursor := ""
	for {
		if err := lease.check(ctx); err != nil {
			return err
		}
		var page []cpmodel.BudgetAllocation
		query := s.db.WithContext(ctx).Where("enabled = ?", true)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Order("id").Limit(rebuildPageSize).Find(&page).Error; err != nil {
			return fmt.Errorf("page budget allocations for rebuild: %w", err)
		}
		if len(page) == 0 {
			return nil
		}
		result.AllocationPages++
		pipe := s.rdb.TxPipeline()
		for _, allocation := range page {
			fact := allocationFact{
				ID: allocation.ID.String(), ScopeType: allocation.ScopeType, ScopeID: allocation.ScopeID,
				Monthly: allocation.AmountUSD >= 0,
				Weekly:  allocation.WeeklyAmountUSD >= 0,
				Daily:   allocation.DailyAmountUSD >= 0,
			}
			raw, _ := json.Marshal(fact)
			pipe.HSet(ctx, run.allocByScopeKey, fact.ScopeType+":"+fact.ScopeID, raw)
			pipe.HSet(ctx, run.allocByIDKey, fact.ID, raw)
			for periodType, window := range windows {
				if !fact.hasPeriod(periodType) {
					continue
				}
				key := fmt.Sprintf("budget:v2:used:%s:%s:%s", fact.ScopeType, fact.ScopeID, window.Key)
				setCounterInitial(ctx, pipe, run, key, periodType, window.ResetAt)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("store allocation rebuild page: %w", err)
		}
		cursor = page[len(page)-1].ID.String()
		if len(page) < rebuildPageSize {
			return nil
		}
	}
}

func (f allocationFact) hasPeriod(periodType budgetdomain.PeriodType) bool {
	switch periodType {
	case budgetdomain.PeriodDaily:
		return f.Daily
	case budgetdomain.PeriodWeekly:
		return f.Weekly
	case budgetdomain.PeriodMonthly:
		return f.Monthly
	default:
		return false
	}
}

func setCounterInitial(
	ctx context.Context,
	pipe redis.Pipeliner,
	run rebuildRun,
	key string,
	periodType budgetdomain.PeriodType,
	resetAt time.Time,
) {
	meta, _ := json.Marshal(counterMeta{PeriodType: periodType, ResetAt: resetAt})
	pipe.HSetNX(ctx, run.valuesKey, key, "0")
	pipe.HSetNX(ctx, run.metaKey, key, meta)
}

func (s *BudgetCounterRebuildService) buildTierLimitFacts(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	windows map[budgetdomain.PeriodType]budgetperiod.Period,
) error {
	cursor := ""
	for {
		if err := lease.check(ctx); err != nil {
			return err
		}
		var page []cpmodel.BudgetTierLimit
		query := s.db.WithContext(ctx).Where("enabled = ?", true)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Order("id").Limit(rebuildPageSize).Find(&page).Error; err != nil {
			return fmt.Errorf("page tier limits for rebuild: %w", err)
		}
		if len(page) == 0 {
			return nil
		}
		ids := make([]string, len(page))
		for i := range page {
			ids[i] = page[i].BudgetAllocationID.String()
		}
		rawFacts, err := s.rdb.HMGet(ctx, run.allocByIDKey, ids...).Result()
		if err != nil {
			return fmt.Errorf("resolve tier allocation facts: %w", err)
		}
		pipe := s.rdb.TxPipeline()
		for i, limit := range page {
			if rawFacts[i] == nil {
				continue
			}
			var allocation allocationFact
			if json.Unmarshal([]byte(fmt.Sprint(rawFacts[i])), &allocation) != nil {
				continue
			}
			window, ok := windows[limit.PeriodType]
			if !ok {
				continue
			}
			key := fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s",
				allocation.ScopeType, allocation.ScopeID, limit.ModelTierID.String(), window.Key)
			setCounterInitial(ctx, pipe, run, key, limit.PeriodType, window.ResetAt)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("store tier-limit rebuild page: %w", err)
		}
		cursor = page[len(page)-1].ID.String()
		if len(page) < rebuildPageSize {
			return nil
		}
	}
}

func (s *BudgetCounterRebuildService) addExistingTierKeys(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	windows map[budgetdomain.PeriodType]budgetperiod.Period,
	activeGeneration string,
) error {
	periodByKey := make(map[string]budgetdomain.PeriodType, len(windows))
	for periodType, window := range windows {
		periodByKey[window.Key] = periodType
	}
	var cursor uint64
	for {
		if lease != nil {
			if err := lease.check(ctx); err != nil {
				return err
			}
		}
		pattern := budgetrebuild.GenerationKey(activeGeneration, "budget:v2:tier:used:*")
		keys, next, err := s.rdb.Scan(ctx, cursor, pattern, rebuildPageSize).Result()
		if err != nil {
			return fmt.Errorf("scan existing tier counters: %w", err)
		}
		pipe := s.rdb.TxPipeline()
		for _, key := range keys {
			if activeGeneration != budgetrebuild.LegacyGeneration {
				key = strings.Replace(key, "budget:v2:gen:"+activeGeneration+":", "budget:v2:", 1)
			}
			parts := strings.Split(key, ":")
			if len(parts) != 8 {
				continue
			}
			periodType, ok := periodByKey[parts[7]]
			if !ok {
				continue
			}
			setCounterInitial(ctx, pipe, run, key, periodType, windows[periodType].ResetAt)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("store existing tier counter page: %w", err)
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *BudgetCounterRebuildService) aggregateCallLogs(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	windows map[budgetdomain.PeriodType]budgetperiod.Period,
	earliestStart, cutoff time.Time,
	result *RebuildV2Result,
) (int, error) {
	cursorTS := earliestStart
	cursorRequestID := ""
	sourceRows := 0
	for {
		if err := lease.check(ctx); err != nil {
			return 0, err
		}
		var page []obsmodel.CallLog
		query := s.db.WithContext(ctx).
			Where("ts >= ? AND ts <= ? AND status < ? AND est_cost_usd > ?", earliestStart, cutoff, 400, 0).
			Where("(ts > ?) OR (ts = ? AND request_id > ?)", cursorTS, cursorTS, cursorRequestID).
			Order("ts, request_id").Limit(rebuildPageSize)
		if err := query.Find(&page).Error; err != nil {
			return 0, fmt.Errorf("page call logs for budget rebuild: %w", err)
		}
		if len(page) == 0 {
			return sourceRows, nil
		}
		result.CallLogPages++
		for _, row := range page {
			if err := s.aggregateCallLog(ctx, run, windows, row); err != nil {
				return 0, err
			}
			sourceRows++
		}
		last := page[len(page)-1]
		cursorTS, cursorRequestID = last.TS, last.RequestID
		if len(page) < rebuildPageSize {
			return sourceRows, nil
		}
	}
}

func (s *BudgetCounterRebuildService) aggregateCallLog(
	ctx context.Context,
	run rebuildRun,
	windows map[budgetdomain.PeriodType]budgetperiod.Period,
	row obsmodel.CallLog,
) error {
	micro := decimal.NewFromFloat(row.EstCostUSD).Mul(decimal.NewFromInt(1_000_000)).Round(0).IntPart()
	if micro <= 0 {
		return nil
	}
	scopes := rebuildScopeCandidates(row)
	scopeFields := make([]string, len(scopes))
	for i, scope := range scopes {
		scopeFields[i] = scope.scopeType + ":" + scope.scopeID
	}
	rawAllocations, err := s.rdb.HMGet(ctx, run.allocByScopeKey, scopeFields...).Result()
	if err != nil {
		return fmt.Errorf("resolve call-log allocation facts: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	for i, raw := range rawAllocations {
		if raw == nil {
			continue
		}
		var allocation allocationFact
		if json.Unmarshal([]byte(fmt.Sprint(raw)), &allocation) != nil {
			continue
		}
		for periodType, window := range windows {
			if allocation.hasPeriod(periodType) && !row.TS.Before(window.StartAt) {
				key := fmt.Sprintf("budget:v2:used:%s:%s:%s",
					scopes[i].scopeType, scopes[i].scopeID, window.Key)
				pipe.HIncrBy(ctx, run.valuesKey, key, micro)
			}
		}
	}
	if row.BudgetTierAccountingEligible {
		for _, fact := range row.BudgetTierConstraintsSnapshot {
			periodType := budgetdomain.PeriodType(fact.PeriodType)
			window, ok := windows[periodType]
			if !ok || fact.Period != window.Key || row.TS.Before(window.StartAt) {
				continue
			}
			expected := fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s",
				fact.ScopeType, fact.ScopeID, fact.TierID, fact.Period)
			if fact.RedisKey != expected {
				continue
			}
			resetAt := fact.ResetAt
			if resetAt.IsZero() {
				resetAt = window.ResetAt
			}
			setCounterInitial(ctx, pipe, run, fact.RedisKey, periodType, resetAt)
			pipe.HIncrBy(ctx, run.valuesKey, fact.RedisKey, micro)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("aggregate call-log rebuild page: %w", err)
	}
	return nil
}

type rebuildScope struct{ scopeType, scopeID string }

func rebuildScopeCandidates(row obsmodel.CallLog) []rebuildScope {
	candidates := make([]rebuildScope, 0, 4)
	if row.KeyType == obsmodel.KeyTypeService {
		if row.DeptID != "" {
			candidates = append(candidates, rebuildScope{cpmodel.BudgetScopeDeptService, row.DeptID})
		}
	} else {
		if row.UserID != "" {
			candidates = append(candidates, rebuildScope{cpmodel.BudgetScopeMember, row.UserID})
		}
		if row.DeptID != "" {
			candidates = append(candidates, rebuildScope{cpmodel.BudgetScopeDeptDaily, row.DeptID})
		}
	}
	if row.StudioID != "" {
		candidates = append(candidates, rebuildScope{cpmodel.BudgetScopeStudio, row.StudioID})
	}
	candidates = append(candidates, rebuildScope{cpmodel.BudgetScopeCompany, cpmodel.BudgetScopeGlobal})
	return candidates
}

func (s *BudgetCounterRebuildService) validateAndCollectDiffs(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	result *RebuildV2Result,
	activeGeneration string,
) error {
	return s.scanRunValues(ctx, lease, run, func(values map[string]string) error {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		activeKeys := make([]string, len(keys))
		for i, key := range keys {
			activeKeys[i] = budgetrebuild.GenerationKey(activeGeneration, key)
		}
		actualValues, err := s.rdb.MGet(ctx, activeKeys...).Result()
		if err != nil {
			return fmt.Errorf("read official counters before rebuild: %w", err)
		}
		for i, key := range keys {
			expected, err := strconv.ParseInt(values[key], 10, 64)
			if err != nil || expected < 0 {
				return fmt.Errorf("invalid rebuilt counter %s=%q", key, values[key])
			}
			actual := int64(0)
			if actualValues[i] != nil {
				actual, err = strconv.ParseInt(fmt.Sprint(actualValues[i]), 10, 64)
				if err != nil || actual < 0 {
					return fmt.Errorf("invalid official budget counter %s=%q", key, actualValues[i])
				}
			}
			diff := newCounterDiff(key, expected, actual)
			if !diff.WithinThreshold {
				result.AllDiffsWithinThreshold = false
			}
			if len(result.Diffs) < maxReportedDiffs {
				result.Diffs = append(result.Diffs, diff)
			} else {
				result.DiffsTruncated = true
			}
			if legacyKey, ok := legacyCounterKey(key); ok {
				legacy, err := readLegacyMicroUSD(ctx, s.rdb, legacyKey)
				if err != nil {
					return err
				}
				legacyDiff := newCounterDiff(key, legacy, actual)
				if !legacyDiff.WithinThreshold {
					result.AllDiffsWithinThreshold = false
				}
				if len(result.LegacyV2Diffs) < maxReportedDiffs {
					result.LegacyV2Diffs = append(result.LegacyV2Diffs, legacyDiff)
				} else {
					result.DiffsTruncated = true
				}
			}
		}
		return nil
	})
}

func (s *BudgetCounterRebuildService) scanRunValues(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	handle func(map[string]string) error,
) error {
	var cursor uint64
	for {
		if lease != nil {
			if err := lease.check(ctx); err != nil {
				return err
			}
		}
		fields, next, err := s.rdb.HScan(ctx, run.valuesKey, cursor, "*", rebuildSwitchBatch).Result()
		if err != nil {
			return fmt.Errorf("scan rebuilt counters: %w", err)
		}
		for start := 0; start < len(fields); start += rebuildSwitchBatch * 2 {
			end := start + rebuildSwitchBatch*2
			if end > len(fields) {
				end = len(fields)
			}
			values := make(map[string]string, (end-start)/2)
			for i := start; i+1 < end; i += 2 {
				values[fields[i]] = fields[i+1]
			}
			if len(values) > 0 {
				if err := handle(values); err != nil {
					return err
				}
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

var buildGenerationScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return redis.error_reply('budget rebuild lease lost')
end
local count = tonumber(ARGV[2])
for i = 1, count do
  local value = ARGV[2 + i]
  if not string.match(value, '^%d+$') then
    return redis.error_reply('invalid rebuilt integer')
  end
end
for i = 1, count do
  redis.call('SET', KEYS[1 + i], ARGV[2 + i])
  redis.call('EXPIRE', KEYS[1 + i], ARGV[2 + count + i])
end
redis.call('PEXPIRE', KEYS[1], ARGV[3 + (count * 2)])
return count
`)

func (s *BudgetCounterRebuildService) buildGeneration(
	ctx context.Context,
	lease *rebuildLease,
	run rebuildRun,
	result *RebuildV2Result,
	now time.Time,
) error {
	return s.scanRunValues(ctx, lease, run, func(values map[string]string) error {
		if err := lease.check(ctx); err != nil {
			return err
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		metaValues, err := s.rdb.HMGet(ctx, run.metaKey, keys...).Result()
		if err != nil {
			return fmt.Errorf("read rebuilt counter metadata: %w", err)
		}
		scriptKeys := make([]string, 0, len(keys)+1)
		scriptKeys = append(scriptKeys, budgetrebuild.LockKey)
		for _, key := range keys {
			scriptKeys = append(scriptKeys, budgetrebuild.GenerationKey(run.generation, key))
		}
		args := make([]any, 0, 3+len(keys)*2)
		args = append(args, lease.token, len(keys))
		ttls := make([]int64, len(keys))
		for i, key := range keys {
			args = append(args, values[key])
			var meta counterMeta
			if metaValues[i] == nil || json.Unmarshal([]byte(fmt.Sprint(metaValues[i])), &meta) != nil {
				return fmt.Errorf("missing rebuilt counter metadata for %s", key)
			}
			ttls[i] = rebuildTTL(now, meta.ResetAt, meta.PeriodType)
		}
		for _, ttl := range ttls {
			args = append(args, ttl)
		}
		args = append(args, lease.ttl.Milliseconds())
		written, err := buildGenerationScript.Run(ctx, s.rdb, scriptKeys, args...).Int()
		if err != nil {
			return fmt.Errorf("build budget generation batch: %w", err)
		}
		result.KeysWritten += written
		result.SwitchBatches++
		return nil
	})
}

var activateGenerationScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return redis.error_reply('budget rebuild lease lost')
end
local current = redis.call('GET', KEYS[2]) or ARGV[2]
if current ~= ARGV[2] then
  return redis.error_reply('active budget generation changed')
end
redis.call('SET', KEYS[2], ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return 1
`)

func (s *BudgetCounterRebuildService) activateGeneration(
	ctx context.Context,
	lease *rebuildLease,
	previousGeneration, generation string,
) error {
	if err := lease.check(ctx); err != nil {
		return err
	}
	if err := activateGenerationScript.Run(ctx, s.rdb,
		[]string{budgetrebuild.LockKey, budgetrebuild.ActiveGenerationKey},
		lease.token, previousGeneration, generation, lease.ttl.Milliseconds(),
	).Err(); err != nil {
		return fmt.Errorf("activate rebuilt budget generation: %w", err)
	}
	return nil
}

func (s *BudgetCounterRebuildService) cleanupGeneration(ctx context.Context, run rebuildRun) error {
	return s.scanRunValues(ctx, nil, run, func(values map[string]string) error {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, budgetrebuild.GenerationKey(run.generation, key))
		}
		return s.rdb.Del(ctx, keys...).Err()
	})
}

func newCounterDiff(key string, expected, actual int64) CounterDiff {
	absDiff := expected - actual
	if absDiff < 0 {
		absDiff = -absDiff
	}
	allowed := int64(math.Ceil(float64(expected) * 0.001))
	if allowed < 1_000_000 {
		allowed = 1_000_000
	}
	ratio := 0.0
	if expected > 0 {
		ratio = float64(absDiff) / float64(expected)
	}
	return CounterDiff{
		Key: key, ExpectedMicroUSD: expected, ActualMicroUSD: actual,
		AbsDiffMicroUSD: absDiff, DiffRatio: ratio, AllowedDiffMicroUSD: allowed,
		WithinThreshold: absDiff <= allowed,
	}
}

func legacyCounterKey(v2Key string) (string, bool) {
	const prefix = "budget:v2:used:"
	if !strings.HasPrefix(v2Key, prefix) {
		return "", false
	}
	return "budget:used:" + strings.TrimPrefix(v2Key, prefix), true
}

func readLegacyMicroUSD(ctx context.Context, rdb *redis.Client, key string) (int64, error) {
	raw, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() {
		return 0, fmt.Errorf("invalid legacy budget counter %s=%q", key, raw)
	}
	return value.Mul(decimal.NewFromInt(1_000_000)).Round(0).IntPart(), nil
}

func rebuildTTL(now, resetAt time.Time, periodType budgetdomain.PeriodType) int64 {
	var retention time.Duration
	switch periodType {
	case budgetdomain.PeriodDaily:
		retention = 3 * 24 * time.Hour
	case budgetdomain.PeriodWeekly:
		retention = 14 * 24 * time.Hour
	default:
		retention = 70 * 24 * time.Hour
	}
	ttl := resetAt.Sub(now) + retention
	if ttl <= 0 {
		return 1
	}
	return int64(math.Ceil(ttl.Seconds()))
}
