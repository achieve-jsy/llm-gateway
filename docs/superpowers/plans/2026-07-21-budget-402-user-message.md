# Budget 402 User Message Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** v2 预算 402 的 `error.message` 改为带周期/等级细节的中文提示。

**Architecture:** 在 `internal/gateway/budget` 增加纯函数 `exceededMessage(Constraint) string`，由 `ExceededError.WriteHTTPResponse` 使用；拦截逻辑与 JSON 其它字段不变。

**Tech Stack:** Go, gin, 现有 `tests/gateway` 表驱动单测。

**Spec:** `docs/superpowers/specs/2026-07-21-budget-402-user-message-design.md`

## Global Constraints

- 只改 v2 `ExceededError` 文案；不扩展新 JSON 字段。
- `code`/`type`/`gate`/`additional_constraints` 行为不变。
- 文案规则以 design spec 表格为准。

---

## Task 1: 表驱动单测 `exceededMessage` + 更新 402 集成断言

**Files:**
- Create: `tests/gateway/budget/exceeded_message_test.go`
- Modify: `tests/gateway/middleware/budget_middleware_test.go`

- [x] 写失败测试：覆盖 total/tier × daily/weekly/monthly × TierName 有无 × StaticZero
- [x] 更新 `TestMiddleware_IncludesBudgetTierInfoIn402Response` 断言中文 message
- [x] `go test` 确认失败

## Task 2: 实现 `exceededMessage` 并接线

**Files:**
- Modify: `internal/gateway/budget/gate.go`（或同包小文件 `exceeded_message.go`）

- [x] 实现周期映射与 message 规则
- [x] `WriteHTTPResponse` 使用 `exceededMessage(e.Primary)`
- [x] `go test ./tests/gateway/budget/ ./tests/gateway/middleware/ -count=1` 通过
- [x] Commit
