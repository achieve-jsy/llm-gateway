package keyactivity_test

import (
	"strings"
	"testing"
	"time"

	ka "github.com/example/llm-gateway/internal/controlplane/keyactivity"
)

func TestFormatNotification_E1_NewSource(t *testing.T) {
	ts := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	title, body := ka.FormatNotification(ka.EventTypeNewSource, "MyKey", "curl/8.0", "10.0.0.1", ts, "")
	if title != "Key 新设备使用提醒" {
		t.Errorf("E1 title = %q", title)
	}
	for _, want := range []string{"MyKey", "curl/8.0", "10.0.0.1", "2026-06-24"} {
		if !strings.Contains(body, want) {
			t.Errorf("E1 body should contain %q: %s", want, body)
		}
	}
}

func TestFormatNotification_E2_FirstUse(t *testing.T) {
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	title, body := ka.FormatNotification(ka.EventTypeFirstUse, "BrandNew", "curl/7.0", "192.0.2.10", ts, "")
	if title != "Key 首次使用" {
		t.Errorf("E2 title = %q", title)
	}
	if !strings.Contains(body, "BrandNew") || !strings.Contains(body, "首次被使用") {
		t.Errorf("E2 body unexpected: %s", body)
	}
}

func TestFormatNotification_E3_Wakeup(t *testing.T) {
	ts := time.Now()
	detail := `{"dormant_days":45}`
	title, body := ka.FormatNotification(ka.EventTypeWakeup, "Sleeper", "curl/8", "10.0.0.2", ts, detail)
	if title != "Key 重新启用提醒" {
		t.Errorf("E3 title = %q", title)
	}
	if !strings.Contains(body, "45 天") {
		t.Errorf("E3 body should include dormant days: %s", body)
	}
}

func TestFormatNotification_E4_Spike(t *testing.T) {
	ts := time.Now()
	detail := `{"count":250,"mean":30.5}`
	title, body := ka.FormatNotification(ka.EventTypeSpike, "HotKey", "", "", ts, detail)
	if title != "Key 用量异常提醒" {
		t.Errorf("E4 title = %q", title)
	}
	if !strings.Contains(body, "250 次") {
		t.Errorf("E4 body should include count: %s", body)
	}
	if !strings.Contains(body, "30.5") {
		t.Errorf("E4 body should include mean baseline: %s", body)
	}
}

// TestFormatNotification_UsesShanghaiTime 验证邮件正文时间按北京时间展示。
// call_logs.ts 落库是 UTC（数据库连接时区 UTC），若直接 Format 会早 8 小时。
func TestFormatNotification_UsesShanghaiTime(t *testing.T) {
	// 真实场景：北京时间 2026-09-16 06:34:53 的请求，call_logs.ts 存的是 2026-09-15 22:34:53Z
	ts := time.Date(2026, 9, 15, 22, 34, 53, 0, time.UTC)

	for _, eventType := range []string{
		ka.EventTypeNewSource, ka.EventTypeFirstUse, ka.EventTypeWakeup,
	} {
		detail := ""
		if eventType == ka.EventTypeWakeup {
			detail = `{"dormant_days":45}`
		}
		_, body := ka.FormatNotification(eventType, "MyKey", "curl/8.0", "10.0.0.1", ts, detail)

		if !strings.Contains(body, "2026-09-16 06:34:53") {
			t.Errorf("%s: body should show Beijing time 2026-09-16 06:34:53, got: %s", eventType, body)
		}
		if strings.Contains(body, "2026-09-15 22:34:53") {
			t.Errorf("%s: body must NOT show raw UTC time 2026-09-15 22:34:53, got: %s", eventType, body)
		}
	}
}

// TestFormatNotification_UTCInputAlreadyShanghaiZone 已带东八区偏移的时间不应被二次平移。
func TestFormatNotification_UTCInputAlreadyShanghaiZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	ts := time.Date(2026, 9, 16, 6, 34, 53, 0, shanghai)
	_, body := ka.FormatNotification(ka.EventTypeFirstUse, "MyKey", "curl/8.0", "10.0.0.1", ts, "")
	if !strings.Contains(body, "2026-09-16 06:34:53") {
		t.Errorf("body should keep 2026-09-16 06:34:53, got: %s", body)
	}
}

// TestWakeupDedupKey_BeijingDateBoundary 验证唤醒去重键按北京日期切分（UTC 日边界会差 8 小时）。
func TestWakeupDedupKey_BeijingDateBoundary(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}

	tests := []struct {
		name string
		ts   time.Time
		want string
	}{
		{
			name: "UTC 日边界前 1 秒（北京 09-15 23:59:59）",
			ts:   time.Date(2026, 9, 15, 15, 59, 59, 0, time.UTC),
			want: "2026-09-15",
		},
		{
			name: "UTC 日边界整点（北京 09-16 00:00:00）",
			ts:   time.Date(2026, 9, 15, 16, 0, 0, 0, time.UTC),
			want: "2026-09-16",
		},
		{
			name: "UTC 日边界后（北京 09-16 06:34:53，即 call_logs 的 22:34:53Z）",
			ts:   time.Date(2026, 9, 15, 22, 34, 53, 0, time.UTC),
			want: "2026-09-16",
		},
		{
			name: "北京时间下午（同日）",
			ts:   time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC),
			want: "2026-09-16",
		},
		{
			name: "已是东八区的时间不做二次平移",
			ts:   time.Date(2026, 9, 16, 1, 0, 0, 0, shanghai),
			want: "2026-09-16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ka.WakeupDedupKey(tt.ts); got != tt.want {
				t.Errorf("WakeupDedupKey(%s) = %q, want %q", tt.ts.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

// TestWakeupDedupKey_SameBeijingDayAcrossUTCDays 同一北京日内的两次唤醒必须共用一个去重键
// （否则会重复发提醒），而北京日的两侧必须得到不同键（否则会漏报）。
func TestWakeupDedupKey_SameBeijingDayAcrossUTCDays(t *testing.T) {
	// 北京 2026-09-16 00:30（= UTC 09-15 16:30）与北京 2026-09-16 23:00（= UTC 09-16 15:00）
	early := time.Date(2026, 9, 15, 16, 30, 0, 0, time.UTC)
	late := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	if ka.WakeupDedupKey(early) != ka.WakeupDedupKey(late) {
		t.Errorf("same Beijing day should share dedup key: %q vs %q",
			ka.WakeupDedupKey(early), ka.WakeupDedupKey(late))
	}
	// 北京 2026-09-16 23:00 的下一秒（= UTC 09-16 15:00:01）仍在同一天；
	// 用北京 2026-09-17 00:00（= UTC 09-16 16:00）验证跨日换键。
	nextDay := time.Date(2026, 9, 16, 16, 0, 0, 0, time.UTC)
	if ka.WakeupDedupKey(late) == ka.WakeupDedupKey(nextDay) {
		t.Errorf("different Beijing day should change dedup key: both %q", ka.WakeupDedupKey(late))
	}
	// 直击缺陷：按 UTC 取日期会把这两个时间分到同一天
	if early.Format("2006-01-02") == late.Format("2006-01-02") {
		t.Fatal("test premise broken: early/late should differ in UTC date")
	}
}

func TestEventLevel(t *testing.T) {
	cases := []struct {
		event string
		want  string
	}{
		{ka.EventTypeFirstUse, "info"},
		{ka.EventTypeNewSource, "warning"},
		{ka.EventTypeWakeup, "warning"},
		{ka.EventTypeSpike, "warning"},
	}
	for _, c := range cases {
		got := ka.EventLevel(c.event)
		if got != c.want {
			t.Errorf("EventLevel(%s) = %q, want %q", c.event, got, c.want)
		}
	}
}

func TestParseDormantDays(t *testing.T) {
	tests := []struct {
		detail string
		want   int
	}{
		{`{"dormant_days":30}`, 30},
		{`{"dormant_days":0}`, 0},
		{`{}`, 0},
		{`invalid`, 0},
		{``, 0},
	}
	for _, tt := range tests {
		got := ka.ParseDormantDays(tt.detail)
		if got != tt.want {
			t.Errorf("ParseDormantDays(%q) = %d, want %d", tt.detail, got, tt.want)
		}
	}
}

func TestParseSpikeDetail(t *testing.T) {
	count, mean := ka.ParseSpikeDetail(`{"count":100,"mean":30.5}`)
	if count != 100 || mean != 30.5 {
		t.Errorf("ParseSpikeDetail invalid: got count=%d mean=%f", count, mean)
	}
	if c, m := ka.ParseSpikeDetail(""); c != 0 || m != 0 {
		t.Error("empty detail should return zeros")
	}
	if c, m := ka.ParseSpikeDetail("{bad json"); c != 0 || m != 0 {
		t.Error("invalid JSON should return zeros")
	}
}
