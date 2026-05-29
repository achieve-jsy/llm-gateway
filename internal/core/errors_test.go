package core

import (
	"bytes"
	"net/http"
	"testing"
)

func TestParseProviderError_PreservesRawBody(t *testing.T) {
	raw := []byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	gwErr := ParseProviderError("openai", http.StatusTooManyRequests, raw, nil)
	if gwErr == nil {
		t.Fatal("ParseProviderError returned nil")
	}
	if !bytes.Equal(gwErr.Body, raw) {
		t.Errorf("Body = %q, want %q", gwErr.Body, raw)
	}
	if gwErr.Message != "rate limit exceeded" {
		t.Errorf("Message = %q, want %q", gwErr.Message, "rate limit exceeded")
	}
	if gwErr.Code == nil || *gwErr.Code != "rate_limit_exceeded" {
		t.Errorf("Code = %v, want %q", gwErr.Code, "rate_limit_exceeded")
	}
}

func TestParseProviderError_NonJSONBodyPreserved(t *testing.T) {
	raw := []byte("Bad Gateway: upstream crashed")
	gwErr := ParseProviderError("custom", http.StatusBadGateway, raw, nil)
	if gwErr == nil {
		t.Fatal("ParseProviderError returned nil")
	}
	if !bytes.Equal(gwErr.Body, raw) {
		t.Errorf("Body = %q, want %q", gwErr.Body, raw)
	}
	if gwErr.Message != "Bad Gateway: upstream crashed" {
		t.Errorf("Message = %q, want %q", gwErr.Message, "Bad Gateway: upstream crashed")
	}
}
