package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetrebuild"
	"github.com/example/llm-gateway/internal/calllogstream"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/example/llm-gateway/internal/gateway/observability/model"
)

const (
	CallLogSpoolStream    = calllogstream.Spool
	CallLogSpoolDLQStream = calllogstream.DLQ
	callLogSpoolGroup     = calllogstream.ConsumerGroup
)

type CallLogSpoolConfig struct {
	MaxRetries   int
	ClaimMinIdle time.Duration
	Capacity     int64
}

type callLogBatchSink interface {
	BatchInsert(ctx context.Context, logs []model.CallLog) error
}

type CallLogSpool struct {
	rdb          *redis.Client
	consumer     string
	spooled      metric.Int64Counter
	replayed     metric.Int64Counter
	failures     metric.Int64Counter
	dlq          metric.Int64Counter
	maxRetries   int
	claimMinIdle time.Duration
	capacity     int64
}

func NewCallLogSpool(rdb *redis.Client) *CallLogSpool {
	return NewCallLogSpoolWithConfig(rdb, CallLogSpoolConfig{MaxRetries: 3})
}

func NewCallLogSpoolWithConfig(rdb *redis.Client, cfg CallLogSpoolConfig) *CallLogSpool {
	if rdb == nil {
		return nil
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.ClaimMinIdle <= 0 {
		cfg.ClaimMinIdle = 30 * time.Second
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 100_000
	}
	host, _ := os.Hostname()
	meter := otel.Meter("llm-gateway/observability")
	spooled, _ := meter.Int64Counter("gateway_call_log_spooled_total")
	replayed, _ := meter.Int64Counter("gateway_call_log_spool_replayed_total")
	failures, _ := meter.Int64Counter("gateway_call_log_spool_failure_total")
	dlq, _ := meter.Int64Counter("gateway_call_log_spool_dlq_total")
	return &CallLogSpool{
		rdb:          rdb,
		consumer:     host + "-" + strconv.Itoa(os.Getpid()) + "-" + uuid.NewString(),
		spooled:      spooled,
		replayed:     replayed,
		failures:     failures,
		dlq:          dlq,
		maxRetries:   cfg.MaxRetries,
		claimMinIdle: cfg.ClaimMinIdle,
		capacity:     cfg.Capacity,
	}
}

var appendWithinCapacityScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[1])
local count = tonumber(ARGV[3])
if redis.call('XLEN', KEYS[1]) + redis.call('ZCARD', KEYS[2]) + count > tonumber(ARGV[2]) then
  return 0
end
for i = 1, count do
  local offset = 3 + ((i - 1) * 2)
  redis.call('XADD', KEYS[1], '*',
    'request_id', ARGV[offset + 1], 'payload', ARGV[offset + 2], 'retries', 0)
end
return count
`)

var completeReservationScript = redis.NewScript(`
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
redis.call('XADD', KEYS[2], '*',
  'request_id', ARGV[1], 'payload', ARGV[2], 'retries', 0)
redis.call('ZREM', KEYS[1], ARGV[1])
return 1
`)

var completeAccountedReservationScript = redis.NewScript(`
local stream_type = redis.call('TYPE', KEYS[2])
if type(stream_type) == 'table' then stream_type = stream_type.ok end
if stream_type ~= 'none' and stream_type ~= 'stream' then
  return redis.error_reply('invalid call-log stream type')
end
local marker_type = redis.call('TYPE', KEYS[5])
if type(marker_type) == 'table' then marker_type = marker_type.ok end
if marker_type ~= 'none' and marker_type ~= 'string' then
  return redis.error_reply('invalid request marker type')
end
local member = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not member then
  if redis.call('EXISTS', KEYS[3]) == 1 then return 2 end
  local current = redis.call('GET', KEYS[4]) or 'legacy'
  if current ~= ARGV[3] then return 3 end
  if redis.call('XLEN', KEYS[2]) + redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[6]) then
    return 4
  end
end
local count = tonumber(ARGV[5])
local maxint = '9223372036854775807'
local function normalize(value)
  if not string.match(value, '^%d+$') then return nil end
  value = string.gsub(value, '^0+', '')
  if value == '' then return '0' end
  return value
end
local function add_decimal(a, b)
  local carry, out = 0, {}
  local ai, bi = string.len(a), string.len(b)
  while ai > 0 or bi > 0 or carry > 0 do
    local av, bv = 0, 0
    if ai > 0 then av = string.byte(a, ai) - 48 end
    if bi > 0 then bv = string.byte(b, bi) - 48 end
    local sum = av + bv + carry
    table.insert(out, 1, string.char((sum % 10) + 48))
    carry = math.floor(sum / 10)
    ai, bi = ai - 1, bi - 1
  end
  return table.concat(out)
end
local function greater_than(a, b)
  if string.len(a) ~= string.len(b) then return string.len(a) > string.len(b) end
  return a > b
end
local newly_accounted = 0
if redis.call('EXISTS', KEYS[5]) == 0 then
  local increment = normalize(ARGV[4])
  if not increment then return redis.error_reply('invalid budget increment') end
  for i = 1, count do
    local current = normalize(redis.call('GET', KEYS[5 + i]) or '0')
    if not current then return redis.error_reply('invalid budget counter') end
    if greater_than(add_decimal(current, increment), maxint) then
      return redis.error_reply('budget counter overflow')
    end
  end
  for i = 1, count do
    redis.call('INCRBY', KEYS[5 + i], ARGV[4])
    redis.call('EXPIRE', KEYS[5 + i], ARGV[6 + i])
  end
  redis.call('SET', KEYS[5], '1', 'EX', 7776000)
  newly_accounted = 1
end
redis.call('XADD', KEYS[2], '*',
  'request_id', ARGV[1], 'payload', ARGV[2], 'retries', 0)
redis.call('ZREM', KEYS[1], ARGV[1])
if newly_accounted == 1 then return 1 end
return 5
`)

var ErrReservationRebindUnsafe = errors.New("billable reservation cannot be safely rebound")

func (s *CallLogSpool) StoreReservedWithAccounting(
	ctx context.Context,
	log model.CallLog,
	pending *budget.PendingAccount,
	reservation *budget.Reservation,
) (bool, error) {
	if s == nil || pending == nil || pending.Plan == nil || reservation == nil {
		return false, ErrReservationRebindUnsafe
	}
	payload, err := json.Marshal(log)
	if err != nil {
		return false, err
	}
	generation := reservation.Generation()
	constraints := make([]budget.Constraint, 0, len(pending.Plan.Constraints))
	seen := make(map[string]struct{}, len(pending.Plan.Constraints))
	for _, constraint := range pending.Plan.Constraints {
		if constraint.Key == "" {
			continue
		}
		if _, exists := seen[constraint.Key]; exists {
			continue
		}
		seen[constraint.Key] = struct{}{}
		constraints = append(constraints, constraint)
	}
	for {
		keys := []string{
			budgetrebuild.BillableInflightKey, CallLogSpoolStream, budgetrebuild.LockKey,
			budgetrebuild.ActiveGenerationKey, "budget:v2:accounted:" + log.RequestID,
		}
		args := []any{
			log.RequestID, string(payload), generation, pending.CostMicroUSD,
			len(constraints), s.capacity,
		}
		now := time.Now()
		for _, constraint := range constraints {
			keys = append(keys, budget.GenerationKey(generation, constraint.Key))
			args = append(args, budget.CounterTTLSeconds(constraint, now))
		}
		code, runErr := completeAccountedReservationScript.Run(ctx, s.rdb, keys, args...).Int()
		if runErr != nil {
			s.recordFailure(ctx)
			return false, fmt.Errorf("%w: %v", ErrReservationRebindUnsafe, runErr)
		}
		switch code {
		case 1:
			if s.spooled != nil {
				s.spooled.Add(ctx, 1)
			}
			return true, nil
		case 5:
			if s.spooled != nil {
				s.spooled.Add(ctx, 1)
			}
			return false, nil
		case 2:
			if err := waitForRebuildRelease(ctx, s.rdb); err != nil {
				return false, fmt.Errorf("%w: %v", ErrReservationRebindUnsafe, err)
			}
		case 3:
			// active generation changed between read and atomic completion; retry below.
		case 4:
			return false, fmt.Errorf("%w: spool capacity reached", ErrReservationRebindUnsafe)
		default:
			return false, fmt.Errorf("%w: unexpected completion state %d", ErrReservationRebindUnsafe, code)
		}
		current, getErr := s.rdb.Get(ctx, budgetrebuild.ActiveGenerationKey).Result()
		if getErr == redis.Nil {
			current = budgetrebuild.LegacyGeneration
		} else if getErr != nil {
			return false, fmt.Errorf("%w: read active generation: %v", ErrReservationRebindUnsafe, getErr)
		}
		generation = current
	}
}

func waitForRebuildRelease(ctx context.Context, rdb *redis.Client) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		exists, err := rdb.Exists(ctx, budgetrebuild.LockKey).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
		}
	}
}

func (s *CallLogSpool) StoreReserved(ctx context.Context, log model.CallLog) error {
	if s == nil {
		return errors.New("call log spool is not configured")
	}
	payload, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("marshal reserved call log %s: %w", log.RequestID, err)
	}
	completed, err := completeReservationScript.Run(ctx, s.rdb,
		[]string{budgetrebuild.BillableInflightKey, CallLogSpoolStream},
		log.RequestID, string(payload),
	).Int()
	if err != nil {
		s.recordFailure(ctx)
		return fmt.Errorf("complete billable reservation %s: %w", log.RequestID, err)
	}
	if completed != 1 {
		slog.WarnContext(ctx, "billable reservation missing during completion; using capacity-checked append",
			"request_id", log.RequestID,
			"inflight_key", budgetrebuild.BillableInflightKey,
		)
		return s.Store(ctx, []model.CallLog{log})
	}
	if s.spooled != nil {
		s.spooled.Add(ctx, 1)
	}
	return nil
}

func (s *CallLogSpool) ensureGroup(ctx context.Context) error {
	if s == nil {
		return errors.New("call log spool is not configured")
	}
	err := s.rdb.XGroupCreateMkStream(ctx, CallLogSpoolStream, callLogSpoolGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create call log spool consumer group: %w", err)
	}
	return nil
}

func (s *CallLogSpool) Store(ctx context.Context, logs []model.CallLog) error {
	if s == nil {
		return errors.New("call log spool is not configured")
	}
	payloads := make([]string, len(logs))
	for i := range logs {
		payload, err := json.Marshal(logs[i])
		if err != nil {
			s.recordFailure(ctx)
			return fmt.Errorf("marshal call log %s for spool: %w", logs[i].RequestID, err)
		}
		payloads[i] = string(payload)
	}
	args := make([]any, 0, 3+len(logs)*2)
	args = append(args, time.Now().UnixMilli(), s.capacity, len(logs))
	for i := range logs {
		args = append(args, logs[i].RequestID, payloads[i])
	}
	appended, err := appendWithinCapacityScript.Run(ctx, s.rdb,
		[]string{CallLogSpoolStream, budgetrebuild.BillableInflightKey}, args...).Int()
	if err != nil {
		s.recordFailure(ctx)
		return fmt.Errorf("append %d call logs to spool atomically: %w", len(logs), err)
	}
	if appended != len(logs) {
		s.recordFailure(ctx)
		return fmt.Errorf("call log spool hard capacity %d reached", s.capacity)
	}
	if s.spooled != nil {
		s.spooled.Add(ctx, int64(len(logs)))
	}
	return nil
}

func (s *CallLogSpool) ReplayOnce(ctx context.Context, sink callLogBatchSink) error {
	if s == nil || sink == nil {
		return nil
	}
	if err := s.ensureGroup(ctx); err != nil {
		s.recordFailure(ctx)
		return err
	}
	messages, _, claimErr := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: CallLogSpoolStream, Group: callLogSpoolGroup, Consumer: s.consumer,
		MinIdle: s.claimMinIdle, Start: "0-0", Count: 100,
	}).Result()
	if claimErr != nil && claimErr != redis.Nil {
		// Redis < 6.2 cannot auto-claim. New messages remain replayable; the
		// error is surfaced so operators know pending recovery is degraded.
		s.recordFailure(ctx)
		return fmt.Errorf("claim pending call logs: %w", claimErr)
	}
	if len(messages) == 0 {
		streams, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: callLogSpoolGroup, Consumer: s.consumer,
			Streams: []string{CallLogSpoolStream, ">"}, Count: 100, Block: -1,
		}).Result()
		if err == redis.Nil {
			return nil
		}
		if err != nil {
			s.recordFailure(ctx)
			return fmt.Errorf("read call log spool: %w", err)
		}
		for _, stream := range streams {
			messages = append(messages, stream.Messages...)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	decoded := make([]decodedSpoolMessage, 0, len(messages))
	for _, message := range messages {
		raw, ok := message.Values["payload"]
		if !ok {
			if err := s.moveToDLQ(ctx, message, errors.New("payload is missing"), spoolRetries(message)+1); err != nil {
				return err
			}
			continue
		}
		var callLog model.CallLog
		if err := json.Unmarshal([]byte(fmt.Sprint(raw)), &callLog); err != nil {
			if moveErr := s.moveToDLQ(ctx, message, fmt.Errorf("decode payload: %w", err), spoolRetries(message)+1); moveErr != nil {
				return moveErr
			}
			continue
		}
		decoded = append(decoded, decodedSpoolMessage{message: message, callLog: callLog})
	}
	if len(decoded) == 0 {
		return nil
	}
	return s.persistIsolated(ctx, sink, decoded)
}

type decodedSpoolMessage struct {
	message redis.XMessage
	callLog model.CallLog
}

func (s *CallLogSpool) persistIsolated(
	ctx context.Context,
	sink callLogBatchSink,
	messages []decodedSpoolMessage,
) error {
	logs := make([]model.CallLog, len(messages))
	for i := range messages {
		logs[i] = messages[i].callLog
	}
	if err := sink.BatchInsert(ctx, logs); err != nil {
		if len(messages) > 1 {
			middle := len(messages) / 2
			if leftErr := s.persistIsolated(ctx, sink, messages[:middle]); leftErr != nil {
				return leftErr
			}
			return s.persistIsolated(ctx, sink, messages[middle:])
		}
		message := messages[0].message
		retries := spoolRetries(message) + 1
		s.recordFailure(ctx)
		if retries >= s.maxRetries {
			return s.moveToDLQ(ctx, message, err, retries)
		}
		return s.requeueWithRetry(ctx, message, err, retries)
	}
	ids := make([]string, len(messages))
	for i := range messages {
		ids[i] = messages[i].message.ID
	}
	return s.ackAndDelete(ctx, ids)
}

func spoolRetries(message redis.XMessage) int {
	retries, _ := strconv.Atoi(fmt.Sprint(message.Values["retries"]))
	return retries
}

func (s *CallLogSpool) requeueWithRetry(
	ctx context.Context,
	message redis.XMessage,
	replayErr error,
	retries int,
) error {
	values := cloneStreamValues(message.Values)
	values["retries"] = retries
	values["last_error"] = replayErr.Error()
	pipe := s.rdb.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: CallLogSpoolStream, Values: values})
	pipe.XAck(ctx, CallLogSpoolStream, callLogSpoolGroup, message.ID)
	pipe.XDel(ctx, CallLogSpoolStream, message.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		s.recordFailure(ctx)
		return fmt.Errorf("requeue call log spool entry %s: %w", message.ID, err)
	}
	return nil
}

func (s *CallLogSpool) moveToDLQ(
	ctx context.Context,
	message redis.XMessage,
	replayErr error,
	retries int,
) error {
	values := cloneStreamValues(message.Values)
	values["retries"] = retries
	values["error"] = replayErr.Error()
	values["source_id"] = message.ID
	pipe := s.rdb.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: CallLogSpoolDLQStream, Values: values})
	pipe.XAck(ctx, CallLogSpoolStream, callLogSpoolGroup, message.ID)
	pipe.XDel(ctx, CallLogSpoolStream, message.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		s.recordFailure(ctx)
		return fmt.Errorf("move call log spool entry %s to DLQ: %w", message.ID, err)
	}
	if s.dlq != nil {
		s.dlq.Add(ctx, 1)
	}
	slog.ErrorContext(ctx, "call log spool entry isolated to DLQ",
		"request_id", fmt.Sprint(message.Values["request_id"]),
		"retries", retries,
		"error", replayErr,
	)
	return nil
}

func cloneStreamValues(values map[string]any) map[string]any {
	out := make(map[string]any, len(values)+2)
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (s *CallLogSpool) ackAndDelete(ctx context.Context, ids []string) error {
	pipe := s.rdb.TxPipeline()
	pipe.XAck(ctx, CallLogSpoolStream, callLogSpoolGroup, ids...)
	pipe.XDel(ctx, CallLogSpoolStream, ids...)
	if _, err := pipe.Exec(ctx); err != nil {
		s.recordFailure(ctx)
		return fmt.Errorf("ack call log spool: %w", err)
	}
	if s.replayed != nil {
		s.replayed.Add(ctx, int64(len(ids)))
	}
	return nil
}

func (s *CallLogSpool) recordFailure(ctx context.Context) {
	if s != nil && s.failures != nil {
		s.failures.Add(ctx, 1)
	}
}

func (s *CallLogSpool) RunReplay(ctx context.Context, sink callLogBatchSink, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ReplayOnce(ctx, sink); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "call log spool replay failed",
				"stream", CallLogSpoolStream,
				"consumer", s.consumer,
				"error", err,
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
