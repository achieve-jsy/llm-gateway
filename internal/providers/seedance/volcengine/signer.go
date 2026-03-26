package volcengine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Signer 火山引擎 AK/SK 签名器
type Signer struct {
	accessKey string
	secretKey string
	region    string
	service   string
}

// NewSigner 创建签名器
func NewSigner(accessKey, secretKey, region string) *Signer {
	return &Signer{
		accessKey: accessKey,
		secretKey: secretKey,
		region:    region,
		service:   "ark",
	}
}

// Sign 对请求签名
func (s *Signer) Sign(req *http.Request) {
	now := time.Now().UTC()
	timestamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")

	// Step 1: Canonical Request
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery

	signedHeaders := s.getSignedHeaders(req)
	canonicalHeaders := s.getCanonicalHeaders(req, signedHeaders)
	payloadHash := s.hash("")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	)

	// Step 2: String to Sign
	credentialScope := fmt.Sprintf("%s/%s/%s/request", date, s.region, s.service)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		timestamp,
		credentialScope,
		s.hash(canonicalRequest),
	)

	// Step 3: Signature
	signingKey := s.getSigningKey(date, s.region, s.service)
	signature := hex.EncodeToString(s.hmacSHA256(signingKey, []byte(stringToSign)))

	// Step 4: Authorization Header
	authorization := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey,
		credentialScope,
		strings.Join(signedHeaders, ";"),
		signature,
	)

	req.Header.Set("Authorization", authorization)
	req.Header.Set("X-Date", timestamp)
}

func (s *Signer) getSignedHeaders(req *http.Request) []string {
	headers := []string{"host", "x-date"}
	if req.Header.Get("Content-Type") != "" {
		headers = append(headers, "content-type")
	}
	sort.Strings(headers)
	return headers
}

func (s *Signer) getCanonicalHeaders(req *http.Request, signedHeaders []string) string {
	var result strings.Builder
	for _, h := range signedHeaders {
		result.WriteString(h)
		result.WriteString(":")
		switch h {
		case "host":
			result.WriteString(req.Host)
		case "x-date":
			result.WriteString(req.Header.Get("X-Date"))
		case "content-type":
			result.WriteString(req.Header.Get("Content-Type"))
		}
		result.WriteString("\n")
	}
	return result.String()
}

func (s *Signer) hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func (s *Signer) hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (s *Signer) getSigningKey(date, region, service string) []byte {
	kDate := s.hmacSHA256([]byte("HMAC"+s.secretKey), []byte(date))
	kRegion := s.hmacSHA256(kDate, []byte(region))
	kService := s.hmacSHA256(kRegion, []byte(service))
	kSigning := s.hmacSHA256(kService, []byte("request"))
	return kSigning
}
