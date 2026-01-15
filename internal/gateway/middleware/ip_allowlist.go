package middleware

import (
	"encoding/json"
	"net"
	"strings"
)

// IsIPAllowed 判断 clientIP 是否在 allowlistJSON 允许的范围内。
// allowlistJSON 为空字符串或 "[]" 表示不限制（始终允许）。
// 白名单项支持单 IP（如 "1.2.3.4"）或 CIDR（如 "10.0.0.0/8"）。
//
// 放行策略（按顺序）：
//  1. allowlistJSON 为空 / "[]" / 空数组 → 始终允许（向后兼容）
//  2. clientIP 为空或非法 → 当配置了白名单但无法识别客户端 IP 时拒绝
//  3. clientIP 命中白名单 → 允许
func IsIPAllowed(clientIP, allowlistJSON string) bool {
	// 第 1 步：白名单为空（无限制），无条件放行。
	// 这是最关键的兼容性检查，必须放在 clientIP 校验之前，
	// 否则未设代理头的请求（clientIP 为空）会被误拒。
	trimmed := strings.TrimSpace(allowlistJSON)
	if trimmed == "" || trimmed == "[]" {
		return true
	}
	var list []string
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		// JSON 损坏：保守拒绝
		return false
	}
	if len(list) == 0 {
		return true
	}

	// 第 2 步：已配置白名单但 clientIP 无法解析，保守拒绝
	if clientIP == "" {
		return false
	}

	// 第 3 步：匹配
	parsed := net.ParseIP(strings.TrimSpace(clientIP))
	if parsed == nil {
		return false
	}
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			if ip.Equal(parsed) {
				return true
			}
			continue
		}
		if _, cidr, err := net.ParseCIDR(entry); err == nil {
			if cidr.Contains(parsed) {
				return true
			}
		}
	}
	return false
}

// ExtractClientIP 从请求上下文中解析客户端真实 IP，
// 优先级：X-Forwarded-For（取首个非空）> X-Real-IP > RemoteAddr。
// 仅做基础清理，不做 IP 合法性校验。
func ExtractClientIP(xForwardedFor, xRealIP, remoteAddr string) string {
	if xForwardedFor != "" {
		for _, part := range strings.Split(xForwardedFor, ",") {
			if p := strings.TrimSpace(part); p != "" {
				return p
			}
		}
	}
	if xRealIP != "" {
		return strings.TrimSpace(xRealIP)
	}
	if remoteAddr == "" {
		return ""
	}
	// RemoteAddr 通常为 "ip:port"，剥离端口
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
