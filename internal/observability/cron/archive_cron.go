package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	robfigcron "github.com/robfig/cron/v3"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
)

const (
	// archiveFallbackTZ 归档时区解析失败时的兜底（与 observability 侧 archiveSettings 一致）。
	archiveFallbackTZ = "Asia/Shanghai"
	// archiveDateLayout 归档日口径（目录 dt=YYYY-MM-DD）。
	archiveDateLayout = "2006-01-02"
)

// ArchiveRunner 归档服务在调度层的能力面。
// 契约 §7.1：cron 只做编排——决定「什么时候跑、跑几轮」，不实现导出 / 校验 / 置空任何业务逻辑。
type ArchiveRunner interface {
	// Enabled 归档写路径是否启用（= archive.write.enabled）。
	Enabled() bool
	// Schedule 返回 cron 表达式（archive.cron_schedule），为空时由配置兜底。
	Schedule() string
	// RunCycle 单轮归档；由服务内部按配置自行计算水位线与节流（按天能力缺失时的退化路径）。
	RunCycle(ctx context.Context) error
}

// ArchiveDayLister 归档服务的「按天」能力面（可选扩展点）。
//
// 契约 §7：cron 需「按 cron_schedule 与 hot_window_days 驱动 daily 归档」并「尊重 days_per_night 节流」。
// 服务实现本接口后，水位线（hot_window_days / closure_lag_hours）与每晚天数上限均在调度层显式编排、
// 可观测；未实现时退化为 ArchiveRunner.RunCycle（由服务内部按同一配置完成窗口计算与节流，
// 当前 ArchiveService 即此路径）。
//
// 若后续要在调度层显式按天编排，服务侧需补上（另 ArchiveDay 已存在）：
//
//	// PendingDays 返回 before 之前、尚未 verified/cleaned 的归档日（升序），由服务按 tz 计算「已关闭的天」。
//	func (s *ArchiveService) PendingDays(ctx context.Context, before time.Time) ([]time.Time, error)
type ArchiveDayLister interface {
	PendingDays(ctx context.Context, before time.Time) ([]time.Time, error)
	ArchiveDay(ctx context.Context, d time.Time, force bool) (bool, error)
}

// CycleRunner 单轮后台组件（janitor：补归档 → 轮转巡检 → 置空 → 保留期删除）。
type CycleRunner interface {
	Enabled() bool
	RunCycle(ctx context.Context) error
}

// ArchiveCron 归档生命周期调度器：按 archive.cron_schedule 触发「归档 → janitor 全链」。
//
// 开关归属（契约 §7.1）：本调度器整体挂在 archive.write.enabled 之下，
// 与点读开关 archive.read.enabled 无关——回滚（关写）时点读仍可用。
// 轮转巡检与保留期删除位于 janitor 的单轮编排内（Phase B / Phase D），因此同样受该开关控制。
//
// 使用 robfig/cron，按表达式字段数自动匹配解析器（5 字段默认 / 6 字段 WithSeconds），
// 避免【经典 bug】cron.New(WithSeconds) 配 5 字段表达式导致 AddFunc 报错、Start 未执行。
type ArchiveCron struct {
	archiver ArchiveRunner
	janitor  CycleRunner

	cron     *robfigcron.Cron
	schedule string
	entryID  robfigcron.EntryID

	// 编排参数（来自 Config.Archive）：水位线与每晚天数节流，用于日志可观测与按天编排。
	hotWindowDays   int
	closureLagHours int
	daysPerNight    int
	loc             *time.Location
}

// NewArchiveCron 创建归档调度器。janitor 可为 nil（能力未装配时仅跑归档）。
func NewArchiveCron(archiver ArchiveRunner, janitor CycleRunner, cfg cpconfig.ArchiveConfig) *ArchiveCron {
	schedule := cfg.CronSchedule
	if archiver != nil {
		if s := archiver.Schedule(); s != "" {
			schedule = s
		}
	}

	loc, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		slog.Warn("archive_cron: invalid tz, fallback",
			"tz", cfg.TZ, "fallback", archiveFallbackTZ, "error", err)
		loc, _ = time.LoadLocation(archiveFallbackTZ)
	}

	// days_per_night 缺省/非法时按 1 天保守处理，避免「0 = 不限制」把整段存量在一晚打满。
	daysPerNight := cfg.DaysPerNight
	if daysPerNight <= 0 {
		daysPerNight = 1
	}

	return &ArchiveCron{
		archiver:        archiver,
		janitor:         janitor,
		schedule:        schedule,
		hotWindowDays:   cfg.HotWindowDays,
		closureLagHours: cfg.ClosureLagHours,
		daysPerNight:    daysPerNight,
		loc:             loc,
	}
}

// newCronForSchedule 依据字段数选择解析器：6 字段 → WithSeconds，否则默认 5 字段。
func newCronForSchedule(schedule string) *robfigcron.Cron {
	if len(strings.Fields(schedule)) == 6 {
		return robfigcron.New(robfigcron.WithSeconds())
	}
	return robfigcron.New()
}

// Start 启动 cron；归档写路径未启用时不调度（契约 §7.1：WriteEnabled 控制归档 cron）。
// 返回 error 以暴露 AddFunc/Start 失败（自洽修复），不静默跳过。
func (c *ArchiveCron) Start(ctx context.Context) error {
	if c.archiver == nil || !c.archiver.Enabled() {
		slog.Info("archive_cron: disabled, not scheduled")
		return nil
	}
	if c.schedule == "" {
		return fmt.Errorf("archive_cron: empty schedule")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.cron = newCronForSchedule(c.schedule)
	var err error
	c.entryID, err = c.cron.AddFunc(c.schedule, func() {
		c.runCycle(ctx)
	})
	if err != nil {
		return fmt.Errorf("archive cron AddFunc (schedule=%q): %w", c.schedule, err)
	}
	c.cron.Start()
	slog.Info("archive_cron_started",
		"schedule", c.schedule,
		"entry_id", c.entryID,
		"hot_window_days", c.hotWindowDays,
		"closure_lag_hours", c.closureLagHours,
		"days_per_night", c.daysPerNight,
	)
	return nil
}

// Stop 停止调度器（保留已定义能力，可再次 Start）。
func (c *ArchiveCron) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}

// RunOnce 立即执行一次（测试 / 手动触发 / 启动补跑）。
func (c *ArchiveCron) RunOnce(ctx context.Context) error {
	if c.archiver == nil || !c.archiver.Enabled() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.runCycle(ctx)
}

// runCycle 单轮编排：归档 → janitor（补归档 → 巡检 → 置空 → 保留期删除）。
// 归档失败不阻断 janitor（janitor 的 7 项门闸会自行保证「没有合格归档就不置空」）；
// leader lock 由各服务内部获取，cron 不感知多实例。
func (c *ArchiveCron) runCycle(ctx context.Context) error {
	slog.Info("archive_cron: cycle start")
	var firstErr error

	if err := c.runArchive(ctx); err != nil {
		slog.Error("archive_cron: archiver cycle error", "error", err)
		firstErr = err
	}
	if err := c.runCycleRunner(ctx, "janitor", c.janitor); err != nil {
		slog.Error("archive_cron: janitor cycle error", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	slog.Info("archive_cron: cycle done")
	return firstErr
}

// runCycleRunner 执行可选组件的一轮；未注入或未启用时跳过。
func (c *ArchiveCron) runCycleRunner(ctx context.Context, name string, r CycleRunner) error {
	if r == nil || !r.Enabled() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slog.Info("archive_cron: component cycle start", "component", name)
	return r.RunCycle(ctx)
}

// runArchive 归档编排。
//   - 服务实现 ArchiveDayLister 时：按 hot_window_days / closure_lag_hours 计算水位线，
//     取水位线前尚未终态的归档天，按 days_per_night 截断后逐天驱动（窗口与节流显式落在调度层）。
//   - 未实现时（当前 ArchiveService）：调用 RunCycle，由服务内部按同一配置完成窗口计算与节流。
func (c *ArchiveCron) runArchive(ctx context.Context) error {
	lister, ok := c.archiver.(ArchiveDayLister)
	if !ok {
		return c.archiver.RunCycle(ctx)
	}

	waterline := c.waterline(time.Now())
	days, err := lister.PendingDays(ctx, waterline)
	if err != nil {
		return fmt.Errorf("pending archive days: %w", err)
	}
	if len(days) == 0 {
		slog.Info("archive_cron: no pending day below waterline",
			"waterline", waterline.Format(archiveDateLayout))
		return nil
	}
	if len(days) > c.daysPerNight {
		slog.Info("archive_cron: throttle pending days",
			"pending", len(days), "this_cycle", c.daysPerNight, "days_per_night", c.daysPerNight)
		days = days[:c.daysPerNight]
	}

	var firstErr error
	for _, d := range days {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := lister.ArchiveDay(ctx, d, false); err != nil {
			slog.Error("archive_cron: archive day failed", "day", d.Format(archiveDateLayout), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// waterline 归档水位线（不含）：只有严格早于该时刻的「已关闭天」才进入归档。
// 同时满足两个配置条件，取两者中更早（更严格）的那个：
//   - hot_window_days：最近 N 天正文仍留在 PG（热窗口），不入归档；
//   - closure_lag_hours：当天结束后再等 H 小时才认为该天已闭合。
//
// hot_window_days <= 0 时视为未配置，只按 closure_lag_hours 兜底。
func (c *ArchiveCron) waterline(now time.Time) time.Time {
	n := now.In(c.loc)
	dayStart := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, c.loc)
	w := dayStart.Add(-time.Duration(c.closureLagHours) * time.Hour)
	if c.hotWindowDays > 0 {
		// 保留今天在内的 hot_window_days 天：水位线落在「热窗口的第一天」零点。
		if hot := dayStart.AddDate(0, 0, -(c.hotWindowDays - 1)); hot.Before(w) {
			w = hot
		}
	}
	return w
}
