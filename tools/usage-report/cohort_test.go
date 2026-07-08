package main

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestPoolForReportsActualFallback(t *testing.T) {
	target := rosterEntry{UserID: "target", StudioID: "s1", JobCategory: "backend"}
	roster := map[string]rosterEntry{"target": target}
	valid := map[string]bool{}
	values := map[string]report.CohortMetrics{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		roster[id] = rosterEntry{UserID: id, StudioID: "s2", JobCategory: "backend"}
		valid[id] = true
		values[id] = report.CohortMetrics{ReqCount: int64(i + 1)}
	}
	members, poolType := poolFor(target, roster, valid, values)
	if len(members) != 5 || poolType != "全公司+岗位" {
		t.Fatalf("members=%d pool=%q", len(members), poolType)
	}
}

func TestPoolForExcludesTarget(t *testing.T) {
	target := rosterEntry{UserID: "target", StudioID: "s1", JobCategory: "backend"}
	roster := map[string]rosterEntry{"target": target}
	valid := map[string]bool{"target": true}
	values := map[string]report.CohortMetrics{"target": {ReqCount: 1}}
	members, _ := poolFor(target, roster, valid, values)
	if len(members) != 0 {
		t.Fatalf("target leaked into peer pool: %+v", members)
	}
}

// TestPoolForStudioCategoryPrimary 同 studio + 同岗位类别优先命中「工作室+岗位」（模板对标的首选池）。
func TestPoolForStudioCategoryPrimary(t *testing.T) {
	target := rosterEntry{UserID: "target", StudioID: "s1", JobCategory: "backend"}
	roster := map[string]rosterEntry{"target": target}
	valid := map[string]bool{}
	values := map[string]report.CohortMetrics{}
	// 5 个同 studio + 同 category 成员 → 命中「工作室+岗位」
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		roster[id] = rosterEntry{UserID: id, StudioID: "s1", JobCategory: "backend"}
		valid[id] = true
		values[id] = report.CohortMetrics{ReqCount: int64(i + 1)}
	}
	members, poolType := poolFor(target, roster, valid, values)
	if len(members) != 5 || poolType != "工作室+岗位" {
		t.Fatalf("members=%d pool=%q (want 5 / 工作室+岗位)", len(members), poolType)
	}
}
