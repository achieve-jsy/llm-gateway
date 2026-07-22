package report_test

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

func TestBuildSafetyNarrativeExplainsUnfiledOvertime(t *testing.T) {
	n := report.BuildSafetyNarrative(report.SafetyAnomaly{
		UnrelatedCostRatio: 0.267,
		TopHours:           []string{"03:00-07:00"},
		ReviewTag:          "疑似加班未备案",
		ContentSignal:      "work_related",
		ContentConfidence:  0.85,
	})

	for _, required := range []string{"26.7%", "20%", "03:00-07:00", "OpenCode", "工作任务", "85%", "不代表与工作无关", "核实备案"} {
		if !strings.Contains(n.Trigger+n.ContentReview+n.Interpretation+n.Action, required) {
			t.Fatalf("safety narrative missing %q: %+v", required, n)
		}
	}
}

func TestBuildSafetyNarrativeDoesNotInventMissingRatio(t *testing.T) {
	n := report.BuildSafetyNarrative(report.SafetyAnomaly{Hit: true})
	if strings.Contains(n.Trigger, "超过 20%") || !strings.Contains(n.Trigger, "缺少完整") {
		t.Fatalf("incomplete snapshot should not invent a threshold result: %+v", n)
	}
}
