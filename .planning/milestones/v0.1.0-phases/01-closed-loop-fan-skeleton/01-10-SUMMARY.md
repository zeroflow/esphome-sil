---
phase: 01-closed-loop-fan-skeleton
plan: 10
subsystem: sim-bus-session-and-restarts
tags: [go, cpp, esphome, sim-bus, hello-manifest, snapshot, backoff, restart, control-api, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 06)
    provides: "PROTOCOL.md session messages and sim_protocol.h / bus.Decode codecs"
  - phase: 01-closed-loop-fan-skeleton (plan 07)
    provides: "SimBus::set_out/last_out_/children, SimChild::direction, platform split"
  - phase: 01-closed-loop-fan-skeleton (plan 09)
    provides: "supervisor probe loop, generation-counted startProbe, identity and run dirs"
provides:
  - "bus.Server session: hello deadline (2 s), strict framing that closes on any bad line, newest validated hello replaces the older session (bye), welcome / in-snapshot / sync, held-back sends, Handler{Hello,Manifest,Frame,Disconnected(reason)}"
  - "engine: in-snapshot provider, manifest validation warnings (missing / unknown / direction / type), proto-mismatch and disconnect reasons in status warnings, Snapshot.Synced, a stopped engine publishes bus_connected false"
  - "firmware hub on sim_protocol.h: hello / chan / out / sync on every connect, welcome / in / sync / err / bye handling, backoff 100 ms to 2000 ms with reset on welcome, 65536-byte TX cap"
  - "control: POST /api/v1/sim/restart (gap_s 0..30) and POST /api/v1/fw/restart, Client.SimRestart / FwRestart bounded by a clock timeout"
  - "supervisor: engine slot replaced on SimRestart (D-02), per-process firmware handle for FwRestart (D-05), opMu serialization, Status independent of it"
  - "CLI: sil sim restart [<name>] [--gap D], sil fw restart [<name>]"
affects: [01-11, 01-12, 01-13]

actuals:
  tokens: 28500
  tasks: 2
  commits: 2
plan_head_before: 14161b0a1bac4a5f069176eb665e754f0b901b15
plan_head_after: 0c6e39db3f66926a838336c308d98bcee03d39d7

tech-stack:
  added: []
  patterns:
    - "Session ordering by a per-connection state (holding / ready) in bus.Server: Sends issued while the engine prepares the in-snapshot are queued and follow the sync, so welcome and the snapshot always come first and a stale snapshot value can never overwrite a newer steady frame"
    - "Handler.Hello is answered by the engine goroutine through the inbox (request/reply event), so the snapshot is taken at one defined point of the engine's single-writer order"
    - "One dispatch mutex serializes every Handler call across connections; the replacement notification (Disconnected(\"replaced\")) is delivered before the newer connection's Hello"
    - "Supervisor operations (SimRestart, FwRestart, shutdown) hold opMu; shutdown first closes downReq so an operation in flight gives up at once; Status takes only mu"
    - "fwProc per firmware process (gone, recorded, requested) replaces the single-use fwGone channel"

key-files:
  created:
    - internal/bus/server_test.go
    - internal/control/control_test.go
    - e2e/restart_test.go
  modified:
    - components/sim_bus/sim_bus.h
    - components/sim_bus/sim_bus.cpp
    - internal/bus/server.go
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/supervisor/supervisor.go
    - internal/control/control.go
    - internal/cli/cli.go
    - e2e/io_test.go
    - protocol/PROTOCOL.md

key-decisions:
  - "A connection becomes the active session at its first valid hello (not at accept): a silent client, garbage or a debugging socat never disturbs a running session; a mismatching hello still takes the slot so status describes the newest peer, then is refused"
  - "Disconnected(reason) is delivered for every session that was active (reasons: \"\", replaced, proto_mismatch, bad_frame: <class>, peer_error: <msg>, send_queue_full); a connection that never said hello is reported (hello_timeout, no_hello, bad_hello) only when no session is active"
  - "The very first firmware connection happens in setup() before any output was written, so its snapshot carries no out frames and the boot duties follow as ordinary out frames; every later connect replays last_out_ before sync (documented in PROTOCOL.md)"
  - "The firmware accepts in frames at any time after connecting; welcome only resets the reconnect backoff. This keeps the shared busPeer of harness_test.go (no welcome) working unedited"
  - "Reconnect backoff: a connect failure or a session that ended before welcome doubles the delay (100, 200, 400, 800, 1600, 2000 ms); a session that reached welcome retries after 100 ms"
  - "Sim time keeps running across a simulator restart (the supervisor clock is monotonic and shared); only the World is fresh"
  - "SimRestart waits up to 15 s for bus_connected and manifest checked, FwRestart up to 30 s for phase ready; a restart whose firmware is not running answers firmware_not_running at once instead of waiting"

patterns-established:
  - "Pre-session faults are visible in status warnings, never fatal: the engine turns every manifest difference and every session end reason into a warning"
  - "The firmware half of the session is tested by a real firmware against a scripted peer (TestHubSession): snapshot order, manifest, backoff gaps, err/bad-line/bye handling"

requirements-completed: [BUS-01, BUS-02, BUS-03, CLI-06]

coverage:
  - id: D1
    description: "Firmware and simulator exchange hello{proto,name,chans,t}, chan per channel, last out values, sync, and welcome, in per port in port order, sync on every (re)connect; order is fixed; welcome always first, sync always last"
    requirement: "BUS-02"
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineSnapshotOrderAfterHello, TestEngineSnapshotCarriesCurrentRPM; internal/bus/server_test.go#TestSessionOrderAndHeldSends, TestManifestAtSyncAndOutFramesInOrder"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestHubSession"
        status: pass
    human_judgment: false
  - id: D2
    description: "Zero channels, missing, unknown, wrong-direction and wrong-type channels are status warnings, never fatal; a protocol mismatch gets err + close, a warning and bus_connected false; the firmware logs it and retries with backoff"
    requirement: "BUS-02"
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineManifestWarnings, TestEngineProtoMismatch, TestEngineUnknownOutChannelWarnsOnce; internal/bus/server_test.go#TestProtoMismatch"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestHubSession (welcome with proto 2 -> drop and growing reconnect gaps)"
        status: pass
    human_judgment: false
  - id: D3
    description: "One active firmware connection per bus: a newer session replaces the older (bye, then close), late frames of the replaced connection are discarded, stray clients do not disturb the session; hello deadline, line cap and close on decode error"
    requirement: "BUS-01"
    verification:
      - kind: unit
        ref: "internal/bus/server_test.go#TestNewerConnectionReplacesOlder, TestStrayClientDoesNotDisturbTheSession, TestSilentClientIsClosed, TestFirstFrameMustBeHello, TestBadFrameClosesTheSession, TestPeerByeAndErrEndTheSession, TestHelloTimeoutIsTwoSeconds"
        status: pass
    human_judgment: false
  - id: D4
    description: "Hub reconnects with exponential backoff 100 ms to 2 s reset by welcome, parses and encodes with sim_protocol.h, closes on any decode error (after err), stays non-blocking"
    requirement: "BUS-03"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestHubSession|TestFakePeer|TestFirmwareBus'"
        status: pass
    human_judgment: false
  - id: D5
    description: "Hub drops the connection and backs off when more than 64 KiB is pending for a stalled simulator"
    requirement: "BUS-03"
    verification: []
    human_judgment: true
    rationale: "Backstop truth. The code path (flush_ checks tx_.size() > kMaxTx) is simple but no test stalls a peer long enough to fill the kernel socket buffer and then 64 KiB of queue; a maintainer may want a one-off stress run"
  - id: D6
    description: "sil sim restart replaces the engine (fresh World, same bus path, engine_restarts + 1) while the firmware keeps its pid; the duty commanded before reappears from the firmware snapshot within 5 s and rpm restarts from 0; --gap 3s keeps the simulator away while the firmware API answers and status says bus_connected false, then the loop resyncs by itself"
    requirement: "CLI-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestSimRestartResync|TestSimGapAPIResponsive'"
        status: pass
    human_judgment: false
  - id: D7
    description: "sil fw restart stops the firmware (SIGTERM, SIGKILL after 3 s), starts the same program with the same prefs: new pid, last_exit.kind requested, restarts 1, readiness re-probed, the coasting rpm snapshot reaches the new firmware, duty restarts at 0"
    requirement: "CLI-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFwRestartResync"
        status: pass
    human_judgment: false
  - id: D8
    description: "Simulator and firmware restarts in either order, and a simulator restart issued during a firmware restart, converge to phase ready with one bus connection, no warnings, and a loop that still follows commands; status answers every 100 ms through a 2 s gap"
    requirement: "CLI-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestRestartEitherOrder|TestStatusDuringRestart'"
        status: pass
      - kind: unit
        ref: "internal/control/control_test.go#TestSimRestartRoute, TestFwRestartRoute"
        status: pass
    human_judgment: false

duration: 27min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 10: Sim-bus session and restart matrix Summary

**Both sides of the sim-bus now speak the full session (HELLO manifest, welcome, in/out snapshots, sync, replacement, backoff 100 ms to 2 s) and `sil sim restart [--gap]` / `sil fw restart` restart either half of the closed loop on demand, in any order, with status and the firmware API staying up.**

## Performance

- **Duration:** about 27 min
- **Completed:** 2026-11-09
- **Tasks:** 2 (both `auto`; TDD not enforced for this plan)
- **Files:** 3 created, 10 modified (plus this SUMMARY); 2 task commits
- **Verification:** `make check` (race, 112 C++ codec checks) and the full `make e2e` (28 tests, 236 s) pass. The ten plan-listed e2e tests all print `--- PASS`, plus the added `TestHubSession`.

## Accomplishments

- **Go session (BUS-01, BUS-02):** `bus.Server` frames lines with the 4096 cap, requires a hello within 2 s (through the engine clock's `Deadline`), refuses other versions with `err{"proto mismatch: sim 1, firmware N"}`, answers `welcome`, the engine's in-snapshot and `sync`, collects the `chan` manifest until the firmware's `sync`, and passes every `out` frame on in arrival order. A newer validated hello replaces the older session (`bye`, close, `Disconnected("replaced")` first). Any decode error answers `err` and closes, which finally enforces the 01-06 rule that the server used to skip bad lines.
- **Engine:** answers the hello from its own goroutine with one `in` frame per port in port order carrying the current rpm, checks the manifest against the configured ports (`missing channel port3.rpm (in)`, `unknown channel foo.bar (ignored)`, wrong direction or type) and turns session end reasons into warnings; a firmware with zero channels completes the session with eight warnings.
- **Firmware hub (BUS-02, BUS-03):** every line goes through `sim_protocol.h`; on connect it sends hello (proto, name, chans, t), one `chan` per child in registration order, the last value of every output, `sync`. It handles welcome, in, sync, err, bye; any parse error sends `err` and closes; reconnect delay 100, 200, 400, 800, 1600, 2000 ms reset by a welcome; more than 65536 pending bytes drops the connection.
- **Restarts (CLI-06, D-02, D-05):** `SimRestart` cancels the engine, waits for `Run` to return (socket unlinked), waits the gap, starts a fresh engine on the same path and waits for the firmware to resync (15 s); `FwRestart` stops the process group (3 s grace), starts the same program with the same environment and prefs from a locked-thread goroutine, re-probes readiness and waits for phase ready (30 s). One `opMu` serializes both with shutdown; `Status` never takes it.
- **Proof:** a real firmware against a scripted peer (`TestHubSession`: snapshot order, manifest, measured backoff gaps 163 / 247 / 444 / 855 / 1656 ms, err and bad-line handling, reset after welcome), plus the restart matrix on the whole bench.

## Task Commits

1. **Task 1: Session protocol on both sides** - `ad855c3` (feat)
2. **Task 2: `sil sim restart [--gap]` and `sil fw restart` with the restart-matrix e2e** - `0c6e39d` (feat)

**Plan metadata:** the `docs(01-10)` commit that adds this SUMMARY, STATE.md, ROADMAP.md and REQUIREMENTS.md follows.

## Files Created/Modified

- `internal/bus/server.go` (+ `server_test.go`) - session framing, hello deadline, replacement, held sends, Handler interface
- `internal/engine/engine.go` (+ `engine_test.go`) - hello/manifest/disconnect events, in-snapshot, warnings, Synced, stopped snapshot
- `components/sim_bus/sim_bus.h`, `sim_bus.cpp` - hub session on `sim_protocol.h`, backoff, TX cap
- `internal/supervisor/supervisor.go` - engine slot, fwProc, SimRestart, FwRestart, shutdown/opMu
- `internal/control/control.go` (+ `control_test.go`) - restart routes, request validation, client methods
- `internal/cli/cli.go` - `sim restart`, `fw restart`
- `e2e/restart_test.go` - the five restart tests; `e2e/io_test.go` - peer speaks the session, `TestHubSession`
- `protocol/PROTOCOL.md` - replacement at hello, ordering guarantees, backoff, end reasons

## Decisions Made

See `key-decisions`. The one that changes a stated plan sentence is replace-at-hello (below).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] A stopped engine kept publishing bus_connected true**
- **Found during:** Task 2 (`TestSimGapAPIResponsive`: status during the gap said bus_connected true)
- **Issue:** the supervisor keeps the stopped engine in its slot through the gap; its last published snapshot still said connected. My first edit of `Run` had silently not applied (indentation mismatch), so `stopped()` was defined but never called.
- **Fix:** `Run` now publishes a final snapshot (disconnected, not synced, proto 0) on both exit paths; `TestEngineStoppedSnapshotIsDisconnected` pins it.
- **Files modified:** internal/engine/engine.go, internal/engine/engine_test.go
- **Committed in:** 0c6e39d

**2. [Rule 1 - Bug] FwRestart reported phase starting for its whole wait**
- **Found during:** Task 2 (first run of `TestFwRestartResync`: `ready_timeout` with api_ready and bus_connected both true)
- **Issue:** the `restarting` flag (which hides the exit-to-start gap from the phase) was cleared by a defer, so the readiness poll inside FwRestart never saw `ready`.
- **Fix:** the flag is cleared as soon as the new process exists (still deferred for early failures).
- **Files modified:** internal/supervisor/supervisor.go
- **Committed in:** 0c6e39d

**3. [Rule 2 - Missing critical] Sends during the hello were not ordered against the snapshot**
- **Found during:** Task 1 (design of Handler.Hello)
- **Issue:** with "server sends welcome, these, sync" and an engine that keeps ticking, a steady in frame could reach the firmware before the welcome, or a stale snapshot value could land after a newer steady one, leaving the firmware on an old rpm until the next change.
- **Fix:** per-connection state in `Server`: after a hello `Send` is held; welcome, snapshot and sync are queued first, held frames follow. The engine sends no steady frames before it has processed the hello (its `connected` flag), so the held frames are all newer than the snapshot. `TestSessionOrderAndHeldSends` covers it.
- **Files modified:** internal/bus/server.go, internal/bus/server_test.go
- **Committed in:** ad855c3

### Plan wording not followed literally

- **Replacement happens at the newer connection's validated hello, not at accept.** The plan says "a newer connection replaces the older one ... events of the replaced connection are discarded after the newer hello", which both readings satisfy; replace-at-hello keeps a silent or garbage connection (a `socat` on bus.sock) from killing a healthy session. PROTOCOL.md says so now.
- **Pre-session failures:** the plan lists `Disconnected("bad_frame: <class>")` for a bad first line; it is delivered when no session is active (so a lone broken peer is visible) and only logged otherwise.
- **`bus.Listen(path, clk)`** gained the clock parameter (hello and write deadlines, welcome / sync time stamps); the engine is its only caller.
- **Fake peers:** `e2e/io_test.go`'s `ioPeer` now answers a hello with welcome and an empty in snapshot (opt out with `ioListenMode(..., true)`), and `TestFakePeer`'s wait for the post-outage reconnect went from 2 s to 4 s because the backoff cap is 2 s. `busPeer` in `harness_test.go` is unchanged: the hub accepts `in` frames without a welcome.
- **Added beyond `files_modified`:** `internal/control/control_test.go` (route and validation tests), `protocol/PROTOCOL.md`, and `TestHubSession` in `e2e/io_test.go`.
- **First connection has no out frames before sync** (it is made in `setup()`, before the outputs are written). The boot duties follow as steady-state frames. Reconnects replay them before sync. The plan's "the last value of every output as out frames" holds for every connect that has values.

---

**Total deviations:** 3 auto-fixed (2 bugs, 1 missing critical) plus 5 wording-level notes
**Impact on plan:** no scope creep; every plan truth is covered by a passing test except the TX-cap backstop.

## Issues Encountered

- None blocking. A python-based edit script silently skipped two replacements whose indentation did not match (the `Run` exit paths and the first fake-peer welcome edit); both were caught by tests (status during the gap, backoff gaps of 0 s) and redone. The first `FwRestart` run exposed the `restarting` flag bug above.

## Known Stubs

None.

## Threat Flags

None. T-01-20 is mitigated as planned (hello deadline, line cap, close on decode error, one active session, backoff, TX cap). T-01-22 is mitigated (gap capped at 30 s and range-checked in both the route and the supervisor, restarts serialized by `opMu`, status lock-free of it). T-01-21 stays accepted. No new modules (go.mod/go.sum unchanged).

## Needs Human

Nothing blocks. Optional:
- **D5 (TX cap):** the 64 KiB drop is only reasoned about, not exercised (backstop truth in the plan). A one-off stress run with a peer that stops reading would confirm it.
- **Sim time continuity:** `sim.time_s` continues across a simulator restart (the supervisor clock is monotonic) instead of resetting to 0; confirm that is the desired reading of "fresh World" for the Phase 2 scenario runner.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- **01-11 (auto-restart):** `fwProc` / `recordExit` are per process; an unexpected exit still sets `failure` and phase failed in `recordExit` and the main loop's `case ex := <-s.fwExits`, which is where auto-restart and the crash-loop guard hook in. `firmware.restarts` already counts requested restarts; `FwRestart` clears a previous failure, so it also works as the manual recovery.
- **01-12 (CLI polish):** `sim restart` and `fw restart` follow the `status` name resolution (`<name>` or `SIL_INSTANCE`) and the one-JSON-object contract; `--gap` uses kong's duration mapping.
- **01-13:** warnings in `status` now carry the manifest and session-end diagnostics.

## Self-Check: PASSED

- Files: server_test.go, control_test.go, restart_test.go created; all modified files present; commits `ad855c3` and `0c6e39d` in `git log`.
- `make check` and the full `make e2e` (28 tests) pass; acceptance greps (`sim_protocol.h`, `65536`, `2000` in sim_bus.cpp; `proto mismatch` in server.go; both restart routes in control.go; `bin/sil sim restart --help` and `fw restart --help`) match.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
