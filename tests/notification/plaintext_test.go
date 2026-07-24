package notification_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/notification"
	"github.com/stretchr/testify/assert"
)

// TestPlainTextStripsMarkdownForEmail 断言：发给邮箱的正文是纯文本，
// 不会再出现 "**"、">" 这些 Markdown 标记。
func TestPlainTextStripsMarkdownForEmail(t *testing.T) {
	content := "**X-Lab大模型·成员日常预算** 本月用量已达 **80%**（当前 80.1%），请及时关注。\n\n" +
		"- 已用：$6400.55 USD\n" +
		"- 额度上限：$8000.00 USD\n" +
		"- 剩余可用：$1599.45 USD\n\n" +
		"> 告警不等于拦截：额度真正用尽后才会触发拦截。如需提高额度，请提交扩容申请。"

	got := notification.PlainText(content)

	assert.NotContains(t, got, "**")
	assert.NotContains(t, got, "\n> ")
	assert.Contains(t, got, "X-Lab大模型·成员日常预算 本月用量已达 80%（当前 80.1%）")
	assert.Contains(t, got, "· 已用：$6400.55 USD")
	assert.Contains(t, got, "告警不等于拦截：额度真正用尽后才会触发拦截。")
}

// TestPlainTextKeepsPlainContent 断言：没有 Markdown 的正文原样保留。
func TestPlainTextKeepsPlainContent(t *testing.T) {
	assert.Equal(t, "扩容申请已通过", notification.PlainText("扩容申请已通过"))
	assert.Equal(t, "", notification.PlainText(""))
}
