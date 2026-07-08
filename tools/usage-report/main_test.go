package main

import "testing"

func TestInclusiveDays(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		want           int
	}{
		{"single day", "2026-08-01", "2026-08-01", 1},
		{"week", "2026-08-01", "2026-08-07", 7},
		{"cross month and leap day", "2024-02-28", "2024-03-01", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inclusiveDays(tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("inclusiveDays=%d want %d", got, tt.want)
			}
		})
	}
}

func TestInclusiveDaysRejectsInvalidRange(t *testing.T) {
	for _, tt := range [][2]string{{"bad", "2026-08-01"}, {"2026-08-02", "2026-08-01"}} {
		if _, err := inclusiveDays(tt[0], tt[1]); err == nil {
			t.Fatalf("expected error for %q..%q", tt[0], tt[1])
		}
	}
}

func TestISOWeekOf(t *testing.T) {
	for _, tt := range []struct {
		date, want string
	}{
		{"2026-08-10", "2026-W33"}, // 周一 → W33（模板目标形态）
		{"2026-08-16", "2026-W33"}, // 周日仍属 W33（ISO 周日归前一周）
		{"2026-08-17", "2026-W34"},
		{"bad-date", ""}, // 非法格式 → 空串，不 panic
	} {
		if got := isoWeekOf(tt.date); got != tt.want {
			t.Fatalf("isoWeekOf(%q)=%q want %q", tt.date, got, tt.want)
		}
	}
}

func TestPeriodLabelIsoWeek(t *testing.T) {
	// 有 ISO 周：period + ISO 周 + 日期区间（标题/Vicky 周期行展示）。
	if got := periodLabel("week", "2026-08-10", "2026-08-16", "2026-W33"); got != "week 2026-W33 2026-08-10~2026-08-16" {
		t.Fatalf("periodLabel(iso)=%q", got)
	}
	// 无 ISO 周：回退原形态。
	if got := periodLabel("week", "2026-08-10", "2026-08-16", ""); got != "week 2026-08-10~2026-08-16" {
		t.Fatalf("periodLabel(noiso)=%q", got)
	}
}
