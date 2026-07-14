package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

// TestEnumerateScopes_Department 枚举部门：/org/departments 返回 DeptID/DeptName/StudioID。
func TestEnumerateScopes_Department(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/departments" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"departments":[
			{"id":"d1","dept_id":"d-dev","dept_name":"研发部","studio_id":"s1"},
			{"id":"d2","dept_id":"d-sales","dept_name":"销售部","studio_id":"s2"}
		],"total":2}`))
	}))
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	scopes, err := enumerateScopes(context.Background(), cli, Config{Level: "department"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(scopes))
	}
	if scopes[0].ID != "d-dev" || scopes[0].Name != "研发部" || scopes[0].StudioID != "s1" || scopes[0].DeptID != "d-dev" {
		t.Errorf("dept scope wrong: %+v", scopes[0])
	}
}

// TestEnumerateScopes_Studio 枚举工作室：/org/studios 返回 ID/Name。
func TestEnumerateScopes_Studio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/studios" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"studios":[{"id":"s1","name":"工作室A"},{"id":"s2","name":"工作室B"}]}`))
	}))
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	scopes, err := enumerateScopes(context.Background(), cli, Config{Level: "studio"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0].ID != "s1" || scopes[0].Name != "工作室A" {
		t.Fatalf("studio scopes wrong: %+v", scopes)
	}
}

// TestEnumerateScopes_Personal_Dedup 枚举成员并去重：成员 ID 出现于两个部门时只保留一次。
func TestEnumerateScopes_Personal_Dedup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/org/departments":
			w.Write([]byte(`{"departments":[
				{"id":"d1","dept_id":"d-dev","dept_name":"研发部"},
				{"id":"d2","dept_id":"d-sales","dept_name":"销售部"}
			],"total":2}`))
		case strings.Contains(r.URL.Path, "/members"):
			// 两个部门都有 u1，且 d-dev 另有 u2 —— 验证按 ID 去重。
			if strings.Contains(r.URL.Path, "d-dev") {
				w.Write([]byte(`{"members":[
					{"id":"u1","name":"张三"},
					{"id":"u2","name":"李四"}
				],"total":2}`))
			} else {
				w.Write([]byte(`{"members":[
					{"id":"u1","name":"张三"}
				],"total":1}`))
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cli := client.New(srv.URL, "tok")
	scopes, err := enumerateScopes(context.Background(), cli, Config{Level: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 deduped scopes, got %d: %+v", len(scopes), scopes)
	}
	// 排序无关关键：应含 u1 与 u2 各一次。
	ids := map[string]bool{}
	for _, s := range scopes {
		ids[s.ID] = true
	}
	if !ids["u1"] || !ids["u2"] {
		t.Errorf("dedup failed, got %+v", ids)
	}
}

// TestBatchFileName 命名约定：level_scope_from_to.md，name 有值时用 name_id。
func TestBatchFileName(t *testing.T) {
	// slug 保留连字符（dept_id 的 '-' 不容丢失）；非 ASCII 名 slug 为空 → 用 shortHash(name) 前缀，
	// 保证多个共享同 id（如 DeptID="import"）的中文部门文件名唯一、不覆盖。
	if got := batchFileName("department", report.Scope{ID: "d-dev", Name: "研发部"}, "2026-06-25", "2026-07-01"); got != "department_n-f706f416_d-dev_2026-06-25_2026-07-01.md" {
		t.Errorf("batchFileName = %q", got)
	}
	// name 为空 → 用 id。
	if got := batchFileName("personal", report.Scope{ID: "u1"}, "2026-06-25", "2026-07-01"); got != "personal_u1_2026-06-25_2026-07-01.md" {
		t.Errorf("batchFileName = %q", got)
	}
	// 两个中文名部门共享同 id（DeptID="import"）→ 必须生成不同文件名，避免互相覆盖。
	a := batchFileName("department", report.Scope{ID: "import", Name: "人力资源部"}, "2026-06-25", "2026-07-01")
	b := batchFileName("department", report.Scope{ID: "import", Name: "运营部"}, "2026-06-25", "2026-07-01")
	if a == b {
		t.Errorf("共享同 id 的中文部门文件名不应相同: %q == %q", a, b)
	}
}

// TestRenderBatchSummary 汇总索引表格含表头与每对象一行。
func TestRenderBatchSummary(t *testing.T) {
	cfg := Config{Level: "department", From: "2026-06-25", To: "2026-07-01", Agg: true}
	items := []*BatchSummary{
		{Level: "department", ScopeID: "d-dev", ScopeName: "研发部", ReqCount: 100, SuccessCount: 95, ErrorCount: 5, TotalTokens: 1500, CostUSD: 5, SuccessRate: 0.95, CostPer1k: 3.33, KeyAnomalies: 2},
		{Level: "department", ScopeID: "d-sales", ScopeName: "销售部", ReqCount: 50, SuccessCount: 48, ErrorCount: 2, TotalTokens: 700, CostUSD: 2, SuccessRate: 0.96, CostPer1k: 2.86, KeyAnomalies: 0},
	}
	md := renderBatchSummary(cfg, items, 2, 0)
	if !strings.Contains(md, "研发部") || !strings.Contains(md, "销售部") {
		t.Errorf("summary missing object names")
	}
	if !strings.Contains(md, "A1-A5命中") || !strings.Contains(md, "| 研发部 | 100 |") {
		t.Errorf("summary table malformed:\n%s", md)
	}
}

// TestBuildSummary 从报告提取关键指标（个人事实层）。
func TestBuildSummary(t *testing.T) {
	rep := &report.Report{
		Meta: report.Meta{Level: "personal", Scope: report.Scope{ID: "u1", Name: "张三"}},
		Fact: report.Fact{Personal: &report.PersonalFact{ReqCount: 100, SuccessCount: 95, ErrorCount: 5, TotalTokens: 1500, EstCostUSD: 5}},
		Judgment: report.Judgment{
			Efficiency: report.Efficiency{SuccessRate: 0.95, CostPer1kTokens: 3.33},
			Anomalies: []report.AnomalyHit{
				{Rule: "A1", Severity: "P1", Hit: true},
				{Rule: "A3", Severity: "P2", Hit: true},
				{Rule: "A5", Severity: "P1", Hit: false},
			},
		},
	}
	s := buildSummary(rep)
	if s.ReqCount != 100 || s.SuccessCount != 95 || s.SuccessRate != 0.95 || s.KeyAnomalies != 2 {
		t.Errorf("buildSummary wrong: %+v", s)
	}
}
