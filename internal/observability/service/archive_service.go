package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/archive"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

// ─── 归档运行时（docs/trace-archive-solution-v3.md §3/§4 + 实现契约 §5/§6）──────
// 单源化：归档运行时不再自行读环境变量，而是由控制面把 Config.Archive（Viper，yaml+env
// 统一生效）经构造器注入。ARCHIVE_* env 由 config 包经 AutomaticEnv 覆盖 yaml。

const (
	archiveLeaderKey     = "trace:archive:leader"
	archiveLeaderTTL     = 30 * time.Minute
	archiveLeaderExtTTL  = 5 * time.Minute
	archiveJanitorKey    = "trace:archive:janitor:leader"
	archiveJanitorTTL    = 30 * time.Minute
	archiveJanitorExtTTL = 5 * time.Minute
	// archiveFallbackTZ 仅用于时区加载失败时的兜底（默认值以 config 为准）。
	archiveFallbackTZ = "Asia/Shanghai"

	// archiveDefaultChunkMB / archiveDefaultZstdWindowMB / archiveDefaultEncoderConcurrency
	// 契约 §2.1 的默认值；WS-C 侧也有同名默认，这里保证服务层不因配置缺省而退化。
	archiveDefaultChunkMB            = 256
	archiveDefaultZstdWindowMB       = 128
	archiveDefaultEncoderConcurrency = 1
	archiveDefaultBatchRows          = 1000
)

// archiveSettings 归档任务所需配置（由 Config.Archive 注入构建，带默认值）。
// 字段命名与 v3 配置契约 §2.1 对齐；enabled 为旧字段，等价于 writeEnabled（janitor 兼容）。
type archiveSettings struct {
	enabled      bool // 旧字段：等同 writeEnabled
	writeEnabled bool // archive.write.enabled（v3 §5.1 拆分）
	readEnabled  bool // archive.read.enabled（由装配层使用）
	root         string
	tz           string

	hotWindowDays   int
	daysPerNight    int // 仅常规归档；<=0 表示不限
	closureLagHours int
	schedule        string

	chunkMB            int
	zstdLevel          string
	zstdWindowMB       int
	encoderConcurrency int
	maxConcurrentReads int

	batchRows int
	maxRetry  int

	verifySampleRows      int
	inspectFreshnessHours int
	inspectPerCycle       int

	backfillEnabled bool
	backfillMaxDays int

	requireOffhostCopy      bool
	offhostMarker           string
	offhostMarkerFreshnessH int

	stagingRetentionH int
	diskWatermarkPct  int // 磁盘水位（使用率百分比），≥该值告警；0 = 按 80 解释
	maxDiskGB         int // 归档总占用上限（GB）；0 = 不限
	retentionDays     int
}

// loadArchiveSettings 把控制面注入的 Config.Archive 转成运行时 archiveSettings，并补默认值。
func loadArchiveSettings(cfg cpconfig.ArchiveConfig) archiveSettings {
	s := archiveSettings{
		enabled:      cfg.WriteEnabled,
		writeEnabled: cfg.WriteEnabled,
		readEnabled:  cfg.ReadEnabled,
		root:         cfg.Root,
		tz:           cfg.TZ,

		hotWindowDays:   cfg.HotWindowDays,
		daysPerNight:    cfg.DaysPerNight,
		closureLagHours: cfg.ClosureLagHours,
		schedule:        cfg.CronSchedule,

		chunkMB:            cfg.ChunkMB,
		zstdLevel:          cfg.ZstdLevel,
		zstdWindowMB:       cfg.ZstdWindowMB,
		encoderConcurrency: cfg.EncoderConcurrency,
		maxConcurrentReads: cfg.MaxConcurrentReads,

		batchRows: cfg.BatchRows,
		maxRetry:  cfg.MaxRetry,

		verifySampleRows:      cfg.VerifySampleRows,
		inspectFreshnessHours: cfg.InspectFreshnessHours,
		inspectPerCycle:       cfg.InspectPerCycle,

		backfillEnabled: cfg.BackfillEnabled,
		backfillMaxDays: cfg.BackfillMaxDays,

		requireOffhostCopy:      cfg.RequireOffhostCopy,
		offhostMarker:           cfg.OffhostCopyMarker,
		offhostMarkerFreshnessH: cfg.OffhostMarkerFreshnessHours,

		stagingRetentionH: cfg.StagingRetentionHours,
		diskWatermarkPct:  cfg.DiskWatermarkPercent,
		maxDiskGB:         cfg.MaxDiskGB,
		retentionDays:     cfg.RetentionDays,
	}
	if s.batchRows <= 0 {
		s.batchRows = archiveDefaultBatchRows
	}
	if s.chunkMB <= 0 {
		s.chunkMB = archiveDefaultChunkMB
	}
	if s.zstdLevel == "" {
		s.zstdLevel = "best"
	}
	if s.zstdWindowMB <= 0 {
		s.zstdWindowMB = archiveDefaultZstdWindowMB
	}
	if s.encoderConcurrency <= 0 {
		s.encoderConcurrency = archiveDefaultEncoderConcurrency
	}
	return s
}

// scrubArchiveNUL 将 JSON 字节中残留的 \u0000 转义替换为 \ufffd，与 TraceWriter 写入侧保持一致。
func scrubArchiveNUL(b []byte) []byte {
	if !strings.Contains(string(b), `\u0000`) {
		return b
	}
	return []byte(strings.ReplaceAll(string(b), `\u0000`, `\ufffd`))
}

// rawOrNull 把 JSONB 原始字节转成 json.RawMessage；空/NULL 列输出 null（合法 JSON）。
func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// archiveTraceLine 归档文件每行的最小回放载荷（docs §4）。
// 字段顺序与文档示例一致；request_body/response_body/request_headers 保存原始 JSON。
type archiveTraceLine struct {
	RequestID      string          `json:"request_id"`
	TS             string          `json:"ts"`
	RequestHeaders json.RawMessage `json:"request_headers"`
	RequestBody    json.RawMessage `json:"request_body"`
	ResponseBody   json.RawMessage `json:"response_body"`
	ErrorCode      string          `json:"error_code"`
	ErrorMessage   string          `json:"error_message"`
}

// archiveExportRow 流式导出单行扫描结构。
type archiveExportRow struct {
	RequestID      string    `gorm:"column:request_id"`
	TS             time.Time `gorm:"column:ts"`
	RequestHeaders []byte    `gorm:"column:request_headers"`
	RequestBody    []byte    `gorm:"column:request_body"`
	ResponseBody   []byte    `gorm:"column:response_body"`
	ErrorCode      string    `gorm:"column:error_code"`
	ErrorMessage   string    `gorm:"column:error_message"`
}

// archiveExpected 一天开始归档前冻结的期望集（v3 §3.1 / 契约 §5.1）。
//
// 关键点：total 来自【不含 body 非空谓词】的基准 1，是校验②的全量分母——
// 这正是修 BLK-02（分母与导出谓词同源导致自证循环）的核心。
type archiveExpected struct {
	total          int64  // 基准 1：该天全部行数
	bodyCount      int64  // 基准 2：该天有正文行数
	bodyRIDsSHA256 string // 基准 2：有正文行 rid 按 COLLATE "C" 串联的 sha256（无正文行为空串）
}

// ArchiveService 按天从 call_traces 导出正文到本地归档目录（分块 zstd + index.jsonl +
// manifest.json + _SUCCESS），执行 v3 四步校验，驱动 trace_archive_manifests 状态机，
// 并在 verified 落库成功后回写 call_traces.archive_ref/archive_status=1（正文不动）。
// 单实例执行由 Redis leader lock 保证。
type ArchiveService struct {
	db         *gorm.DB
	rdb        *redis.Client
	cfg        archiveSettings
	loc        *time.Location
	store      archive.ArchiveStore
	instanceID string
}

// NewTraceArchiveService 创建归档服务（配置由控制面注入 Config.Archive，单源化）。
// 写路径使用的 ArchiveStore 由 Root 推导为 LocalStore；读路径的 store 由装配层另行构造。
func NewTraceArchiveService(db *gorm.DB, rdb *redis.Client, cfg cpconfig.ArchiveConfig) *ArchiveService {
	host, _ := os.Hostname()
	settings := loadArchiveSettings(cfg)
	loc, err := time.LoadLocation(settings.tz)
	if err != nil {
		slog.Warn("archive: invalid tz, fallback to Asia/Shanghai", "tz", settings.tz, "error", err)
		loc, _ = time.LoadLocation(archiveFallbackTZ)
	}
	store := archive.NewLocalStore(settings.root, "")
	if settings.zstdWindowMB > 0 {
		store.SetZstdWindowBytes(settings.zstdWindowMB << 20)
	}
	if settings.maxConcurrentReads > 0 {
		store.SetMaxConcurrentReads(settings.maxConcurrentReads)
	}
	return &ArchiveService{
		db:         db,
		rdb:        rdb,
		cfg:        settings,
		loc:        loc,
		store:      store,
		instanceID: fmt.Sprintf("%s-%d", host, time.Now().UnixNano()),
	}
}

// SetArchiveStore 替换归档存储后端（装配与测试注入用；nil 忽略）。
func (s *ArchiveService) SetArchiveStore(store archive.ArchiveStore) {
	if store != nil {
		s.store = store
	}
}

// Enabled 返回归档写路径是否启用（archive.write.enabled，v3 §5.1）。
func (s *ArchiveService) Enabled() bool { return s.cfg.writeEnabled }

// Schedule 返回归档 cron 表达式（供调度器使用）。
func (s *ArchiveService) Schedule() string { return s.cfg.schedule }

// BackfillTraceTaskFields 存量回填 call_traces 的 task_id / documents_count 独立列（Phase A）。
// 现状：migration 062 只 ADD COLUMN，extractTaskFields 仅覆盖新写入行；既有行（约 271 万）这两列全为 NULL。
// 而读侧（QueryLogs/ScopedLogs）已改为读 ct.task_id / ct.documents_count，故历史行列表回归 NULL。
// 只处理 task_id IS NULL AND request_body IS NOT NULL 的行；按 request_id keyset 游标分页（每批约 1000），
// 避免 `WHERE task_id IS NULL` 全表逐行 / 或 UPDATE 不改变 task_id 时重复命中造成死循环。
// task_id 取值与写侧 extractTaskFields 一致：resp.id > resp.task_id（不回退请求体 task_id，读侧从未用 req）。
// 幂等：同一行重跑得到相同值；跑完一行不再回看，收敛为 no-op。
func (s *ArchiveService) BackfillTraceTaskFields(ctx context.Context) error {
	const batchSize = 1000
	db := s.db.WithContext(ctx)

	type row struct {
		RequestID    string `gorm:"column:request_id"`
		RequestBody  []byte `gorm:"column:request_body"`
		ResponseBody []byte `gorm:"column:response_body"`
	}

	lastID := ""
	for {
		var rows []row
		if err := db.Raw(`
			SELECT ct.request_id, ct.request_body, ct.response_body
			FROM call_traces ct
			WHERE ct.task_id IS NULL
			  AND ct.request_body IS NOT NULL
			  AND ct.request_id > ?
			ORDER BY ct.request_id
			LIMIT ?`, lastID, batchSize).Scan(&rows).Error; err != nil {
			return fmt.Errorf("backfill task fields select: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.RequestID)
			if r.RequestID > lastID {
				lastID = r.RequestID
			}
		}
		if err := db.Exec(`
			UPDATE call_traces ct SET
				task_id = COALESCE(NULLIF(ct.response_body->>'id', ''), NULLIF(ct.response_body->>'task_id', '')),
				documents_count = CASE
					WHEN jsonb_typeof(ct.request_body->'documents') = 'array'
						THEN jsonb_array_length(ct.request_body->'documents')
					ELSE NULL
				END
			WHERE ct.request_id IN (?)`, ids).Error; err != nil {
			return fmt.Errorf("backfill task fields update: %w", err)
		}
		slog.Info("archive task-fields backfill batch", "batch_rows", len(ids), "last_request_id", lastID)
	}
}

// RunCycle 执行一次归档周期：水位线扫描 + 逐天驱动状态机（Archiver 单实例）。
func (s *ArchiveService) RunCycle(ctx context.Context) error {
	if !s.cfg.writeEnabled {
		slog.Info("archive: write disabled, skip cycle")
		return nil
	}
	if !s.acquireLeaderLock(ctx) {
		slog.Debug("archive: not leader, skip cycle")
		return nil
	}
	defer s.releaseLeaderLock(ctx)

	// 续期锁（与 ops_cleanup 同范式）
	extCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.extendLoop(extCtx, archiveLeaderKey, archiveLeaderExtTTL)

	return s.archiveWaterline(ctx)
}

// archiveWaterline 计算水位线并归档 [min_dt, waterline_dt) 范围内尚未终态的每一天。
// waterline_dt = 今天00:00(时区) - closure_lag_hours；增量每轮归档「上一个已关闭天」，存量覆盖历史。
// 每轮实际执行的归档天数受 days_per_night 限制（v3 §9：仅常规归档；终态天不占预算）。
func (s *ArchiveService) archiveWaterline(ctx context.Context) error {
	now := time.Now().In(s.loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	waterline := todayStart.Add(-time.Duration(s.cfg.closureLagHours) * time.Hour)

	var minDt time.Time
	if err := s.db.WithContext(ctx).Raw(
		`SELECT MIN((ts AT TIME ZONE ?)::date) FROM call_traces`, s.cfg.tz,
	).Scan(&minDt).Error; err != nil {
		slog.Error("archive: min_dt query failed", "error", err)
		return fmt.Errorf("archive min_dt: %w", err)
	}
	if minDt.IsZero() {
		slog.Info("archive: no call_traces data, skip")
		return nil
	}
	// 归一化为归档时区当天的 00:00，避免跨时区时刻比较导致枚举多/少一天。
	minDt = s.dayStart(minDt)
	waterline = s.dayStart(waterline)
	if !minDt.Before(waterline) {
		slog.Info("archive: nothing below waterline, skip", "min_dt", minDt, "waterline", waterline)
		return nil
	}

	slog.Info("archive: waterline sweep start",
		"min_dt", minDt, "waterline", waterline,
		"closure_lag_hours", s.cfg.closureLagHours,
		"days_per_night", s.cfg.daysPerNight,
	)

	// 磁盘容量守卫（每次归档周期一次，结果缓存，避免每批重复 Statfs）。
	// 超上限 → 跳过本轮，不写归档文件、不推进状态机、不置空；至少记一个 archiving 天 last_error 供观测。
	if guard := s.diskGuardOnce(s.cfg.root); guard.blocked {
		slog.Error("archive: disk quota exceeded, skip entire cycle",
			"root", s.cfg.root, "reason", guard.reason)
		var m cpmodel.TraceArchiveManifest
		if err := s.db.WithContext(ctx).
			Where("state = ?", cpmodel.ArchiveStateArchiving).
			Order("dt ASC").
			First(&m).Error; err == nil {
			_ = s.markManifestFailure(ctx, m.DT, fmt.Errorf("archive skipped: %s", guard.reason))
		}
		return nil
	}

	limit := s.cfg.daysPerNight
	worked := 0
	consecutiveFails := 0
	for d := minDt; d.Before(waterline); d = d.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if limit > 0 && worked >= limit {
			slog.Info("archive: days_per_night budget reached, stop this cycle", "days_per_night", limit)
			break
		}
		settled, didWork, err := s.archiveDay(ctx, d, false)
		if didWork {
			worked++
		}
		if err != nil {
			consecutiveFails++
			slog.Error("archive: day failed", "day", d.Format("2006-01-02"), "error", err)
			if consecutiveFails >= s.cfg.maxRetry {
				slog.Error("archive: continuous failure alert",
					"day", d.Format("2006-01-02"),
					"consecutive_fails", consecutiveFails,
					"max_retry", s.cfg.maxRetry,
				)
			}
			continue
		}
		consecutiveFails = 0
		if settled && didWork {
			slog.Info("archive: day archived", "day", d.Format("2006-01-02"))
		}
	}
	return nil
}

// ArchiveDay 归档某一天（对外入口，保持既有签名）。
// 返回 ok 表示该天已处于终态（verified / cleaned / empty，或本次刚到达终态）。
func (s *ArchiveService) ArchiveDay(ctx context.Context, d time.Time, force bool) (bool, error) {
	settled, _, err := s.archiveDay(ctx, d, force)
	return settled, err
}

// archiveDay v3 §4.1 的单日流程。
//
//	settled：该天是否已处于终态（无需再做）
//	worked ：本次是否真的做了归档工作（供 days_per_night 预算计数；终态短路与空天不占预算）
func (s *ArchiveService) archiveDay(ctx context.Context, d time.Time, force bool) (settled bool, worked bool, err error) {
	if !s.cfg.writeEnabled {
		return false, false, nil
	}
	d = s.dayStart(d)
	dStr := d.Format("2006-01-02")

	m, err := s.loadManifest(ctx, d)
	if err != nil {
		return false, false, err
	}

	if !force && m != nil {
		switch m.State {
		case cpmodel.ArchiveStateVerified:
			// F4：verified 天仍存在 status=0 的正文行（晚到/漏网，或「verified 已落库但指针写入失败」）。
			// 旧实现直接 return error，会让这类天【永远】无法补写指针——契约 §6 要求的
			// 「写指针失败 → 次轮幂等重试」成了死路（manifest 已 verified 就不会再走导出）。
			//
			// 这里改为【整日重新归档】，在 v3 语义下是安全的：
			//   1) state==verified 且未被 janitor 置空（门闸①要求 state==verified 且 janitor 置空后会把
			//      该天收敛为 cleaned），故该天正文此刻【仍在 PG】——重导读的是唯一权威副本，不是替代品；
			//   2) 重导重新执行「冻结集 → 分块写 → 四步校验 → 落 verified → rid 盖戳」全链路，
			//      会重建 index.jsonl / manifest.json / _SUCCESS 并把漏网行纳入归档，
			//      校验①②仍以新冻结的 expected_* 为独立分母，不走任何捷径；
			//   3) 盖戳按本轮 rid 列表逐行进行，重导后断言保证「盖戳集合 == 归档集合」逐行相等，
			//      因此重导不会制造「已盖戳但不在归档里」的行；
			//   4) 重导期间状态先回落 archiving（步骤④ UPSERT），janitor 门闸①因此不通过，置空被冻结。
			if n := s.countLeakRows(ctx, d); n > 0 {
				slog.Warn("archive: verified day has unarchived rows, re-archiving the whole day (bodies still in PG)",
					"day", dStr, "leak_rows", n)
				return s.exportDay(ctx, d)
			}
			return true, false, nil
		case cpmodel.ArchiveStateCleaned:
			// F4：cleaned 天仍有 status=0 的正文行才是真异常——该天已被判定「正文离场」，
			// 这些行既不在归档里、也不会再被正常流程归档，必须告警交人工处理，
			// 【绝不】把 cleaned 天重新归档（会与已置空的正文语义冲突）。
			if n := s.countLeakRows(ctx, d); n > 0 {
				slog.Error("archive: cleaned day still has unarchived body rows; refusing to re-archive, manual review required",
					"day", dStr, "leak_rows", n)
				return false, false, fmt.Errorf("cleaned day %s has %d unarchived rows", dStr, n)
			}
			return true, false, nil
		case cpmodel.ArchiveStateEmpty:
			// 空天：若出现晚到/漏网的有正文行，天型已变，需重新冻结并归档；否则直接短路。
			if n := s.countLeakRows(ctx, d); n == 0 {
				return true, false, nil
			}
			slog.Warn("archive: empty day has new body rows, re-evaluate day type", "day", dStr)
		}
	}

	return s.exportDay(ctx, d)
}

// ─── 单日导出（v3 §4.1，含四步校验与失败语义）────────────────────────────────

// exportDay 导出某天并完成四步校验、verified 落库与指针回写。
//
// 严格顺序（v3 §3.3 / §4.1，含 F1/F2b 修订）：
//
//	① 冻结期望行集（基准1 全量行数 + 基准2 有正文行 rid 摘要）——【只读】，先于任何破坏性动作
//	①' F2b 守卫：盘上已有 index.jsonl 而冻结 bodyCount==0 → state 被外部改动过：
//	    不删 _SUCCESS、不标 empty，改标 suspect + 告警 + 返回错误（人工复核）
//	② 删除旧 _SUCCESS + 清理遗留 .tmp（先于任何归档写；修 BLK-02 陈旧标记）
//	③ 天型判定：无正文 → state=empty，不写指针、不置空
//	④ UPSERT manifest(archiving, expected_*)；落库失败 → 立即终止本轮，不写任何归档文件
//	⑤ 流式导出 → 分块 zstd（PartWriter 写 index.jsonl / manifest.json），同时按导出顺序收集 rid
//	⑥ 四步校验之 ①②④（回读重算 / 全量覆盖 / 结构一致）
//	⑦ 写 _SUCCESS（最后一步写入）
//	⑧ 四步校验之 ③（_SUCCESS 存在且 mtime 不早于导出开始）
//	⑨ markManifestVerified 落库；失败 → 终止本轮并返回 error（修 BLK-01/B4）
//	⑩ writeArchivePointers：按【本轮 rid 列表】分批盖戳，使盖戳集合 == 归档集合（修 F1）
//	⑪ assertPointerCoverage：消费 expected_body_count 断言上述相等；不等 → 标失败并阻止 janitor
func (s *ArchiveService) exportDay(ctx context.Context, d time.Time) (bool, bool, error) {
	dStr := d.Format("2006-01-02")
	dir := s.dayDir(d)
	dayStart, dayEnd := s.dayRange(d)

	// 本次导出开始时间：必须在删除旧标记之前取，校验③以它为陈旧判据。
	exportStartedAt := time.Now()

	// ① 冻结期望行集（独立基准，分母不含 body 谓词）。
	//    F2b：本步【只读】且必须早于删除 _SUCCESS——只有先拿到冻结的 bodyCount，
	//    才能在执行任何破坏性动作之前识别「盘上已有归档、却被判为空天」的外部改动现场。
	exp, err := s.freezeExpectations(ctx, dayStart, dayEnd)
	if err != nil {
		return false, false, fmt.Errorf("archive %s: freeze expectations: %w", dStr, err)
	}

	// ①' F2b 前置守卫：盘上已有归档产物（index.jsonl）与冻结的 bodyCount==0 同时成立，
	//     说明该天此前确实归档成功过，而 manifest 状态被外部改动/丢失。
	//     若照旧删除 _SUCCESS 并标记 empty：RunInspectCycle 的候选集只有 verified/cleaned，
	//     empty 天永不被巡检，也没有任何路径会重写 _SUCCESS → 该天归档永久不可读（不可逆）。
	//     因此这里【不删除 _SUCCESS、不标 empty】，改为标 suspect + 告警 + 返回错误，交人工复核。
	//     Exists 报错（权限/IO）同样 fail-closed：证据不足时不执行任何破坏性动作。
	artifactsExist, aerr := s.store.Exists(ctx, archiveKey(dir, archive.IndexFileName))
	if aerr != nil {
		slog.Error("archive: check existing archive artifacts failed, abort day without touching markers",
			"day", dStr, "dir", dir, "error", aerr)
		return false, false, fmt.Errorf("archive %s: probe existing archive artifacts: %w", dStr, aerr)
	}
	if artifactsExist && exp.bodyCount == 0 {
		suspectErr := fmt.Errorf(
			"day %s: index.jsonl exists on disk but frozen expected_body_count==0 (manifest state externally changed)",
			dStr)
		if merr := s.markManifestSuspect(ctx, d, exp, suspectErr); merr != nil {
			slog.Error("archive: mark manifest suspect failed", "day", dStr, "error", merr)
		}
		slog.Error("archive: existing archive found while frozen body count is 0; _SUCCESS kept, day marked suspect, manual review required",
			"day", dStr, "dir", dir, "expected_total", exp.total, "expected_body_count", exp.bodyCount)
		return false, false, suspectErr
	}

	// ② 导出开始前先删除该 dt 的旧 _SUCCESS（v3 §3.3 步骤①，修 BLK-02）。
	//    绝不允许陈旧标记在本次导出期间存在，否则会把上一轮残留当成本轮证据。
	if err := s.removeSuccessMarker(ctx, dir); err != nil {
		slog.Error("archive: remove stale _SUCCESS failed, abort day", "day", dStr, "error", err)
		return false, false, fmt.Errorf("archive %s: remove stale _SUCCESS: %w", dStr, err)
	}
	// 顺手清理遗留 .part/.tmp（尽力而为；残留中间产物不影响正确性，但会占盘）。
	s.cleanupDayTempFiles(dir)

	// ③ 天型判定：该天无正文行（本就没有数据，或正文早已离场）→ state=empty。
	//    【不写指针、不置空、不计 KPI】（v3 §3.1）。
	//    盘上已有归档却判为空天的情况已在步骤 ①' 拦下（不会走到这里）。
	if exp.bodyCount == 0 {
		if err := s.markManifestEmpty(ctx, d, exp); err != nil {
			return false, false, fmt.Errorf("archive %s: mark empty: %w", dStr, err)
		}
		slog.Info("archive: day has no body rows, marked empty", "day", dStr, "expected_total", exp.total)
		return true, false, nil
	}

	// ④ 冻结集 UPSERT 落库（archiving + attempt+1）。落库失败 → 终止本轮，不写任何归档文件（v3 §3.3）。
	if err := s.upsertManifestArchiving(ctx, d, exp); err != nil {
		slog.Error("archive: manifest upsert failed, abort without writing archive files",
			"day", dStr, "error", err)
		return false, false, fmt.Errorf("archive %s: upsert manifest: %w", dStr, err)
	}

	// ⑤ 流式导出 → 分块 zstd。任何写盘失败 → Abort 清理 + manifest 回 archiving，正文绝不动。
	//    同时按【导出顺序】收集本轮真正写进归档的 request_id（供 ⑩ 逐行盖戳，修 F1）。
	res, rids, err := s.writeDayArchive(ctx, d, dir)
	if err != nil {
		_ = s.markManifestFailure(ctx, d, err)
		slog.Error("archive: export failed, bodies stay in PG", "day", dStr, "error", err)
		return false, true, fmt.Errorf("archive %s export: %w", dStr, err)
	}
	// 盖戳集合的基数必须等于归档行数：不等则「盖戳集合 == 归档集合」不可能成立，
	// 由此刻起 janitor 若按 status 点名置空就可能删掉归档里没有的正文 → 立即失败（fail-closed）。
	if int64(len(rids)) != res.RowCount {
		merr := fmt.Errorf("stamped rid count=%d != archived row_count=%d", len(rids), res.RowCount)
		_ = s.markManifestFailure(ctx, d, merr)
		slog.Error("archive: rid list length mismatch, bodies stay in PG", "day", dStr, "error", merr)
		return false, true, fmt.Errorf("archive %s: %w", dStr, merr)
	}

	// ⑥ 内容校验（四步校验之 ①②④）。不过 → 回 archiving，正文绝不动，也不写 _SUCCESS。
	mf, err := s.verifyArchiveContent(ctx, d, dir, res, exp)
	if err != nil {
		_ = s.markManifestFailure(ctx, d, err)
		slog.Error("archive: four-step verify (content) failed, bodies stay in PG", "day", dStr, "error", err)
		return false, true, fmt.Errorf("archive %s verify: %w", dStr, err)
	}

	// ⑦ 写 _SUCCESS（本次导出的最后一步写入）。
	if err := s.storeSuccessMarker(ctx, dir); err != nil {
		_ = s.markManifestFailure(ctx, d, err)
		return false, true, fmt.Errorf("archive %s: write _SUCCESS: %w", dStr, err)
	}

	// ⑧ 校验③：_SUCCESS 存在且 mtime 不早于本次导出开始时间（时间戳不可伪造）。
	//    不过 → 撤掉刚写的标记并回 archiving（四步全过才允许置 verified）。
	if err := s.verifySuccessMarker(ctx, dir, exportStartedAt); err != nil {
		_ = s.removeSuccessMarker(ctx, dir)
		_ = s.markManifestFailure(ctx, d, err)
		slog.Error("archive: four-step verify (success marker) failed, bodies stay in PG", "day", dStr, "error", err)
		return false, true, fmt.Errorf("archive %s verify: %w", dStr, err)
	}

	// ⑨ markManifestVerified 落库。失败必须终止本轮并返回 error——【严禁】仅 Warn 后继续写指针
	//    （v3 §3.3 修 B4 / 评审 BLK-01：manifest 未落 verified 却写指针会让 janitor 在错误前提下置空）。
	if err := s.markManifestVerified(ctx, d, res, mf.SHA256); err != nil {
		// 记 last_error 供观测（尽力而为；真正的要求是终止本轮、不写指针）。
		_ = s.markManifestFailure(ctx, d, err)
		slog.Error("archive: manifest verified save failed, abort cycle; archive pointers NOT written",
			"day", dStr, "error", err)
		return false, true, fmt.Errorf("archive %s: mark manifest verified: %w", dStr, err)
	}

	// ⑩ verified 落库成功之后才写指针。
	//    F1（严重·不可逆）：盖戳谓词【不再】是 ts 区间 + archive_status IN (0,1)——
	//    call_traces 没有 created_at/冻结快照列，ts 区间无法收窄到「导出读到的那批行」，
	//    于是导出之后、盖戳之前进入该天窗口的行会被误盖 status=1（但它不在归档里），
	//    janitor 随后按 status 无差别点名置空 → 正文永久删除且归档无副本。
	//    现改为按【本轮 rid 列表】分批盖戳：盖戳集合与归档集合逐行相等。
	if err := s.writeArchivePointers(ctx, d, rids); err != nil {
		slog.Error("archive: write archive pointers failed, will retry next cycle (manifest already verified)",
			"day", dStr, "error", err)
		return false, true, fmt.Errorf("archive %s: write archive_ref: %w", dStr, err)
	}

	// ⑪ F1 断言（expected_* 在置空侧的唯一消费点）：盖戳后的 PG 状态必须与冻结的归档集合逐行一致。
	//    不等 → slog.Error + 把该天标记为失败（state 回落 archiving，janitor 门闸① 因此不通过），
	//    【绝不允许】janitor 在「盖戳集合 ≠ 归档集合」的前提下继续置空。
	if err := s.assertPointerCoverage(ctx, d, exp.bodyCount); err != nil {
		_ = s.markManifestFailure(ctx, d, err)
		slog.Error("archive: pointer coverage assertion FAILED; day marked failed, janitor must not nullify",
			"day", dStr, "expected_body_count", exp.bodyCount, "error", err)
		return false, true, fmt.Errorf("archive %s: %w", dStr, err)
	}

	slog.Info("archive: day verified",
		"day", dStr,
		"rows", res.RowCount,
		"expected_total", exp.total,
		"expected_body_count", exp.bodyCount,
		"parts", len(res.Parts),
		"uncompressed_bytes", res.UncompressedBytes,
		"compressed_bytes", res.CompressedBytes,
	)
	return true, true, nil
}

// writeDayArchive 用 WS-C 的 PartWriter 流式导出当天待归档行。
// PartWriter.Close 只写 index.jsonl 与 manifest.json，**不写 _SUCCESS**（由 exportDay 在校验前后控制）。
// 返回值第二项是本轮真正写进归档的 request_id（按导出顺序，即 COLLATE "C" 顺序），供 F1 盖戳使用。
func (s *ArchiveService) writeDayArchive(ctx context.Context, d time.Time, dir string) (archive.PartWriterResult, []string, error) {
	w, err := archive.NewPartWriter(ctx, s.store, dir, archive.PartWriterOptions{
		SpoolDir:           archiveKey(dir, archive.SpoolDirName),
		ChunkBytes:         int64(s.cfg.chunkMB) << 20,
		ZstdWindowBytes:    s.cfg.zstdWindowMB << 20,
		ZstdLevel:          s.cfg.zstdLevel,
		EncoderConcurrency: s.cfg.encoderConcurrency,
	})
	if err != nil {
		return archive.PartWriterResult{}, nil, fmt.Errorf("partwriter: %w", err)
	}

	total, rids, err := s.streamExport(ctx, d, w)
	if err != nil {
		if aerr := w.Abort(ctx); aerr != nil {
			slog.Error("archive: partwriter abort failed", "dir", dir, "error", aerr)
		}
		return archive.PartWriterResult{}, nil, err
	}
	res, err := w.Close(ctx)
	if err != nil {
		if aerr := w.Abort(ctx); aerr != nil {
			slog.Error("archive: partwriter abort failed", "dir", dir, "error", aerr)
		}
		return archive.PartWriterResult{}, nil, fmt.Errorf("partwriter close: %w", err)
	}
	slog.Debug("archive: day exported", "dir", dir, "rows", total, "rids", len(rids))
	return res, rids, nil
}

// streamExport 用 keyset 游标流式导出当天 call_traces，逐行交给 PartWriter，
// 返回写出的行数与【按导出顺序】收集的 request_id 列表（F1）。
// 顺序即索引顺序：SQL 与游标比较一律 `COLLATE "C"`（字节序），与回读重算/FindPart 对齐（v3 §4.3）。
//
// 内存量级（F1 明确要求记录）：rid 列表常驻内存 = 行数 × (16 B string header + 约 36 B rid) ；
// 实测口径 116,891 行/天 ≈ 4.2 MB，峰值日 182,439 行 ≈ 6.6 MB（v3 §3.1 同口径），
// 即约 4~7 MB/天，相对单天归档导出（数百 MB~数 GB 未压缩正文）可忽略，故不做分段落盘。
func (s *ArchiveService) streamExport(ctx context.Context, d time.Time, w *archive.PartWriter) (int64, []string, error) {
	dayStart, dayEnd := s.dayRange(d)
	lastID := ""
	var total int64
	// 预分配按契约的日均量级（≈116k 行）推测，避免增长期多次扩容拷贝。
	rids := make([]string, 0, 128*1024)
	loc := s.loc

	for {
		rows, err := s.fetchBatch(ctx, dayStart, dayEnd, lastID)
		if err != nil {
			return total, nil, err
		}
		if len(rows) == 0 {
			return total, rids, nil
		}
		for _, r := range rows {
			line := archiveTraceLine{
				RequestID:      r.RequestID,
				TS:             r.TS.In(loc).Format(time.RFC3339),
				RequestHeaders: rawOrNull(scrubArchiveNUL(r.RequestHeaders)),
				RequestBody:    rawOrNull(scrubArchiveNUL(r.RequestBody)),
				ResponseBody:   rawOrNull(scrubArchiveNUL(r.ResponseBody)),
				ErrorCode:      r.ErrorCode,
				ErrorMessage:   r.ErrorMessage,
			}
			b, err := json.Marshal(line)
			if err != nil {
				return total, nil, fmt.Errorf("marshal trace line %s: %w", r.RequestID, err)
			}
			if err := w.WriteLine(b); err != nil {
				return total, nil, fmt.Errorf("write trace line %s: %w", r.RequestID, err)
			}
			// 只有【成功写进归档】的行才登记 rid——盖戳将严格按此列表进行（F1）。
			rids = append(rids, r.RequestID)
			total++
		}
		lastID = rows[len(rows)-1].RequestID
		if len(rows) < s.cfg.batchRows {
			return total, rids, nil
		}
		if err := ctx.Err(); err != nil {
			return total, nil, err
		}
	}
}

// exportReadSession 在【关闭并行】的事务中执行归档导出读，返回结果。
//
// 【这是纵深防御，不是修复已观测到的归档溢出 —— 实测证据见实现报告 §10】
//
// 测试环境 PG 18.4 上的对照实测（30000 行 × 226KB 正文，TOAST 7.0 GB，
// 均带 LIMIT 与 ORDER BY request_id COLLATE "C"，temp_file_limit 设 3GB 作保险丝）：
//
//	查询形状                                    LIMIT     临时文件
//	json_build_object(...)::text 投影正文        25000     超过 3GB（撞保险丝报错）
//	本函数逐列取原始列（无投影）                  25000     2.8 MB
//
// 机理：并行 worker 经共享内存队列把元组交回 leader 时【必须先把 TOAST 值 detoast】
// （TOAST 指针指向源进程的缓冲区，在别的进程里无效）。因此排序的输入若已含展开后的正文，
// 物化量就是正文总量（生产 226 KB/行 → GB 级）；若只是原始列，排序里是 TOAST 指针
// （约 18 字节/列），正文仅在行真正被投影出去时逐行 detoast（流式）。
//
// 结论：本函数只取原始列，本来就不在该危害的主路径上（生产 batch_rows 默认 1000，
// 实测量级为 MB 而非 GB）。关并行是廉价的纵深防御 —— call_traces 上
//「排序 + 对正文做投影」这一类查询已被实测证明足以写满 /data（生产 temp_file_limit = -1
// 无上限，一旦写满会导致 PostgreSQL 报 ENOSPC），将来若有人在 SELECT 列表里加了计算列，
// 本保护仍然成立。
//
// 用 SET LOCAL 限定在本次事务内，不污染连接池中连接上的后续查询。
// 注：max_parallel_workers_per_gather 的 context 是 user，普通应用角色即可设置
// （temp_file_limit 是 superuser，应用角色设不了，故只能从计划侧防守）。
func (s *ArchiveService) exportReadSession(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL max_parallel_workers_per_gather = 0").Error; err != nil {
			return fmt.Errorf("禁用导出并行: %w", err)
		}
		return fn(tx)
	})
}

// fetchBatch 查询一批（keyset 游标）当天待归档行（契约 §5.2）。
// 谓词与基准 2 一致（设计意图）；游标比较显式 `COLLATE "C"`，避免默认 collation 与
// ORDER BY 不一致时漏行（request_id 是客户端可控任意文本，v3 §4.3）。
func (s *ArchiveService) fetchBatch(ctx context.Context, dayStart, dayEnd time.Time, afterID string) ([]archiveExportRow, error) {
	var rows []archiveExportRow
	err := s.exportReadSession(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`
			SELECT ct.request_id, ct.ts, ct.request_headers, ct.request_body, ct.response_body,
			       ct.error_code, ct.error_message
			  FROM call_traces ct
			 WHERE ct.ts >= ? AND ct.ts < ?
			   AND (ct.request_body IS NOT NULL OR ct.response_body IS NOT NULL)
			   AND ct.archive_status IN (0, 1)
			   AND ct.request_id COLLATE "C" > ?
			 ORDER BY ct.request_id COLLATE "C"
			 LIMIT ?`,
			dayStart, dayEnd, afterID, s.cfg.batchRows,
		).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("fetch batch: %w", err)
	}
	return rows, nil
}

// freezeExpectations 冻结某天的期望集（契约 §5.1 两条 SQL）。
//
// 说明：基准 2 用 string_agg 一次性聚合（116,891 行 × 36 B ≈ 4.2 MB，可接受）。
// 若将来某天行数增长导致聚合内存压力，可改为分段聚合，但结果必须与
// `sha256(convert_to(string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C"), 'UTF8'))`
// 逐字节等价（契约 §5.1 注）。
func (s *ArchiveService) freezeExpectations(ctx context.Context, dayStart, dayEnd time.Time) (archiveExpected, error) {
	var exp archiveExpected

	// 基准 1：该天【全部】行数。【绝不】含 body 非空谓词——它是校验②的全量分母（修 BLK-02）。
	if err := s.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM call_traces WHERE ts >= ? AND ts < ?`,
		dayStart, dayEnd,
	).Scan(&exp.total).Error; err != nil {
		return exp, fmt.Errorf("freeze expected_total: %w", err)
	}

	// 基准 2：有正文行的 rid 摘要（固定字节序 COLLATE "C"）。
	// 无正文行时 string_agg 返回 NULL → sha 为 NULL，契约约定此时为 empty 天。
	var row struct {
		Cnt int64          `gorm:"column:cnt"`
		SHA sql.NullString `gorm:"column:sha"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT count(*) AS cnt,
		       encode(sha256(convert_to(
		         string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C"), 'UTF8')), 'hex') AS sha
		  FROM call_traces
		 WHERE ts >= ? AND ts < ?
		   AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		dayStart, dayEnd,
	).Scan(&row).Error; err != nil {
		return exp, fmt.Errorf("freeze expected_body_rids_sha256: %w", err)
	}
	exp.bodyCount = row.Cnt
	if row.SHA.Valid {
		exp.bodyRIDsSHA256 = row.SHA.String
	}
	if exp.bodyCount > 0 && exp.bodyRIDsSHA256 == "" {
		return exp, fmt.Errorf("expected_body_count=%d but rids sha256 is NULL", exp.bodyCount)
	}
	return exp, nil
}

// countNoBodyRows 统计某天无正文行数（= 基准 1 − 基准 2），供校验②使用。
func (s *ArchiveService) countNoBodyRows(ctx context.Context, dayStart, dayEnd time.Time) (int64, error) {
	var n int64
	if err := s.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM call_traces
		  WHERE ts >= ? AND ts < ?
		    AND (request_body IS NULL AND response_body IS NULL)`,
		dayStart, dayEnd,
	).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count no-body rows: %w", err)
	}
	return n, nil
}

// countLeakRows 统计某天内 archive_status=0 且正文仍在的行（泄漏行）。
func (s *ArchiveService) countLeakRows(ctx context.Context, d time.Time) int64 {
	dayStart, dayEnd := s.dayRange(d)
	var n int64
	_ = s.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM call_traces
		  WHERE ts >= ? AND ts < ?
		    AND archive_status = 0
		    AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		dayStart, dayEnd,
	).Scan(&n).Error
	return n
}

// ─── 四步校验（v3 §3.2）────────────────────────────────────────────────────

// verifyArchiveContent 执行四步校验之 ①②④（均为读盘/读库重算，不信任导出侧计数），
// 全部通过后返回盘上 manifest.json（其 SHA256 作为归档整体哈希写入 PG）。
func (s *ArchiveService) verifyArchiveContent(ctx context.Context, d time.Time, dir string, res archive.PartWriterResult, exp archiveExpected) (archive.ManifestFile, error) {
	dayStart, dayEnd := s.dayRange(d)
	dStr := d.Format("2006-01-02")

	// ① 回读重算：重新解压读盘，重算行数与 rid 摘要，必须等于冻结的基准 2。
	rb, err := archive.ReadBack(ctx, s.store, dir, res.Parts)
	if err != nil {
		return archive.ManifestFile{}, fmt.Errorf("verify① readback %s: %w", dStr, err)
	}
	if rb.RowCount != res.RowCount {
		return archive.ManifestFile{}, fmt.Errorf("verify① %s: readback rows=%d written rows=%d", dStr, rb.RowCount, res.RowCount)
	}
	if rb.RowCount != exp.bodyCount {
		return archive.ManifestFile{}, fmt.Errorf("verify① %s: readback rows=%d expected_body_count=%d", dStr, rb.RowCount, exp.bodyCount)
	}
	if rb.RIDsSHA256 != exp.bodyRIDsSHA256 {
		return archive.ManifestFile{}, fmt.Errorf("verify① %s: rids sha256 mismatch: readback=%s expected=%s",
			dStr, rb.RIDsSHA256, exp.bodyRIDsSHA256)
	}

	// ② 全量覆盖：导出行数 + 该天无正文行数 == expected_total（分母来自独立基准 1）。
	noBody, err := s.countNoBodyRows(ctx, dayStart, dayEnd)
	if err != nil {
		return archive.ManifestFile{}, fmt.Errorf("verify② %s: %w", dStr, err)
	}
	if res.RowCount+noBody != exp.total {
		return archive.ManifestFile{}, fmt.Errorf("verify② %s: exported=%d + no_body=%d != expected_total=%d",
			dStr, res.RowCount, noBody, exp.total)
	}

	// ④ 结构一致：index.jsonl 与实际文件一致，manifest.json 与实际字节数一致。
	parts, err := archive.LoadIndex(ctx, s.store, dir)
	if err != nil {
		return archive.ManifestFile{}, fmt.Errorf("verify④ %s: load index: %w", dStr, err)
	}
	if len(parts) != len(res.Parts) {
		return archive.ManifestFile{}, fmt.Errorf("verify④ %s: index parts=%d writer parts=%d", dStr, len(parts), len(res.Parts))
	}
	var idxRows, idxCompressed int64
	for i := range parts {
		if parts[i] != res.Parts[i] {
			return archive.ManifestFile{}, fmt.Errorf("verify④ %s: index[%d]=%+v differs from writer result=%+v",
				dStr, i, parts[i], res.Parts[i])
		}
		idxRows += parts[i].Rows
		idxCompressed += parts[i].CompressedBytes

		info, herr := s.store.Head(ctx, archiveKey(dir, parts[i].Part))
		if herr != nil {
			return archive.ManifestFile{}, fmt.Errorf("verify④ %s: head part %s: %w", dStr, parts[i].Part, herr)
		}
		if info.Size != parts[i].CompressedBytes {
			return archive.ManifestFile{}, fmt.Errorf("verify④ %s: part %s size on disk=%d index=%d",
				dStr, parts[i].Part, info.Size, parts[i].CompressedBytes)
		}
	}
	if idxRows != res.RowCount {
		return archive.ManifestFile{}, fmt.Errorf("verify④ %s: index rows=%d writer rows=%d", dStr, idxRows, res.RowCount)
	}
	if idxCompressed != res.CompressedBytes {
		return archive.ManifestFile{}, fmt.Errorf("verify④ %s: index compressed bytes=%d writer=%d", dStr, idxCompressed, res.CompressedBytes)
	}

	mf, err := archive.LoadManifest(ctx, s.store, dir)
	if err != nil {
		return archive.ManifestFile{}, fmt.Errorf("verify④ %s: load manifest.json: %w", dStr, err)
	}
	if mf.Parts != len(parts) ||
		mf.RowCount != res.RowCount ||
		mf.UncompressedBytes != res.UncompressedBytes ||
		mf.CompressedBytes != res.CompressedBytes ||
		mf.RIDsSHA256 != res.RIDsSHA256 ||
		len(mf.Files) != len(parts) {
		return archive.ManifestFile{}, fmt.Errorf(
			"verify④ %s: manifest.json inconsistent with index/writer result: parts=%d/%d rows=%d/%d uncompressed=%d/%d compressed=%d/%d",
			dStr, mf.Parts, len(parts), mf.RowCount, res.RowCount,
			mf.UncompressedBytes, res.UncompressedBytes, mf.CompressedBytes, res.CompressedBytes)
	}
	return mf, nil
}

// verifySuccessMarker 执行四步校验之 ③：_SUCCESS 必须存在，且 mtime 不早于本次导出开始时间。
//
// 说明：本步只能在写 _SUCCESS 之后执行（v3 §3.2③ 与 §4.1 步骤 8 的合并口径）；
// 由于导出开始前已强制删除旧标记（步骤①），这里再比对 mtime 是为了兜住
// 「删除失败 / 标记被外部重新造出」的情形——陈旧标记绝不作为完成证据。
//
// 文件系统 mtime 粒度可能只到秒，故把起始时间向下取整到秒后比较，避免秒内完成的导出误判。
func (s *ArchiveService) verifySuccessMarker(ctx context.Context, dir string, exportStartedAt time.Time) error {
	info, err := s.store.Head(ctx, archiveKey(dir, archive.SuccessMarkerName))
	if err != nil {
		return fmt.Errorf("verify③ _SUCCESS not found: %w", err)
	}
	if info.ModTime.Before(exportStartedAt.Truncate(time.Second)) {
		return fmt.Errorf("verify③ stale _SUCCESS: mtime=%s export_started_at=%s",
			info.ModTime.Format(time.RFC3339Nano), exportStartedAt.Format(time.RFC3339Nano))
	}
	return nil
}

// storeSuccessMarker 写 _SUCCESS（0 字节，覆盖写）。
func (s *ArchiveService) storeSuccessMarker(ctx context.Context, dir string) error {
	key := archiveKey(dir, archive.SuccessMarkerName)
	if _, err := s.store.Put(ctx, key, strings.NewReader(""), 0, archive.PutOptions{Overwrite: true}); err != nil {
		return fmt.Errorf("put _SUCCESS: %w", err)
	}
	return nil
}

// removeSuccessMarker 删除 _SUCCESS（不存在视为成功）。
func (s *ArchiveService) removeSuccessMarker(ctx context.Context, dir string) error {
	key := archiveKey(dir, archive.SuccessMarkerName)
	if err := s.store.Delete(ctx, key); err != nil && !archiveObjectMissing(err) {
		return err
	}
	return nil
}

// cleanupDayTempFiles 清理天目录下遗留的 .part/.tmp 中间产物（尽力而为，失败只告警）。
// 直接走文件系统：ArchiveStore.List 按设计会跳过临时文件名，无法用它发现残留。
func (s *ArchiveService) cleanupDayTempFiles(dir string) {
	if s.cfg.root == "" {
		return
	}
	dayPath := filepath.Join(s.cfg.root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(dayPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("archive: read day dir for temp cleanup failed", "dir", dir, "error", err)
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".part") && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if rerr := os.Remove(filepath.Join(dayPath, name)); rerr != nil && !os.IsNotExist(rerr) {
			slog.Warn("archive: remove stale temp file failed", "dir", dir, "file", name, "error", rerr)
		}
	}
}

// ─── manifest 落库（契约 §6 失败语义）────────────────────────────────────────

// loadManifest 读取某天的 trace_archive_manifests；不存在返回 (nil, nil)。
func (s *ArchiveService) loadManifest(ctx context.Context, d time.Time) (*cpmodel.TraceArchiveManifest, error) {
	var m cpmodel.TraceArchiveManifest
	err := s.db.WithContext(ctx).Where("dt = ?::date", d.Format("2006-01-02")).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	return &m, nil
}

// upsertManifestArchiving 冻结集 UPSERT 落库：state=archiving、写 expected_*、attempt+1、清 last_error。
// 失败必须由调用方终止本轮（契约 §6：manifest 落库失败不写任何归档文件）。
func (s *ArchiveService) upsertManifestArchiving(ctx context.Context, d time.Time, exp archiveExpected) error {
	var sha any
	if exp.bodyRIDsSHA256 != "" {
		sha = exp.bodyRIDsSHA256
	}
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO trace_archive_manifests
			(dt, state, state_rank, expected_total, expected_body_count, expected_body_rids_sha256,
			 files, row_count, attempt, created_at, updated_at)
		VALUES (?::date, ?, 0, ?, ?, ?, '[]'::jsonb, 0, 1, NOW(), NOW())
		ON CONFLICT (dt) DO UPDATE SET
			state = ?,
			state_rank = 0,
			expected_total = EXCLUDED.expected_total,
			expected_body_count = EXCLUDED.expected_body_count,
			expected_body_rids_sha256 = EXCLUDED.expected_body_rids_sha256,
			attempt = trace_archive_manifests.attempt + 1,
			last_error = NULL,
			updated_at = NOW()`,
		d.Format("2006-01-02"), cpmodel.ArchiveStateArchiving,
		exp.total, exp.bodyCount, sha,
		cpmodel.ArchiveStateArchiving,
	).Error
	if err != nil {
		return fmt.Errorf("upsert manifest archiving: %w", err)
	}
	return nil
}

// markManifestEmpty 空天（expected_body_count==0）落库：state=empty、state_rank=0。
// 【绝不写指针、绝不置空】（v3 §3.1）。
func (s *ArchiveService) markManifestEmpty(ctx context.Context, d time.Time, exp archiveExpected) error {
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO trace_archive_manifests
			(dt, state, state_rank, expected_total, expected_body_count, expected_body_rids_sha256,
			 files, row_count, attempt, created_at, updated_at)
		VALUES (?::date, ?, 0, ?, 0, NULL, '[]'::jsonb, 0, 0, NOW(), NOW())
		ON CONFLICT (dt) DO UPDATE SET
			state = ?,
			state_rank = 0,
			expected_total = EXCLUDED.expected_total,
			expected_body_count = 0,
			expected_body_rids_sha256 = NULL,
			last_error = NULL,
			updated_at = NOW()`,
		d.Format("2006-01-02"), cpmodel.ArchiveStateEmpty,
		exp.total,
		cpmodel.ArchiveStateEmpty,
	).Error
	if err != nil {
		return fmt.Errorf("mark manifest empty: %w", err)
	}
	return nil
}

// markManifestSuspect F2b：盘上已有归档产物、却被冻结基准判为「无正文」时，
// 把 manifest 标为 suspect（state=suspect + verify_state=suspect + inspect_error），
// 交由人工复核。绝不删除 _SUCCESS、绝不标 empty、绝不写指针、绝不置空。
//
// 为什么必须是 suspect 而不是 empty：
//   - RunInspectCycle 的候选集是 state IN (verified, cleaned)，empty 天永远不会被巡检，
//     而 _SUCCESS 一旦在导出步骤被删就再没有任何路径重写它 → 该天归档永久不可读；
//   - suspect 天同时被 janitor 门闸①（要求 verified）与保留期删除门闸挡在外面，安全。
//
// manifest 行丢失（0 行受影响）时执行 INSERT：把该天重新纳管为 suspect，而不是放任它「无 manifest」。
func (s *ArchiveService) markManifestSuspect(ctx context.Context, d time.Time, exp archiveExpected, cause error) error {
	msg := "suspect"
	if cause != nil {
		msg = cause.Error()
	}
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO trace_archive_manifests
			(dt, state, state_rank, expected_total, expected_body_count, expected_body_rids_sha256,
			 files, row_count, attempt, verify_state, inspect_error, created_at, updated_at)
		VALUES (?::date, ?, 0, ?, 0, NULL, '[]'::jsonb, 0, 0, ?, ?, NOW(), NOW())
		ON CONFLICT (dt) DO UPDATE SET
			state = EXCLUDED.state,
			state_rank = 0,
			verify_state = EXCLUDED.verify_state,
			inspect_error = EXCLUDED.inspect_error,
			updated_at = NOW()`,
		d.Format("2006-01-02"), cpmodel.ArchiveStateSuspect, exp.total,
		cpmodel.ArchiveVerifySuspect, msg,
	).Error
	if err != nil {
		return fmt.Errorf("mark manifest suspect: %w", err)
	}
	return nil
}

// markManifestVerified verified 落库（PG 侧唯一权威状态）。
//
// 失败必须返回 error 并由调用方终止本轮：绝不能只 Warn 后继续写指针（v3 §3.3 修 B4 / BLK-01）。
// verify_state 不在此处置位：按 v3 §3.5 它归巡检所有，避免导出侧“顺带”放行 janitor 的巡检门闸。
func (s *ArchiveService) markManifestVerified(ctx context.Context, d time.Time, res archive.PartWriterResult, manifestSHA string) error {
	// files = index 条目数组（契约 §1.2 / §3.2），供巡检与清理阶段消费。
	files, err := json.Marshal(res.Parts)
	if err != nil {
		return fmt.Errorf("marshal manifest files: %w", err)
	}
	dayStart, dayEnd := s.dayRange(d)

	var sha any
	if manifestSHA != "" {
		sha = manifestSHA
	}
	tx := s.db.WithContext(ctx).Exec(`
		UPDATE trace_archive_manifests
		   SET state = ?,
		       state_rank = 1,
		       files = ?::jsonb,
		       row_count = ?,
		       uncompressed_bytes = ?,
		       compressed_bytes = ?,
		       sha256 = ?,
		       min_ts = ?,
		       max_ts = ?,
		       verified_at = NOW(),
		       attempt = 0,
		       last_error = NULL,
		       updated_at = NOW()
		 WHERE dt = ?::date`,
		cpmodel.ArchiveStateVerified, string(files), res.RowCount,
		res.UncompressedBytes, res.CompressedBytes, sha,
		dayStart, dayEnd, d.Format("2006-01-02"),
	)
	if tx.Error != nil {
		return fmt.Errorf("update manifest verified: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		// manifest 行不存在（被外部删除等）→ 视为落库失败：不得在无 verified 记录时写指针。
		return fmt.Errorf("update manifest verified: no row for dt=%s", d.Format("2006-01-02"))
	}
	return nil
}

// markManifestFailure 归档失败：manifest 回 archiving、记 last_error（attempt 已在本轮 upsert 时 +1）。
func (s *ArchiveService) markManifestFailure(ctx context.Context, d time.Time, cause error) error {
	err := s.db.WithContext(ctx).Exec(`
		UPDATE trace_archive_manifests
		   SET state = ?,
		       state_rank = 0,
		       last_error = ?,
		       updated_at = NOW()
		 WHERE dt = ?::date`,
		cpmodel.ArchiveStateArchiving, cause.Error(), d.Format("2006-01-02"),
	).Error
	if err != nil {
		slog.Error("archive: mark manifest failure save failed", "dt", d.Format("2006-01-02"), "error", err)
		return err
	}
	return nil
}

// writeArchivePointers 四步校验通过且 verified 落库成功后回写指针（契约 §5.3，幂等）。
//
// F1：谓词由「ts 区间 + archive_status IN (0,1)」改为「request_id IN (本轮归档 rid) + archive_status IN (0,1)」。
// 原因：call_traces 没有 created_at 列，ts 区间无法收窄到导出时读到的那个冻结快照，
// 导出结束后进入该天窗口的新行会被旧谓词一并盖戳，但它们【不在归档里】——
// janitor 之后按 status 无差别点名置空，正文就永久删除且无归档副本（不可逆）。
// 按 rid 分批盖戳后，盖戳集合与归档集合逐行相等（request_id 是 call_traces 主键，全局唯一，无需再加 ts 谓词）。
// 仍保留 archive_status IN (0,1)：绝不覆盖已置空(2)的行；重复执行安全（rid 幂等）。
func (s *ArchiveService) writeArchivePointers(ctx context.Context, d time.Time, rids []string) error {
	ref := s.dayDir(d) + "/"
	batchRows := s.cfg.batchRows
	if batchRows <= 0 {
		// 防御：0 会让下面的分批循环无法推进（loadArchiveSettings 已兜底，这里再保一层）。
		batchRows = archiveDefaultBatchRows
	}
	for start := 0; start < len(rids); start += batchRows {
		end := start + batchRows
		if end > len(rids) {
			end = len(rids)
		}
		if err := s.db.WithContext(ctx).Exec(
			`UPDATE call_traces
			    SET archive_ref = ?, archive_status = 1, archived_at = NOW()
			  WHERE request_id IN (?) AND archive_status IN (0, 1)`,
			ref, rids[start:end],
		).Error; err != nil {
			return fmt.Errorf("write archive_ref batch [%d:%d): %w", start, end, err)
		}
	}
	return nil
}

// assertPointerCoverage F1 断言：盖戳完成后，PG 中「有正文且 archive_status=1」的行数
// 必须等于冻结的 expected_body_count（独立基准，分母不含「谁盖了戳」）。
//
// 这是 expected_* 这个独立基准在【置空侧】真正被消费一次的检查点：
//   - 相等 ⇒ 有正文且已盖戳的行集 == 本轮归档行集，janitor 按 status 点名置空不会丢正文；
//   - 不等 ⇒ 存在「已盖戳却不在归档里」（或反之）的行，立即返回错误，由调用方
//     标失败（state 回落 archiving）+ slog.Error，使 janitor 门闸① 不通过而无法置空。
//
// 断言谓词与冻结基准 2 对齐，仅额外要求 archive_status = 1。
func (s *ArchiveService) assertPointerCoverage(ctx context.Context, d time.Time, expectedBodyCount int64) error {
	dayStart, dayEnd := s.dayRange(d)
	var stamped int64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM call_traces
		 WHERE ts >= ? AND ts < ?
		   AND archive_status = 1
		   AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		dayStart, dayEnd,
	).Scan(&stamped).Error; err != nil {
		return fmt.Errorf("pointer coverage count: %w", err)
	}
	if stamped != expectedBodyCount {
		return fmt.Errorf("pointer coverage mismatch: stamped body rows=%d expected_body_count=%d",
			stamped, expectedBodyCount)
	}
	return nil
}

// ─── 时间与 key 辅助 ─────────────────────────────────────────────────────────

// dayStart 把任意时刻归一化为归档时区的当天 00:00。
func (s *ArchiveService) dayStart(d time.Time) time.Time {
	dd := d.In(s.loc)
	return time.Date(dd.Year(), dd.Month(), dd.Day(), 0, 0, 0, 0, s.loc)
}

// dayRange 返回该天在归档时区的 [start, end) 区间（ts >= start AND ts < end）。
func (s *ArchiveService) dayRange(d time.Time) (time.Time, time.Time) {
	start := s.dayStart(d)
	return start, start.AddDate(0, 0, 1)
}

// dayDir 返回该天的归档目录 store key（契约 §3.1：dt=2026-09-19）。
func (s *ArchiveService) dayDir(d time.Time) string {
	return "dt=" + d.Format("2006-01-02")
}

// archiveKey 拼接 store key（相对 archive.root），容忍两侧多余的 "/"。
func archiveKey(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "/")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// archiveObjectMissing 判断归档存储错误是否为 CodeNotFound。
// 命名与 WS-D（archive_inspect.go）的同义辅助函数区分，避免并行开发期符号冲突。
func archiveObjectMissing(err error) bool {
	if err == nil {
		return false
	}
	var se *archive.StoreError
	return errors.As(err, &se) && se.Code == archive.CodeNotFound
}

// ─── 磁盘容量守卫（docs/trace-archive-final-design.md §12/§13；写前主动守卫而非 ENOSPC 被动告警）────────

// diskGuardStat 一次磁盘守卫的只读结果。
type diskGuardStat struct {
	usedPercent float64 // 使用率（%）
	blocked     bool    // true = 超出上限，应跳过本轮归档
	reason      string  // 跳过原因（记 trace_archive_manifests.last_error 并告警）
	statfsErr   error   // statfs 失败时非 nil（放行 + 告警，不归责）
}

// archiveDirStats 统计 root 下归档文件总字节数与「dt=* 归档日」子目录数。
// 仅在 max_disk_gb>0 时由 diskGuardOnce 触发，每轮归档周期一次；遍历失败忽略（尽力而为）。
func archiveDirStats(root string) (bytes int64, days int) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略遍历错误，尽力而为
		}
		if d.IsDir() {
			// 只统计 root 的直接子目录中的 dt=* 归档日目录。
			if path != root && filepath.Dir(path) == root && strings.HasPrefix(d.Name(), "dt=") {
				days++
			}
			return nil
		}
		if info, e := d.Info(); e == nil {
			bytes += info.Size()
		}
		return nil
	})
	return bytes, days
}

// diskGuardDecision 磁盘守卫纯决策函数（便于单测）：
//   - usedPercent = 已用/总量百分比（供调用方做水位告警判断）。
//   - 仅当 maxGB>0（0=不限）才可能 blocked：超 max_disk_gb 或剩余 < 日均归档体积 2 倍 → blocked。
//   - 超过水位/上限绝不删除已有归档/正文。
func diskGuardDecision(totalB, availB uint64, archiveBytes int64, dayCount, maxGB int) (usedPercent float64, blocked bool, reason string) {
	if totalB == 0 {
		return 0, false, ""
	}
	usedB := totalB
	if totalB > availB {
		usedB = totalB - availB
	}
	usedPercent = float64(usedB) / float64(totalB) * 100

	if maxGB > 0 {
		maxBytes := int64(maxGB) << 30
		if archiveBytes >= maxBytes {
			blocked = true
			reason = fmt.Sprintf("archive size %d bytes >= max_disk_gb %d", archiveBytes, maxGB)
			return usedPercent, blocked, reason
		}
		dailyAvg := int64(0)
		if dayCount > 0 {
			dailyAvg = archiveBytes / int64(dayCount)
		}
		if availB < uint64(2*dailyAvg) {
			blocked = true
			reason = fmt.Sprintf("remaining %d bytes < 2x daily archive avg %d bytes", availB, 2*dailyAvg)
		}
	}
	return usedPercent, blocked, reason
}

// diskGuardOnce 检查目标盘（s.cfg.root 所在挂载点）容量。每次归档周期调用一次并缓存结果，避免每批重复 Statfs。
//   - 使用率 ≥ disk_watermark_percent → 仅 slog.Warn 告警并继续写（水位只是告警阈值）。
//   - max_disk_gb>0 且（超上限 或 剩余 < 日均体积 2 倍）→ blocked=true，调用方跳过本轮。
//   - statfs 失败 → 放行 + 告警（读不到状态不归责，避免误停归档）。
//
// 跨平台：用 syscall.Statfs（无 cgo）；Bsize 在 Linux 为 int64、Darwin 为 int32，统一转 uint64（本部署为 Linux）。
func (s *ArchiveService) diskGuardOnce(root string) diskGuardStat {
	var st diskGuardStat
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(root, &fsStat); err != nil {
		st.statfsErr = err
		slog.Warn("archive: disk guard statfs failed, allowing this cycle",
			"root", root, "error", err)
		return st
	}
	bsize := uint64(fsStat.Bsize)
	totalB := fsStat.Blocks * bsize
	availB := fsStat.Bavail * bsize

	archiveBytes, days := archiveDirStats(root)
	st.usedPercent, st.blocked, st.reason = diskGuardDecision(
		totalB, availB, archiveBytes, days, s.cfg.maxDiskGB)

	wm := s.cfg.diskWatermarkPct
	if wm <= 0 {
		wm = 80
	}
	if st.usedPercent >= float64(wm) {
		slog.Warn("archive: disk usage above watermark, continue",
			"root", root, "used_percent", fmt.Sprintf("%.1f", st.usedPercent),
			"watermark_percent", wm, "avail_bytes", availB, "avail_mb", availB>>20)
	}
	if st.blocked {
		slog.Warn("archive: disk quota exceeded, skip cycle",
			"root", root, "reason", st.reason,
			"blocked_used_percent", fmt.Sprintf("%.1f", st.usedPercent))
	}
	return st
}

// ─── Redis leader lock（与 ops_cleanup 同范式）───────────────────────────────

func (s *ArchiveService) acquireLeaderLock(ctx context.Context) bool {
	if s.rdb == nil {
		return false
	}
	ok, err := s.rdb.SetNX(ctx, archiveLeaderKey, s.instanceID, archiveLeaderTTL).Result()
	if err != nil {
		slog.Warn("archive: leader lock SetNX failed", "error", err)
		return false
	}
	return ok
}

func (s *ArchiveService) releaseLeaderLock(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)
	if _, err := script.Run(ctx, s.rdb, []string{archiveLeaderKey}, s.instanceID).Result(); err != nil {
		slog.Warn("archive: leader lock release failed", "error", err)
	}
}

// extendLoop 定时续期 leader lock（直到 ctx 取消）。
func (s *ArchiveService) extendLoop(ctx context.Context, key string, extTTL time.Duration) {
	ticker := time.NewTicker(extTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.rdb == nil {
				return
			}
			script := redis.NewScript(`
				if redis.call("GET", KEYS[1]) == ARGV[1] then
					return redis.call("EXPIRE", KEYS[1], ARGV[2])
				else
					return 0
				end
			`)
			if _, err := script.Run(ctx, s.rdb, []string{key}, s.instanceID, int(extTTL.Seconds())).Result(); err != nil {
				slog.Warn("archive: leader lock extend failed", "error", err)
			}
		}
	}
}
