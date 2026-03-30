package volcengine

import (
	"net/http"
	"strings"
	"testing"
)

func TestSignSetsXDateBeforeCanonical(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "ark.cn-beijing.volces.com"

	NewSigner("AKTEST", "SKTEST", "cn-beijing").Sign(req)

	if req.Header.Get("X-Date") == "" {
		t.Fatal("X-Date should be set")
	}
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "HMAC-SHA256 Credential=AKTEST/") {
		t.Fatalf("unexpected Authorization: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") || !strings.Contains(auth, "x-date") {
		t.Fatalf("SignedHeaders should include x-date: %s", auth)
	}
}
