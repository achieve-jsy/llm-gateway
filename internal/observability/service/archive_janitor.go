package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

// archiveJanitorBatchRows 置空每批行数（与契约 §5.4 一致：keyset 游标 + 每批 1000）。
const archiveJanitorBatchRows = 1000

// archiveJanitorSleep 批间 sleep，避免长时间占用 DB 连接与锁。
const archiveJanitorSleep = 200 * time.Millisecond

// Janitor 自己的 Redis leader key / 时区兜底（与写路径分离，避免跨文件常量耦合）。
const (
	janitorLockKey    = "trace:archive:janitor:leader"
	janitorLockTTL    = 30 * time.Minute
	janitorLockExtTTL = 5 * time.Minute
	// janitorFallbackTZ 时区加载失败时的兜底（默认值仍以 config 为准）。
	janitorFallbackTZ = "Asia/Shanghai"
)

// ArchiveJanitor 每日 cron：对「过热窗口」的已归档行做置空（正文离场，保留元数据）。
//
// v3（docs/trace-archive-solution-v3.md §3.4–§3.6）把置空从「看状态」升级为【门闸全过】：
//
//	① state==verified ② verified_at 非陈旧 ③ 冻结 rid 摘要存在
//	④ 逐块 sha256 校验 + 抽样回读全部命中 ⑤ 该 dt 巡检 PASS 且新鲜
//	⑥ 离机 marker 含 dt 且新鲜 ⑦ 磁盘水位达标
//	⑧ 已盖戳(archive_status=1)且有正文的行数 == expected_body_count（F1 第二道防线）
//
// 任何一项不过 → 跳过该 dt，记原因 + 告警（绝不静默跳过），正文继续留在 PG。
// 本类同时负责轮转巡检（archive_inspect.go）与保留期删除的同一套门闸。
type ArchiveJanitor struct {
	db         *gorm.DB
	rdb        *redis.Client
	archiver   *ArchiveService
	cfg        janitorConfig
	store      *archive.LocalStore
	loc        *time.Location
	instanceID string
}

// NewTraceArchiveJanitor 创建归档 Janitor（依赖 Archiver 以补归档 archive_status=0 泄漏行）。
// 配置由控制面注入 Config.Archive（单源化）；清理侧门闸参数见 janitorConfig。
func NewTraceArchiveJanitor(db *gorm.DB, rdb *redis.Client, archiver *ArchiveService, cfg cpconfig.ArchiveConfig) *ArchiveJanitor {
	host, _ := os.Hostname()
	jc := loadJanitorConfig(cfg)
	loc, err := time.LoadLocation(jc.tz)
	if err != nil {
		slog.Warn("archive: janitor invalid tz, fallback to Asia/Shanghai", "tz", jc.tz, "error", err)
		loc, _ = time.LoadLocation(janitorFallbackTZ)
	}
	var store *archive.LocalStore
	if jc.root != "" {
		store = archive.NewLocalStore(jc.root, "")
		// 解码窗口必须与写入侧一致，否则大窗口 zstd 帧无法回读。
		store.SetZstdWindowBytes(jc.zstdWindowMB << 20)
	}
	return &ArchiveJanitor{
		db:         db,
		rdb:        rdb,
		archiver:   archiver,
		cfg:        jc,
		store:      store,
		loc:        loc,
		instanceID: fmt.Sprintf("%s-%d", host, time.Now().UnixNano()),
	}
}

// Enabled 返回归档写侧是否启用（v3 §5.1：WriteEnabled 控制归档/置空/巡检/保留期删除）。
func (j *ArchiveJanitor) Enabled() bool { return j.cfg.writeEnabled }

// RunCycle 执行一次 Janitor 周期（单实例 leader lock）。
func (j *ArchiveJanitor) RunCycle(ctx context.Context) error {
	if !j.cfg.writeEnabled {
		slog.Info("archive: janitor disabled, skip")
		return nil
	}
	if !j.acquireJanitorLock(ctx) {
		slog.Debug("archive: janitor not leader, skip")
		return nil
	}
	defer j.releaseJanitorLock(ctx)

	extCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go j.extendJanitorLoop(extCtx)

	return j.run(ctx)
}

// run 执行：补归档 → 巡检 → 逐 dt 置空（门闸全过）→ 保留期删除 → 未过闸天审计 → 清理暂存残留。
func (j *ArchiveJanitor) run(ctx context.Context) error {
	now := time.Now().In(j.loc)
	cutoff := now.Add(-time.Duration(j.cfg.hotWindowDays) * 24 * time.Hour)
	// 候选 dt 必须整日早于 cutoff，避免把仍处热窗口的当天行提前置空。
	cutoffDate := cutoff.Format("2006-01-02")

	// Phase A：补归档 archive_status=0 的过热泄漏行（已关闭天的晚到/失败行）。
	// 补不上不阻塞置空；错误已内部告警。
	if err := j.archiveLeakRows(ctx, cutoff); err != nil {
		slog.Warn("archive: janitor leak-archive phase error", "error", err)
	}

	// Phase B：轮转巡检。必须早于置空——门闸⑤要求巡检 PASS 且新鲜，
	// 先巡检才能让本轮刚 verified 的天具备置空资格。
	j.RunInspectCycle(ctx)

	// Phase C：逐 dt 置空（唯一不可逆环节，7 项门闸全过才动正文）。
	cleaned, err := j.nullifyVerifiedDays(ctx, cutoffDate, now)
	if err != nil {
		return fmt.Errorf("janitor nullify: %w", err)
	}

	// Phase D：保留期删除（v3 §3.6，绝不到期静默删除）。
	j.RetentionSweep(ctx)

	// Phase E：把「PG 仍有 archive_status=1 行但 manifest 未 verified」的危险天显式报出来。
	j.reportUngatedDays(ctx, cutoff, cleaned)

	// Phase F：清理崩溃残留的 .spool/.tmp 暂存。
	// 此前 CleanupStaging 只有测试调用方，生产零接线——一次进程崩溃留下的未压缩暂存
	// 最多可达一个 chunk（默认 256 MiB）且永不回收，会持续吃掉 /datalog。
	// 按 staging_retention_hours（默认 336h）保留，避免误删正在写入的块。
	if j.store != nil {
		if res, cerr := j.store.CleanupStaging(ctx, time.Duration(j.cfg.stagingRetentionH)*time.Hour); cerr != nil {
			slog.Warn("archive: janitor cleanup staging failed", "error", cerr)
		} else if res.Removed > 0 {
			slog.Info("archive: janitor cleaned staging leftovers", "removed", res.Removed, "bytes", res.Bytes)
		}
	}

	slog.Info("archive: janitor cycle done", "cleaned_days", len(cleaned))
	return nil
}

// nullifyVerifiedDays 逐 dt 执行置空：先过门闸（v3 §3.4 的 7 项 + F1 的⑧），
// 再 keyset 分批幂等置空。返回本轮实际置空过的 dt 集合。
func (j *ArchiveJanitor) nullifyVerifiedDays(ctx context.Context, cutoffDate string, now time.Time) (map[string]bool, error) {
	cleaned := map[string]bool{}

	var ms []cpmodel.TraceArchiveManifest
	if err := j.db.WithContext(ctx).Raw(
		`SELECT * FROM trace_archive_manifests
		  WHERE state = ?
		    AND dt < ?::date
		  ORDER BY dt ASC`,
		cpmodel.ArchiveStateVerified, cutoffDate).Scan(&ms).Error; err != nil {
		return cleaned, fmt.Errorf("nullify candidate query: %w", err)
	}

	for i := range ms {
		if err := ctx.Err(); err != nil {
			return cleaned, err
		}
		m := &ms[i]
		dStr := m.DT.Format("2006-01-02")

		g := j.checkNullifyGates(ctx, m, now)
		if !g.OK {
			if g.Suspect {
				// 数据完整性类失败（F3：逐块 sha256 与 manifest.files 不符）：把该天打成 suspect，
				// 冻结其置空与保留期删除（v3 §3.5），直到人工/巡检确认归档可读。
				j.markInspectResult(ctx, dStr, cpmodel.ArchiveVerifySuspect, errors.New(g.Reason))
			}
			// 不得静默跳过：slog.Error + 审计留痕，正文继续留在 PG。
			slog.Error("archive: janitor nullify SKIPPED - gate failed; bodies stay in PG",
				"day", dStr, "gate", g.Gate, "reason", g.Reason)
			j.appendAudit(janitorAuditSkipFile, map[string]any{
				"dt": dStr, "gate": g.Gate, "stage": "nullify", "reason": g.Reason,
			})
			continue
		}

		n, err := j.nullifyDay(ctx, dStr)
		if err != nil {
			// 置空任一批失败：记 error，次轮幂等续做（已置空的行不会重复处理）。
			slog.Error("archive: janitor nullify failed; will retry next cycle", "day", dStr, "error", err)
			j.appendAudit(janitorAuditSkipFile, map[string]any{
				"dt": dStr, "gate": "C", "stage": "nullify", "reason": err.Error(),
			})
			continue
		}
		slog.Info("archive: janitor nullified day", "day", dStr, "rows", n)
		cleaned[dStr] = true
	}

	j.markCleanedDays(ctx, cleaned)
	return cleaned, nil
}

// nullifyDay 幂等置空某天正文：按 request_id keyset 分批（COLLATE "C" 与冻结序一致），
// 每批只更新 archive_status=1 的行（契约 §5.4），重复执行安全。
//
// F5：游标比较【必须】显式 `COLLATE "C"`（字节序），与 ORDER BY 及写路径 streamExport 口径一致。
// 若漏掉 COLLATE，在默认 collation 非 C 的库（本机实测 en_US.UTF-8）上 `request_id > $cursor`
// 会按本地 collation 比较，与 ORDER BY 的字节序不同 → 批次满 N 条时跳行，被跳过的行永不置空
// → 该天到不了 cleaned，整条归档链（含保留期删除）永久停摆。
//
// 写法说明：显式 collation 挂在校标操作数上（`request_id > ? COLLATE "C"`），与写路径
// archive_service.go 的 `ct.request_id COLLATE "C" > ?` 在 PostgreSQL 中语义完全等价
// （比较运算符的 collation 由任一带显式 collation 的操作数决定），已在 en_US.UTF-8 库上
// 逐例验证两种写法结果一致；不得删除该 COLLATE。
func (j *ArchiveJanitor) nullifyDay(ctx context.Context, dStr string) (int64, error) {
	var total int64
	lastID := ""

	for {
		var ids []string
		if err := j.db.WithContext(ctx).Raw(
			`SELECT request_id FROM call_traces
			  WHERE (ts AT TIME ZONE ?)::date = ?::date
			    AND archive_status = 1
			    AND request_id > ? COLLATE "C"
			  ORDER BY request_id COLLATE "C"
			  LIMIT ?`,
			j.cfg.tz, dStr, lastID, j.cfg.batchRows,
		).Scan(&ids).Error; err != nil {
			return total, fmt.Errorf("janitor batch query: %w", err)
		}
		if len(ids) == 0 {
			return total, nil
		}
		res := j.db.WithContext(ctx).Exec(
			`UPDATE call_traces
			    SET request_body = NULL, response_body = NULL, archive_status = 2
			  WHERE request_id IN (?) AND archive_status = 1`, ids)
		if res.Error != nil {
			return total, fmt.Errorf("janitor nullify batch: %w", res.Error)
		}
		total += res.RowsAffected
		lastID = ids[len(ids)-1]

		if len(ids) < j.cfg.batchRows {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
		time.Sleep(archiveJanitorSleep)
	}
}

// reportUngatedDays 审计「PG 有 archive_status=1 行、但 manifest 未 verified 或缺失」的天。
// 这类天是 v2 危险语义的遗留（指针已写、manifest 未收敛），必须显式告警而非静默留在 PG。
func (j *ArchiveJanitor) reportUngatedDays(ctx context.Context, cutoff time.Time, cleaned map[string]bool) {
	var days []time.Time
	if err := j.db.WithContext(ctx).Raw(
		`SELECT DISTINCT (ts AT TIME ZONE ?)::date AS d
		 FROM call_traces
		 WHERE archive_status = 1
		   AND ts < ?`, j.cfg.tz, cutoff).Scan(&days).Error; err != nil {
		slog.Warn("archive: janitor ungated day query failed", "error", err)
		return
	}
	for _, d := range days {
		dStr := d.Format("2006-01-02")
		if cleaned[dStr] {
			continue
		}
		var m cpmodel.TraceArchiveManifest
		err := j.db.WithContext(ctx).Where("dt = ?", d).First(&m).Error
		state := "missing"
		if err == nil {
			state = m.State
			// 已 verified 的天：门闸判定已在 nullifyVerifiedDays 逐条记录，此处不重复告警。
			if m.State == cpmodel.ArchiveStateVerified {
				continue
			}
		}
		slog.Error("archive: janitor left archive_status=1 rows in PG; manifest not verified, bodies stay in PG",
			"day", dStr, "manifest_state", state)
		j.appendAudit(janitorAuditSkipFile, map[string]any{
			"dt": dStr, "gate": "①", "stage": "nullify-scan",
			"manifest_state": state,
			"reason":         "call_traces 仍有 archive_status=1 行，但 manifest 未 verified",
		})
	}
}

// archiveLeakRows 对 archive_status=0 且过热的行按天补归档；补不上只告警不置空。
func (j *ArchiveJanitor) archiveLeakRows(ctx context.Context, cutoff time.Time) error {
	var days []time.Time
	if err := j.db.WithContext(ctx).Raw(
		`SELECT DISTINCT (ts AT TIME ZONE ?)::date AS d
		 FROM call_traces
		 WHERE archive_status = 0
		   AND ts < ?
		   AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		j.cfg.tz, cutoff,
	).Scan(&days).Error; err != nil {
		return fmt.Errorf("leak day query: %w", err)
	}
	if len(days) == 0 {
		return nil
	}
	slog.Info("archive: janitor leak-archive days", "count", len(days))
	for _, d := range days {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := j.archiver.ArchiveDay(ctx, d, false); err != nil {
			// 补不上 → 告警（不置空，正文留 PG）
			slog.Error("archive: janitor could not archive leak day",
				"day", d.Format("2006-01-02"), "error", err)
		}
	}
	return nil
}

// markCleanedDays 对已无待置空行的天，把 manifest 收敛到 cleaned（终态）。
// 只有 state==cleaned 且巡检 ok 的天才可能进入保留期删除（v3 §3.6）。
func (j *ArchiveJanitor) markCleanedDays(ctx context.Context, affected map[string]bool) {
	for dStr := range affected {
		d, err := time.Parse("2006-01-02", dStr)
		if err != nil {
			continue
		}
		var pending int64
		if err := j.db.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM call_traces
			 WHERE (ts AT TIME ZONE ?)::date = ?::date
			   AND archive_status IN (0, 1)
			   AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
			j.cfg.tz, dStr,
		).Scan(&pending).Error; err != nil {
			slog.Warn("archive: janitor pending count failed", "day", dStr, "error", err)
			continue
		}
		if pending > 0 {
			continue
		}
		var m cpmodel.TraceArchiveManifest
		if err := j.db.WithContext(ctx).Where("dt = ?", d).First(&m).Error; err != nil {
			continue
		}
		if m.State == cpmodel.ArchiveStateCleaned {
			continue
		}
		now := time.Now()
		m.State = cpmodel.ArchiveStateCleaned
		m.StateRank = 2
		m.CleanedAt = &now
		if err := j.db.WithContext(ctx).Save(&m).Error; err != nil {
			slog.Warn("archive: janitor manifest cleaned save failed", "day", dStr, "error", err)
			continue
		}
		slog.Info("archive: janitor day cleaned", "day", dStr)
	}
}

// ─── Redis leader lock（Janitor 与 Inspector 共用原语）─────────────────────────

func (j *ArchiveJanitor) acquireJanitorLock(ctx context.Context) bool {
	return acquireLeaderLock(ctx, j.rdb, janitorLockKey, j.instanceID, janitorLockTTL)
}

func (j *ArchiveJanitor) releaseJanitorLock(ctx context.Context) {
	releaseLeaderLock(ctx, j.rdb, janitorLockKey, j.instanceID)
}

func (j *ArchiveJanitor) extendJanitorLoop(ctx context.Context) {
	extendLeaderLock(ctx, j.rdb, janitorLockKey, j.instanceID, janitorLockExtTTL)
}

// acquireLeaderLock 抢一个 Redis leader lock（rdb==nil 时返回 false，调用方自行决定退化策略）。
func acquireLeaderLock(ctx context.Context, rdb *redis.Client, key, instanceID string, ttl time.Duration) bool {
	if rdb == nil {
		return false
	}
	ok, err := rdb.SetNX(ctx, key, instanceID, ttl).Result()
	if err != nil {
		slog.Warn("archive: leader lock SetNX failed", "key", key, "error", err)
		return false
	}
	return ok
}

// releaseLeaderLock 仅在自己持有锁时释放（Lua 比对 instanceID）。
func releaseLeaderLock(ctx context.Context, rdb *redis.Client, key, instanceID string) {
	if rdb == nil {
		return
	}
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)
	if _, err := script.Run(ctx, rdb, []string{key}, instanceID).Result(); err != nil {
		slog.Warn("archive: leader lock release failed", "key", key, "error", err)
	}
}

// extendLeaderLock 定时续期 leader lock（直到 ctx 取消）。
func extendLeaderLock(ctx context.Context, rdb *redis.Client, key, instanceID string, extTTL time.Duration) {
	ticker := time.NewTicker(extTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if rdb == nil {
				return
			}
			script := redis.NewScript(`
				if redis.call("GET", KEYS[1]) == ARGV[1] then
					return redis.call("EXPIRE", KEYS[1], ARGV[2])
				else
					return 0
				end
			`)
			if _, err := script.Run(ctx, rdb, []string{key}, instanceID, int(extTTL.Seconds())).Result(); err != nil {
				slog.Warn("archive: leader lock extend failed", "key", key, "error", err)
			}
		}
	}
}
