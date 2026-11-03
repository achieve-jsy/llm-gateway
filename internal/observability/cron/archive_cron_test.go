// archive_cron_test.go —— WS-F：归档生命周期调度器（契约 §7.1：cron 只编排；
// WriteEnabled 控制整条写链路，与点读开关 read.enabled 无关）。
package cron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
)

// TestNewCronForSchedule_FieldCount 验证 newCronForSchedule 按字段数选择解析器：
// 5 字段默认、6 字段 WithSeconds，均能成功 AddFunc（不因解析器与表达式不匹配而报错）。
func TestNewCronForSchedule_FieldCount(t *testing.T) {
	cases := []string{
		"0 2 * * *",     // 5 字段（minute hour dom month dow）
		"0 0 2 * * *",   // 6 字段（含 seconds）
		"*/5 * * * * *", // 6 字段
	}
	for _, sched := range cases {
		cr := newCronForSchedule(sched)
		if _, err := cr.AddFunc(sched, func() {}); err != nil {
			t.Errorf("AddFunc(%q) failed: %v", sched, err)
		}
	}
}

// TestNewCronForSchedule_Regression 回归【经典 cron bug】：5 字段表达式配 WithSeconds 解析器必须失败，
// 而 newCronForSchedule 正确处理让它成功（旧实现 cron.New(WithSeconds()) + "0 3 * * *" 会 AddFunc 报错、Start 未执行）。
func TestNewCronForSchedule_Regression(t *testing.T) {
	// 旧实现（bug）：WithSeconds(6 字段解析器) 配 5 字段表达式 → AddFunc 报错。
	buggy := cron.New(cron.WithSeconds())
	if _, err := buggy.AddFunc("0 3 * * *", func() {}); err == nil {
		t.Fatal("expected AddFunc mismatch error for 5-field schedule with WithSeconds parser")
	}

	// 修复后：newCronForSchedule 自动匹配 → AddFunc 成功。
	fixed := newCronForSchedule("0 3 * * *")
	if _, err := fixed.AddFunc("0 3 * * *", func() {}); err != nil {
		t.Fatalf("5-field schedule with default parser should succeed: %v", err)
	}
}

// ─── 假实现（cron 只依赖能力接口，测试完全脱离 DB/Redis）──────────────────────

// fakeArchiver 只实现 ArchiveRunner：走 RunCycle 退化路径。
type fakeArchiver struct {
	enabled  bool
	schedule string
	calls    int
	err      error
	rec      *[]string // 调用顺序记录
}

func (f *fakeArchiver) Enabled() bool    { return f.enabled }
func (f *fakeArchiver) Schedule() string { return f.schedule }

func (f *fakeArchiver) RunCycle(context.Context) error {
	f.calls++
	if f.rec != nil {
		*f.rec = append(*f.rec, "archiver")
	}
	return f.err
}

// fakeDayArchiver 额外实现 ArchiveDayLister：走「按天 + days_per_night 节流」编排路径。
type fakeDayArchiver struct {
	fakeArchiver
	pending   []time.Time
	err       error
	days      []time.Time
	dayErrors map[string]error
}

func (f *fakeDayArchiver) PendingDays(context.Context, time.Time) ([]time.Time, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pending, nil
}

func (f *fakeDayArchiver) ArchiveDay(_ context.Context, d time.Time, _ bool) (bool, error) {
	f.days = append(f.days, d)
	if f.rec != nil {
		*f.rec = append(*f.rec, "day:"+d.Format("2006-01-02"))
	}
	if err, ok := f.dayErrors[d.Format("2006-01-02")]; ok {
		return false, err
	}
	return true, nil
}

type fakeCycleRunner struct {
	enabled bool
	calls   int
	err     error
	rec     *[]string
	name    string
}

func (f *fakeCycleRunner) Enabled() bool { return f.enabled }
func (f *fakeCycleRunner) RunCycle(context.Context) error {
	f.calls++
	if f.rec != nil {
		*f.rec = append(*f.rec, f.name)
	}
	return f.err
}

func testArchiveCfg() cpconfig.ArchiveConfig {
	return cpconfig.ArchiveConfig{
		WriteEnabled:    true,
		ReadEnabled:     true,
		TZ:              "Asia/Shanghai",
		CronSchedule:    "0 3 * * *",
		HotWindowDays:   30,
		ClosureLagHours: 24,
		DaysPerNight:    2,
	}
}

// TestArchiveCron_DisabledNotScheduled 覆盖 N10 的调度侧：write=false 时 cron 不调度，
// RunOnce 也不执行任何一轮（点读开关 read=true 与调度无关）。
func TestArchiveCron_DisabledNotScheduled(t *testing.T) {
	ctx := context.Background()
	var order []string
	arch := &fakeArchiver{enabled: false, schedule: "0 3 * * *", rec: &order}
	jan := &fakeCycleRunner{enabled: true, name: "janitor", rec: &order}
	cfg := testArchiveCfg()
	cfg.WriteEnabled = false

	c := NewArchiveCron(arch, jan, cfg)
	if err := c.Start(ctx); err != nil {
		t.Fatalf("write=false 时 Start 必须是 no-op 且不报错: %v", err)
	}
	defer c.Stop()
	if c.cron != nil {
		t.Fatal("write=false 时不得创建调度器（否则关写回滚后仍在跑归档）")
	}
	if err := c.RunOnce(ctx); err != nil {
		t.Fatalf("write=false 时 RunOnce: %v", err)
	}
	if arch.calls != 0 || jan.calls != 0 {
		t.Fatalf("write=false 时不得执行任何一轮: archiver=%d janitor=%d", arch.calls, jan.calls)
	}
	if len(order) != 0 {
		t.Fatalf("write=false 时不得调用任何组件: %v", order)
	}
}

// TestArchiveCron_EmptyScheduleFailsLoudly 断言：启用但表达式为空时必须显式报错（不静默不调度）。
func TestArchiveCron_EmptyScheduleFailsLoudly(t *testing.T) {
	arch := &fakeArchiver{enabled: true, schedule: ""}
	c := NewArchiveCron(arch, nil, cpconfig.ArchiveConfig{WriteEnabled: true})
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("空 cron 表达式必须报错，而不是静默跳过")
	}
}

// TestArchiveCron_RunOnceOrderAndErrors 覆盖单轮编排顺序（归档 → janitor）与错误传播。
func TestArchiveCron_RunOnceOrderAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("顺序与正常返回", func(t *testing.T) {
		var order []string
		arch := &fakeArchiver{enabled: true, schedule: "0 3 * * *", rec: &order}
		jan := &fakeCycleRunner{enabled: true, name: "janitor", rec: &order}
		c := NewArchiveCron(arch, jan, testArchiveCfg())
		if err := c.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if len(order) != 2 || order[0] != "archiver" || order[1] != "janitor" {
			t.Fatalf("编排顺序错误: %v", order)
		}
	})

	t.Run("归档失败仍跑 janitor 并返回首个错误", func(t *testing.T) {
		boom := errors.New("archive cycle failed")
		var order []string
		arch := &fakeArchiver{enabled: true, schedule: "0 3 * * *", err: boom, rec: &order}
		jan := &fakeCycleRunner{enabled: true, name: "janitor", rec: &order}
		c := NewArchiveCron(arch, jan, testArchiveCfg())
		if err := c.RunOnce(ctx); !errors.Is(err, boom) {
			t.Fatalf("RunOnce err = %v want %v", err, boom)
		}
		if len(order) != 2 || order[1] != "janitor" {
			t.Fatalf("归档失败不得阻断 janitor（其门闸自会拦截）: %v", order)
		}
	})

	t.Run("janitor 未启用或未注入时跳过", func(t *testing.T) {
		arch := &fakeArchiver{enabled: true, schedule: "0 3 * * *"}
		c := NewArchiveCron(arch, &fakeCycleRunner{enabled: false, name: "janitor"}, testArchiveCfg())
		if err := c.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		c2 := NewArchiveCron(arch, nil, testArchiveCfg())
		if err := c2.RunOnce(ctx); err != nil {
			t.Fatalf("nil janitor 必须被容忍: %v", err)
		}
	})
}

// TestArchiveCron_DaysPerNightThrottle 覆盖按天编排路径（服务实现 ArchiveDayLister 时）：
// 水位线前的待归档天必须按 days_per_night 截断（防一夜打满整段存量）。
func TestArchiveCron_DaysPerNightThrottle(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	pending := []time.Time{base, base.AddDate(0, 0, 1), base.AddDate(0, 0, 2), base.AddDate(0, 0, 3)}
	arch := &fakeDayArchiver{fakeArchiver: fakeArchiver{enabled: true, schedule: "0 3 * * *"}, pending: pending}
	cfg := testArchiveCfg()
	cfg.DaysPerNight = 2

	c := NewArchiveCron(arch, nil, cfg)
	if err := c.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(arch.days) != 2 {
		t.Fatalf("按天归档天数 = %d want 2（days_per_night 节流失效）", len(arch.days))
	}
	if !arch.days[0].Equal(pending[0]) || !arch.days[1].Equal(pending[1]) {
		t.Fatalf("应按升序取最早的 days_per_night 天，got %v", arch.days)
	}
}

// TestArchiveCron_DayErrorDoesNotStopRest 单天失败不得中断后续天，且错误要上抛。
func TestArchiveCron_DayErrorDoesNotStopRest(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	pending := []time.Time{base, base.AddDate(0, 0, 1)}
	boom := errors.New("day failed")
	arch := &fakeDayArchiver{
		fakeArchiver: fakeArchiver{enabled: true, schedule: "0 3 * * *"},
		pending:      pending,
		dayErrors:    map[string]error{"2026-06-01": boom},
	}
	c := NewArchiveCron(arch, nil, testArchiveCfg())
	err := c.RunOnce(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v want %v", err, boom)
	}
	if len(arch.days) != 2 {
		t.Fatalf("单天失败后必须继续处理后续天，got %v", arch.days)
	}
}

// TestArchiveCron_Waterline 覆盖水位线口径：hot_window_days 与 closure_lag_hours 取更早（更严格）者。
func TestArchiveCron_Waterline(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, loc)

	// hot_window_days=30 → 6/20 零点回退 29 天 = 5/22；closure_lag=24h → 6/19 零点。
	// 取更早的 hot 分支。
	c := NewArchiveCron(&fakeArchiver{enabled: true, schedule: "0 3 * * *"}, nil,
		cpconfig.ArchiveConfig{TZ: "Asia/Shanghai", HotWindowDays: 30, ClosureLagHours: 24, DaysPerNight: 1})
	want := time.Date(2026, 5, 22, 0, 0, 0, 0, loc)
	if got := c.waterline(now); !got.Equal(want) {
		t.Fatalf("waterline = %s want %s", got, want)
	}

	// hot_window_days<=0 → 只按 closure_lag_hours 兜底：6/19 零点。
	c2 := NewArchiveCron(&fakeArchiver{enabled: true, schedule: "0 3 * * *"}, nil,
		cpconfig.ArchiveConfig{TZ: "Asia/Shanghai", HotWindowDays: 0, ClosureLagHours: 24, DaysPerNight: 1})
	want2 := time.Date(2026, 6, 19, 0, 0, 0, 0, loc)
	if got := c2.waterline(now); !got.Equal(want2) {
		t.Fatalf("waterline(no hot window) = %s want %s", got, want2)
	}
}

// TestArchiveCron_ScheduleAndThrottleDefaults 覆盖表达式来源与 days_per_night 兜底。
func TestArchiveCron_ScheduleAndThrottleDefaults(t *testing.T) {
	cfg := testArchiveCfg()
	cfg.CronSchedule = "0 4 * * *"
	if c := NewArchiveCron(&fakeArchiver{enabled: true, schedule: "30 2 * * *"}, nil, cfg); c.schedule != "30 2 * * *" {
		t.Fatalf("schedule = %q want archiver 的值", c.schedule)
	}
	if c := NewArchiveCron(&fakeArchiver{enabled: true, schedule: ""}, nil, cfg); c.schedule != "0 4 * * *" {
		t.Fatalf("schedule = %q want 配置值", c.schedule)
	}
	if c := NewArchiveCron(&fakeArchiver{enabled: true, schedule: "0 4 * * *"}, nil, cpconfig.ArchiveConfig{}); c.daysPerNight != 1 {
		t.Fatalf("daysPerNight = %d want 1（缺省保守）", c.daysPerNight)
	}
}

// TestArchiveCron_StartSchedulesWhenEnabled 断言启用时真的挂上调度项（可 Stop）。
func TestArchiveCron_StartSchedulesWhenEnabled(t *testing.T) {
	arch := &fakeArchiver{enabled: true, schedule: "0 3 * * *"}
	c := NewArchiveCron(arch, nil, testArchiveCfg())
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()
	if c.cron == nil {
		t.Fatal("启用时必须创建调度器")
	}
	if len(c.cron.Entries()) != 1 {
		t.Fatalf("调度项数 = %d want 1", len(c.cron.Entries()))
	}
}
