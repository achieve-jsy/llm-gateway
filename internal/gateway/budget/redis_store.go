package budget

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/budgetrebuild"
	"github.com/example/llm-gateway/internal/calllogstream"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var checkScript = redis.NewScript(`
local function ge_decimal(a, b)
  a = string.gsub(a or '0', '^0+', '')
  b = string.gsub(b or '0', '^0+', '')
  if a == '' then a = '0' end
  if b == '' then b = '0' end
  if string.len(a) ~= string.len(b) then
    return string.len(a) > string.len(b)
  end
  return a >= b
end
local blocked = {}
for i = 1, #KEYS do
  local used = redis.call('GET', KEYS[i]) or '0'
  if ge_decimal(used, ARGV[i]) then
    table.insert(blocked, tostring(i))
    table.insert(blocked, used)
  end
end
return blocked
`)

var addScript = redis.NewScript(`
local MAX_INT64 = '9223372036854775807'
local count = #KEYS
local amount = ARGV[1]

local function normalize_integer(value)
  if not value or not string.match(value, '^%d+$') then
    return nil
  end
  value = string.gsub(value, '^0+', '')
  if value == '' then return '0' end
  return value
end

local function add_decimal(a, b)
  local carry = 0
  local out = {}
  local ai = string.len(a)
  local bi = string.len(b)
  while ai > 0 or bi > 0 or carry > 0 do
    local av = 0
    local bv = 0
    if ai > 0 then av = string.byte(a, ai) - 48 end
    if bi > 0 then bv = string.byte(b, bi) - 48 end
    local sum = av + bv + carry
    table.insert(out, 1, string.char((sum % 10) + 48))
    carry = math.floor(sum / 10)
    ai = ai - 1
    bi = bi - 1
  end
  return table.concat(out)
end

local function greater_than(a, b)
  if string.len(a) ~= string.len(b) then
    return string.len(a) > string.len(b)
  end
  return a > b
end

amount = normalize_integer(amount)
if not amount or amount == '0' then
  return redis.error_reply('invalid budget increment')
end
for i = 1, count do
  local current = normalize_integer(redis.call('GET', KEYS[i]) or '0')
  if not current then
    return redis.error_reply('invalid integer counter: ' .. KEYS[i])
  end
  if greater_than(add_decimal(current, amount), MAX_INT64) then
    return redis.error_reply('counter overflow: ' .. KEYS[i])
  end
end
for i = 1, count do
  redis.call('INCRBY', KEYS[i], ARGV[1])
  if redis.call('TTL', KEYS[i]) < 0 then
    redis.call('EXPIRE', KEYS[i], ARGV[i + 1])
  end
end
return 1
`)

var reserveScript = redis.NewScript(`
local generation = redis.call('GET', KEYS[4]) or ARGV[6]
if redis.call('EXISTS', KEYS[1]) == 1 then return {2, generation} end
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[1])
if redis.call('ZSCORE', KEYS[2], ARGV[4]) then return {1, generation} end
local used = redis.call('XLEN', KEYS[3]) + redis.call('ZCARD', KEYS[2])
if used >= tonumber(ARGV[2]) then return {3, generation} end
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('EXPIRE', KEYS[2], ARGV[5])
return {1, generation}
`)

var heartbeatScript = redis.NewScript(`
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  if redis.call('EXISTS', KEYS[2]) == 1 then return 2 end
  return 0
end
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1
`)

const (
	RebuildLockKey      = budgetrebuild.LockKey
	BillableInflightKey = budgetrebuild.BillableInflightKey
	CallLogSpoolStream  = calllogstream.Spool
	ActiveGenerationKey = budgetrebuild.ActiveGenerationKey
	LegacyGeneration    = budgetrebuild.LegacyGeneration
)

var (
	ErrRebuildInProgress = errors.New("budget rebuild in progress")
	ErrSpoolCapacity     = errors.New("call-log spool capacity reached")
)

type AdmissionConfig struct {
	SpoolCapacity     int64
	ReservationTTL    time.Duration
	HeartbeatInterval time.Duration
}

type CheckResult struct {
	Blockers   []Constraint
	RedisError error
}

type RedisStore struct {
	rdb             *redis.Client
	admission       AdmissionConfig
	reservations    sync.Map
	reservationLost metric.Int64Counter
	heartbeatError  metric.Int64Counter
}

func NewRedisStore(rdb *redis.Client) *RedisStore {
	return NewRedisStoreWithAdmission(rdb, AdmissionConfig{})
}

func NewRedisStoreWithAdmission(rdb *redis.Client, cfg AdmissionConfig) *RedisStore {
	if cfg.SpoolCapacity <= 0 {
		cfg.SpoolCapacity = 100_000
	}
	if cfg.ReservationTTL <= 0 {
		cfg.ReservationTTL = 10 * time.Minute
	}
	if cfg.HeartbeatInterval <= 0 || cfg.HeartbeatInterval >= cfg.ReservationTTL {
		cfg.HeartbeatInterval = cfg.ReservationTTL / 3
	}
	meter := otel.Meter("llm-gateway/budget")
	reservationLost, _ := meter.Int64Counter("gateway_budget_reservation_lost_total")
	heartbeatError, _ := meter.Int64Counter("gateway_budget_reservation_heartbeat_error_total")
	return &RedisStore{
		rdb: rdb, admission: cfg,
		reservationLost: reservationLost, heartbeatError: heartbeatError,
	}
}

func (s *RedisStore) Reserve(ctx context.Context, requestID string, now time.Time) error {
	_, err := s.ReserveWithGeneration(ctx, requestID, now)
	return err
}

type Reservation struct {
	store      *RedisStore
	requestID  string
	generation string
	cancel     context.CancelFunc
	done       chan struct{}
	once       sync.Once
	lost       atomic.Bool
	lastError  atomic.Pointer[reservationError]
}

type reservationError struct{ err error }

func (r *Reservation) Generation() string {
	if r == nil {
		return LegacyGeneration
	}
	return r.generation
}

func (r *Reservation) Lost() bool {
	return r != nil && r.lost.Load()
}

func (r *Reservation) Err() error {
	if r == nil {
		return nil
	}
	if value := r.lastError.Load(); value != nil {
		return value.err
	}
	return nil
}

func (r *Reservation) markLost(err error) {
	if r.lost.CompareAndSwap(false, true) && r.store.reservationLost != nil {
		r.store.reservationLost.Add(context.Background(), 1)
	}
	if err != nil {
		r.lastError.Store(&reservationError{err: err})
		if r.store.heartbeatError != nil {
			r.store.heartbeatError.Add(context.Background(), 1)
		}
	}
}

func (r *Reservation) Finish(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var err error
	r.once.Do(func() {
		r.cancel()
		<-r.done
		r.store.reservations.Delete(r.requestID)
		err = r.store.rdb.ZRem(ctx, BillableInflightKey, r.requestID).Err()
	})
	return err
}

func (s *RedisStore) ReserveWithGeneration(
	ctx context.Context,
	requestID string,
	now time.Time,
) (*Reservation, error) {
	if s == nil || s.rdb == nil {
		return nil, fmt.Errorf("budget redis store is not initialized")
	}
	if requestID == "" {
		return nil, fmt.Errorf("billable request id is empty")
	}
	expiresAt := now.Add(s.admission.ReservationTTL)
	raw, err := reserveScript.Run(ctx, s.rdb,
		[]string{RebuildLockKey, BillableInflightKey, CallLogSpoolStream, ActiveGenerationKey},
		now.UnixMilli(), s.admission.SpoolCapacity, expiresAt.UnixMilli(), requestID,
		int64(math.Ceil((s.admission.ReservationTTL + time.Minute).Seconds())),
		LegacyGeneration,
	).Slice()
	if err != nil {
		return nil, fmt.Errorf("reserve billable spool capacity: %w", err)
	}
	if len(raw) != 2 {
		return nil, fmt.Errorf("invalid billable reservation result %v", raw)
	}
	code, _ := strconv.Atoi(fmt.Sprint(raw[0]))
	generation := fmt.Sprint(raw[1])
	switch code {
	case 1:
		heartbeatCtx, cancel := context.WithCancel(context.Background())
		reservation := &Reservation{
			store: s, requestID: requestID, generation: generation,
			cancel: cancel, done: make(chan struct{}),
		}
		go reservation.heartbeat(heartbeatCtx)
		s.reservations.Store(requestID, reservation)
		return reservation, nil
	case 2:
		return nil, ErrRebuildInProgress
	case 3:
		return nil, ErrSpoolCapacity
	default:
		return nil, fmt.Errorf("unexpected billable reservation result %d", code)
	}
}

func (r *Reservation) heartbeat(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.store.admission.HeartbeatInterval)
	defer ticker.Stop()
	missingOnce := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			expiry := now.Add(r.store.admission.ReservationTTL).UnixMilli()
			result, err := heartbeatScript.Run(ctx, r.store.rdb,
				[]string{BillableInflightKey, RebuildLockKey},
				r.requestID, expiry,
				int64(math.Ceil((r.store.admission.ReservationTTL + time.Minute).Seconds())),
			).Int()
			if err != nil {
				r.markLost(fmt.Errorf("renew billable reservation: %w", err))
				return
			}
			if result == 0 && !missingOnce {
				missingOnce = true
				continue
			}
			if result != 1 {
				r.markLost(fmt.Errorf("billable reservation lost (state=%d)", result))
				return
			}
			missingOnce = false
		}
	}
}

func GenerationKey(generation, key string) string {
	return budgetrebuild.GenerationKey(generation, key)
}

func (s *RedisStore) Release(ctx context.Context, requestID string) error {
	if s == nil || s.rdb == nil || requestID == "" {
		return nil
	}
	if value, ok := s.reservations.Load(requestID); ok {
		return value.(*Reservation).Finish(ctx)
	}
	return s.rdb.ZRem(ctx, BillableInflightKey, requestID).Err()
}

func (s *RedisStore) Check(ctx context.Context, plan *AccountingPlan) CheckResult {
	if s == nil || s.rdb == nil || plan == nil {
		return CheckResult{RedisError: fmt.Errorf("budget redis store is not initialized")}
	}
	keys := make([]string, 0, len(plan.Constraints))
	args := make([]any, 0, len(plan.Constraints))
	constraints := make([]Constraint, 0, len(plan.Constraints))
	for _, constraint := range plan.Constraints {
		if constraint.StaticZero || constraint.LimitMicroUSD <= 0 {
			continue
		}
		keys = append(keys, GenerationKey(plan.Generation, constraint.Key))
		args = append(args, strconv.FormatInt(constraint.LimitMicroUSD, 10))
		constraints = append(constraints, constraint)
	}
	if len(keys) == 0 {
		return CheckResult{}
	}
	raw, err := checkScript.Run(ctx, s.rdb, keys, args...).Slice()
	if err != nil {
		return CheckResult{RedisError: err}
	}
	if len(raw)%2 != 0 {
		return CheckResult{RedisError: fmt.Errorf("invalid budget check result length %d", len(raw))}
	}
	result := CheckResult{Blockers: make([]Constraint, 0, len(raw)/2)}
	for i := 0; i < len(raw); i += 2 {
		index, err := strconv.Atoi(fmt.Sprint(raw[i]))
		if err != nil || index < 1 || index > len(constraints) {
			return CheckResult{RedisError: fmt.Errorf("invalid budget check result index %v", raw[i])}
		}
		used, err := strconv.ParseInt(fmt.Sprint(raw[i+1]), 10, 64)
		if err != nil || used < 0 {
			return CheckResult{RedisError: fmt.Errorf("invalid budget check used value %v", raw[i+1])}
		}
		blocker := constraints[index-1]
		blocker.UsedMicroUSD = used
		result.Blockers = append(result.Blockers, blocker)
	}
	return result
}

func (s *RedisStore) Add(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) error {
	if costMicroUSD <= 0 || plan == nil || len(plan.Constraints) == 0 {
		return nil
	}
	if s == nil || s.rdb == nil {
		return fmt.Errorf("budget redis store is not initialized")
	}
	keys := make([]string, 0, len(plan.Constraints))
	ttls := make([]any, 0, len(plan.Constraints)+1)
	ttls = append(ttls, strconv.FormatInt(costMicroUSD, 10))
	seen := make(map[string]struct{}, len(plan.Constraints))
	for _, constraint := range plan.Constraints {
		if constraint.Key == "" {
			continue
		}
		if _, ok := seen[constraint.Key]; ok {
			continue
		}
		seen[constraint.Key] = struct{}{}
		keys = append(keys, GenerationKey(plan.Generation, constraint.Key))
		ttls = append(ttls, ttlSeconds(constraint, time.Now()))
	}
	if len(keys) == 0 {
		return nil
	}
	return addScript.Run(ctx, s.rdb, keys, ttls...).Err()
}

func ttlSeconds(constraint Constraint, now time.Time) int64 {
	var retention time.Duration
	switch constraint.PeriodType {
	case budgetdomain.PeriodDaily:
		retention = 3 * 24 * time.Hour
	case budgetdomain.PeriodWeekly:
		retention = 14 * 24 * time.Hour
	default:
		retention = 70 * 24 * time.Hour
	}
	if constraint.ResetAt.IsZero() {
		return int64(retention.Seconds())
	}
	ttl := constraint.ResetAt.Sub(now) + retention
	if ttl <= 0 {
		return 1
	}
	return int64(math.Ceil(ttl.Seconds()))
}

func CounterTTLSeconds(constraint Constraint, now time.Time) int64 {
	return ttlSeconds(constraint, now)
}
