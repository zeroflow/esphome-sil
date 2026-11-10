---
phase: 01-closed-loop-fan-skeleton
fixed_at: 2026-11-09T07:00:00Z
review_path: .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-11-09T07:00:00Z
**Source review:** .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
**Iteration:** 3

**Summary:**
- Findings in scope: 1 (fix_scope critical_warning; IN-01 and IN-02 are out of scope)
- Fixed: 1
- Skipped: 0

## Fixed Issues

### WR-01: `FwRestart` clears an `engine_failed` failure while the engine is still dead

**Files modified:** `internal/supervisor/supervisor.go`, `internal/supervisor/policy_test.go`
**Commit:** cab20ad
**Applied fix:** `cycleFirmware` now resets only `apiReady`/`apiFailure` and clears `s.failure` only when its code is not `engine_failed`, mirroring the check in `SimRestart`. A firmware crash loop or failed auto-restart is still dropped on a requested restart. Added `TestFwRestartKeepsEngineFailed`: a failing `SimRestart` (engine cannot bind its bus path), then `FwRestart`, then asserts the call returns `engine_failed`, `Status().Error.Code == "engine_failed"` and the phase is `failed`. Without the fix the test does not finish (`FwRestart` waits for readiness against a dead engine); with it the test passes. This is a state-handling (logic) fix: requires human verification of the intended semantics.

## Skipped Issues

None in scope. Not attempted (out of scope for `critical_warning`): IN-01 (SIGHUP handling for `nohup`), IN-02 (compile-failure classifier verified only against hand-written logs).

## Verification

Run in the main checkout (no worktree), on branch `claude/main`:
- `make check`: passes (gofmt, go vet, all Go packages with the new test, 112 checks passed, 0 failed).
- `make e2e E2E_RUN='TestFwRestartResync|TestSimRestartResync'`: both PASS (TestSimRestartResync 7.23s, TestFwRestartResync 8.94s).

---

_Fixed: 2026-11-09T07:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_
