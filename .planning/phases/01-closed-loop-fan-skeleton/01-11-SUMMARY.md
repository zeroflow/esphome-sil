---
phase: 01-closed-loop-fan-skeleton
plan: 11
subsystem: supervisor-lifecycle
tags: [go, supervisor, restart-policy, crash-loop, pdeathsig, sil-down, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "supervisor spawn code (LockOSThread, Setpgid, Pdeathsig) and the single stopAll path"
  - phase: 01-closed-loop-fan-skeleton (plan 09)
    provides: "readiness probe loop, instance.ProcessAlive, per-instance lock"
  - phase: 01-closed-loop-fan-skeleton (plan 10)
    provides: "fwProc handle, opMu serialization, FwRestart, the main loop's recordExit path"
provides:
  - "supervisor.Starter/Process/ExitInfo/StartSpec: the firmware process behind an interface; ExecStarter spawns from a LockOSThread goroutine with Setpgid and Pdeathsig SIGKILL"
  - "supervisor.Classify (requested|rebooted|crashed) and CrashGuard (5 unrequested exits in 30 s on the injected clock)"
  - "D-03 auto-restart: an unrequested firmware exit records last_exit, increments firmware.restarts, restarts the same program/prefs/env and re-probes readiness; the 5th exit in 30 s fails the instance with firmware_crash_loop (exit class 1)"
  - "Every Serve exit path (down, SIGTERM/SIGINT/SIGHUP, engine failure, loop panic) stops the firmware process group before run.json and the sockets are removed"
  - "lifecycle.Down fallback: SIGTERM then SIGKILL of the run.json pid, only after /proc/<pid>/cmdline confirms `sil serve <name>`; a stale run.json is cleaned up"
  - "instance.LoadRunFile (raw) and a liveness-checked instance.ReadRunFile: a dead supervisor's run.json reads as not_running"
affects: [01-12, 01-13]

actuals:
  tokens: 11100
  tasks: 2
  commits: 2
plan_head_before: ee1671265dac62ee3aee03a29dd1c73114c2d3e6
plan_head_after: 58307c5784b19d42d2a694b2a629e403882e34a1

tech-stack:
  added: []
  patterns:
    - "Process lifecycle behind a Starter/Process interface so restart policy is unit-tested with a fake process and clock.Manual; the real starter is the only code that touches os/exec"
    - "Exit handling split: recordExit (under mu, decides) and autoRestart (under opMu, acts) so a restart never races FwRestart or shutdown and the recorded channel always closes before the restart waits on opMu"
    - "Down never trusts a run.json pid alone: every signal is preceded by a /proc cmdline check, and a stale run.json is treated as not running"

key-files:
  created:
    - internal/supervisor/firmware.go
    - internal/supervisor/policy.go
    - internal/supervisor/policy_test.go
    - e2e/supervise_test.go
  modified:
    - internal/supervisor/supervisor.go
    - internal/lifecycle/lifecycle.go
    - internal/instance/instance.go

key-decisions:
  - "Unrequested exits that race a FwRestart are decided under mu by fwProc.requested; the restart itself is a goroutine that takes opMu and gives up if the exited process is no longer the current one or the instance is stopping, so there can never be two firmware processes"
  - "restarts is incremented when a process is actually started (auto or requested), not when an exit is observed, so a crash coinciding with a requested restart is counted once"
  - "CrashGuard counts an exit as inside the window while now-t < 30 s: exits exactly 30 s apart do not accumulate"
  - "sil down fallback sends SIGTERM, waits 5 s, then SIGKILL (no SIGCONT), so a SIGSTOPped supervisor is killed and Pdeathsig takes the firmware with it; total about 10 s after the 5 s control-socket wait"
  - "last_exit.signal uses POSIX names (SIGKILL) from an explicit table, because syscall.Signal.String() returns prose such as killed"
  - "FwRestart does not reset the crash guard; a later crash within the window still counts toward the limit"

patterns-established:
  - "supervisor tests build a *supervisor around a fake Starter and clock.Manual and drive mainLoop directly; no engine, control socket or real probe (the probe is an injectable field)"

requirements-completed: [BUILD-07]

coverage:
  - id: D1
    description: "Unrequested firmware exit restarts the same binary with the same prefs/env, increments restarts, records last_exit (rebooted for code 0, crashed otherwise) and re-probes readiness"
    requirement: BUILD-07
    verification:
      - kind: unit
        ref: "internal/supervisor/policy_test.go#TestSupervisorRebootExit"
        status: pass
      - kind: e2e
        ref: "e2e/supervise_test.go#TestFirmwareCrashAutoRestart"
        status: pass
    human_judgment: false
  - id: D2
    description: "Crash-loop guard: the 5th unrequested exit within 30 s stops restarting and fails with firmware_crash_loop (exit 1); spread-out exits and requested exits never trip it"
    requirement: BUILD-07
    verification:
      - kind: unit
        ref: "internal/supervisor/policy_test.go#TestCrashGuard, TestSupervisorCrashLoop, TestSupervisorRequestedExitsAreNotCounted"
        status: pass
    human_judgment: false
  - id: D3
    description: "No orphaned firmware: SIGKILL of the supervisor kills the firmware within 2 s, status exits 1 not_running and the same name can be brought up again; SIGTERM stops the group and removes run.json and sockets"
    requirement: BUILD-07
    verification:
      - kind: e2e
        ref: "e2e/supervise_test.go#TestSupervisorKillNoOrphan, TestSupervisorSIGTERM"
        status: pass
      - kind: unit
        ref: "internal/supervisor/policy_test.go#TestSupervisorShutdownEscalates, TestSupervisorShutdownPolite"
        status: pass
    human_judgment: false
  - id: D4
    description: "sil down falls back to cmdline-checked SIGTERM/SIGKILL when the control socket does not answer, and reports stopped false for an instance that is not running"
    requirement: BUILD-07
    verification:
      - kind: e2e
        ref: "e2e/supervise_test.go#TestDownFallback, TestDownNotRunning"
        status: pass
    human_judgment: false

duration: 13 min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 11: Supervised Firmware Restart and Orphan-Proof Shutdown Summary

**The firmware now reboots like a device (same prefs and env, readiness re-probed, a 5-exits-in-30-s crash-loop guard on an injected clock), never outlives its supervisor on any exit path, and `sil down` falls back to cmdline-checked SIGTERM/SIGKILL when the supervisor no longer answers.**

## Performance

- **Duration:** 13 min (start time estimated from the first file written; not recorded at launch)
- **Started:** 2026-11-09T02:47Z (approx.)
- **Completed:** 2026-11-09T03:01Z
- **Tasks:** 2
- **Files modified:** 7 (4 created, 3 modified)

## Accomplishments

- Firmware process behind `Starter`/`Process`: `ExecStarter` launches from a goroutine that stays `LockOSThread`ed until the child is reaped, with `Setpgid` and `Pdeathsig SIGKILL`; each launch writes a `=== firmware start N ===` separator into firmware.log.
- D-03 restart policy: `Classify` plus `CrashGuard` decide every unrequested exit; restarts reuse one `StartSpec` and re-run the readiness probe; the instance goes `failed` with `firmware_crash_loop` (detail: last_exit, window_s 30, limit 5) instead of restarting a fifth time. `sil up` reports it with exit 1.
- `Serve` always stops the firmware group (SIGTERM, SIGKILL after 3 s on the clock) before removing run.json and the sockets, for down, signals, an engine that dies on its own, and a panic in the main loop.
- `lifecycle.Down`: 5 s on the control socket, then SIGTERM (5 s) and SIGKILL of the run.json pid, each preceded by the `/proc/<pid>/cmdline` check; a dead or foreign pid is cleaned up and reported `stopped:false`.
- Kill matrix verified end to end: firmware SIGKILL gives a new pid within 10 s with `last_exit {crashed, SIGKILL}`; supervisor SIGKILL leaves no firmware, `sil status` exits 1 `not_running`, and `sil up` of the same name works again.

## Task Commits

1. **Task 1: Firmware runner behind Starter, restart classification and crash-loop guard** - `29505e1` (feat)
2. **Task 2: `sil down` fallback and the kill-matrix e2e** - `58307c5` (feat)

**Plan metadata:** recorded in the docs commit that carries this file.

## Files Created/Modified

- `internal/supervisor/firmware.go` - Starter/Process/ExitInfo/StartSpec, ExecStarter, POSIX signal names
- `internal/supervisor/policy.go` - Classify, CrashGuard (window 30 s, limit 5)
- `internal/supervisor/policy_test.go` - Manual-clock tests of Classify, CrashGuard, the restart loop, requested exits, shutdown escalation, ExecStarter
- `internal/supervisor/supervisor.go` - restart loop (recordExit/autoRestart), cycleFirmware shared with FwRestart, mainLoop with panic recovery and engine-death watcher, one shutdown before cleanup
- `internal/lifecycle/lifecycle.go` - Down fallback, forceStop, cleanupRun, exit class 1 for firmware_crash_loop in `sil up`
- `internal/instance/instance.go` - `LoadRunFile` (raw) and a liveness-checked `ReadRunFile`
- `e2e/supervise_test.go` - TestFirmwareCrashAutoRestart, TestSupervisorKillNoOrphan, TestSupervisorSIGTERM, TestDownFallback, TestDownNotRunning

## Decisions Made

See `key-decisions` above. The main ones: the restart is its own goroutine under `opMu` so it cannot race `FwRestart`; `restarts` counts started processes; the Down fallback deliberately omits SIGCONT so it exercises the SIGKILL path that a truly wedged supervisor needs.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `instance.ReadRunFile` now checks liveness**
- **Found during:** Task 2 (kill-matrix e2e)
- **Issue:** The plan requires `sil status` to exit 1 `not_running` after a supervisor SIGKILL, but status goes through `internal/cli` (owned by 01-12, not editable here) and `ReadRunFile`, which returned the stale run.json and ended in `control_unreachable` (exit 2).
- **Fix:** `ReadRunFile` returns `not_running` when the run.json pid is dead or not `sil serve <name>`; the raw read moved to `LoadRunFile`, which `Down` uses to find and clean up a stale run.json. All CLI callers get the fix without edits to `internal/cli`.
- **Files modified:** internal/instance/instance.go (not in the plan's file list)
- **Verification:** TestSupervisorKillNoOrphan, TestSupervisorSIGTERM (status exit 1, `error.code` not_running)
- **Committed in:** 58307c5

**2. [Rule 1 - Bug] `last_exit.signal` was prose ("killed") instead of `SIGKILL`**
- **Found during:** Task 2 (first e2e run)
- **Issue:** `syscall.Signal.String()` returns "killed"; the plan and status schema want POSIX names.
- **Fix:** `SignalName` with an explicit table (fallback `SIG<n>`), used by `ExecStarter`; unit test added.
- **Files modified:** internal/supervisor/firmware.go, internal/supervisor/policy_test.go
- **Verification:** TestSignalName, TestFirmwareCrashAutoRestart
- **Committed in:** 58307c5

**3. [Rule 2 - Missing Critical] `firmware_crash_loop` exit class survives the control API**
- **Found during:** Task 1 (reviewing how the error reaches `sil up`)
- **Issue:** The error class (`apperr.Error.Exit`) is not serialized; the control client defaults a missing class to infrastructure (2), contradicting the required exit 1.
- **Fix:** `lifecycle.userClass` restores exit class 1 for `firmware_crash_loop` in `waitReady`; `control.go` (not in scope) is untouched. `sil status` still prints the failed status with exit 0; any change there belongs to 01-12.
- **Files modified:** internal/lifecycle/lifecycle.go
- **Committed in:** 58307c5

---

**Total deviations:** 3 auto-fixed (1 blocking, 1 bug, 1 missing critical)
**Impact on plan:** All three were needed to meet the plan's own truths; no scope creep beyond one extra file (instance.go). `StartSpec` carries two fields beyond the interface sketch (`Dir`, `Start`) for the working directory and the log separator.

## Issues Encountered

- First unit run hung: a fake process in the reboot test did not react to SIGTERM, so the cleanup shutdown waited for the Manual clock. Fixed in the test fixture (later fakes are polite), not in production code.

## Known Stubs

None.

## Needs Human

None. (Edge review flagged in the plan for `EDGE-BUILD-07-unclassified`, e.g. a supervisor exiting normally with a wedged firmware or PID reuse, is covered by Pdeathsig, the shutdown stop, and the cmdline-gated fallback; a maintainer may still want to read `lifecycle.forceStop`.)

## Next Phase Readiness

- 01-12 (CLI contract) can rely on `ReadRunFile` returning `not_running` for stale run files and on `firmware_crash_loop` appearing in `status.error`; whether `sil status` should exit non-zero for `phase: failed` is its call.
- 01-13 (registry and final acceptance): criterion 4 lifecycle half is proven by the kill-matrix e2e.
- Caveat: the crash guard is not reset by `sil fw restart`; revisit if a manual recovery after a crash loop should start with a clean window.

## Self-Check: PASSED

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
