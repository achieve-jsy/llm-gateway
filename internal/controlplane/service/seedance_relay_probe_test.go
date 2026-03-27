package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeSeedanceRelayConnectivity_404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/seedance/api/v3/contents/generations/tasks/__gateway_health_probe__") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("auth=%s", got)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
	}))
	defer srv.Close()

	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "sk-test")
	if !res.Success {
		t.Fatalf("want success, got %#v", res)
	}
	if !strings.Contains(res.Message, "connectivity+auth") {
		t.Fatalf("message should declare scope: %s", res.Message)
	}
}

func TestProbeSeedanceRelayConnectivity_401IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "bad")
	if res.Success {
		t.Fatal("401 must fail")
	}
}

func TestProbeSeedanceRelayConnectivity_503IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "sk")
	if res.Success {
		t.Fatal("5xx must fail")
	}
}

func TestProbeSeedanceRelayConnectivity_200IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	res := ProbeSeedanceRelayConnectivity(context.Background(), srv.URL, "sk")
	if !res.Success {
		t.Fatalf("200 should succeed defensively: %#v", res)
	}
}
