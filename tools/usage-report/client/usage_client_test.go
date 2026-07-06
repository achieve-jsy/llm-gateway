package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchUserMetricsPaginatesAggregates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/dashboard/users" || r.URL.Query().Get("from") != "2026-08-01" || r.URL.Query().Get("to") != "2026-08-07" {
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		rows := []DashboardUserMetric{{UserID: "u1", TotalTokens: 1000, EstCostUSD: 1}}
		if page == "2" {
			rows = []DashboardUserMetric{{UserID: "u2", TotalTokens: 2000, EstCostUSD: 3}}
		}
		_ = json.NewEncoder(w).Encode(dashboardUsersResponse{Rows: rows, Page: 1, PageSize: 1, TotalCount: 2})
	}))
	defer server.Close()

	rows, err := New(server.URL, "token").FetchUserMetrics(context.Background(), "2026-08-01", "2026-08-07", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].UserID != "u2" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestFetchUserMetricsScopeFilters 验证非空 dept_id/studio_id 作为服务端过滤参数下发。
func TestFetchUserMetricsScopeFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/dashboard/users" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("dept_id") != "d1" || r.URL.Query().Get("studio_id") != "s1" {
			t.Fatalf("expected dept_id/studio_id filters, got %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(dashboardUsersResponse{})
	}))
	defer server.Close()

	rows, err := New(server.URL, "token").FetchUserMetrics(context.Background(), "2026-08-01", "2026-08-07", "d1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %+v", rows)
	}
}

// TestFetchServiceKeys 验证 /usage/report/service-keys 的路径/查询参数，以及 ServiceKeyRow 对新字段的解析。
func TestFetchServiceKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/report/service-keys" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("start") != "2026-08-01" || q.Get("end") != "2026-08-07" {
			t.Fatalf("missing start/end: %s", r.URL.RawQuery)
		}
		if q.Get("dept_id") != "d1" || q.Get("studio_id") != "s1" {
			t.Fatalf("expected dept_id/studio_id filters, got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(struct {
			Rows []ServiceKeyRow `json:"rows"`
		}{Rows: []ServiceKeyRow{{
			KeyID: "key1", KeyName: "svc", OwnerName: "owner", ReqCount: 10,
			SuccessCount: 9, ErrorCount: 1, TotalTokens: 1000, CostUSD: 0.5,
			ModelCount: 2, ActiveDays: 3, StartDate: "2026-08-01", EndDate: "2026-08-07",
		}}})
	}))
	defer server.Close()

	rows, err := New(server.URL, "token").FetchServiceKeys(context.Background(), "2026-08-01", "2026-08-07", "d1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].KeyID != "key1" || rows[0].OwnerName != "owner" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestFetchServiceKeysNoScope 验证 deptID/studioID 为空时不下发过滤参数。
func TestFetchServiceKeysNoScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dept_id") != "" || r.URL.Query().Get("studio_id") != "" {
			t.Fatalf("unexpected scope filters: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(struct {
			Rows []ServiceKeyRow `json:"rows"`
		}{})
	}))
	defer server.Close()

	if _, err := New(server.URL, "token").FetchServiceKeys(context.Background(), "2026-08-01", "2026-08-07", "", ""); err != nil {
		t.Fatal(err)
	}
}

// TestFetchUserMetricsAll 验证 FetchUserMetricsAll 走全公司路径（不下发 dept_id/studio_id）。
func TestFetchUserMetricsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/dashboard/users" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("dept_id") != "" || r.URL.Query().Get("studio_id") != "" {
			t.Fatalf("expected all-company (no scope filters), got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(dashboardUsersResponse{Rows: []DashboardUserMetric{{UserID: "u1", TotalTokens: 1000}}, Page: 1, PageSize: 100, TotalCount: 1})
	}))
	defer server.Close()

	rows, err := New(server.URL, "token").FetchUserMetricsAll(context.Background(), "2026-08-01", "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UserID != "u1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestMemberAggRowNewFields 验证新耗时/TTFT/上下文字段被 JSON 反序列化并透传到 ToMemberAgg。
func TestMemberAggRowNewFields(t *testing.T) {
	var r MemberAggRow
	if err := json.Unmarshal([]byte(`{
		"user_id":"u1","req_count":10,"cost_usd":1.2,
		"avg_duration_ms":12.5,"p95_duration_ms":45.7,
		"avg_first_token_ms":3.2,"avg_input_ctx_tokens":1024.5
	}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.AvgDurationMs != 12.5 || r.P95DurationMs != 45.7 || r.AvgFirstTokenMs != 3.2 || r.AvgInputCtxTokens != 1024.5 {
		t.Fatalf("unexpected parsed fields: %+v", r)
	}
	ma := r.ToMemberAgg()
	if ma.AvgDurationMs != 12.5 || ma.P95DurationMs != 45.7 || ma.AvgFirstTokenMs != 3.2 || ma.AvgInputCtxTokens != 1024.5 {
		t.Fatalf("unexpected ToMemberAgg mapping: %+v", ma)
	}
}

// TestFetchLogsSubWindow 验证子窗拉取发 start_ts/end_ts（timestamptz 半开区间），且绝不发日粒度 start/end。
// 覆盖 be-eng t10 服务端子窗端口的客户端入口，并锁定「命中窗口按精准时间拉取而非整天」的契约。
func TestFetchLogsSubWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage/report/logs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("start_ts") != "2026-08-28T08:58:00+08:00" || q.Get("end_ts") != "2026-08-28T09:13:00+08:00" {
			t.Fatalf("expected start_ts/end_ts sub-window, got %s", r.URL.RawQuery)
		}
		// 子窗模式不允许混用日粒度 start/end。
		if q.Get("start") != "" || q.Get("end") != "" {
			t.Fatalf("sub-window must not send day start/end: %s", r.URL.RawQuery)
		}
		if q.Get("dept_id") != "d1" || q.Get("studio_id") != "s1" {
			t.Fatalf("expected dept_id/studio_id filters, got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(logsResponse{
			Rows:  []LogRow{{RequestID: "r1", KeyType: "member", UserID: "u1", TotalTokens: 1000}},
			Total: 1, Page: 1, PageSize: 1,
		})
	}))
	defer server.Close()

	rows, err := New(server.URL, "token").FetchLogsSubWindow(context.Background(), "2026-08-28T08:58:00+08:00", "2026-08-28T09:13:00+08:00", "member", "", "d1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestID != "r1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestFetchLogsDayGranularity 验证日粒度 FetchLogs 仍发 start/end（向后兼容），不发 start_ts/end_ts。
func TestFetchLogsDayGranularity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("start") != "2026-08-28" || q.Get("end") != "2026-08-29" {
			t.Fatalf("expected day start/end, got %s", r.URL.RawQuery)
		}
		if q.Get("start_ts") != "" || q.Get("end_ts") != "" {
			t.Fatalf("day mode must not send start_ts/end_ts: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(logsResponse{Rows: []LogRow{{RequestID: "r1"}}, Total: 1, Page: 1, PageSize: 1})
	}))
	defer server.Close()

	if _, err := New(server.URL, "token").FetchLogs(context.Background(), "2026-08-28", "2026-08-29", "member", "", "d1", "s1"); err != nil {
		t.Fatal(err)
	}
}
