package notification

import "strings"

// PlainText 把站内信使用的轻量 Markdown 渲染成纯文本，供纯文本邮件通道使用。
//
// 站内信正文由前端按 Markdown 渲染，但邮件通道发的是纯文本：直接透传会把
// "**"、">" 这类标记原样送到用户邮箱里（例如 `**member**`、`> 告警不等于拦截`），
// 这正是告警邮件「文案很乱」的一部分。
//
// 只处理站内信实际会产生的语法：加粗 `**x**`、无序列表 `- x`、引用 `> x`。
func PlainText(markdown string) string {
	if markdown == "" {
		return ""
	}
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "> "):
			lines[i] = strings.TrimPrefix(trimmed, "> ")
		case strings.HasPrefix(trimmed, "- "):
			lines[i] = "· " + strings.TrimPrefix(trimmed, "- ")
		}
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "**", "")
}
