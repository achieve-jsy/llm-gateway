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

// SourceFP 来源指纹：ua_fp 或 ua_fp|ip（取决于 ipAsSource 配置）
func SourceFP(uaRaw, ip string, ipAsSource bool) string {
	uaFP := UAFingerprint(uaRaw)
	if ipAsSource && ip != "" {
		return uaFP + "|" + ip
	}
	return uaFP
}
