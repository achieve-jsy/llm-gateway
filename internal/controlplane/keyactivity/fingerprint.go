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

// IsSilentUserAgent returns true for clients that should not produce
// first-use/new-source/wakeup notifications (站内信/邮件)。
//
// 说明：
//   - trae：编辑器探针/引导流量，易误报
//   - hertz：Trae/内部网关常见 UA；出口 IP 频繁变化，在 ip_as_source=true
//     时会刷爆「新设备使用」提醒，故按 UA 静默（仍会更新 seen，只是不通知）
func IsSilentUserAgent(raw string) bool {
	ua := strings.ToLower(raw)
	return strings.Contains(ua, "trae") || strings.Contains(ua, "hertz")
}
