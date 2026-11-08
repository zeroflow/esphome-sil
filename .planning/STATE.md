---
gsd_state_version: "1.0"
current_phase: 1
current_phase_name: Closed-Loop Fan Skeleton
status: planning
stopped_at: Phase 3 UI-SPEC approved
last_updated: "2026-11-08T22:45:01.052Z"
last_activity: 2026-11-08
last_activity_desc: Roadmap created (3 phases, 40/40 v1 requirements mapped)
state_head: 2db7f0a95d611dcd9fe858138c3e1027bd868445
progress:
  total_phases: 3
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-11-08)

**Core value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.
**Current focus:** Phase 1 - Closed-Loop Fan Skeleton

## Current Position

Phase: 1 of 3 (Closed-Loop Fan Skeleton)
Plan: 0 of TBD in current phase
Status: Ready to plan
Last activity: 2026-11-08 — Roadmap created (3 phases, 40/40 v1 requirements mapped)

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

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Init]: Static firmware binary runs on the host (`build_flags: [-static]`); run-in-image is the fallback
- [Init]: Sim-bus is NDJSON over a Unix socket; one `sim_bus` component dir with platforms; RPM 1 s mean over the wire
- [Init]: API encryption on with a stable per-instance key; isolated, parallel-safe instances from day one
- [Init]: HIL contract entity names; status LEDs internal (not in HA), visible in CLI/UI only
- [Roadmap]: Contract tests (TEST-01/02) live in Phase 2 next to the IO surface they guard, not in a separate final phase

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 1] Go 1.27.3 is not yet installed on this host (checksum in research/STACK.md)
- [Phase 1] Spike needed: static build with `api: encryption:` (Noise libs fetched at build time, hermetic build cache), same-path bind mounts for local external components, incremental rebuild timing
- [Phase 1] Ready check needs a real encrypted API handshake from the Go side (Noise); pick the approach during planning
- [Phase 2] `partition` + `pulse` effects on the sim addressable light and the resulting LED frame rate are unverified at runtime
- [Phase 2] Live HA behaviour with the stable-MAC scheme is unverified (research read HA source, no live run)
- [Later] Static linking with real product modules (DNS, `json:`) is unverified; run-in-image fallback stays available
- [Later] Host `select()` timeout lives in `core/wake/wake_host.cpp::wakeable_delay` in 2026.10.1, not `core/application.cpp` as PROJECT.md says (affects the time-factor milestone)

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-11-08T22:45:01.040Z
Stopped at: Phase 3 UI-SPEC approved
Resume file: .planning/phases/03-live-web-ui/03-UI-SPEC.md
