---
phase: 02-full-rev-3-3-bench
plan: 01
subsystem: sim-bus buttons, engine, control API, CLI
tags: [esphome, binary_sensor, sim-bus, go, kong, press, rev33]

requires:
  - phase: 01-closed-loop-fan-skeleton
    provides: sim_bus hub/sensor/output, engine single-writer inbox, supervisor engine slot, control API, CLI, e2e harness
provides:
  - sim_bus binary_sensor platform (SimBinarySensor) and a unique-channel check over output/sensor/binary_sensor/light
  - examples/rev33.yaml (fans, RPM, Button USR1..3 with the D-13 click windows)
  - engine button World (released levels in every hello in-snapshot, SetButton with strict rules, ErrStopped)
  - supervisor Press/SetButton on the sim clock, POST /api/v1/buttons/{id}/press|down|up, Status.buttons
  - sil press [--long | --hold DUR | --down | --up] [-i NAME]
  - exit-1 codes input_not_available, firmware_not_connected, button_busy, button_not_held
affects: [02-02 LEDs, 02-03 environment, 02-04..02-08 set/watch/logs/contract tests, 03 UI]

actuals:
  tokens: 17300
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Button = level on an optional `in` channel; the firmware's on_click windows decide short vs long"
    - "Engine operations answer ErrStopped at once after Run returned (done channel), never block"
    - "Optional manifest channels enable operations; a missing one never warns"
    - "--instance/-i selector for verbs with further positional arguments"

key-files:
  created:
    - components/sim_bus/binary_sensor.py
    - components/sim_bus/sim_binary_sensor.h
    - examples/rev33.yaml
    - e2e/rev33_test.go
  modified:
    - components/sim_bus/__init__.py
    - internal/engine/engine.go
    - internal/supervisor/supervisor.go
    - internal/control/control.go
    - internal/cli/cli.go
    - internal/apperr/codes.go
    - internal/engine/engine_test.go
    - internal/control/control_test.go
    - internal/cli/cli_test.go
    - internal/supervisor/supervisor_test.go

key-decisions:
  - "Button edge time is stamped with the clock's current time (not the last 10 ms tick), so t_up_us - t_down_us >= hold_ms * 1000 holds exactly"
  - "Press releases on whichever engine is in the slot at release time, with a context that cannot be cancelled by the caller; ErrStopped on the release counts as released (fresh World has the button released)"
  - "ShortPress/LongPress/MaxHold live in internal/control; the HTTP API requires an explicit hold_ms"
  - "PressCmd.Hold is *time.Duration so an explicit --hold 0s reaches the server and fails as invalid_argument instead of becoming the preset"
  - "A held button is not supervisor-held state: a sim restart releases it (the firmware sees the release through the new in-snapshot)"

requirements-completed: [IO-03, CLI-02, CLI-01, FW-01, FW-02, FW-03]

coverage:
  - id: D1
    description: "sim_bus binary_sensor platform delivers USR1-3 as levels; the firmware's on_click windows make USR1/USR3 short presses step every fan by 10 % and USR2 long set every fan to 100 %; USR2 short and USR1 long change nothing"
    requirement: IO-03
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressTracer"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressVocabulary/usr3_lowers, usr1_long_is_noop, usr2_short_is_noop, usr2_long_full"
        status: pass
    human_judgment: false
  - id: D2
    description: "sil press presets 200 ms / 1500 ms, --hold override, bounds 1..60000 ms, release edge sent before return with t_down_us/t_up_us in sim time"
    requirement: CLI-02
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestPressHolds, internal/control/control_test.go#TestPressRoute"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressTracer, TestPressVocabulary/hold_bounds"
        status: pass
    human_judgment: false
  - id: D3
    description: "Raw --down/--up, button_busy and button_not_held rejections that queue nothing, input_not_available, firmware_not_connected while the bus is down, unknown button lists the known ids"
    requirement: CLI-02
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineSetButtonErrors, internal/supervisor/supervisor_test.go#TestButtonOperationErrors"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressVocabulary/down_up_and_busy, bus_down, unknown_button, input_not_available"
        status: pass
    human_judgment: false
  - id: D4
    description: "sil status carries buttons {usr1,usr2,usr3} and keeps answering during a 1.5 s press; the first press after sil up, sim restart and fw restart counts"
    requirement: CLI-01
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressVocabulary/status_during_long_press, bus_down, first_press_after_fw_restart"
        status: pass
    human_judgment: false
  - id: D5
    description: "examples/rev33.yaml compiles in the pinned image, consumes sim_bus via external_components type local, carries only sil-injected identity; two sim_bus platforms sharing a channel are rejected at config time"
    requirement: FW-01
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressTracer (status and firmware device_info identity)"
        status: pass
      - kind: unit
        ref: "internal/fwbuild/lint_test.go#TestLintConfigIdentityAndApi"
        status: pass
      - kind: other
        ref: "esphome config on a copy of rev33.yaml with channel port1.rpm on Button USR2 -> 'duplicate sim_bus channel port1.rpm used by fan1_speed and btn_usr2'"
        status: pass
    human_judgment: false
  - id: D6
    description: "Button USR1 entity reads true over the native API while held with --down and false after --up (HA-02 proxy through aioesphomeapi)"
    requirement: FW-02
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestPressVocabulary/down_up_and_busy (watch series)"
        status: pass
    human_judgment: false

duration: 16min
completed: 2026-11-09
status: complete
commits: 4
plan_head_before: 7a6bec366ded0977144857483d73e8403a964bde
plan_head_after: 8f9e8a134311391d70b6fd53f268ddeef07df8d8
---

# Phase 2 Plan 01: Button slice (sil press) Summary

**`sil press usr1|usr2|usr3` drives the real Rev 3.3 firmware's own on_click logic through a new sim_bus binary_sensor platform, with strict hold/release rules (button_busy, button_not_held), sim-time edge stamps and `buttons` in `sil status`.**

## Performance

- **Duration:** 16 min
- **Started:** 2026-11-09T06:05:55Z
- **Completed:** 2026-11-09T06:21:40Z
- **Tasks:** 2 (1 tracer, 1 TDD)
- **Files modified:** 14 (4 new, 10 changed)
- **Build time of examples/rev33.yaml** (warm toolcache and ccache): cold `sil up` of a new instance name 10.1 s, warm `sil up` (build skipped by hash) 1.4 s. The very first compile of the file during Task 1 was about 14 s wall including the 127-step ninja build.

## Accomplishments

- Tracer through every layer: `sil press usr1` -> control route -> supervisor Press (sim clock) -> engine level -> bus `in usr1` -> `SimBinarySensor` -> ESPHome `on_click` -> `step_fans` -> `out portN.duty` -> `sil status`. Fan 1 40 % -> 50 %, Fans 2-4 0 % -> 10 % on a USR1 short press.
- Engine World gained buttons: the hello in-snapshot now carries `usr1..usr3` at their current level after the four rpm frames, so ESPHome's swallowed-first-state (Research Pitfall 1) can never eat the first press, also after `sim restart` and `fw restart`.
- Full D-01..D-04 vocabulary with four new exit-1 codes; `sil status` keeps answering (exit 0, valid JSON) during a 1.5 s press.
- FW-01/02/03 for the Rev 3.3 example: identity only from sil's substitutions, proven by TestPressTracer (status and the firmware's own device_info agree on `sil-e2e-r33` and the derived MAC) and the Phase 1 identity lint.
- IO-06 extended: duplicate channels across output/sensor/binary_sensor/light are rejected (checked by `esphome config` on a doctored copy).
- Regression: `make check` (112 tests, race detector, wall-clock lint, registry scan, C++ vectors) and the full `make e2e` (36 tests, 362 s) pass.

## Task Commits

1. **Task 1: Tracer, `sil press usr1` to fan duty** - `65cc2e9` (feat)
2. **Task 2 RED: failing tests for the press vocabulary** - `23c5012` (test)
3. **Task 2 GREEN: raw down/up with strict rules** - `43920a5` (feat)
4. **Task 2: e2e TestPressVocabulary** - `8f9e8a1` (test)

**Plan metadata:** committed separately (docs: complete plan).

## TDD Gate Compliance

- RED: `23c5012`. The target test `internal/supervisor TestButtonOperationErrors` failed on the assertion for the planned behavior: `SetButton(down=true) without a firmware: internal: raw button control is not implemented yet, want firmware_not_connected` (SetButton was the deliberate Task 1 stub, exactly the "remaining implementation" of Task 2). The engine, control and CLI tests of the behavior list passed already at RED because Task 1 (a tracer, production quality) had implemented those layers; they are regression pins, not RED evidence.
- GREEN: `43920a5`, the supervisor SetButton implementation; `make check` green.
- REFACTOR: none needed.

## Files Created/Modified

- `components/sim_bus/binary_sensor.py`, `sim_binary_sensor.h` - the button platform (header-only, `USE_BINARY_SENSOR` guard, `publish_state(v > 0.5f)`)
- `components/sim_bus/__init__.py` - `check_unique_channels` scans output, sensor, binary_sensor, light (light named by `output_id` when present)
- `examples/rev33.yaml` - fans, RPM, `Button USR1..3`, `step_fans` script; LEDs and environment are added by 02-02/02-03
- `internal/engine/engine.go` - `ButtonIDs`, sentinels, button World, `SetButton`, optional channels in `checkManifest`, `done` channel
- `internal/supervisor/supervisor.go` - `Press`, `SetButton`, `buttonErr`, `Status.Buttons`
- `internal/control/control.go` - `ShortPress/LongPress/MaxHold`, `PressRequest/PressResult`, routes, client methods
- `internal/cli/cli.go` - `PressCmd`; `internal/apperr/codes.go` - four codes
- Tests: engine, control, cli, supervisor unit tests; `e2e/rev33_test.go` (`TestPressTracer`, `TestPressVocabulary`)

## Decisions Made

See `key-decisions`. In short: edge times use `clock.Now()` rather than the last tick so the hold arithmetic is exact; the release goes to whichever engine is current and cannot be cancelled by the caller; `--hold` is a pointer to distinguish `--hold 0s`; buttons are not supervisor-held (a sim restart releases a held button).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `TAG` undeclared in the header-only binary sensor**
- **Found during:** Task 1, first `TestPressTracer` compile
- **Issue:** `LOG_BINARY_SENSOR` expands to a use of `TAG`; the header-only class had none, and a namespace-level `TAG` in a header would clash with the per-.cpp `TAG`s of the other sim_bus files.
- **Fix:** `static const char *const TAG` declared locally inside `dump_config()`.
- **Files modified:** `components/sim_bus/sim_binary_sensor.h`
- **Verification:** the pinned-image compile succeeds; `TestPressTracer` passes.
- **Committed in:** `65cc2e9`

**2. [Rule 2 - Missing critical] Supervisor unit test added to carry the TDD RED**
- **Found during:** Task 2 (tests first)
- **Issue:** the behavior list names engine, control and CLI tests only; those already passed after the tracer, so the task had no failing test at all.
- **Fix:** `TestButtonOperationErrors` in `internal/supervisor/supervisor_test.go` (real engine on a Manual clock, no firmware) asserts the error mapping and `Status.Buttons`; it failed against the Task 1 stub and went green with the implementation.
- **Files modified:** `internal/supervisor/supervisor_test.go`
- **Committed in:** `23c5012`

### Plan-order notes (not defects)

- Task 1 shipped `supervisor.SetButton` as a stub returning Infra `internal` ("not implemented yet") so the Control interface compiled; Task 2 replaced it. The `down`/`up` routes and CLI flags were therefore wired in Task 1 and only functional from `43920a5`.
- `engine_test.go` also needed the frame counts of `TestEngineManifestWarnings` and `TestEngineSnapshotCarriesCurrentRPM` raised from 6 to 9 (four rpm + three button frames), beyond the one test the plan names.

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 missing critical) plus 2 order notes.
**Impact on plan:** no scope change; all acceptance criteria met.

## Issues Encountered

- The shell's `grep` is a ugrep wrapper that mishandles a literal `${...}` in a BRE pattern, so the plan's identity greps (`grep -n 'mac_address: ${sil_mac}' ...`) printed nothing there. GNU grep via `command grep` matched all of them (lines 13, 19, 25, 28 of `examples/rev33.yaml`); the file was correct all along.

Requirement checkboxes in REQUIREMENTS.md stay open on purpose: `requirements.ready-ids` reported 0 of 6 ready because sibling plans of this phase declare the same IDs and have no SUMMARY yet (shared-ID gate); the last declaring plan flips them.

## Known Stubs

None. No placeholder data flows to a consumer; `examples/rev33.yaml` intentionally has no LED or environment entities yet (02-02 and 02-03).

## Threat Flags

None beyond the plan's register. The new routes sit behind the existing 0600 `api.sock`; T-02-01 and T-02-02 are mitigated as planned (hold cap 60000 ms and 64 KiB body with unknown fields rejected; id checked against `engine.ButtonIDs` before any engine call; shutdown ends a hold through `withDown`). No Go module or package was added (T-02-SC).

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocks the plan. Items flagged for the morning review:

- **A-02-01 (unattended choice, unresolved):** presets 200 ms / 1500 ms live as `control.ShortPress/LongPress`, the HTTP API needs an explicit `hold_ms`, and a sim restart releases a held button. Confirm or change before the Phase 3 UI binds to it.
- **EDGE-IO-03 (unclassified edge, unresolved):** covered here: initial released level in every snapshot, strict busy/not-held, window boundaries (USR2 short and USR1 long are no-ops, both asserted), hold bounds 1..60000 ms. A manual edge review of IO-03 is still wanted, notably: `--up` on a button held when the firmware disconnects returns `firmware_not_connected` until the firmware is back (strict rule order from the plan).
- **MVP framing:** the phase goal is not in user-story form; `/gsd mvp-phase 2` was not run.

## Next Phase Readiness

Ready for 02-02. The wire path for the next `in`/`out` kinds exists end to end (optional manifest channels, supervisor-side operations, control routes, `--instance/-i`, `check_unique_channels` already scanning `light`). No blockers.

## Self-Check: PASSED

- Created files exist: `components/sim_bus/binary_sensor.py`, `components/sim_bus/sim_binary_sensor.h`, `examples/rev33.yaml`, `e2e/rev33_test.go`.
- Commits exist: `65cc2e9`, `23c5012`, `43920a5`, `8f9e8a1`.
- `make check` passes (112 passed, 0 failed); `make e2e` full run passes (36 tests, includes TestPressTracer, TestPressVocabulary and every subtest, TestTracer, TestCLIContract, TestSimRestartResync).

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*
