package core

import (
	"encoding/json"
	"testing"
)

func TestRerankRequestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    RerankRequest
		wantErr bool
	}{
		{
			name: "basic fields",
			json: `{
				"model": "bge-reranker-v2-m3",
				"query": "What is the capital of France?",
				"documents": ["Paris is the capital of France.", "London is the capital of the UK."]
			}`,
			want: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Query:     "What is the capital of France?",
				Documents: []string{"Paris is the capital of France.", "London is the capital of the UK."},
			},
		},
		{
			name: "with provider field",
			json: `{
				"model": "bge-reranker-v2-m3",
				"provider": "zhipu",
				"query": "What is the capital of France?",
				"documents": ["Paris is the capital of France."]
			}`,
			want: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Provider:  "zhipu",
				Query:     "What is the capital of France?",
				Documents: []string{"Paris is the capital of France."},
			},
		},
		{
			name: "with optional fields",
			json: `{
				"model": "bge-reranker-v2-m3",
				"query": "test query",
				"documents": ["doc1", "doc2"],
				"top_n": 5,
				"return_documents": true,
				"max_chunks_per_doc": 3
			}`,
			want: RerankRequest{
				Model:           "bge-reranker-v2-m3",
				Query:           "test query",
				Documents:       []string{"doc1", "doc2"},
				TopN:            intPtr(5),
				ReturnDocuments: boolPtr(true),
				MaxChunksPerDoc: intPtr(3),
			},
		},
		{
			name: "with extra fields",
			json: `{
				"model": "bge-reranker-v2-m3",
				"query": "test query",
				"documents": ["doc1"],
				"custom_field": "custom_value",
				"another_field": 42
			}`,
			want: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Query:     "test query",
				Documents: []string{"doc1"},
				ExtraFields: UnknownJSONFields{
					raw: json.RawMessage(`{"another_field":42,"custom_field":"custom_value"}`),
				},
			},
		},
		{
			name: "empty documents",
			json: `{
				"model": "bge-reranker-v2-m3",
				"query": "test query",
				"documents": []
			}`,
			want: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Query:     "test query",
				Documents: []string{},
			},
		},
		{
			name:    "invalid JSON",
			json:    `{invalid}`,
			wantErr: true,
		},
		{
			name:    "non-object JSON",
			json:    `["array"]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RerankRequest
			err := json.Unmarshal([]byte(tt.json), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", got.Model, tt.want.Model)
			}
			if got.Provider != tt.want.Provider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.want.Provider)
			}
			if got.Query != tt.want.Query {
				t.Errorf("Query = %q, want %q", got.Query, tt.want.Query)
			}
			if len(got.Documents) != len(tt.want.Documents) {
				t.Fatalf("Documents len = %d, want %d", len(got.Documents), len(tt.want.Documents))
			}
			for i, doc := range got.Documents {
				if doc != tt.want.Documents[i] {
					t.Errorf("Documents[%d] = %q, want %q", i, doc, tt.want.Documents[i])
				}
			}
			compareIntPtr(t, "TopN", got.TopN, tt.want.TopN)
			compareBoolPtr(t, "ReturnDocuments", got.ReturnDocuments, tt.want.ReturnDocuments)
			compareIntPtr(t, "MaxChunksPerDoc", got.MaxChunksPerDoc, tt.want.MaxChunksPerDoc)

			if !tt.want.ExtraFields.IsEmpty() {
				if !jsonEqual(t, got.ExtraFields.raw, tt.want.ExtraFields.raw) {
					t.Errorf("ExtraFields = %s, want %s", got.ExtraFields.raw, tt.want.ExtraFields.raw)
				}
			}
		})
	}
}

func TestRerankRequestMarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		req     RerankRequest
		want    string
		wantErr bool
	}{
		{
			name: "basic fields",
			req: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Query:     "What is the capital of France?",
				Documents: []string{"Paris is the capital of France.", "London is the capital of the UK."},
			},
			want: `{"model":"bge-reranker-v2-m3","query":"What is the capital of France?","documents":["Paris is the capital of France.","London is the capital of the UK."]}`,
		},
		{
			name: "with provider",
			req: RerankRequest{
				Model:     "bge-reranker-v2-m3",
				Provider:  "zhipu",
				Query:     "test query",
				Documents: []string{"doc1"},
			},
			want: `{"model":"bge-reranker-v2-m3","provider":"zhipu","query":"test query","documents":["doc1"]}`,
		},
		{
			name: "with optional fields",
			req: RerankRequest{
				Model:           "bge-reranker-v2-m3",
				Query:           "test query",
				Documents:       []string{"doc1", "doc2"},
				TopN:            intPtr(5),
				ReturnDocuments: boolPtr(true),
				MaxChunksPerDoc: intPtr(3),
			},
			want: `{"model":"bge-reranker-v2-m3","query":"test query","documents":["doc1","doc2"],"top_n":5,"return_documents":true,"max_chunks_per_doc":3}`,
		},
		{
			name: "with extra fields",
			req: RerankRequest{
				Model:       "bge-reranker-v2-m3",
				Query:       "test query",
				Documents:   []string{"doc1"},
				ExtraFields: UnknownJSONFields{raw: json.RawMessage(`{"custom_field":"custom_value"}`)},
			},
			want: `{"model":"bge-reranker-v2-m3","query":"test query","documents":["doc1"],"custom_field":"custom_value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !jsonEqual(t, got, []byte(tt.want)) {
				t.Errorf("MarshalJSON = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRerankRequestExtraFields(t *testing.T) {
	t.Run("round-trip preserves extra fields", func(t *testing.T) {
		input := `{
			"model": "bge-reranker-v2-m3",
			"query": "test query",
			"documents": ["doc1"],
			"custom_field": "custom_value",
			"another_field": 42,
			"nested": {"key": "value"}
		}`

		var req RerankRequest
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if req.ExtraFields.IsEmpty() {
			t.Fatal("expected ExtraFields to be non-empty")
		}

		// Verify individual extra fields are accessible via Lookup
		customVal := req.ExtraFields.Lookup("custom_field")
		if string(customVal) != `"custom_value"` {
			t.Errorf("Lookup(custom_field) = %s, want \"custom_value\"", customVal)
		}

		anotherVal := req.ExtraFields.Lookup("another_field")
		if string(anotherVal) != `42` {
			t.Errorf("Lookup(another_field) = %s, want 42", anotherVal)
		}

		nestedVal := req.ExtraFields.Lookup("nested")
		if string(nestedVal) != `{"key": "value"}` {
			t.Errorf("Lookup(nested) = %s, want {\"key\": \"value\"}", nestedVal)
		}

		// Marshal back and verify extra fields are present
		output, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		// Verify known fields are in the output
		if !jsonContains(t, output, `"model":"bge-reranker-v2-m3"`) {
			t.Error("output missing model field")
		}
		if !jsonContains(t, output, `"custom_field":"custom_value"`) {
			t.Error("output missing custom_field")
		}
		if !jsonContains(t, output, `"another_field":42`) {
			t.Error("output missing another_field")
		}
	})

	t.Run("no extra fields results in empty ExtraFields", func(t *testing.T) {
		input := `{
			"model": "bge-reranker-v2-m3",
			"query": "test",
			"documents": ["doc1"]
		}`

		var req RerankRequest
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if !req.ExtraFields.IsEmpty() {
			t.Errorf("expected empty ExtraFields, got %s", req.ExtraFields.raw)
		}
	})

	t.Run("extra fields with empty object", func(t *testing.T) {
		input := `{
			"model": "bge-reranker-v2-m3",
			"query": "test",
			"documents": ["doc1"]
		}`

		var req RerankRequest
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		output, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		// Verify no trailing comma or extra fields
		if !jsonEqual(t, output, []byte(`{"model":"bge-reranker-v2-m3","query":"test","documents":["doc1"]}`)) {
			t.Errorf("unexpected output: %s", output)
		}
	})
}

func TestRerankRequestOptionalFields(t *testing.T) {
	t.Run("top_n, return_documents, max_chunks_per_doc round-trip", func(t *testing.T) {
		req := RerankRequest{
			Model:           "bge-reranker-v2-m3",
			Query:           "test",
			Documents:       []string{"doc1", "doc2", "doc3"},
			TopN:            intPtr(2),
			ReturnDocuments: boolPtr(false),
			MaxChunksPerDoc: intPtr(1),
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var got RerankRequest
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if got.TopN == nil || *got.TopN != 2 {
			t.Errorf("TopN = %v, want 2", got.TopN)
		}
		if got.ReturnDocuments == nil || *got.ReturnDocuments != false {
			t.Errorf("ReturnDocuments = %v, want false", got.ReturnDocuments)
		}
		if got.MaxChunksPerDoc == nil || *got.MaxChunksPerDoc != 1 {
			t.Errorf("MaxChunksPerDoc = %v, want 1", got.MaxChunksPerDoc)
		}
	})

	t.Run("nil optional fields are omitted from JSON", func(t *testing.T) {
		req := RerankRequest{
			Model:     "bge-reranker-v2-m3",
			Query:     "test",
			Documents: []string{"doc1"},
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		output := string(data)
		if jsonContains(t, data, `"top_n"`) {
			t.Error("output should not contain top_n when nil")
		}
		if jsonContains(t, data, `"return_documents"`) {
			t.Error("output should not contain return_documents when nil")
		}
		if jsonContains(t, data, `"max_chunks_per_doc"`) {
			t.Error("output should not contain max_chunks_per_doc when nil")
		}
		_ = output
	})

	t.Run("top_n=0 is included in JSON", func(t *testing.T) {
		req := RerankRequest{
			Model:     "bge-reranker-v2-m3",
			Query:     "test",
			Documents: []string{"doc1"},
			TopN:      intPtr(0),
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		if !jsonContains(t, data, `"top_n":0`) {
			t.Errorf("expected top_n:0 in output, got %s", data)
		}
	})
}

func TestRerankResponseUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    RerankResponse
		wantErr bool
	}{
		{
			name: "basic response",
			json: `{
				"id": "rerank-123",
				"model": "bge-reranker-v2-m3",
				"provider": "zhipu",
				"results": [
					{"index": 0, "relevance_score": 0.95},
					{"index": 1, "relevance_score": 0.42}
				],
				"usage": {"total_tokens": 100}
			}`,
			want: RerankResponse{
				ID:       "rerank-123",
				Model:    "bge-reranker-v2-m3",
				Provider: "zhipu",
				Results: []RerankResult{
					{Index: 0, RelevanceScore: 0.95},
					{Index: 1, RelevanceScore: 0.42},
				},
				Usage: RerankUsage{TotalTokens: 100},
			},
		},
		{
			name: "response with document",
			json: `{
				"id": "rerank-456",
				"model": "bge-reranker-v2-m3",
				"provider": "zhipu",
				"results": [
					{"index": 0, "document": {"text": "Paris is the capital of France."}, "relevance_score": 0.98}
				],
				"usage": {"total_tokens": 50, "search_units": 1.5}
			}`,
			want: RerankResponse{
				ID:       "rerank-456",
				Model:    "bge-reranker-v2-m3",
				Provider: "zhipu",
				Results: []RerankResult{
					{
						Index:          0,
						Document:       &RerankDocument{Text: "Paris is the capital of France."},
						RelevanceScore: 0.98,
					},
				},
				Usage: RerankUsage{TotalTokens: 50, SearchUnits: 1.5},
			},
		},
		{
			name: "response with extra fields",
			json: `{
				"id": "rerank-789",
				"model": "bge-reranker-v2-m3",
				"provider": "zhipu",
				"results": [],
				"usage": {"total_tokens": 0},
				"extra_meta": "some_value",
				"request_id": "req-abc"
			}`,
			want: RerankResponse{
				ID:       "rerank-789",
				Model:    "bge-reranker-v2-m3",
				Provider: "zhipu",
				Results:  []RerankResult{},
				Usage:    RerankUsage{TotalTokens: 0},
				ExtraFields: UnknownJSONFields{
					raw: json.RawMessage(`{"extra_meta":"some_value","request_id":"req-abc"}`),
				},
			},
		},
		{
			name:    "invalid JSON",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RerankResponse
			err := json.Unmarshal([]byte(tt.json), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", got.Model, tt.want.Model)
			}
			if got.Provider != tt.want.Provider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.want.Provider)
			}
			if got.Usage.TotalTokens != tt.want.Usage.TotalTokens {
				t.Errorf("Usage.TotalTokens = %d, want %d", got.Usage.TotalTokens, tt.want.Usage.TotalTokens)
			}
			if got.Usage.SearchUnits != tt.want.Usage.SearchUnits {
				t.Errorf("Usage.SearchUnits = %f, want %f", got.Usage.SearchUnits, tt.want.Usage.SearchUnits)
			}
			if len(got.Results) != len(tt.want.Results) {
				t.Fatalf("Results len = %d, want %d", len(got.Results), len(tt.want.Results))
			}
			for i, res := range got.Results {
				if res.Index != tt.want.Results[i].Index {
					t.Errorf("Results[%d].Index = %d, want %d", i, res.Index, tt.want.Results[i].Index)
				}
				if res.RelevanceScore != tt.want.Results[i].RelevanceScore {
					t.Errorf("Results[%d].RelevanceScore = %f, want %f", i, res.RelevanceScore, tt.want.Results[i].RelevanceScore)
				}
				if tt.want.Results[i].Document != nil {
					if res.Document == nil {
						t.Errorf("Results[%d].Document should not be nil", i)
					} else if res.Document.Text != tt.want.Results[i].Document.Text {
						t.Errorf("Results[%d].Document.Text = %q, want %q", i, res.Document.Text, tt.want.Results[i].Document.Text)
					}
				}
			}

			if !tt.want.ExtraFields.IsEmpty() {
				if !jsonEqual(t, got.ExtraFields.raw, tt.want.ExtraFields.raw) {
					t.Errorf("ExtraFields = %s, want %s", got.ExtraFields.raw, tt.want.ExtraFields.raw)
				}
			}
		})
	}
}

func TestRerankResponseMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		resp RerankResponse
		want string
	}{
		{
			name: "basic response",
			resp: RerankResponse{
				ID:       "rerank-123",
				Model:    "bge-reranker-v2-m3",
				Provider: "zhipu",
				Results: []RerankResult{
					{Index: 0, RelevanceScore: 0.95},
				},
				Usage: RerankUsage{TotalTokens: 100},
			},
			want: `{"id":"rerank-123","model":"bge-reranker-v2-m3","provider":"zhipu","results":[{"index":0,"relevance_score":0.95}],"usage":{"total_tokens":100}}`,
		},
		{
			name: "response with document and search_units",
			resp: RerankResponse{
				ID:       "rerank-456",
				Model:    "bge-reranker-v2-m3",
				Provider: "zhipu",
				Results: []RerankResult{
					{
						Index:          0,
						Document:       &RerankDocument{Text: "Paris is the capital of France."},
						RelevanceScore: 0.98,
					},
				},
				Usage: RerankUsage{TotalTokens: 50, SearchUnits: 1.5},
			},
			want: `{"id":"rerank-456","model":"bge-reranker-v2-m3","provider":"zhipu","results":[{"index":0,"document":{"text":"Paris is the capital of France."},"relevance_score":0.98}],"usage":{"total_tokens":50,"search_units":1.5}}`,
		},
		{
			name: "response with extra fields",
			resp: RerankResponse{
				ID:          "rerank-789",
				Model:       "bge-reranker-v2-m3",
				Provider:    "zhipu",
				Results:     []RerankResult{},
				Usage:       RerankUsage{TotalTokens: 0},
				ExtraFields: UnknownJSONFields{raw: json.RawMessage(`{"extra_meta":"some_value"}`)},
			},
			want: `{"id":"rerank-789","model":"bge-reranker-v2-m3","provider":"zhipu","results":[],"usage":{"total_tokens":0},"extra_meta":"some_value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !jsonEqual(t, got, []byte(tt.want)) {
				t.Errorf("MarshalJSON = %s, want %s", got, tt.want)
			}
		})
	}
}

// --- helpers ---

func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func compareIntPtr(t *testing.T, name string, got, want *int) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %v, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s is nil, want %d", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s = %d, want %d", name, *got, *want)
	}
}

func compareBoolPtr(t *testing.T, name string, got, want *bool) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %v, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s is nil, want %v", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s = %v, want %v", name, *got, *want)
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var ja, jb any
	if err := json.Unmarshal(a, &ja); err != nil {
		t.Fatalf("failed to unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &jb); err != nil {
		t.Fatalf("failed to unmarshal b: %v", err)
	}
	// Use Marshal to normalize
	na, _ := json.Marshal(ja)
	nb, _ := json.Marshal(jb)
	return string(na) == string(nb)
}

func jsonContains(t *testing.T, data []byte, substr string) bool {
	t.Helper()
	return contains(string(data), substr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}