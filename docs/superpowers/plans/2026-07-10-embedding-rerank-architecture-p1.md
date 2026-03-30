# Embedding/Rerank Architecture Closure P1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close three architecture gaps: UpdateModel accepts `embedding_vl`; group members must share one `model_type`; Bailian empty `model_type` no longer silently falls back to multimodal.

**Architecture:** Control-plane enforces capability consistency at write time; Bailian adapter treats missing type conservatively (text-only, never multimodal fallback); gateway handler already 400s multimodal≠vl (P0).

**Tech Stack:** Go, existing controlplane services/repos, bailian provider tests.

**Worktree:** `/Users/jsy/Public/self_code/llm-gateway/.claude/worktrees/compassionate-colden-79f1c0`  
**Branch:** `feat/bailian-embedding-rerank-p0p1`  
**No DB migration** (varchar model_type already stores embedding_vl).

**Do NOT commit** unless parent explicitly asks.

---

### Task 1: UpdateModel binding includes `embedding_vl`

**Files:**
- Modify: `internal/controlplane/service/model_service.go` (`UpdateModelInput.ModelType` binding)
- Modify: `tests/controlplane/service/model_service_test.go` (add case updating to embedding_vl)

- [ ] Change `oneof=chat embedding rerank` → `oneof=chat embedding embedding_vl rerank` on `UpdateModelInput.ModelType` (line ~213). Create already has embedding_vl — keep them identical.
- [ ] Add/extend unit test that UpdateModel with `ModelType: "embedding_vl"` succeeds (mock repo).
- [ ] Run: `go test ./tests/controlplane/service/ -count=1 -run UpdateModel`

---

### Task 2: Group member model_type consistency

**Files:**
- Modify: `internal/controlplane/service/group_service.go`
- Modify: `internal/controlplane/router/router.go` (wire modelRepo into GroupService)
- Modify: `internal/controlplane/repository/group_repo.go` if needed for listing existing member types
- Modify: `tests/controlplane/service/group_service_test.go`
- Optional FE: warn in group add-model UI if easy without large refactor — backend enforcement is required; FE is nice-to-have in same task only if small.

**Behavior:**
1. When `AddModelsToGroup`, load existing group members' `model_type` (join models) and the new models via `modelRepo.GetByIDs`.
2. All types in the union must be identical (ignore empty only if you treat empty as error — prefer: empty model_type on a model → reject add with clear error).
3. Empty group: all newly added models in the same request must share one type.
4. New error: `ErrGroupModelTypeMismatch` with message like `group models must share the same model_type`.
5. Extend `NewGroupService` to accept `*repository.ModelRepo` (or a narrow interface `GetByIDs`). Update `router.go` call site. Keep tests compiling (`NewGroupService(nil)` may need `NewGroupService(nil, nil)` or optional setter — prefer two-arg constructor and fix call sites).

**List existing types:** either extend `ListGroupModels` select to include `models.model_type`, or query via modelRepo after listing member IDs. Prefer one clear approach.

- [ ] Implement + tests (table-driven: empty group ok; mismatch reject; matching ok; update existing membership still ok)
- [ ] Run: `go test ./tests/controlplane/service/ -count=1 -run Group` and any package tests for group_service
- [ ] `go build ./cmd/control-plane/`

---

### Task 3: Bailian — tighten empty model_type

**Files:**
- Modify: `internal/providers/bailian/bailian.go`
- Modify: `internal/providers/bailian/bailian_test.go`

**Behavior (replace current default branch text-then-multimodal):**

```
switch ModelType:
  embedding_vl → multimodal
  embedding → text; if multimodal input → invalid request (already)
  default/empty:
    if multimodal input → invalid request ("model_type required" / must be embedding_vl)
    else → native text only  // NO fallback to multimodal
```

- [ ] Update/remove tests that expected empty-type multimodal fallback
- [ ] Add tests for empty+text → text API; empty+image → error; no model rewrite
- [ ] Run: `go test ./internal/providers/bailian/ -count=1`

**Do not touch** `internal/gateway/handler/*` (P0 already handles multimodal≠vl).

---

### Task 4: Verify (parent)

```bash
go build ./...
go test ./internal/providers/bailian/ ./tests/controlplane/service/ ./tests/gateway/handler/ ./internal/errors/ -count=1
```

---

## Parallelism

Tasks 1–3 touch different packages → **run in parallel**.  
Task 4 after all complete.
