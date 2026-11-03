package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// ---- 压缩检测/分段选读 ----

func TestPromptDropRatio_NoDrop(t *testing.T) {
	compacted, drops := promptDropRatio([]int64{100, 200, 300}, 0.7)
	if compacted || len(drops) != 0 {
		t.Fatalf("期望无压缩，got compacted=%v drops=%v", compacted, drops)
	}
}

func TestPromptDropRatio_Drop(t *testing.T) {
	compacted, drops := promptDropRatio([]int64{8000, 20000, 45000, 9000, 30000, 12000}, 0.7)
	if !compacted {
		t.Fatal("期望检测到压缩")
	}
	// 9000 < 45000*0.7 → 下标3；12000 < 30000*0.7 → 下标5
	if len(drops) != 2 || drops[0] != 3 || drops[1] != 5 {
		t.Fatalf("压缩点不对: %v", drops)
	}
}

func TestChooseReadRequests_NoCompaction(t *testing.T) {
	reqIDs := []string{"r1", "r2", "r3"}
	got := chooseReadRequests([]int64{100, 200, 300}, reqIDs, 0.7)
	if len(got) != 1 || got[0] != "r3" {
		t.Fatalf("无压缩应只读末轮, got %v", got)
	}
}

func TestChooseReadRequests_Compacted(t *testing.T) {
	// 8k→20k→45k→[压缩]→9k→30k→[压缩]→12k：读第3、6、7轮（doc §3.3 例）
	seq := []int64{8000, 20000, 45000, 9000, 30000, 12000, 12000}
	reqIDs := []string{"a", "b", "c", "d", "e", "f", "g"}
	got := chooseReadRequests(seq, reqIDs, 0.7)
	// 期望下标 2(c), 4(e), 6(g) —— 每个上升段末轮 + 末轮
	want := map[string]bool{"c": true, "e": true, "g": true}
	if len(got) != 3 {
		t.Fatalf("期望读 3 条, got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("多读了 %s (全部 %v)", g, got)
		}
	}
}

// ---- 意图序列抽取/脱敏/压缩 ----

func TestExtractUserIntents_Basic(t *testing.T) {
	body := `{"messages":[
		{"role":"system","content":"sys"},
		{"role":"user","content":"帮我把函数改成异步的"},
		{"role":"assistant","content":"好"},
		{"role":"user","content":"写一下单测"}
	]}`
	texts, ok := extractUserIntents(body)
	if !ok || len(texts) != 2 {
		t.Fatalf("期望抽到 2 条用户消息, got %v ok=%v", texts, ok)
	}
	if texts[0] != "帮我把函数改成异步的" || texts[1] != "写一下单测" {
		t.Fatalf("内容不对: %v", texts)
	}
}

func TestExtractUserIntents_Invalid(t *testing.T) {
	if _, ok := extractUserIntents("not json"); ok {
		t.Fatal("非 JSON 应不可判")
	}
	if _, ok := extractUserIntents(""); ok {
		t.Fatal("空正文应不可判")
	}
}

func TestCompressTurn_StripsCodeAndPII(t *testing.T) {
	raw := "帮我看下这个代码 ```func main(){fmt.Println(1)}``` 然后发邮件给 a@b.com 我的电话 13812345678"
	out := compressTurn(raw)
	if containsStr(out, "func main") {
		t.Fatalf("代码块未被剥离: %s", out)
	}
	if containsStr(out, "a@b.com") || containsStr(out, "13812345678") {
		t.Fatalf("PII 未脱敏: %s", out)
	}
}

func TestHeadTail_TruncatesMiddle(t *testing.T) {
	long := ""
	for i := 0; i < 60; i++ {
		long += "字"
	}
	out := headTail(long, 20)
	if len([]rune(out)) >= 60 {
		t.Fatalf("未截断: len=%d", len([]rune(out)))
	}
	if !containsStr(out, "…") {
		t.Fatalf("应有省略号: %q", out)
	}
}

func TestBuildIntentPrompt(t *testing.T) {
	turns := []intentTurn{{Index: 1, UserHead: "帮我把函数改成异步的"}, {Index: 2, UserHead: "写一下单测"}}
	p := buildIntentPrompt("toothpaste", turns, false, false)
	if !containsStr(p, "同一件事") || !containsStr(p, "帮我把函数改成异步的") {
		t.Fatalf("prompt 内容不对:\n%s", p)
	}
	if containsStr(p, "压缩") {
		t.Fatalf("无压缩不应提压缩提示")
	}
}

// ---- hitSessionsFor 与规则口径一致 ----

func TestHitSessionsFor_Toothpaste(t *testing.T) {
	sessions := []SessionFact{
		{SessionID: "s-short", PromptSeq: []int64{10, 20, 30}, SplitEvidence: SplitEvidenceStrong}, // 3轮 < MinTurns 8
		{SessionID: "s-batch", PromptSeq: []int64{10, 20, 30, 40, 50, 60, 70, 80, 90}, SplitEvidence: SplitEvidenceBatch},
		{SessionID: "s-single", PromptSeq: []int64{10}, SplitEvidence: SplitEvidenceStrong},
		{SessionID: "s-hit", PromptSeq: []int64{10, 12, 15, 9, 11, 18, 8, 14, 10}, SplitEvidence: SplitEvidenceStrong}, // 9轮全短
	}
	hits := hitSessionsFor("toothpaste", sessions, DefaultToothpasteParams())
	if len(hits) != 1 || hits[0].SessionID != "s-hit" {
		t.Fatalf("命中会话不对: %+v", hits)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsContextBoilerplate(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"学习阅读我上传的节奏纲", false},
		{"生成剧本第一集", false},
		{"改吧", false},
		{"10", true},
		{"<system-reminder>\nThe following workspace instructions may be relevant", true},
		{"Current runtime context. This snapshot supersedes earlier runtime-context snapshots.", true},
		{"Instructions from: AGENTS.md\n你现在是一位拥有十年经验", true},
		{"You are an AI agent powered by DeepSeek Harness.", true},
	}
	for _, c := range cases {
		if got := isContextBoilerplate(c.in); got != c.want {
			t.Fatalf("isContextBoilerplate(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestExtractUserIntents_SkipsBoilerplate(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"10"},
		{"role":"user","content":"学习阅读我上传的节奏纲"},
		{"role":"user","content":"<system-reminder>上下文注入</system-reminder>"},
		{"role":"user","content":"改吧"}
	]}`
	texts, ok := extractUserIntents(body)
	if !ok || len(texts) != 2 {
		t.Fatalf("应只保留 2 条真实意图, got %v", texts)
	}
	if texts[0] != "学习阅读我上传的节奏纲" || texts[1] != "改吧" {
		t.Fatalf("内容不对: %v", texts)
	}
}

func TestHitSessionsFor_MultiTask(t *testing.T) {
	mp := DefaultMultiTaskParams()
	sessions := []SessionFact{
		// 命中近似态：超长(410k)+命中好+无 reset+有突增(100→400k)。
		{SessionID: "s-mt-hit", UserID: "u1", PromptSeq: []int64{100, 400000, 410000},
			CachedSeq: []int64{50, 200000, 200000}, SplitEvidence: SplitEvidenceWeak,
			ReqIDs: []string{"r1", "r2", "r3"}},
		// 不命中：无突增（一次到位）。
		{SessionID: "s-mt-nojump", UserID: "u1", PromptSeq: []int64{400000, 410000},
			CachedSeq: []int64{200000, 200000}, SplitEvidence: SplitEvidenceWeak},
		// 不命中：批处理。
		{SessionID: "s-mt-batch", PromptSeq: []int64{100, 400000, 410000},
			CachedSeq: []int64{50, 200000, 200000}, SplitEvidence: SplitEvidenceBatch},
		// 不命中：有 reset（正常切换）。
		{SessionID: "s-mt-reset", PromptSeq: []int64{100, 400000, 410000},
			CachedSeq: []int64{50, 200000, 200000}, ResetFlags: []bool{false, true, false},
			SplitEvidence: SplitEvidenceWeak},
	}
	hits := hitSessionsFor("multi_task_session", sessions, mp)
	if len(hits) != 1 || hits[0].SessionID != "s-mt-hit" {
		t.Fatalf("multi_task hits 不对: %+v", hits)
	}
}

func TestEvenlyPick(t *testing.T) {
	// n>=len → 全量。
	if got := evenlyPick([]string{"a", "b"}, 8); len(got) != 2 {
		t.Fatalf("short input 应全量, got %v", got)
	}
	// 10 取 4 → 保头尾 + 均匀 2 个中间。
	ten := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	got := evenlyPick(ten, 4)
	if len(got) != 4 || got[0] != "0" || got[len(got)-1] != "9" {
		t.Fatalf("evenlyPick 头尾不对: %v", got)
	}
	// 中间两个下标应在 [1,8] 区间且递增。
	if !(got[1] >= "1" && got[2] <= "9") {
		t.Fatalf("evenlyPick 中间项越界: %v", got)
	}
	// n=1 → 只取头。
	if got := evenlyPick(ten, 1); len(got) != 1 || got[0] != "0" {
		t.Fatalf("n=1 应只取头, got %v", got)
	}
}

func TestBuildIntentPrompt_MultiTaskSegmentMark(t *testing.T) {
	turns := []intentTurn{
		{Index: 1, Segment: 0, UserHead: "分析接口延迟"},
		{Index: 2, Segment: 1, UserHead: "帮我看一下这个报错"},
	}
	p := buildIntentPrompt("multi_task_session", turns, true, true)
	if !containsStr(p, "[段0]") || !containsStr(p, "[段1]") {
		t.Fatalf("multi_task 段标记缺失:\n%s", p)
	}
	if !containsStr(p, "任务有没有切换") {
		t.Fatalf("问题不对:\n%s", p)
	}
	if !containsStr(p, "主题跳跃") {
		t.Fatalf("压缩提示缺失:\n%s", p)
	}
	// toothpaste 无段标记。
	p2 := buildIntentPrompt("toothpaste", turns, true, true)
	if containsStr(p2, "[段0]") {
		t.Fatalf("toothpaste 不应有段标记:\n%s", p2)
	}
}

// 3.5 over_model：无正文源/空正文 → 维持 suspected（P1 行为），不 panic。
func TestSemanticUpgradeOverModel_NoBodyKeepsSuspected(t *testing.T) {
	issue := report.UsageIssue{
		Type: report.IssueTypeOverModel, Verdict: report.VerdictSuspected,
		RequestCount: 3, Evidence: map[string]any{
			"hit_request_ids": []string{"r1", "r2", "r3"},
		},
	}
	// 网关设 nil → 短路返回，维持疑似（provider 一并传 nil 即可触发首行短路）。
	up, ok := semanticUpgradeOverModel(context.Background(), nil, nil, issue)
	if ok {
		t.Fatal("gw=nil 不应升级")
	}
	if up.Verdict != report.VerdictSuspected {
		t.Fatalf("gw=nil 后 verdict=%q, want suspected", up.Verdict)
	}
}

func TestCapIntents(t *testing.T) {
	// 生成 200 条 → cap 90。
	var turns []intentTurn
	for i := 0; i < 200; i++ {
		turns = append(turns, intentTurn{Index: i + 1, Segment: i % 3, UserHead: "意图"})
	}
	got := capIntents(turns, 90, 80)
	if len(got) != 90 {
		t.Fatalf("cap 后条数=%d want 90", len(got))
	}
	if got[0].UserHead != "意图" || got[len(got)-1].UserHead != "意图" {
		t.Fatalf("首尾丢失")
	}
	// Index 重新编号连续 1..90。
	for i, g := range got {
		if g.Index != i+1 {
			t.Fatalf("index 不连续: i=%d idx=%d", i, g.Index)
		}
	}
	// rune 截断。
	long := []intentTurn{{Index: 1, UserHead: string(make([]rune, 200))}}
	got2 := capIntents(long, 5, 80)
	if len([]rune(got2[0].UserHead)) != 81 { // 80 + "…"
		t.Fatalf("rune 截断后长度=%d want 81", len([]rune(got2[0].UserHead)))
	}
}

func TestFlattenContent_PartsAndToolResult(t *testing.T) {
	// parts 数组：text + tool_result(content 嵌套)。
	c := []any{
		map[string]any{"type": "text", "text": "帮我改这段代码"},
		map[string]any{"type": "tool_result", "content": "文件已更新"},
	}
	got := flattenContent(c)
	if !containsStr(got, "帮我改这段代码") || !containsStr(got, "文件已更新") {
		t.Fatalf("parts flatten 不全: %q", got)
	}
	// 字符串内含嵌套 JSON 数组（工具结果字符串化）。
	s := `[{"type":"text","text":"分析结果：延迟高"}]`
	if got := flattenNestedJSON(s); !containsStr(got, "分析结果") {
		t.Fatalf("nested json flatten 失败: %q", got)
	}
	// 普通以 { 开头的正文不被误伤。
	plain := "{\"这里\"不是JSON: 正常文本"
	if got := flattenNestedJSON(plain); got != plain {
		t.Fatalf("普通文本被误展开: %q", got)
	}
}

func TestIsContextBoilerplate_SkillsList(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"[{\"text\": \"<system-reminder>\\nThe following skills are available for use with this session\", \"type\": \"text\"}]", true},
		{"The following skills are available for use with this session", true},
		{"修复 API 超时问题", false},
	}
	for _, c := range cases {
		if got := isContextBoilerplate(c.in); got != c.want {
			t.Fatalf("isContextBoilerplate(%q)=%v want %v", c.in[:minInt(40, len(c.in))], got, c.want)
		}
	}
}

// ---- 语义优先（refute 剔除费用/主导）----

// stubProvider 仅实现 TraceBodies（其余方法借嵌入 nil 接口——judgeSession 只调 TraceBodies）。
type stubProvider struct {
	ReportDataProvider
	bodies map[string]string
}

func (p *stubProvider) TraceBodies(_ context.Context, reqIDs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range reqIDs {
		if b, ok := p.bodies[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

// TestSemanticUpgradeIssue_AllRefuteZeroesCost 用户拍板：语义读内容优先于元数据数值推断——
// 全部会话 refute 时费用清零、不进占比/主导，但仍保留疑似供人工核（不武断 not_hit）。
func TestSemanticUpgradeIssue_AllRefuteZeroesCost(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}
	body := `{"messages":[{"role":"user","content":"帮我看看这段代码的运行结果"}]}`
	prov := &stubProvider{bodies: map[string]string{"req-1": body, "req-2": body}}
	// mock 网关：固定回 same_task（refute multi_task）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"same_task\",\"confidence\":0.9}"}}]}`)
	}))
	defer srv.Close()
	gw := &SemanticGateway{BaseURL: srv.URL, Key: "k", Model: "m", Timeout: 5 * time.Second, MaxCallsPerReport: 10}
	issue := report.UsageIssue{
		Type: report.IssueTypeMultiTaskSession, Verdict: report.VerdictSuspected,
		SessionCount: 2, SessionShare: 0.5,
		AttributedCostUSD: 10, AffectedCostUSD: 10,
		Evidence: map[string]any{
			"hit_session_ids": []string{"s1", "s2"},
			"_attr_by_session": map[string]any{
				"s1": 6.0, "s2": 4.0,
			},
		},
	}
	sessions := []SessionFact{
		{SessionID: "s1", ReqIDs: []string{"req-1"}, PromptSeq: []int64{500, 500}},
		{SessionID: "s2", ReqIDs: []string{"req-2"}, PromptSeq: []int64{500, 500}},
	}
	up, ok := semanticUpgradeIssue(context.Background(), prov, gw, report.IssueTypeMultiTaskSession, issue, sessions, DefaultMultiTaskParams())
	if !ok {
		t.Fatal("语义应升级")
	}
	if up.Verdict != report.VerdictSuspected {
		t.Fatalf("全 refute 应保留 suspected(供人核), got %q", up.Verdict)
	}
	if up.AttributedCostUSD != 0 || up.AffectedCostUSD != 0 {
		t.Fatalf("全 refute 费用应清零, got attr=%v aff=%v", up.AttributedCostUSD, up.AffectedCostUSD)
	}
	if up.SessionCount != 0 {
		t.Fatalf("全 refute 命中会话应清 0, got %d", up.SessionCount)
	}
	note := report.SemanticNote(up.Evidence)
	if !strings.Contains(note, "模型复核") || !strings.Contains(note, "费用已排除") {
		t.Fatalf("note 应说明模型复核后排除费用, got %q", note)
	}
	// Evidence 语义明细：refuted=2（int 入 map，断言按 int；兼容 float64/json.Number 亦接受）
	sem, _ := up.Evidence["semantic"].(map[string]any)
	if sem == nil || !refutedIs(sem["refuted"], 2) {
		t.Fatalf("semantic evidence refuted 应=2, got %v", sem)
	}
	// 内容样本应带上（报告卡复核依据）：模型读到正文有内容 → content_sample 非空。
	if cs := report.SemanticContentSamples(up.Evidence); len(cs) == 0 {
		t.Fatal("refute 后应保留语义读取的内容样本供负责人复核")
	}
}

// TestSemanticUpgradeIssue_MixedRefuteUncertain 部分 refute 部分 uncertain：只扣 refute 的会话费用，
// uncertain 部分保留疑似与费用。
func TestSemanticUpgradeIssue_MixedRefuteUncertain(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}
	// s1 refute, s2 无正文 → uncertain。
	body := `{"messages":[{"role":"user","content":"短剧剧本创作"}]}`
	prov := &stubProvider{bodies: map[string]string{"req-1": body}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"same_task\",\"confidence\":0.9}"}}]}`)
	}))
	defer srv.Close()
	gw := &SemanticGateway{BaseURL: srv.URL, Key: "k", Model: "m", Timeout: 5 * time.Second, MaxCallsPerReport: 10}
	issue := report.UsageIssue{
		Type: report.IssueTypeMultiTaskSession, Verdict: report.VerdictSuspected,
		SessionCount: 2, SessionShare: 1.0,
		AttributedCostUSD: 10, AffectedCostUSD: 10,
		Evidence: map[string]any{
			"hit_session_ids": []string{"s1", "s2"},
			"_attr_by_session": map[string]any{
				"s1": 6.0, "s2": 4.0,
			},
		},
	}
	sessions := []SessionFact{
		{SessionID: "s1", ReqIDs: []string{"req-1"}, PromptSeq: []int64{500, 500}},
		{SessionID: "s2", ReqIDs: []string{"req-2"}, PromptSeq: []int64{500, 500}},
	}
	up, ok := semanticUpgradeIssue(context.Background(), prov, gw, report.IssueTypeMultiTaskSession, issue, sessions, DefaultMultiTaskParams())
	if !ok {
		t.Fatal("语义应升级")
	}
	if up.Verdict != report.VerdictSuspected {
		t.Fatalf("无 affirm 应保持 suspected, got %q", up.Verdict)
	}
	// refute 扣 6，留 uncertain 的 4。
	if abs(up.AttributedCostUSD-4) > 1e-9 {
		t.Fatalf("refute 后应留 uncertain 会话费用 4, got %v", up.AttributedCostUSD)
	}
	if up.SessionCount != 1 {
		t.Fatalf("uncertain 会话数应=1, got %d", up.SessionCount)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// refutedIs 容忍 int/float64/json.Number 表示。
func refutedIs(v any, want int) bool {
	switch n := v.(type) {
	case int:
		return n == want
	case int64:
		return int(n) == want
	case float64:
		return int(n) == want
	default:
		return false
	}
}

// TestIntentSamples 意图样本应带段标记、截断、均匀覆盖。
func TestIntentSamples(t *testing.T) {
	var turns []intentTurn
	for i := 0; i < 10; i++ {
		turns = append(turns, intentTurn{Index: i + 1, Segment: i / 5, UserHead: string(make([]rune, 200))})
	}
	got := intentSamples(turns, 6)
	if len(got) != 6 {
		t.Fatalf("样本数=%d want 6", len(got))
	}
	if !strings.HasPrefix(got[5], "[段1] ") {
		t.Fatalf("尾部样本应带段1标记, got %q", got[5])
	}
	// 每条 ≤ 60+4（"…" 与 "[段N] " 前缀）。
	for _, g := range got {
		if len([]rune(g)) > 70 {
			t.Fatalf("样本超长: %d 字", len([]rune(g)))
		}
	}
}

// ---- 3.6 严重异常 LLM 语义识别（work_related 降低加班虚警）单元测试 ----

// TestSemanticUpgradeSafetyAnomaly_WorkRelated 验证：
// 非工作时段用量超标(>20%)，模型判定 work_related 且置信度达标(>=0.7)，
// 标记为「疑似加班未备案」，在报告中提示核实备案，降低虚警风险，
// 且结论天花板恒为「待核查」（status=pending_review），记录内容样本。
func TestSemanticUpgradeSafetyAnomaly_WorkRelated(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}

	body := `{"messages":[
		{"role":"user","content":"线上订单服务报死锁了，帮我排查下日志堆栈，同时写个修复补丁"},
		{"role":"assistant","content":"收到，分析如下..."},
		{"role":"user","content":"把修复补丁的单元测试也补一下"}
	]}`
	prov := &stubProvider{bodies: map[string]string{"req-night-1": body}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"work_related\",\"confidence\":0.88,\"reason_tag\":\"online_troubleshooting\"}"}}]}`)
	}))
	defer srv.Close()

	gw := &SemanticGateway{BaseURL: srv.URL, Key: "k", Model: "bf-glm-5.3-flash", Timeout: 5 * time.Second}
	sa := report.SafetyAnomaly{
		Hit:                true,
		UnrelatedCostUSD:   100,
		UnrelatedCostRatio: 0.35,
		TopHours:           []string{"01:00-03:00"},
		TopModel:           "gpt-4o",
		TopKey:             "k1",
		Status:             report.SafetyAnomalyPendingReview,
	}
	hitRows := []OffhoursCallRow{
		{RequestID: "req-night-1", UserID: "u1", CostUSD: 100},
	}

	up, ok := semanticUpgradeSafetyAnomaly(context.Background(), prov, gw, sa, hitRows)
	if !ok {
		t.Fatal("期望语义升级成功")
	}
	if up.ReviewTag != "疑似加班未备案" {
		t.Errorf("ReviewTag = %q, want 疑似加班未备案", up.ReviewTag)
	}
	if !strings.Contains(up.Hint, "疑似加班未备案") || !strings.Contains(up.Hint, "核实备案") {
		t.Errorf("Hint 必须提示核实备案降低虚警风险, got %q", up.Hint)
	}
	if up.Priority != "low" {
		t.Errorf("Priority = %q, want low (疑似加班误报，核查优先级低)", up.Priority)
	}
	if up.ContentSignal != "work_related" {
		t.Errorf("ContentSignal = %q, want work_related", up.ContentSignal)
	}
	if up.ContentConfidence != 0.88 {
		t.Errorf("ContentConfidence = %v, want 0.88", up.ContentConfidence)
	}
	// 规范铁律：无论大模型如何判定，结论天花板恒为待核查
	if up.Status != report.SafetyAnomalyPendingReview {
		t.Errorf("Status = %q, want %q", up.Status, report.SafetyAnomalyPendingReview)
	}
	// 内容样本必须记录
	if len(up.ContentSamples) == 0 {
		t.Fatal("必须记录正文内容样本供复核")
	}
	if !strings.Contains(up.ContentSamples[0], "线上订单服务报死锁") {
		t.Errorf("样本内容不匹配: %v", up.ContentSamples)
	}
}

// TestSemanticUpgradeSafetyAnomaly_LowConfidence 验证：
// 模型判定为 work_related 但置信度未达标（<0.7），保留为「待核查」，并记录内容样本。
func TestSemanticUpgradeSafetyAnomaly_LowConfidence(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}

	body := `{"messages":[{"role":"user","content":"写个小工具"}]}`
	prov := &stubProvider{bodies: map[string]string{"req-night-2": body}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"work_related\",\"confidence\":0.55,\"reason_tag\":\"unclear\"}"}}]}`)
	}))
	defer srv.Close()

	gw := &SemanticGateway{BaseURL: srv.URL, Key: "k", Model: "bf-glm-5.3-flash", Timeout: 5 * time.Second}
	sa := report.SafetyAnomaly{
		Hit:                true,
		UnrelatedCostUSD:   80,
		UnrelatedCostRatio: 0.25,
		Status:             report.SafetyAnomalyPendingReview,
	}
	hitRows := []OffhoursCallRow{
		{RequestID: "req-night-2", UserID: "u1", CostUSD: 80},
	}

	up, ok := semanticUpgradeSafetyAnomaly(context.Background(), prov, gw, sa, hitRows)
	if !ok {
		t.Fatal("期望执行语义分析")
	}
	if up.ReviewTag != "待核查" {
		t.Errorf("置信度未达标时必须保留为「待核查」, got %q", up.ReviewTag)
	}
	if up.Priority != "medium" {
		t.Errorf("Priority = %q, want medium", up.Priority)
	}
	if len(up.ContentSamples) == 0 {
		t.Fatal("必须记录内容样本")
	}
}

// TestSemanticUpgradeSafetyAnomaly_NotWorkRelated 验证：
// 模型判定为 not_work_related 且置信度达标(>=0.7)，保留为「待核查」，核查优先级升为高，记录内容样本。
func TestSemanticUpgradeSafetyAnomaly_NotWorkRelated(t *testing.T) {
	if !SemanticEnabled {
		SemanticEnabled = true
		defer func() { SemanticEnabled = false }()
	}

	body := `{"messages":[{"role":"user","content":"帮我写一封写给朋友的生日祝福卡片，包含一些好玩的段子"}]}`
	prov := &stubProvider{bodies: map[string]string{"req-night-3": body}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"signal\":\"not_work_related\",\"confidence\":0.85,\"reason_tag\":\"personal_letter\"}"}}]}`)
	}))
	defer srv.Close()

	gw := &SemanticGateway{BaseURL: srv.URL, Key: "k", Model: "bf-glm-5.3-flash", Timeout: 5 * time.Second}
	sa := report.SafetyAnomaly{
		Hit:                true,
		UnrelatedCostUSD:   120,
		UnrelatedCostRatio: 0.40,
		Status:             report.SafetyAnomalyPendingReview,
	}
	hitRows := []OffhoursCallRow{
		{RequestID: "req-night-3", UserID: "u1", CostUSD: 120},
	}

	up, ok := semanticUpgradeSafetyAnomaly(context.Background(), prov, gw, sa, hitRows)
	if !ok {
		t.Fatal("期望执行语义分析")
	}
	if up.ReviewTag != "待核查" {
		t.Errorf("非工作内容必须保留「待核查」（永不认定违纪）, got %q", up.ReviewTag)
	}
	if up.Priority != "high" {
		t.Errorf("Priority = %q, want high (时间+内容双线索，高核查优先级)", up.Priority)
	}
	if up.ContentSignal != "not_work_related" {
		t.Errorf("ContentSignal = %q, want not_work_related", up.ContentSignal)
	}
	if len(up.ContentSamples) == 0 {
		t.Fatal("必须记录内容样本")
	}
}

// TestSemanticUpgradeSafetyAnomaly_Disabled 验证：总开关关闭时直接短路回退。
func TestSemanticUpgradeSafetyAnomaly_Disabled(t *testing.T) {
	SemanticEnabled = false
	sa := report.SafetyAnomaly{Hit: true, Status: report.SafetyAnomalyPendingReview}
	up, ok := semanticUpgradeSafetyAnomaly(context.Background(), &stubProvider{}, &SemanticGateway{}, sa, nil)
	if ok {
		t.Fatal("关闭时不应触发升级")
	}
	if up.ReviewTag != "" && up.ReviewTag != "待核查" {
		t.Errorf("关闭时应保持默认, got %q", up.ReviewTag)
	}
}
