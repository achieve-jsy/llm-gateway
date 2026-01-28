package keyactivity

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// RunBootstrap 冷启动回灌：首次部署必须先用历史 call_logs 回灌已见来源表和初始化 EWMA 基线，
// 全程不写任何 key_activity_alert、不写任何 notifications。仅灌基线，保证幂等。
//
// 流程：
//  1. 遍历近 bootstrap_days 天 call_logs，在 Go 内算 source_fp，批量 INSERT ON CONFLICT DO NOTHING
//  2. 用近 14 天每 key 每小时调用数初始化 EWMA mean/dev，baseline_ready=true
//  3. 每 key 取历史最近 ts 写 last_seen_at
//  4. 游标推到 now（不回看历史），写 key_activity.bootstrap_done=true
func RunBootstrap(ctx context.Context, repo KeyActivityRepo, cfg *KeyActivityConfig) error {
	slog.Info("key_activity: bootstrap starting", "bootstrap_days", cfg.BootstrapDays)

	slog.Info("key_activity: bootstrap phase 1 - backfill source_seen")
	since := time.Now().AddDate(0, 0, -cfg.BootstrapDays)
	backfilled, err := backfillSourceSeen(ctx, repo, since, cfg.IPAsSource)
	if err != nil {
		return fmt.Errorf("bootstrap backfill source_seen: %w", err)
	}
	slog.Info("key_activity: bootstrap phase 1 done", "rows_backfilled", backfilled)

	slog.Info("key_activity: bootstrap phase 2 - init EWMA baseline (14d)")
	if err := initEWMABaseline(ctx, repo, cfg); err != nil {
		return fmt.Errorf("bootstrap init EWMA: %w", err)
	}
	slog.Info("key_activity: bootstrap phase 2 done")

	now := time.Now()
	repo.SaveCursor(Cursor{TS: now, RequestID: ""})
	repo.SetConfigValue("key_activity.bootstrap_done", "true")
	slog.Info("key_activity: bootstrap complete", "cursor", now)
	return nil
}

// Bootstrap 兼容包装器：接受 *Repo 并委托 RunBootstrap
func Bootstrap(ctx context.Context, repo *Repo, cfg *KeyActivityConfig) error {
	return RunBootstrap(ctx, repo, cfg)
}

// backfillSourceSeen 遍历历史 call_logs，计算 source_fp，批量 BatchUpsertSeen
func backfillSourceSeen(ctx context.Context, repo KeyActivityRepo, since time.Time, ipAsSource bool) (int, error) {
	const batchSize = 2000
	const upsertBatchSize = 500
	var totalBackfilled int
	var lastCursor Cursor

	for {
		if ctx.Err() != nil {
			return totalBackfilled, ctx.Err()
		}
		rows, err := repo.BatchFetchCallLogsForBootstrap(since, batchSize, lastCursor)
		if err != nil {
			return totalBackfilled, err
		}
		if len(rows) == 0 {
			break
		}

		var batch []KeySourceSeen
		for _, r := range rows {
			fp := SourceFP(r.UserAgent, r.IPAddress, ipAsSource)
			uaFP := UAFingerprint(r.UserAgent)
			batch = append(batch, KeySourceSeen{
				KeyID:     r.KeyID,
				SourceFP:  fp,
				UAFp:      uaFP,
				UARaw:     r.UserAgent,
				IP:        r.IPAddress,
				FirstSeen: r.TS,
				LastSeen:  r.TS,
				HitCount:  1,
			})
			if len(batch) >= upsertBatchSize {
				if err := repo.BatchUpsertSeen(batch); err != nil {
					return totalBackfilled, err
				}
				totalBackfilled += len(batch)
				batch = batch[:0]
			}
		}
		if len(batch) > 0 {
			if err := repo.BatchUpsertSeen(batch); err != nil {
				return totalBackfilled, err
			}
			totalBackfilled += len(batch)
		}

		lastRow := rows[len(rows)-1]
		lastCursor = Cursor{TS: lastRow.TS, RequestID: lastRow.RequestID}

		if len(rows) < batchSize {
			break
		}
	}
	return totalBackfilled, nil
}

// initEWMABaseline 用近 14 天每 key 每小时调用数初始化 EWMA mean/dev，并设置 last_seen_at
func initEWMABaseline(ctx context.Context, repo KeyActivityRepo, cfg *KeyActivityConfig) error {
	hourlyStats, err := repo.FetchRecentHourlyStats(14)
	if err != nil {
		return fmt.Errorf("fetch hourly stats: %w", err)
	}

	sinceDays := time.Now().AddDate(0, 0, -cfg.BootstrapDays)
	lastSeenMap, err := repo.FetchLastSeenPerKey(sinceDays)
	if err != nil {
		return fmt.Errorf("fetch last_seen: %w", err)
	}

	now := time.Now()
	processed := 0
	for keyID, hours := range hourlyStats {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(hours) == 0 {
			continue
		}
		mean, dev := calcMeanDev(hours)
		var lastSeen *time.Time
		if ts, ok := lastSeenMap[keyID]; ok {
			t := ts
			lastSeen = &t
		}
		state := &KeyActivityState{
			KeyID:         keyID,
			EwmaMean:      mean,
			EwmaDev:       dev,
			LastSeenAt:    lastSeen,
			LastSpikeHour: nil,
			Samples:       int64(len(hours)),
			BaselineReady: true,
			UpdatedAt:     now,
		}
		if err := repo.UpdateState(state); err != nil {
			slog.Warn("key_activity: bootstrap save state failed", "key_id", keyID, "error", err)
			continue
		}
		processed++
	}
	slog.Info("key_activity: EWMA baseline initialized", "keys_processed", processed)
	return nil
}

func calcMeanDev(hours map[time.Time]float64) (mean, dev float64) {
	if len(hours) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, cnt := range hours {
		sum += cnt
	}
	mean = sum / float64(len(hours))
	devSum := 0.0
	for _, cnt := range hours {
		devSum += math.Abs(cnt - mean)
	}
	dev = devSum / float64(len(hours))
	return mean, dev
}
