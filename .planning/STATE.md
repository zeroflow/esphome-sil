---
gsd_state_version: "1.0"
current_phase: 2
current_phase_name: Full Rev 3.3 Bench
status: executing
stopped_at: Completed 02-07-PLAN.md
last_updated: "2026-11-09T08:22:38.897Z"
last_activity: 2026-11-09
last_activity_desc: Phase 2 execution started
state_head: 03d98ae9e4e6c85e8261b44f858053832317909d
progress:
  total_phases: 3
  completed_phases: 0
  total_plans: 21
  completed_plans: 20
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-11-08)

**Core value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.
**Current focus:** Phase 2 — Full Rev 3.3 Bench

## Current Position

Phase: 2 (Full Rev 3.3 Bench) — EXECUTING
Plan: 2 of 8
Status: Ready to execute
Last activity: 2026-11-09 — Phase 2 execution started

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**
- Total plans completed: 0
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

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

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 1] Go 1.27.3 is not yet installed on this host (checksum in research/STACK.md)
- [Phase 1] Spike needed: static build with `api: encryption:` (Noise libs fetched at build time, hermetic build cache), same-path bind mounts for local external components, incremental rebuild timing
- [Phase 1] Ready check needs a real encrypted API handshake from the Go side (Noise); pick the approach during planning
- [Phase 2] RESOLVED in 02-02: `partition` + `pulse` (and `addressable_rainbow`) on the sim addressable light verified at runtime; boot pulse 12 frames/s, rainbow 11 to 15 frames/s, idle 0 frames in 5 s
- [Phase 2] Live HA behaviour with the stable-MAC scheme is unverified (research read HA source, no live run)
- [Later] Static linking with real product modules (DNS, `json:`) is unverified; run-in-image fallback stays available
- [Later] Host `select()` timeout lives in `core/wake/wake_host.cpp::wakeable_delay` in 2026.10.1, not `core/application.cpp` as PROJECT.md says (affects the time-factor milestone)
- 01-05 A1 (non-blocking): Arctic start_duty 0.10, tau_up 1.0 s, tau_down 2.5 s are ASSUMED; maintainer to confirm or measure (fans/arctic-p12-pwm-pst.toml)

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-11-09T08:22:38.851Z
Stopped at: Completed 02-07-PLAN.md
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

### Needs Human — Phase 2 (planning, 2026-11-09)

| Phase | Item | Where | Status |
|-------|------|-------|--------|
| 2 | Plan check hit the 3-iteration cap with 0 blockers / 3 warnings (missing named tests for: non-pulse LED effect dedupe in 02-02; seg > 2048 bytes in 02-04; `sil watch` SIGINT exit 0 / dropped stream exit 2 / too_many_streams in 02-06). Forced proceed; executors told to add the tests | 02-02/02-04/02-06 PLAN | needs review |
| 2 | Autonomous decisions D-17 (example ignores `sil`'s own readiness probe for the board LED) and D-18 (TEST-01 drives/observes only via `sil`; temperature-arrival check moved to TEST-02), plus a plan-size acceptance note (10–14 files) | 02-CONTEXT.md | needs review |
| 2 | Unattended choices: HDC1080 bounds -40..125 °C / 0..100 % (A1); Noctua NF-A12x25 constants ASSUMED (A2); press presets in `control.ShortPress/LongPress`, HTTP API requires `hold_ms`; RPM events sampled once per sim second; logs default firmware/last 100; contract state in `~/.cache/sil-contract/state` with `ct-main`/`ct-second`; `lan_host` by default-route detection (A8) | 02-RESEARCH.md, plans | needs review |
| 2 | Python test deps (aioesphomeapi 46.12.0, pytest 9.2.0, pytest-timeout 2.4.0) flagged SUS by package check; human review replaced by `make lock-check` hash diff against ../wifi-fancontroller/hil/uv.lock | 02-07-PLAN | needs review |
| 2 | Live Home Assistant UAT (7-step checklist, HA on another machine) — automated proxy is aioesphomeapi only | 02-07/02-08 backstop truths | needs human |
