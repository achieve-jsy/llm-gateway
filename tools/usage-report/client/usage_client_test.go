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
