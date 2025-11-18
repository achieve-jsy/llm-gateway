import { getProcessor } from 'bytemd'
import gfm from '@bytemd/plugin-gfm'

// 与编辑器保持一致的 processor（GFM + breaks）
const processor = getProcessor({ plugins: [gfm()] })

export function renderMarkdown(text: string): string {
  if (!text) return ''
  return String(processor.processSync(text))
}

/** 把 Markdown 转为纯文本（剥除所有标签和符号），用于预览摘要 */
export function markdownToPlainText(text: string): string {
  if (!text) return ''
  const html = renderMarkdown(text)
  return html
    .replace(/<[^>]+>/g, ' ')   // 去除 HTML 标签
    .replace(/\s+/g, ' ')       // 合并多余空白
    .trim()
}