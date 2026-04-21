package service

import "testing"

func TestInferModelType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"qwen3-vl-rerank", "rerank_vl"},
		{"multimodal-rerank-v1", "rerank_vl"},
		{"vision-rerank", "rerank_vl"},
		{"gte-rerank-v2", "rerank"},
		{"qwen3-vl-embedding", "embedding_vl"},
		{"text-embedding-v4", "embedding"},
		{"gpt-4o", "chat"},
	}
	for _, tt := range tests {
		if got := InferModelType(tt.name); got != tt.want {
			t.Fatalf("InferModelType(%q)=%q, want %q", tt.name, got, tt.want)
		}
	}
}
