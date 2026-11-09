---
phase: 02-full-rev-3-3-bench
fixed_at: 2026-11-09T00:00:00Z
review_path: .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
iteration: 1
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-11-09
**Source review:** .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (WR-01..WR-05; IN-* out of scope)
- Fixed: 5
- Skipped: 0

**Verification:** `make check` (fmt-check, `go vet` incl. the `sil_e2e` tag, `go test -race -count=1 ./...`, `make test-cpp` with 119 vectors) ran green in the isolated worktree after the last fix, before it was fast-forwarded into `claude/main`. The `sil_e2e` suite and the docker-based firmware build were not run. Each new regression test was also confirmed to fail against the pre-fix code where it could compile against it (WR-01, WR-02). No new error codes were added, so `apperr.Registry` is unchanged.

## Fixed Issues

### WR-01: Build cache hash ignores a component directory that is a symlink (stale firmware reused)

**Files modified:** `internal/fwbuild/cache.go`, `internal/fwbuild/cache_test.go`
**Commit:** ce2c756
**Applied fix:** Replaced the `filepath.WalkDir` walk in `hashTree` with a `collectRels` walk over `os.ReadDir`/`os.Stat`. It follows symlinks for the root and for every sub-directory (WalkDir never descends into a link). A depth bound of 64 turns a symlink loop into a `build_failed` error rather than a hang. `__pycache__` and `.pyc` are still skipped. Regression test `TestInputHashFollowsSymlinkedDirs` covers a symlinked root, a symlinked sub-directory and a loop.

### WR-02: `Set` records the new inputs and fans before the engine has applied them

**Files modified:** `internal/supervisor/supervisor.go`, `internal/supervisor/supervisor_test.go`
**Commit:** e4f1046
**Applied fix:** This deviates from the review's suggestion (plain rollback on a context error). Rolling back there is unsafe: `Engine.Apply` can return `ctx.Err()` after the event is already queued, so the engine would then take the value while the record reverted. Instead:
- `Set` fails with the context error before recording anything if the caller is already gone.
- `Apply` runs with `context.WithoutCancel(ctx)` plus the shutdown signal (`withDown`), so a client disconnect cannot split the record from the engine.
- `ErrStopped` is accepted only when no `engine_failed` failure is held (the restart gap, where the record seeds the next engine). Under `engine_failed` the record is rolled back and `engine_failed` is returned.
- Any other `Apply` error rolls the record back.

Regression tests: `TestSetWithACancelledCallerRecordsNothing` and `TestSetOnAFailedEngineFailsAndRollsBack`. The existing "Set with the engine stopped is accepted" tests still pass.
**Status note:** logic change, so requires human verification.

### WR-03: Any docker failure other than 125..127 in the config/generate stage is reported as a user-fixable `config_invalid`

**Files modified:** `internal/fwbuild/fwbuild.go`, `internal/fwbuild/fwbuild_test.go`
**Commit:** 1469f38
**Applied fix:** `stageFailure` now reports `config_invalid` only for exit codes 1 and 2. Anything else (125..127, 137 OOM kill, -1 signal, other codes, a non-exit error) is `build_failed` infrastructure. This deviates from the review, which proposed "exactly 1". The pinned image's `esphome/__main__.py` returns 2 when `read_config` rejects the YAML and 1 for other `EsphomeError`s. Requiring 1 would have turned every validation error into `build_failed`, and the existing `TestBuildConfigFailureIsRedactedAndRepeatable` (stub exit 2) caught that. Regression test: `TestStageFailureClassification`.
**Status note:** logic change, so requires human verification.

### WR-04: Engine warnings are unbounded and are re-copied on every publish

**Files modified:** `internal/engine/engine.go`, `internal/engine/engine_test.go`
**Commit:** 291aebe
**Applied fix:** `warn` cuts each entry to 256 bytes on a rune boundary (plus "...") and stops at 64 entries. The 64th entry is "further warnings suppressed", so a reader can tell the list was truncated (a small addition to the review's sketch). Tests: `TestEngineWarningsAreBounded` (a flood of unknown channels, a multi-byte long name, duplicates at the cap) and `TestEngineWarnTruncatesLongEntries`.

### WR-05: `sil up --api-port N` persists N to identity.json before N is known to be usable

**Files modified:** `internal/instance/instance.go`, `internal/instance/instance_test.go`
**Commit:** febfe21
**Applied fix:** The bind test sits inside `LoadOrCreate`, after `AllocatePort` has done the range and `port_claimed` checks and before `identity.json` is written, instead of in `lifecycle.Up` before the call. The review's placement would test-bind out-of-range or claimed ports first and mask `invalid_argument` and `port_claimed`. It runs only for an explicit override (`apiPort != 0`) and only when the identity would change. A busy port returns `port_in_use` and writes nothing, for a fresh instance and for an existing one (its sticky port is kept). `TestLoadOrCreate` used a fixed port 7000, which would now depend on that port being free, so it uses a free port instead. New test: `TestLoadOrCreateBusyOverrideIsNotPersisted`.

## Skipped Issues

None.

## Notes

- The commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, as the orchestrator's task instruction specified. This agent is Claude Sonnet 5.5, so the trailer does not name the model that wrote the changes.
- Fixes ran in an isolated worktree (`workflow.use_worktrees` unset, so the default applies). The branch was fast-forwarded into `claude/main` and the worktree, temp branch and recovery sentinel were removed. Verification therefore ran in the worktree, not the main checkout. The main checkout's uncommitted `.planning/config.json`, `.gsd/`, `.planning/milestone.lock` and `.planning/state.json` were not touched.
- `tests/silbench.py:Bench.up` still carries its `port_in_use` retry workaround for WR-05. It is now redundant for this cause but harmless, and the file is outside the finding's scope.

---

_Fixed: 2026-11-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
