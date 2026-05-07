package keyactivity

import (
	"crypto/sha1"
	"fmt"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`\d+(\.\d+)*`)

// NormalizeUA 规范化 User-Agent：小写 → 去版本号 → 合并空白 → 空串归 unknown
func NormalizeUA(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "unknown"
	}
	s = versionPattern.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "unknown"
	}
	return s
}

// UAFingerprint 客户端指纹：sha1(规范化UA) 前 16 位 hex
func UAFingerprint(raw string) string {
	norm := NormalizeUA(raw)
	h := sha1.New()
	h.Write([]byte(norm))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// SourceFP 来源指纹：启用 IP 来源识别时优先使用 IP，否则仅使用 UA。
// IP 为空时始终回退到 UA 指纹。
func SourceFP(uaRaw, ip string, ipAsSource bool) string {
	if ipAsSource && ip != "" {
		return ip
	}
	return UAFingerprint(uaRaw)
}

// IsSilentUserAgent returns true for clients whose bootstrap/probe requests are
// expected and should not produce first-use/new-source/wakeup notifications.
func IsSilentUserAgent(raw string) bool {
	return strings.Contains(strings.ToLower(raw), "trae")
}
