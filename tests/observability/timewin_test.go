package observability_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/service"
)

// 锁定"日期窗口 → 半开时间戳区间"的语义。
//
// 线上曾用 ts::date / date(ts) 切天：既按**服务器时区（UTC）**切，漏掉北京时间
// 0–8 点的调用，又让 idx_call_logs_ts 失效。这里保证两天边界语义不被改回去。

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	return loc
}

// 天边界必须是本地 00:00 到次日 00:00，而不是 UTC 的。
func TestTSWholeDayRange_UsesBusinessTimezone(t *testing.T) {
	loc := shanghai(t)
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)

	lo, hi := service.TSWholeDayRange(day)

	if !lo.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("lo = %v, want 10-01 00:00 +08", lo)
	}
	if !hi.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, loc)) {
		t.Errorf("hi = %v, want 10-02 00:00 +08", hi)
	}
	// 本地零点换算成 UTC 是前一天 16:00
	if got := lo.UTC().Hour(); got != 16 {
		t.Errorf("lo.UTC().Hour() = %d, want 16", got)
	}
}

// 两端都按整天，hi 为开区间：to 当天 23:59:59 在内，次日 00:00 在外。
func TestTSWholeDays_InclusiveBothEndsOpenAtEnd(t *testing.T) {
	loc := shanghai(t)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 10, 7, 0, 0, 0, 0, loc)

	lo, hi := service.TSWholeDays(from, to)

	lastMoment := time.Date(2026, 10, 7, 23, 59, 59, 0, loc)
	if lastMoment.Before(lo) || !lastMoment.Before(hi) {
		t.Errorf("10-07 23:59:59 应在 [lo,hi) 内，lo=%v hi=%v", lo, hi)
	}
	if after := time.Date(2026, 10, 8, 0, 0, 0, 0, loc); after.Before(hi) {
		t.Errorf("10-08 00:00 不应在内，hi=%v", hi)
	}
	// 时段中间的 08:00 也要在内
	mid := time.Date(2026, 10, 3, 8, 0, 0, 0, loc)
	if mid.Before(lo) || !mid.Before(hi) {
		t.Errorf("10-03 08:00 应在窗口内")
	}
}

// 带时刻的输入只取日历日，与历史 ts::date BETWEEN 语义一致。
func TestTSWholeDays_IgnoresTimeOfDay(t *testing.T) {
	loc := shanghai(t)
	from := time.Date(2026, 10, 1, 15, 30, 0, 0, loc)
	to := time.Date(2026, 10, 3, 9, 15, 0, 0, loc)

	lo, hi := service.TSWholeDays(from, to)

	if !lo.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("lo = %v, want 10-01 00:00", lo)
	}
	if !hi.Equal(time.Date(2026, 10, 4, 0, 0, 0, 0, loc)) {
		t.Errorf("hi = %v, want 10-04 00:00", hi)
	}
}

// ParseDayWindow 是 handler 侧的入口，解析失败不能 panic。
func TestParseDayWindow_HandlesBadInput(t *testing.T) {
	lo, hi := service.ParseDayWindow("not-a-date", "2026-10-02")
	if lo.IsZero() || hi.IsZero() {
		t.Fatalf("解析失败应返回零值而非 panic：lo=%v hi=%v", lo, hi)
	}
}

// 天分组表达式必须把 Asia/Shanghai 写进去，否则又回到按服务器时区切天。
func TestTSKeySQL_UsesBusinessTimezone(t *testing.T) {
	expr := service.TSKeySQL("")
	if expr == "" {
		t.Fatal("TSKeySQL 返回空")
	}
	if !contains(expr, "Asia/Shanghai") && !contains(expr, service.BusinessLocation().String()) {
		t.Errorf("TSKeySQL 未包含业务时区: %s", expr)
	}
	if contains(expr, "::date") {
		t.Errorf("TSKeySQL 不应退化为 ts::date（会废掉索引）: %s", expr)
	}
	if got := service.TSKeySQL("cl"); !contains(got, "cl.ts") {
		t.Errorf("指定表别名时应用 cl.ts，got %s", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
