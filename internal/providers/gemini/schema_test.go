package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustParamSchema decodes a raw JSON schema into an any-tree for assertions.
func mustDecode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode cleaned schema: %v\nraw=%s", err, raw)
	}
	return m
}

func TestGeminiToolParametersSchema_StripsOpenAIOnlyFields(t *testing.T) {
	// OpenCode / OpenAI SDK emit additionalProperties + strict, both of which
	// Gemini's function-declaration schema validator rejects.
	raw := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"q": map[string]any{"type": "string"}},
		"required":             []any{"q"},
		"additionalProperties": false,
		"strict":               true,
		"$schema":              "http://json-schema.org/draft-2020-12/schema",
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)

	if _, ok := m["additionalProperties"]; ok {
		t.Errorf("additionalProperties should be stripped, got: %v", m["additionalProperties"])
	}
	if _, ok := m["strict"]; ok {
		t.Errorf("strict should be stripped")
	}
	if _, ok := m["$schema"]; ok {
		t.Errorf("$schema should be stripped")
	}
	if m["type"] != "object" {
		t.Errorf("type should remain object, got %v", m["type"])
	}
}

func TestGeminiToolParametersSchema_UnrollsDefsRefs(t *testing.T) {
	// $defs + $ref are non-OpenAPI and rejected by Gemini; must be unrolled.
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user": map[string]any{"$ref": "#/$defs/User"},
			"note": map[string]any{"type": "string"},
		},
		"$defs": map[string]any{
			"User": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)

	if _, ok := m["$defs"]; ok {
		t.Errorf("$defs should be removed after unrolling")
	}
	props := m["properties"].(map[string]any)
	user, ok := props["user"].(map[string]any)
	if !ok {
		t.Fatalf("user property missing: %v", props)
	}
	if _, ok := user["$ref"]; ok {
		t.Errorf("$ref should be unrolled")
	}
	if user["type"] != "object" {
		t.Errorf("unrolled user.type should be object, got %v", user["type"])
	}
	uprops := user["properties"].(map[string]any)
	if _, ok := uprops["id"]; !ok {
		t.Errorf("unrolled user should carry id property: %v", uprops)
	}
}

func TestGeminiToolParametersSchema_AnyOfNullBecomesNullable(t *testing.T) {
	// anyOf: [string, null] must be folded to nullable on the string branch.
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "null"},
				},
			},
		},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)
	props := m["properties"].(map[string]any)
	value := props["value"].(map[string]any)
	if _, ok := value["anyOf"]; !ok {
		t.Fatalf("value should retain anyOf after null fold: %v", value)
	}
	anyOf := value["anyOf"].([]any)
	if len(anyOf) != 1 {
		t.Fatalf("null branch should be removed, got %d branches", len(anyOf))
	}
	branch := anyOf[0].(map[string]any)
	if branch["nullable"] != true {
		t.Errorf("remaining branch should be nullable=true, got %v", branch["nullable"])
	}
}

func TestGeminiToolParametersSchema_FormatWhitelist(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"when": map[string]any{"type": "string", "format": "date-time"},
			"url":  map[string]any{"type": "string", "format": "uri"},
			"kind": map[string]any{"type": "string", "enum": []any{"a", "b"}},
		},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)
	props := m["properties"].(map[string]any)

	when := props["when"].(map[string]any)
	if when["format"] != "date-time" {
		t.Errorf("date-time format should be kept, got %v", when["format"])
	}
	url := props["url"].(map[string]any)
	if _, ok := url["format"]; ok {
		t.Errorf("uri format should be dropped, got %v", url["format"])
	}
}

func TestGeminiToolParametersSchema_EmptyPropertiesBecomeObject(t *testing.T) {
	raw := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)
	if _, ok := m["properties"]; ok {
		t.Errorf("empty properties should be removed")
	}
	if m["type"] != "object" {
		t.Errorf("type should be object after empty-property trim, got %v", m["type"])
	}
}

func TestGeminiToolParametersSchema_TypeArrayToAnyOf(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": []any{"string", "null"}},
		},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)
	props := m["properties"].(map[string]any)
	id := props["id"].(map[string]any)
	if _, ok := id["anyOf"]; !ok {
		t.Fatalf("type array should become anyOf, got: %v", id)
	}
}

func TestGeminiToolParametersSchema_ArrayRequiresItems(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{"type": "array", "items": map[string]any{}},
		},
	}
	cleaned, err := geminiToolParametersSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := mustDecode(t, cleaned)
	props := m["properties"].(map[string]any)
	tags := props["tags"].(map[string]any)
	items := tags["items"].(map[string]any)
	if items["type"] != "object" {
		t.Errorf("empty array items should become type=object, got %v", items)
	}
}

func TestGeminiToolParametersSchema_RootMustBeObject(t *testing.T) {
	cases := []map[string]any{
		{"type": "object"},        // ok
		{"type": "string"},        // not object → error
		{"type": []any{"object"}}, // type array at root → error
		{"properties": map[string]any{"a": map[string]any{"type": "string"}}}, // no type → object
	}
	for i, c := range cases {
		cleaned, err := geminiToolParametersSchema(c)
		if err != nil {
			if i == 1 || i == 2 {
				continue // expected errors
			}
			t.Fatalf("case %d: unexpected error: %v", i, err)
		}
		m := mustDecode(t, cleaned)
		if m["type"] != "object" {
			t.Errorf("case %d: root type should be object, got %v", i, m["type"])
		}
	}
}

func TestGeminiToolParametersSchema_NullParametersReturnsNil(t *testing.T) {
	cleaned, err := geminiToolParametersSchema(nil)
	if err != nil {
		t.Fatalf("nil parameters should not error: %v", err)
	}
	if cleaned != nil {
		t.Errorf("nil parameters should return nil, got %s", cleaned)
	}
}

func TestGeminiToolsFromOpenAI_UsesParametersField(t *testing.T) {
	// The function declaration must serialize parameters, never parametersJsonSchema.
	tools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "weather",
				"description": "get weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []any{"city"},
				},
			},
		},
	}
	out, err := geminiToolsFromOpenAI(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected one tool")
	}
	fd := out[0].FunctionDeclarations
	if len(fd) == 0 {
		t.Fatal("expected one function declaration")
	}
	if len(fd[0].Parameters) == 0 {
		t.Fatalf("parameters should be populated on the declaration")
	}
	// Assert parameters field is present and parametersJsonSchema absent in JSON.
	b, _ := json.Marshal(fd[0])
	s := string(b)
	if strings.Contains(s, "parametersJsonSchema") {
		t.Errorf("declaration must not carry parametersJsonSchema: %s", s)
	}
	if !strings.Contains(s, `"parameters"`) {
		t.Errorf("declaration must carry parameters field: %s", s)
	}
}
