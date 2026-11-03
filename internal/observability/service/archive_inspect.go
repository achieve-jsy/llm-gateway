package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ─── WS-D：清理门闸（v3 §3.4）+ 轮转巡检（§3.5）+ 保留期删除（§3.6）───────────────
//
// 本文件覆盖「归档正文置空」这条【唯一不可逆】链路的全部前置判定：
//   - 置空：v3 §3.4 的 7 项门闸 + 盖戳计数门闸⑧（F1 第二道防线）全过才允许 SET body=NULL；
//   - 巡检：盘上 sha256 的真正消费点，失败即把该 dt 标记 suspect 并冻结置空与保留期删除（§3.5）；
//   - 保留期删除：state==cleaned + 巡检 ok 且新鲜 + 不在保护窗，绝不「到期静默删除」（§3.6）。
//
// 纪律（契约 §6 日志级别约定）：任何跳过都必须落 slog.Error 并写审计文件，禁止静默跳过；
// 读不到前置证据（statfs 失败、marker 不可读等）一律按「不过闸」处理（fail closed）——
// 宁可正文继续留在 PG，也不能在证据不足时销毁唯一副本。

const (
	// janitorAuditDirName 审计目录（相对 archive.root）；门闸跳过与保留期删除均留痕于此。
	janitorAuditDirName = "_audit"
	// janitorAuditSkipFile 置空门闸跳过审计（JSONL，按天追加）。
	janitorAuditSkipFile = "nullify-skips.jsonl"
	// janitorAuditRetentionFile 保留期删除审计（JSONL）。
	janitorAuditRetentionFile = "retention-deletions.jsonl"
	// janitorOffhostMarkerMaxRead 读取离机 marker 的最大字节数，防误指大文件。
	janitorOffhostMarkerMaxRead = 1 << 20
)

// errArchiveDayDeleted 表示该天的归档目录已不存在（保留期删除后的终态），巡检应跳过而非判失败。
var errArchiveDayDeleted = errors.New("archive day already deleted")

// markerTimeLayouts 离机 marker 行内可识别的时间戳格式（不含「仅日期」格式，
// 避免把行内的 dt 令牌本身误当作新鲜度时间戳）。
var markerTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006/01/02 15:04:05",
}

// janitorConfig 是 Janitor（置空 + 巡检 + 保留期删除）自持的配置快照。
//
// 直接由控制面注入的 cpconfig.ArchiveConfig 构建，不复用写路径的 archiveSettings：
// 清理不可逆，本文件读到的每个门闸参数必须与字段一一对应，避免与写路径结构耦合后被改坏。
type janitorConfig struct {
	writeEnabled                bool
	root                        string
	tz                          string
	hotWindowDays               int
	batchRows                   int
	zstdWindowMB                int
	requireOffhostCopy          bool
	offhostMarker               string
	offhostMarkerFreshnessHours int
	verifySampleRows            int
	inspectFreshnessHours       int
	inspectPerCycle             int
	diskWatermarkPct            int
	retentionDays               int
}

// loadJanitorConfig 把控制面注入的 Config.Archive 转成清理侧配置，并对 0 值兜底。
// retentionDays<=0 表示显式关闭保留期删除（绝不删除）。
func loadJanitorConfig(cfg cpconfig.ArchiveConfig) janitorConfig {
	jc := janitorConfig{
		writeEnabled:                cfg.WriteEnabled,
		root:                        cfg.Root,
		tz:                          cfg.TZ,
		hotWindowDays:               cfg.HotWindowDays,
		batchRows:                   cfg.BatchRows,
		zstdWindowMB:                cfg.ZstdWindowMB,
		requireOffhostCopy:          cfg.RequireOffhostCopy,
		offhostMarker:               cfg.OffhostCopyMarker,
		offhostMarkerFreshnessHours: cfg.OffhostMarkerFreshnessHours,
		verifySampleRows:            cfg.VerifySampleRows,
		inspectFreshnessHours:       cfg.InspectFreshnessHours,
		inspectPerCycle:             cfg.InspectPerCycle,
		diskWatermarkPct:            cfg.DiskWatermarkPercent,
		retentionDays:               cfg.RetentionDays,
	}
	if jc.tz == "" {
		jc.tz = janitorFallbackTZ
	}
	if jc.hotWindowDays <= 0 {
		jc.hotWindowDays = 3
	}
	if jc.batchRows <= 0 {
		jc.batchRows = archiveJanitorBatchRows
	}
	if jc.zstdWindowMB <= 0 {
		jc.zstdWindowMB = 128
	}
	if jc.verifySampleRows <= 0 {
		jc.verifySampleRows = 20
	}
	if jc.inspectFreshnessHours <= 0 {
		jc.inspectFreshnessHours = 168
	}
	if jc.inspectPerCycle <= 0 {
		jc.inspectPerCycle = 3
	}
	if jc.offhostMarkerFreshnessHours <= 0 {
		jc.offhostMarkerFreshnessHours = 24
	}
	if jc.diskWatermarkPct <= 0 {
		jc.diskWatermarkPct = 80
	}
	return jc
}

// inspectFreshness 巡检新鲜度窗口（v3 §3.4⑤ / §3.6）。
func (j *ArchiveJanitor) inspectFreshness() time.Duration {
	return time.Duration(j.cfg.inspectFreshnessHours) * time.Hour
}

// offhostFreshness 离机 marker 新鲜度窗口（v3 §3.4⑥）。
func (j *ArchiveJanitor) offhostFreshness() time.Duration {
	return time.Duration(j.cfg.offhostMarkerFreshnessHours) * time.Hour
}

// ─── 审计留痕 ───────────────────────────────────────────────────────────────

// appendAudit 向 {root}/_audit/<file> 追加一行 JSON 审计记录（尽力而为，失败仅告警）。
// 审计是「不得静默跳过」的落盘凭据；主记录仍是 slog.Error。
func (j *ArchiveJanitor) appendAudit(file string, payload map[string]any) {
	if j.cfg.root == "" {
		return
	}
	payload["ts"] = time.Now().Format(time.RFC3339Nano)
	b, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("archive: janitor audit marshal failed", "file", file, "error", err)
		return
	}
	dir := filepath.Join(j.cfg.root, janitorAuditDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Warn("archive: janitor audit mkdir failed", "dir", dir, "error", err)
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, file), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		slog.Warn("archive: janitor audit open failed", "file", file, "error", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		slog.Warn("archive: janitor audit write failed", "file", file, "error", err)
	}
}

// ─── 巡检组件（供调度层编排，装配契约见 internal/observability/cron/archive_cron.go）───

// inspectorLockKey 巡检独立 leader key：与 janitor 锁分离，多副本部署下同一轮只由一个副本巡检。
const inspectorLockKey = "trace:archive:inspector:leader"

// TraceArchiveInspector 轮转巡检的独立调度组件（v3 §3.5）。
//
// 实现 cron.CycleRunner（Enabled + RunCycle），由装配层注入 ArchiveCron；
// 未注入时 Janitor 自带同一套巡检（archive_janitor.go run 的 Phase B），机制不会缺席。
type TraceArchiveInspector struct {
	jan *ArchiveJanitor
}

// NewTraceArchiveInspector 创建巡检器（签名与装配层约定一致：db + archiver + 归档配置）。
// 复用 Archiver 的 Redis 连接做 leader 选举；Archiver 为 nil 时退化为无锁单实例巡检。
func NewTraceArchiveInspector(db *gorm.DB, svc *ArchiveService, cfg cpconfig.ArchiveConfig) *TraceArchiveInspector {
	var rdb *redis.Client
	if svc != nil {
		rdb = svc.rdb
	}
	return &TraceArchiveInspector{jan: NewTraceArchiveJanitor(db, rdb, svc, cfg)}
}

// Enabled 巡检与写路径同开关（契约 §7.1：WriteEnabled 控制归档/置空/巡检/保留期删除）。
func (i *TraceArchiveInspector) Enabled() bool { return i.jan.Enabled() }

// RunCycle 执行一轮轮转巡检（单实例 + 独立 leader lock）。
func (i *TraceArchiveInspector) RunCycle(ctx context.Context) error {
	j := i.jan
	if !j.cfg.writeEnabled {
		slog.Info("archive: inspector disabled, skip")
		return nil
	}
	if j.rdb != nil {
		if !acquireLeaderLock(ctx, j.rdb, inspectorLockKey, j.instanceID, janitorLockTTL) {
			slog.Debug("archive: inspector not leader, skip")
			return nil
		}
		defer releaseLeaderLock(ctx, j.rdb, inspectorLockKey, j.instanceID)

		extCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go extendLeaderLock(extCtx, j.rdb, inspectorLockKey, j.instanceID, janitorLockExtTTL)
	}
	j.RunInspectCycle(ctx)
	return nil
}

// ─── 轮转巡检（v3 §3.5：sha256 的真正消费点）─────────────────────────────────

// RunInspectCycle 执行一轮轮转巡检：按「最久未巡检优先」抽 inspect_per_cycle 个 dt 回读校验。
//
// 巡检通过 → verify_state=ok + last_inspected_at=now；
// 巡检失败 → verify_state=suspect + inspect_error，该 dt 的置空（门闸⑤）与保留期删除同时被冻结。
func (j *ArchiveJanitor) RunInspectCycle(ctx context.Context) {
	if j.store == nil {
		slog.Warn("archive: inspect skipped - archive store unavailable", "root", j.cfg.root)
		return
	}
	var ms []cpmodel.TraceArchiveManifest
	if err := j.db.WithContext(ctx).Raw(
		`SELECT * FROM trace_archive_manifests
		  WHERE state IN (?, ?)
		    AND expected_body_count > 0
		  ORDER BY last_inspected_at ASC NULLS FIRST, dt ASC
		  LIMIT ?`,
		cpmodel.ArchiveStateVerified, cpmodel.ArchiveStateCleaned, j.cfg.inspectPerCycle,
	).Scan(&ms).Error; err != nil {
		slog.Error("archive: inspect candidate query failed; all days keep their previous verify_state",
			"error", err)
		return
	}
	if len(ms) == 0 {
		return
	}
	now := time.Now()
	for i := range ms {
		if err := ctx.Err(); err != nil {
			return
		}
		j.inspectDay(ctx, &ms[i], now)
	}
}

// inspectDay 巡检单个 dt 并落库巡检结论。
func (j *ArchiveJanitor) inspectDay(ctx context.Context, m *cpmodel.TraceArchiveManifest, now time.Time) {
	dStr := m.DT.Format("2006-01-02")
	err := j.verifyDayArchive(ctx, m)
	if errors.Is(err, errArchiveDayDeleted) {
		// 已按保留期删除的终态天：无归档可回读，跳过而非判失败。
		slog.Debug("archive: inspect skipped - archive day already deleted", "day", dStr)
		return
	}
	if err != nil {
		j.markInspectResult(ctx, dStr, cpmodel.ArchiveVerifySuspect, err)
		slog.Error("archive: inspect FAILED - day marked suspect, nullify and retention-delete are BLOCKED for it",
			"day", dStr, "error", err)
		j.appendAudit(janitorAuditSkipFile, map[string]any{
			"dt": dStr, "gate": "⑤", "stage": "inspect", "reason": err.Error(),
		})
		return
	}
	j.markInspectResult(ctx, dStr, cpmodel.ArchiveVerifyOK, nil)
	slog.Info("archive: inspect passed", "day", dStr,
		"expected_body_count", m.ExpectedBodyCount,
		"verify_state", cpmodel.ArchiveVerifyOK)
}

// isArchiveNotFound 判断归档存储错误是否为「对象不存在」（不依赖字符串匹配）。
func isArchiveNotFound(err error) bool {
	var se *archive.StoreError
	return errors.As(err, &se) && se.Code == archive.CodeNotFound
}

// verifyDayArchive 回读归档并与冻结期望集比对（v3 §3.5）。返回 nil 表示校验通过。
func (j *ArchiveJanitor) verifyDayArchive(ctx context.Context, m *cpmodel.TraceArchiveManifest) error {
	dStr := m.DT.Format("2006-01-02")
	dir := "dt=" + dStr

	if m.ExpectedBodyRIDsSHA256 == nil || strings.TrimSpace(*m.ExpectedBodyRIDsSHA256) == "" {
		return fmt.Errorf("expected_body_rids_sha256 为空，无可比对的冻结基准")
	}

	// 归档目录缺失：已删除的终态天跳过，其余视为失败。
	dirPath := filepath.Join(j.cfg.root, dir)
	if _, statErr := os.Stat(dirPath); statErr != nil {
		if os.IsNotExist(statErr) {
			if m.State == cpmodel.ArchiveStateCleaned {
				return errArchiveDayDeleted
			}
			return fmt.Errorf("归档目录不存在: %s", dirPath)
		}
		return fmt.Errorf("归档目录不可访问: %s: %w", dirPath, statErr)
	}

	// _SUCCESS 必须存在（缺标记 = 该天不可信）。
	ok, err := j.store.Exists(ctx, dir+"/"+archive.SuccessMarkerName)
	if err != nil {
		return fmt.Errorf("检查 _SUCCESS 失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("_SUCCESS 缺失，该天归档不可信")
	}

	parts, err := j.dayParts(ctx, m, dir)
	if err != nil {
		return err
	}

	// 交叉校验盘上 index.jsonl：必须可读，且块名/块数/每块的 FirstRID/LastRID/Rows 与 PG 冻结的
	// files 逐条、逐顺序一致。
	//
	// F2 缺陷1：index.jsonl 读不出【不得】只 Warn 后继续——点读强依赖盘上 index（ReadBody 流程为
	// _SUCCESS → LoadIndex → FindPart → 点读），index 丢失会让 ReadBody 返回 CodeNotFound，
	// 对用户表现为「数据不存在」，而 parts 里的正文其实完好；且 WriteIndex 只有 PartWriter.Close
	// 一个调用点，verified/cleaned 的天在非 force 路径下永不重导出 → 这个「读不出来」是永久的。
	// 故这里必须返回 error（由 inspectDay 落 suspect + inspect_error + slog.Error）。
	//
	// F2 缺陷2：只比块名/块数是不够的——点读用盘上 index 的 LastRID 做二分，index 的 last_rid
	// 被改坏时块名仍一致、巡检会判 ok，但点读全 miss。故必须逐条比对定位字段与顺序。
	diskParts, ierr := archive.LoadIndex(ctx, j.store, dir)
	if ierr != nil {
		slog.Error("archive: inspect FAILED - cannot load index.jsonl; point read depends on it, day is NOT trustworthy",
			"day", dStr, "error", ierr)
		return fmt.Errorf("加载 index.jsonl 失败（点读强依赖该索引，缺失即对用户表现为数据不存在）: %w", ierr)
	}
	if len(diskParts) != len(parts) {
		return fmt.Errorf("index.jsonl 块数(%d) 与 manifest.files 块数(%d) 不一致",
			len(diskParts), len(parts))
	}
	for i := range diskParts {
		if diskParts[i].Part != parts[i].Part {
			return fmt.Errorf("index.jsonl 第 %d 块(%s) 与 manifest.files(%s) 不一致",
				i, diskParts[i].Part, parts[i].Part)
		}
		if diskParts[i].FirstRID != parts[i].FirstRID {
			return fmt.Errorf("index.jsonl 第 %d 块 first_rid(%q) 与 manifest.files(%q) 不一致（点读定位将错位）",
				i, diskParts[i].FirstRID, parts[i].FirstRID)
		}
		if diskParts[i].LastRID != parts[i].LastRID {
			return fmt.Errorf("index.jsonl 第 %d 块 last_rid(%q) 与 manifest.files(%q) 不一致（点读按 LastRID 二分，会全 miss）",
				i, diskParts[i].LastRID, parts[i].LastRID)
		}
		if diskParts[i].Rows != parts[i].Rows {
			return fmt.Errorf("index.jsonl 第 %d 块 rows(%d) 与 manifest.files(%d) 不一致",
				i, diskParts[i].Rows, parts[i].Rows)
		}
	}

	// 消费 index 中的 per-part sha256（压缩后字节），逐块验证盘上内容未被篡改/截断。
	for _, e := range parts {
		if strings.TrimSpace(e.SHA256) == "" {
			continue
		}
		got, err := j.partSHA256(ctx, dir, e.Part)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, e.SHA256) {
			return fmt.Errorf("part %s sha256 不一致: 盘上=%s index=%s", e.Part, got, e.SHA256)
		}
	}

	// 真正解压回读，重算 row_count 与 rid 摘要，与冻结期望集逐字节比对（sha256 的消费点）。
	rb, err := archive.ReadBack(ctx, j.store, dir, parts)
	if err != nil {
		return fmt.Errorf("ReadBack 失败: %w", err)
	}
	if rb.RowCount != m.ExpectedBodyCount {
		return fmt.Errorf("行数不一致: readback=%d expected_body_count=%d", rb.RowCount, m.ExpectedBodyCount)
	}
	if !strings.EqualFold(rb.RIDsSHA256, *m.ExpectedBodyRIDsSHA256) {
		return fmt.Errorf("rid 摘要不一致: readback=%s expected=%s", rb.RIDsSHA256, *m.ExpectedBodyRIDsSHA256)
	}

	// 盘上 manifest.json 若可读，再做一次交叉校验（字段缺失不判失败，避免与写路径耦合过紧）。
	if mf, merr := archive.LoadManifest(ctx, j.store, dir); merr == nil {
		if mf.RIDsSHA256 != "" && !strings.EqualFold(mf.RIDsSHA256, *m.ExpectedBodyRIDsSHA256) {
			return fmt.Errorf("manifest.json rid 摘要与 PG 冻结基准不一致: disk=%s pg=%s",
				mf.RIDsSHA256, *m.ExpectedBodyRIDsSHA256)
		}
		if mf.RowCount > 0 && mf.RowCount != m.ExpectedBodyCount {
			return fmt.Errorf("manifest.json 行数不一致: disk=%d pg=%d", mf.RowCount, m.ExpectedBodyCount)
		}
	} else {
		slog.Warn("archive: inspect cannot load manifest.json, index-based check only",
			"day", dStr, "error", merr)
	}

	return nil
}

// dayParts 取该天的块索引：优先用 manifest.files（v3 存 index 条目数组），
// 否则从盘上 index.jsonl 读取（兼容/容错）。
func (j *ArchiveJanitor) dayParts(ctx context.Context, m *cpmodel.TraceArchiveManifest, dir string) ([]archive.IndexEntry, error) {
	parts := decodeManifestIndexEntries(m.Files)
	if len(parts) == 0 {
		p, err := archive.LoadIndex(ctx, j.store, dir)
		if err != nil {
			return nil, fmt.Errorf("加载 index.jsonl 失败: %w", err)
		}
		parts = p
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("块索引为空（manifest.files 与 index.jsonl 均无条目）")
	}
	for i, e := range parts {
		if strings.TrimSpace(e.Part) == "" {
			return nil, fmt.Errorf("块索引第 %d 条缺 part 名", i)
		}
	}
	return parts, nil
}

// decodeManifestIndexEntries 从 manifest.files JSONB 解析 index 条目数组。
// v3 格式为 []archive.IndexEntry；旧格式（[{"name": ...}]）解析结果为空，由调用方回退到 index.jsonl。
func decodeManifestIndexEntries(files []byte) []archive.IndexEntry {
	if len(files) == 0 {
		return nil
	}
	var parts []archive.IndexEntry
	if err := json.Unmarshal(files, &parts); err != nil {
		return nil
	}
	for _, e := range parts {
		if strings.TrimSpace(e.Part) == "" {
			return nil
		}
	}
	return parts
}

// partSHA256 计算盘上某块文件（压缩后字节）的 sha256，用于与 index 条目比对。
func (j *ArchiveJanitor) partSHA256(ctx context.Context, dir, part string) (string, error) {
	key := strings.Trim(dir, "/") + "/" + strings.Trim(part, "/")
	rc, err := j.store.Get(ctx, key, archive.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("读取 part %s 失败: %w", key, err)
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", fmt.Errorf("读取 part %s 内容失败: %w", part, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// markInspectResult 落库巡检结论：通过 → ok + last_inspected_at + 清空 inspect_error；
// 失败 → suspect + inspect_error（保留上一次巡检时间，便于运维判断「曾经可信过」）。
func (j *ArchiveJanitor) markInspectResult(ctx context.Context, dStr, state string, inspectErr error) {
	var err error
	if state == cpmodel.ArchiveVerifyOK {
		err = j.db.WithContext(ctx).Exec(
			`UPDATE trace_archive_manifests
			    SET verify_state = ?, last_inspected_at = ?, inspect_error = NULL, updated_at = NOW()
			  WHERE dt = ?::date`,
			cpmodel.ArchiveVerifyOK, time.Now(), dStr).Error
	} else {
		msg := "unknown"
		if inspectErr != nil {
			msg = inspectErr.Error()
		}
		err = j.db.WithContext(ctx).Exec(
			`UPDATE trace_archive_manifests
			    SET verify_state = ?, inspect_error = ?, updated_at = NOW()
			  WHERE dt = ?::date`,
			state, msg, dStr).Error
	}
	if err != nil {
		slog.Error("archive: persist inspect result failed", "day", dStr, "state", state, "error", err)
	}
}

// ─── 置空 7 项门闸（v3 §3.4）────────────────────────────────────────────────

// nullifyGateResult 置空门闸判定结果（可单测的纯数据）。
type nullifyGateResult struct {
	OK      bool   // 全部门闸通过
	Gate    string // 未过闸编号（①..⑧ / R1..R5），OK 时为空
	Reason  string // 未过闸原因（写日志 + 审计）
	Suspect bool   // 该失败是否属于「归档数据完整性缺陷」，应把该天标记 verify_state=suspect
}

// checkNullifyGates 对候选 dt 逐条校验 v3 §3.4 的 7 项前置 + F1 的盖戳计数门闸⑧，
// 全部通过才允许置空。任一项不过都返回具体编号与原因，由调用方 slog.Error + 审计，绝不静默跳过。
//
// 评估顺序说明：条件与编号与 v3 §3.4 一一对应，但把最贵的④（逐块 sha256 + 抽样点读，需真实读盘）
// 放到最后，避免大量「因⑤/⑥/⑦不过而必然跳过」的天白付回读 I/O 成本；⑧（计数 SQL）编号与执行
// 顺序解耦，同样放最后且排在④之后。
func (j *ArchiveJanitor) checkNullifyGates(ctx context.Context, m *cpmodel.TraceArchiveManifest, now time.Time) nullifyGateResult {
	dStr := m.DT.Format("2006-01-02")
	dir := "dt=" + dStr

	// ① manifest.state == verified
	if m.State != cpmodel.ArchiveStateVerified {
		return nullifyGateResult{Gate: "①", Reason: "manifest.state=" + m.State + "（要求 verified）"}
	}
	// ② verified_at 非空且晚于 created_at（非陈旧）
	if m.VerifiedAt == nil || m.VerifiedAt.IsZero() {
		return nullifyGateResult{Gate: "②", Reason: "verified_at 为空"}
	}
	if !m.VerifiedAt.After(m.CreatedAt) {
		return nullifyGateResult{Gate: "②", Reason: fmt.Sprintf(
			"verified_at(%s) 未晚于 created_at(%s)，疑似陈旧校验",
			m.VerifiedAt.Format(time.RFC3339), m.CreatedAt.Format(time.RFC3339))}
	}
	// ③ expected_body_rids_sha256 非空（独立基准存在）
	if m.ExpectedBodyRIDsSHA256 == nil || strings.TrimSpace(*m.ExpectedBodyRIDsSHA256) == "" {
		return nullifyGateResult{Gate: "③", Reason: "expected_body_rids_sha256 为空"}
	}
	// ⑤ 该 dt 最近一次巡检 PASS 且在 freshness 窗口内
	if m.VerifyState != cpmodel.ArchiveVerifyOK {
		return nullifyGateResult{Gate: "⑤", Reason: "verify_state=" + m.VerifyState + "（要求 ok）"}
	}
	if m.LastInspectedAt == nil || m.LastInspectedAt.IsZero() {
		return nullifyGateResult{Gate: "⑤", Reason: "last_inspected_at 为空（尚未巡检）"}
	}
	if age := now.Sub(*m.LastInspectedAt); age > j.inspectFreshness() {
		return nullifyGateResult{Gate: "⑤", Reason: fmt.Sprintf(
			"巡检不新鲜：last_inspected_at=%s，距今 %s > %s",
			m.LastInspectedAt.Format(time.RFC3339), age.Truncate(time.Second), j.inspectFreshness())}
	}
	// ⑥ 离机凭证：marker 含该 dt 且时间戳新鲜
	if ok, reason := j.offhostMarkerOK(m.DT, now); !ok {
		return nullifyGateResult{Gate: "⑥", Reason: reason}
	}
	// ⑦ 磁盘水位达标
	if ok, reason := j.diskBelowWatermark(); !ok {
		return nullifyGateResult{Gate: "⑦", Reason: reason}
	}
	// ④ 归档可读性（最贵，放最后）：
	//   4a 逐块 sha256 确定性校验 —— 对【该天全部块】校验盘上 sha256 与 manifest.files 记录一致；
	//   4b 抽样回读 —— 随机 N 条 + 首尾各 1 条，全部命中且正文非空。
	// 4a 是 F3 的修复：只靠 4b 时「第 5 块损坏但 20 条抽样没落在它上面」会让门闸通过并置空正文，
	// 该天部分唯一副本已损坏而正文已离场（巡检最多 7 天后才置 suspect，为时已晚）。
	// 4a 一天只读约 15 MB（压缩后总量），成本很低，把概率保证升级为逐块确定性保证。
	if err := j.verifyAllPartsSHA256(ctx, m, dir); err != nil {
		return nullifyGateResult{Gate: "④", Reason: "逐块 sha256 校验失败: " + err.Error(), Suspect: true}
	}
	if err := j.sampleReadBack(ctx, m); err != nil {
		return nullifyGateResult{Gate: "④", Reason: err.Error()}
	}
	// ⑧ F1 在置空侧的第二道独立防线：PG 侧「已盖戳(archive_status=1)且有正文」的行数必须
	// 等于冻结的 expected_body_count。不等说明「盖戳集合 != 归档集合」，置空会漏掉仍留在 PG 的
	// 正文（与 F1 盖戳修复互为独立防线）。不等 → 不置空 + Error，等盖戳修好后自然通过。
	if err := j.verifyStampedBodyCount(ctx, m); err != nil {
		return nullifyGateResult{Gate: "⑧", Reason: err.Error()}
	}
	slog.Debug("archive: nullify gates all passed", "day", dStr)
	return nullifyGateResult{OK: true}
}

// verifyStampedBodyCount 门闸⑧（F1 第二道防线）：统计 PG 中 archive_status=1 且有正文的行数，
// 必须与该天冻结的 expected_body_count 逐值相等；查询失败同样不过闸（fail closed）。
func (j *ArchiveJanitor) verifyStampedBodyCount(ctx context.Context, m *cpmodel.TraceArchiveManifest) error {
	if j.db == nil {
		return fmt.Errorf("DB 不可用，无法校验已盖戳正文行数")
	}
	dStr := m.DT.Format("2006-01-02")
	var n int64
	if err := j.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM call_traces
		  WHERE (ts AT TIME ZONE ?)::date = ?::date
		    AND archive_status = 1
		    AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		j.cfg.tz, dStr).Scan(&n).Error; err != nil {
		return fmt.Errorf("统计已盖戳正文行数失败: %w", err)
	}
	if n != m.ExpectedBodyCount {
		return fmt.Errorf(
			"已盖戳(archive_status=1)且有正文的行数 %d != expected_body_count %d：盖戳集合与归档集合不一致，置空会漏掉正文",
			n, m.ExpectedBodyCount)
	}
	return nil
}

// verifyAllPartsSHA256 门闸④-4a（F3）：对【该天全部块】逐块计算盘上压缩字节的 sha256，
// 与 manifest.files（缺失时回退盘上 index.jsonl）里记录的 sha256 逐条比对。
//
// 任一不符 / 任一块读不出 / 任一块未记录 sha256 都返回错误（fail closed）：
// 逐块校验的意义正是「不依赖抽样命中」，读不到证据时绝不能放行不可逆的置空。
func (j *ArchiveJanitor) verifyAllPartsSHA256(ctx context.Context, m *cpmodel.TraceArchiveManifest, dir string) error {
	if j.store == nil {
		return fmt.Errorf("归档存储不可用（archive.root=%q）", j.cfg.root)
	}
	parts := decodeManifestIndexEntries(m.Files)
	if len(parts) == 0 {
		p, err := archive.LoadIndex(ctx, j.store, dir)
		if err != nil {
			return fmt.Errorf("加载 index.jsonl 失败: %w", err)
		}
		parts = p
	}
	if len(parts) == 0 {
		return fmt.Errorf("块索引为空（manifest.files 与 index.jsonl 均无条目）")
	}
	for i, e := range parts {
		if strings.TrimSpace(e.Part) == "" {
			return fmt.Errorf("块索引第 %d 条缺 part 名", i)
		}
		if strings.TrimSpace(e.SHA256) == "" {
			return fmt.Errorf("块 %s（第 %d 块）未记录 sha256，无法逐块确定性校验", e.Part, i)
		}
		got, err := j.partSHA256(ctx, dir, e.Part)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, e.SHA256) {
			return fmt.Errorf("part %s sha256 不一致: 盘上=%s index=%s", e.Part, got, e.SHA256)
		}
	}
	slog.Debug("archive: all parts sha256 verified", "day", m.DT.Format("2006-01-02"), "parts", len(parts))
	return nil
}

// sampleReadBack 抽样回读（门闸④）：随机 verify_sample_rows 条 + 首/尾各 1 条，
// 逐条经 LocalStore.ReadBody 走「_SUCCESS → index → 点读」完整链路，要求全部命中且正文非空。
func (j *ArchiveJanitor) sampleReadBack(ctx context.Context, m *cpmodel.TraceArchiveManifest) error {
	if j.store == nil {
		return fmt.Errorf("归档存储不可用（archive.root=%q），无法抽样回读", j.cfg.root)
	}
	dStr := m.DT.Format("2006-01-02")
	rids, err := j.sampleRIDs(ctx, dStr, j.cfg.verifySampleRows)
	if err != nil {
		return err
	}
	if len(rids) == 0 {
		return fmt.Errorf("该 dt 无 archive_status IN (1,2) 的行可供抽样")
	}
	ref := "dt=" + dStr + "/"
	for _, rid := range rids {
		if err := ctx.Err(); err != nil {
			return err
		}
		obj, err := j.store.ReadBody(ctx, ref, rid)
		if err != nil {
			return fmt.Errorf("抽样回读 request_id=%s 失败: %w", rid, err)
		}
		if !archiveObjectHasBody(obj) {
			return fmt.Errorf("抽样回读 request_id=%s 命中但正文为空", rid)
		}
	}
	slog.Debug("archive: sample readback passed", "day", dStr, "samples", len(rids))
	return nil
}

// sampleRIDs 取抽样 request_id：随机 N 条 + 首尾各 1 条（按 COLLATE "C" 字节序，与冻结基准同序）。
//
// 候选口径为 archive_status IN (1,2) 【且必须有正文】（F6）——覆盖「已置空但元数据仍在」的行，
// 保证幂等重跑仍可抽样；同时排除「无正文却被打上 status=1」的行（F1 修复前的历史天）。
// 若不加 body 谓词，这些无正文行会被抽出来做 ReadBody，必然 NotFound → 门闸④失败 → 整天被跳过；
// 当该天 C 序首/尾 rid 恰是这类行时【每轮必然】命中，该天永久无法置空。
func (j *ArchiveJanitor) sampleRIDs(ctx context.Context, dStr string, n int) ([]string, error) {
	if n <= 0 {
		n = 20
	}
	var randomIDs []string
	if err := j.db.WithContext(ctx).Raw(
		`SELECT request_id FROM call_traces
		  WHERE (ts AT TIME ZONE ?)::date = ?::date
		    AND archive_status IN (1, 2)
		    AND (request_body IS NOT NULL OR response_body IS NOT NULL)
		  ORDER BY random()
		  LIMIT ?`, j.cfg.tz, dStr, n).Scan(&randomIDs).Error; err != nil {
		return nil, fmt.Errorf("抽样随机查询失败: %w", err)
	}
	var edgeIDs []string
	if err := j.db.WithContext(ctx).Raw(
		`(SELECT request_id FROM call_traces
		   WHERE (ts AT TIME ZONE ?)::date = ?::date AND archive_status IN (1, 2)
		     AND (request_body IS NOT NULL OR response_body IS NOT NULL)
		   ORDER BY request_id COLLATE "C" ASC LIMIT 1)
		 UNION
		 (SELECT request_id FROM call_traces
		   WHERE (ts AT TIME ZONE ?)::date = ?::date AND archive_status IN (1, 2)
		     AND (request_body IS NOT NULL OR response_body IS NOT NULL)
		   ORDER BY request_id COLLATE "C" DESC LIMIT 1)`,
		j.cfg.tz, dStr, j.cfg.tz, dStr).Scan(&edgeIDs).Error; err != nil {
		return nil, fmt.Errorf("抽样首尾查询失败: %w", err)
	}

	seen := make(map[string]bool, len(randomIDs)+len(edgeIDs))
	out := make([]string, 0, len(randomIDs)+len(edgeIDs))
	for _, rid := range append(randomIDs, edgeIDs...) {
		if rid == "" || seen[rid] {
			continue
		}
		seen[rid] = true
		out = append(out, rid)
	}
	return out, nil
}

// archiveObjectHasBody 判断抽样命中的归档对象正文是否非空（null / 空串均视为空）。
//
// 【fail-closed 说明，F6】jsonb 字面量 null（`request_body = 'null'::jsonb`，即 SQL 层面
// IS NOT NULL 但 JSON 值为 null）与空串一样被判为「无正文」，从而拦下该天的置空。
// 这个方向是刻意为之、不得放宽：宁可把正文继续留在 PG（可读、可重试、幂等），
// 也不能在「抽样回读命中的对象看起来没有正文」时推进不可逆的置空。
func archiveObjectHasBody(o *archive.ArchiveObject) bool {
	if o == nil {
		return false
	}
	return jsonBodyNonEmpty(o.RequestBody) || jsonBodyNonEmpty(o.ResponseBody)
}

// jsonBodyNonEmpty 判断 JSON 原始字节是否为「有正文」（非空、非 null）。
func jsonBodyNonEmpty(b json.RawMessage) bool {
	s := strings.TrimSpace(string(b))
	return s != "" && s != "null"
}

// offhostMarkerOK 门闸⑥：离机 marker 必须【含该 dt】且【时间戳新鲜】（v3 §3.4⑥，修 MAJ-10）。
// require_offhost_copy=false 时放行；marker 不可读 / 不含该 dt / 陈旧均不过闸。
func (j *ArchiveJanitor) offhostMarkerOK(dt time.Time, now time.Time) (bool, string) {
	if !j.cfg.requireOffhostCopy {
		return true, ""
	}
	if strings.TrimSpace(j.cfg.offhostMarker) == "" {
		return false, "offhost_copy_marker 未配置"
	}
	st, err := os.Stat(j.cfg.offhostMarker)
	if err != nil {
		return false, fmt.Sprintf("离机 marker 不可读: %v", err)
	}
	dStr := dt.Format("2006-01-02")

	// 目录形态：目录内存在名字含该 dt 的条目，取其中最新 mtime。
	if st.IsDir() {
		entries, err := os.ReadDir(j.cfg.offhostMarker)
		if err != nil {
			return false, fmt.Sprintf("离机 marker 目录不可读: %v", err)
		}
		var newest time.Time
		for _, e := range entries {
			if !strings.Contains(e.Name(), dStr) {
				continue
			}
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
		if newest.IsZero() {
			return false, fmt.Sprintf("离机 marker 目录内无 %s 的凭证", dStr)
		}
		return markerTimeFresh(newest, dStr, now, j.offhostFreshness())
	}

	// 文件形态：内容含该 dt，且时间戳（行内时间戳或文件 mtime）新鲜。
	f, err := os.Open(j.cfg.offhostMarker)
	if err != nil {
		return false, fmt.Sprintf("离机 marker 不可读: %v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, janitorOffhostMarkerMaxRead))
	if err != nil {
		return false, fmt.Sprintf("离机 marker 读取失败: %v", err)
	}
	return offhostMarkerFresh(string(b), st.ModTime(), dStr, now, j.offhostFreshness())
}

// offhostMarkerFresh 纯函数：marker 文本必须含 dStr，且新鲜度时间戳在窗口内。
// 时间戳取「行内可解析时间戳」与「文件 mtime」的较新者，便于运维在同一文件追加多天记录。
func offhostMarkerFresh(content string, mtime time.Time, dStr string, now time.Time, freshness time.Duration) (bool, string) {
	if !strings.Contains(content, dStr) {
		return false, fmt.Sprintf("离机 marker 不含 dt %s", dStr)
	}
	newest := mtime
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, dStr) {
			continue
		}
		for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t' || r == ',' || r == ';' || r == '=' || r == '|'
		}) {
			for _, layout := range markerTimeLayouts {
				if t, err := time.Parse(layout, tok); err == nil && t.After(newest) {
					newest = t
				}
			}
		}
	}
	return markerTimeFresh(newest, dStr, now, freshness)
}

// markerTimeFresh 判定 marker 时间戳是否落在新鲜度窗口内。
func markerTimeFresh(t time.Time, dStr string, now time.Time, freshness time.Duration) (bool, string) {
	if t.IsZero() {
		return false, fmt.Sprintf("离机 marker(%s) 无有效时间戳", dStr)
	}
	if age := now.Sub(t); age > freshness {
		return false, fmt.Sprintf("离机 marker(%s) 时间戳陈旧：%s，距今 %s > %s",
			dStr, t.Format(time.RFC3339), age.Truncate(time.Second), freshness)
	}
	return true, ""
}

// diskBelowWatermark 门闸⑦：归档盘使用率必须低于 disk_watermark_percent。
// statfs 失败按不过闸处理（fail closed）——置空不可逆，证据缺失时不推进。
func (j *ArchiveJanitor) diskBelowWatermark() (bool, string) {
	if j.cfg.root == "" {
		return false, "archive.root 未配置，无法判定磁盘水位"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(j.cfg.root, &st); err != nil {
		return false, fmt.Sprintf("statfs(%s) 失败，按不过闸处理: %v", j.cfg.root, err)
	}
	used := diskUsedPercent(uint64(st.Blocks), uint64(st.Bavail), uint64(st.Bsize))
	if used >= float64(j.cfg.diskWatermarkPct) {
		return false, fmt.Sprintf("磁盘水位 %.2f%% >= %d%%", used, j.cfg.diskWatermarkPct)
	}
	return true, ""
}

// diskUsedPercent 纯函数：已用容量百分比（总块数、可用块数、块大小）。
// 异常输入（总块数为 0、可用 > 总量）一律返回 0（不过闸判定由调用方按阈值比较）。
func diskUsedPercent(totalBlocks, availBlocks, bsize uint64) float64 {
	if totalBlocks == 0 || bsize == 0 {
		return 0
	}
	total := totalBlocks * bsize
	avail := availBlocks * bsize
	if avail >= total {
		return 0
	}
	return float64(total-avail) / float64(total) * 100
}

// ─── 保留期删除（v3 §3.6）───────────────────────────────────────────────────

// RetentionSweep 保留期删除：只删「state==cleaned 且巡检 ok 且新鲜、不在保护窗、
// 且 PG 已无正文」的 dt。绝不「到期静默删除」——每次删除都留审计，每次拦截都 slog.Error。
func (j *ArchiveJanitor) RetentionSweep(ctx context.Context) {
	if j.cfg.retentionDays <= 0 {
		slog.Info("archive: retention delete disabled (retention_days<=0)")
		return
	}
	if j.store == nil {
		slog.Warn("archive: retention sweep skipped - archive store unavailable", "root", j.cfg.root)
		return
	}
	now := time.Now().In(j.loc)
	cutoff := now.AddDate(0, 0, -j.cfg.retentionDays).Format("2006-01-02")

	var ms []cpmodel.TraceArchiveManifest
	if err := j.db.WithContext(ctx).Raw(
		`SELECT * FROM trace_archive_manifests
		  WHERE state = ?
		    AND dt < ?::date
		  ORDER BY dt ASC`,
		cpmodel.ArchiveStateCleaned, cutoff).Scan(&ms).Error; err != nil {
		slog.Error("archive: retention candidate query failed", "error", err)
		return
	}
	for i := range ms {
		if err := ctx.Err(); err != nil {
			return
		}
		m := &ms[i]
		dStr := m.DT.Format("2006-01-02")
		g := j.retentionGate(ctx, m, now)
		if !g.OK {
			slog.Error("archive: retention delete BLOCKED - archive kept",
				"day", dStr, "gate", g.Gate, "reason", g.Reason)
			j.appendAudit(janitorAuditRetentionFile, map[string]any{
				"dt": dStr, "action": "blocked", "gate": g.Gate, "reason": g.Reason,
			})
			continue
		}
		files, bytes, err := j.deleteDayArchive(ctx, dStr)
		if err != nil {
			slog.Error("archive: retention delete failed", "day", dStr, "error", err)
			j.appendAudit(janitorAuditRetentionFile, map[string]any{
				"dt": dStr, "action": "failed", "reason": err.Error(),
			})
			continue
		}
		slog.Info("archive: retention deleted archive day",
			"day", dStr, "files", files, "bytes", bytes,
			"retention_days", j.cfg.retentionDays,
			"verify_state", m.VerifyState, "last_inspected_at", m.LastInspectedAt)
		j.appendAudit(janitorAuditRetentionFile, map[string]any{
			"dt": dStr, "action": "deleted", "files": files, "bytes": bytes,
			"retention_days": j.cfg.retentionDays,
			"audit":          "auto-by-janitor", "verify_state": m.VerifyState,
			"last_inspected_at": m.LastInspectedAt, "cleaned_at": m.CleanedAt,
		})
	}
}

// retentionGate 保留期删除前置（v3 §3.6）：state==cleaned + 巡检 ok 且新鲜 +
// 不在保护窗（最近 hot_window_days 天）+ PG 已无正文（防删掉唯一副本）+
// R5 盘上索引可用（index.jsonl 可读且非空、_SUCCESS 存在，F2 缺陷3：否则删掉即不可读）。
func (j *ArchiveJanitor) retentionGate(ctx context.Context, m *cpmodel.TraceArchiveManifest, now time.Time) nullifyGateResult {
	dStr := m.DT.Format("2006-01-02")

	if m.State != cpmodel.ArchiveStateCleaned {
		return nullifyGateResult{Gate: "R1", Reason: "manifest.state=" + m.State + "（要求 cleaned）"}
	}
	if m.VerifyState != cpmodel.ArchiveVerifyOK {
		return nullifyGateResult{Gate: "R2", Reason: "verify_state=" + m.VerifyState + "（要求 ok）"}
	}
	if m.LastInspectedAt == nil || m.LastInspectedAt.IsZero() {
		return nullifyGateResult{Gate: "R2", Reason: "last_inspected_at 为空（尚未巡检）"}
	}
	if age := now.Sub(*m.LastInspectedAt); age > j.inspectFreshness() {
		return nullifyGateResult{Gate: "R2", Reason: fmt.Sprintf(
			"巡检不新鲜：last_inspected_at=%s，距今 %s > %s",
			m.LastInspectedAt.Format(time.RFC3339), age.Truncate(time.Second), j.inspectFreshness())}
	}
	protectSince := now.AddDate(0, 0, -j.cfg.hotWindowDays).Format("2006-01-02")
	if dStr >= protectSince {
		return nullifyGateResult{Gate: "R3", Reason: fmt.Sprintf(
			"处于保护窗内：dt=%s >= %s（hot_window_days=%d）",
			dStr, protectSince, j.cfg.hotWindowDays)}
	}
	n, err := j.countLiveBodies(ctx, dStr)
	if err != nil {
		return nullifyGateResult{Gate: "R4", Reason: "统计 PG 剩余正文失败: " + err.Error()}
	}
	if n > 0 {
		return nullifyGateResult{Gate: "R4", Reason: fmt.Sprintf(
			"PG 仍有 %d 行正文（置空未完成），删除归档会失去唯一副本", n)}
	}
	// R5（F2 缺陷3）：删除会移除【唯一可恢复副本】，删除前必须确认盘上索引可用——
	// index.jsonl 可读且条目非空、_SUCCESS 存在。任一不满足即拒绝删除（fail closed）：
	// 索引不可读 = 删掉就再也点读不出来，比保留占用空间严重得多。
	rdir := "dt=" + dStr
	rdirPath := filepath.Join(j.cfg.root, rdir)
	if _, statErr := os.Stat(rdirPath); statErr != nil {
		if os.IsNotExist(statErr) {
			// 目录已不存在（此前删除或人工处理）：无副本可删，deleteDayArchive 会 no-op。
			return nullifyGateResult{OK: true}
		}
		return nullifyGateResult{Gate: "R5", Reason: fmt.Sprintf("归档目录不可访问: %v", statErr)}
	}
	ok, err := j.store.Exists(ctx, rdir+"/"+archive.SuccessMarkerName)
	if err != nil {
		return nullifyGateResult{Gate: "R5", Reason: fmt.Sprintf("检查 _SUCCESS 失败: %v", err)}
	}
	if !ok {
		return nullifyGateResult{Gate: "R5", Reason: "_SUCCESS 缺失，该天归档不可信，拒绝删除"}
	}
	idx, err := archive.LoadIndex(ctx, j.store, rdir)
	if err != nil {
		return nullifyGateResult{Gate: "R5", Reason: fmt.Sprintf("index.jsonl 不可读，删除后无法点读，拒绝删除: %v", err)}
	}
	if len(idx) == 0 {
		return nullifyGateResult{Gate: "R5", Reason: "index.jsonl 条目为空，删除后无法点读，拒绝删除"}
	}
	return nullifyGateResult{OK: true}
}

// countLiveBodies 统计某 dt 在 PG 中仍带正文的行数。
func (j *ArchiveJanitor) countLiveBodies(ctx context.Context, dStr string) (int64, error) {
	var n int64
	if err := j.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM call_traces
		  WHERE (ts AT TIME ZONE ?)::date = ?::date
		    AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		j.cfg.tz, dStr).Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// deleteDayArchive 删除某天归档目录下的全部对象（分页列全 → 逐个 Delete 留痕 → RemoveAll 清残留）。
// 目录已不存在（此前删除失败后人工处理，或重复扫描）时直接返回 0/0/nil。
func (j *ArchiveJanitor) deleteDayArchive(ctx context.Context, dStr string) (int, int64, error) {
	if _, err := time.Parse("2006-01-02", dStr); err != nil {
		return 0, 0, fmt.Errorf("非法 dt %q: %w", dStr, err)
	}
	dir := "dt=" + dStr
	dirPath := filepath.Join(j.cfg.root, dir)
	if _, err := os.Stat(dirPath); err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("归档目录不可访问: %w", err)
	}

	var files int
	var bytes int64
	marker := ""
	for {
		res, err := j.store.List(ctx, dir, archive.ListOptions{MaxKeys: 1000, Marker: marker})
		if err != nil {
			return files, bytes, fmt.Errorf("列出归档对象失败: %w", err)
		}
		for _, o := range res.Objects {
			if derr := j.store.Delete(ctx, o.Key); derr != nil && !isArchiveNotFound(derr) {
				return files, bytes, fmt.Errorf("删除 %s 失败: %w", o.Key, derr)
			}
			files++
			bytes += o.Size
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextMarker
	}
	// 清除 .part/.tmp/.spool 等残留，确保目录整体离场（不做静默部分删除）。
	if err := os.RemoveAll(dirPath); err != nil {
		return files, bytes, fmt.Errorf("清理归档目录失败: %w", err)
	}
	// 旧版盘上 manifest 副本（写路径遗留），尽力删除。
	_ = os.Remove(filepath.Join(j.cfg.root, "_manifest", "manifest-"+dStr+".json"))
	return files, bytes, nil
}
