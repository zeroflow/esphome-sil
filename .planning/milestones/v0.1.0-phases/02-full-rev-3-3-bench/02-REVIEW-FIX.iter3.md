---
phase: 02-full-rev-3-3-bench
fixed_at: 2026-11-09T00:00:00Z
review_path: .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
iteration: 2
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-11-09
**Source review:** .planning/phases/02-full-rev-3-3-bench/02-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 2
- Fixed: 2
- Skipped: 0

The iteration-1 report is kept as `02-REVIEW-FIX.iter2.md`. IN-01 to IN-08 are out of scope (`fix_scope: critical_warning`).

## Fixed Issues

### WR-01: e2e `TestPortInUse` still asserts the pre-WR-05 sticky busy port and now fails

**Files modified:** `e2e/isolation_test.go`
**Commit:** 58d3e7e
**Applied fix:** The test now asserts the WR-05 contract. A busy explicit `--api-port` is refused with exit 2 `port_in_use` (port, holder pid and message still checked) and no `identity.json` exists afterwards, so nothing pins the busy port. The old second step (plain `up` expecting `port_in_use`) is gone, because it would have built and started firmware. The sticky-busy-port coverage is kept differently. The test writes a valid `identity.json` on a free port, holds that port, runs a plain `up e2e-iso-c`, and expects `port_in_use` with `detail.port` equal to the stored port. Seeding the file directly avoids a firmware build. `run.json` is asserted absent in both cases.

**Run result:** `make e2e E2E_RUN='^TestPortInUse$'` (that is `go test -tags sil_e2e -count=1 -timeout 30m -v -run '^TestPortInUse$' ./e2e/`) gave `--- PASS: TestPortInUse (0.03s)`, `ok github.com/zeroflow/esphome-sil/e2e 0.788s`. It ran in the main checkout. Only this test was run, not the whole `sil_e2e` suite.

### WR-02: `Set` treats `ErrStopped` as a restart gap whenever the failure is not `engine_failed`

**Files modified:** `internal/supervisor/supervisor.go`, `internal/supervisor/supervisor_test.go`
**Commit:** da1a9ca
**Status note:** fixed, requires human verification (concurrency and state logic; unit tests cover the paths, but the interleaving is reasoned, not exercised under a real restart).
**Applied fix:** Added a real state flag, `engGap`, guarded by `mu`, as the review suggested.
- `SimRestart` sets it together with `engRestarts++`, before `stopEngine(old)`.
- It is cleared when the new engine is installed, when `startEngine` fails, and on the shutdown-abort paths (the gap sleep and the `stopping` check).
- `Set` now accepts `ErrStopped` only while `engGap` is set. Otherwise it rolls back and returns `engine_failed`, whatever failure the instance holds. If the instance is stopping it returns `stopping` instead, so a shutdown abort does not read as an engine fault.
- This removes the dependence on `s.failure.Code`.
- Ordering holds. `Set` holds `inMu` across `Apply`, and the install in `SimRestart` also needs `inMu`, so a `Set` that sees the old engine's `ErrStopped` always sees `engGap` still set.

**Tests:**
- New `TestSetAfterAFailedRestartWithAHeldFailureFails` drives the real double fault. It holds `firmware_crash_loop`, makes the new engine fail to start through a bad bus path, and calls `SimRestart`. It checks that the held failure is kept and `engGap` is false, then that `Set` returns `engine_failed` and the record is rolled back.
- New `TestSetInARestartGapIsKept` checks that a `Set` with `engGap` set and the engine stopped is applied and recorded.
- `TestSetFanSeedsTheNextEngine` and `TestSetSeedsTheNextEngine` used to simulate a gap by only stopping the engine. They now set `engGap` as `SimRestart` does.

## Verification

- `make check` (fmt-check, vet incl. `-tags sil_e2e`, `go test -race -count=1 ./...`, test-cpp) is green on the final tree, run in the main checkout `/home/claude/esphome-sil`.
- The first `make check` run failed once in `TestSupervisorRebootExit` (`policy_test.go:336: 2 launches and 1 probes, want 2 and 2`). The same test passed 30 times in isolation (`-race -count=30`) on the fixed tree and 30 times on the pre-fix `febfe21` supervisor. The whole supervisor package passed 15 times with `-race -count=15`, and the rerun of `make check` passed. It looks like a timing flake in the probe-counting test under full parallel load. It is not related to these changes, but it exists and is worth a look.

## Skipped Issues

None.

---

_Fixed: 2026-11-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
