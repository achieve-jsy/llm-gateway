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

// SourceFP 来源指纹：以 IP 为唯一标识。同 IP 视为同一设备，不区分 UA。
// IP 为空时回退到 UA 指纹（兜底）。
func SourceFP(uaRaw, ip string, _ bool) string {
	if ip != "" {
		return ip
	}
	return UAFingerprint(uaRaw)
}
