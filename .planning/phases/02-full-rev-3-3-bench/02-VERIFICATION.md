---
phase: 02-full-rev-3-3-bench
verified: 2026-11-09T10:40:00Z
status: human_needed
score: 4/5 must-haves verified
covered_files:
  - .planning/phases/02-full-rev-3-3-bench/02-01-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-01-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-02-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-02-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-03-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-03-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-04-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-04-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-05-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-05-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-06-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-06-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-07-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-07-SUMMARY.md
  - .planning/phases/02-full-rev-3-3-bench/02-08-PLAN.md
  - .planning/phases/02-full-rev-3-3-bench/02-08-SUMMARY.md
  - Makefile
  - README.md
  - components/sim_bus/__init__.py
  - components/sim_bus/binary_sensor.py
  - components/sim_bus/light.py
  - components/sim_bus/sensor.py
  - components/sim_bus/sim_binary_sensor.h
  - components/sim_bus/sim_bus.cpp
  - components/sim_bus/sim_bus.h
  - components/sim_bus/sim_light.cpp
  - components/sim_bus/sim_light.h
  - components/sim_bus/sim_sensor.cpp
  - components/sim_bus/sim_sensor.h
  - e2e/fans_test.go
  - e2e/isolation_test.go
  - e2e/leds_test.go
  - e2e/rev33_test.go
  - e2e/watch_test.go
  - examples/rev33.yaml
  - fans/noctua-nf-a12x25-pwm.toml
  - internal/apperr/codes.go
  - internal/bus/server.go
  - internal/bus/server_test.go
  - internal/cli/cli.go
  - internal/cli/cli_test.go
  - internal/cli/logs_test.go
  - internal/cli/watch_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/events/events.go
  - internal/events/events_test.go
  - internal/fwbuild/cache.go
  - internal/fwbuild/cache_test.go
  - internal/fwbuild/fwbuild.go
  - internal/fwbuild/fwbuild_test.go
  - internal/fwbuild/lint.go
  - internal/instance/instance.go
  - internal/instance/instance_test.go
  - internal/instance/lanhost.go
  - internal/instance/lanhost_test.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/supervisor/logs.go
  - internal/supervisor/logs_test.go
  - internal/supervisor/policy_test.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - internal/supervisor/watch.go
  - internal/supervisor/watch_test.go
  - protocol/PROTOCOL.md
  - protocol/vectors/frames.tsv
  - tests/conftest.py
  - tests/pyproject.toml
  - tests/silbench.py
  - tests/test_api_smoke.py
  - tests/test_contract.py
  - tests/test_identity.py
  - tests/tools/lockdiff.py
  - tests/uv.lock
covered_digest: "v2:sha256:d7b67a750807110a30dac60eb3049a3da74c48f5d4d5c21b69a1030a3489e2b5"
behavior_unverified: 0
overrides_applied: 0
human_verification:
  - test: "Live Home Assistant UAT (criterion 4, HA-01, HA-02): `bin/sil up ha-a`, add it in a real Home Assistant by `api.lan_host`:`api.port` and the `api.key` from `sil status`; set Fan 1 to 40 % in HA; run `sil set temperature=27 humidity=60` and `sil press usr1` (also `--down`/`--up`); `sil down ha-a && sil up ha-a`; `sil up ha-b`; delete both devices afterwards (7 steps in 02-08-SUMMARY.md, section Pending Live Home Assistant UAT)"
    expected: "Device `SiL ha-a` appears with the 13 entities and no light entities; `sil status` ports[0].duty is 0.4 and the Fan 1 Speed entity rises to about 790 RPM; Temperature, Humidity and Button USR1 follow within about a second; after down/up the same device reconnects with no duplicate; `ha-b` is a separate device"
    why_human: "Needs a real Home Assistant, ideally on another LAN machine. The repo only has the aioesphomeapi proxy on localhost. Real HA discovery, encryption-key handshake, device registry identity (name + MAC) and LAN reachability of lan_host:port are not exercised by any automated test."
  - test: "Review-fix concurrency changes (da1a9ca, e4f1046): `Set` with the engine dead or restarting (`engGap` flag), cancellation semantics of Set against SimRestart and shutdown"
    expected: "Set during a simulator restart gap reports a restartable error and leaves recorded inputs consistent; a dead engine is not misreported"
    why_human: "02-REVIEW-DISPOSITION.md marks both fixes `needs human verification`; the iteration-3 fixes (8542d9d, b976a6c) were never re-reviewed because the review loop hit its cap. This is a state-transition / cancellation invariant, not part of the 5 roadmap criteria, so it does not change the score."
---

# Phase 2: Full Rev 3.3 Bench Verification Report

**Phase Goal:** The complete Rev 3.3 IO surface runs in the sim and can be driven both from the CLI and from the user's own Home Assistant, with a contract test that proves the whole loop.
**Verified:** 2026-11-09T10:40:00Z
**Status:** human_needed
**Re-verification:** No, initial verification

## Goal Achievement

The goal splits into three claims. "Runs in the sim" and "driven from the CLI" are verified in code and by independent test runs. "Contract test proves the whole loop" is verified: `tests/test_contract.py` drives the whole chain (button, firmware, duty, plant, RPM, firmware, LED) through `sil` only, and I re-ran it. "Driven from the user's own Home Assistant" is only proxied by aioesphomeapi on localhost; no real HA was involved. That claim is `human_needed`, not passed.

### Observable Truths (ROADMAP success criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Example mirrors Rev 3.3: 4 fans, LED strip with board and per-fan `partition` lights, USR1-3, temp/humidity; compiles without `wifi:`; `external_components:`; HIL contract entity names; status LEDs internal | VERIFIED | `examples/rev33.yaml` read directly: no `wifi:` key (grep confirms), `external_components` with `type: local, path: ../components, components: [sim_bus]`, four `fan: platform: speed` named `Fan 1..4` (speed_count 100), four `Fan N Speed` sensors, `Temperature`/`Humidity` (mode latest), `Button USR1..3`, `sim_bus` light `internal: true` plus five `partition` lights (board with `pulse` and `addressable_rainbow` effects, `fan1..4_led`), all `internal: true`. That is exactly 13 exposed entities. Compile and entity set proven by e2e `TestRev33Entities`, `TestLEDCodegen` (full e2e 48/48 passed per orchestrator) and `tests/test_api_smoke.py::test_entity_set` (re-run by me, passed). |
| 2 | `sil status` full JSON; `sil set` temperature, humidity, per-port fan; `sil press usr1 [--long]`; `sil watch` NDJSON; `sil logs` | VERIFIED | `internal/cli/cli.go` defines `Status`, `Set` (key=value incl. `portN.fan=ID`), `Press` (`--long`, `--hold`, `--down/--up`), `Watch`, `Logs`, `Fans`; `bin/sil --help` lists them. Backed by unit tests (`internal/cli`, `control`, `supervisor` pass in my `make check` run) and e2e `TestSetVocabulary`, `TestEnvSet`, `TestPressTracer`, `TestPressVocabulary`, `TestWatchStream`, `TestLogs`, `TestCLIContract`. `tests/test_contract.py` step 3 asserts the watch snapshot carries every status key, step 4 sees button/duty events on the watch stream, steps 6/7 use `press --long` and `set port2.fan=`. |
| 3 | LED changes incl. partition and pulse appear in status and watch; idle firmware sends LED frames only on change, rate-capped | VERIFIED | `components/sim_bus/sim_light.cpp` lines 47-55: skips send when frame equals `last_sent_` and the bus is connected, and enforces `kMinGapMs` between frames (20 Hz, documented in `sim_light.h`). Wire-level proof in `e2e/leds_test.go::TestLEDFrames`: manifest segment list `board:0:0,fan1:1:1,...`, `pulse_rate` asserts each gap >= 50000 us, `idle_silence` asserts 0 LED frames in a 5 s idle window, rainbow effect obeys the same cap, replay on reconnect. `TestLEDStatus` covers status/watch visibility of the board pulse and per-fan colours; contract test asserts a blue-dominant boot pulse and fan 1 LED turning green. |
| 4 | User can add an instance to Home Assistant by host:port and key, control fans, sensors follow; restart reuses the same device; second instance is a separate device | UNCERTAIN (human needed) | Automated proxy exists and passes (re-run by me): `tests/test_api_smoke.py::test_fan_round_trip`, `test_inputs_visible_over_api`, `tests/test_identity.py::test_identity_stable_across_restart`, `test_second_instance_is_separate`; `api.lan_host` + HA hint in `sil up` (`TestHAHint`). These use aioesphomeapi on localhost, not Home Assistant. Per orchestrator and 02-08-SUMMARY.md, the live HA UAT was not done. Not marked passed. |
| 5 | One command runs the contract test and the aioesphomeapi smoke test, and both pass | VERIFIED | `make test-contract` (Makefile target: build, lock-check, `uv run --locked pytest` in `tests/`). I ran it: `collected 6 items`, `test_api_smoke.py ...`, `test_contract.py .`, `test_identity.py ..`, `6 passed in 22.36s`, contract session 22.4 s. `test_contract.py` is a substantive chain test (see Truth 2/3 evidence), not a stub. |

**Score:** 4/5 truths verified (0 present-but-behavior-unverified); truth 4 routed to human verification.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `examples/rev33.yaml` | Rev 3.3 host config | VERIFIED | Substantive (fan, sensor, binary_sensor, light, script, on_boot/api hooks); default config of `sil up` (`lifecycle.defaultConfig`) |
| `components/sim_bus/{binary_sensor,sensor,light}.py` + `sim_binary_sensor.h`, `sim_sensor.h`, `sim_light.{h,cpp}` | IO-03/04/05 platforms | VERIFIED | Present, non-trivial, wired through the `sim_bus` hub and loaded by the example; exercised by e2e on the real firmware build |
| `internal/cli/cli.go` | CLI verbs | VERIFIED | `Status/Set/Press/Watch/Logs/Fans` present and routed to control/supervisor |
| `internal/supervisor/{watch,logs}.go`, `internal/events/events.go` | watch and logs | VERIFIED | Present, unit tests pass |
| `internal/instance/lanhost.go` | `api.lan_host` | VERIFIED | `TestLANHost` passes in `make check` |
| `tests/test_contract.py`, `test_api_smoke.py`, `test_identity.py`, `silbench.py`, `conftest.py`, `uv.lock` | TEST-01/02 | VERIFIED | Present, run, 6 passed |
| `protocol/vectors/frames.tsv` + `tests/cpp/test_protocol.cpp` | codec conformance | VERIFIED | `make test-cpp` (in `make check`): 119 passed, 0 failed |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `examples/rev33.yaml` | `components/sim_bus` | `external_components` local path | WIRED | Built by e2e and the contract test |
| sim_bus sensor/binary_sensor/light/output | Go sim-bus server | NDJSON channels `portN.duty/rpm`, `temperature`, `humidity`, `usrN`, `leds` | WIRED | Contract chain observes duty/RPM/LED reaction end to end |
| `sil press` | firmware buttons | bus level frames, firmware click windows | WIRED | Contract steps 4-6 |
| `sil set portN.fan=` | engine plant swap | control API -> engine | WIRED | Contract step 7 (Noctua above 1700 rpm on port 2 only) |
| `sil status` -> HA address | `api.lan_host` | `instance.LANHost` -> supervisor Status -> lifecycle hint | WIRED | `TestHAHint`, `TestLANHost` |
| real Home Assistant -> firmware API | host:port + key | native API on 0.0.0.0 | NOT VERIFIED | No real HA exercised (see human item 1) |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| Fan N Speed sensor | `portN.rpm` | Go plant model driven by firmware duty | Yes (RPM within 2 % of the computed 789.5 at duty 0.4 in the contract test) | FLOWING |
| Temperature/Humidity | `sil set` values re-sent every 10 s | engine inputs | Yes (`test_inputs_visible_over_api`) | FLOWING |
| Status LED pixels | firmware light writes -> `leds` frames | real firmware | Yes (green/blue dominance asserted) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Static checks, unit tests with -race, C++ codec | `make check` (run by me at HEAD) | exit 0, all packages ok, `119 passed, 0 failed` | PASS |
| Contract + smoke + identity tests | `make test-contract` (run by me) | `6 passed in 22.36s` | PASS |
| CLI surface | `bin/sil --help` | lists up/status/down/sim/fw/press/set/fans/watch/logs | PASS |
| Full e2e | `make e2e` (not re-run by me; 537 s) | Orchestrator reports ok, 48 top-level tests; 02-08-SUMMARY records the same | ACCEPTED on orchestrator evidence, not independently re-run |

### Probe Execution

Step 7c: SKIPPED (no PLAN declares a `scripts/*/tests/probe-*.sh`; none exist).

### Requirements Coverage

All 15 phase IDs are claimed by at least one PLAN `requirements:` field and have a REQUIREMENTS.md entry. No orphaned Phase 2 requirements.

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| IO-03 | 02-01, 02-04 | binary_sensor USR1-3 short/long | SATISFIED | `binary_sensor.py`, `Button USR1..3` in example, contract steps 4-6 |
| IO-04 | 02-02, 02-04 | addressable light carries LED strip, partition lights, change-only and rate-capped | SATISFIED | `sim_light.cpp`, `TestLEDFrames` |
| IO-05 | 02-03, 02-04 | temperature/humidity sensor | SATISFIED | `sensor.py`, example `mode: latest` sensors, `test_inputs_visible_over_api` |
| FW-01 | 02-01, 02-02, 02-03, 02-07, 02-08 | Rev 3.3 example, no `wifi:` | SATISFIED | `examples/rev33.yaml` |
| FW-02 | 02-01, 02-02, 02-03 | HIL contract names, status LEDs internal | SATISFIED | 13 entities, all LED lights `internal: true`, `test_entity_set` |
| FW-03 | 02-01, 02-08 | `external_components:` local path now, `github://` later | SATISFIED | local form built and tested; `github://` form documented in README only (no tag exists; REL-02 is v2) |
| MODEL-03 | 02-05 | choose library fan per port | SATISFIED | `sil set portN.fan=ID`, `sil fans`, `TestFanSwap`, contract step 7 |
| CLI-01 | 02-01, 02-02, 02-03, 02-06, 02-08 | full status JSON | SATISFIED | `sil status`, snapshot-key check in contract test, `TestHAHint` |
| CLI-02 | 02-01, 02-03, 02-05 | `sil set`, `sil press [--long]` | SATISFIED | `TestSetVocabulary`, `TestPressVocabulary`, `TestPressTracer` |
| CLI-03 | 02-06 | `sil watch` NDJSON | SATISFIED | `TestWatchStream`, contract watch assertions |
| CLI-04 | 02-06 | `sil logs` | SATISFIED | `TestLogs`, `internal/cli/logs_test.go` |
| HA-01 | 02-07, 02-08 | add to HA by host:port + key; restart reuses device | NEEDS HUMAN | Automated identity tests pass (localhost aioesphomeapi); no live HA. REQUIREMENTS.md correctly still shows Pending |
| HA-02 | 02-07, 02-08 | control fans and see sensors from HA | NEEDS HUMAN | `test_fan_round_trip`/`test_inputs_visible_over_api` are a proxy only. Still Pending in REQUIREMENTS.md |
| TEST-01 | 02-07 | contract test | SATISFIED | `tests/test_contract.py`, 6 passed |
| TEST-02 | 02-07 | aioesphomeapi smoke test | SATISFIED | `tests/test_api_smoke.py`, passed |

REQUIREMENTS.md status matches this verdict: HA-01 and HA-02 are Pending, the other 13 are Complete. Do not tick HA-01/HA-02 until the human UAT is done.

### Anti-Patterns Found

Scanned the 61 non-planning files changed since the Phase 1 base (199fc81) for `TBD|FIXME|XXX`: none found. No stub patterns observed in the artifacts read.

| File | Pattern | Severity | Impact |
|------|---------|----------|--------|
| 9 open review info items (IN-01..IN-09, `02-REVIEW-DISPOSITION.md`) | e.g. `light.py` wrong pixel-size comment, `SIL_BUS` bypasses the 107-byte check, control handlers accept trailing JSON data, `tests/silbench.py` dead state, `sim_light.h` missing `<algorithm>` include | Info | Not goal blockers. Note IN-07 is a latent portability compile issue worth fixing early. |

### Human Verification Required

1. **Live Home Assistant UAT (Criterion 4, HA-01, HA-02)**
   **Test:** Follow the 7 steps in 02-08-SUMMARY.md: `bin/sil up ha-a`; add by `api.lan_host`:`api.port` + `api.key`; Fan 1 to 40 % in HA; `sil set temperature=27 humidity=60`; `sil press usr1` with `--down`/`--up`; `sil down ha-a && sil up ha-a`; `sil up ha-b`; clean up.
   **Expected:** One device with 13 entities and no light entities; values follow in both directions within about a second; the same device after restart, no duplicate; `ha-b` is a separate device.
   **Why human:** Needs a real Home Assistant, ideally on a different LAN machine. The repo only tested aioesphomeapi on localhost.

2. **Review-fix concurrency logic (not a roadmap criterion)**
   **Test:** Exercise `sil set` while the simulator restarts (`sil sim restart`) and while the engine is dead or stopping.
   **Expected:** Errors are accurate and recorded inputs stay consistent; no stuck `engGap`.
   **Why human:** The disposition lists da1a9ca and e4f1046 as needing human verification, and 8542d9d/b976a6c were not re-reviewed. IN-09 (a Set racing shutdown reports `engine_failed` instead of `stopping`) is open.

### Gaps Summary

No automated gaps. Criteria 1, 2, 3 and 5 are verified against code and by tests I ran myself (`make check` exit 0, `make test-contract` 6 passed). The full e2e (537 s, 48 top-level tests) is accepted on the orchestrator's evidence and the identical 02-08 acceptance record; I did not re-run it. Criterion 4 (and with it HA-01 and HA-02) cannot be closed without a live Home Assistant, so the phase is `human_needed`, not `passed`. Treat the claim "the user can add an instance to their own Home Assistant" as unproven until the UAT in item 1 is done.

---

_Verified: 2026-11-09T10:40:00Z_
_Verifier: Claude (gsd-verifier)_
