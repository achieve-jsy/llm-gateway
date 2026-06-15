package usage_report_client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	usageclient "github.com/example/llm-gateway/tools/usage-report/client"
)

func TestFetchLogsBeyondTwoHundredPages(t *testing.T) {
	const total = 40001
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Fatal(err)
		}
		start := (page - 1) * 200
		count := 200
		if remaining := total - start; remaining < count {
			count = remaining
		}
		rows := make([]usageclient.LogRow, count)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"rows": rows, "total": total, "page": page, "page_size": 200,
		})
	}))
	defer server.Close()

	client := usageclient.New(server.URL, "token", usageclient.WithMaxLogs(50000))
	rows, err := client.FetchLogs(context.Background(), "2026-01-01", "2026-01-31", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != total {
		t.Fatalf("got %d rows, want %d", len(rows), total)
	}
	if got := requests.Load(); got != 201 {
		t.Fatalf("got %d requests, want 201", got)
	}
}

func TestFetchLogsRejectsConfiguredLimitInsteadOfTruncating(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"rows": make([]usageclient.LogRow, 200), "total": 201, "page": 1, "page_size": 200,
		})
	}))
	defer server.Close()

	client := usageclient.New(server.URL, "token", usageclient.WithMaxLogs(200))
	rows, err := client.FetchLogs(context.Background(), "2026-01-01", "2026-01-31", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "max logs 200") || !strings.Contains(err.Error(), "total 201") {
		t.Fatalf("got rows=%v err=%v, want explicit max logs error", rows, err)
	}
	if rows != nil {
		t.Fatalf("got %d partial rows, want nil", len(rows))
	}
}

func TestFetchLogsRejectsIncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		rows := make([]usageclient.LogRow, 200)
		if page == 2 {
			rows = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"rows": rows, "total": 300, "page": page, "page_size": 200,
		})
	}))
	defer server.Close()

	client := usageclient.New(server.URL, "token", usageclient.WithMaxLogs(500))
	rows, err := client.FetchLogs(context.Background(), "2026-01-01", "2026-01-31", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "incomplete usage logs") || !strings.Contains(err.Error(), "fetched 200 of 300") {
		t.Fatalf("got rows=%v err=%v, want incomplete error", rows, err)
	}
	if rows != nil {
		t.Fatalf("got %d partial rows, want nil", len(rows))
	}
}
