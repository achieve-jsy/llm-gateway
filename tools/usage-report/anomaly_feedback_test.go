package main

// anomaly_feedback_test.go —— 方案 X（真反馈闭环）report 侧装配/读回的单测。
// 覆盖：① 上报/读回 client 方法（httptest 桩服务，验证体/query）；② applyAnomalyFeedback 读回
// 按 (rule, window_start分钟, member_id) 覆盖 AnomalyHit.Status；③ 接口不可用（404）静默降级为默认态。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

// shTest 构造 Asia/Shanghai 本地时间（与 shLoc 口径一致）。
func shTest(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, shLoc)
}

// TestClient_PostReportAnomalies 上报：POST /usage/report/anomalies，验证 JSON 体与服务端返回透传。
func TestClient_PostReportAnomalies(t *testing.T) {
	mux := http.NewServeMux()
	const respJSON = `[{"id":"rec1","status":"待确认"}]`
	mux.HandleFunc("/usage/report/anomalies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json, got %q", ct)
		}
		var body []client.AnomalyUpsert
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body) != 1 {
			t.Fatalf("expected 1 item, got %d", len(body))
		}
		if body[0].Rule != "A2" || body[0].ScopeID != "dept_x" || body[0].WindowStart == "" {
			t.Fatalf("unexpected item: %+v", body[0])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(respJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	res, err := cli.PostReportAnomalies(context.Background(), []client.AnomalyUpsert{
		{ScopeLevel: "department", ScopeID: "dept_x", Rule: "A2", RuleVersion: "v1", WindowStart: "2026-08-28T08:58:00+08:00", MemberID: "user_7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "rec1" || res[0].Status != "待确认" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestClient_FetchAnomalyStatus 读回：GET /usage/report/anomaly-status，验证 query 参数与状态行透传。
func TestClient_FetchAnomalyStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/usage/report/anomaly-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Query().Get("scope_level"); got != "department" {
			t.Fatalf("scope_level=%q", got)
		}
		if got := r.URL.Query().Get("scope_id"); got != "dept_x" {
			t.Fatalf("scope_id=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"rule":"A2","window_start":"2026-08-28T08:58:00+08:00","member_id":"user_7","status":"误报"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	rows, err := cli.FetchAnomalyStatus(context.Background(), "department", "dept_x", "2026-08-21", "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Status != "误报" || rows[0].Rule != "A2" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestAnomalyMinuteTS 窗口起点归一：带秒/纳秒的时间→分钟精度 +08:00 RFC3339。
func TestAnomalyMinuteTS(t *testing.T) {
	ts := anomalyMinuteTS(shTest(2026, 8, 28, 8, 58, 45)) // 08:58:45 → 08:58
	if ts != "2026-08-28T08:58:00+08:00" {
		t.Fatalf("minuteTS=%q", ts)
	}
}

// TestAnomalyKey 三元组 key：rule|window_start|member_id。
func TestAnomalyKey(t *testing.T) {
	k := anomalyKey("A2", "2026-08-28T08:58:00+08:00", "user_7")
	if k != "A2|2026-08-28T08:58:00+08:00|user_7" {
		t.Fatalf("key=%q", k)
	}
}

// TestApplyAnomalyFeedback_Readback 读回覆盖：hit 的 Status 被读回三元组覆盖为「误报」。
func TestApplyAnomalyFeedback_Readback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/usage/report/anomalies", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"rec1","status":"待确认"}]`)) // 上报成功
	})
	mux.HandleFunc("/usage/report/anomaly-status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"rule":"A2","window_start":"2026-08-28T08:58:00+08:00","member_id":"user_7","status":"误报"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	rep := &report.Report{Judgment: report.Judgment{Anomalies: []report.AnomalyHit{
		{Rule: "A2", RuleVersion: "v1", WindowStart: shTest(2026, 8, 28, 8, 58, 45), Dimensions: map[string]string{"user_id": "user_7"}, Status: ""},
	}}}
	// Hit 默认 true（derive 侧）；isHit=true 走读回匹配分支。
	rep.Judgment.Anomalies[0].Hit = true
	applyAnomalyFeedback(context.Background(), cli, Config{Level: "department", From: "2026-08-21", To: "2026-08-28", Debug: true}, report.Scope{ID: "dept_x"}, rep)
	if rep.Judgment.Anomalies[0].Status != "误报" {
		t.Fatalf("expected status 误报, got %q", rep.Judgment.Anomalies[0].Status)
	}
}

// TestApplyAnomalyFeedback_Degrade404 接口不可用：读回 404 → 静默降级，Status 保持默认「待确认」。
func TestApplyAnomalyFeedback_Degrade404(t *testing.T) {
	mux := http.NewServeMux()
	// 接口未建：两个端点都 404。
	mux.HandleFunc("/usage/report/anomalies", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("/usage/report/anomaly-status", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	rep := &report.Report{Judgment: report.Judgment{Anomalies: []report.AnomalyHit{
		{Rule: "A1", Hit: true, WindowStart: shTest(2026, 8, 28, 8, 58, 45), Status: "待确认"},
	}}}
	applyAnomalyFeedback(context.Background(), cli, Config{Level: "department", From: "2026-08-21", To: "2026-08-28"}, report.Scope{ID: "dept_x"}, rep)
	if rep.Judgment.Anomalies[0].Status != "待确认" {
		t.Fatalf("expected degrade to 待确认, got %q", rep.Judgment.Anomalies[0].Status)
	}
}

