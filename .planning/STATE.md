---
gsd_state_version: "1.0"
milestone: v0.2.0
milestone_name: Real Fans
current_phase: 04
current_phase_name: Fan Model v2
status: executing
stopped_at: Completed 04-05-PLAN.md
last_updated: "2026-11-11T06:09:03.606Z"
last_activity: 2026-11-11
last_activity_desc: Phase 04 execution started
state_head: 4b85b5d6c1b474a7e81025f0ac0c10f92d5d0f15
progress:
  total_phases: 5
  completed_phases: 0
  total_plans: 10
  completed_plans: 5
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-11-10)

**Core value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.
**Current focus:** Phase 04 — Fan Model v2

## Current Position

Phase: 04 (Fan Model v2) — EXECUTING
Plan: 6 of 10
Status: Ready to execute
Last activity: 2026-11-11 — Phase 04 execution started

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**
- Total plans completed: 36
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 2 | 8 | - | - |
| 1 | 13 | - | - |
| 3 | 12 | - | - |
| 03.1 | 3 | - | - |

**Recent Trend:**
- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 1 P01 | 12 min | 2 tasks | 14 files |
| Phase 01 P02 | 20min | 2 tasks | 12 files |
| Phase 01 P03 | 10min | 2 tasks | 11 files |
| Phase 1 P04 | 3 min | 2 tasks | 3 files |
| Phase 01 P05 | 6 min | 2 tasks | 6 files |
| Phase 01 P06 | 8min | 2 tasks | 7 files |
| Phase 01 P07 | 15min | 2 tasks | 10 files |
| Phase 01 P08 | 20min | 2 tasks | 15 files |
| Phase 01 P09 | 15min | 2 tasks | 11 files |
| Phase 01 P10 | 27 min | 2 tasks | 13 files |
| Phase 01 P11 | 13 min | 2 tasks | 7 files |
| Phase 1 P12 | 25 min | 2 tasks | 5 files |
| Phase 1 P13 | 15min | 2 tasks | 4 files |
| Phase 02 P01 | 16 min | 2 tasks | 14 files |
| Phase 02 P02 | 25 min | 2 tasks | 13 files |
| Phase 02 P03 | 15 min | 2 tasks | 14 files |
| Phase 02 P04 | 25min | 2 tasks | 5 files |
| Phase 02 P05 | 21min | 2 tasks | 13 files |
| Phase 02 P06 | 28 min | 2 tasks | 17 files |
| Phase 02 P07 | 26 min | 2 tasks | 9 files |
| Phase 02 P08 | 25 min | 2 tasks | 9 files |
| Phase 3 P01 | 40 min | 2 tasks | 16 files |
| Phase 3 P02 | 25 min | 2 tasks | 8 files |
| Phase 03 P03 | 40 min | 2 tasks | 6 files |
| Phase 03 P04 | 35 min | 2 tasks | 9 files |
| Phase 3 P05 | 17 min | 2 tasks | 15 files |
| Phase 3 P06 | 8 min | 2 tasks | 9 files |
| Phase 3 P07 | 15 min | 3 tasks | 17 files |
| Phase 3 P08 | 10 min | 2 tasks | 13 files |
| Phase 3 P09 | 10 min | 2 tasks | 6 files |
| Phase 03 P10 | 15 min | 2 tasks | 12 files |
| Phase 03 P11 | 13min | 2 tasks | 13 files |
| Phase 03 P12 | 19 min | 2 tasks | 6 files |
| Phase 03.1 P01 | 7 min | 3 tasks | 7 files |
| Phase 03.1 P02 | 6 min | 2 tasks | 3 files |
| Phase 03.1 P03 | 4 min | 2 tasks | 8 files |
| Phase 04 P01 | 20 min | 2 tasks | 12 files |
| Phase 04 P02 | 3 min | 2 tasks | 8 files |
| Phase 04 P03 | 25 min | 2 tasks | 11 files |
| Phase 04 P04 | 10 min | 2 tasks | 8 files |
| Phase 04 P05 | 5 min | 2 tasks | 9 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Init]: Static firmware binary runs on the host (`build_flags: [-static]`); run-in-image is the fallback
- [Init]: Sim-bus is NDJSON over a Unix socket; one `sim_bus` component dir with platforms; RPM 1 s mean over the wire
- [Init]: API encryption on with a stable per-instance key; isolated, parallel-safe instances from day one
- [Init]: HIL contract entity names; status LEDs internal (not in HA), visible in CLI/UI only
- [Roadmap]: Contract tests (TEST-01/02) live in Phase 2 next to the IO surface they guard, not in a separate final phase
- [Phase 1]: ESPHOME_NOGITIGNORE=1 in the build container: read-only repo mount otherwise breaks esphome compile (write_gitignore)
- [Phase 1]: fwbuild.Result.Cached stays false until 01-08 builds the hash-skip pipeline
- [Phase 01]: 01-02: bus.Server.Send is queue-based and non-blocking (1024 frames) so a stalled firmware cannot freeze the engine goroutine
- [Phase 01]: 01-02: Frame.GetNum re-expresses the wire float32 as its shortest decimal so exact duty comparisons hold; GetF32 keeps the raw float32
- [Phase 01]: 01-02: wall-clock lint bans context timers and stdlib timeout fields too; infrastructure timeouts go through clock.WithTimeout or clock.Deadline
- [Phase 01]: 01-03 D-02: engine is a goroutine in sil serve with fresh state per engine.New; engine.Run owns bus.Listen and socket unlink; supervisor owns lifecycle of engine slot and firmware process group; a sim restart (01-10) keeps the firmware PID
- [Phase 01]: 01-03: control API only on <run>/api.sock (0600); run-dir parent must be a real 0700 dir owned by the user (run_dir_unsafe otherwise)
- [Phase 1]: 01-04: clock.Manual delivers tick/timer values by blocking unbuffered sends; Timer.Stop is false exactly when Advance already took the timer
- [Phase 01]: 01-05: start_duty == min_duty accepted as a zero-width hysteresis band; only start_duty < min_duty rejected
- [Phase 01]: 01-05: missing fan.toml key reports <file>: missing required key <key> (no line); unknown keys report file:line:col; non-finite numbers rejected; NaN duty treated as 0 in model.Step
- [Phase 01]: 01-06: sim-bus line cap is 4096 bytes including the newline (4095 content); non-printable string bytes are encoded as '?' by both encoders; decode error closes the connection (enforced in 01-07/01-10)
- [Phase 01]: RPM sensor is a PollingComponent (1s) by recorded exception; temperature/humidity/buttons stay push-only
- [Phase 01]: sim_bus hub path keeps /tmp/sil.sock default; SIL_BUS always overrides at runtime
- [Phase 01]: Hub drops the connection on any line over 4096 bytes, complete or unfinished
- [Phase 01]: ESPHome republishes an unchanged sensor mean every update_interval, so no force_update is needed for IO-02
- [Phase 01]: 01-08: toolcache has two parts, pio_components and pio_http (PlatformIO registry responses with expiry frozen to 2100); a toolcache with only archives is not a seed because the compile resolves libraries through a 1 h registry cache
- [Phase 01]: 01-08: offline compile failures are named build_failed errors (delete the toolcache to refetch), never a silent online retry; config/generate stages always run --network none
- [Phase 01]: 01-08: mount root is RepoRoot when it contains the config, else the YAML's git top-level unless that is / or HOME, else the YAML dir
- [Phase 01]: [Phase 01-09]: Instance readiness is a real Noise handshake with the instance key plus name/MAC match; plaintext or rejected answers fail at once, a taken sticky or explicit API port is port_in_use and never moved
- [Phase 01]: [Phase 01-09]: sil up checks liveness before LoadOrCreate and runs pure checks (names, socket length, config, fan, run-dir parent) before taking the instance flock, so refused ups leave no state
- [Phase 01]: 01-10: a connection becomes the active sim-bus session at its first valid hello, so stray clients never disturb a running session; a newer hello replaces the older (bye, close)
- [Phase 01]: 01-10: Server.Send is held between a hello and the queued snapshot so welcome and the in-snapshot always precede steady frames; the hub's reconnect backoff is 100 ms to 2 s reset by welcome, TX cap 64 KiB
- [Phase 01]: 01-10: sim restart keeps the monotonic sim clock (only the World is fresh); restarts, fw restart and shutdown serialize on one supervisor mutex while status never takes it
- [Phase 01]: 01-11: unrequested firmware exits restart via a goroutine under opMu that gives up if the process was replaced or the instance is stopping; restarts counts started processes
- [Phase 01]: 01-11: sil down fallback is SIGTERM (5 s) then SIGKILL of the run.json pid, gated by /proc cmdline; instance.ReadRunFile now reports not_running for a dead/foreign pid (LoadRunFile is the raw read)
- [Phase 01]: 01-11: last_exit.signal uses POSIX names via SignalName; firmware_crash_loop keeps exit class 1 in sil up through lifecycle.userClass
- [Phase 1]: 01-12: sil status exits 0 whenever the control API answers (phase failed is data); --help goes to stderr; client dials its own run dir api.sock and refuses a non-private runtime base
- [Phase 1]: apperr.Registry is the single list of 51 error codes with exit class 1 or 2; an AST scan of internal/ and cmd/ fails make check on unregistered, reclassified, dual-class, non-literal or stale codes
- [Phase 02]: Button edges are stamped with clock.Now() (not the last tick) so t_up_us - t_down_us >= hold_ms*1000 holds exactly
- [Phase 02]: Press release goes to the engine in the slot at release time with an uncancellable context; ErrStopped counts as released; a sim restart releases a held button (A-02-01)
- [Phase 02]: ShortPress 200 ms / LongPress 1500 ms / MaxHold 60 s live in internal/control; HTTP API requires explicit hold_ms; --instance/-i is the selector for Phase 2 verbs with positional args
- [Phase 02]: 02-02: LED state is per bus session (hello clears, disconnect keeps the last frame); px before the manifest accepted then checked against n
- [Phase 02]: 02-02: segment names come only from the firmware's partition lights (light.py codegen, segment_labels override); final-validate rejects unknown label keys and invalid or duplicate labels
- [Phase 02]: 02-03: each env value has its own 10 s re-send deadline; inputs live in the supervisor under inMu and seed every engine (sim restart keeps them, a set while the bus is down is accepted)
- [Phase 02]: 02-03: sil set numbers are plain decimals only (no hex, separators, Inf/NaN, overflow); ranges temperature -40..125, humidity 0..100 (HDC1080, assumption A1 open)
- [Phase 02]: 02-04: duplicate-channel message names a sim light by its user-written id, not the generated output_id; default_label() is the single partition-to-label mapping
- [Phase 02]: 02-05: per-port fan list is held by the supervisor and is the single source of every engine's ports; DefaultFan is only the default of sil up --fan
- [Phase 02]: 02-05: engine.Apply publishes its snapshot before replying, so a status read after sil set returned never shows the old fan
- [Phase 02]: 02-06: watch joins the engine through SnapshotSync (snapshot built on the engine goroutine, stamped with the broker seq) so no change is lost or repeated at the snapshot/stream join
- [Phase 02]: 02-06: log lines are cleaned whole (ANSI strip, key redaction) before the 64 KiB cut; lines whose beginning was not read are never served
- [Phase 02]: 02-07: test-contract depends on lock-check so dependencies are never installed without the hash diff against the product HIL lock
- [Phase 02]: 02-07: contract state root persists in ~/.cache/sil-contract/state (0700); ct-main/ct-second reserved; warm contract session 22 s (budget 60 s)
- [Phase 02]: 02-08: api.lan_host from a default-route UDP connect to 192.0.2.1 (nothing sent), null when unknown; sil up hint falls back to 127.0.0.1
- [Phase 02]: 02-08: examples/rev33.yaml is the default sil up config; HA-01/HA-02 stay pending until the live Home Assistant UAT (only localhost and aioesphomeapi were exercised)
- [Phase 3]: 03-01: web port allocation excludes the API port decided in the same LoadOrCreate call; explicit --api-port is re-bound only when it changes
- [Phase 3]: 03-01: redacting wrapper also redacts SimRestart/FwRestart statuses although D-19 refuses those routes (defence in depth)
- [Phase 3]: 03-02: cdptest.Eval wraps its argument as an async expression (replMode returned unawaited promises as {}); Eval takes one expression
- [Phase 3]: 03-02: linkState tokens (connecting/live/retrying) live in app.js, display strings in view.js; app.js clears #message on every snapshot
- [Phase 03]: 03-03: web listener served as web.Serve(web.Limit(...), web.LimitBody(...)); header deadline relies on net/http clearing it only after headers with keep-alives off; no per-IP cap (accepted residual)
- [Phase 03]: 03-03: WS reap releases Watch and browser slots before the close handshake; browsers capped at 8 of 16 with the existing too_many_streams code
- [Phase 03]: 03-04: control.Operations is the identity of the control API; the listener policy (socket all rows, LAN only LAN rows, others 403 operation_not_allowed) follows from the row's LAN flag
- [Phase 03]: 03-04: logs (tail and follow) is socket-only on the web listener (D-20) and enforced by the table
- [Phase 3]: 03-05: web.Guard sits inside LimitBody and outside NewHandler; Host allowlist, then security headers, then CrossOriginProtection; every non-GET row of control.Operations is covered by TestCSRFCoversEveryMutatingRoute and a GET row needs a reviewed read-only entry
- [Phase 3]: 03-05: TestWSPingReaps flake was a test race (Manual clock jumped during an in-flight coder/websocket write, which closes the connection hard); fixed in the test, ws.go unchanged
- [Phase 3]: 03-06: LED lens colours reach the page only as CSSOM custom properties (--lens, --glow) with the look in app.css, so the page runs under style-src 'self'
- [Phase 3]: 03-06: the connecting LED placeholder lens carries no data-src/data-fmt; a placeholder for values is not a value
- [Phase 3]: 03-06: header cells have fixed px widths (instance 168, SIM TIME 144, TIME FACTOR 112, FIRMWARE 152, LINK 240); new header cells must keep a fixed width
- [Phase 3]: Samples share the broker seq: a default stream's seq is strictly increasing but not consecutive — Keeps history.seq consistent with every stream; e2e TestWatchStream relaxed accordingly
- [Phase 3]: History window 0 on the Go side means the whole ring; the handler passes 300 on so Control.History sees 0 < W <= 300 — The client omits window_s when 0
- [Phase 3]: Max-hold auto-release runs inside the tick's inTick region — Release is stamped with the tick time and happens at most one tick (10 ms) after the deadline
- [Phase 3]: 03-08: non-JSON API errors show the trimmed response body or nothing (plan D-17 supersedes UI-SPEC UIC-29's HTTP status line); a set success shows the server's applied value; dirty or focused env fields ignore WS values and take the server value on blur
- [Phase 3]: 03-09: LICENSE.uplot is listed in the VENDOR.md hash table, so TestVendorHashes covers the license text; vendor/ is skipped by TestNoExternalRefs and covered by hashes
- [Phase 03]: 03-10: the history request starts after the snapshot so its seq is at least the snapshot's and no sample falls between history and stream; held samples with seq above the history's are replayed, a reconnect replaces the rings
- [Phase 03]: 03-10: an unknown fan leaves the RPM scale unknown and hides the RPM trace rather than guess; the restart-gap null point at the new sample's own x breaks the line in real Chrome; style-src 'self' suffices for uPlot (no CSP fallback)
- [Phase 03]: 03-11: a USR up is chained behind the answer to its down; a hold open when the link drops is released best-effort; the stale class applies only after the page has been LIVE once
- [Phase 03]: 03-11: the page's own releasePointerCapture is tracked per pointer id (ownLoss); Chrome 157 fires no lostpointercapture for it, so the guard is tested with a synthetic event
- [Phase 3]: 03-12: parity_real compares RPM within 1 rpm only after every RPM has settled (a freshly swapped fan is still ramping)
- [Phase 3]: 03-12: the LED group is asserted by natural height (108 px) against the strip content box (110 px), not the stretched group box
- [Phase 03.1]: Engine.stopped() releases held buttons with reason engine_stopped, so restart, sil down and engine failure share one fix (A-03.1-01/02)
- [Phase 03.1]: Missed-release page path kept as a user-approved known limit (2026-11-10), pinned by TestUSRSimRestartWithoutReleaseSendsAnUp
- [Phase 03.1]: Set rolls back element-wise (copy(s.fans, prevFans)) so the s.fans slice header is written only in Serve (F1)
- [Phase 03.1]: Set answers stopping with a rollback once a shutdown is requested or cancels its Apply; a shutdown is terminal so the rollback cannot desync a later engine (F2, F6)
- [Phase 03.1]: Concurrency review kept as 03.1-CONCURRENCY-REVIEW.md; F3 documented, F5 and F7 left unchanged
- [Phase 03.1]: 03.1-03: hub TX cap tested on host via stub ESPHome headers (tests/cpp/stubs); SimChild::on_value unnamed param keeps -Werror
- [Roadmap v0.2.0]: Five phases 4-8, continuing numbering after 3.1: Fan Model v2 → Fault Injection ∥ fan-analyze Offline Core → Measuring Firmware and Board Link → Arctic P12 Calibration (granularity coarse; five kept because the user-set research order defines five distinct slices)
- [Roadmap v0.2.0]: No separate spike phase; the MEAS-01 hardware spikes open Phase 7 (user decision)
- [Roadmap v0.2.0]: ANLZ-07 (synthetic-fan recovery) maps to Phase 7, not 6: its host-firmware leg needs the Go native API client (MEAS-05). The fake-board recovery already proves ANLZ-04 in Phase 6. "Before any real hardware is used" is read as "before any real-fan recording is fitted"; the MEAS-01 spikes characterise the instrument only
- [Roadmap v0.2.0]: Fault lifecycle per FAULT-09 is decided: cleared by `sil sim restart` and `sil down`, kept across firmware restart, cleared with an event on fan swap; unplug is a fault, replug clears it
- [Roadmap v0.2.0]: Done criterion is the Arctic P12 only, verified on a held-out recording (CAL-02); Noctua NF-A12x25 stays `assumed` (CAL-03, v0.3)
- [Roadmap v0.2.0]: UI hint on Phase 4 (airflow/power readouts on the page, MODEL-11) and Phase 5 (fault controls and scope markers, FAULT-10/11); Phases 6-8 explicitly `UI hint: no` (`report.html` is a generated offline report, not the page)
- [Roadmap v0.2.0]: Hardware/human-gated: Phase 7 (board on the bench for spikes, first flash, fail-safe checks) and Phase 8 (bench setup, preflight, run with repeatability pass, report review). Deeper research: Phase 6 (identifiability in the count domain, fitter robustness, estimator/tolerance) and Phase 7 (instrument design, PCNT limits, device-stamped delivery, api.proto IDs)
- [Phase 04]: 04-01: direction (LastDuty/Falling) is tracked in State only for fans with a separate down curve, so legacy State stays bit-identical to v0.1.0
- [Phase 04]: 04-01: schema 2 loader errors use a table-aware lineOf (duty/rpm repeat across tables); provenance entry typos reported before missing entries
- [Phase 04]: 04-01: A1 sticky direction without deadband reviewed and accepted for Phase 4; carried to Phase 8 measured sweeps
- [Phase 04]: 04-02: start boundary is at or above (duty >= start.duty, v0.1.0 operator); a delay expiring exactly on a tick leaves rpm 0 until the next tick
- [Phase 04]: 04-02: coast_tau_s applies only to a non-running segment; a running fan slowing down uses tau_down_s; snap stays at segment end, only the integral uses the analytic crossing time
- [Phase 04]: 04-03: locate scanner matches full dotted paths (bare, quoted, dotted-in-table, multi-line arrays); replaces lineOf/entryLine — duty and rpm repeat across tables; one mechanism covers provenance keys too
- [Phase 04]: 04-03: [measured] optional table of five strings; only measured.tolerance carries provenance (A7 accepted, flagged for Phase 6/8) — MODEL-08 names measured tables; MODEL-12 needs provenance on every fan value
- [Phase 04]: 04-03: v1 files load with Params.Schema 1; schema >= 3 says it needs a newer sil — explicit schema = 1 is accepted; sil fans reports schema 1 with null provenance
- [Phase 04]: 04-04: pulse count uses floor(phase + 1e-9) so on-grid fans read the same on every tick pattern
- [Phase 04]: 04-04: checkGlobalRand flags every non-constructor, non-type name of math/rand(/v2) under internal/model, not only calls
- [Phase 04]: 04-04: EDGE-MODEL-09 accepted as bounded (first Frame after a bus gap is the true mean over the gap); A11 stays open for Phase 7/8
- [Phase 04]: 04-05: noise seed travels in spec.json (sil up) and is copied into run.json and status sim.seed by the supervisor; default lifecycle.DefaultSeed 0x5EED, cap 2^53-1 (A-SEED resolved)

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 1] RESOLVED: Go toolchain installed; static encrypted build with hermetic toolcache (01-08); Noise ready check (01-09)
- [Phase 2] RESOLVED in 02-02: `partition` + `pulse` (and `addressable_rainbow`) on the sim addressable light verified at runtime; boot pulse 12 frames/s, rainbow 11 to 15 frames/s, idle 0 frames in 5 s
- [Phase 2] Live HA behaviour with the stable-MAC scheme is unverified (aioesphomeapi identity proxy passes; no live HA run) — see Needs Human
- [Phase 2] `TestSupervisorRebootExit` flake fixed in 8542d9d (test race); watch for recurrence
- [Later] Static linking with real product modules (DNS, `json:`) is unverified; run-in-image fallback stays available
- [Later] Host `select()` timeout lives in `core/wake/wake_host.cpp::wakeable_delay` in 2026.10.1, not `core/application.cpp` as PROJECT.md says (affects the time-factor milestone)
- 01-05 A1 (non-blocking): Arctic start_duty 0.10, tau_up 1.0 s, tau_down 2.5 s are ASSUMED; closed by measurement in Phase 8 (CAL-01/CAL-02)
- [Phase 4] Tach accumulator (spike S7, no hardware): if firmware-side arrival jitter into the 1 s time-weighted mean blurs the 30 rpm steps, switch to analytic quantisation (protocol stays v1 either way)
- [Phase 6] Fit identifiability of tau and start delay in the count domain is MEDIUM confidence; Nelder-Mead may need the LM fallback on real data
- [Phase 7] ESP32-S2 PCNT unit count and ISR latency unverified; Rev 3.3 schematic (tach pull-up, supply) unchecked; 100 ms device-stamped sample publish rate over the API unconfirmed (fallback: host-stamped `total` with Wi-Fi jitter)
- [Phase 7] Native API: confirm message IDs/fields from the pinned 2026.10.1 `api.proto` and that no password/`ConnectRequest` step is needed for an encrypted API
- [Phase 8] Arctic P12 SKU mismatch: repo cites ACFAN00120A, research fetched ACFAN00170A; record the real SKU at bench setup. PST daisy-chain topology: measure a single fan
- [Phase 8] Tolerance numbers (`floor`, `k`) are the maintainer's call, frozen after the repeatability pass and before the held-out recording
- [Phase 8] Real fan may show behaviour model v2 cannot express (rpm-dependent tau, low-duty hunting, PWM-frequency effects); budget one model iteration as a decimal phase

### Roadmap Evolution

- Phase 3.1 inserted after Phase 3: Address tech debt: sim-restart button release, Set/SimRestart re-review, TX-cap test (URGENT)
- v0.2.0 Real Fans roadmap created 2026-11-10: Phases 4-8 (Fan Model v2, Fault Injection, fan-analyze Offline Core, Measuring Firmware and Board Link, Arctic P12 Calibration); 32/32 requirements mapped

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| deferred_items | 03/deferred-items.md: TestWSPingReaps flake (found in 03-04, resolved in 03-05) | acknowledged | 2026-11-10 | v0.1.0 |
| verification | Phases 1, 2, 3, 03.1: VERIFICATION.md stale (later phases and review fixes touched covered files) | override (UATs passed) | 2026-11-10 | v0.1.0 |

## Session Continuity

Last session: 2026-11-11T06:09:03.576Z
Stopped at: Completed 04-05-PLAN.md
Resume file: None

## Needs Human

Items recorded during the unattended `/gsd-autonomous --from 1 --to 3` run (2026-11-08/09). Check each in the morning.

| Phase | Item | Where | Status |
|-------|------|-------|--------|
| 1 | Decision taken autonomously: RPM sensor is a `PollingComponent` (exception to the push-only sensor rule) because IO-02 requires a 1 s mean at `update_interval`. Plan-checker blocker resolved by recording this decision after 3 revision iterations. Confirm or revert. | PROJECT.md Key Decisions; .claude/CLAUDE.md + research/STACK.md rows; 01-07-PLAN.md | needs review |
| 1 | Go module provenance (`flynn/noise`, `go.yaml.in/yaml/v3`): human checkpoint replaced by automated proxy-origin + sum.golang.org checks | 01-02-PLAN.md backstop truth | needs review |
| 1 | Assumptions taken unattended: A1 Arctic fan constants (`start_duty` 0.10, `tau_up` 1.0 s, `tau_down` 2.5 s); A3 control API on a Unix socket only in Phase 1 (TCP in Phase 3); A4 RPM sensor stops publishing on bus disconnect; A2/A5/A10 per plans | 01-RESEARCH.md, plan frontmatter | needs review |
| 1 | Flagged edge assumptions left unresolved (unclassified probe rows): EDGE-MODEL-04, EDGE-MODEL-02, EDGE-BUS-04, EDGE-IO-02, EDGE-BUILD-02, EDGE-BUILD-07; 6 descriptor-less prohibitions (flagged-unverified) | plan must_haves | needs review |
| 1 | `sil sim restart` builds a fresh World, so RPM restarts from 0 (D-02) rather than resuming its pre-restart trajectory (Research Pitfall 10). Confirm that is intended under criterion 3 | 01-10-PLAN.md | needs review |
| 1 | Control server drops `ReadHeaderTimeout` to satisfy the wall-clock lint; slow-client risk accepted on the 0600 `api.sock` (T-01-30) | 01-03-PLAN.md | needs review |
| 1 | UI gate matched "component" in Phase 1 text (false positive, no frontend); UI-SPEC skipped for Phase 1 | — | info |
| 1 | Verification `human_needed` (5/5 success criteria verified automatically; full e2e 34/34 green). 6 UAT items pending — run `/gsd-verify-work 1` | 01-VERIFICATION.md, 01-UAT.md | needs human |
| 1 | Review fixes flagged for human verification: a192260/5fbe66f (`compile_failed` classification), 6dbff8e (run.json before startup, SIGHUP), 1c8519a, cab20ad (not re-reviewed: 3-iteration cap). Open Info: IN-01 SIGHUP defeats `nohup sil up`; IN-02 broad `^FAILED: ` marker | 01-REVIEW-DISPOSITION.md | needs review |
| 1 | Security: 31/31 threats closed after one fix round (T-01-17 `sil down` runtime-base gap fixed in ce3bed4). SECURITY.md still `status: draft`, sign-off pending. Residual notes: hub `path:` default `/tmp/sil.sock`, first-build network fetch integrity, mount-root check does not resolve symlinks | 01-SECURITY.md | needs review |
| 1 | `sim.time_s` keeps running across a simulator restart (not reset). `TestRestartEitherOrder` flaked once in 01-12, green since | 01-10/01-12 SUMMARY | info |

## Deferred Verification

| Phase | State | Resume |
|-------|-------|--------|
| 1 | verification_deferred_human | /gsd-verify-work 1 |
| 3 | verification_deferred_human | /gsd-verify-work 3 |

### Needs Human — Phase 03.1 (2026-11-10)

| Phase | Item | Where | Status |
|-------|------|-------|--------|
| 03.1 | F7 design question: a `Set` in a restart gap while shutdown has already started answers success (F2 now answers `stopping` once the gap ends). Left unchanged; confirm | 03.1-CONCURRENCY-REVIEW.md | needs review |
| 03.1 | Code review 0 critical / 2 warning / 4 info, all open: WR-01 `Manual.BlockUntil(1)` can be satisfied by the old engine's ticker (latent flake); WR-02 late cleanup leaks goroutine on early failure; IN-01..04 doc/test hygiene | 03.1-REVIEW.md, 03.1-REVIEW-DISPOSITION.md | resolved 2026-11-10: WR-02 + IN-01..04 fixed, WR-01 skipped (premise wrong); see 03.1-REVIEW-FIX.md |

### Needs Human — Phase 3 (2026-11-10)

| Phase | Item | Where | Status |
|-------|------|-------|--------|
| 3 | UAT deferred by user: 10 hands-on items (phone on LAN, USR2 long press + HA, touch slide-off on a real phone, temperature to HA, sim restart on phone, visual character, 10 min soak, behind-real-time thresholds A-03-05/06, `--listen 127.0.0.1`, timed-out button down semantics). All automated checks green (9/9 must-haves) | 03-UAT.md, 03-VERIFICATION.md | needs human |
| 3 | Review-fix semantics to confirm: timed-out down (3 s) counts as never registered (bfbb808); client per-cap down/up chain (4bd7507). 6 info findings open (IN-01/02/03/05/06/07) | 03-REVIEW-DISPOSITION.md | needs review |
| 3 | User decisions taken during planning: D-18 api.key redacted on TCP, D-19 lifecycle socket-only, D-20 logs socket-only; plans split to ≤14 files. Per-IP connection cap not added (NAT households) — accepted residual | 03-CONTEXT.md, 03-03-SUMMARY.md | info |

### Needs Human — Phase 2 (planning, 2026-11-09)

| Phase | Item | Where | Status |
|-------|------|-------|--------|
| 2 | Plan check hit the 3-iteration cap with 0 blockers / 3 warnings (missing named tests for: non-pulse LED effect dedupe in 02-02; seg > 2048 bytes in 02-04; `sil watch` SIGINT exit 0 / dropped stream exit 2 / too_many_streams in 02-06). Forced proceed; executors told to add the tests | 02-02/02-04/02-06 PLAN | needs review |
| 2 | Autonomous decisions D-17 (example ignores `sil`'s own readiness probe for the board LED) and D-18 (TEST-01 drives/observes only via `sil`; temperature-arrival check moved to TEST-02), plus a plan-size acceptance note (10–14 files) | 02-CONTEXT.md | needs review |
| 2 | Unattended choices: HDC1080 bounds -40..125 °C / 0..100 % (A1); Noctua NF-A12x25 constants ASSUMED (A2); press presets in `control.ShortPress/LongPress`, HTTP API requires `hold_ms`; RPM events sampled once per sim second; logs default firmware/last 100; contract state in `~/.cache/sil-contract/state` with `ct-main`/`ct-second`; `lan_host` by default-route detection (A8) | 02-RESEARCH.md, plans | needs review |
| 2 | Python test deps (aioesphomeapi 46.12.0, pytest 9.2.0, pytest-timeout 2.4.0) flagged SUS by package check; human review replaced by `make lock-check` hash diff against ../wifi-fancontroller/hil/uv.lock | 02-07-PLAN | needs review |
| 2 | Live Home Assistant UAT (7-step checklist, HA on another machine) — automated proxy is aioesphomeapi only | 02-07/02-08 backstop truths | needs human |
| 2 | Phase 2 verification accepted by user on automated evidence (4/5; criterion 4 HA-01/HA-02 proxied only). Live HA UAT still open — run `/gsd-verify-work 2` to close | 02-VERIFICATION.md, 02-UAT.md | needs human |
| 2 | Review-fix concurrency changes (8542d9d, b976a6c, da1a9ca, e4f1046) re-reviewed in Phase 03.1: F1/F2(IN-09)/F6 fixed with race tests. Remaining IN-01..IN-08 open (IN-07 missing `<algorithm>` include is a latent compile risk) | 03.1-CONCURRENCY-REVIEW.md, 02-REVIEW-DISPOSITION.md | resolved in 03.1 (IN-01..08 still open) |

## Operator Next Steps

- Phase 4 (Fan Model v2) carries `UI hint: yes` (airflow/power readouts on the page): run `/gsd-ui-phase 4` if a design contract is wanted, then `/gsd-discuss-phase 4` or `/gsd-plan-phase 4`
- Phases 5 and 6 can run in parallel once Phase 4 is done; Phase 5 needs `/gsd-ui-phase 5` (fault controls, scope markers)
- Phases 7 and 8 need the Rev 3.3 board on the bench and the maintainer present at the checkpoints
