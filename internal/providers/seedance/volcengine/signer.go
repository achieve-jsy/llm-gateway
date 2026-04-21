package volcengine

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Signer 火山引擎 AK/SK 签名器（对齐 volc-openapi-demos/signature/golang）
type Signer struct {
	accessKey string
	secretKey string
	region    string
	service   string
}

// NewSigner 创建签名器
func NewSigner(accessKey, secretKey, region string) *Signer {
	if region == "" {
		region = "cn-beijing"
	}
	return &Signer{
		accessKey: accessKey,
		secretKey: secretKey,
		region:    region,
		service:   "ark",
	}
}

// Sign 对请求签名。会读取并重置 Body（若存在），以便计算真实 payload hash。
func (s *Signer) Sign(req *http.Request) {
	now := time.Now().UTC()
	timestamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")

	if req.Host == "" && req.URL != nil {
		req.Host = req.URL.Host
	}

	payloadHash := s.hashBytes(s.readBody(req))
	req.Header.Set("X-Date", timestamp)
	req.Header.Set("X-Content-Sha256", payloadHash)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery
	// url.Values.Encode 会把空格编成 +，火山要求 %20
	canonicalQuery = strings.ReplaceAll(canonicalQuery, "+", "%20")

	signedHeaders := []string{"content-type", "host", "x-content-sha256", "x-date"}
	sort.Strings(signedHeaders)
	canonicalHeaders := s.getCanonicalHeaders(req, signedHeaders)

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	)

	credentialScope := fmt.Sprintf("%s/%s/%s/request", date, s.region, s.service)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		timestamp,
		credentialScope,
		s.hash(canonicalRequest),
	)

	signingKey := s.getSigningKey(date, s.region, s.service)
	signature := hex.EncodeToString(s.hmacSHA256(signingKey, []byte(stringToSign)))

	authorization := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey,
		credentialScope,
		strings.Join(signedHeaders, ";"),
		signature,
	)
	req.Header.Set("Authorization", authorization)
}

func (s *Signer) readBody(req *http.Request) []byte {
	if req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body
}

func (s *Signer) getCanonicalHeaders(req *http.Request, signedHeaders []string) string {
	var result strings.Builder
	for _, h := range signedHeaders {
		result.WriteString(h)
		result.WriteString(":")
		switch h {
		case "host":
			result.WriteString(strings.TrimSpace(req.Host))
		default:
			result.WriteString(strings.TrimSpace(req.Header.Get(h)))
		}
		result.WriteString("\n")
	}
	return result.String()
}

func (s *Signer) hash(data string) string {
	return s.hashBytes([]byte(data))
}

func (s *Signer) hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (s *Signer) hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// getSigningKey 对齐官方 demo：kDate = HMAC(secretKey, date)，无 "HMAC" 前缀。
func (s *Signer) getSigningKey(date, region, service string) []byte {
	kDate := s.hmacSHA256([]byte(s.secretKey), []byte(date))
	kRegion := s.hmacSHA256(kDate, []byte(region))
	kService := s.hmacSHA256(kRegion, []byte(service))
	return s.hmacSHA256(kService, []byte("request"))
}
