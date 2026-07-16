package gemini

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

// This file ports the Gemini tool-parameter schema cleaning pipeline that
// LiteLLM applies in `_build_vertex_schema`
// (litellm/llms/vertex_ai/common_utils.py). Gemini's function-declaration
// `parameters` field accepts a strict OpenAPI-style schema subset. OpenAI
// clients (OpenAI SDK, OpenCode, etc.) emit schemas that carry fields Gemini
// rejects outright (additionalProperties, strict, $schema, $ref/$defs, anyOf
// with null, arbitrary `format`), so we must normalize them before forwarding.
//
// The transform is lossy by design and mirrors LiteLLM: drop unknown fields,
// unroll $ref/$defs, fold anyOf null into `nullable`, coerce type arrays into
// anyOf, and fill required object/array scaffolding, then filter to the field
// whitelist Gemini actually accepts.

// geminiSchemaValidFields is the set of Schema keys Gemini accepts for
// function-declaration parameters. Anything outside this set is dropped so the
// upstream schema validator does not reject the request. It mirrors the keys
// LiteLLM derives from Gemini's Schema TypedDict
// (litellm/types/llms/vertex_ai.py): type, format, title, description,
// nullable, default, items, enum, properties, required, min/max*,
// minLength/maxLength, minimum/maximum, pattern, example, propertyOrdering,
// anyOf. Note: allOf/oneOf/exclusiveMin|Max/additionalProperties/$ref/$defs are
// intentionally absent — Gemini either rejects them or they are handled by the
// ref-unrolling and anyOf-null folds above.
var geminiSchemaValidFields = map[string]struct{}{
	"type":             {},
	"format":           {},
	"title":            {},
	"description":      {},
	"nullable":         {},
	"default":          {},
	"items":            {},
	"minItems":         {},
	"maxItems":         {},
	"enum":             {},
	"properties":       {},
	"required":         {},
	"minProperties":    {},
	"maxProperties":    {},
	"minimum":          {},
	"maximum":          {},
	"minLength":        {},
	"maxLength":        {},
	"pattern":          {},
	"example":          {},
	"propertyOrdering": {},
	"anyOf":            {},
}

// maxGeminiSchemaDepth bounds recursion so a malicious or accidental cyclic
// schema cannot overflow the stack.
const maxGeminiSchemaDepth = 20

// geminiToolParametersSchema converts an OpenAI tool.function.parameters value
// into a Gemini-acceptable function-declaration parameters schema. It mutates a
// decoded copy and returns the cleaned JSON. Errors are invalid-request (400)
// errors, consistent with the existing translation layer.
func geminiToolParametersSchema(raw any) (json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to marshal Gemini tool parameters", err)
	}
	if bytes.Equal(encoded, []byte("null")) {
		return nil, nil
	}

	node, err := decodeSchemaNode(encoded)
	if err != nil {
		return nil, core.NewInvalidRequestError("tool.function.parameters must be an object", nil)
	}
	schema, ok := node.(map[string]any)
	if !ok {
		return nil, core.NewInvalidRequestError("tool.function.parameters must be an object", nil)
	}

	cleaned, err := buildGeminiSchema(schema, 0)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(cleaned)
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to marshal Gemini tool parameters", err)
	}
	return out, nil
}

// decodeSchemaNode decodes JSON into map[string]any / []any trees. It rejects
// trailing garbage so malformed parameters fail loudly.
func decodeSchemaNode(encoded []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	var node any
	if err := dec.Decode(&node); err != nil {
		return nil, err
	}
	return node, nil
}

// buildGeminiSchema runs the full cleaning pipeline on an already-decoded
// schema object.
func buildGeminiSchema(schema map[string]any, depth int) (map[string]any, error) {
	if depth > maxGeminiSchemaDepth {
		return nil, core.NewInvalidRequestError("gemini tool parameters schema is too deeply nested", nil)
	}

	// Ensure the root is an object schema; Gemini requires type=object.
	rootType, hasType := schema["type"]
	if !hasType || rootType == nil {
		schema["type"] = "object"
	} else {
		switch t := rootType.(type) {
		case string:
			if strings.TrimSpace(t) == "" {
				schema["type"] = "object"
			} else if t != "object" {
				return nil, core.NewInvalidRequestError("tool.function.parameters must define an object schema", nil)
			}
		default:
			// Non-scalar type (array or nested) at the root is not an object schema.
			return nil, core.NewInvalidRequestError("tool.function.parameters must define an object schema", nil)
		}
	}

	// 1. Unroll $defs / $ref (OpenAI JSON Schema refs are non-OpenAPI and
	//    rejected by Gemini's schema validator).
	unrollSchemaRefs(schema)

	// 2. Normalize enum values (Gemini rejects empty strings in enums, and only
	//    allows enums on string-typed fields).
	fixSchemaEnums(schema, depth)

	// 3. Coerce type arrays into anyOf and normalize types.
	convertSchemaTypes(schema, depth)

	// 4. Fold anyOf null into a nullable flag on the sibling branches.
	convertAnyOfNullToNullable(schema, depth)

	// 5. Ensure array items carry a schema (Gemini requires items for arrays).
	processSchemaItems(schema, depth)

	// 6. Fill object scaffolding (empty properties, missing type).
	addObjectType(schema, depth)

	// 7. Filter to the Gemini-acceptable field whitelist and drop
	//    additionalProperties (Gemini does not support it on tool schemas).
	return filterSchemaFields(schema, geminiSchemaValidFields)
}

// unrollSchemaRefs resolves OpenAPI-style $defs + $ref references in place and
// removes the definitions container. Unknown refs (e.g. external pointers) are
// left as-is and subsequently stripped by the field filter.
func unrollSchemaRefs(schema map[string]any) {
	defs, _ := schema["$defs"].(map[string]any)
	if defs == nil {
		return
	}
	resolveRefs(schema, defs, 0)
	delete(schema, "$defs")
}

func resolveRefs(node any, defs map[string]any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			if name := refName(ref); name != "" {
				if def, found := defs[name]; found {
					if defMap, isMap := def.(map[string]any); isMap {
						// Merge referenced definition as the base, then overlay
						// any sibling keys carried next to the $ref, mutating v.
						merged := shallowCopyMap(defMap)
						for k, val := range v {
							if k == "$ref" || k == "$defs" {
								continue
							}
							merged[k] = val
						}
						for k := range v {
							delete(v, k)
						}
						for k, val := range merged {
							v[k] = val
						}
					}
				}
			}
		}
		for k, val := range v {
			switch k {
			case "$defs":
				continue
			default:
				resolveRefs(val, defs, depth+1)
			}
		}
	case []any:
		for _, item := range v {
			resolveRefs(item, defs, depth+1)
		}
	}
}

func refName(ref string) string {
	ref = strings.TrimSpace(ref)
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// fixSchemaEnums removes empty string enum entries and drops enums declared on
// non-string fields, both of which Gemini rejects.
func fixSchemaEnums(node any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if raw, ok := m["enum"]; ok {
		if entries, isList := raw.([]any); isList {
			cleaned := make([]any, 0, len(entries))
			for _, e := range entries {
				if s, isStr := e.(string); isStr && s == "" {
					continue
				}
				cleaned = append(cleaned, e)
			}
			m["enum"] = cleaned
		}
		typ, _ := m["type"].(string)
		if strings.TrimSpace(typ) != "" && typ != "string" {
			delete(m, "enum")
		}
	}
	for _, val := range m {
		fixSchemaEnums(val, depth+1)
	}
}

// convertSchemaTypes coerces OpenAPI type arrays into anyOf branches and keeps
// scalar types unchanged. Paths under properties/items/anyOf are recursed.
func convertSchemaTypes(node any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if typeVal, ok := m["type"]; ok {
		if typeList, isList := typeVal.([]any); isList {
			if len(typeList) > 1 {
				anyOf := make([]any, 0, len(typeList))
				typeSpecific := map[string]bool{
					"properties":           true,
					"required":             true,
					"additionalProperties": true,
					"items":                true,
					"minItems":             true,
					"maxItems":             true,
					"minProperties":        true,
					"maxProperties":        true,
				}
				hasObjOrArr := false
				for _, t := range typeList {
					ts, isStr := t.(string)
					if !isStr {
						continue
					}
					if ts == "null" {
						anyOf = append(anyOf, map[string]any{"type": "null"})
						continue
					}
					if ts == "object" || ts == "array" {
						item := map[string]any{"type": ts}
						for field := range typeSpecific {
							if val, ok := m[field]; ok {
								item[field] = val
							}
						}
						anyOf = append(anyOf, item)
						hasObjOrArr = true
						continue
					}
					anyOf = append(anyOf, map[string]any{"type": ts})
				}
				if hasObjOrArr {
					for field := range typeSpecific {
						delete(m, field)
					}
				}
				// Any multi-type list becomes anyOf (LiteLLM pops type
				// unconditionally once it has emitted anyOf branches), even if
				// it contained no object/array entry — e.g. ["string","null"].
				m["anyOf"] = anyOf
				delete(m, "type")
			} else if len(typeList) == 1 {
				if ts, isStr := typeList[0].(string); isStr {
					m["type"] = ts
				}
			}
		}
	}
	for _, key := range []string{"items", "anyOf"} {
		if val, ok := m[key]; ok {
			convertSchemaTypes(val, depth+1)
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, val := range props {
			convertSchemaTypes(val, depth+1)
		}
	}
}

// convertAnyOfNullToNullable removes explicit null branches from anyOf and
// marks the remaining branches nullable, matching Gemini's nullable convention.
func convertAnyOfNullToNullable(node any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if anyOfRaw, ok := m["anyOf"]; ok {
		if anyOf, isList := anyOfRaw.([]any); isList {
			containsNull := false
			nonNull := make([]any, 0, len(anyOf))
			for _, branch := range anyOf {
				if bm, isMap := branch.(map[string]any); isMap {
					if t, _ := bm["type"].(string); t == "null" {
						containsNull = true
						continue
					}
				}
				nonNull = append(nonNull, branch)
			}
			if containsNull {
				for _, branch := range nonNull {
					if bm, isMap := branch.(map[string]any); isMap {
						bm["nullable"] = true
					}
				}
				m["anyOf"] = nonNull
			}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, v := range props {
			convertAnyOfNullToNullable(v, depth+1)
		}
	}
	if items, ok := m["items"]; ok {
		convertAnyOfNullToNullable(items, depth+1)
	}
}

// processSchemaItems guarantees array schemas carry an `items` object, which
// Gemini requires; a bare `type: array` or empty `items` is filled.
func processSchemaItems(node any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if t, _ := m["type"].(string); t == "array" {
		if items, ok := m["items"]; !ok || items == nil || isEmptySchema(items) {
			m["items"] = map[string]any{"type": "object"}
		}
	}
	for _, val := range m {
		processSchemaItems(val, depth+1)
	}
}

func isEmptySchema(node any) bool {
	m, ok := node.(map[string]any)
	return ok && len(m) == 0
}

// addObjectType fills the object/array scaffolding Gemini demands: a node with
// properties but no type becomes type=object; empty properties are removed;
// a node with no type/anyOf/oneOf/allOf becomes type=object.
func addObjectType(node any, depth int) {
	if depth > maxGeminiSchemaDepth {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	_, hasType := m["type"]
	_, hasAnyOf := m["anyOf"]
	_, hasOneOf := m["oneOf"]
	_, hasAllOf := m["allOf"]
	if !hasType && !hasAnyOf && !hasOneOf && !hasAllOf {
		m["type"] = "object"
	}
	if props, ok := m["properties"].(map[string]any); ok {
		if required, ok := m["required"].([]any); ok && required == nil {
			delete(m, "required")
		}
		if len(props) == 0 {
			delete(m, "properties")
			delete(m, "required")
			m["type"] = "object"
		} else {
			m["type"] = "object"
			for _, v := range props {
				addObjectType(v, depth+1)
			}
		}
	}
	if items, ok := m["items"]; ok {
		addObjectType(items, depth+1)
	}
	if list, ok := m["anyOf"].([]any); ok {
		for _, v := range list {
			addObjectType(v, depth+1)
		}
	}
}

// filterSchemaFields recursively drops any key not in the Gemini whitelist.
// When a node carries anyOf it keeps only anyOf and its contents (Vertex/Gemini
// reject sibling keys alongside anyOf, see LiteLLM issue #11164), propagating
// title/description into each branch. additionalProperties and strict are
// dropped outright since Gemini does not accept them on tool schemas.
func filterSchemaFields(node any, valid map[string]struct{}) (map[string]any, error) {
	m, ok := node.(map[string]any)
	if !ok {
		return map[string]any{}, nil
	}

	// anyOf present → keep only anyOf (propagating title/description) so Gemini
	// does not reject the sibling keys, then filter each branch.
	if anyOfRaw, hasAnyOf := m["anyOf"]; hasAnyOf {
		if list, isList := anyOfRaw.([]any); isList {
			anyOf := []any{}
			for _, item := range list {
				if itemMap, isMap := item.(map[string]any); isMap {
					if title, ok := m["title"]; ok {
						itemMap["title"] = title
					}
					if desc, ok := m["description"]; ok {
						itemMap["description"] = desc
					}
					filtered, err := filterSchemaFields(itemMap, valid)
					if err != nil {
						return nil, err
					}
					anyOf = append(anyOf, filtered)
				}
			}
			return map[string]any{"anyOf": anyOf}, nil
		}
	}

	out := map[string]any{}
	for k, v := range m {
		switch k {
		case "additionalProperties":
			// Gemini does not accept additionalProperties on tool schemas.
			continue
		case "strict":
			continue
		case "format":
			// Gemini only accepts enum/date-time formats; drop everything else.
			if s, isStr := v.(string); isStr && (s == "enum" || s == "date-time") {
				out[k] = v
			}
			continue
		}
		if _, ok := valid[k]; !ok {
			continue
		}
		switch k {
		case "properties":
			if props, isMap := v.(map[string]any); isMap {
				cleaned := map[string]any{}
				for pk, pv := range props {
					filtered, err := filterSchemaFields(pv, valid)
					if err != nil {
						return nil, err
					}
					cleaned[pk] = filtered
				}
				out[k] = cleaned
			}
		case "items":
			if item, isMap := v.(map[string]any); isMap {
				filtered, err := filterSchemaFields(item, valid)
				if err != nil {
					return nil, err
				}
				out[k] = filtered
			} else {
				out[k] = v
			}
		case "anyOf":
			if list, isList := v.([]any); isList {
				cleaned := make([]any, 0, len(list))
				for _, item := range list {
					if itemMap, isMap := item.(map[string]any); isMap {
						filtered, err := filterSchemaFields(itemMap, valid)
						if err != nil {
							return nil, err
						}
						cleaned = append(cleaned, filtered)
					}
				}
				out[k] = cleaned
			}
		default:
			out[k] = v
		}
	}
	return out, nil
}

func shallowCopyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
