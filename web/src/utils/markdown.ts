import { marked } from 'marked'

// 配置 marked
marked.setOptions({
  breaks: true,   // 单个换行转 <br>
  gfm: true,      // GitHub Flavored Markdown
})

export function renderMarkdown(text: string): string {
  if (!text) return ''
  return marked.parse(text) as string
}

/** 把 Markdown 转为纯文本（剥除所有标签和符号），用于预览摘要 */
export function markdownToPlainText(text: string): string {
  if (!text) return ''
  const html = marked.parse(text) as string
  return html
    .replace(/<[^>]+>/g, ' ')   // 去除 HTML 标签
    .replace(/\s+/g, ' ')       // 合并多余空白
    .trim()
}