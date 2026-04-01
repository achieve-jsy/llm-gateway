# VL Rerank Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `/v1/rerank` to accept SiliconFlow-style `string | {text|image|video}` inputs, gate multimodal with `model_type=rerank_vl`, and map Bailian to DashScope native API.

**Architecture:** Widen `RerankRequest.Query/Documents` to `any`; validate parts in handler; reject multimodal unless `rerank_vl`; Bailian adapter forwards objects into native `input`.

**Tech Stack:** Go, existing gateway handler / bailian provider / controlplane model_type bindings.

**Spec:** `docs/superpowers/specs/2026-07-10-vl-rerank-protocol-design.md`

---

### Task 1: Core types + validation helpers

**Files:**
- Modify: `internal/core/types.go` (`RerankRequest`)
- Create/Modify: helpers in `internal/gateway/handler/rerank.go` or small helper file
- Test: `internal/gateway/handler/rerank_test.go` / `tests/gateway/handler/`

- [x] Query `any`, Documents `[]any`; add `ModelType string \`json:"-"\``
- [x] `isMultimodalRerankInput`, `validateRerankQuery`, `validateRerankDocuments`
- [x] Unit tests for string / object / invalid shapes

### Task 2: Handler capability gate

**Files:**
- Modify: `internal/gateway/handler/rerank.go`
- Test: rerank handler tests

- [x] After dispatch, multimodal + ModelType != `rerank_vl` → 400 `model_capability_mismatch`
- [x] Keep empty-query / empty-documents checks working for string and objects

### Task 3: Bailian adapter

**Files:**
- Modify: `internal/providers/bailian/bailian.go` (`bailianRerankInput`)
- Test: `internal/providers/bailian/bailian_test.go`

- [x] `Query any`, `Documents []any` in native request body
- [x] Pass through string/object; return_documents only for string docs when possible
- [x] Tests: text path unchanged; VL object body reaches upstream JSON

### Task 4: `rerank_vl` control plane + infer

**Files:**
- Modify: `model_service.go` Create/Update oneof
- Modify: `sync_service.go` InferModelType
- Modify: frontend `ModelDialog.vue` if needed
- Test: model_service / sync_service tests

- [x] Add `rerank_vl` to bindings
- [x] Infer: name has rerank + (vl|multimodal|vision) → `rerank_vl`

### Task 5: Verify

- [x] `go test` bailian, gateway handler, controlplane relevant packages
- [ ] Optional: live aimeter VL image query if env available
