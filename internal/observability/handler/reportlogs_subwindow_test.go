package handler

import (
	"strings"
	"testing"
)

// TestReportLogsTimeWindow_Subwindow 断言 start_ts/end_ts 成对提供时走 timestamptz 子窗半开区间，
// 优先于 start/end 日粒度（2026-08-28 A2 成本燃烧窗口 08:58~09:13，命中日单日 15 万条，
// 子窗只拉窗口内少量行）。
func TestReportLogsTimeWindow_Subwindow(t *testing.T) {
	startStr := "2026-08-28"
	endStr := "2026-08-28"
	startTS := "2026-08-28T08:58:00+08:00"
	endTS := "2026-08-28T09:13:00+08:00"

	cond, args, err := reportLogsTimeWindow(startStr, endStr, startTS, endTS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond != "call_logs.ts >= ?::timestamptz AND call_logs.ts < ?::timestamptz" {
		t.Fatalf("subwindow cond mismatch:\n got %q", cond)
	}
	if len(args) != 2 || args[0] != startTS || args[1] != endTS {
		t.Fatalf("subwindow args mismatch: want %q,%q got %#v", startTS, endTS, args)
	}
}

// TestReportLogsTimeWindow_PreferSubwindowOverDay：当 start_ts/end_ts 与 start/end 同时传，
// 必须以子窗为准（子窗优先）。
func TestReportLogsTimeWindow_PreferSubwindowOverDay(t *testing.T) {
	cond, _, err := reportLogsTimeWindow("2026-08-27", "2026-08-28", "2026-08-28T08:58:00+08:00", "2026-08-28T09:13:00+08:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cond, "::timestamptz") {
		t.Fatalf("expected subwindow (timestamptz) to take priority, got %q", cond)
	}
}

// TestReportLogsTimeWindow_DayGranularity：不传 start_ts/end_ts 时保持既有日粒度行为（向后兼容）。
func TestReportLogsTimeWindow_DayGranularity(t *testing.T) {
	cond, args, err := reportLogsTimeWindow("2026-08-21", "2026-08-28", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond != "call_logs.ts >= ?::date AND call_logs.ts < (?::date + INTERVAL '1 day')" {
		t.Fatalf("day-granularity cond mismatch:\n got %q", cond)
	}
	if len(args) != 2 || args[0] != "2026-08-21" || args[1] != "2026-08-28" {
		t.Fatalf("day-granularity args mismatch: got %#v", args)
	}
}

// TestReportLogsTimeWindow_OnlyOneTS：只传其一报错。
func TestReportLogsTimeWindow_OnlyOneTS(t *testing.T) {
	if _, _, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "2026-08-28T08:58:00+08:00", ""); err == nil {
		t.Fatalf("expected error when only start_ts provided")
	}
	if _, _, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "", "2026-08-28T09:13:00+08:00"); err == nil {
		t.Fatalf("expected error when only end_ts provided")
	}
}

// TestReportLogsTimeWindow_InvalidTS：非法时间串报错。
func TestReportLogsTimeWindow_InvalidTS(t *testing.T) {
	if _, _, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "not-a-time", "2026-08-28T09:13:00+08:00"); err == nil {
		t.Fatalf("expected error for invalid start_ts")
	}
	if _, _, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "2026-08-28T08:58:00+08:00", "also-invalid"); err == nil {
		t.Fatalf("expected error for invalid end_ts")
	}
}

// TestReportLogsTimeWindow_StartNotBeforeEnd：start_ts 不早于 end_ts 报错。
func TestReportLogsTimeWindow_StartNotBeforeEnd(t *testing.T) {
	if _, _, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "2026-08-28T09:13:00+08:00", "2026-08-28T08:58:00+08:00"); err == nil {
		t.Fatalf("expected error when start_ts >= end_ts")
	}
}

// TestReportLogsTimeWindow_SpaceLayout：兼容 2006-01-02 15:04:05 裸格式。
func TestReportLogsTimeWindow_SpaceLayout(t *testing.T) {
	cond, args, err := reportLogsTimeWindow("2026-08-28", "2026-08-28", "2026-08-28 08:58:00", "2026-08-28 09:13:00")
	if err != nil {
		t.Fatalf("unexpected error for space layout: %v", err)
	}
	if !strings.Contains(cond, "::timestamptz") {
		t.Fatalf("expected timestamptz subwindow for space layout, got %q", cond)
	}
	if len(args) != 2 || args[0] != "2026-08-28 08:58:00" || args[1] != "2026-08-28 09:13:00" {
		t.Fatalf("space layout args mismatch: got %#v", args)
	}
}

// TestParseReportLogsTS：解析成功/失败路径。
func TestParseReportLogsTS(t *testing.T) {
	if _, err := parseReportLogsTS("2026-08-28T08:58:00+08:00"); err != nil {
		t.Fatalf("RFC3339 should parse: %v", err)
	}
	if _, err := parseReportLogsTS("2026-08-28 08:58:00"); err != nil {
		t.Fatalf("space layout should parse: %v", err)
	}
	if _, err := parseReportLogsTS("garbage"); err == nil {
		t.Fatalf("garbage should not parse")
	}
}
