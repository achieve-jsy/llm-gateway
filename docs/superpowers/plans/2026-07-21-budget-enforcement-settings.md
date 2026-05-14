# Budget Enforcement Settings Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose four budget interception flags in admin System Settings with hot-reload via snapshot publish (no restart).

**Architecture:** Persist flags in `system_configs`; overlay onto `BudgetTierSnapshotPublisher` settings; republish Redis snapshot on save. Env/YAML seeds only when DB keys are absent.

**Tech Stack:** Go control-plane settings + publisher; Vue SettingsPage; existing Redis snapshot pipeline.

## File map

- `internal/controlplane/model/settings.go` — fields on `SystemSettings`
- `internal/controlplane/service/settings_service.go` — keys, get/update, seed, sync publisher
- `internal/controlplane/service/budget_tier_snapshot_publisher.go` — `UpdateEnforcement` + mutex
- `internal/controlplane/router/router.go` — wire seed + defaults + publisher hook
- `internal/controlplane/handler/settings_handler.go` — map validation errors to 400
- `tests/controlplane/service/budget_enforcement_settings_test.go` — unit tests
- `web/src/types/settings.ts` + `SettingsPage.vue` — UI tab

## Tasks

### Task 1: Publisher UpdateEnforcement + settings persistence

**Files:** publisher, settings model/service, router wire-up, tests

### Task 2: Settings API validation + handler 400

### Task 3: Frontend tab + form fields

### Task 4: Verify `go test` + vitest/build as needed
