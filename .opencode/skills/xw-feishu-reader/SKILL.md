---
name: xw-feishu-reader
description: Read a Feishu (Lark) document as complete Markdown, including all embedded spreadsheet/table content and images (auto-uploaded to Aliyun OSS as permanent public URLs). Supports Wiki URLs, Docx URLs, raw docx tokens, AND standalone Sheet (spreadsheet) documents — all sheet tabs are rendered as Markdown tables. Works on Windows and macOS. Automatically re-authenticates when the OAuth token is expired. Use when (1) User says "读取飞书文档", "获取飞书内容", "下载飞书文档", "读取飞书", "抓取飞书" (2) User provides a Feishu URL and wants its content (3) A skill needs to read an existing Feishu document before processing or transforming it (4) User wants to standardize, diff, or extract data from a Feishu document or spreadsheet.
disable-model-invocation: true
---

# Feishu Reader

Read a Feishu **Docx** or **Sheet (spreadsheet)** wiki document as complete Markdown using the `lark-mcp` OAuth token cached on the local machine.

## Supported Document Types

| Wiki node type | Behavior                                                                                                                          |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| **docx**       | Fetches all blocks via Blocks API; embedded Sheet tables fetched via Sheets API; images uploaded to OSS                           |
| **sheet**      | Resolves spreadsheet token; reads all sheet tabs via Sheets API; each tab rendered as a Markdown table under its own `##` heading |

Fetches **all content types** that the plain-text API silently omits:

- **Text blocks** — paragraphs, headings, lists, code, dividers, quotes
- **Embedded spreadsheet tables** — fetched via Sheets API, rendered as Markdown tables
- **Standalone spreadsheet (Sheet) documents** — all tabs rendered as Markdown tables, one `## TabName` per tab
- **Images** — downloaded from Feishu and uploaded to Aliyun OSS (`harbor-yw/doc-ai/`) as permanent public URLs; already-uploaded images are reused from OSS cache automatically (no re-upload)

## Input

One of:

| Parameter              | Example                                   |
| ---------------------- | ----------------------------------------- |
| `--url <feishu_url>`   | `https://xxx.feishu.cn/wiki/Aa3PwfCXxxx`  |
| `--url <feishu_url>`   | `https://xxx.feishu.cn/docx/Z7xqdIQrexxx` |
| `--token <docx_token>` | `Z7xqdIQreohiIFxGx8ScSilknUd`             |

Wiki URLs that point to a **Sheet** document are automatically detected and handled — no special flag needed.

Optional:

| Parameter      | Default                 | Description                           |
| -------------- | ----------------------- | ------------------------------------- |
| `--out <path>` | `feishu_read_output.md` | Output file path — **use ASCII path** |

## Output

A local `.md` file with the full document content.

**Docx output:** Images appear as `![image](https://harbor-yw.oss-cn-hangzhou.aliyuncs.com/doc-ai/...)`.

**Sheet output:** Each tab is rendered as a Markdown table under its own `##` heading:

```markdown
# 文档标题

## Sheet 标签 1

| col1 | col2 | col3 |
| ---- | ---- | ---- |
| ...  | ...  | ...  |

## Sheet 标签 2

...
```

Console output on success:

```
────────────────────────────────────────────────────────────
SUCCESS
FILE:  docs/output.md
SIZE:  19.0 KB
IMG:   35 total, 35 uploaded to OSS          ← docx only; omitted for sheet
────────────────────────────────────────────────────────────
```

## Workflow

### Step 1: Run the script

```bash
node ".claude/skills/xuanwu/xw-feishu-reader/scripts/feishu_read.js" \
  --url "https://xxx.feishu.cn/wiki/WIKI_TOKEN" \
  --out "docs/output.md"
```

Or with a raw docx token:

```bash
node ".claude/skills/xuanwu/xw-feishu-reader/scripts/feishu_read.js" \
  --token "Z7xqdIQreohiIFxGx8ScSilknUd" \
  --out "docs/output.md"
```

**If `--out` path contains Chinese characters**, use a temporary wrapper script to avoid shell encoding issues:

```javascript
// Write to docs/_run_read.js, run it, then delete it
process.argv = [
  process.argv[0],
  process.argv[1],
  "--url",
  "https://xxx.feishu.cn/wiki/TOKEN",
  "--out",
  "docs/_feishu_output.md", // keep ASCII
];
require("../.claude/skills/xuanwu/xw-feishu-reader/scripts/feishu_read.js");
```

### Step 2: Read the output

Use the **Read tool** to load the output `.md` file for further processing.

## Auto Re-authentication

**重要：检测到 OAuth 过期时，仅通过本脚本的 login 流程重新授权，不要改用 Feishu MCP 接口获取文档内容。**

When the OAuth token is expired (detected from missing/expired storage or Feishu API code 99991663), the script **directly** runs the login command — it does **not** use Feishu MCP to fetch the document. Re-auth is always via CLI login:

1. **Script runs** `npx -y @larksuiteoapi/lark-mcp login -a cli_a90b5c88df769bd8 -s ab8mPg6b5XMi35eWone4dbysVeJkZPhJ` — opens the browser for the Feishu OAuth flow (app id/secret are fixed in the script).
2. User completes the browser login (timeout: 3 minutes).
3. Script retries the read with the new token.

If the script reports **login failed or timed out**, it prints the exact command to run. **Agent must not fallback to Feishu MCP.** Instead: run the printed command (or ask the user to complete the browser auth), then re-run the reader script.

## Block Type Reference

| block_type | Type                 | Notes                                                         |
| ---------- | -------------------- | ------------------------------------------------------------- |
| 1          | Page (document root) | Entry point for traversal                                     |
| 2          | Paragraph            | `text` field                                                  |
| 3–11       | Heading1–9           | `heading1`–`heading9` fields                                  |
| 12         | Bulleted list        | `bullet` field                                                |
| 13         | Ordered list         | `ordered` field                                               |
| 14         | Code block           | `code` field                                                  |
| 15         | Quote block          | `quote.elements` with bold support → `> **bold** text`        |
| 16         | TodoList             | `todo.elements` + `todo.style.done` → `- [ ] / - [x]`         |
| 18         | Callout (高亮块)     | Children paragraphs rendered as `> [emoji] text`              |
| 21 / 22    | Divider              | Rendered as `---`                                             |
| 24         | Grid (分栏容器)      | Recurse children, no special output                           |
| 25         | GridColumn (分栏列)  | Recurse children, no special output                           |
| 27         | Image                | `image.token` → rendered as `![image](token)` placeholder     |
| 30         | Embedded spreadsheet | `sheet.token` → fetched via Sheets API                        |
| 31         | Table                | `table.cells` + `table.property` → rendered as Markdown table |
| 32         | TableCell            | Consumed by type-31 handler                                   |
| 34         | Quote container      | Children rendered as `> ` blockquotes                         |

## Error Handling

| Error                                        | Cause                              | Fix                                                                                                                                                                                                                |
| -------------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `lark-mcp login failed or timed out`         | OAuth not completed in 3 min       | **Do not use Feishu MCP.** 脚本会打印完整 login 命令；手动执行该命令，在浏览器完成授权后重新运行读取脚本。命令：`npx -y @larksuiteoapi/lark-mcp login -a cli_a90b5c88df769bd8 -s ab8mPg6b5XMi35eWone4dbysVeJkZPhJ` |
| `Token still expired after login`            | Login incomplete                   | 同上，再次执行上述 login 命令完成授权                                                                                                                                                                              |
| API `code=99991663` (token expired)          | Token expired                      | Script auto-triggers login and retries once; no fallback to MCP                                                                                                                                                    |
| `Sheet values error: code=90215`             | Sheet tab deleted from spreadsheet | Safe to ignore — that tab will render as empty                                                                                                                                                                     |
| `Wiki node type "mindnote" is not supported` | Unsupported node type              | Only `docx` and `sheet` are supported                                                                                                                                                                              |

## Requirements

- **Windows or macOS** (AES key read from Credential Manager / Keychain respectively)
- Node.js ≥ 16
- `lark-mcp` previously authenticated via `npx @larksuiteoapi/lark-mcp mcp`
