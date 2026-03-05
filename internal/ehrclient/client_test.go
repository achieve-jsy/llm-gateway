package ehrclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(baseURL string) Config {
	cfg := DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.Token = "test-token"
	cfg.RateLimit = 0
	return cfg
}

func TestClient_ListOrganizations_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"orgId":"org-1","orgName":"TestOrg"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].OrgID != "org-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClient_401_Once_Then_200(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"orgId":"org-1"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
}

func TestClient_401_Twice_GivesUp(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	cfg := testConfig(ts.URL)
	cfg.MaxRetries = 3
	c := NewClient(cfg)
	_, err := c.ListOrganizations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *ErrHTTP in chain, got %T: %v", err, err)
	}
	if httpErr.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", httpErr.StatusCode)
	}
}

func TestClient_5xx_Retries_Then_OK(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"server error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer ts.Close()

	cfg := testConfig(ts.URL)
	cfg.MaxRetries = 3
	c := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.ListOrganizations(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := callCount.Load()
	if total != 4 {
		t.Fatalf("expected 4 calls, got %d", total)
	}
}

func TestClient_5xx_MaxRetries(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer ts.Close()

	cfg := testConfig(ts.URL)
	cfg.MaxRetries = 3
	c := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.ListOrganizations(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("expected 'max retries exceeded', got: %v", err)
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *ErrHTTP in chain, got %T", err)
	}
	total := callCount.Load()
	if total != 4 {
		t.Fatalf("expected 4 calls (MaxRetries=3 + 1 initial), got %d", total)
	}
}

func TestClient_429_Retries(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer ts.Close()

	cfg := testConfig(ts.URL)
	cfg.MaxRetries = 3
	c := NewClient(cfg)
	_, err := c.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_403_NoRetry(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	_, err := c.ListOrganizations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	total := callCount.Load()
	if total != 1 {
		t.Fatalf("expected 1 call (no retry for 403), got %d", total)
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 403 {
		t.Fatalf("expected 403 ErrHTTP, got: %v", err)
	}
}

func TestClient_NoToken_ReturnsErrToken(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testConfig(ts.URL)
	cfg.Token = ""
	c := NewClient(cfg)
	_, err := c.ListOrganizations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token not configured") {
		t.Fatalf("expected 'token not configured' error, got: %v", err)
	}
}

func TestClient_ContextCancel(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewClient(testConfig(ts.URL))
	_, err := c.ListOrganizations(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected context error, got: %v", err)
	}
}

func TestClient_ListCompanies_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/resource/v1/companies" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("orgId") != "org-1" {
			t.Fatalf("expected orgId=org-1, got %s", r.URL.Query().Get("orgId"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"companyId":"c1","companyName":"AlphaCo","lastModifyDate":"2025-01-01"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListCompanies(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].CompanyID != "c1" || resp.Items[0].CompanyName != "AlphaCo" || resp.Items[0].LastModifyDate != "2025-01-01" {
		t.Fatalf("unexpected item: %+v", resp.Items[0])
	}
}

func TestClient_ListDepartments_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"departmentId":"d1","departmentName":"Engineering","leaderName":"Alice","managerName":"Bob"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListDepartments(context.Background(), "org-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].DepartmentID != "d1" || resp.Items[0].LeaderName != "Alice" || resp.Items[0].ManagerName != "Bob" {
		t.Fatalf("unexpected item: %+v", resp.Items[0])
	}
}

func TestClient_ListEmployees_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"employeeId":"e1","passport":"P001","appointName":"John","email":"john@example.com","companyId":"c1","departmentId":"d1","positionName":"Engineer"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListEmployees(context.Background(), "org-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	emp := resp.Items[0]
	if emp.EmployeeID != "e1" || emp.Passport != "P001" || emp.AppointName != "John" || emp.Email != "john@example.com" || emp.PositionName != "Engineer" {
		t.Fatalf("unexpected item: %+v", emp)
	}
}

func TestClient_ListEmployeesBatch_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/resource/v1/employees/batch" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		got := r.URL.Query().Get("employeeIds")
		if got != "e1,e2" {
			t.Fatalf("expected employeeIds=e1,e2, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"employeeId":"e1","appointName":"Alice"},{"employeeId":"e2","appointName":"Bob"}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListEmployeesBatch(context.Background(), "org-1", []string{"e1", "e2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].EmployeeID != "e1" || resp.Items[1].EmployeeID != "e2" {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
}

func TestClient_ListPositions_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[{"id":"p1","name":"Root","type":1,"namePath":"/Root","items":[{"id":"p2","name":"Child","type":2,"namePath":"/Root/Child"}]}]}`))
	}))
	defer ts.Close()

	c := NewClient(testConfig(ts.URL))
	resp, err := c.ListPositions(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 top-level item, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "p1" || resp.Items[0].Name != "Root" {
		t.Fatalf("unexpected root item: %+v", resp.Items[0])
	}
	if len(resp.Items[0].Items) != 1 {
		t.Fatalf("expected 1 nested item, got %d", len(resp.Items[0].Items))
	}
	if resp.Items[0].Items[0].ID != "p2" || resp.Items[0].Items[0].Name != "Child" {
		t.Fatalf("unexpected nested item: %+v", resp.Items[0].Items[0])
	}
}

func TestEncodeQuery(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		got := encodeQuery(nil)
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
		got = encodeQuery(map[string]string{})
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("one_param", func(t *testing.T) {
		got := encodeQuery(map[string]string{"k": "v"})
		if got != "k=v" {
			t.Fatalf("expected k=v, got %q", got)
		}
	})

	t.Run("multiple_params", func(t *testing.T) {
		got := encodeQuery(map[string]string{"a": "1", "b": "2"})
		if !strings.Contains(got, "a=1") || !strings.Contains(got, "b=2") {
			t.Fatalf("expected both a=1 and b=2, got %q", got)
		}
	})

	t.Run("special_chars", func(t *testing.T) {
		got := encodeQuery(map[string]string{"k": "a=b&c"})
		if !strings.Contains(got, "%3D") || !strings.Contains(got, "%26") {
			t.Fatalf("expected encoded special chars, got %q", got)
		}
	})

	t.Run("skips_empty_values", func(t *testing.T) {
		got := encodeQuery(map[string]string{"a": "1", "b": ""})
		if strings.Contains(got, "b=") {
			t.Fatalf("expected b= to be skipped, got %q", got)
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	t.Run("no_header", func(t *testing.T) {
		h := http.Header{}
		d := parseRetryAfter(h)
		if d != 0 {
			t.Fatalf("expected 0, got %v", d)
		}
	})

	t.Run("numeric_seconds", func(t *testing.T) {
		h := http.Header{}
		h.Set("Retry-After", "5")
		d := parseRetryAfter(h)
		if d != 5*time.Second {
			t.Fatalf("expected 5s, got %v", d)
		}
	})

	t.Run("invalid_value", func(t *testing.T) {
		h := http.Header{}
		h.Set("Retry-After", "not-a-number")
		d := parseRetryAfter(h)
		if d != 0 {
			t.Fatalf("expected 0 for invalid value, got %v", d)
		}
	})

	t.Run("rfc1123_future_date", func(t *testing.T) {
		h := http.Header{}
		future := time.Now().Add(10 * time.Second).UTC().Format(time.RFC1123)
		h.Set("Retry-After", future)
		d := parseRetryAfter(h)
		if d <= 0 {
			t.Fatalf("expected > 0 for future date, got %v", d)
		}
	})
}

func TestWithSliceParam(t *testing.T) {
	t.Parallel()

	t.Run("empty_vals", func(t *testing.T) {
		base := map[string]string{"orgId": "org-1"}
		out := withSliceParam(base, "ids", nil)
		if _, ok := out["ids"]; ok {
			t.Fatalf("expected ids key to be absent for nil vals")
		}
		if out["orgId"] != "org-1" {
			t.Fatalf("expected orgId preserved, got %q", out["orgId"])
		}
	})

	t.Run("empty_slice", func(t *testing.T) {
		out := withSliceParam(map[string]string{}, "ids", []string{})
		if _, ok := out["ids"]; ok {
			t.Fatalf("expected ids key to be absent for empty slice")
		}
	})

	t.Run("non_empty_vals", func(t *testing.T) {
		base := map[string]string{"orgId": "org-1"}
		out := withSliceParam(base, "ids", []string{"a", "b", "c"})
		if out["ids"] != "a,b,c" {
			t.Fatalf("expected a,b,c, got %q", out["ids"])
		}
		if out["orgId"] != "org-1" {
			t.Fatalf("expected orgId preserved, got %q", out["orgId"])
		}
	})
}
