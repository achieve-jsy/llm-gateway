package normalizer

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// TestNormalizeResponse_NilInput 处理 nil 输入
func TestNormalizeResponse_NilInput(t *testing.T) {
	result := NormalizeResponse(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

// TestNormalizeResponse_GenerateID 生成缺失的 ID
func TestNormalizeResponse_GenerateID(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	if !strings.HasPrefix(result.ID, "chatcmpl-") {
		t.Fatalf("expected ID to start with 'chatcmpl-', got %s", result.ID)
	}

	if len(result.ID) <= len("chatcmpl-") {
		t.Fatalf("expected ID to have additional characters after prefix")
	}
}

// TestNormalizeResponse_PreserveExistingID 保留已有的 ID
func TestNormalizeResponse_PreserveExistingID(t *testing.T) {
	originalID := "chatcmpl-existing-id"
	resp := &model.ChatCompletionResponse{
		ID:      originalID,
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.ID != originalID {
		t.Fatalf("expected ID to be preserved, got %s", result.ID)
	}
}

// TestNormalizeResponse_SetObject 设置缺失的 Object
func TestNormalizeResponse_SetObject(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		ID:      "test-id",
		Created: 1234567890,
		Model:   "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.Object != "chat.completion" {
		t.Fatalf("expected object 'chat.completion', got %s", result.Object)
	}
}

// TestNormalizeResponse_PreserveExistingObject 保留已有的 Object
func TestNormalizeResponse_PreserveExistingObject(t *testing.T) {
	originalObject := "custom.object"
	resp := &model.ChatCompletionResponse{
		ID:     "test-id",
		Object: originalObject,
		Model:  "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.Object != originalObject {
		t.Fatalf("expected object to be preserved, got %s", result.Object)
	}
}

// TestNormalizeResponse_SetCreated 设置缺失的 Created
func TestNormalizeResponse_SetCreated(t *testing.T) {
	before := time.Now().Unix()
	resp := &model.ChatCompletionResponse{
		ID:     "test-id",
		Object: "chat.completion",
		Model:  "gpt-4",
	}

	result := NormalizeResponse(resp)

	after := time.Now().Unix()

	if result.Created == 0 {
		t.Fatal("expected non-zero Created timestamp")
	}

	if result.Created < before || result.Created > after+1 {
		t.Fatalf("expected Created to be between %d and %d, got %d", before, after, result.Created)
	}
}

// TestNormalizeResponse_PreserveExistingCreated 保留已有的 Created
func TestNormalizeResponse_PreserveExistingCreated(t *testing.T) {
	originalCreated := int64(9999999999)
	resp := &model.ChatCompletionResponse{
		ID:      "test-id",
		Object:  "chat.completion",
		Created: originalCreated,
		Model:   "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.Created != originalCreated {
		t.Fatalf("expected Created to be preserved, got %d", result.Created)
	}
}

// TestNormalizeResponse_CreateUsage 创建缺失的 Usage
func TestNormalizeResponse_CreateUsage(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		ID:      "test-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
	}

	result := NormalizeResponse(resp)

	if result.Usage == nil {
		t.Fatal("expected non-nil Usage")
	}

	if result.Usage.PromptTokens != 0 {
		t.Fatalf("expected zero PromptTokens, got %d", result.Usage.PromptTokens)
	}

	if result.Usage.CompletionTokens != 0 {
		t.Fatalf("expected zero CompletionTokens, got %d", result.Usage.CompletionTokens)
	}
}

// TestNormalizeResponse_PreserveExistingUsage 保留已有的 Usage
func TestNormalizeResponse_PreserveExistingUsage(t *testing.T) {
	originalUsage := &model.Usage{
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
	}
	resp := &model.ChatCompletionResponse{
		ID:      "test-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Usage:   originalUsage,
	}

	result := NormalizeResponse(resp)

	if result.Usage.PromptTokens != 100 {
		t.Fatalf("expected PromptTokens to be 100, got %d", result.Usage.PromptTokens)
	}

	if result.Usage.CompletionTokens != 200 {
		t.Fatalf("expected CompletionTokens to be 200, got %d", result.Usage.CompletionTokens)
	}

	if result.Usage.TotalTokens != 300 {
		t.Fatalf("expected TotalTokens to be 300, got %d", result.Usage.TotalTokens)
	}
}

// TestNormalizeResponse_CompleteNormalization 完整归一化测试
func TestNormalizeResponse_CompleteNormalization(t *testing.T) {
	// 最小化的响应
	resp := &model.ChatCompletionResponse{
		Model:   "gpt-4",
		Choices: []model.Choice{},
	}

	result := NormalizeResponse(resp)

	// 验证所有字段都被填充
	if result.ID == "" {
		t.Fatal("ID should be filled")
	}

	if result.Object != "chat.completion" {
		t.Fatal("Object should be set to 'chat.completion'")
	}

	if result.Created == 0 {
		t.Fatal("Created should be set to current timestamp")
	}

	if result.Usage == nil {
		t.Fatal("Usage should be initialized")
	}

	if result.Model != "gpt-4" {
		t.Fatal("Model should be preserved")
	}
}

// TestNormalizeResponse_WithChoices 包含 Choices 的响应
func TestNormalizeResponse_WithChoices(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		Model: "gpt-4",
		Choices: []model.Choice{
			{
				Index:   0,
				Message: &model.Message{Role: "assistant", Content: "Hello"},
			},
		},
	}

	result := NormalizeResponse(resp)

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	if result.Choices[0].Message.Content != "Hello" {
		t.Fatalf("expected content 'Hello', got %s", result.Choices[0].Message.Content)
	}
}

// TestNormalizeResponse_IDFormat ID 格式正确性
// 生成器使用 fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:12])，
// 而 uuid.New().String() 是 8-4-4-4-12 形式，前 12 字符为 8 hex + "-" + 3 hex，
// 因此 ID 形如 `chatcmpl-XXXXXXXX-XXX`（含 12 个有效内容字符）。
func TestNormalizeResponse_IDFormat(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		Model: "gpt-4",
	}

	result := NormalizeResponse(resp)

	if !strings.HasPrefix(result.ID, "chatcmpl-") {
		t.Fatalf("expected ID to start with 'chatcmpl-', got %s", result.ID)
	}

	idPattern := regexp.MustCompile(`^chatcmpl-[0-9a-f]{8}-[0-9a-f]{3}$`)
	if !idPattern.MatchString(result.ID) {
		t.Fatalf("expected ID format 'chatcmpl-XXXXXXXX-XXX' (UUID-derived), got %s", result.ID)
	}

	suffix := strings.TrimPrefix(result.ID, "chatcmpl-")
	if len(suffix) != 12 {
		t.Fatalf("expected suffix length 12 (incl. embedded dash), got %d", len(suffix))
	}
}

// TestNormalizeResponse_Idempotent 幂等性（重复调用结果一致）
func TestNormalizeResponse_Idempotent(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		Model: "gpt-4",
	}

	// 第一次归一化
	result1 := NormalizeResponse(resp)
	id1 := result1.ID

	// 第二次归一化
	result2 := NormalizeResponse(result1)
	id2 := result2.ID

	// ID 应该保持不变
	if id1 != id2 {
		t.Fatalf("expected ID to be preserved on second normalization, got %s then %s", id1, id2)
	}
}

// TestStreamNormalizer_CreateNormalizer 创建流归一化器
func TestStreamNormalizer_CreateNormalizer(t *testing.T) {
	sn := NewStreamNormalizer()

	if sn == nil {
		t.Fatal("expected non-nil StreamNormalizer")
	}

	if sn.id == "" {
		t.Fatal("expected non-empty ID")
	}

	if !strings.HasPrefix(sn.id, "chatcmpl-") {
		t.Fatalf("expected ID to start with 'chatcmpl-', got %s", sn.id)
	}

	if sn.created == 0 {
		t.Fatal("expected non-zero created timestamp")
	}
}

// TestStreamNormalizer_NormalizeChunk_NilInput 处理 nil chunk
func TestStreamNormalizer_NormalizeChunk_NilInput(t *testing.T) {
	sn := NewStreamNormalizer()
	result := sn.NormalizeChunk(nil)

	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

// TestStreamNormalizer_NormalizeChunk_GenerateID 为 chunk 生成 ID
func TestStreamNormalizer_NormalizeChunk_GenerateID(t *testing.T) {
	sn := NewStreamNormalizer()
	chunk := &model.ChatCompletionChunk{
		Object: "chat.completion.chunk",
		Model:  "gpt-4",
	}

	result := sn.NormalizeChunk(chunk)

	if result.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	if result.ID != sn.id {
		t.Fatalf("expected ID to match normalizer's ID: %s vs %s", result.ID, sn.id)
	}
}

// TestStreamNormalizer_NormalizeChunk_PreserveID 保留已有的 ID
func TestStreamNormalizer_NormalizeChunk_PreserveID(t *testing.T) {
	sn := NewStreamNormalizer()
	originalID := "custom-id"
	chunk := &model.ChatCompletionChunk{
		ID:     originalID,
		Object: "chat.completion.chunk",
		Model:  "gpt-4",
	}

	result := sn.NormalizeChunk(chunk)

	if result.ID != originalID {
		t.Fatalf("expected ID to be preserved, got %s", result.ID)
	}
}

// TestStreamNormalizer_NormalizeChunk_SetObject 设置 Object
func TestStreamNormalizer_NormalizeChunk_SetObject(t *testing.T) {
	sn := NewStreamNormalizer()
	chunk := &model.ChatCompletionChunk{
		Model: "gpt-4",
	}

	result := sn.NormalizeChunk(chunk)

	if result.Object != "chat.completion.chunk" {
		t.Fatalf("expected object 'chat.completion.chunk', got %s", result.Object)
	}
}

// TestStreamNormalizer_NormalizeChunk_SetCreated 设置 Created
func TestStreamNormalizer_NormalizeChunk_SetCreated(t *testing.T) {
	sn := NewStreamNormalizer()
	chunk := &model.ChatCompletionChunk{
		Model: "gpt-4",
	}

	result := sn.NormalizeChunk(chunk)

	if result.Created == 0 {
		t.Fatal("expected non-zero Created timestamp")
	}

	if result.Created != sn.created {
		t.Fatalf("expected Created to match normalizer's: %d vs %d", result.Created, sn.created)
	}
}

// TestStreamNormalizer_MultipleChunks_ConsistentID 多个 chunks 的一致性
func TestStreamNormalizer_MultipleChunks_ConsistentID(t *testing.T) {
	sn := NewStreamNormalizer()

	chunk1 := sn.NormalizeChunk(&model.ChatCompletionChunk{Model: "gpt-4"})
	chunk2 := sn.NormalizeChunk(&model.ChatCompletionChunk{Model: "gpt-4"})
	chunk3 := sn.NormalizeChunk(&model.ChatCompletionChunk{Model: "gpt-4"})

	// 所有 chunks 应该有相同的 ID
	if chunk1.ID != chunk2.ID || chunk2.ID != chunk3.ID {
		t.Fatalf("expected all chunks to have same ID: %s, %s, %s", chunk1.ID, chunk2.ID, chunk3.ID)
	}

	// 所有 chunks 应该有相同的 Created
	if chunk1.Created != chunk2.Created || chunk2.Created != chunk3.Created {
		t.Fatalf("expected all chunks to have same Created: %d, %d, %d", chunk1.Created, chunk2.Created, chunk3.Created)
	}
}

// TestStreamNormalizer_MultipleNormalizers_DifferentIDs 不同的 normalizers 有不同的 ID
func TestStreamNormalizer_MultipleNormalizers_DifferentIDs(t *testing.T) {
	sn1 := NewStreamNormalizer()
	time.Sleep(1 * time.Millisecond) // 确保不同的 UUID
	sn2 := NewStreamNormalizer()

	chunk1 := sn1.NormalizeChunk(&model.ChatCompletionChunk{Model: "gpt-4"})
	chunk2 := sn2.NormalizeChunk(&model.ChatCompletionChunk{Model: "gpt-4"})

	if chunk1.ID == chunk2.ID {
		t.Fatalf("expected different IDs for different normalizers, got %s", chunk1.ID)
	}
}

// TestStreamNormalizer_NormalizeChunk_WithDelta 包含 Delta 的 chunk
func TestStreamNormalizer_NormalizeChunk_WithDelta(t *testing.T) {
	sn := NewStreamNormalizer()
	chunk := &model.ChatCompletionChunk{
		Model: "gpt-4",
		Choices: []model.Choice{
			{
				Index: 0,
				Delta: &model.Delta{Content: "Hello"},
			},
		},
	}

	result := sn.NormalizeChunk(chunk)

	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}

	if result.Choices[0].Delta.Content != "Hello" {
		t.Fatalf("expected content 'Hello', got %s", result.Choices[0].Delta.Content)
	}
}
