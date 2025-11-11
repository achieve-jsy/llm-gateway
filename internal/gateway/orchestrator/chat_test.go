package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// TestChat_EmptyRouter returns ErrModelNotFound when the router is nil.
func TestChat_EmptyRouter(t *testing.T) {
	orch := NewChatOrchestrator(nil)

	req := newTestChatRequest("gpt-4")
	resp, err := orch.Chat(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for nil router")
	}
	if err != model.ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

// TestChatStream_EmptyRouter returns ErrModelNotFound when the router is nil.
func TestChatStream_EmptyRouter(t *testing.T) {
	orch := NewChatOrchestrator(nil)

	req := newTestChatRequest("gpt-4")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := orch.ChatStream(ctx, req)

	if err == nil {
		t.Fatal("expected error for nil router")
	}
	if err != model.ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
	if stream != nil {
		t.Fatal("expected nil stream")
	}
}

// TestEmbeddings_EmptyRouter returns ErrModelNotFound when the router is nil.
func TestEmbeddings_EmptyRouter(t *testing.T) {
	orch := NewChatOrchestrator(nil)

	req := newTestEmbeddingRequest("text-embedding-ada-002")
	resp, err := orch.Embeddings(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for nil router")
	}
	if err != model.ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}
