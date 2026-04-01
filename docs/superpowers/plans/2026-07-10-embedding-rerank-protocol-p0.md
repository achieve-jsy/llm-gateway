# Embedding/Rerank Protocol P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce the protocol design iron rule: never silently rewrite `req.Model`; reject multimodal embedding input against non-`embedding_vl` models with HTTP 400; keep Bailian endpoint routing driven by `model_type`.

**Architecture:** Capability mismatch is validated in the gateway embeddings handler (after group dispatch fills `ModelType`). Bailian adapter only selects text vs multimodal API from `ModelType` and must not auto-switch models. Debug logs must not dump full multimodal payloads.

**Tech Stack:** Go, existing `internal/gateway/handler`, `internal/providers/bailian`, `tests/` mirror layout, `core.NewInvalidRequestError`.

**Spec:** `docs/superpowers/specs/2026-07-10-embedding-rerank-protocol-design.md` §5.2, §6.3, §8, §10 P0.

**Worktree:** `/Users/jsy/Public/self_code/llm-gateway/.claude/worktrees/compassionate-colden-79f1c0`  
**Branch:** `feat/bailian-embedding-rerank-p0p1`

---

## File map

| File | Responsibility |
|------|----------------|
| `internal/providers/bailian/bailian.go` | Remove auto model switch; route by `ModelType`; sanitize logs |
| `internal/providers/bailian/bailian_test.go` | Tests for no model rewrite + routing |
| `internal/gateway/handler/embeddings.go` | After dispatch: multimodal input + type≠embedding_vl → 400 |
| `tests/gateway/handler/embeddings_test.go` | Handler capability mismatch tests |
| `internal/core/types.go` or errors | Reuse existing invalid-request error helpers if present |

---

### Task 1: Bailian — remove silent model switch + route by model_type

**Files:**
- Modify: `internal/providers/bailian/bailian.go`
- Modify: `internal/providers/bailian/bailian_test.go` (or `tests/providers/bailian/` if that is the project convention — prefer existing colocated `bailian_test.go`)

- [ ] **Step 1: Write failing tests**

Cover:
1. `multimodalEmbeddings` with `Model=text-embedding-v4` and image input must **not** change `req.Model` to `qwen3-vl-embedding` (assert model sent upstream equals original, or assert 400 if you choose adapter-level reject — prefer handler-level 400; adapter should keep model unchanged when called with embedding_vl path).
2. Prefer: when `Embeddings` is called with multimodal input and `ModelType=""` or `"embedding"`, either handler blocks first OR adapter returns invalid request **without** rewriting model. Spec: adapter must not rewrite. If adapter is invoked incorrectly, return error rather than switch.
3. `ModelType=embedding` + text → native text API path.
4. `ModelType=embedding_vl` + multimodal → multimodal API path, model name unchanged.
5. Remove/adjust any existing tests that expect auto-switch to `qwen3-vl-embedding`.

- [ ] **Step 2: Implement**

In `bailian.go`:
1. Delete the block in `multimodalEmbeddings` that calls `isMultimodalCapableModel` and sets `req.Model = "qwen3-vl-embedding"`.
2. Delete `isMultimodalCapableModel` if unused.
3. Change `Embeddings` entry routing:
   - Primary: `switch req.ModelType` (`embedding_vl` → multimodal, `embedding` → native, default → existing text-then-multimodal fallback for empty type).
   - **Do not** early-route solely on `isMultimodalInput` in a way that bypasses capability checks; if keeping input-based route as safety net, when multimodal input arrives with `ModelType=="embedding"`, return `core.NewInvalidRequestError` (or WithStatus 400) with a clear message — do **not** rewrite model.
4. Soften debug logs: log model, model_type, input_type, is_multimodal; **do not** log full `input_value` (may contain huge base64).

- [ ] **Step 3: Run tests**

```bash
cd /Users/jsy/Public/self_code/llm-gateway/.claude/worktrees/compassionate-colden-79f1c0
go test ./internal/providers/bailian/ -count=1
```

- [ ] **Step 4: Commit** (only if user previously allowed commits in this session — otherwise leave unstaged; parent will commit if requested)

```bash
git add internal/providers/bailian/bailian.go internal/providers/bailian/bailian_test.go
git commit -m "$(cat <<'EOF'
fix(bailian): 禁止多模态输入时静默切换 embedding 模型

按 model_type 选择 text/multimodal API；能力不匹配返回错误而非改写 req.Model。
EOF
)"
```

---

### Task 2: Gateway handler — capability mismatch 400

**Files:**
- Modify: `internal/gateway/handler/embeddings.go`
- Modify: `tests/gateway/handler/embeddings_test.go`

- [ ] **Step 1: Write failing tests**

After mock dispatch returns `ModelType: "embedding"` (or empty treated as non-vl), request body with input `[{"image":"http://x"}]` must respond **400** and must **not** call orchestrator Embeddings (if mockable).

Dispatch returns `ModelType: "embedding_vl"` + same multimodal input → proceeds (orchestrator called or success path).

Pure text input + `embedding` → proceeds.

- [ ] **Step 2: Implement helper + check**

Add a small helper (in handler file or `internal/gateway/handler/embedding_input.go`):

```go
func isMultimodalEmbeddingInput(input any) bool
```

Same semantics as bailian's check: `[]interface{}` of maps with `image` or `audio` keys.

In `Handle`, **after** `groupDispatch.Dispatch` fills `req.ModelType` (and also if no dispatch / direct model), if `isMultimodalEmbeddingInput(req.Input) && req.ModelType != "embedding_vl"`, return 400 JSON error with message like: multimodal input requires an embedding_vl model/group. Prefer existing error response helpers used by the handler.

Suggested code: `model_capability_mismatch` if the project has a WithCode pattern; otherwise clear invalid_request message is fine.

- [ ] **Step 3: Run tests**

```bash
go test ./tests/gateway/handler/ -count=1
go test ./internal/gateway/handler/ -count=1
```

- [ ] **Step 4: Commit** (same policy as Task 1)

```bash
git add internal/gateway/handler/embeddings.go tests/gateway/handler/embeddings_test.go
# include any new helper file
git commit -m "$(cat <<'EOF'
fix(gateway): 多模态 embedding 输入校验 model_type

非 embedding_vl 模型收到 image/audio input 时返回 400，避免错误路由与向量空间污染。
EOF
)"
```

---

### Task 3: Integration smoke + build

**Files:** none new (verification only)

- [ ] **Step 1: Build and targeted tests**

```bash
go build ./...
go test ./internal/providers/bailian/ ./tests/gateway/handler/ -count=1
```

- [ ] **Step 2: Confirm no remaining auto-switch**

```bash
rg -n "qwen3-vl-embedding|isMultimodalCapableModel|Auto-switching" internal/providers/bailian/
```

Expect: model name may still appear in tests as the **configured** VL model, but no assignment that overwrites unrelated models.

---

### Task 4 (optional P1 stretch — only if P0 done and time permits): docs note

- [ ] Add a short "P0 implemented" note under the design doc §10, or leave for parent.

---

## Parallelism

- Task 1 and Task 2 touch different packages → **safe to run in parallel**.
- Task 3 runs after both complete.
- Do **not** commit unless the parent agent / user explicitly asked to commit in the worker prompt.

## Out of scope (later)

- Jina/Voyage adapters
- Group-level auto-prefer VL member
- Moving Bailian text embed to compatible-mode
- Swagger formalization of multimodal input
