---
phase: 02-full-rev-3-3-bench
plan: 04
subsystem: sim-bus protocol spec, golden vectors, sim light config validation
tags: [protocol, golden-vectors, esphome, config-validation, partition, leds, e2e]

requires:
  - phase: 02-full-rev-3-3-bench
    provides: 02-02 sim light, rgb chan/px wire fields, derive_segments; 02-03 env channels and the 10 s re-send; 02-01 button channels
provides:
  - protocol/PROTOCOL.md Phase 2 section (channels, rgb type with n/seg/px, snapshot orders, optional channels, env re-send, LED rules), still version 1
  - frames.tsv rows chan_rgb, out_px, in_button, in_env (valid) and enc_chan_rgb, enc_out_px, enc_in_button (encode), read by the Go and both C++ (host g++ and image gcc) tests
  - light.py validate_segments as FINAL_VALIDATE_SCHEMA (segment range, labels, uniqueness, seg <= SEG_MAX_BYTES 2048)
  - e2e TestLEDCodegen (bounds, labels, seg_too_long, cross-domain duplicate channels, light-only and binary_sensor-only compiles)
affects: [02-06 watch leds events, 02-07 contract test, 03 UI]

plan_head_before: c4919ff0ffdc3b28d26d5c9c8ea78d71ff054f4d
plan_head_after: bdb27d286ddea1dc64f6dd2cb606ae2ba414bc32

actuals:
  tokens: 6500
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Every wire frame shape a phase sends is pinned in the shared frames.tsv and read by both codecs"
    - "Config mistakes are rejected in FINAL_VALIDATE_SCHEMA with the light id and the offending entry in the message"
    - "e2e codegen fixtures are built by a small YAML builder (ledsLightYAML) so each subtest differs by one fact"

key-files:
  created: []
  modified:
    - protocol/PROTOCOL.md
    - protocol/vectors/frames.tsv
    - components/sim_bus/light.py
    - components/sim_bus/__init__.py
    - e2e/leds_test.go

key-decisions:
  - "The duplicate-channel message of the hub names a light by its user-written id, not by the generated SimLight output_id (an auto id such as sim_bus_simlight_id told the developer nothing and did not match the id partitions refer to)"
  - "default_label() is the single place that maps a partition id to its label; both validation and codegen use it, so what is validated is exactly what is sent"
  - "PROTOCOL.md says the Phase 2 additions are backward compatible and the version stays 1"

patterns-established:
  - "A partition segment outside 0..num_leds-1 is a config error that names the partition and the light"

requirements-completed: [IO-04, IO-03, IO-05]

coverage:
  - id: D1
    description: "PROTOCOL.md documents the Phase 2 channels, the rgb chan fields n/seg, the px out field, both snapshot orders, optional vs required channels, the 10 s env re-send and the LED dedupe/gap/replay rules"
    requirement: "IO-03"
    verification:
      - kind: other
        ref: "grep -n 'Phase 2 channels' and 'temperature, humidity, usr1, usr2, usr3' protocol/PROTOCOL.md"
        status: pass
    human_judgment: true
    rationale: "Prose accuracy against the implementation was checked by reading engine.go and the hub, but no test asserts the wording"
  - id: D2
    description: "Golden vectors for rgb chan, px out, button in and env in pass in the Go codec test, the host g++ test and the pinned image gcc test"
    requirement: "IO-05"
    verification:
      - kind: unit
        ref: "make check (internal/bus TestVectors, TestRoundTrip) and make test-cpp-image: 119 passed, 0 failed"
        status: pass
    human_judgment: false
  - id: D3
    description: "num_leds 1 and 255 accepted, 0 and 256 rejected; a segment ending at num_leds - 1 accepted, ending at num_leds rejected naming the partition"
    requirement: "IO-04"
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDCodegen/num_leds_bounds and range_bounds"
        status: pass
    human_judgment: false
  - id: D4
    description: "Unknown segment_labels key, invalid label, duplicate label and a derived seg over 2048 bytes each stop esphome config, naming the light and the entry"
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDCodegen/unknown_label_key, bad_label, duplicate_label, seg_too_long"
        status: pass
    human_judgment: false
  - id: D5
    description: "Two sim_bus platforms in different domains on the same channel are rejected naming both ids; a light-only and a binary_sensor-only config compile in the pinned image"
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDCodegen/duplicate_channel_cross_domain and guard_compiles"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-11-09
status: complete
---

# Phase 2 Plan 04: Phase 2 wire contract and LED config validation Summary

**Phase 2 frames (rgb chan with n/seg, px out, button and env in) are written into PROTOCOL.md and pinned byte for byte by both codecs (C++ 112 -> 119 rows), and the sim light's segments are validated at `esphome config` with messages that name the light and the offending entry.**

## Performance

- **Duration:** about 25 min
- **Completed:** 2026-11-09T07:08Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments

- `protocol/PROTOCOL.md` (still version 1, additive): a "Phase 2 channels" table (usr1..usr3, temperature, humidity, leds), required vs optional channels and which mismatches warn, the rgb `n` and `seg` chan fields and the `px` out field, the firmware snapshot (`hello`, `chan`, f32 `out`, `px` out, `sync`) and simulator snapshot (`welcome`, port rpm 1..4, temperature, humidity, usr1..usr3, `sync`) orders, the 10 s per-value env re-send, and an "LED frames" block (px format, 255 LED cap, seg grammar, content dedupe, 50 ms gap, loop flush, replay before `sync`).
- `protocol/vectors/frames.tsv`: `chan_rgb`, `out_px`, `in_button`, `in_env` valid rows plus `enc_chan_rgb`, `enc_out_px`, `enc_in_button` encode rows. They passed on the first run in Go, host g++ and the image's gcc with no codec change: `make test-cpp-image` went from 112 to **119 passed, 0 failed**.
- `components/sim_bus/light.py`: `validate_segments` (the `FINAL_VALIDATE_SCHEMA`) rejects a segment_labels key that is no partition of the light, a label outside `^[a-z][a-z0-9_]*$`, two partitions with the same label, a partition segment outside `0..num_leds-1` (or `from > to`), and a derived seg over `SEG_MAX_BYTES` (2048). Every message starts with the light id and names the partition or label. `num_leds` stays `int_range(1, 255)`.
- `e2e/leds_test.go` `TestLEDCodegen`: `num_leds_bounds`, `range_bounds`, `unknown_label_key`, `bad_label`, `duplicate_label`, `seg_too_long` (the plan-checker follow-up: 24 partitions with 90-character labels exceed 2048 bytes and fail naming `strip`, `2048` and `derived seg`; five of the same labels pass), `duplicate_channel_cross_domain` (binary_sensor vs light, sensor vs binary_sensor), `guard_compiles` (`light-only`, `binary-sensor-only` compile in the pinned image).

## Task Commits

1. **Task 1: Phase 2 wire contract in PROTOCOL.md and golden vectors** - `056ae23` (feat)
2. **Task 2: LED and channel misconfigurations fail at config time** - `bdb27d2` (feat)

**Plan metadata:** the docs commit that carries this file.

## Files Created/Modified

- `protocol/PROTOCOL.md` - Phase 2 channels, rgb fields, snapshot orders, optional channels, env re-send, LED rules
- `protocol/vectors/frames.tsv` - 7 new rows
- `components/sim_bus/light.py` - `validate_segments`, `default_label`, `SEG_MAX_BYTES`
- `components/sim_bus/__init__.py` - duplicate-channel message names the light by its `id`
- `e2e/leds_test.go` - `TestLEDCodegen` and its fixture builder `ledsLightYAML`

## Decisions Made

- The cross-domain duplicate-channel error used the generated `output_id` for a light (`sim_bus_simlight_id`). It now uses the user-written `id` (the one partitions reference), falling back to `output_id`. Without this the message could not "name both ids" in a way the developer recognises.
- Label defaulting lives in one helper, `default_label`, shared by validation and `derive_segments`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Duplicate-channel message named the generated output_id of a light**
- **Found during:** Task 2 (`duplicate_channel_cross_domain` failed on the first run: output lacked `strip`)
- **Issue:** `check_unique_channels` preferred `CONF_OUTPUT_ID`, which is auto-generated for a sim light, so the message said "used by btn_x and sim_bus_simlight_id".
- **Fix:** prefer `CONF_ID` and fall back to `CONF_OUTPUT_ID`.
- **Files modified:** `components/sim_bus/__init__.py`
- **Verification:** `TestLEDCodegen/duplicate_channel_cross_domain`, `TestCodegenValidation` (all subtests) pass.
- **Committed in:** `bdb27d2`

### Plan vs. state of the code

- 02-02 had already added a `final_validate` in `light.py` (unknown key, label charset, duplicate label). Task 2 completed it rather than adding it: renamed to the plan's `validate_segments`, added the range check and the 2048-byte check, added the light id to every message.
- Task 1 is `tdd="true"` but the plan itself states that the new rows pass without code changes, so there is no RED commit. Honest evidence: the rows were added first, `make check` and `make test-cpp-image` ran green with 119 rows, and the Go `-v` output shows each of the seven new rows as its own passing subtest (TestVectors and TestRoundTrip), so the rows are really read.

**Total deviations:** 1 auto-fixed (1 bug). **Impact:** none beyond a clearer error message.

## Issues Encountered

None.

## Verification

- `make check`: pass. `make test-cpp-image`: 119 passed, 0 failed (was 112).
- `make e2e E2E_RUN='TestLEDCodegen|TestCodegenValidation|TestGuardsCompile|TestLEDStatus'`: all pass, including `TestLEDCodegen/guard_compiles`.
- Acceptance greps: `"type":"rgb"`, `"px":"0000ff000000"` in frames.tsv; `Phase 2 channels` and `temperature, humidity, usr1, usr2, usr3` in PROTOCOL.md; `FINAL_VALIDATE_SCHEMA` and `2048` in light.py.

## Known Stubs

None.

## Threat Flags

None. T-02-10 (config validation) and T-02-11 (codec drift) are mitigated as planned; no new network or trust-boundary surface.

## Needs Human

None. (PROTOCOL.md prose is checked against the code by reading, not by a test; a reviewer may want to skim the "LED frames" paragraph.)

## Next Phase Readiness

Ready for 02-05. The wire contract for Phase 2 is documented and pinned; 02-06 (`watch` leds events) and 02-07 (contract test) can rely on the snapshot orders and LED rules as written.

## Self-Check: PASSED

- Files modified exist on disk (PROTOCOL.md, frames.tsv, light.py, `__init__.py`, leds_test.go).
- Commits `056ae23` and `bdb27d2` exist; `git rev-list --count c4919ff..HEAD` was 2 at the end of the tasks.

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*
