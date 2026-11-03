package report_test

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestBuildIssueNarrativeMultiTaskRefuted(t *testing.T) {
	issue := report.UsageIssue{
		Type:         report.IssueTypeMultiTaskSession,
		Verdict:      report.VerdictSuspected,
		SessionCount: 0,
		Evidence: map[string]any{
			"hit_sessions": 1,
			"semantic": map[string]any{
				"affirmed":  0,
				"refuted":   1,
				"uncertain": 0,
			},
		},
	}

	n := report.BuildIssueNarrative(issue)
	if !strings.Contains(n.Finding, "1 个长会话") {
		t.Fatalf("finding should preserve original candidate count: %q", n.Finding)
	}
	if !strings.Contains(n.SemanticReview, "仍围绕同一任务") || !strings.Contains(n.SemanticReview, "费用已排除") {
		t.Fatalf("semantic review is not human-readable: %q", n.SemanticReview)
	}
	for _, internalTerm := range []string{"refute", "P1", "CSA", "hit_session_ids"} {
		if strings.Contains(n.Finding+n.Reason+n.SemanticReview, internalTerm) {
			t.Fatalf("narrative leaked internal term %q: %+v", internalTerm, n)
		}
	}
}

func TestBuildIssueNarrativeUncertain(t *testing.T) {
	issue := report.UsageIssue{
		Type:         report.IssueTypeToothpaste,
		Verdict:      report.VerdictSuspected,
		SessionCount: 1,
		Evidence: map[string]any{
			"semantic": map[string]any{"uncertain": 1},
		},
	}
	n := report.BuildIssueNarrative(issue)
	if !strings.Contains(n.SemanticReview, "内容不足") || !strings.Contains(n.SemanticReview, "疑似") {
		t.Fatalf("unexpected uncertain narrative: %+v", n)
	}
}

func TestBuildSafetyNarrativeWorkRelated(t *testing.T) {
	n := report.BuildSafetyNarrative(report.SafetyAnomaly{
		UnrelatedCostRatio: 0.267,
		TopHours:           []string{"03:00-07:00"},
		ReviewTag:          "疑似加班未备案",
		ContentSignal:      "work_related",
		ContentConfidence:  0.85,
	})
	for _, want := range []string{"26.7%", "超过 20%", "03:00-07:00"} {
		if !strings.Contains(n.Trigger, want) {
			t.Fatalf("trigger missing %q: %+v", want, n)
		}
	}
	if !strings.Contains(n.ContentReview, "工作任务") || !strings.Contains(n.ContentReview, "85%") {
		t.Fatalf("content review should explain model result: %+v", n)
	}
	if !strings.Contains(n.Interpretation, "不代表与工作无关") || !strings.Contains(n.Action, "备案") {
		t.Fatalf("interpretation/action should guide manager review: %+v", n)
	}
}
