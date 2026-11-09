---
phase: 02-full-rev-3-3-bench
fixed_at: 2026-11-09T00:00:00Z
review_path: .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-11-09
**Source review:** .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
**Iteration:** 3 (final)

**Summary:**
- Findings in scope: 1 (WR-01)
- Fixed: 1
- Skipped: 0
- Extra, by instruction: IN-10 included (see below)

Edits were made directly on the main tree (`/home/claude/esphome-sil`, branch `claude/main`), with no worktree and no stash, because the tree holds uncommitted planning files. The two fix commits touch only their own source file each. No planning file was staged.

## Fixed Issues

### WR-01: `TestSupervisorRebootExit` asserts the probe count before the probe is started

**Files modified:** `internal/supervisor/policy_test.go`
**Commit:** 8542d9d
**Applied fix:** The wait condition is now `r == 1 && running && probes.count() == 2`, so the test waits for the quantity it asserts. The wait is renamed "the restart and its probe" and has a comment explaining the order. `autoRestart` sets `fwRunning` before `startProbe`, and the probe counts from its own goroutine. The later `st.count() != 2 || probes.count() != 2` assertion is unchanged.

## Out-of-scope item included: IN-10

**Included: yes.**
**Files modified:** `internal/supervisor/supervisor.go`
**Commit:** b976a6c
**Applied fix:** `SimRestart` now has `defer s.endEngGap()` right after the section that sets `engGap = true`. The four explicit clear points are untouched: the sleep abort, the `startEngine` error, the stopping abort and the normal install.

Why it was safe:
- `endEngGap` only sets the flag to false, so it is idempotent after the explicit clears.
- The defer runs before `defer s.opMu.Unlock()`, because defers run last-in-first-out and `opMu` is deferred earlier. No other `SimRestart` can set the flag between the explicit clear and the deferred one.
- A `Set` during the resync wait sees `false`, the same value the explicit clear already left.
- `s.mu` is never held across a call that can panic. The only `mu` sections between the set and the clears are plain field writes, so the defer cannot self-deadlock.
- The explicit clear on the install path still shares one `mu` section with `s.slot = sl`, so `Set` never sees the flag cleared before the new engine is installed.

The rest of the IN findings (IN-01 to IN-09) were not touched, as instructed.

## Verification

Both gates ran in the main checkout, not in a worktree. The numbers are reproducible from this tree.

- `go test -race -count=50 -run TestSupervisorRebootExit ./internal/supervisor/`: **pass** (`ok ... 1.165s`).
- `make check`: **pass**, exit 0. It ran `go vet ./...`, `go vet -tags sil_e2e ./e2e/` and `go test -race -count=1 ./...`, and every package was `ok`. Its last line was `119 passed, 0 failed`.
- I also ran `go test -race -count=2 ./internal/supervisor/` after the IN-10 change, before committing: pass.

I did not try to prove that the old test flaked, because the reviewer could not reproduce it either. The fix follows from the order in `autoRestart` described in the review. The 50-run result shows no regression and is not evidence that the race is gone.

---

_Fixed: 2026-11-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_
