package report_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func validReport(level string) report.Report {
	r := report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       level,
			Period:      report.Period{From: "2024-02-28", To: "2024-03-01", Type: "custom", Days: 3, Label: "跨月"},
			Scope:       report.Scope{Level: level, ID: "scope-1", Name: "研发 | 平台"},
			GeneratedAt: "2026-08-27T10:00:00+08:00", DataRetrievedAt: "2026-08-27T10:00:01+08:00",
			Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2},
		},
	}
	switch level {
	case "personal":
		r.Fact.Personal = &report.PersonalFact{ScopeID: "scope-1"}
	case "department":
		r.Fact.Department = &report.UnitFact{ScopeID: "scope-1"}
	case "studio":
		r.Fact.Studio = &report.UnitFact{ScopeID: "scope-1"}
	}
	return r
}

func TestReportJSONRoundTripAndValidate(t *testing.T) {
	for _, level := range []string{"personal", "department", "studio"} {
		t.Run(level, func(t *testing.T) {
			want := validReport(level)
			if err := want.Validate(); err != nil {
				t.Fatalf("valid report rejected: %v", err)
			}
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var got report.Report
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("round-tripped report rejected: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round trip changed report\nwant=%#v\ngot=%#v", want, got)
			}
		})
	}
}

func TestReportValidateRejectsBrokenMetadata(t *testing.T) {
	tests := []struct {
		name string
		edit func(*report.Report)
	}{
		{"level mismatch", func(r *report.Report) { r.Meta.Scope.Level = "studio" }},
		{"multiple facts", func(r *report.Report) { r.Fact.Studio = &report.UnitFact{} }},
		{"wrong inclusive days", func(r *report.Report) { r.Meta.Period.Days = 2 }},
		{"bad generated timestamp", func(r *report.Report) { r.Meta.GeneratedAt = "today" }},
		{"bad retrieved timestamp", func(r *report.Report) { r.Meta.DataRetrievedAt = "today" }},
		{"nonpositive currency", func(r *report.Report) { r.Meta.Currency.USDCNYRate = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validReport("personal")
			tt.edit(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
