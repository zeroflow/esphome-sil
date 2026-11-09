---
phase: 02-full-rev-3-3-bench
plan: 08
subsystem: infra
tags: [home-assistant, lan-host, default-config, readme, acceptance, esphome-host]

requires:
  - phase: 02-full-rev-3-3-bench
    provides: examples/rev33.yaml, contract and smoke tests (02-07), LED codegen and lint (02-04)
provides:
  - api.lan_host (default-route IPv4, null when unknown) in sil status and the sil up JSON
  - Home Assistant hint on stderr of sil up with lan_host, port and key location
  - examples/rev33.yaml as the default firmware config of sil up
  - Phase 2 lint hints for esp32_rmt_led_strip and i2c
  - README with quick start, Rev 3.3 example, Home Assistant guide, consumer switch, contract tests, fan library
  - Recorded Phase 2 acceptance run and criterion-to-test map
affects: [phase-03-web-ui, verify-work phase 2, release]

actuals:
  tokens: 5600
  tasks: 2
  commits: 3
plan_head_before: 235b9c0692f2e5323067512bf6bcbf5385853d2e
plan_head_after: 49ebc3901431af017a9c056b624c16feb6d0f0d5
commits: 3

tech-stack:
  added: []
  patterns:
    - "lan_host by UDP connect to a TEST-NET-1 address: the kernel names the default-route source address, nothing is sent"
    - "nullable JSON field as *string (null = unknown) shared by control.APIStatus and lifecycle.APIInfo"

key-files:
  created:
    - internal/instance/lanhost.go
    - internal/instance/lanhost_test.go
  modified:
    - internal/supervisor/supervisor.go
    - internal/control/control.go
    - internal/lifecycle/lifecycle.go
    - internal/cli/cli.go
    - internal/fwbuild/lint.go
    - README.md
    - e2e/rev33_test.go

key-decisions:
  - "lan_host is computed once in Serve (status) and once in Up (hint and result); both use instance.LANHost, which has no timeout fields so the wall-clock lint stays satisfied"
  - "HA-01 and HA-02 are not marked complete: only the automatable parts and a aioesphomeapi proxy ran; the live Home Assistant UAT stays a human item"

requirements-completed: [FW-01, FW-03, CLI-01]
requirements-pending-live-uat: [HA-01, HA-02]

coverage:
  - id: D1
    description: "sil status and sil up carry api.lan_host (null when unknown); api.host stays 127.0.0.1; the sil up hint on stderr names lan_host (or 127.0.0.1) and the port"
    requirement: "CLI-01"
    verification:
      - kind: unit
        ref: "internal/instance/lanhost_test.go#TestLANHost"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestHAHint"
        status: pass
    human_judgment: false
  - id: D2
    description: "sil up without --config builds examples/rev33.yaml; the lint hints point to the Phase 2 platforms"
    requirement: "FW-01"
    verification:
      - kind: e2e
        ref: "make e2e (full, 48 top-level tests, 0 failures): TestRev33Entities, TestPressTracer, TestHAHint"
        status: pass
      - kind: unit
        ref: "internal/fwbuild/lint_test.go#TestBannedComponents (hint non-empty)"
        status: pass
    human_judgment: false
  - id: D3
    description: "README documents the external_components block, the github://zeroflow/esphome-sil@vX.Y switch, the HA steps, quick start, contract tests and fan library"
    requirement: "FW-03"
    verification:
      - kind: other
        ref: "grep -n 'github://zeroflow/esphome-sil' README.md; grep -n 'Add Integration' README.md; grep -n 'sil watch' README.md; grep -n 'test-contract' README.md"
        status: pass
    human_judgment: true
    rationale: "Documentation adequacy is a reading judgment; the github:// form cannot be built until a tag exists (REL-02 is v2), only the local form is exercised"
  - id: D4
    description: "Live Home Assistant UAT (7 steps) and reachability of lan_host:port from another machine of the LAN"
    requirement: "HA-01"
    verification: []
    human_judgment: true
    rationale: "Needs a real Home Assistant on another machine; only localhost and aioesphomeapi on this host were exercised"

duration: 25min
completed: 2026-11-09
status: complete
---

# Phase 2 Plan 08: Home Assistant reachability, Rev 3.3 default and Phase 2 acceptance Summary

**api.lan_host from a default-route UDP probe in status, the `sil up` hint and the README HA guide, examples/rev33.yaml as the default bench, and a green recorded Phase 1 + Phase 2 acceptance run (full e2e, image C++ test, lock check, contract tests at 22 s warm, module verify, go.mod/go.sum untouched).**

## Performance

- **Duration:** about 25 min plus about 20 min of e2e wall time inside it (two full e2e runs)
- **Started:** 2026-11-09T08:23Z
- **Completed:** 2026-11-09T08:48Z
- **Tasks:** 2
- **Files modified:** 9 (2 created)

## Accomplishments

- A Home Assistant on another machine gets a usable address: `instance.LANHost()` dials `udp4` to 192.0.2.1:9 (a documentation address, no packet is sent) and returns the local IPv4 when it is neither loopback nor unspecified. `sil status` reports it as `api.lan_host` (null when unknown), `sil up` prints the hint `add to Home Assistant: host <lan_host or 127.0.0.1> port <port>, encryption key from this JSON (no mDNS on host; ...)` on stderr and carries `lan_host` in its JSON.
- `sil up NAME` now builds `examples/rev33.yaml` by default; `--config examples/fan-skeleton.yaml` still gives the minimal four-fan config; the `--config` help names both.
- Lint hints: `esp32_rmt_led_strip` -> light platform `sim_bus` with `partition` lights, `i2c` -> sensor platform `sim_bus` with `mode: latest`.
- README rewritten from the idea-stage text: requirements, quick start (one example per verb), the Rev 3.3 example with its behaviour table, the `external_components` block and the `github://zeroflow/esphome-sil@vX.Y` switch, contract tests, fan library, and a Home Assistant section (add, stable device across down/up, second device, removal and identity reset, LAN and key security note).
- Phase 2 acceptance recorded below.

## Task Commits

1. **Task 1 RED: LANHost unit test and TestHAHint** - `600f325` (test; `TestLANHost` failed to build with `undefined: LANHost`)
2. **Task 1 GREEN: lan_host in status and the sil up hint, README HA guide** - `9d86944` (feat)
3. **Task 2: Rev 3.3 default, lint hints, README bench docs, Rule 3 pin** - `49ebc39` (feat)

**Plan metadata:** the docs commit that carries this file.

## Files Created/Modified

- `internal/instance/lanhost.go` - `LANHost()`
- `internal/instance/lanhost_test.go` - `TestLANHost`
- `internal/supervisor/supervisor.go` - `lanHost` computed in Serve, reported by Status
- `internal/control/control.go` - `APIStatus.LanHost` (`lan_host`)
- `internal/lifecycle/lifecycle.go` - `APIInfo.LanHost`, new hint line, `defaultConfig = "examples/rev33.yaml"`
- `internal/cli/cli.go` - `--config` help
- `internal/fwbuild/lint.go` - Phase 2 hints
- `README.md` - docs
- `e2e/rev33_test.go` - `TestHAHint`; pin in `TestPressVocabulary/input_not_available`

## Decisions Made

- `instance.LANHost` uses a zero-value `net.Dialer` (no Timeout/Deadline fields) so the wall-clock lint holds; a UDP connect never waits for the network.
- HA-01 and HA-02 are left pending: automatable parts are green, the live Home Assistant UAT is a human item and is not claimed.
- Requirement IDs `requirements-completed` lists only FW-01, FW-03 and CLI-01; HA-01/HA-02 appear under `requirements-pending-live-uat`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Phase 2 test relied on the old default's smaller IO surface**
- **Found during:** Task 2 (first full `make e2e`)
- **Issue:** `TestPressVocabulary/input_not_available` brought up an instance without `--config` and expected a firmware with no buttons (`input_not_available`). With the new default it got the Rev 3.3 bench and `sil press usr1` exited 0.
- **Fix:** the test pins `--config examples/fan-skeleton.yaml`, as the plan anticipated. No Phase 1 test needed a pin; the default was not reverted.
- **Files modified:** `e2e/rev33_test.go`
- **Verification:** `make e2e E2E_RUN=TestPressVocabulary` passes, then the complete second `make e2e` passes with 0 failures.
- **Committed in:** `49ebc39`

---

**Total deviations:** 1 auto-fixed (1 Rule 3)
**Impact on plan:** none beyond the recorded test pin.

## Acceptance Record (this host, 2026-11-09)

| Step | Command | Result |
|------|---------|--------|
| 1 | `make check` | exit 0: gofmt clean, vet (incl. e2e tag), `go test -race ./...` all packages ok, host C++ codec test `119 passed, 0 failed` |
| 2 | `make test-cpp-image` | exit 0: g++ (Debian 14.2.0-19) in the pinned image, `119 passed, 0 failed` |
| 3 | `make e2e` (no filter), first run | FAIL: only `TestPressVocabulary` (the Rule 3 case above), 47 other top-level tests passed, 622 s |
| 3 | `make e2e` (no filter), after the pin | exit 0: `ok github.com/zeroflow/esphome-sil/e2e 538.983s`, 48 top-level `--- PASS`, 0 `--- FAIL` |
| 4 | `make lock-check` | exit 0: 20 packages checked, `missing: []`, `mismatched: []` |
| 5 | `make test-contract` (run 1) | exit 0: `6 passed`, `contract session: 23.2 s`, wall 24 s |
| 6 | `make test-contract` (run 2, warm) | exit 0: `6 passed`, `contract session: 22.2 s` (budget below 60 s), wall 23 s |
| 7 | `make mod-verify` | exit 0: `all modules verified` |
| 8 | `git diff --stat 199fc81 -- go.mod go.sum` | prints nothing: no new Go dependency in Phase 2 |

Task 1 gate: `make check && make e2e E2E_RUN='TestHAHint|TestTracer|TestCLIContract'` passed (`--- PASS: TestHAHint`, `TestTracer`, `TestCLIContract`); `make test-pkg PKG=./internal/instance/ RUN=TestLANHost` printed `ok`.

## Phase 2 Criterion-to-Test Map

| Criterion | Proving tests |
|-----------|---------------|
| 1. Shipped example mirrors Rev 3.3, compiles without `wifi:`, uses `external_components`, exposes the 13 contract entities, LEDs internal | e2e `TestRev33Entities`, `TestLEDCodegen`, `TestBannedComponents`; contract `tests/test_api_smoke.py::test_entity_set`; default config via `TestHAHint`, `TestPressTracer` |
| 2. `sil status` full run status; `sil set`; `sil press [--long]`; `sil watch`; `sil logs` | e2e `TestSetVocabulary`, `TestEnvSet`, `TestFanSwap`, `TestPressTracer`, `TestPressVocabulary`, `TestWatchStream`, `TestLogs`, `TestCLIContract`, `TestHAHint` (api.lan_host) |
| 3. LED changes incl. `partition` and `pulse` in status and watch; idle frames only on change, capped | e2e `TestLEDStatus`, `TestLEDFrames`, `TestLEDCodegen`, `TestWatchStream` |
| 4. Instance usable from Home Assistant; restart keeps the device; second instance separate | automated proxy only: `tests/test_identity.py::test_identity_stable_across_restart`, `test_second_instance_is_separate`, `tests/test_api_smoke.py::test_fan_round_trip`, `test_inputs_visible_over_api`; e2e `TestHAHint` (address and hint). The live Home Assistant UAT is pending (below) |
| 5. One command runs contract and smoke tests, both pass | `make test-contract`: `tests/test_contract.py::test_closed_loop_chain` (TEST-01) and `tests/test_api_smoke.py` (TEST-02), `6 passed`, twice |

## Pending Live Home Assistant UAT (D-15, criterion 4) - NOT verified

Pending maintainer; nothing here was exercised against a real Home Assistant. Only localhost and aioesphomeapi on this host ran.

1. `bin/sil up ha-a`, note `api.lan_host`, `api.port`, `api.key`.
2. Add it in HA by host:port + key -> device `SiL ha-a` with the 13 entities and no light entities.
3. Fan 1 to 40 % in HA -> `sil status` `ports[0].duty` 0.4 and the RPM entity rises to about 790.
4. `sil set temperature=27 humidity=60` -> HA follows within a second; `sil press usr1` -> Fan 1 50 %; `--down` / `--up` toggles `Button USR1` in HA.
5. `sil down ha-a && sil up ha-a` -> same device, no duplicate, reconnects by itself.
6. `sil up ha-b` -> a separate device.
7. Delete both devices in HA, `sil down ha-a ha-b`.

Also unverified: that a Home Assistant on a different machine of the LAN reaches `api.lan_host:api.port` with the key; and `github://zeroflow/esphome-sil@vX.Y` (no tag exists yet, only the local path form is built and tested). Research A8 (default-route detection on hosts without internet) is covered by the null path: when no default route exists `lan_host` is null and the hint falls back to 127.0.0.1.

## Issues Encountered

None beyond the Rule 3 test pin. `TestLANHost` skips (not fails) on a host with no default route.

## Known Stubs

None. The README `vX.Y` placeholder is intentional documentation of the consumer switch until a release tag exists.

## Threat Flags

None. The new surface (printing the LAN address next to the key) is covered by T-02-20 to T-02-22: encryption stays mandatory, README states the port is LAN-reachable and should be firewalled, status stays on the 0600 api.sock.

## User Setup Required

None - no external service configuration required. The live Home Assistant UAT above is a manual verification item.

## Next Phase Readiness

- Phase 2 is complete as far as automation goes: all five criteria are mapped to passing tests except the live-HA half of criterion 4.
- Ready for phase verification (`/gsd-verify-work 2`), which should carry the live HA UAT as the human item. Phase 3 (web UI) can reuse `api.lan_host` and the unchanged status schema.

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*

## Self-Check: PASSED

- Created files exist: `internal/instance/lanhost.go`, `internal/instance/lanhost_test.go`
- Commits exist: `600f325`, `9d86944`, `49ebc39`
- Acceptance criteria re-run: `defaultConfig = "examples/rev33.yaml"`, lint hint lines, `test-contract`, `github://zeroflow/esphome-sil`, `sil watch` greps all match; `git diff --stat 199fc81 -- go.mod go.sum` empty
