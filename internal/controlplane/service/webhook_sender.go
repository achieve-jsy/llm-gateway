package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type WebhookSender struct {
	url    string
	secret string
	client *http.Client
}

func NewWebhookSender(url, secret string) *WebhookSender {
	return &WebhookSender{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// webhookSenderIface 可注入的 webhook 发送器(测试用 fake;真实实现为 WebhookSender)。
type webhookSenderIface interface {
	SendText(ctx context.Context, text string) error
}

func (w *WebhookSender) Send(ctx context.Context, msgType string, content map[string]interface{}) error {
	if w.url == "" {
		return nil
	}

	payload := map[string]interface{}{
		"msg_type": msgType,
		"content":  content,
	}

	if w.secret != "" {
		ts := time.Now().Unix()
		stringToSign := fmt.Sprintf("%d\n%s", ts, w.secret)
		mac := hmac.New(sha256.New, []byte(stringToSign))
		mac.Write([]byte{})
		sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		payload["timestamp"] = fmt.Sprintf("%d", ts)
		payload["sign"] = sig
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LLM-Gateway-Webhook/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[webhook] response from %s: status=%d body=%s", w.url, resp.StatusCode, string(respBody))

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var feishuResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(respBody, &feishuResp) == nil && feishuResp.Code != 0 {
		return fmt.Errorf("webhook error: code=%d msg=%s", feishuResp.Code, feishuResp.Msg)
	}

	log.Printf("[webhook] sent msg_type=%s to %s status=%d", msgType, w.url, resp.StatusCode)
	return nil
}

func (w *WebhookSender) SendText(ctx context.Context, text string) error {
	if w.url == "" {
		return fmt.Errorf("webhook URL is empty")
	}
	return w.Send(ctx, "text", map[string]interface{}{
		"text": text,
	})
}

func (w *WebhookSender) SendTest(ctx context.Context) error {
	if w.url == "" {
		return fmt.Errorf("webhook URL is empty")
	}
	return w.SendText(ctx, "This is a test notification from LLM Gateway")
}