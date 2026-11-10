---
phase: 01-closed-loop-fan-skeleton
fixed_at: 2026-11-09T06:10:00Z
review_path: .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
iteration: 2
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-11-09T06:10:00Z
**Source review:** .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 3
- Fixed: 3
- Skipped: 0

IN-01 (SIGHUP handling defeats `nohup`) is Info and outside the `critical_warning` scope; it was not attempted.

## Fixed Issues

### WR-01: The fixed build-container name collides across users and run roots

**Files modified:** `internal/fwbuild/fwbuild.go`, `internal/fwbuild/fwbuild_test.go`
**Commit:** 73314f5
**Applied fix:** `containerName` is now `sil-build-<sil_name>-<first 4 bytes of sha256(CacheDir) in hex>`, so the orphan cleanup and the cancellation `docker rm -f` only ever touch a container owned by the same cache directory. `TestDockerArgsSeparateInstances` matches the computed name; new `TestContainerNameIsScopedToTheCacheDir` asserts that two cache dirs with the same instance name get different names and that the name is stable.

### WR-02: `compile_failed` is chosen by `ExitCode() == 1` alone

**Files modified:** `internal/fwbuild/fwbuild.go`, `internal/fwbuild/fwbuild_test.go`
**Commit:** 5fbe66f
**Applied fix:** The decision is factored into the pure function `classifyCompileFailure(exitCode int, log []byte) string`. It returns `compile_failed` only for exit 1, no download failure marker, and a positively identified compiler diagnostic (gcc/ld `file:line:col: error:`, `fatal error:`, `undefined reference to`, `collect2: error`, ninja `FAILED:`, make `*** [..] Error`); everything else is `build_failed`. The ninja `FAILED:` marker was added to the reviewer's regex because the 2026.10.1 host build uses ninja rather than make. New table test `TestClassifyCompileFailure` covers gcc, fatal, link, ninja and make logs, download and HTTP failures, `UnknownPackageError`, DNS, disk full, an empty log, and exits 125, 137 and -1.
**Note:** Logic classification; no e2e covers a real firmware compile error (none exists in the suite), so the regex is verified against representative logs only. Requires human verification against a real failing compile.
**Status:** fixed: requires human verification

### WR-03: A failed `SimRestart` overwrites an existing failure

**Files modified:** `internal/supervisor/supervisor.go`, `internal/supervisor/policy_test.go`
**Commit:** 1c8519a
**Applied fix:** In `SimRestart`, the failed-engine-start path now records `engine_failed` only when `s.failure == nil`. The existing clear-on-success (only for code `engine_failed`) then no longer erases a crash-loop diagnosis. New `TestSimRestartKeepsAnExistingFailure`: crash loop first, a `SimRestart` whose engine cannot start (`engine_failed` returned), then a recovering `SimRestart`; both times `Status().Error.Code` stays `firmware_crash_loop`. The test fails without the fix (verified by reverting `supervisor.go`).
**Status:** fixed: requires human verification

## Verification

Ran in the main checkout (no worktree; edits and commits on `claude/main`):
- `make check` passes (gofmt, vet incl. e2e tag, `go test -race ./...`, 112 lint checks).
- `make e2e E2E_RUN='TestBuildCache|TestSimRestartResync|TestBannedComponents'` passes (real docker builds with the new container names, sim restart resync).
- No stray `sil-build-*` containers remain.

---

_Fixed: 2026-11-09T06:10:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
