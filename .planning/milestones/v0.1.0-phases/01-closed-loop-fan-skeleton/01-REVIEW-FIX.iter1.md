---
phase: 01-closed-loop-fan-skeleton
fixed_at: 2026-11-09T05:00:00Z
review_path: .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-11-09
**Source review:** .planning/phases/01-closed-loop-fan-skeleton/01-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6 (fix_scope critical_warning; the 6 Info findings were out of scope)
- Fixed: 6
- Skipped: 0

**Verification:** run in the isolated review-fix worktree (`.claude/worktrees/rf-01-*`), not the main checkout; the commits were then fast-forwarded onto `claude/main`. `make check` (gofmt, vet, `go test -race ./...`, C++ protocol test) passed, and the full `make e2e` suite (34 tests, local pinned image) passed, 335 s.

## Fixed Issues

### WR-01: A failed `SimRestart` leaves the stopped engine in the slot and status never reports the failure

**Files modified:** `internal/supervisor/supervisor.go`
**Commits:** 870b93f, 86c9e1c
**Applied fix:** When `startEngine` fails during `SimRestart`, the supervisor now records the `engine_failed` error in `s.failure`, so `Status()` reports phase `failed` with the error. A later successful restart clears an `engine_failed` failure. The review also suggested counting `engine_restarts` only after success, but `TestSimGapAPIResponsive` requires the counter to be 1 during the gap. The follow-up commit 86c9e1c restores counting when the old engine is stopped.

### WR-02: Cancelling a build kills the `docker` client but not the container

**Files modified:** `internal/fwbuild/fwbuild.go`, `internal/fwbuild/fwbuild_test.go`
**Commit:** aafab04
**Applied fix:** The build container is named `sil-build-<sil_name>`. `cmd.Cancel` runs `docker rm -f <name>` and then kills the client. Each stage also removes a same-named orphan before it starts (for example one left by a SIGKILLed `sil up`), so an old compile cannot race the next build and the name cannot collide. This avoids `WaitDelay`, which the wall-clock lint bans. `TestDockerArgsSeparateInstances` now asserts the `--name` argument.

### WR-03: `probeCancel` is never called on shutdown and `probeWG` is never waited

**Files modified:** `internal/supervisor/supervisor.go`
**Commit:** 75147f4
**Applied fix:** `stopAll` cancels the current probe context after setting `stopping`, and waits on `probeWG` after the firmware and engine are stopped, before closing `stopped`.

### WR-04: The Go server accepts CRLF-terminated lines that the protocol and the C++ side reject

**Files modified:** `internal/bus/server.go`, `internal/bus/server_test.go`
**Commit:** 210ffab
**Applied fix:** The server's scanner uses a new `splitLF` split function that splits on `\n` only, so a trailing `\r` reaches `Decode` and is rejected (`bad_frame: trailing`). `TestBadFrameClosesTheSession` has a new CRLF row.
**Status:** fixed: requires human verification (framing logic). The CRLF test and the full e2e suite pass.

### WR-05: A compile failure in the firmware under test is reported as a bench infrastructure error

**Files modified:** `internal/fwbuild/fwbuild.go`, `internal/apperr/codes.go`
**Commit:** a192260
**Applied fix:** `compile()` returns the new registered code `compile_failed` (exit 1, carries `output_tail` and the log path) when the container exited with code 1 and the log shows no download failure. Docker-level failures, other exit codes (for example OOM kill) and download/toolcache failures stay `build_failed` (exit 2). There is no unit test for the new branch because it needs a full stubbed build pipeline, and no e2e test compiles a broken firmware, so only the registry test covers it.
**Status:** fixed: requires human verification (error classification).

### WR-06: `sil up` leaves an unmanaged supervisor if it dies between `startServe` and `run.json`

**Files modified:** `internal/supervisor/supervisor.go`, `internal/cli/cli.go`
**Commit:** 6dbff8e
**Applied fix:** The supervisor writes `run.json` at the start of `run()`, before the engine and firmware start. A deferred removal runs after the shutdown defer, so the file goes away only once the firmware group is stopped, on every exit path. Until the control socket exists, `sil down` falls back to signalling the PID (existing `forceStop`), and `checkNotRunning` sees the instance. `cli.Execute` also handles SIGHUP in its signal context so `sil up` takes the abort path when the terminal closes.
**Status:** fixed: requires human verification (lifecycle behaviour). The SIGKILL-between-startServe-and-ready window itself is not covered by a test.

---

_Fixed: 2026-11-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
