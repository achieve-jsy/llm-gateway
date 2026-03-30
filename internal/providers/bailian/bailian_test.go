package bailian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helper functions ────────────────────────────────────────────────────────

func makeTexts(n int) []string {
	texts := make([]string, n)
	for i := 0; i < n; i++ {
		texts[i] = fmt.Sprintf("text-%d", i)
	}
	return texts
}

// newTestNativeClient creates a llmclient.Client pointing at the given test server URL.
func newTestNativeClient(t *testing.T, serverURL string) *llmclient.Client {
	t.Helper()
	cfg := llmclient.Config{
		ProviderName: "test-bailian",
		BaseURL:      serverURL,
	}
	headerSetter := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer test-key")
	}
	return llmclient.NewWithHTTPClient(&http.Client{}, cfg, headerSetter)
}

// newTestProvider creates a Provider with nativeClient pointing at the test server.
// The compatible field is nil — tests that need it should use a different setup.
func newTestProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()
	return &Provider{
		nativeClient: newTestNativeClient(t, serverURL),
		embedSem:     make(chan struct{}, maxEmbeddingConcurrency),
	}
}

// ─── splitTextsByCount tests ────────────────────────────────────────────────

func TestSplitTextsByCount_EmptyInput(t *testing.T) {
	result := splitTextsByCount([]string{}, 10)
	assert.Nil(t, result, "empty input should return nil")
}

func TestSplitTextsByCount_NilInput(t *testing.T) {
	result := splitTextsByCount(nil, 10)
	assert.Nil(t, result, "nil input should return nil")
}

func TestSplitTextsByCount_SingleText(t *testing.T) {
	result := splitTextsByCount([]string{"hello"}, 10)
	require.Len(t, result, 1)
	assert.Equal(t, []string{"hello"}, result[0])
}

func TestSplitTextsByCount_ExactlyBatchSize(t *testing.T) {
	texts := makeTexts(10)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 1)
	assert.Equal(t, 10, len(result[0]))
	assert.Equal(t, texts, result[0])
}

func TestSplitTextsByCount_OneMoreThanBatchSize(t *testing.T) {
	texts := makeTexts(11)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 2)
	assert.Equal(t, 10, len(result[0]))
	assert.Equal(t, 1, len(result[1]))
	assert.Equal(t, texts[:10], result[0])
	assert.Equal(t, texts[10:], result[1])
}

func TestSplitTextsByCount_17Texts(t *testing.T) {
	texts := makeTexts(17)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 2)
	assert.Equal(t, 10, len(result[0]))
	assert.Equal(t, 7, len(result[1]))
}

func TestSplitTextsByCount_25Texts(t *testing.T) {
	texts := makeTexts(25)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 3)
	assert.Equal(t, 10, len(result[0]))
	assert.Equal(t, 10, len(result[1]))
	assert.Equal(t, 5, len(result[2]))
}

func TestSplitTextsByCount_1000Texts(t *testing.T) {
	texts := makeTexts(1000)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 100)
	for i := 0; i < 100; i++ {
		assert.Equal(t, 10, len(result[i]), "batch %d should have 10 texts", i)
	}
}

func TestSplitTextsByCount_1001Texts(t *testing.T) {
	texts := makeTexts(1001)
	result := splitTextsByCount(texts, 10)
	require.Len(t, result, 101)
	assert.Equal(t, 10, len(result[0]))
	assert.Equal(t, 1, len(result[100]))
	assert.Equal(t, "text-1000", result[100][0])
}

func TestSplitTextsByCount_PreservesOrder(t *testing.T) {
	texts := makeTexts(23)
	result := splitTextsByCount(texts, 10)

	var reconstructed []string
	for _, batch := range result {
		reconstructed = append(reconstructed, batch...)
	}
	assert.Equal(t, texts, reconstructed, "reconstructed texts should match original order")
}

func TestSplitTextsByCount_SmallBatchSize(t *testing.T) {
	texts := makeTexts(7)
	result := splitTextsByCount(texts, 3)
	require.Len(t, result, 3)
	assert.Equal(t, 3, len(result[0]))
	assert.Equal(t, 3, len(result[1]))
	assert.Equal(t, 1, len(result[2]))
}

// ─── embeddingInputToStrings tests ──────────────────────────────────────────

func TestEmbeddingInputToStrings_String(t *testing.T) {
	result := embeddingInputToStrings("hello world")
	assert.Equal(t, []string{"hello world"}, result)
}

func TestEmbeddingInputToStrings_EmptyString(t *testing.T) {
	result := embeddingInputToStrings("")
	assert.Nil(t, result, "empty string should return nil")
}

func TestEmbeddingInputToStrings_StringArray(t *testing.T) {
	input := []string{"hello", "world"}
	result := embeddingInputToStrings(input)
	assert.Equal(t, input, result)
}

func TestEmbeddingInputToStrings_EmptyStringArray(t *testing.T) {
	result := embeddingInputToStrings([]string{})
	assert.Equal(t, []string{}, result)
}

func TestEmbeddingInputToStrings_InterfaceArray(t *testing.T) {
	input := []interface{}{"hello", "world"}
	result := embeddingInputToStrings(input)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestEmbeddingInputToStrings_EmptyInterfaceArray(t *testing.T) {
	result := embeddingInputToStrings([]interface{}{})
	assert.Equal(t, []string{}, result)
}

func TestEmbeddingInputToStrings_MixedTypes(t *testing.T) {
	input := []interface{}{"hello", 123, "world", nil, true}
	result := embeddingInputToStrings(input)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestEmbeddingInputToStrings_Nil(t *testing.T) {
	result := embeddingInputToStrings(nil)
	assert.Nil(t, result)
}

func TestEmbeddingInputToStrings_IntType(t *testing.T) {
	result := embeddingInputToStrings(42)
	assert.Nil(t, result, "unsupported int type should return nil")
}

func TestEmbeddingInputToStrings_AllNonStringInterface(t *testing.T) {
	input := []interface{}{123, 456, true, nil}
	result := embeddingInputToStrings(input)
	assert.Equal(t, []string{}, result)
}

// ─── isModelNotSupported tests ──────────────────────────────────────────────

func TestIsModelNotSupported_Nil(t *testing.T) {
	assert.False(t, isModelNotSupported(nil))
}

func TestIsModelNotSupported_ModelNotSupported(t *testing.T) {
	assert.True(t, isModelNotSupported(fmt.Errorf("model_not_supported: xyz")))
}

func TestIsModelNotSupported_ModelNotSupportedPascalCase(t *testing.T) {
	assert.True(t, isModelNotSupported(fmt.Errorf("ModelNotSupported error")))
}

func TestIsModelNotSupported_UnsupportedModel(t *testing.T) {
	assert.True(t, isModelNotSupported(fmt.Errorf("unsupported model: xyz")))
}

func TestIsModelNotSupported_OtherError(t *testing.T) {
	assert.False(t, isModelNotSupported(fmt.Errorf("connection timeout")))
}

func TestIsModelNotSupported_InvalidRequest(t *testing.T) {
	assert.False(t, isModelNotSupported(fmt.Errorf("invalid request: missing field")))
}

func TestIsModelNotSupported_EmptyError(t *testing.T) {
	assert.False(t, isModelNotSupported(fmt.Errorf("")))
}

// ─── Constants test ─────────────────────────────────────────────────────────

func TestConstants(t *testing.T) {
	assert.Equal(t, 10, maxEmbeddingBatchSize, "maxEmbeddingBatchSize should be 10 per Bailian docs")
	assert.Equal(t, 10, maxEmbeddingConcurrency, "maxEmbeddingConcurrency should be 10 based on API limit of ~30 QPS")
}

// ─── Concurrent batch processing tests (logic) ─────────────────────────────
// These test the batching/concurrency/index-offset logic without HTTP calls.

func TestConcurrentBatchProcessing_AllSucceed(t *testing.T) {
	texts := makeTexts(25)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)
	require.Len(t, batches, 3)

	type batchResult struct {
		data   []string
		tokens int
		err    error
	}

	results := make([]batchResult, len(batches))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	sem := make(chan struct{}, maxEmbeddingConcurrency)

	for i, batch := range batches {
		wg.Add(1)
		go func(idx int, b []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mu.Lock()
			defer mu.Unlock()
			results[idx] = batchResult{data: b, tokens: len(b) * 100}
		}(i, batch)
	}
	wg.Wait()

	assert.Nil(t, firstErr)
	for i, r := range results {
		assert.Nil(t, r.err, "batch %d should not have error", i)
		assert.Equal(t, batches[i], r.data)
		assert.Equal(t, len(batches[i])*100, r.tokens)
	}

	totalTokens := 0
	for _, r := range results {
		totalTokens += r.tokens
	}
	assert.Equal(t, 2500, totalTokens)
}

func TestConcurrentBatchProcessing_OneBatchFails(t *testing.T) {
	texts := makeTexts(25)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)
	require.Len(t, batches, 3)

	type batchResult struct {
		data []string
		err  error
	}

	results := make([]batchResult, len(batches))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	sem := make(chan struct{}, maxEmbeddingConcurrency)

	for i, batch := range batches {
		wg.Add(1)
		go func(idx int, b []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var err error
			if idx == 1 {
				err = fmt.Errorf("batch %d failed: API error", idx)
			}

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				results[idx] = batchResult{err: err}
				return
			}
			results[idx] = batchResult{data: b}
		}(i, batch)
	}
	wg.Wait()

	require.NotNil(t, firstErr)
	assert.Contains(t, firstErr.Error(), "batch 1 failed")
	assert.NotNil(t, results[0].data)
	assert.Nil(t, results[0].err)
	assert.Nil(t, results[1].data)
	assert.NotNil(t, results[1].err)
	assert.NotNil(t, results[2].data)
	assert.Nil(t, results[2].err)
}

// ─── Index offset tests ─────────────────────────────────────────────────────

func TestBatchIndexOffset_Correctness(t *testing.T) {
	texts := makeTexts(25)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)

	offset := 0
	for batchIdx, batch := range batches {
		batchOffset := offset
		offset += len(batch)

		for textIdx, text := range batch {
			expectedIdx := batchOffset + textIdx
			assert.Equal(t, texts[expectedIdx], text,
				"batch %d, text %d should map to original index %d, got %s",
				batchIdx, textIdx, expectedIdx, text)
		}
	}
}

func TestBatchIndexOffset_1000Texts(t *testing.T) {
	texts := makeTexts(1000)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)
	require.Len(t, batches, 100)

	allIndices := make([]int, 0, 1000)
	offset := 0
	for _, batch := range batches {
		batchOffset := offset
		offset += len(batch)

		for textIdx := range batch {
			globalIdx := batchOffset + textIdx
			allIndices = append(allIndices, globalIdx)
		}
	}

	sort.Ints(allIndices)
	expected := make([]int, 1000)
	for i := range expected {
		expected[i] = i
	}
	assert.Equal(t, expected, allIndices, "all indices 0..999 should be present exactly once")
}

// ─── Result merging tests ───────────────────────────────────────────────────

func TestResultMerging_AllSucceed(t *testing.T) {
	texts := makeTexts(25)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)

	type batchResult struct {
		data []struct {
			text  string
			index int
		}
		tokens int
	}

	results := make([]batchResult, len(batches))
	offset := 0
	for i, batch := range batches {
		batchOffset := offset
		offset += len(batch)

		data := make([]struct {
			text  string
			index int
		}, len(batch))
		for j, text := range batch {
			data[j] = struct {
				text  string
				index int
			}{text: text, index: batchOffset + j}
		}
		results[i] = batchResult{data: data, tokens: len(batch) * 100}
	}

	allData := make([]struct {
		text  string
		index int
	}, 0, len(texts))
	totalTokens := 0
	for _, r := range results {
		allData = append(allData, r.data...)
		totalTokens += r.tokens
	}

	assert.Equal(t, 25, len(allData))
	assert.Equal(t, 2500, totalTokens)

	for i, d := range allData {
		assert.Equal(t, i, d.index, "merged index %d should equal %d", d.index, i)
		assert.Equal(t, texts[i], d.text)
	}
}

func TestResultMerging_1000Texts(t *testing.T) {
	texts := makeTexts(1000)
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)
	require.Len(t, batches, 100)

	type batchResult struct {
		data []struct {
			text  string
			index int
		}
		tokens int
	}

	results := make([]batchResult, len(batches))
	offset := 0
	for i, batch := range batches {
		batchOffset := offset
		offset += len(batch)

		data := make([]struct {
			text  string
			index int
		}, len(batch))
		for j, text := range batch {
			data[j] = struct {
				text  string
				index int
			}{text: text, index: batchOffset + j}
		}
		results[i] = batchResult{data: data, tokens: len(batch) * 100}
	}

	allData := make([]struct {
		text  string
		index int
	}, 0, len(texts))
	totalTokens := 0
	for _, r := range results {
		allData = append(allData, r.data...)
		totalTokens += r.tokens
	}

	assert.Equal(t, 1000, len(allData))
	assert.Equal(t, 100000, totalTokens)

	for i, d := range allData {
		assert.Equal(t, i, d.index, "merged index %d should equal %d", d.index, i)
		assert.Equal(t, texts[i], d.text)
	}
}

// ─── Integration tests with httptest (nativeEmbeddings) ─────────────────────

func TestNativeEmbeddings_SingleBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/embeddings/text-embedding/text-embedding", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "text-embedding-v4", req.Model)
		require.Len(t, req.Input.Texts, 3)

		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{0.1 * float64(i+1), 0.2 * float64(i+1)},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 42},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: []string{"hello", "world", "foo"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, "text-embedding-v4", resp.Model)
	assert.Equal(t, 42, resp.Usage.TotalTokens)
	require.Len(t, resp.Data, 3)
	assert.Equal(t, 0, resp.Data[0].Index)
	assert.Equal(t, 1, resp.Data[1].Index)
	assert.Equal(t, 2, resp.Data[2].Index)
}

func TestNativeEmbeddings_MultipleBatches(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	var receivedTextCounts []int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		mu.Lock()
		requestCount++
		receivedTextCounts = append(receivedTextCounts, len(req.Input.Texts))
		mu.Unlock()

		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{float64(i) + 0.1},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: len(req.Input.Texts) * 10},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	texts := makeTexts(25) // 3 batches: 10, 10, 5
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: texts,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should have made 3 requests
	mu.Lock()
	assert.Equal(t, 3, requestCount, "should have made 3 batch requests")
	sort.Ints(receivedTextCounts)
	assert.Equal(t, []int{5, 10, 10}, receivedTextCounts)
	mu.Unlock()

	// Should have 25 results total
	require.Len(t, resp.Data, 25)

	// Verify indices are 0..24 in correct order
	indices := make([]int, len(resp.Data))
	for i, d := range resp.Data {
		indices[i] = d.Index
	}
	expectedIndices := make([]int, 25)
	for i := range expectedIndices {
		expectedIndices[i] = i
	}
	assert.Equal(t, expectedIndices, indices, "indices should be 0..24 in order")

	// Total tokens = sum of batch tokens = 10*10 + 10*10 + 5*10 = 250
	assert.Equal(t, 250, resp.Usage.TotalTokens)
}

func TestNativeEmbeddings_EmptyInput(t *testing.T) {
	provider := &Provider{embedSem: make(chan struct{}, maxEmbeddingConcurrency)}
	_, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input must be a non-empty")
}

func TestNativeEmbeddings_NilInput(t *testing.T) {
	provider := &Provider{embedSem: make(chan struct{}, maxEmbeddingConcurrency)}
	_, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: nil,
	})
	require.Error(t, err)
}

func TestNativeEmbeddings_StringInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		require.Len(t, req.Input.Texts, 1)
		assert.Equal(t, "single text", req.Input.Texts[0])

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "single text",
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, 0, resp.Data[0].Index)
}

func TestNativeEmbeddings_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: []string{"test"},
	})
	require.Error(t, err)
}

// ─── Integration tests with httptest (multimodalEmbeddings) ─────────────────

func TestMultimodalEmbeddings_SingleBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "qwen3-vl-embedding", req.Model)
		require.Len(t, req.Input.Contents, 2)
		assert.Equal(t, "hello", req.Input.Contents[0].Text)
		assert.Equal(t, "world", req.Input.Contents[1].Text)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 30},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.1, 0.2}},
			{Index: 1, Embedding: []float64{0.3, 0.4}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: []string{"hello", "world"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, "qwen3-vl-embedding", resp.Model)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, 0, resp.Data[0].Index)
	assert.Equal(t, 1, resp.Data[1].Index)
	assert.Equal(t, 30, resp.Usage.TotalTokens)
}

func TestMultimodalEmbeddings_MultipleBatches(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		json.Unmarshal(body, &req)

		mu.Lock()
		requestCount++
		mu.Unlock()

		items := make([]bailianMultimodalEmbeddingItem, len(req.Input.Contents))
		for i := range req.Input.Contents {
			items[i] = bailianMultimodalEmbeddingItem{
				Index:     i,
				Embedding: []float64{float64(i) + 0.5},
			}
		}

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: len(req.Input.Contents) * 8},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	texts := makeTexts(15) // 2 batches: 10, 5
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: texts,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	mu.Lock()
	assert.Equal(t, 2, requestCount, "should have made 2 batch requests")
	mu.Unlock()

	require.Len(t, resp.Data, 15)

	// Verify indices are 0..14 in correct order
	for i, d := range resp.Data {
		assert.Equal(t, i, d.Index, "index %d should be %d", d.Index, i)
	}

	// Total tokens = 10*8 + 5*8 = 120
	assert.Equal(t, 120, resp.Usage.TotalTokens)
}

func TestMultimodalEmbeddings_EmptyInput(t *testing.T) {
	provider := &Provider{embedSem: make(chan struct{}, maxEmbeddingConcurrency)}
	_, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input must be a non-empty")
}

// ─── Integration tests with httptest (Rerank) ───────────────────────────────

func TestRerank_Basic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/rerank/text-rerank/text-rerank", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		body, _ := io.ReadAll(r.Body)
		var req bailianRerankReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "gte-rerank-v2", req.Model)
		assert.Equal(t, "search query", req.Input.Query)
		assert.Equal(t, []any{"doc1", "doc2", "doc3"}, req.Input.Documents)
		assert.Equal(t, 5, req.Parameters.TopN)

		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 20},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 2, RelevanceScore: 0.95},
			{Index: 0, RelevanceScore: 0.75},
			{Index: 1, RelevanceScore: 0.30},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	topN := 5
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "search query",
		Documents: []any{"doc1", "doc2", "doc3"},
		TopN:      &topN,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "gte-rerank-v2", resp.Model)
	require.Len(t, resp.Results, 3)
	assert.Equal(t, 2, resp.Results[0].Index)
	assert.InDelta(t, 0.95, resp.Results[0].RelevanceScore, 0.001)
	assert.Equal(t, 0, resp.Results[1].Index)
	assert.Equal(t, 1, resp.Results[2].Index)
	assert.Equal(t, 20, resp.Usage.TotalTokens)
}

func TestRerank_VLObjectBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/rerank/text-rerank/text-rerank", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req bailianRerankReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "qwen3-vl-rerank", req.Model)

		query, ok := req.Input.Query.(map[string]any)
		require.True(t, ok, "query should be object")
		assert.Equal(t, "https://example.com/q.png", query["image"])

		require.Len(t, req.Input.Documents, 2)
		doc0, ok := req.Input.Documents[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "a cat", doc0["text"])
		doc1, ok := req.Input.Documents[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "https://example.com/d.mp4", doc1["video"])

		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 42},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 0, RelevanceScore: 0.9},
			{Index: 1, RelevanceScore: 0.2},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	topN := 2
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model: "qwen3-vl-rerank",
		Query: map[string]any{"image": "https://example.com/q.png"},
		Documents: []any{
			map[string]any{"text": "a cat"},
			map[string]any{"video": "https://example.com/d.mp4"},
		},
		TopN: &topN,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, 0, resp.Results[0].Index)
	assert.Equal(t, 42, resp.Usage.TotalTokens)
}

func TestRerank_WithReturnDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 15},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 1, RelevanceScore: 0.88},
			{Index: 0, RelevanceScore: 0.65},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	returnDocs := true
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:           "gte-rerank-v2",
		Query:           "query",
		Documents: []any{"first doc", "second doc"},
		ReturnDocuments: &returnDocs,
	})

	require.NoError(t, err)
	require.Len(t, resp.Results, 2)

	// Result 0: index=1 → "second doc"
	require.NotNil(t, resp.Results[0].Document)
	assert.Equal(t, "second doc", resp.Results[0].Document.Text)

	// Result 1: index=0 → "first doc"
	require.NotNil(t, resp.Results[1].Document)
	assert.Equal(t, "first doc", resp.Results[1].Document.Text)
}

func TestRerank_WithoutReturnDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 0, RelevanceScore: 0.9},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "query",
		Documents: []any{"doc1"},
	})

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Nil(t, resp.Results[0].Document, "document should be nil when return_documents is not set")
}

func TestRerank_NilRequest(t *testing.T) {
	provider := &Provider{embedSem: make(chan struct{}, maxEmbeddingConcurrency)}
	_, err := provider.Rerank(context.Background(), nil)
	require.Error(t, err)
}

func TestRerank_DefaultTopN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianRerankReq
		json.Unmarshal(body, &req)
		assert.Equal(t, 10, req.Parameters.TopN, "default top_n should be 10")

		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 0, RelevanceScore: 0.8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "query",
		Documents: []any{"doc1"},
		// TopN not set → defaults to 10
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestRerank_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "service unavailable"})
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "query",
		Documents: []any{"doc1"},
	})
	require.Error(t, err)
}

// ─── Embeddings routing tests ───────────────────────────────────────────────

func TestEmbeddings_NilRequest(t *testing.T) {
	provider := &Provider{embedSem: make(chan struct{}, maxEmbeddingConcurrency)}
	_, err := provider.Embeddings(context.Background(), nil)
	require.Error(t, err)
}

func TestEmbeddings_ModelTypeEmbeddingRoutesToNativeTextAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/embeddings/text-embedding/text-embedding", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "text-embedding-v4", req.Model)
		assert.Equal(t, []string{"hello"}, req.Input.Texts)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 3},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.Embeddings(context.Background(), &core.EmbeddingRequest{
		Model:     "text-embedding-v4",
		ModelType: "embedding",
		Input:     "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "text-embedding-v4", resp.Model)
	require.Len(t, resp.Data, 1)
}

func TestEmbeddings_ModelTypeEmbeddingVLRoutesToMultimodalAPIWithoutModelRewrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "custom-vl-embedding", req.Model)
		require.Len(t, req.Input.Contents, 1)
		assert.Equal(t, "https://example.com/image.png", req.Input.Contents[0].Image)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.2}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.Embeddings(context.Background(), &core.EmbeddingRequest{
		Model:     "custom-vl-embedding",
		ModelType: "embedding_vl",
		Input: []interface{}{
			map[string]interface{}{"image": "https://example.com/image.png"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "custom-vl-embedding", resp.Model)
	require.Len(t, resp.Data, 1)
}

func TestEmbeddings_ModelTypeEmbeddingRejectsMultimodalInputWithoutUpstreamCall(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	req := &core.EmbeddingRequest{
		Model:     "text-embedding-v4",
		ModelType: "embedding",
		Input: []interface{}{
			map[string]interface{}{"image": "data:image/png;base64,AAA"},
		},
	}

	_, err := provider.Embeddings(context.Background(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "multimodal input requires an embedding_vl model")
	assert.Equal(t, "text-embedding-v4", req.Model)
	assert.Equal(t, 0, requestCount, "capability mismatch should not call upstream")
}

func TestEmbeddings_DefaultModelTypeTextInputRoutesToNativeTextAPI(t *testing.T) {
	tests := []struct {
		name      string
		modelType string
	}{
		{name: "empty", modelType: ""},
		{name: "unknown", modelType: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				assert.Equal(t, "/api/v1/services/embeddings/text-embedding/text-embedding", r.URL.Path)

				body, _ := io.ReadAll(r.Body)
				var req bailianTextEmbeddingReq
				require.NoError(t, json.Unmarshal(body, &req))
				assert.Equal(t, "custom-text-embedding", req.Model)
				assert.Equal(t, []string{"hello"}, req.Input.Texts)

				resp := bailianTextEmbeddingResp{
					Usage: struct {
						TotalTokens int `json:"total_tokens"`
					}{TotalTokens: 3},
				}
				resp.Output.Embeddings = []bailianTextEmbeddingItem{
					{TextIndex: 0, Embedding: []float64{0.1}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			provider := newTestProvider(t, server.URL)
			resp, err := provider.Embeddings(context.Background(), &core.EmbeddingRequest{
				Model:     "custom-text-embedding",
				ModelType: tt.modelType,
				Input:     "hello",
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, "custom-text-embedding", resp.Model)
			assert.Equal(t, 1, requestCount, "text input should call native text API once")
		})
	}
}

func TestEmbeddings_DefaultModelTypeRejectsMultimodalInputWithoutUpstreamCall(t *testing.T) {
	tests := []struct {
		name      string
		modelType string
	}{
		{name: "empty", modelType: ""},
		{name: "unknown", modelType: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			provider := newTestProvider(t, server.URL)
			req := &core.EmbeddingRequest{
				Model:     "text-embedding-v4",
				ModelType: tt.modelType,
				Input: []interface{}{
					map[string]interface{}{"image": "data:image/png;base64,AAA"},
				},
			}

			_, err := provider.Embeddings(context.Background(), req)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "multimodal input requires an embedding_vl model")
			assert.Equal(t, "text-embedding-v4", req.Model)
			assert.Equal(t, 0, requestCount, "missing or unknown model_type should reject multimodal input before upstream")
		})
	}
}

func TestMultimodalEmbeddings_DoesNotAutoSwitchModelName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "text-embedding-v4", req.Model)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.2}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	req := &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: []interface{}{
			map[string]interface{}{"image": "https://example.com/image.png"},
		},
	}

	resp, err := provider.multimodalEmbeddings(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "text-embedding-v4", req.Model)
	assert.Equal(t, "text-embedding-v4", resp.Model)
}

// ─── Concurrency limit test ─────────────────────────────────────────────────

func TestConcurrencyLimit_Respected(t *testing.T) {
	var mu sync.Mutex
	maxConcurrent := 0
	currentConcurrent := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		mu.Unlock()

		// Simulate slow response to increase overlap
		// (but keep test fast)

		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{0.1},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		mu.Lock()
		currentConcurrent--
		mu.Unlock()
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	// 50 texts → 5 batches, concurrency limit is 3
	texts := makeTexts(50)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: texts,
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 50)

	mu.Lock()
	observed := maxConcurrent
	mu.Unlock()

	assert.LessOrEqual(t, observed, maxEmbeddingConcurrency,
		"max concurrent requests (%d) should not exceed limit (%d)",
		observed, maxEmbeddingConcurrency)
}

// ─── Edge case: single text via array ───────────────────────────────────────

func TestNativeEmbeddings_SingleTextArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		require.Len(t, req.Input.Texts, 1)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 7},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.5, 0.6}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: []string{"single"},
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, 0, resp.Data[0].Index)
	assert.Equal(t, 7, resp.Usage.TotalTokens)
}

// ─── Batch boundary tests ──────────────────────────────────────────────────

func TestNativeEmbeddings_ExactlyBatchBoundary(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		mu.Lock()
		requestCount++
		mu.Unlock()

		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{0.1},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: len(req.Input.Texts) * 5},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	// Exactly 20 texts = 2 batches of 10
	texts := makeTexts(20)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: texts,
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 20)

	mu.Lock()
	assert.Equal(t, 2, requestCount, "exactly 20 texts should produce 2 batch requests")
	mu.Unlock()

	// Verify indices
	for i, d := range resp.Data {
		assert.Equal(t, i, d.Index)
	}
}

// ─── Verify request headers ─────────────────────────────────────────────────

func TestNativeEmbeddings_SetsAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 1},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "test",
	})
	require.NoError(t, err)
}

// ─── Verify embedding data format ───────────────────────────────────────────

func TestNativeEmbeddings_EmbeddingDataFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "test",
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "embedding", resp.Data[0].Object)

	// Verify embedding is stored as json.RawMessage
	var embedding []float64
	require.NoError(t, json.Unmarshal(resp.Data[0].Embedding, &embedding))
	assert.Equal(t, []float64{0.1, 0.2, 0.3}, embedding)
}

// ─── Verify no trailing empty strings in splitTextsByCount ──────────────────

func TestSplitTextsByCount_NoEmptyBatches(t *testing.T) {
	for _, n := range []int{1, 9, 10, 11, 20, 99, 100, 101} {
		texts := makeTexts(n)
		batches := splitTextsByCount(texts, 10)

		for i, batch := range batches {
			assert.NotEmpty(t, batch, "batch %d should not be empty (n=%d)", i, n)
		}

		// Verify total count
		total := 0
		for _, batch := range batches {
			total += len(batch)
		}
		assert.Equal(t, n, total, "total texts across all batches should equal %d", n)
	}
}

// ─── Embeddings with interface{} input ──────────────────────────────────────

func TestNativeEmbeddings_InterfaceArrayInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{float64(i)},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: []interface{}{"hello", "world"},
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 2)
}

// ─── 1000 条全链路集成测试 ─────────────────────────────────────────────────

func TestNativeEmbeddings_1000Texts_FullIntegration(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	batchSizes := make([]int, 0, 100)
	maxConcurrent := 0
	currentConcurrent := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		requestCount++
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		json.Unmarshal(body, &req)

		mu.Lock()
		batchSizes = append(batchSizes, len(req.Input.Texts))
		mu.Unlock()

		// 每批返回对应数量的 embedding
		items := make([]bailianTextEmbeddingItem, len(req.Input.Texts))
		for i := range req.Input.Texts {
			items[i] = bailianTextEmbeddingItem{
				TextIndex: i,
				Embedding: []float64{float64(i) * 0.01, float64(i) * 0.02},
			}
		}

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: len(req.Input.Texts) * 100},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		mu.Lock()
		currentConcurrent--
		mu.Unlock()
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	texts := makeTexts(1000)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: texts,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// 验证 100 批请求
	mu.Lock()
	assert.Equal(t, 100, requestCount, "1000 texts should produce 100 batch requests")
	assert.Equal(t, 100, len(batchSizes))
	// 每批应该是 10 条
	for i, sz := range batchSizes {
		assert.Equal(t, 10, sz, "batch %d should have 10 texts", i)
	}
	// 并发不超过 3
	assert.LessOrEqual(t, maxConcurrent, maxEmbeddingConcurrency,
		"max concurrent requests (%d) should not exceed %d", maxConcurrent, maxEmbeddingConcurrency)
	mu.Unlock()

	// 验证 1000 条结果
	require.Len(t, resp.Data, 1000, "should return 1000 embedding results")

	// 验证索引从 0 到 999 连续且有序
	for i, d := range resp.Data {
		assert.Equal(t, i, d.Index, "data[%d].Index should be %d, got %d", i, i, d.Index)
		assert.Equal(t, "embedding", d.Object)
		// 验证 embedding 不为空
		assert.NotEmpty(t, d.Embedding)
	}

	// 验证总 token 数 = 100 * 10 * 100 = 100000
	assert.Equal(t, 100000, resp.Usage.TotalTokens)
	assert.Equal(t, 100000, resp.Usage.PromptTokens)
	assert.Equal(t, "text-embedding-v4", resp.Model)
	assert.Equal(t, "list", resp.Object)
}

func TestMultimodalEmbeddings_1000Texts_FullIntegration(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	maxConcurrent := 0
	currentConcurrent := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		requestCount++
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		json.Unmarshal(body, &req)

		items := make([]bailianMultimodalEmbeddingItem, len(req.Input.Contents))
		for i := range req.Input.Contents {
			items[i] = bailianMultimodalEmbeddingItem{
				Index:     i,
				Embedding: []float64{float64(i) * 0.01},
			}
		}

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: len(req.Input.Contents) * 50},
		}
		resp.Output.Embeddings = items
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		mu.Lock()
		currentConcurrent--
		mu.Unlock()
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	texts := makeTexts(1000)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: texts,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// 验证请求数
	mu.Lock()
	assert.Equal(t, 100, requestCount, "1000 texts should produce 100 batch requests")
	assert.LessOrEqual(t, maxConcurrent, maxEmbeddingConcurrency,
		"max concurrent should not exceed %d", maxEmbeddingConcurrency)
	mu.Unlock()

	// 验证结果
	require.Len(t, resp.Data, 1000)
	for i, d := range resp.Data {
		assert.Equal(t, i, d.Index)
		assert.NotEmpty(t, d.Embedding)
	}

	// 总 token = 100 * 10 * 50 = 50000
	assert.Equal(t, 50000, resp.Usage.TotalTokens)
}

func TestRerank_1000Documents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianRerankReq
		json.Unmarshal(body, &req)

		// rerank 不需要分批，直接验证 1000 条文档都能传过去
		assert.Len(t, req.Input.Documents, 1000)
		assert.Equal(t, "test query", req.Input.Query)
		assert.Equal(t, "gte-rerank-v2", req.Model)

		// 返回 top 5 结果
		results := []bailianRerankResult{
			{Index: 500, RelevanceScore: 0.99},
			{Index: 0, RelevanceScore: 0.95},
			{Index: 999, RelevanceScore: 0.88},
			{Index: 250, RelevanceScore: 0.72},
			{Index: 750, RelevanceScore: 0.61},
		}

		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5000},
		}
		resp.Output.Results = results
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)

	docs := make([]any, 1000)
	for i := 0; i < 1000; i++ {
		docs[i] = fmt.Sprintf("document-%d", i)
	}

	topN := 5
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "test query",
		Documents: docs,
		TopN:      &topN,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Results, 5)
	assert.Equal(t, 500, resp.Results[0].Index)
	assert.InDelta(t, 0.99, resp.Results[0].RelevanceScore, 0.001)
	assert.Equal(t, 999, resp.Results[2].Index)
	assert.Equal(t, 5000, resp.Usage.TotalTokens)
}

// ─── P0: parseMultimodalInputs tests ────────────────────────────────────────

func TestParseMultimodalInputs_String(t *testing.T) {
	contents, err := parseMultimodalInputs("hello")
	require.NoError(t, err)
	require.Len(t, contents, 1)
	assert.Equal(t, "hello", contents[0].Text)
	assert.Empty(t, contents[0].Image)
	assert.Empty(t, contents[0].Audio)
	assert.Nil(t, contents[0].Factor)
}

func TestParseMultimodalInputs_EmptyString(t *testing.T) {
	contents, err := parseMultimodalInputs("")
	assert.NoError(t, err)
	assert.Nil(t, contents)
}

func TestParseMultimodalInputs_StringArray(t *testing.T) {
	contents, err := parseMultimodalInputs([]string{"hello", "world"})
	require.NoError(t, err)
	require.Len(t, contents, 2)
	assert.Equal(t, "hello", contents[0].Text)
	assert.Equal(t, "world", contents[1].Text)
}

func TestParseMultimodalInputs_ImageItem(t *testing.T) {
	contents, err := parseMultimodalInputs([]interface{}{
		map[string]interface{}{"image": "data:image/png;base64,iVBORw0KGgo="},
	})
	require.NoError(t, err)
	require.Len(t, contents, 1)
	assert.Empty(t, contents[0].Text)
	assert.Equal(t, "data:image/png;base64,iVBORw0KGgo=", contents[0].Image)
}

func TestParseMultimodalInputs_AudioItem(t *testing.T) {
	contents, err := parseMultimodalInputs([]interface{}{
		map[string]interface{}{"audio": "https://example.com/audio.mp3"},
	})
	require.NoError(t, err)
	require.Len(t, contents, 1)
	assert.Empty(t, contents[0].Text)
	assert.Equal(t, "https://example.com/audio.mp3", contents[0].Audio)
}

func TestParseMultimodalInputs_MixedItems(t *testing.T) {
	contents, err := parseMultimodalInputs([]interface{}{
		map[string]interface{}{"text": "describe this image", "factor": 1.0},
		map[string]interface{}{"image": "https://example.com/img.jpg", "factor": 0.5},
		map[string]interface{}{"audio": "https://example.com/audio.mp3"},
	})
	require.NoError(t, err)
	require.Len(t, contents, 3)

	assert.Equal(t, "describe this image", contents[0].Text)
	assert.NotNil(t, contents[0].Factor)
	assert.InDelta(t, 1.0, *contents[0].Factor, 0.001)

	assert.Equal(t, "https://example.com/img.jpg", contents[1].Image)
	assert.NotNil(t, contents[1].Factor)
	assert.InDelta(t, 0.5, *contents[1].Factor, 0.001)

	assert.Equal(t, "https://example.com/audio.mp3", contents[2].Audio)
	assert.Nil(t, contents[2].Factor)
}

func TestParseMultimodalInputs_MixedStringsAndObjects(t *testing.T) {
	contents, err := parseMultimodalInputs([]interface{}{
		"plain string",
		map[string]interface{}{"image": "https://img.jpg"},
	})
	require.NoError(t, err)
	require.Len(t, contents, 2)
	assert.Equal(t, "plain string", contents[0].Text)
	assert.Equal(t, "https://img.jpg", contents[1].Image)
}

func TestParseMultimodalInputs_EmptyObject(t *testing.T) {
	contents, err := parseMultimodalInputs([]interface{}{
		map[string]interface{}{},
	})
	assert.Error(t, err)
	assert.Nil(t, contents)
	assert.Contains(t, err.Error(), "must have at least one of text/image/audio")
}

func TestParseMultimodalInputs_UnsupportedType(t *testing.T) {
	contents, err := parseMultimodalInputs(42)
	assert.Error(t, err)
	assert.Nil(t, contents)
	assert.Contains(t, err.Error(), "unsupported multimodal input type")
}

// ─── P0: splitMultimodalByCount tests ───────────────────────────────────────

func TestSplitMultimodalByCount_Basic(t *testing.T) {
	contents := []bailianMultimodalContent{
		{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}, {Text: "e"},
		{Text: "f"}, {Text: "g"}, {Text: "h"}, {Text: "i"}, {Text: "j"},
		{Text: "k"},
	}
	batches := splitMultimodalByCount(contents, 10)
	require.Len(t, batches, 2)
	assert.Equal(t, 10, len(batches[0]))
	assert.Equal(t, 1, len(batches[1]))
}

func TestSplitMultimodalByCount_Empty(t *testing.T) {
	batches := splitMultimodalByCount([]bailianMultimodalContent{}, 10)
	assert.Nil(t, batches)
}

// ─── P0: Multimodal embedding with image/audio integration tests ────────────

func TestMultimodalEmbeddings_WithImageInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "qwen3-vl-embedding", req.Model)
		require.Len(t, req.Input.Contents, 2)
		assert.Equal(t, "describe this", req.Input.Contents[0].Text)
		assert.Equal(t, "data:image/png;base64,xxx", req.Input.Contents[1].Image)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 20},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.1, 0.2}},
			{Index: 1, Embedding: []float64{0.3, 0.4}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: []interface{}{
			map[string]interface{}{"text": "describe this"},
			map[string]interface{}{"image": "data:image/png;base64,xxx"},
		},
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, 0, resp.Data[0].Index)
	assert.Equal(t, 1, resp.Data[1].Index)
}

func TestMultimodalEmbeddings_WithFactor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.Len(t, req.Input.Contents, 1)
		require.NotNil(t, req.Input.Contents[0].Factor)
		assert.InDelta(t, 0.8, *req.Input.Contents[0].Factor, 0.001)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.5}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: []interface{}{
			map[string]interface{}{"text": "hello", "factor": 0.8},
		},
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

// ─── P1: Text embedding params tests ────────────────────────────────────────

func TestNativeEmbeddings_WithTextType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		assert.Equal(t, "query", req.Parameters.TextType)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "search query",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"text_type": json.RawMessage(`"query"`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

func TestNativeEmbeddings_WithDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		require.NotNil(t, req.Parameters.Dimension)
		assert.Equal(t, 1024, *req.Parameters.Dimension)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dim := 1024
	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model:      "text-embedding-v4",
		Input:      "test",
		Dimensions: &dim,
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

func TestNativeEmbeddings_WithInstruct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		assert.Equal(t, "Classify the sentiment", req.Parameters.Instruct)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "test",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"instruct": json.RawMessage(`"Classify the sentiment"`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

func TestNativeEmbeddings_WithOutputType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		assert.Equal(t, "dense&sparse", req.Parameters.OutputType)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "test",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"output_type": json.RawMessage(`"dense&sparse"`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

func TestNativeEmbeddings_NoExtraParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianTextEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		// No extra params → Parameters should be nil (omitempty)
		assert.Nil(t, req.Parameters)

		resp := bailianTextEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		}
		resp.Output.Embeddings = []bailianTextEmbeddingItem{
			{TextIndex: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.nativeEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-v4",
		Input: "test",
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

// ─── P1: Multimodal embedding params tests ──────────────────────────────────

func TestMultimodalEmbeddings_WithEnableFusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		require.NotNil(t, req.Parameters.EnableFusion)
		assert.True(t, *req.Parameters.EnableFusion)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: "test",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"enable_fusion": json.RawMessage(`true`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

func TestMultimodalEmbeddings_WithVideoParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianMultimodalEmbeddingReq
		require.NoError(t, json.Unmarshal(body, &req))
		require.NotNil(t, req.Parameters)
		require.NotNil(t, req.Parameters.FPS)
		assert.InDelta(t, 0.5, *req.Parameters.FPS, 0.001)
		require.NotNil(t, req.Parameters.MaxVideoFrames)
		assert.Equal(t, 32, *req.Parameters.MaxVideoFrames)

		resp := bailianMultimodalEmbeddingResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Embeddings = []bailianMultimodalEmbeddingItem{
			{Index: 0, Embedding: []float64{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.multimodalEmbeddings(context.Background(), &core.EmbeddingRequest{
		Model: "qwen3-vl-embedding",
		Input: "test",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"fps":              json.RawMessage(`0.5`),
			"max_video_frames": json.RawMessage(`32`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Data, 1)
}

// ─── P1: Rerank with instruct test ──────────────────────────────────────────

func TestRerank_WithInstruct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req bailianRerankReq
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "Prioritize recent documents", req.Parameters.Instruct)

		resp := bailianRerankResp{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		resp.Output.Results = []bailianRerankResult{
			{Index: 0, RelevanceScore: 0.9},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "query",
		Documents: []any{"doc1"},
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"instruct": json.RawMessage(`"Prioritize recent documents"`),
		}),
	})

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
}

// Ensure unused import is used
var _ = strings.Contains
