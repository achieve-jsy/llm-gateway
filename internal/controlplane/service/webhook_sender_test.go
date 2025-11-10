package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFeishuSign(t *testing.T) {
	secret := "test_secret"
	ts := int64(1609459200)

	stringToSign := fmt.Sprintf("%d\n%s", ts, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	mac.Write([]byte{})
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	t.Logf("timestamp=%d stringToSign=%q sign=%s", ts, stringToSign, sig)

	if sig == "" {
		t.Fatal("sign should not be empty")
	}

	decoded, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("sign is not valid base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("SHA256 output should be 32 bytes, got %d", len(decoded))
	}
}

func TestSendTest_FeishuPayloadFormat(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer ts.Close()

	sender := NewWebhookSender(ts.URL, "my_secret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.SendTest(ctx); err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", receivedContentType)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, string(receivedBody))
	}

	if msgType, _ := payload["msg_type"].(string); msgType != "text" {
		t.Errorf("msg_type = %q, want text", msgType)
	}

	content, _ := payload["content"].(map[string]interface{})
	text, _ := content["text"].(string)
	if !strings.Contains(text, "test notification") {
		t.Errorf("content.text = %q, want to contain 'test notification'", text)
	}

	if _, ok := payload["timestamp"]; !ok {
		t.Error("payload missing 'timestamp' field when secret is set")
	}
	if _, ok := payload["sign"]; !ok {
		t.Error("payload missing 'sign' field when secret is set")
	}

	t.Logf("payload = %s", string(receivedBody))
}

func TestSendTest_FeishuSignVerification(t *testing.T) {
	secret := "verify_secret"

	var receivedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]

		var payload map[string]interface{}
		json.Unmarshal(receivedBody, &payload)

		tsStr, _ := payload["timestamp"].(string)
		signStr, _ := payload["sign"].(string)

		var tsInt int64
		fmt.Sscanf(tsStr, "%d", &tsInt)

		stringToSign := fmt.Sprintf("%d\n%s", tsInt, secret)
		mac := hmac.New(sha256.New, []byte(stringToSign))
		mac.Write([]byte{})
		expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		if signStr != expectedSig {
			t.Errorf("sign mismatch: got %q, want %q", signStr, expectedSig)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"code":19001,"msg":"sign match fail, expected=%s"}`, expectedSig)))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer ts.Close()

	sender := NewWebhookSender(ts.URL, secret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.SendTest(ctx); err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}
}

func TestSendTest_NoSecret(t *testing.T) {
	var receivedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer ts.Close()

	sender := NewWebhookSender(ts.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.SendTest(ctx); err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}

	var payload map[string]interface{}
	json.Unmarshal(receivedBody, &payload)

	if _, ok := payload["timestamp"]; ok {
		t.Error("payload should not contain 'timestamp' when secret is empty")
	}
	if _, ok := payload["sign"]; ok {
		t.Error("payload should not contain 'sign' when secret is empty")
	}
}

func TestSendTest_EmptyURL(t *testing.T) {
	sender := NewWebhookSender("", "")
	ctx := context.Background()
	if err := sender.SendTest(ctx); err == nil {
		t.Fatal("SendTest with empty URL should return error")
	}
}

func TestSendTest_FeishuErrorCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":19001,"msg":"sign match fail"}`))
	}))
	defer ts.Close()

	sender := NewWebhookSender(ts.URL, "wrong_secret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sender.SendTest(ctx)
	if err == nil {
		t.Fatal("SendTest should return error when feishu returns non-zero code")
	}
	if !strings.Contains(err.Error(), "19001") {
		t.Errorf("error should contain feishu error code 19001, got: %v", err)
	}
}

func TestSendText(t *testing.T) {
	var receivedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer ts.Close()

	sender := NewWebhookSender(ts.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.SendText(ctx, "hello world"); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	var payload map[string]interface{}
	json.Unmarshal(receivedBody, &payload)
	content, _ := payload["content"].(map[string]interface{})
	text, _ := content["text"].(string)
	if text != "hello world" {
		t.Errorf("content.text = %q, want 'hello world'", text)
	}
}
