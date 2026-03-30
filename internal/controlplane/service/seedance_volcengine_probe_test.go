package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeSeedanceVolcengineConnectivity_404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v3/contents/generations/tasks/__gateway_health_probe__") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); !strings.Contains(auth, "HMAC-SHA256") {
			t.Fatalf("expected HMAC auth, got %q", auth)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
	}))
	defer srv.Close()

	res := ProbeSeedanceVolcengineConnectivity(context.Background(), srv.URL, "ak", "sk", "cn-beijing")
	if !res.Success {
		t.Fatalf("want success, got %#v", res)
	}
	if !strings.Contains(res.Message, "connectivity+auth") {
		t.Fatalf("message=%s", res.Message)
	}
}

func TestProbeSeedanceVolcengineConnectivity_Bearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ark-key-1" {
			t.Fatalf("Authorization=%q", got)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	res := ProbeSeedanceVolcengineConnectivityCreds(context.Background(), srv.URL, SeedanceVolcCreds{APIKey: "ark-key-1"})
	if !res.Success {
		t.Fatalf("want success, got %#v", res)
	}
	if !strings.Contains(res.Message, "Bearer") {
		t.Fatalf("message=%s", res.Message)
	}
}

func TestProbeSeedanceVolcengineConnectivity_401IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res := ProbeSeedanceVolcengineConnectivity(context.Background(), srv.URL, "ak", "sk", "cn-beijing")
	if res.Success {
		t.Fatal("401 must fail")
	}
}

func TestParseSeedanceVolcCredentials(t *testing.T) {
	ak, sk, region := ParseSeedanceVolcCredentials(`{"auth_type":"aksk","access_key":"AK","secret_key":"SK","region":"cn-shanghai"}`)
	if ak != "AK" || sk != "SK" || region != "cn-shanghai" {
		t.Fatalf("got %s %s %s", ak, sk, region)
	}
	c := ParseSeedanceVolcCreds(`{"auth_type":"api_key","api_key":"uuid-key","access_key":"AK","secret_key":"SK"}`)
	if c.APIKey != "uuid-key" || !c.HasAuth() || c.AccessKey != "" {
		t.Fatalf("got %#v", c)
	}
}
