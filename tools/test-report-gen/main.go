// 生成 Key Activity TDD 测试报告（HTML）
//
// 用法：
//
//	go test -json ./tests/controlplane/keyactivity/... -count=1 | go run tools/test-report-gen/main.go > report.html
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type testResult struct {
	Name    string
	Package string
	Status  string // pass / fail / skip
	Elapsed float64
	Output  string
}

func main() {
	results := make(map[string]*testResult)
	var pkgPass, pkgFail int
	pkgElapsed := 0.0

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev testEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}

		if ev.Test == "" {
			// 包级别事件 — 记录整体耗时
			if ev.Action == "pass" {
				pkgPass++
				pkgElapsed = ev.Elapsed
			} else if ev.Action == "fail" {
				pkgFail++
				pkgElapsed = ev.Elapsed
			}
			continue
		}

		key := ev.Package + " / " + ev.Test
		switch ev.Action {
		case "run":
			results[key] = &testResult{Name: ev.Test, Package: ev.Package, Status: "running"}
		case "output":
			if r, ok := results[key]; ok {
				r.Output += ev.Output
			}
		case "pass":
			if r, ok := results[key]; ok {
				r.Status = "pass"
				r.Elapsed = ev.Elapsed
			}
		case "fail":
			if r, ok := results[key]; ok {
				r.Status = "fail"
				r.Elapsed = ev.Elapsed
			}
		case "skip":
			if r, ok := results[key]; ok {
				r.Status = "skip"
				r.Elapsed = ev.Elapsed
			}
		case "pause", "cont":
			// ignore
		}
	}

	var pass, fail, skip int
	var ordered []*testResult
	for _, r := range results {
		switch r.Status {
		case "pass":
			pass++
		case "fail":
			fail++
		case "skip":
			skip++
		}
		ordered = append(ordered, r)
	}

	total := pass + fail + skip
	statusLabel := "PASS"
	statusColor := "#22c55e"
	if fail > 0 {
		statusLabel = "FAIL"
		statusColor = "#ef4444"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<title>Key Activity TDD 测试报告</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, "PingFang SC", "Hiragino Sans GB", sans-serif;
         background: #f5f5f5; color: #1a1a1a; padding: 32px; }
  .container { max-width: 960px; margin: 0 auto; }
  h1 { font-size: 1.6rem; margin-bottom: 4px; }
  .subtitle { color: #666; font-size: 0.85rem; margin-bottom: 20px; }
  .summary { display: flex; gap: 12px; margin-bottom: 24px; flex-wrap: wrap; }
  .stat { background: #fff; border-radius: 8px; padding: 14px 20px;
          box-shadow: 0 1px 3px rgba(0,0,0,0.06); min-width: 110px; }
  .stat .label { font-size: 0.75rem; color: #888; text-transform: uppercase; letter-spacing: 0.5px; }
  .stat .value { font-size: 1.8rem; font-weight: 700; margin-top: 2px; }
  .stat.pass .value { color: #22c55e; }
  .stat.fail .value { color: #ef4444; }
  .stat.skip .value { color: #f59e0b; }
  .stat.total .value { color: #2563eb; }
  .overall-badge {
    display: inline-block; padding: 4px 14px; border-radius: 999px; color: #fff;
    font-size: 0.85rem; font-weight: 600; margin-bottom: 20px;
    background: %[2]s;
  }
  table { width: 100%%; border-collapse: collapse; background: #fff;
          border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  th { background: #f9fafb; text-align: left; padding: 10px 14px; font-size: 0.8rem;
       color: #666; border-bottom: 1px solid #eee; font-weight: 600; }
  td { padding: 10px 14px; border-bottom: 1px solid #f0f0f0; font-size: 0.9rem; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: #fafbfc; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 4px;
           font-size: 0.78rem; font-weight: 600; text-transform: uppercase; }
  .badge.pass { background: #dcfce7; color: #16a34a; }
  .badge.fail { background: #fee2e2; color: #dc2626; }
  .badge.skip { background: #fef3c7; color: #d97706; }
  .elapsed { color: #888; font-size: 0.85rem; font-variant-numeric: tabular-nums; }
  .test-name { font-family: "SF Mono", "Menlo", monospace; }
  details { margin-top: 6px; }
  details summary { font-size: 0.78rem; color: #888; cursor: pointer; }
  pre { background: #f9fafb; border: 1px solid #eee; border-radius: 4px;
        padding: 8px 10px; font-size: 0.78rem; overflow-x: auto;
        color: #444; line-height: 1.5; max-height: 220px; overflow-y: auto;
        margin-top: 6px; white-space: pre-wrap; word-break: break-all; }
  .footer { margin-top: 24px; font-size: 0.78rem; color: #aaa; text-align: center; }
</style>
</head>
<body>
<div class="container">
  <h1>Key Activity 测试报告</h1>
  <div class="subtitle">%[3]s · 包: tests/controlplane/keyactivity · 总耗时: %[4].2fs</div>
  <div class="overall-badge">%[1]s</div>
  <div class="summary">
    <div class="stat total"><div class="label">总数</div><div class="value">%[5]d</div></div>
    <div class="stat pass"><div class="label">通过</div><div class="value">%[6]d</div></div>
    <div class="stat fail"><div class="label">失败</div><div class="value">%[7]d</div></div>
    <div class="stat skip"><div class="label">跳过</div><div class="value">%[8]d</div></div>
  </div>
  <table>
    <thead>
      <tr><th style="width:55%%">测试名</th><th style="width:15%%">状态</th><th style="width:15%%">耗时</th><th>说明</th></tr>
    </thead>
    <tbody>
`, statusLabel, statusColor, time.Now().Format("2006-01-02 15:04:05"), pkgElapsed, total, pass, fail, skip)

	for _, r := range ordered {
		desc := testDescription(r.Name)
		escapedOutput := htmlEscape(r.Output)
		html += fmt.Sprintf(`      <tr>
        <td class="test-name">
          %s
          <details><summary>查看详细输出</summary>
            <pre>%s</pre>
          </details>
        </td>
        <td><span class="badge %s">%s</span></td>
        <td class="elapsed">%.2fs</td>
        <td style="font-size:0.8rem;color:#555">%s</td>
      </tr>
`, r.Name, escapedOutput, r.Status, strings.ToUpper(r.Status), r.Elapsed, desc)
	}

	html += `    </tbody>
  </table>
  <div class="footer">Generated by go test -json · LLM Gateway Key Activity TDD</div>
</div>
</body>
</html>`

	fmt.Print(html)
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func testDescription(name string) string {
	if idx := strings.Index(name, "_"); idx > 0 {
		name = name[idx+1:]
	}
	r := strings.NewReplacer(
		"_", " ",
		"E1", "新来源",
		"E2", "首次使用",
		"E3", "唤醒",
		"E4", "激增",
	)
	return r.Replace(name)
}
