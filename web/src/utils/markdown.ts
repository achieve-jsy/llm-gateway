import { getProcessor } from 'bytemd'
import gfm from '@bytemd/plugin-gfm'
import highlight from '@bytemd/plugin-highlight'
import remarkBreaks from 'remark-breaks'
import type { BytemdPlugin } from 'bytemd'

// 把 remark-breaks 包装成 ByteMD plugin 格式
const breaksPlugin = (): BytemdPlugin => ({
  remark: (processor) => processor.use(remarkBreaks),
})

// 与编辑器完全一致的 processor：GFM + 代码高亮 + 软换行转 <br>
const processor = getProcessor({
  plugins: [gfm(), highlight(), breaksPlugin()],
})

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
