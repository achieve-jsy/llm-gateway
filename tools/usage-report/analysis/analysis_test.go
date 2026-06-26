package analysis

// analysis_test.go —— Generate 单测：各 level prompt 构造 / Sections 回填 / LLM 失败与未启用降级。
// 用 httptest 模拟 OpenAI 兼容 /chat/completions。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/llm"
)

// chatReq 捕获发给 LLM 的请求体（用于断言 prompt 内容与敏感字段排查）。
type chatReq struct {
	Model    string       `json:"model"`
	Messages []messageReq `json:"messages"`
}

type messageReq struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// newTestReport 构造带基本事实层的 Report（仅供 Generate 消费；不触发 derive/validate）。
func newTestReport(level string) *report.Report {
	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level: level,
			Period: report.Period{
				From: "2025-01-06", To: "2025-01-12", Days: 7,
				Type: "week", Label: "week 2025-01-06~2025-01-12",
			},
			Scope:    report.Scope{Level: level, ID: "u-1", Name: "张三"},
			Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2},
		},
	}
	switch level {
	case "personal":
		r.Fact.Personal = &report.PersonalFact{
			ReqCount: 1200, SuccessCount: 1180, ModelCount: 3,
			EstCostUSD: 12.34, TotalTokens: 3_500_000,
			CacheHitRate: 0.42, TopModel: "gpt-4o", HighTierCostRatio: 0.66,
			TopModels: []report.TopModel{
				{Model: "gpt-4o", Calls: 600, CostUSD: 8.2, TotalTokens: 1_800_000},
				{Model: "claude-3.5", Calls: 400, CostUSD: 3.6, TotalTokens: 1_200_000},
			},
		}
		r.Judgment.Efficiency = report.Efficiency{SuccessRate: 0.983, CostPer1kTokens: 0.0035, CacheHitRate: 0.42}
	case "department":
		r.Fact.Department = &report.UnitFact{
			Member: report.UnitMember{
				ReqCount: 5000, ModelCount: 4, EstCostUSD: 45.6, CacheHitRate: 0.3,
				TopModels: []report.TopModel{{Model: "gpt-4o", Calls: 2000, CostUSD: 30, TotalTokens: 8_000_000}},
			},
			ServiceKey: report.UnitService{ReqCount: 300, EstCostUSD: 5.2},
			JobGroups:  []report.JobGroup{{JobCategory: "后端", ActiveUsers: 8, AvgCostUSD: 5.7, TopModel: "gpt-4o"}},
		}
	case "studio":
		r.Fact.Studio = &report.UnitFact{
			Member: report.UnitMember{
				ReqCount: 20000, ModelCount: 6, EstCostUSD: 180.5, CacheHitRate: 0.25,
				TopModels: []report.TopModel{{Model: "gpt-4o", Calls: 8000, CostUSD: 120, TotalTokens: 30_000_000}},
			},
			ServiceKey:   report.UnitService{ReqCount: 1200, EstCostUSD: 22.1},
			CompanyRatio: 0.18, CostConcentration: 0.55, OptimizableCostUSD: 30.0,
			DeptContributions: []report.DeptContribution{{DeptName: "后端组", CostUSD: 60, CostRatio: 0.33}},
		}
		r.Judgment.Attribution = []report.Attribution{{Subject: "模型升级", Detail: "切换至高价模型", Weight: 0.7}}
	}
	return r
}

// lvlRequiredKeyword 各 level 的分析要求关键字（用于断言 prompt 差异化）。
func lvlRequiredKeyword(level string) string {
	switch level {
	case "personal":
		return "场景与合理用模"
	case "department":
		return "模型组合效率"
	case "studio":
		return "模型策略"
	default:
		return ""
	}
}

// newLLM 构造一个指向给定 server 的 llm.Client（固定 model + 测试 key）。
func newLLM(srv *httptest.Server) *llm.Client {
	return llm.New(srv.URL, "test-key", "gpt-4o")
}

func TestGenerate_NilCLI(t *testing.T) {
	ma, err := Generate(context.Background(), nil, "personal", newTestReport("personal"), 7.2)
	if err != nil {
		t.Fatalf("nil cli should not error, got %v", err)
	}
	if ma == nil || ma.Level != "personal" || ma.Sections != "" || ma.Note == "" {
		t.Fatalf("expected degraded analysis with note, got %+v", ma)
	}
}

func TestGenerate_NilReport(t *testing.T) {
	ma, err := Generate(context.Background(), &llm.Client{}, "personal", nil, 7.2)
	if err == nil {
		t.Fatal("nil report should error")
	}
	if ma == nil || ma.Note == "" {
		t.Fatalf("expected degraded note, got %+v", ma)
	}
}

func TestGenerate_Success(t *testing.T) {
	for _, level := range []string{"personal", "department", "studio"} {
		lvl := level
		t.Run(lvl, func(t *testing.T) {
			var got chatReq
			spanPrompt := "## 模型分析\n\n- 结论先行，建议用模优化"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Errorf("want /chat/completions, got %s", r.URL.Path)
				}
				if gotAuth := r.Header.Get("Authorization"); gotAuth != "Bearer test-key" {
					t.Errorf("unexpected auth header %q", gotAuth)
				}
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &got)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"choices": []map[string]interface{}{
						{"message": map[string]interface{}{"role": "assistant", "content": spanPrompt}},
					},
				})
			}))
			defer srv.Close()

			ma, err := Generate(context.Background(), newLLM(srv), lvl, newTestReport(lvl), 7.2)
			if err != nil {
				t.Fatalf("Generate(%s) error: %v", lvl, err)
			}
			if ma == nil || ma.Sections != spanPrompt {
				t.Fatalf("expected sections=%q, got %+v", spanPrompt, ma)
			}
			if ma.Note != "" {
				t.Fatalf("unexpected note on success: %q", ma.Note)
			}
			if len(got.Messages) != 2 {
				t.Fatalf("expected 2 messages, got %d", len(got.Messages))
			}
			user := got.Messages[1].Content
			if !strings.Contains(user, lvlRequiredKeyword(lvl)) {
				t.Errorf("%s prompt missing level keyword %q", lvl, lvlRequiredKeyword(lvl))
			}
			if !strings.Contains(user, "fx_rate_usd_cny") {
				t.Errorf("prompt missing fx rate")
			}
			// API key / 敏感字段不得出现在 prompt 中。
			for _, m := range got.Messages {
				if strings.Contains(m.Content, "test-key") {
					t.Errorf("API key leaked into %s prompt", lvl)
				}
			}
		})
	}
}

func TestGenerate_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ma, err := Generate(context.Background(), newLLM(srv), "personal", newTestReport("personal"), 7.2)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if ma == nil || ma.Note == "" || ma.Sections != "" {
		t.Fatalf("expected degraded note (no sections), got %+v", ma)
	}
}

func TestGenerate_EmptyOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "   "}},
			},
		})
	}))
	defer srv.Close()

	ma, err := Generate(context.Background(), newLLM(srv), "department", newTestReport("department"), 7.2)
	if err == nil {
		t.Fatal("expected error on empty output")
	}
	if ma == nil || ma.Note == "" {
		t.Fatalf("expected degraded note, got %+v", ma)
	}
}
