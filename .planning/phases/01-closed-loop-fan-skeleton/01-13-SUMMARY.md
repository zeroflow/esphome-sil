---
phase: 01-closed-loop-fan-skeleton
plan: 13
subsystem: testing
tags: [go, apperr, exit-codes, go-ast, go-modules, e2e, acceptance]

requires:
  - phase: 01-closed-loop-fan-skeleton (plans 01-12)
    provides: "apperr.Failed/Infra call sites in every package, the pin file, the whole e2e suite"
provides:
  - "apperr.Registry: every Phase 1 error code with its exit class (1 or 2)"
  - "AST consistency test that fails make check when a call site drifts from the registry"
  - "internal/deps/pin.go removed; go.mod/go.sum unchanged and verified"
  - "Phase 1 acceptance run (make check, test-cpp-image, full e2e) green, criterion-to-test map"
affects: [phase-2, product-repo-pytest, agents-parsing-sil-json]

actuals:
  tokens: 2540
  tasks: 2
  commits: 2
plan_head_before: b99635cc80ac07b2d85e5b702f4ff975371d7346
plan_head_after: 2d59536fb43080dc523abac9e206ce05c95518d6

tech-stack:
  added: []
  patterns:
    - "Registry-plus-AST-scan: a table in code and a go/parser test over production sources keep a stable external contract from drifting"

key-files:
  created:
    - internal/apperr/codes.go
    - internal/apperr/codes_test.go
  modified:
    - internal/supervisor/supervisor.go
  deleted:
    - internal/deps/pin.go

key-decisions:
  - "The registry lists the 14 Failed and 37 Infra codes the scan actually finds (51 total); plan-listed io_error is dropped (never used), and 21 literals the plan list lacked are added rather than renamed"
  - "The scan covers production .go files only (_test.go and testdata skipped); test fixtures such as bad_state and resync_failed are not part of the contract"
  - "Codes must be string literals at the call site; the one dynamic site (supervisor probe errors) was rewritten as a switch with two literal calls"

patterns-established:
  - "Adding, removing or reclassifying an error code fails make check until apperr.Registry is updated"

requirements-completed: [CLI-05]

coverage:
  - id: D1
    description: "Every error code has exactly one registered exit class; call-site drift (unregistered, reclassified, dual-class, non-literal, stale) fails the build"
    requirement: CLI-05
    verification:
      - kind: unit
        ref: "internal/apperr/codes_test.go#TestRegistryMatchesCallSites"
        status: pass
      - kind: unit
        ref: "internal/apperr/codes_test.go#TestRegistryClasses"
        status: pass
      - kind: unit
        ref: "internal/apperr/codes_test.go#TestExitCodeFollowsRegistry"
        status: pass
    human_judgment: false
  - id: D2
    description: "Dependency pin file removed; go mod tidy leaves the five Phase 1 modules; go mod verify clean"
    verification:
      - kind: other
        ref: "make mod-verify (prints: all modules verified)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Full Phase 1 acceptance run from a cleaned build cache is green: make check, make test-cpp-image, make e2e (34 tests, no filter)"
    verification:
      - kind: e2e
        ref: "make e2e (ok github.com/zeroflow/esphome-sil/e2e 276.530s)"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 13: Exit-class registry and Phase 1 acceptance Summary

**A central `apperr.Registry` (51 codes, exit 1 or 2) enforced by a go/parser scan of every `apperr.Failed/Infra` call site, the dependency pin file removed with go.mod unchanged by `go mod tidy`, and the whole Phase 1 bench (check, C++ image test, 34 e2e tests) green from a cleaned cache.**

## Performance

- **Duration:** about 15 min (the full e2e run alone was 276 s)
- **Completed:** 2026-11-09T03:28Z
- **Tasks:** 2
- **Files modified:** 4 (2 created, 1 modified, 1 deleted)

## Accomplishments

- `internal/apperr/codes.go`: `Registry map[string]int` with 14 exit-1 and 37 exit-2 codes.
- `internal/apperr/codes_test.go`: parses `internal/` and `cmd/` (skipping `testdata` and `_test.go`), resolves the local import name of apperr (and bare calls inside the package), and fails on: a code missing from the registry, a class mismatch, a code used with both constructors, a non-literal code (reported with `file:line:col`), and any registry entry no call site uses. Verified to fail on a planted drift file (unregistered code, wrong class, dual-class, non-literal) and removed afterwards.
- `TestExitCodeFollowsRegistry` checks `ExitCode` maps nil to 0, each registered code to its class, and foreign errors to 2 (`AsError` -> `internal`, which the registry lists as exit 2).
- `internal/deps/pin.go` deleted (the directory is gone); `go mod tidy` produced no change to go.mod or go.sum: kong v1.17.0, go-toml/v2 v2.4.3, go.yaml.in/yaml/v3 v3.0.6 and flynn/noise v1.1.0 are direct, x/crypto v0.58.0 stays indirect. `make mod-verify`: `all modules verified`.
- Acceptance run after `go clean -cache -testcache`: `make check` (vet, race tests, host C++ test: 112 passed, 0 failed), `make test-cpp-image` (g++ 14.2.0 of the pinned image: 112 passed, 0 failed), `make e2e` with no filter: `ok github.com/zeroflow/esphome-sil/e2e 276.530s`, zero `--- FAIL`, zero `SKIP`. `TestRestartEitherOrder` (flaked once in the 01-12 full run) passed in this run (7.47 s), so no change was needed; it stays on watch.

## ROADMAP Phase 1 success criteria to proving tests

| # | Criterion | Proving tests (all PASS in this run) |
|---|-----------|--------------------------------------|
| 1 | `sil up` builds statically in the pinned image, runs on the host, ready only after a real encrypted handshake; firmware keeps running with no API client; no root-owned files | `TestTracer` (12.43 s), `TestFirmwareBus` (15.43 s), `TestStaticAndClean` (4.38 s), `TestBuildCache` (13.10 s), `TestOfflineSeeded` (4.02 s), `TestReadyIsEncrypted` incl. `idle_hold` (16.82 s / 10.01 s), `TestHubSession` (13.55 s) |
| 2 | Fan speed through the API shows in `sil status`; RPM (1 s mean) rises with first-order lag, 0 below the start threshold; model only through the injectable clock; lint on wall-clock calls | `TestTracer/four_ports` (3.50 s), `TestLoop` (8.49 s), `TestModelThreshold` (18.15 s), `TestRPMMean` (11.83 s); Go: `TestNoWallClock`, `TestLintSelfTest` (internal/lint), engine tests `TestEngineAdvancesModelOnlyOnClockTicks`, `TestEngineDutyLastValueWins` and manual-clock tests (internal/engine, internal/clock) |
| 3 | Restart of simulator or firmware, either order, re-syncs; firmware API responsive during sim gap; identical golden vectors in Go and C++ | `TestSimRestartResync` (7.25 s), `TestSimGapAPIResponsive` (7.21 s), `TestFwRestartResync` (8.93 s), `TestRestartEitherOrder` (7.47 s), `TestStatusDuringRestart` (7.24 s), `TestFakePeer` (23.61 s); vectors: Go `TestVectors` (internal/bus) and C++ `tests/cpp/test_protocol.cpp` via `make test-cpp` and `make test-cpp-image` |
| 4 | Two named instances isolated; `sil up` on a taken port exits 2 with JSON; `sil down` stops both; killing the simulator leaves no orphan firmware; fresh prefs per run | `TestTwoInstances` (10.54 s), `TestPrefixNames` (8.09 s), `TestPortInUse` (0.02 s), `TestPortClaimed` (4.00 s), `TestFreshPrefs` (5.37 s), `TestAlreadyRunningAndBusy` (3.96 s), `TestSupervisorKillNoOrphan` (5.34 s), `TestSupervisorSIGTERM` (3.84 s), `TestDownFallback` (13.80 s), `TestFirmwareCrashAutoRestart` (5.60 s) |
| 5 | Misconfigurations fail loudly before anything runs (banned components, sim platform without `channel:`, over-long socket path, unknown `fan.toml` key) | `TestBannedComponents` (5.16 s), `TestCodegenValidation` (8.70 s), `TestGuardsCompile` (7.76 s), `TestFanTomlUnknownKey` (0.00 s), `TestNamesAndPaths` (0.02 s); Go: `TestLintConfig*` (internal/fwbuild) |

CLI contract (CLI-05/06): `TestCLIContract` (11.72 s), `TestCLIErrors` (0.01 s), `TestDownNotRunning` (0.00 s) and the new registry tests.

Per-test durations of the remaining e2e tests are listed above; the whole package took 276.530 s wall time (tests run sequentially).

## Task Commits

1. **Task 1: Central exit-class registry with an AST consistency test** - `94c63db` (feat)
2. **Task 2: Remove the dependency pin file, tidy and verify modules** - `2d59536` (chore); the acceptance run itself changes no files and is recorded here

**Plan metadata:** committed with this SUMMARY (docs: complete plan)

## Files Created/Modified

- `internal/apperr/codes.go` - the registry, the single list of codes and classes
- `internal/apperr/codes_test.go` - AST scan, class sanity and ExitCode tests
- `internal/supervisor/supervisor.go` - probe errors use literal codes instead of `pe.Kind`
- `internal/deps/pin.go` - deleted

## Decisions Made

- Registry reflects what the code base really emits (51 codes): the plan's seed list was a starting point, and the plan itself says to add scanned literals and drop unseen ones. `io_error` was dropped. Added: `engine_failed`, `firmware_start_failed`, `firmware_not_running`, `stopping`, `lock_failed`, `lock_timeout`, `no_home`, `identity_failed`, `run_file_failed`, `run_file_invalid`, `run_file_unreadable`, `spec_invalid`, `spec_unreadable`, `run_dir_failed`, `serve_start_failed`, `serve_exited`, `control_listen_failed`, `control_error`, `runtime_dir_unreadable`, `port_bind_failed`, `supervisor_panic`.
- Consumer note: `plaintext_api` and `handshake_rejected` remain exit 2 with the same message and detail as before.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Non-literal error code at one call site**
- **Found during:** Task 1
- **Issue:** `supervisor.go` called `apperr.Infra(pe.Kind, ...)` with a variable code, which the registry scan (correctly, per the plan's "a non-literal code fails the test with its call site") rejects.
- **Fix:** Replaced by a `switch pe.Kind` with two literal calls (`plaintext_api`, `handshake_rejected`); behavior is unchanged because the previous guard admitted exactly those two kinds.
- **Files modified:** internal/supervisor/supervisor.go
- **Verification:** `make check` and the full e2e run (including `TestReadyIsEncrypted`) pass.
- **Committed in:** 94c63db

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Needed for the registry contract; no scope creep.

## Issues Encountered

None. `go mod tidy` made no change, so no import was lost. The task's TDD red step was not committed separately (the test cannot compile without `Registry`); the test was written first and its drift detection was proven against a temporary planted file.

## Needs Human

None. Nothing in the plan required a human; the HA-facing criteria belong to Phase 2.

## Known Stubs

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Phase 1 (Closed-Loop Fan Skeleton) is complete: all five ROADMAP success criteria are covered by passing tests and the exit-code contract is frozen in `apperr.Registry`. Ready for Phase 2 (verification of Phase 1 can run via `/gsd-verify-work`). `TestRestartEitherOrder` flaked once in 01-12 but passed here; keep an eye on it.

## Self-Check: PASSED

- internal/apperr/codes.go and codes_test.go exist; internal/deps is gone
- commits 94c63db and 2d59536 exist on claude/main
- `make mod-verify`, `make check`, `make test-cpp-image`, `make e2e` all green

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
