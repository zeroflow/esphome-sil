---
milestone: v1
audited: 2026-11-10T11:30:00Z
status: tech_debt
scores:
  requirements: 40/40
  phases: 3/3
  integration: 14/14
  flows: 4/4
gaps:
  requirements: []
  integration: []
  flows: []
tech_debt:
  - phase: cross-phase (01 resync x 02 buttons x 03 page)
    items:
      - "Sim restart while a USR button is held: no `button false` event is published (supervisor.go ~861 publishes only `sim restarted`); `sil watch` consumers keep a stale held state, and the page's later `button.up` gets `button_not_held` on the new engine (buttons.js seenHeld). Affects CLI-03, IO-03, UI-04."
      - "Page MAX_HOLD_MS = 10000 (ui/web/js/buttons.js:9) equals the USR2 `on_click max_length: 10s` (examples/rev33.yaml:253); a hold that reaches the backstop may fall outside the firmware's window. Normal ~2 s long press verified (03-UAT test 2). Affects IO-03, UI-04."
      - "Page does not show Status.error, warnings, phase, firmware.restarts, last_exit (CLI-only); a failed instance shows little on the page. Affects UI-05 (not required by it)."
      - "control.OperationByName / control.MatchOperation (internal/control/operations.go:75,93) have test-only callers."
  - phase: 01-closed-loop-fan-skeleton
    items:
      - "Override: 64 KiB hub TX-cap drop has no automated or manual test (01-UAT test 5 skipped, 'good enough for now'); code present in sim_bus.h kMaxTx / sim_bus.cpp flush_."
      - "01-SECURITY.md frontmatter still `status: draft` (31/31 threats closed, threats_open 0)."
      - "Roadmap `Mode: mvp` but goal is not a user story (also flagged for Phase 2)."
      - "Open review info: IN-01 SIGHUP handling vs `nohup sil up`; IN-02 broad `^FAILED: ` compile marker, no real failing-compile golden log; misc unused consts, /tmp/sil.sock default."
  - phase: 02-full-rev-3-3-bench
    items:
      - "Deferred: independent re-review of Set/SimRestart concurrency fixes (8542d9d, b976a6c not re-reviewed after iteration cap; da1a9ca, e4f1046)."
      - "Open review info IN-01..IN-09: light.py pixel-size comment; SIL_BUS bypasses 107-byte path check; control handlers accept trailing JSON; duplicate lanHostPtr; broker drops final `lagged` marker; silbench.py dead state / BaseException catch; sim_light.h missing <algorithm>; unreachable `default:` in Set; Set racing shutdown reports engine_failed."
      - "TestFirmwareBus working-tree assertion fails when another process writes into the repo during a run (test hazard)."
  - phase: 03-live-web-ui
    items:
      - "Open review info: IN-01 inputs commit() discards edits typed in flight; IN-02 static handler lists directories, --ui-dir served on LAN bind, MaxHeaderBytes 1 MiB; IN-03 unused maxRestartGap, duplicate lanHostPtr, odd WebURL scheme string; IN-05 SIM TIME can step back one tick (engine.go:548); IN-06/IN-07 timed-out button down semantics (accepted in 03-UAT test 10)."
nyquist:
  skipped: "validate-phase hook not active (verify:post hooks: secure-phase, ui-review only)"
---

# Milestone v1 Audit: esphome-sil

**Definition of done (ROADMAP overview):** prove the plumbing end to end. Real ESPHome firmware runs on `host:` and drives simulated fans closed-loop (Phase 1). The full Rev 3.3 IO surface works with the CLI and Home Assistant, guarded by contract tests (Phase 2). A live web UI is a pure client of the same API (Phase 3).

**Verdict:** achieved. All 40 v1 requirements are satisfied across 3 independent sources, all phases verified `passed` with UAT complete, and every cross-phase link and E2E flow is wired. There are no blockers. The status is `tech_debt` because of the deferred and open info items listed below.

## Phases

| Phase | VERIFICATION | Score | UAT | Security | Notes |
|-------|--------------|-------|-----|----------|-------|
| 01 Closed-Loop Fan Skeleton | passed (re-verified 2026-11-10 after Phases 2 and 3) | 7/7 (1 by override) | 5 pass, 1 skipped | draft (0 open threats) | make e2e 57/57, C++ codec 119/119 on host and image gcc |
| 02 Full Rev 3.3 Bench | passed (re-verified 2026-11-10) | 5/5 | 2/2 pass (live HA) | secured | make test-contract 6 passed |
| 03 Live Web UI | passed | 9/9 | 10/10 pass (all 10 human items resolved) | secured | 4-layer parity check |

## Requirements Coverage (3-source cross-reference)

Sources: the REQUIREMENTS.md traceability table (all `[x]`), the VERIFICATION.md requirement tables (all SATISFIED), and the SUMMARY `requirements-completed` frontmatter.

| Group | IDs | Phase | VERIFICATION | SUMMARY | REQ | Final |
|-------|-----|-------|--------------|---------|-----|-------|
| Build and Run | BUILD-01..07 | 1 | SATISFIED | listed | [x] | satisfied |
| Sim-bus | BUS-01..04 | 1 | SATISFIED | listed | [x] | satisfied (BUS-03 TX-cap part by override) |
| IO | IO-01, IO-02, IO-06 | 1 | SATISFIED | listed | [x] | satisfied |
| IO | IO-03, IO-04, IO-05 | 2 | SATISFIED | listed | [x] | satisfied |
| Model | MODEL-01, 02, 04 | 1 | SATISFIED | listed | [x] | satisfied |
| Model | MODEL-03 | 2 | SATISFIED | listed (02-05) | [x] | satisfied |
| Firmware | FW-01..03 | 2 | SATISFIED | listed | [x] | satisfied |
| CLI | CLI-05, CLI-06 | 1 | SATISFIED | listed | [x] | satisfied |
| CLI | CLI-01..04 | 2 | SATISFIED | listed | [x] | satisfied |
| Home Assistant | HA-01, HA-02 | 2 | SATISFIED | listed | [x] | satisfied |
| Tests | TEST-01, TEST-02 | 2 | SATISFIED | listed | [x] | satisfied |
| Web UI | UI-01..06 | 3 | SATISFIED | listed | [x] | satisfied |

**40/40 satisfied. 0 partial, 0 unsatisfied, 0 orphaned.**

Tooling note: `gsd-tools summary-extract` returned an empty value for 02-03, 02-05 and 02-06. Their frontmatter does list `requirements-completed` (IO-05/CLI-02/CLI-01/FW-01/FW-02; MODEL-03/CLI-02; CLI-03/CLI-04/CLI-01), so these were checked by hand. This is an extractor parsing issue, not a coverage gap.

## Cross-Phase Integration

Checked by the integration checker using static tracing plus `go build`, `go vet` and unit tests for control, web, cli and events. Docker and e2e were not re-run; the phase verifiers ran them green.

- **One operation table** (`internal/control/operations.go`) drives the api.sock handler, the LAN handler and the web mux. The JSON is identical by construction. The `LAN` flag is the allow-list (`down`, `sim.restart`, `fw.restart` and `logs` return 403 on the web listener). Every row has a CLI command and a Client method, and the page's 5 OPS are all LAN rows (CLI-06, UI-06).
- **Phase 2 IO into the Phase 1 core:** button, LED and env frames flow through the engine and the event broker into `sil status`, `sil watch`, `/ws` and the page. The page and the CLI read the same `control.Status`. The page shows no field that the CLI lacks.
- **Phase 1 resync covers Phase 2/3 state:** a sim restart seeds env inputs and fans from the supervisor. The history ring lives in the broker and survives. The firmware replays LEDs on reconnect, the engine replays RPM, env and buttons, and the page resyncs on `sim restarted`.
- **Golden vectors:** `protocol/vectors/frames.tsv` includes the Phase 2 rows. Both `internal/bus/codec_test.go` and `tests/cpp/test_protocol.cpp` read it (BUS-04).
- **Orphans:** 2 test-only exports (tech debt). No dead routes.

### Checker findings and how they were classified

| # | Finding | Classification | Reason |
|---|---------|----------------|--------|
| 1 | Held button lost across a sim restart, so `button.up` from the page gets `button_not_held` | tech debt | Confirmed in code. This edge case needs a hold during a restart. The page resyncs, and the message line shows the server error. |
| 2 | `sil watch` keeps a stale held button after a sim restart | tech debt | Same cause as #1. |
| 3 | Page 10 s max hold equals the USR2 `max_length: 10s` | tech debt | It only matters at the backstop. The normal long press (about 2 s) passed on a real phone with HA (03-UAT test 2). |
| 4 | Temperature is one-way; the page echoes the input | not a gap | By design: the sim feeds the sensor. 03-UAT test 4 shows HA displaying 26, and `env_reaches_firmware` reads the firmware entity over the native API. |
| 5 | Page long press not tested against real firmware | resolved | `TestWebAcceptance/usr2_long_like_cli` runs against the real firmware, and 03-UAT test 2 confirmed it on a real phone with HA. |

## E2E Flows

| Flow | Status |
|------|--------|
| `sil up`, then HA sets fan speed, then RPM rises and shows in status, watch and the page | complete |
| Page USR2 long press, then firmware, then all fans at 100 % and visible in status | complete |
| `sil set temperature`, then the firmware Temperature entity (HA), then the page | complete |
| `sil sim restart` with the page open, then resync | complete (held-button edge is tech debt #1) |

## Tech Debt Summary

There are 12 entries in 4 groups, several bundling multiple review items (see the frontmatter). None blocks the milestone. These are worth doing before the v0.1.0 tag (REL-02):

1. Publish a button release (or an explicit `held` reset) when the simulator restarts.
2. The deferred independent re-review of the Phase 2 Set/SimRestart concurrency fixes.
3. A one-off stress test of the 64 KiB TX-cap drop.
4. Flip `01-SECURITY.md` to secured, and resolve the `mode: mvp` metadata on Phases 1 and 2.
