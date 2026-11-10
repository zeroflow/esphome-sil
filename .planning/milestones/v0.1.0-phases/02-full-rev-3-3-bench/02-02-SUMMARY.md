---
phase: 02-full-rev-3-3-bench
plan: 02
subsystem: sim-bus LEDs, engine, status API, rev33 example
tags: [esphome, addressable-light, partition, sim-bus, go, leds, rev33]

requires:
  - phase: 02-full-rev-3-3-bench
    provides: 02-01 optional manifest channels, rev33 example, Status.buttons, e2e r33Up helper
provides:
  - sim_bus light platform (SimLight): AddressableLight with content dedupe, 50 ms gap, loop flush, codegen-derived segments
  - sim_bus hub: type()/pixels()/segments() on SimChild, rgb chan frames with n and seg, set_out_px, px replay before sync
  - bus.Channel N and Seg; engine LEDChannel/LEDState/Snapshot.LEDs with strict px and seg validation
  - control.LEDStatus and Status.leds ({strip, segments}, never null)
  - examples/rev33.yaml LED logic (board boot pulse, green on a real client, per-fan RPM LEDs)
affects: [02-04 PROTOCOL.md and vectors, 02-06 sil watch leds events, 02-07 contract test, 03 UI]

actuals:
  tokens: 14300
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Pixels are a hex string (`px`) on an `rgb` out channel; the Go side stores the exact bytes and never converts"
    - "Segment names come only from the firmware: codegen walks the stock partition lights, the hub sends them in the chan frame"
    - "Dedupe plus a minimum gap at the source, pending change flushed from loop(), state replayed on reconnect"
    - "A new bus session starts the LED state empty; a disconnect keeps the last frame"

key-files:
  created:
    - components/sim_bus/light.py
    - components/sim_bus/sim_light.h
    - components/sim_bus/sim_light.cpp
    - e2e/leds_test.go
  modified:
    - components/sim_bus/sim_bus.h
    - components/sim_bus/sim_bus.cpp
    - internal/bus/server.go
    - internal/bus/server_test.go
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/supervisor/supervisor.go
    - internal/control/control.go
    - examples/rev33.yaml

key-decisions:
  - "Engine LED state is per bus session: hello clears strip, N and segments, a disconnect keeps them, so `sil status` shows the last frame while the bus is down and the replay refills it on reconnect"
  - "A px frame that arrives before the manifest is accepted under the loose rules (multiple of 6, at most 1530 chars) and checked against n once the manifest is known; a size mismatch drops it with one warning"
  - "An invalid n leaves strip and segments alone; a valid n with a bad seg still sizes the strip to black and only leaves the segments empty"
  - "An empty seg (a light without partitions) is no segments and no warning"
  - "light.py validates at final-validate time that every segment_labels key is a partition of the light and that derived labels are valid and unique (the engine would reject the whole seg otherwise); 02-04 still owns the complete config-error set"
  - "The rainbow follow-up builds a gitignored copy of rev33.yaml (`examples/rev33-*_dev.yaml`, removed at test end) because the build mounts one tree and the variant must keep the relative component path"

requirements-completed: [IO-04, CLI-01, FW-01, FW-02]

coverage:
  - id: D1
    description: "`sil status` reports leds {strip: 5 x #rrggbb, segments: {board:[0], fan1..fan4}} for rev33; segment names come from the firmware's partition lights; no simulator-side LED config"
    requirement: IO-04
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDStatus"
        status: pass
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDFrames/manifest"
        status: pass
    human_judgment: false
  - id: D2
    description: "Exact pixel bytes, lowercase normalisation, early px, bad px/n/seg each one warning and previous state kept, no leds channel gives empty strip and segments, last frame kept across a disconnect"
    requirement: IO-04
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineLEDManifest, TestEngineLEDManifestWarnings, TestEngineLEDPixels"
        status: pass
      - kind: unit
        ref: "internal/bus/server_test.go#TestManifestCarriesRGBChannel"
        status: pass
    human_judgment: false
  - id: D3
    description: "Frames only on change, never closer than 50000 us of firmware time, during pulse and during addressable_rainbow; zero frames in 5 s of an idle firmware with the final colour flushed"
    requirement: IO-04
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDFrames/pulse_rate, idle_silence, rainbow_rate"
        status: pass
    human_judgment: false
  - id: D4
    description: "On reconnect exactly one leds frame (the strip state at reconnect) arrives between hello and sync and nothing follows it while the strip is static"
    requirement: IO-04
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDFrames/replay"
        status: pass
    human_judgment: false
  - id: D5
    description: "D-13 example logic: board blue pulse after `sil up` (sil's own probe does not count), static green after a real API client, per-fan LED green only while that fan's RPM is above 0"
    requirement: FW-02
    verification:
      - kind: e2e
        ref: "e2e/leds_test.go#TestLEDStatus"
        status: pass
    human_judgment: false
  - id: D6
    description: "Phase 1 f32 chan frames unchanged (no n or seg), fan-skeleton still warning-free"
    requirement: FW-01
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestHubSession, TestFakePeer, TestGuardsCompile; e2e/leds_test.go#TestLEDFrames/manifest"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-11-09
status: complete
commits: 3
plan_head_before: 1889417c3fc6d37ac18bf8350894273e8bac11a5
plan_head_after: 39cc09809086386b4520feda9e46f649c4f05917
---

# Phase 2 Plan 02: Status LED strip Summary

**The real Rev 3.3 firmware's LED colours, board and per-fan, appear in `sil status` as `leds {strip, segments}`; a new `sim_bus` addressable light under stock `partition` lights names the segments from the firmware's own YAML and keeps the bus quiet with change-only frames capped below 20 Hz.**

## Performance

- **Duration:** about 25 min (start time not recorded at launch; estimated from the 02-01 completion stamp)
- **Tasks:** 2 (1 auto, 1 TDD)
- **Files:** 13 (4 new, 9 changed)
- **Measured LED traffic (real firmware, fake peer, firmware `t`):**
  - boot `pulse`, first 4 s: 49 frames, 12.0 frames/s (identical in two runs); every consecutive pair differs in px and is at least 50000 us apart
  - `addressable_rainbow` on the board partition, first 4 s: 45 to 60 frames, 10.9 to 14.6 frames/s (varies with host load), same rules hold
  - idle firmware (static green, RPM republishing every second), 5 s window: **0 frames** (22 to 23 frames had been sent before the window, i.e. the pulse and the transition to green)
  - reconnect: exactly 1 leds frame between hello and sync, equal to the last px of the previous connection, nothing after it in the next 2 s

## Accomplishments

- `components/sim_bus/light.py` + `sim_light.{h,cpp}`: `SimLight` (AddressableLight + SimChild, type `rgb`), `num_leds` 1..255, `segment_labels`, `derive_segments` walks the stock partition lights in config order (`label:from:to`, label = partition id minus `_led`, overridable). Final validation rejects a `segment_labels` key that is no partition of the light and invalid or duplicate labels.
- Hub: `SimChild::{type, pixels, segments}`, chan frame `{k, ch, dir, type, n, seg}` for pixel channels only, `set_out_px` with `last_px_` replayed after `last_out_` and before `sync`; f32 chan frames are byte-identical to Phase 1.
- Go: `bus.Channel{N, Seg}`; engine `LEDChannel`, `LEDState`, `Snapshot.LEDs` (copied on publish), strict validation (n 1..255, `label:from:to` with regex, unique labels, 0 <= from <= to < n; px even-length lowercase-normalised hex, multiple of 6, at most 1530 chars and exactly 6 x n once n is known); `Status.leds` is `{strip: [], segments: {}}` and never null.
- `examples/rev33.yaml`: `status_leds` light, `Board Status` partition (pulse and addressable_rainbow effects) and four `Fan N Status` partitions, all internal; `on_boot` blue pulse; `on_client_connected` condition `client_info != "sil"`; each RPM sensor's `on_value` drives its fan LED. `internal/apiclient/probe.go` is unchanged.
- Resolves the STATE concern "partition + pulse on the sim light and the LED frame rate are unverified at runtime": both verified by tests on this repo's code.

## Task Commits

1. **Task 1: LED strip from firmware partition lights to `sil status`** - `718137d` (feat)
2. **Task 2 tests: engine LED manifest and pixel validation** - `a3ca1ea` (test)
3. **Task 2 e2e: TestLEDFrames against the real firmware (incl. the rainbow follow-up)** - `39cc098` (test)

**Plan metadata:** committed separately (docs: complete plan).

## TDD Gate Compliance

Task 2 is `tdd="true"` but its production code (engine validation, SimLight gap and dedupe, hub replay) is Task 1's, as the plan lays out. There was therefore no honest RED commit: the engine tests passed on first run. To prove they have teeth, one mutation was tried by hand (dropping the lowercase normalisation of px): four subtests failed, and the original code was restored. The e2e `idle_silence` and `pulse_rate` tests also passed without any change to `sim_light.cpp`, so the plan's step 3 (fix gap/dedupe/flush if they fail) was not needed. GREEN is `make check` plus `make e2e` green; no REFACTOR.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Effect name in the rainbow follow-up**
- **Found during:** Task 2, first `rainbow_rate` run
- **Issue:** the follow-up variant called `set_effect("addressable_rainbow")`; ESPHome logged `'Board Status': no such effect 'addressable_rainbow'` and the strip stayed static (2 frames). The effect's default name is `Rainbow` (`set_effect` matches names case-insensitively, which is why `"pulse"` works for `Pulse`).
- **Fix:** the variant uses `set_effect("rainbow")`.
- **Files modified:** `e2e/leds_test.go`
- **Verification:** `TestLEDFrames/rainbow_rate` passes with 45 to 60 frames in 4 s.
- **Commit:** `39cc098`

**2. [Rule 3 - Blocking] Variant YAML must sit inside the mounted tree**
- **Found during:** Task 2, first `rainbow_rate` run
- **Issue:** a variant of rev33.yaml written to a temp dir has its `../components` path outside the build's single read-only mount (`Could not find directory ...`).
- **Fix:** `ledsRev33With` writes the copy next to the original as `examples/rev33-*_dev.yaml` (matches the existing `*_dev.yaml` gitignore pattern) and removes it in `t.Cleanup`.
- **Files modified:** `e2e/leds_test.go`
- **Commit:** `39cc098`

**3. [Rule 2 - Missing critical] Plan-check follow-up: effects other than pulse**
- The checker's warning (backstop truth without a test) is closed by the `rainbow_rate` subtest of `TestLEDFrames`: the board partition runs `addressable_rainbow` (rewrites the strip about every 32 ms) and the same change-only, 50000 us and at-most-20-frames/s rules hold; LEDs 1 to 4 stay off in every frame. Practical note: the effect plumbing needs a firmware variant because the partition lights are `internal: true` and have no API handle, hence the generated `_dev.yaml` copy above.

### Plan-order notes (not defects)

- The engine unit tests are in their own commit (`a3ca1ea`) and the engine implementation in Task 1, see TDD Gate Compliance.
- `bus.Channel` stayed a struct with exported fields; the one unkeyed literal in `server_test.go` was rewritten keyed and a case `TestManifestCarriesRGBChannel` added, as planned.
- A first version of the engine test asserted that a caller cannot mutate a returned Snapshot's slices. The engine's existing contract (Ports, Buttons) is "do not mutate" by convention, not defensive copies on read, so the test was changed to the real guarantee: a later frame does not change an earlier snapshot.

---

**Total deviations:** 3 (1 bug in the follow-up test, 1 blocking, 1 missing coverage added) plus order notes.
**Impact on plan:** no scope change; all acceptance criteria met.

## Issues Encountered

- The Phase 2 research listed the effect as `addressable_rainbow` for the YAML key; the runtime name for `set_effect` is `Rainbow`. The example only calls `set_effect("pulse")` and `("none")`, so nothing in the shipped YAML is affected.
- Frame rate depends on the host: the firmware main loop runs about every 16 ms, so the practical cap with a 50 ms gap is about 15.6 frames/s (64 ms steps); measured 10.9 to 14.6 under a rainbow and 12 under a pulse (identical consecutive pixels are dropped by the dedupe).

## Known Stubs

None. No placeholder data flows to a consumer.

## Threat Flags

None beyond the plan's register. T-02-04 (frame flood) is mitigated at the source and measured above; T-02-05 (px/seg parsing) by strict checks in `engine.go` with unit tests for every rejected shape; T-02-06 accepted as planned (LED state only on the 0600 `api.sock`, all lights internal). No Go module or package was added (T-02-SC).

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocks the plan. For the morning review:

- **Wire documentation pending by plan:** the `rgb` channel type, `n`/`seg`/`px` fields and the replay rule are not yet in `protocol/PROTOCOL.md` or `protocol/vectors/frames.tsv` (02-04 owns that, as the plan states). Until then only the Go and e2e tests pin them.
- **Pulse colour thresholds:** assertions classify by dominant channel and a 0x20 threshold (Research Pitfall 5). A brightness change in the example would need the thresholds revisited.

## Next Phase Readiness

Ready for 02-03. `leds` is in `Snapshot` and `Status`; 02-06 can emit `leds` watch events from the same state (every px frame, at most 20 Hz by construction). No blockers.

## Self-Check: PASSED

- Created files exist: `components/sim_bus/light.py`, `components/sim_bus/sim_light.h`, `components/sim_bus/sim_light.cpp`, `e2e/leds_test.go`.
- Commits exist: `718137d`, `a3ca1ea`, `39cc098`.
- `make check` passes (gofmt, vet, race tests, C++ vectors); the full `make e2e` (38 tests, 421 s) passes, including TestLEDStatus, TestLEDFrames and every subtest, TestPressTracer, TestPressVocabulary, TestHubSession, TestFakePeer and TestGuardsCompile.

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*
