---
phase: 02-full-rev-3-3-bench
plan: 06
subsystem: live views of a run (watch events, logs), supervisor event broker, streaming control API
tags: [go, events, engine, supervisor, control-api, ndjson, cli, logs, e2e]
requires:
  - phase: 02-full-rev-3-3-bench
    provides: 02-05 per-port fans (Status.ports[].fan, engine Change/Apply), 02-03 env inputs, 02-04 LED strip
provides:
  - internal/events: Broker (seq, bounded per-subscriber queues, lagged marker, kind filter, Close) that outlives engine replacement
  - engine change events (duty, rpm, state, fan, button, env, leds, firmware) and Engine.SnapshotSync for a lossless snapshot-to-stream join
  - GET /api/v1/watch and GET /api/v1/logs (JSON, or application/x-ndjson streams), Client.Stream / Watch / Logs / FollowLogs
  - `sil watch [--kind K,...] [--count N]` and `sil logs [--source firmware|sim|all] [--tail N] [--follow] [--raw]`
  - error codes too_many_streams, logs_unreadable (exit 2)
affects: [02-07 contract test, 03 web UI (reads the same stream), 03 scenario runner]

actuals:
  tokens: 30500
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Subscribe first, then take the engine snapshot on the engine goroutine (SnapshotSync) stamped with the broker seq; forward only events above it: the join loses and repeats nothing"
    - "Streams are NDJSON over api.sock behind one streamNDJSON helper (flush per line, JSON error reply if nothing was sent yet); clients treat an end without a stopped line as a lost connection"
    - "Log content is cleaned as a whole line: ANSI strip, key redaction, and only then the 64 KiB cut"

key-files:
  created:
    - internal/events/events.go
    - internal/events/events_test.go
    - internal/supervisor/watch.go
    - internal/supervisor/watch_test.go
    - internal/supervisor/logs.go
    - internal/supervisor/logs_test.go
    - internal/cli/watch_test.go
    - internal/cli/logs_test.go
    - e2e/watch_test.go
  modified:
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/supervisor/supervisor.go
    - internal/control/control.go
    - internal/control/control_test.go
    - internal/cli/cli.go
    - internal/cli/cli_test.go
    - internal/apperr/codes.go

key-decisions:
  - "Snapshot/stream join through the engine goroutine: Engine.SnapshotSync (an inbox event) returns a snapshot stamped with the broker's seq (Config.Seq); the plan's 'Snapshot.Seq = last seq the engine emitted' alone leaves a window between emit and publish in which a watcher would lose an event"
  - "A new engine emits one state event per port when it starts, so a stream that survives `sil sim restart` sees the new engine's states; sim restarted is published right after the new engine is in the slot (not after the resync wait), so the order on the stream is firmware false, sim restarted, then the new engine's firmware true"
  - "The lagged line carries the seq and t of the last dropped event, so seq stays strictly increasing across it"
  - "stopped lines are {t,k} without a seq; they are not events"
  - "Stream cap: 16 streams per instance counted in the supervisor (watch and logs --follow together); the broker has its own 16-subscriber cap behind it"
  - "Log lines are served whole and redacted before the 64 KiB cut; a line whose beginning was not read (tail cap, or an over-long unterminated line in a follow) is never served in pieces"

requirements-completed: [CLI-03, CLI-04, CLI-01]

coverage:
  - id: D1
    description: "`sil watch` prints a snapshot line (k snapshot, integer t and seq, exactly the fields of `sil status`) and then one typed event per change with integer t, k and strictly increasing seq; the first 10 events after a snapshot taken during the boot pulse are seq + 1 .. + 10 with no lagged line"
    requirement: CLI-03
    verification:
      - kind: e2e
        ref: "e2e/watch_test.go#TestWatchStream/snapshot_first, press_events, env_event"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineEmitsDutyOnChangeOnly, TestEngineEmitsRPMOncePerSecondOnChange, TestEngineEmitsStateAndStartStates, TestEngineEmitsButtonAndEnv, TestEngineEmitsFirmwareAndSnapshotSeq"
        status: pass
    human_judgment: false
  - id: D2
    description: "A change that happens while a watch starts is either in the snapshot or an event after it, never neither and never both (25 watches joined against a continuous writer saw every following value in order)"
    requirement: CLI-03
    verification:
      - kind: unit
        ref: "internal/supervisor/watch_test.go#TestWatchJoinIsLossless, TestWatchJoinsDuringTheEngineGap"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineSnapshotSyncStampsTheBrokerSeq"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every LED frame the engine receives is a leds event (identical replays included, invalid frames not), at most 66 in any 3 s of sim time during the boot pulse"
    requirement: CLI-03
    verification:
      - kind: e2e
        ref: "e2e/watch_test.go#TestWatchStream/leds_events"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineEmitsEveryLEDFrame"
        status: pass
    human_judgment: false
  - id: D4
    description: "The stream survives `sil sim restart` (sim restarted, firmware false then true, later presses still produce events), ends with a stopped line and exit 0 on `sil down` (55 ms with an open stream), SIGINT is exit 0, a dropped connection is exit 2 control_unreachable, --kind filters, --count N exits 0 after N events"
    requirement: CLI-03
    verification:
      - kind: e2e
        ref: "e2e/watch_test.go#TestWatchStream/across_sim_restart, stopped_on_down, snapshot_first (SIGINT), kind_filter, count"
        status: pass
      - kind: unit
        ref: "internal/cli/watch_test.go#TestWatchCopiesTheStreamUntilStopped, TestWatchDroppedStreamIsControlUnreachable, TestWatchSIGINTEndsWithExit0, TestWatchCountEndsTheStream, TestWatchArgumentErrors"
        status: pass
    human_judgment: false
  - id: D5
    description: "A slow subscriber never blocks the engine (1024-event queue, one lagged line with the drop count), at most 16 streams per instance (the 17th is exit 2 too_many_streams, also over the real CLI), status keeps answering while 16 streams are open"
    requirement: CLI-01
    verification:
      - kind: unit
        ref: "internal/events/events_test.go#TestSlowSubscriberNeverBlocksAndLagsOnce, TestSubscriberCap, TestConcurrentUse; internal/cli/watch_test.go#TestWatchTooManyStreams"
        status: pass
      - kind: e2e
        ref: "e2e/watch_test.go#TestWatchStream/too_many_streams, status_while_streaming"
        status: pass
    human_judgment: false
  - id: D6
    description: "`sil logs` prints the last 100 lines of firmware.log (separators included, ANSI removed), --source sim|all, --tail 1..10000, --raw bare lines with colour kept, missing file is an empty list; --follow streams new lines (polled every 200 ms, restart after truncation) across a firmware restart and ends with stopped on `sil down`"
    requirement: CLI-04
    verification:
      - kind: e2e
        ref: "e2e/watch_test.go#TestLogs/firmware_tail, sim_and_all, tail_bounds, raw, follow_across_fw_restart, follow_stopped_on_down"
        status: pass
      - kind: unit
        ref: "internal/supervisor/logs_test.go#TestTailLines, TestStripANSI, TestLogsServesTailsCleanly, TestFollowLogsDeliversGrowthPartialLinesAndTruncation, TestLogsMissingFileAndBadRequests"
        status: pass
    human_judgment: false
  - id: D7
    description: "Logs never leak: only firmware.log and sil.log of the own run dir (enum source), build.log never, the instance key is replaced by <redacted> in every served line including a key that straddles the 64 KiB cut and in --raw output"
    requirement: CLI-04
    verification:
      - kind: unit
        ref: "internal/supervisor/logs_test.go#TestLongLinesAreRedactedBeforeTheyAreCut, TestFollowStartingMidLineDropsTheRestOfIt, TestFollowDropsAnOverlongLine, TestLogsServesTailsCleanly; internal/control/control_test.go#TestLogsRoute"
        status: pass
    human_judgment: false
  - id: D8
    description: "The event vocabulary (kinds, fields, rpm sampled once per sim second, state events at engine start) is adequate for the consumers that will use it (web UI, scenarios, agents)"
    requirement: CLI-03
    verification: []
    human_judgment: true
    rationale: "Names and shapes are internal while 0.x (D-12) and were chosen unattended (assumption A-02-06); whether they serve the later consumers is a design judgment no test asserts"

duration: 28min
completed: 2026-11-09
status: complete
commits: 4
plan_head_before: b83b31002a26bed2b27c48a5d2140bf820fe609f
plan_head_after: 8e9bfc23048ebcaae3669d8e1d031ba1c0ea3ae2
---

# Phase 2 Plan 06: Watch and logs Summary

**`sil watch` streams a snapshot and then typed change events as NDJSON from a supervisor-owned broker that survives simulator restarts and ends with a stopped line, and `sil logs` tails or follows firmware.log and sil.log through the control API with colour removed and the API key redacted.**

## Performance

- **Duration:** about 28 min (plus a 520 s full e2e run)
- **Tasks:** 2 (both TDD), 4 commits (2 feature, 2 security fixes found by the automated review hook)
- **Files:** 17 changed (9 created, 8 modified), +3630 / -20 lines
- **Verification:** `make check` (Go race tests, vet, gofmt, 119 C++ vectors) passes. `make e2e`: full suite, 46 tests pass; the one failure of that run (TestFirmwareBus, "working tree changed by the build") was caused by this executor editing `internal/supervisor/logs.go` while the suite ran; it passes on its own, together with TestLogs, TestWatchStream and TestFwRestartResync, after the final commit. TestPressTracer, TestLEDStatus, TestStatusDuringRestart and TestSupervisorSIGTERM passed after Task 1.

## Accomplishments

- **Event broker** (`internal/events`): mutex-guarded seq, one buffered queue (1024) per subscriber, non-blocking publish, one `lagged` marker with the drop count before the next delivered event, kind filter that never drops lagged, 16-subscriber cap, idempotent `Close` that lets buffered events drain.
- **Engine events** with sim-µs `t`: `duty` and `leds` on every change/frame, `rpm` sampled once per sim second on float32 change, `state`, `fan`, `button`, `env`, `firmware`. Tick-driven events carry the tick's own time, frame-driven ones the clock's exact time.
- **Lossless join**: `Engine.SnapshotSync` runs on the engine goroutine between two events and stamps the broker seq; `Watch` subscribes first, then joins. A property test joins 25 watches against a continuous writer and checks every following value arrives once, in order.
- **Streaming API**: `streamNDJSON` (flush per line, JSON error reply while nothing was sent), `Client.Stream` (no body cap, no deadline, non-2xx parsed like `do`), `Client.Watch`/`FollowLogs` relaying raw lines and reporting a stream without a stopped line as `control_unreachable`; half lines cut by a dropped connection are never passed on.
- **`sil watch`** and **`sil logs`** commands; streaming commands set `silent` and write through the new `runEnv.stdout`.
- **Logs**: backwards 64 KiB-block tail, CR/LF, missing file = empty list, follow on the supervisor clock (200 ms), truncation restart, partial lines held until their newline, final poll plus `stopped` when the instance stops, one of the 16 streams.

### Event kinds (internal while 0.x)

Every line has integer `t` (sim µs) and `k`; events and the snapshot also have a strictly increasing integer `seq`.

| k | fields | when |
|---|--------|------|
| `snapshot` | `t`, `seq`, plus every field of `sil status` at top level | first line only |
| `duty` | `port`, `v` (0..1) | firmware changed a port's duty |
| `rpm` | `port`, `v` (float32 rpm) | at most once per sim second per port, only if the value changed |
| `state` | `port`, `v` `running`/`stopped` | fan state change; one per port when an engine starts; after a replug |
| `fan` | `port`, `v` fan id | `sil set portN.fan=ID` |
| `button` | `id` usr1..usr3, `v` bool | level change |
| `env` | `id` temperature/humidity, `v` | `sil set` |
| `leds` | `strip` (`#rrggbb` per LED) | every valid LED frame, replays included |
| `firmware` | `v` bool | bus connect / disconnect / engine stop |
| `sim` | `v` `"restarted"` | simulator engine replaced |
| `lagged` | `dropped` | queue overflowed; seq/t of the last dropped event |
| `stopped` | `t` | instance stopped (no seq); always the last line |

## Task Commits

1. **Task 1: `sil watch`** - `2bae47f` (feat)
2. **Task 2: `sil logs`** - `de09230` (feat)
3. **Security fix: redact the API key before a long log line is cut** - `e1907b5` (fix)
4. **Security fix: a log follow that starts mid-line drops the rest of that line** - `8e9bfc2` (fix)

**Plan metadata:** committed separately (docs: complete plan).

## Decisions Made

See `key-decisions`. The two that go beyond the plan text are the engine-goroutine snapshot (`SnapshotSync`) and the state events at engine start.

## TDD Gate Compliance

Both tasks are `tdd="true"`. Honest account: the unit tests of `internal/events` were written first (a compile-time red) and then the implementation; for the engine, supervisor, control, CLI and e2e layers the tests were written in the same pass as the code and run green together, so there is **no separate RED commit and no red test run recorded** for them (unlike 02-05, which committed its e2e RED). The behaviours were verified by the tests passing for the right reasons where it mattered: the join property test would fail on a stale snapshot (tick events with a clock that ran ahead of the tick were caught by `TestEngineEmitsRPMOncePerSecondOnChange` at its first run, 990 ms apart, and fixed with the in-tick timestamp rule). `gsd_run check tdd-red-evidence` was not produced.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Correctness] Lossless join needs the snapshot taken on the engine goroutine**
- **Found during:** Task 1 design
- **Issue:** The plan stamps `Snapshot.Seq` with the last seq the engine emitted. The engine emits before it publishes its snapshot, so a watcher that subscribes and reads the snapshot between the two steps would get a snapshot older than an event published before it subscribed: that change would be in neither the snapshot nor the stream, violating the plan's own truth ("never neither").
- **Fix:** `Engine.SnapshotSync` (inbox event, reply with a snapshot built between two events, stamped with `Config.Seq` = broker seq); `Watch` falls back to the stopped engine's last snapshot with the seq at subscription during the restart gap. `Snapshot.Seq` (last emitted seq) stays as the plan describes.
- **Files modified:** internal/engine/engine.go, internal/supervisor/watch.go
- **Verification:** TestWatchJoinIsLossless, TestWatchJoinsDuringTheEngineGap, TestEngineSnapshotSyncStampsTheBrokerSeq
- **Commit:** 2bae47f

**2. [Rule 2 - Security] Key redaction after the 64 KiB cut could leak half a key**
- **Found during:** Task 2, reported by the security review hook (twice)
- **Issue:** `tailLines` and the follower cut lines at 64 KiB before `clean` redacted them, so a key straddling the cut was served in part; `readTail` also served a first line whose beginning it had not read.
- **Fix:** lines are read whole, cleaned (ANSI, key), then cut; a first line that starts before the block read is never served; a line that outgrows 8 MiB without a newline (follow) or a follow that starts mid-line is dropped up to its newline.
- **Files modified:** internal/supervisor/logs.go, logs_test.go
- **Verification:** TestLongLinesAreRedactedBeforeTheyAreCut, TestFollowDropsAnOverlongLine, TestFollowStartingMidLineDropsTheRestOfIt
- **Commits:** e1907b5, 8e9bfc2

**3. [Rule 3 - Scope of files] Supervisor Watch lives in its own file**
- `Watch`, the stream-slot accounting and the join are in `internal/supervisor/watch.go` (not listed in the plan) instead of growing supervisor.go further; `statusFrom(snapshot)` is the Status refactor the plan asked for. New test files: `internal/cli/watch_test.go`, `internal/cli/logs_test.go`, `internal/supervisor/watch_test.go`.

**Total deviations:** 3 (2 correctness/security fixes, 1 file layout). **Impact:** no scope change; the tests the plan checker asked for (stopped line, dropped stream, too_many_streams at CLI level, SIGINT exit 0) are all present: `internal/cli/watch_test.go` (`TestWatchCopiesTheStreamUntilStopped`, `TestWatchDroppedStreamIsControlUnreachable`, `TestWatchTooManyStreams`, `TestWatchSIGINTEndsWithExit0`) and, over the real process, `TestWatchStream/snapshot_first` (SIGINT, exit 0), `stopped_on_down`, `too_many_streams`.

## Issues Encountered

- The first full e2e run failed `TestFirmwareBus` only because the suite's "working tree changed by the build" check saw this executor's edit of `logs.go`; rerun alone it passes. Not a product defect.
- The security hook fired three times on `logs.go`; the first two findings were valid and fixed (above). The third finding (a follow starting mid-line) was also valid and fixed in `8e9bfc2`.

## Known Stubs

None. No placeholder data flows to a consumer.

## Threat Flags

None beyond the plan's register. T-02-14 (bounded queues, lagged marker, 16 streams) and T-02-15 (broker closed before the control server stops; `stopped_on_down` returned in 55 ms) are implemented and tested; T-02-16/T-02-17 (enum source, instance.Paths only, `grep BuildLog logs.go` finds nothing, key redaction including `--raw`, tail 1..10000) with the two hardening fixes above. No Go module was added (T-02-SC).

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocks the plan. For the morning review:

- **A-02-06 / D8 (flagged in the plan):** the event vocabulary, the once-per-second rpm sampling, state events at engine start, the 16-stream cap, the 1024-event buffer and the 200 ms log poll are unattended choices; confirm them when the web UI (phase 3) becomes the second consumer.
- **EDGE-CLI-03-unclassified and EDGE-CLI-04-unclassified (flagged in the plan):** the manual edge review is still open; automated coverage is D1 to D7 (join, filter, count, lagged, restart, stopped, SIGINT, too many streams, tail bounds, missing file, ANSI, raw, truncation, redaction).
- **Behaviour to be aware of:** a watch that joins during a simulator restart gap starts from the stopped engine's last snapshot; `sil logs --raw --follow` prints nothing for the stopped line (it just exits 0).

## Next Phase Readiness

Ready for 02-07. The control API now has two streaming routes behind one helper, so the web UI can use `GET /api/v1/watch` as is; `sil watch --kind` and `sil logs --follow` give the contract test (02-07) the observation channels it needs. No blockers.

## Self-Check: PASSED

- Created files exist: events.go, events_test.go, watch.go, watch_test.go, logs.go, logs_test.go (supervisor), cli watch_test.go and logs_test.go, e2e/watch_test.go.
- Commits exist: `2bae47f`, `de09230`, `e1907b5`, `8e9bfc2`.
- Acceptance greps: `"too_many_streams"` and `"logs_unreadable"` in `internal/apperr/codes.go`, `application/x-ndjson` in `internal/control/control.go`, `BuildLog` absent from `internal/supervisor/logs.go`.
- `make check` passes; `make e2e E2E_RUN='TestFirmwareBus|TestLogs|TestWatchStream|TestFwRestartResync'` passes (final code); TestPressTracer, TestLEDStatus, TestStatusDuringRestart, TestSupervisorSIGTERM passed after Task 1.
