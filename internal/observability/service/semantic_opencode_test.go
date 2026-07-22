package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseOpenCodeJSONLText(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`{"type":"step_start","sessionID":"s1","part":{"type":"step-start"}}`,
		`{"type":"text","sessionID":"s1","part":{"type":"text","text":"{\"signal\":\"same_task\",\"confidence\":0.91,\"reason_tag\":\"same_goal\"}"}}`,
		`{"type":"step_finish","sessionID":"s1","part":{"type":"step-finish"}}`,
	}, "\n"))

	sig, err := parseOpenCodeJSONL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Signal != "same_task" || sig.Confidence != 0.91 || sig.ReasonTag != "same_goal" {
		t.Fatalf("unexpected signal: %+v", sig)
	}
}

func TestParseOpenCodeJSONLStructured(t *testing.T) {
	raw := []byte(`{"type":"message","info":{"structured":{"signal":"task_switch","confidence":0.83,"switch_turns":[3]}}}`)
	sig, err := parseOpenCodeJSONL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Signal != "task_switch" || sig.Confidence != 0.83 || len(sig.SwitchTurns) != 1 || sig.SwitchTurns[0] != 3 {
		t.Fatalf("unexpected structured signal: %+v", sig)
	}
}

func TestParseOpenCodeJSONLError(t *testing.T) {
	raw := []byte(`{"type":"error","sessionID":"s1","error":{"name":"APIError","data":{"message":"model unavailable"}}}`)
	if _, err := parseOpenCodeJSONL(raw); err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("expected event error, got %v", err)
	}
}

func TestValidateSemanticSignal(t *testing.T) {
	if err := validateSemanticSignal(semanticSignal{Signal: "simple_task", Confidence: 0.8}); err != nil {
		t.Fatal(err)
	}
	if err := validateSemanticSignal(semanticSignal{Signal: "invented", Confidence: 0.8}); err == nil {
		t.Fatal("unknown signal should fail")
	}
	if err := validateSemanticSignal(semanticSignal{Signal: "same_task", Confidence: 1.2}); err == nil {
		t.Fatal("out-of-range confidence should fail")
	}
	if _, err := decodeSemanticSignal([]byte(`{"signal":"same_task","confidence":0.8,"unexpected":true}`)); err == nil {
		t.Fatal("unknown fields should fail strict decoding")
	}
}

func TestCallOpenCodeCLIUsesStdinAndParsesResult(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-opencode")
	body := `#!/bin/sh
if [ "$1" = "session" ] && [ "$2" = "delete" ]; then
  exit 0
fi
input=$(cat)
case "$input" in
  *"待判定证据如下"*) ;;
  *) exit 7 ;;
esac
printf '%s\n' '{"type":"text","sessionID":"session-test","part":{"type":"text","text":"{\"signal\":\"same_task\",\"confidence\":0.92}"}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	sig, err := callOpenCodeCLI(context.Background(), SemanticGateway{
		Backend: SemanticBackendOpenCode,
		Command: script,
		Timeout: 2 * time.Second,
	}, "system rubric", "evidence")
	if err != nil {
		t.Fatal(err)
	}
	if sig.Signal != "same_task" || sig.Confidence != 0.92 {
		t.Fatalf("unexpected signal: %+v", sig)
	}
}

func TestDefaultSemanticGatewayOpenCode(t *testing.T) {
	previousEnabled := SemanticEnabled
	defer func() { SemanticEnabled = previousEnabled }()
	t.Setenv("SEMANTIC_ENABLED", "true")
	t.Setenv("SEMANTIC_BACKEND", "opencode")
	t.Setenv("SEMANTIC_OPENCODE_BIN", "/opt/tools/opencode")
	t.Setenv("SEMANTIC_OPENCODE_AGENT", "token-usage-judge")
	t.Setenv("SEMANTIC_OPENCODE_MODEL", "provider/model")
	t.Setenv("SEMANTIC_OPENCODE_PURE", "true")

	gw := DefaultSemanticGateway()
	if gw == nil || gw.Backend != SemanticBackendOpenCode {
		t.Fatalf("unexpected gateway: %+v", gw)
	}
	if gw.Command != "/opt/tools/opencode" || gw.Agent != "token-usage-judge" || gw.Model != "provider/model" {
		t.Fatalf("unexpected opencode config: %+v", gw)
	}
	if !gw.Pure {
		t.Fatal("expected OpenCode pure mode")
	}
}

func TestValidateSemanticGatewayConfig(t *testing.T) {
	if err := ValidateSemanticGatewayConfig(context.Background(), &SemanticGateway{Backend: "bogus"}); err == nil {
		t.Fatal("unsupported backend should fail")
	}
	if err := ValidateSemanticGatewayConfig(context.Background(), &SemanticGateway{Backend: SemanticBackendHTTP}); err == nil {
		t.Fatal("incomplete HTTP backend should fail")
	}
	if err := ValidateSemanticGatewayConfig(context.Background(), &SemanticGateway{
		Backend: SemanticBackendOpenCode,
		Command: filepath.Join(t.TempDir(), "missing-opencode"),
	}); err == nil {
		t.Fatal("missing OpenCode executable should fail")
	}
}

func TestValidateSemanticGatewayConfigOpenCodeSuccess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-opencode")
	body := `#!/bin/sh
if [ "$1" = "agent" ] && [ "$2" = "list" ]; then
  printf '%s\n' 'token-usage-judge (primary)'
  exit 0
fi
if [ "$1" = "models" ] && [ "$2" = "provider" ]; then
  printf '%s\n' 'provider/model'
  exit 0
fi
exit 9
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	err := ValidateSemanticGatewayConfig(context.Background(), &SemanticGateway{
		Backend: SemanticBackendOpenCode,
		Command: script,
		Agent:   "token-usage-judge",
		Model:   "provider/model",
	})
	if err != nil {
		t.Fatal(err)
	}
}
