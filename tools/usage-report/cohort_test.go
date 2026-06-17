package main

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestPoolForReportsActualFallback(t *testing.T) {
	target := rosterEntry{UserID: "target", StudioID: "s1", JobKey: "backend"}
	roster := map[string]rosterEntry{"target": target}
	valid := map[string]bool{}
	values := map[string]report.CohortMetrics{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		roster[id] = rosterEntry{UserID: id, StudioID: "s2", JobKey: "backend"}
		valid[id] = true
		values[id] = report.CohortMetrics{ReqCount: int64(i + 1)}
	}
	members, poolType := poolFor(target, roster, valid, values)
	if len(members) != 5 || poolType != "company+job_category" {
		t.Fatalf("members=%d pool=%q", len(members), poolType)
	}
}

func TestPoolForExcludesTarget(t *testing.T) {
	target := rosterEntry{UserID: "target", StudioID: "s1", JobKey: "backend"}
	roster := map[string]rosterEntry{"target": target}
	valid := map[string]bool{"target": true}
	values := map[string]report.CohortMetrics{"target": {ReqCount: 1}}
	members, _ := poolFor(target, roster, valid, values)
	if len(members) != 0 {
		t.Fatalf("target leaked into peer pool: %+v", members)
	}
}
