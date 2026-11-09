---
phase: 03-live-web-ui
plan: 07
subsystem: engine
tags: [go, sample-events, history-ring, behind-real-time, max-hold, control-api, cli]

requires:
  - phase: 03-live-web-ui
    provides: "03-04 control.Operations table and NewLANHandler; 03-05 guard and CSRF table test"
provides:
  - "events.SampleKind/Sample/HistoryCap/Broker.History, Event.Reason, Event.Gap"
  - "engine sample event every 100 ms of sim time (null duty/rpm without a firmware, gap:true on an engine's first sample)"
  - "GET /api/v1/history?window_s=W (Operations row, LAN true) and `sil history [--window D]`"
  - "status sim.behind_real_time and sim behind/in_sync events (engine noteTick with 2 s hysteresis)"
  - "POST /api/v1/buttons/{id}/down with optional {max_hold_ms}, engine auto-release with button reason max_hold, `sil press --down --max-hold D`"
affects: [03-08, 03-10, 03-11, 03-12]

actuals:
  tokens: 17000
  tasks: 3
  commits: 6
plan_head_before: e29965f27a6f4eb9c94376c2ec25a73648b8737b
plan_head_after: 22d1879ac80a0f98aa70bac5e9e0bd10642c0521
commits: 6

tech-stack:
  added: []
  patterns:
    - "an opt-in event kind: in events.UnknownKind and subscribable by name, absent from events.Kinds, skipped by a nil-kinds subscription"
    - "a bounded ring inside the broker mutex that assigns seq, so a History call is consistent with the seq it returns"
    - "engine-owned derived flags (behind, gap, auto-release) computed once and carried by status/watch, so the page only draws them"

key-files:
  created:
    - internal/supervisor/history.go
    - e2e/signals_test.go
  modified:
    - internal/events/events.go
    - internal/engine/engine.go
    - internal/control/control.go
    - internal/control/operations.go
    - internal/supervisor/supervisor.go
    - internal/cli/cli.go
    - README.md
    - e2e/watch_test.go

key-decisions:
  - "Samples share the broker seq (as the plan fixes), so a default stream's seq is strictly increasing but no longer consecutive"
  - "History window 0 on the Go side means the whole ring: the client leaves window_s out and the handler passes 300 on, so Control.History always sees 0 < W <= 300"
  - "The history row is a read: added to the reviewed read-only list of TestCSRFCoversEveryMutatingRoute"
  - "sampleDue advances nextSample past now in a loop, so a late tick yields one sample and skips the missed slots"
  - "Auto-release runs inside the tick's inTick region, so the release event is stamped with the tick time and happens at most one tick (10 ms) after the deadline"

patterns-established:
  - "New read operation = new row in control.Operations with LAN decided there, plus an entry on the CSRF test's read list"
  - "Hysteresis logic is a pure method (noteTick) driven by hand in Manual-clock tests; the real-clock e2e asserts only wiring and flip consistency"

requirements-completed: [UI-02, UI-04, UI-05, UI-06]

duration: 15min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "The engine publishes one sample event per 100 ms of sim time (null duty/rpm without a firmware, missed slots skipped, gap:true only on an engine's first sample); the broker keeps the last 3000 in a ring; GET /api/v1/history and `sil history` return them"
    requirement: UI-02
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestSampleCadence"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestSampleGapMarker"
        status: pass
      - kind: unit
        ref: "internal/events/events_test.go#TestSampleRing"
        status: pass
      - kind: e2e
        ref: "e2e/signals_test.go#TestHistory"
        status: pass
    human_judgment: false
  - id: D2
    description: "`sample` is opt-in: default watches and empty kind lists never carry it, `--kind sample` does"
    requirement: UI-06
    verification:
      - kind: unit
        ref: "internal/events/events_test.go#TestDefaultSubscriptionExcludesSample"
        status: pass
      - kind: e2e
        ref: "e2e/signals_test.go#TestHistory/sample_is_opt-in"
        status: pass
    human_judgment: false
  - id: D3
    description: "status sim.behind_real_time is always a boolean, set by tick lateness with a 2 s hold-off, and transitions publish sim behind/in_sync"
    requirement: UI-05
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestBehindRealTime"
        status: pass
      - kind: unit
        ref: "internal/supervisor/watch_test.go#TestStatusCarriesBehindRealTime"
        status: pass
      - kind: e2e
        ref: "e2e/signals_test.go#TestBehindField"
        status: pass
    human_judgment: false
  - id: D4
    description: "A button held with max_hold_ms is released by the engine after that much sim time with a button event reason max_hold; the API body, the supervisor and `sil press --down --max-hold` carry it"
    requirement: UI-04
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestMaxHold"
        status: pass
      - kind: unit
        ref: "internal/control/control_test.go#TestDownBody"
        status: pass
      - kind: e2e
        ref: "e2e/signals_test.go#TestMaxHold"
        status: pass
    human_judgment: false
  - id: D5
    description: "The 0.x thresholds (50 ms late, 2 s hold-off, 10 Hz, 5 minute ring) are planner assumptions A-03-05 and A-03-06 pending UAT feedback"
    requirement: UI-05
    verification: []
    human_judgment: true
    rationale: "UIC-30 is unresolved in the UI-SPEC; whether 50 ms and 2 s feel right on a loaded host needs a person watching the page (03-11)"
---

# Phase 3 Plan 07: Samples, Behind-Real-Time and Max Hold Summary

**The engine now publishes a 10 Hz `sample` stream into a 3000-entry broker ring behind `GET /api/v1/history` and `sil history`, computes `sim.behind_real_time` with a 2 s hold-off, and auto-releases a button held with `max_hold_ms`, each reachable from the CLI before the page uses it.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-11-09T14:07:34Z
- **Completed:** 2026-11-09T14:22:18Z
- **Tasks:** 3 (1 tracer, 2 TDD)
- **Files modified:** 17 (2 created)

## Accomplishments

- Task 1 (tracer): `events.SampleKind`, `Sample`, `HistoryCap = 3000` and a ring filled in `Publish` under the seq lock; the engine emits one `sample` per 100 ms of sim time and marks an engine's first sample with `gap: true`; `Broker.History`, the `history` Operations row (LAN true), `supervisor.History`, `Client.History` and `sil history [--window D]`.
- Task 2: `Engine.noteTick(tickAt, handledAt, dt)` with `behindLate` 50 ms and `behindClear` 2 s; `Snapshot.BehindRealTime` and `SimStatus.BehindRealTime` (`sim.behind_real_time`, always present); `sim` events `behind` and `in_sync`.
- Task 3: `DownRequest`, the optional `{"max_hold_ms": N}` body on `.../down` (1..60000, `up` refuses a body), `Engine.holdUntil` with `releaseExpired` on the tick, `PressResult.MaxHoldMS`, `sil press --down --max-hold D`, README command reference.
- Measured: median sample spacing 99855 to 100071 us over 20 to 21 samples on the real stack (three e2e runs); `behind_real_time` did not flip during any e2e run (0 flips in the 3 s idle watch, snapshot value false); the auto-release event arrived at sim t 2.010731 s for a 1500 ms hold started at about 0.5 s.

## Task Commits

1. **Task 1 RED** - `5d8016b` (test): sample ring, cadence, gap marker, history route, `sil history`
2. **Task 1 GREEN** - `52faece` (feat): sample events, history ring, `GET /api/v1/history`, `sil history`
3. **Task 2 RED** - `3ccca88` (test): behind-real-time hysteresis, status field, sim events
4. **Task 2 GREEN** - `7ce47e2` (feat): `sim.behind_real_time` and `behind`/`in_sync` events
5. **Task 3 RED** - `f6556f1` (test): max hold in the engine, down body, `--max-hold`
6. **Task 3 GREEN** - `22d1879` (feat): auto-release, API body, CLI flag, README

## Files Created/Modified

- `internal/events/events.go` - SampleKind, Sample, HistoryCap ring, Broker.History, Event.Reason/Gap, opt-in kind handling
- `internal/engine/engine.go` - sample emission, noteTick, releaseExpired, SetButton with maxHold, Snapshot.BehindRealTime
- `internal/control/control.go`, `operations.go` - HistoryRequest/Result, DownRequest, Control.History, new SetButton signature, SimStatus.BehindRealTime, history row, down/up body handling
- `internal/supervisor/history.go` (new), `supervisor.go` - History, SetButton with DownRequest, behind flag in statusFrom
- `internal/cli/cli.go` - HistoryCmd, PressCmd.MaxHold, watch help lists the opt-in kind
- `e2e/signals_test.go` (new) - TestHistory, TestBehindField, TestMaxHold
- `README.md` - command reference

## Decisions Made

See `key-decisions`. The one worth restating: samples take broker seq numbers a default stream never receives, which is what makes `history.seq` consistent with every stream, at the cost of seq no longer being consecutive for subscribers that do not ask for samples.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestWatchStream/snapshot_first assumed consecutive seq**
- **Found during:** Task 1 (first e2e run)
- **Issue:** the Phase 2 e2e asserted that the first 10 events after the snapshot have seq snapshot+1..+10. Sample events consume broker seq numbers that a default subscription never receives, so the stream now has holes by design.
- **Fix:** the assertion is strictly increasing and above the snapshot's seq, and a default stream must not carry a `sample` line. Join consistency (nothing lost or repeated at the join) stays covered by the supervisor watch tests.
- **Files modified:** `e2e/watch_test.go`
- **Commit:** `52faece`

**2. [Rule 3 - Blocking] TestCSRFCoversEveryMutatingRoute read list**
- **Found during:** Task 1
- **Issue:** the new GET `history` row is not on the reviewed read-only list, so the table test failed by design.
- **Fix:** `history` added to the list after review (it only reads the ring).
- **Files modified:** `internal/web/guard_test.go`
- **Commit:** `52faece`

**3. [Rule 3 - Blocking] Control interface fakes**
- **Found during:** Tasks 1 and 3
- **Issue:** `History` and the new `SetButton(ctx, id, down, DownRequest)` signature had to be added to the fakes of control, cli and web tests.
- **Files modified:** `internal/control/control_test.go`, `internal/cli/cli_test.go`, `internal/web/guard_test.go`
- **Commits:** `5d8016b`, `f6556f1`

**Total deviations:** 3 auto-fixed (1 bug-class test assumption, 2 blocking). **Impact:** none on the plan's behavior; no scope added.

## Issues Encountered

- `internal/web` `TestWSPingReaps` panicked once ("close of closed channel", `ws_test.go:196`) during the first full `./internal/...` run, while the guard test was failing; it passed in every later run (`-count=3`, three `make check` runs). It is a test-side flake in a file this plan does not touch and is not caused by it; logged here, not fixed (scope boundary).

## Known Stubs

None.

## Threat Flags

None. The new surface (GET history, down body) is the plan's T-03-21 to T-03-23: fixed 3000-event ring, `window_s` parsed strictly in (0, 300], samples opt-in, `max_hold_ms` 1..60000, 64 KiB body cap with `DisallowUnknownFields`, `up` refuses a body.

## Next Phase Readiness

- 03-10 and 03-11 can draw the plot from `GET /api/v1/history` plus `/ws?kind=sample` (seq-join on `history.seq`), break the line on `gap: true`, show the behind cell from `sim.behind_real_time` and the sim events, and always send `max_hold_ms` (10 s) with a page hold.
- 03-08 is unaffected (uses `set` and `fans`).
- The `SetButton` signature on `control.Control` changed (extra `DownRequest`); any later fake must follow it.

## Self-Check: PASSED

- Files: `internal/supervisor/history.go`, `e2e/signals_test.go` and this summary exist; `HistoryCap = 3000`, `"/api/v1/history"` row, `json:"behind_real_time"`, `max-hold` and `sil history` in the README all match.
- Commits `5d8016b`, `52faece`, `3ccca88`, `7ce47e2`, `f6556f1`, `22d1879` exist on `claude/main`.
- `make check` passes (119 tests); `make e2e E2E_RUN='TestHistory|TestBehindField|TestMaxHold|TestPressTracer|TestPressVocabulary|TestWatchStream'` and `TestWeb` pass; `make test-ui UI_RUN='Parity'` passes.
