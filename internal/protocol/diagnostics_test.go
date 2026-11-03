// Package protocol_test tests the protocol diagnostics system.
package protocol_test

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/protocol"
)

func makeSession() *protocol.Session {
	return &protocol.Session{
		SourceFormat: types.RelayFormatClaude,
		TargetFormat: types.RelayFormatOpenAI,
	}
}

func TestCollectDiagnostics_NoLoss(t *testing.T) {
	s := makeSession()
	result := protocol.CollectDiagnostics(s, nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty diagnostics, got %d", len(result))
	}
}

func TestCollectDiagnostics_SingleWarning(t *testing.T) {
	s := makeSession()
	relayDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Message: "document content not supported", Severity: types.ConversionDiagnosticWarning},
	}
	result := protocol.CollectDiagnostics(s, relayDiags, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(result))
	}
	if result[0].Code != "protocol.unsupported.document" {
		t.Errorf("expected code 'protocol.unsupported.document', got %q", result[0].Code)
	}
	if result[0].From != types.RelayFormatClaude {
		t.Errorf("expected From=claude, got %s", result[0].From)
	}
	if result[0].To != types.RelayFormatOpenAI {
		t.Errorf("expected To=openai, got %s", result[0].To)
	}
}

func TestCollectDiagnostics_MultipleMixed(t *testing.T) {
	s := makeSession()
	relayDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Message: "document not supported", Severity: types.ConversionDiagnosticWarning},
		{Code: "protocol.unsupported.tool", Message: "tool not supported", Severity: types.ConversionDiagnosticError},
	}
	bridgeDiags := []types.ConversionDiagnostic{
		{Code: "protocol.bridge.temperature", Message: "temperature rounding", Severity: types.ConversionDiagnosticWarning},
	}
	result := protocol.CollectDiagnostics(s, relayDiags, bridgeDiags)
	if len(result) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(result))
	}
	// Verify all severity levels present
	hasWarning := false
	hasError := false
	for _, d := range result {
		if d.Severity == types.ConversionDiagnosticWarning {
			hasWarning = true
		}
		if d.Severity == types.ConversionDiagnosticError {
			hasError = true
		}
		if d.From != types.RelayFormatClaude || d.To != types.RelayFormatOpenAI {
			t.Errorf("diagnostic %q: From/To not set from session", d.Code)
		}
	}
	if !hasWarning {
		t.Error("expected at least one warning diagnostic")
	}
	if !hasError {
		t.Error("expected at least one error diagnostic")
	}
}

func TestCollectDiagnostics_Deduplicate(t *testing.T) {
	s := makeSession()
	relayDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Message: "doc1", Severity: types.ConversionDiagnosticWarning},
	}
	bridgeDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Message: "doc2", Severity: types.ConversionDiagnosticWarning},
		{Code: "protocol.bridge.unique", Message: "unique", Severity: types.ConversionDiagnosticWarning},
	}
	result := protocol.CollectDiagnostics(s, relayDiags, bridgeDiags)
	// "protocol.unsupported.document" should appear only once
	if len(result) != 2 {
		t.Fatalf("expected 2 diagnostics after dedup, got %d", len(result))
	}
	seen := map[string]int{}
	for _, d := range result {
		seen[d.Code]++
	}
	if seen["protocol.unsupported.document"] != 1 {
		t.Errorf("expected 'protocol.unsupported.document' once, got %d", seen["protocol.unsupported.document"])
	}
	if seen["protocol.bridge.unique"] != 1 {
		t.Errorf("expected 'protocol.bridge.unique' once, got %d", seen["protocol.bridge.unique"])
	}
}

func TestCollectDiagnostics_EmptySession(t *testing.T) {
	// nil session should not panic
	var s *protocol.Session
	result := protocol.CollectDiagnostics(s, nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty diagnostics for nil session, got %d", len(result))
	}
}

func TestFormatLossHeader_Normal(t *testing.T) {
	diags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document"},
		{Code: "protocol.bridge.temperature"},
		{Code: "protocol.unsupported.tool"},
	}
	header := protocol.FormatLossHeader(diags)
	expected := "protocol.unsupported.document,protocol.bridge.temperature,protocol.unsupported.tool"
	if header != expected {
		t.Errorf("expected %q, got %q", expected, header)
	}
}

func TestFormatLossHeader_Truncation(t *testing.T) {
	// Create codes that will exceed 512 ASCII bytes
	longCode := "protocol.very.long.code.name.that.takes.up.space."
	diags := make([]types.ConversionDiagnostic, 0, 30)
	for i := 0; i < 30; i++ {
		diags = append(diags, types.ConversionDiagnostic{Code: longCode})
	}
	header := protocol.FormatLossHeader(diags)
	if len(header) > 512 {
		t.Errorf("header exceeds 512 bytes: %d bytes", len(header))
	}
	// Should end with "..." when truncated
	if len(header) < (len(longCode)*30+29) {
		// It was truncated
		if last3 := header[len(header)-3:]; last3 != "..." {
			t.Errorf("truncated header should end with '...', got ending %q (full len=%d)", last3, len(header))
		}
	}
}

func TestFormatLossHeader_Empty(t *testing.T) {
	header := protocol.FormatLossHeader(nil)
	if header != "" {
		t.Errorf("expected empty string for nil diags, got %q", header)
	}
	header = protocol.FormatLossHeader([]types.ConversionDiagnostic{})
	if header != "" {
		t.Errorf("expected empty string for empty diags, got %q", header)
	}
}

func TestFormatLossHeader_DeduplicatesInHeader(t *testing.T) {
	// FormatLossHeader should not deduplicate (that's CollectDiagnostics' job),
	// but it should still produce comma-separated codes
	diags := []types.ConversionDiagnostic{
		{Code: "protocol.a"},
		{Code: "protocol.b"},
		{Code: "protocol.a"},
	}
	header := protocol.FormatLossHeader(diags)
	expected := "protocol.a,protocol.b,protocol.a"
	if header != expected {
		t.Errorf("expected %q, got %q", expected, header)
	}
}

func TestApplyLossPolicy_Allow(t *testing.T) {
	diags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Severity: types.ConversionDiagnosticError},
		{Code: "protocol.bridge.temperature", Severity: types.ConversionDiagnosticWarning},
	}
	err := protocol.ApplyLossPolicy(types.ConversionLossPolicyAllow, diags)
	if err != nil {
		t.Errorf("allow policy should never reject, got: %v", err)
	}
}

func TestApplyLossPolicy_Safe(t *testing.T) {
	// safe: errors reject, warnings pass
	warnDiags := []types.ConversionDiagnostic{
		{Code: "protocol.bridge.temperature", Severity: types.ConversionDiagnosticWarning},
	}
	err := protocol.ApplyLossPolicy(types.ConversionLossPolicySafe, warnDiags)
	if err != nil {
		t.Errorf("safe policy should not reject warnings, got: %v", err)
	}

	errDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Severity: types.ConversionDiagnosticError},
	}
	err = protocol.ApplyLossPolicy(types.ConversionLossPolicySafe, errDiags)
	if err == nil {
		t.Error("safe policy should reject errors")
	}
}

func TestApplyLossPolicy_Strict(t *testing.T) {
	// strict: all diagnostics reject
	warnDiags := []types.ConversionDiagnostic{
		{Code: "protocol.bridge.temperature", Severity: types.ConversionDiagnosticWarning},
	}
	err := protocol.ApplyLossPolicy(types.ConversionLossPolicyStrict, warnDiags)
	if err == nil {
		t.Error("strict policy should reject warnings")
	}

	errDiags := []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.document", Severity: types.ConversionDiagnosticError},
	}
	err = protocol.ApplyLossPolicy(types.ConversionLossPolicyStrict, errDiags)
	if err == nil {
		t.Error("strict policy should reject errors")
	}
}

func TestApplyLossPolicy_Empty(t *testing.T) {
	// No diagnostics should never reject regardless of policy
	err := protocol.ApplyLossPolicy(types.ConversionLossPolicyStrict, nil)
	if err != nil {
		t.Errorf("empty diags should never reject, got: %v", err)
	}
	err = protocol.ApplyLossPolicy(types.ConversionLossPolicyStrict, []types.ConversionDiagnostic{})
	if err != nil {
		t.Errorf("empty diags should never reject, got: %v", err)
	}
}

func TestApplyLossPolicy_DefaultPolicy(t *testing.T) {
	// Empty policy string should default to "allow"
	diags := []types.ConversionDiagnostic{
		{Code: "protocol.error", Severity: types.ConversionDiagnosticError},
	}
	err := protocol.ApplyLossPolicy("", diags)
	if err != nil {
		t.Errorf("empty/unknown policy should default to allow, got: %v", err)
	}
}

func TestConversionError_Error(t *testing.T) {
	ce := &protocol.ConversionError{
		Diagnostics: []types.ConversionDiagnostic{
			{Code: "protocol.unsupported.document", Message: "document conversion lost"},
			{Code: "protocol.unsupported.tool", Message: "tool lost"},
		},
		HTTPStatus: 400,
	}
	msg := ce.Error()
	if msg == "" {
		t.Error("Error() should return non-empty message")
	}
	// Error() uses diagnostic messages, not codes
	if !contains(msg, "document conversion lost") || !contains(msg, "tool lost") {
		t.Errorf("Error() message missing diagnostic messages: %q", msg)
	}
	// Should also include the policy prefix
	if !contains(msg, "conversion rejected by loss policy") {
		t.Errorf("Error() missing policy prefix: %q", msg)
	}
}

func TestConversionError_Empty(t *testing.T) {
	ce := &protocol.ConversionError{
		HTTPStatus: 400,
	}
	msg := ce.Error()
	if msg == "" {
		t.Error("Error() on empty diagnostics should still return a message")
	}
}

func TestConversionError_HTTPStatus(t *testing.T) {
	ce := &protocol.ConversionError{
		Diagnostics: []types.ConversionDiagnostic{
			{Code: "protocol.error", Severity: types.ConversionDiagnosticError},
		},
		HTTPStatus: 422,
	}
	if ce.HTTPStatus != 422 {
		t.Errorf("expected HTTPStatus 422, got %d", ce.HTTPStatus)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
