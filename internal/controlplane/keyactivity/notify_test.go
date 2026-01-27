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
