---
phase: 01-closed-loop-fan-skeleton
plan: 04
subsystem: simulator-core
tags: [go, clock, deterministic-time, engine, testing, race-detector]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 02)
    provides: "internal/clock interface + WithTimeout/TimedOut, single-writer engine, bus codec, model.Step, fanlib"
provides:
  - "clock.Manual / clock.NewManual: deterministic test clock (Advance, BlockUntil, zero Deadline)"
  - "Tests proving clock.WithTimeout is driven by the injected clock (timeout, cancel and parent-cancel paths)"
  - "Engine tests: determinism against 100 x model.Step, last-duty-wins ordering, race-free Snapshot, sim-microsecond t on in frames"
affects: [01-05, 01-06, 01-10]

actuals:
  tokens: 6100
  tasks: 2
  commits: 3
plan_head_before: 0e66bf01c7386441fd2832b3e997b293c593d7c7
plan_head_after: fb885a70f4bf318277f07419f66dc018454cd4e5

tech-stack:
  added: []
  patterns:
    - "Manual clock: waiters kept in (due, seq) order; Advance sets Now to each due time, delivers with blocking unbuffered sends, tickers re-arm before delivery so Stop mid-delivery is safe"
    - "Tests wait only on real-clock guards (clock.NewReal timers), never on the engine clock, and use a FIFO marker frame as an inbox barrier"
    - "Fake firmware peer = unix socket client recording decoded frames, socket under os.MkdirTemp(/tmp, sil-)"

key-files:
  created:
    - internal/clock/manual.go
    - internal/clock/manual_test.go
    - internal/engine/engine_test.go
  modified: []

key-decisions:
  - "Manual timer/ticker deliveries are blocking sends on unbuffered channels (as planned), so Advance returns only after receivers took their values; Timer.Stop returns false exactly when Advance already took the timer"
  - "A stopped ticker delivers nothing even if Advance had it queued (stopped is checked before the send)"
  - "Manual.Sleep with d <= 0 returns nil immediately; a Sleep whose waiter Advance already took reports success even if ctx ended at the same moment"
  - "Frame timestamp test ties t to the model: the rpm in the frame stamped T must equal float32 of the reference rpm after T/10 ms steps, which pins sim time and value together"

patterns-established:
  - "Engine tests run on clock.Manual with BlockUntil(1) after Ready, then Advance(1 s) and poll Snapshot().SimTime"
  - "Mutation check on the engine (first-wins duty, millis instead of micros, wrong dt) makes the new tests fail; engine.go restored afterwards"

requirements-completed: [MODEL-04, MODEL-01, IO-01]

coverage:
  - id: D1
    description: "clock.Manual: Now starts at 0 and moves only on Advance; 10 ms ticker fires exactly 100 times in order with values equal to due times; timers fire once, Stop before due returns true; Sleep honours Advance and ctx; BlockUntil counts tickers, timers and sleepers; Deadline is the zero time"
    requirement: "MODEL-04"
    verification:
      - kind: unit
        ref: "internal/clock/manual_test.go#TestManualTickerFiresExactly100TimesInOrder (and the other TestManual* tests)"
        status: pass
    human_judgment: false
  - id: D2
    description: "clock.WithTimeout is driven by the injected clock: live after 1999 ms of a 2 s timeout, done with TimedOut true after one more ms; cancel or parent cancel first leaves TimedOut false and a later Advance(5 s) returns"
    requirement: "MODEL-04"
    verification:
      - kind: unit
        ref: "internal/clock/manual_test.go#TestManualWithTimeoutIsDrivenByTheInjectedClock, ...CancelBeforeDue..., ...ParentCancelledBeforeDue..."
        status: pass
    human_judgment: false
  - id: D3
    description: "Engine advances the model only on clock ticks: after Advance(1 s) rpm equals 100 x model.Step at duty 0.4 within 1e-9 relative, SimTime == 1 s, and the model does not move while real time passes"
    requirement: "MODEL-04"
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineAdvancesModelOnlyOnClockTicks"
        status: pass
    human_judgment: false
  - id: D4
    description: "Duty frames apply in arrival order and the decimal-normalized commanded value is exact (0.2 then 0.4 gives 0.4; 0.4 then 0.2 gives 0.2)"
    requirement: "IO-01"
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineDutyLastValueWins"
        status: pass
    human_judgment: false
  - id: D5
    description: "Snapshot reads are race-free against the engine goroutine, welcome carries proto 1, and every port1.rpm in frame carries an integer t equal to sim microseconds"
    requirement: "MODEL-01"
    verification:
      - kind: unit
        ref: "go test -race ./internal/engine/ (TestEngineWelcomesHelloWithProto1, TestEngineAdvancesModelOnlyOnClockTicks with two concurrent readers)"
        status: pass
    human_judgment: false

duration: 3min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 04: Manual Clock and Engine Determinism Summary

**A deterministic clock.Manual (ordered tickers, timers and sleepers driven only by Advance, blocking delivery, BlockUntil for test sync) plus Manual-clock engine tests that pin the model to 100 exact model.Step compositions, last-duty-wins ordering, race-free snapshots and sim-microsecond frame timestamps.**

## Performance

- **Duration:** about 3 min
- **Started:** 2026-11-09T01:07:38Z
- **Completed:** 2026-11-09T01:10:48Z
- **Tasks:** 2 (both TDD-marked)
- **Files:** 3 created, 0 modified
- **Commits:** 3 (RED test, clock GREEN, engine test); `plan_head_before` and `plan_head_after` in the frontmatter

## Accomplishments

- `clock.Manual` implements `clock.Clock` without reading the wall clock. Waiters sit in a (due, seq) ordered list; `Advance` sets `Now` to each due time before delivering, so a receiver reading `Now` on a tick sees exactly the due time. Tickers re-arm before delivery, so `Stop` during an in-flight delivery releases `Advance` instead of deadlocking.
- `clock.WithTimeout` is now proven to be clock-driven: the 2 s timeout survives `Advance(1999 ms)`, fires after one more millisecond with `TimedOut` true, and the cancel and parent-cancel paths leave `TimedOut` false and never leave `Advance` blocked on an undrained delivery.
- Engine determinism: with Manual time, `Advance(1 s)` in 100 ticks gives port 1 the same rpm as 100 compositions of `model.Step` (checked within 1e-9 relative), `SimTime` is exactly 1 s, and the model does not move while real time passes without ticks.
- Every `port1.rpm` in frame has an integer `t` that is a tick time, strictly increasing, and the rpm it carries equals float32 of the reference rpm at that sim time, so timestamp and value are verified together.
- Engine ordering and concurrency: last duty frame wins in both directions, decimal-exact (`0.4`, not `0.4000000059604645`), with a FIFO marker frame as a barrier; two concurrent Snapshot readers run during `Advance` under `-race` without a report.
- Test sensitivity was checked by temporary mutations of engine.go (first-wins duty, milliseconds instead of microseconds for `t`, dt + 1 ms): each made at least one new test fail. engine.go was restored byte-identical afterwards.

## Task Commits

1. **Task 1: Deterministic Manual clock (TDD)**
   - RED: `01c6d24` (test) - manual_test.go only, failed to compile (`undefined: Manual`)
   - GREEN: `af0519a` (feat) - manual.go
2. **Task 2: Engine determinism, ordering, race, timestamps (TDD)** - `fb885a7` (test)

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `internal/clock/manual.go` - Manual clock (NewManual, Advance, BlockUntil, NewTicker/NewTimer/Sleep/Deadline)
- `internal/clock/manual_test.go` - 14 tests for Manual and for WithTimeout/TimedOut under it; two unexported helpers (`count`, `due`) live in the test file
- `internal/engine/engine_test.go` - fake-firmware harness and four test functions

## Decisions Made

See `key-decisions`. The plan's blocking unbuffered delivery was kept; the one addition is that a ticker stopped while queued delivers nothing.

## Deviations from Plan

None - plan executed exactly as written. `engine.go` needed no fix: all engine tests passed against the 01-02 implementation, and mutation checks confirmed they would fail on regressions.

### Process notes

- Task 2 is marked `tdd="true"` but the behaviors already held in the 01-02 engine, so there is no RED commit for it (a test that passes on first run has no failing state to commit). Task 1 does have the RED then GREEN commit order the acceptance criterion asks for.
- The plan's 2 s poll budget for Snapshot was widened to a 5 s real-time guard to avoid flakiness under `-race` on a loaded host. It is only a hang guard; the engine itself never waits on real time. 30 repeated runs of the engine package (`-count=30`, `-race`) and 20 of the clock package passed.

## Issues Encountered

None.

## Threat Surface

T-01-28 verified: `grep -rn NewManual --include='*.go' internal cmd | grep -v _test.go | grep -v '^internal/clock/'` prints nothing, and the supervisor still builds the engine with `clock.NewReal()`. T-01-SC: go.mod and go.sum are unchanged (no new modules). No new network, auth or file-access surface.

## Known Stubs

None.

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing new from this plan. Carried over from 01-02: the pending maintainer provenance review of flynn/noise and go.yaml.in/yaml/v3. The flagged assumption `EDGE-MODEL-04-unclassified` now has its Manual-clock engine test in place; its manual edge review remains the maintainer's call.

## Next Phase Readiness

Ready for 01-05 (model semantics behind the unchanged `model.Step` signature): the engine reference test composes `model.Step` directly, so it will follow the model changes while still failing on a wrong dt or a wall-clock leak. Ready for 01-06 (strict codec) and 01-10 (engine session): the harness in engine_test.go (fake firmware peer, `settle` barrier) is reusable for them. Note for 01-05: the reference test uses duty 0.4 on the default fan; if start hysteresis changes the trajectory, the test still holds because it recomputes from `model.Step`.

## Self-Check: PASSED

- Files exist: `internal/clock/manual.go`, `internal/clock/manual_test.go`, `internal/engine/engine_test.go`.
- Commits present in `git log`: `01c6d24`, `af0519a`, `fb885a7`; test commit precedes the implementation commit for manual.go.
- `make check` (gofmt, vet incl. e2e tag, `go test -race ./...`) passes; `make test-pkg PKG=./internal/clock/` and `PKG=./internal/engine/` print `ok`; the NewManual production-use grep prints nothing; `grep -n WithTimeout|TimedOut internal/clock/manual_test.go` match.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
