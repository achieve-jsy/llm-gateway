package report_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// 人少（<30）：按 dominant_issue 分色，不上算法。
func TestClusterMembers_FewByDominant(t *testing.T) {
	inputs := []report.MemberQuadrantInput{
		{UserID: "a", IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 0.5}},
		{UserID: "b", IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 0.2}},
		{UserID: "c", IssueAttributed: map[string]float64{report.IssueTypeOutputRunaway: 0.4}},
		{UserID: "d", IssueAttributed: map[string]float64{}},
	}
	clusters := report.ClusterMembers(inputs, report.DefaultClusterParams())
	if len(clusters) != 3 {
		t.Fatalf("clusters=%d, want 3", len(clusters))
	}
	labelToIDs := map[string][]string{}
	for _, c := range clusters {
		labelToIDs[c.Label] = c.MemberIDs
	}
	if len(labelToIDs[report.IssueTypePrefixPollution]) != 2 {
		t.Errorf("prefix cluster members=%v, want 2", labelToIDs[report.IssueTypePrefixPollution])
	}
	if len(labelToIDs[report.IssueTypeOutputRunaway]) != 1 {
		t.Errorf("output cluster members=%v, want 1", labelToIDs[report.IssueTypeOutputRunaway])
	}
	if len(labelToIDs[report.IssueTypeNormal]) != 1 {
		t.Errorf("normal cluster members=%v, want 1", labelToIDs[report.IssueTypeNormal])
	}
}

// 人多（>=30）：KMeans 跑通且分区完整、无重漏。
func TestClusterMembers_ManyKMeans(t *testing.T) {
	inputs := make([]report.MemberQuadrantInput, 0, 40)
	// 20 人主打前缀污染，20 人主打输出失控。
	for i := 0; i < 20; i++ {
		inputs = append(inputs, report.MemberQuadrantInput{UserID: itoa("p", i), IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 0.9}})
	}
	for i := 0; i < 20; i++ {
		inputs = append(inputs, report.MemberQuadrantInput{UserID: itoa("o", i), IssueAttributed: map[string]float64{report.IssueTypeOutputRunaway: 0.9}})
	}
	clusters := report.ClusterMembers(inputs, report.DefaultClusterParams())
	if len(clusters) == 0 {
		t.Fatal("kmeans produced no clusters")
	}
	seen := map[string]bool{}
	total := 0
	for _, c := range clusters {
		if c.Label == "" {
			t.Errorf("cluster with empty label: %v", c.MemberIDs)
		}
		for _, id := range c.MemberIDs {
			if seen[id] {
				t.Errorf("member %s duplicated", id)
			}
			seen[id] = true
			total++
		}
	}
	if total != 40 {
		t.Errorf("total member ids=%d, want 40 (no drop, no dup)", total)
	}
	// 40 人全部被分进某个簇。
	for _, in := range inputs {
		if !seen[in.UserID] {
			t.Errorf("member %s not clustered", in.UserID)
		}
	}
}

func itoa(prefix string, i int) string {
	// 简单整数转字符串。
	if i == 0 {
		return prefix + "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return prefix + string(b)
}
