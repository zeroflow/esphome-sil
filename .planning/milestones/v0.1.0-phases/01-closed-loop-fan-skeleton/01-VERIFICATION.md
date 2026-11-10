---
phase: 01-closed-loop-fan-skeleton
verified: 2026-11-10T09:36:00Z
status: passed
score: 7/7 must-haves verified (5/5 roadmap success criteria, 2/2 tracked backstop truths; 1 by override)
behavior_unverified: 0
overrides_applied: 1
overrides:
  - must_have: "The hub drops the connection and reconnects with backoff when its pending TX buffer exceeds 64 KiB, so a stalled simulator can never block or grow the firmware loop"
    reason: "Maintainer skipped UAT test 5 with the reason 'skip it -> good enough for now' (01-UAT.md). The drop path is present (components/sim_bus/sim_bus.h kMaxTx = 65536, sim_bus.cpp flush_ closes the fd when tx_ exceeds it) but no automated test exercises the overflow. Not a roadmap success criterion (BUS-03 core: non-blocking, backoff, path-length validation is test-covered)."
    accepted_by: "maintainer (01-UAT.md test 5, skipped with reason)"
    accepted_at: "2026-11-10T09:19:48Z"
re_verification:
  previous_status: human_needed
  previous_score: "5/5 roadmap success criteria verified; 0 failed"
  reason: "Stale fingerprint: Phase 2 and 3 changed shared covered files (engine, control, supervisor, bus server, sim_bus hub, fwbuild, instance, lifecycle, harness)"
  gaps_closed: []
  gaps_remaining: []
  regressions: []
  human_items_resolved:
    - "Provenance of flynn/noise and go.yaml.in/yaml/v3: UAT test 1 pass"
    - "ASSUMED fan parameters accepted as M1 placeholders: UAT test 2 pass"
    - "Real failing compile maps to compile_failed: UAT test 3 pass"
    - "Startup-window orphan and nohup/SIGHUP behaviour: UAT test 4 pass"
    - "chan manifest order stable across rebuilds: UAT test 6 pass (observed, two --rebuild builds with differing sha256, identical chan order)"
    - "64 KiB TX-buffer drop: UAT test 5 skipped by maintainer, recorded as override"
covered_files:
  - ".planning/phases/01-closed-loop-fan-skeleton/01-01-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-01-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-02-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-02-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-03-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-03-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-04-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-04-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-05-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-05-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-06-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-06-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-07-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-07-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-08-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-08-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-09-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-09-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-10-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-10-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-11-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-11-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-12-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-12-SUMMARY.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-13-PLAN.md"
  - ".planning/phases/01-closed-loop-fan-skeleton/01-13-SUMMARY.md"
  - "Makefile"
  - "cmd/sil/main.go"
  - "components/sim_bus/__init__.py"
  - "components/sim_bus/output.py"
  - "components/sim_bus/sensor.py"
  - "components/sim_bus/sim_bus.cpp"
  - "components/sim_bus/sim_bus.h"
  - "components/sim_bus/sim_output.cpp"
  - "components/sim_bus/sim_output.h"
  - "components/sim_bus/sim_protocol.h"
  - "components/sim_bus/sim_sensor.cpp"
  - "components/sim_bus/sim_sensor.h"
  - "e2e/api_client.py"
  - "e2e/build_test.go"
  - "e2e/cli_test.go"
  - "e2e/codegen_test.go"
  - "e2e/firmware_test.go"
  - "e2e/harness_test.go"
  - "e2e/io_test.go"
  - "e2e/isolation_test.go"
  - "e2e/loop_test.go"
  - "e2e/model_test.go"
  - "e2e/restart_test.go"
  - "e2e/supervise_test.go"
  - "e2e/testdata/with-i2c.yaml"
  - "e2e/testdata/with-ledc.yaml"
  - "e2e/testdata/with-wifi-ota.yaml"
  - "e2e/tracer_test.go"
  - "examples/fan-skeleton.yaml"
  - "fans/arctic-p12-pwm-pst.toml"
  - "fans/embed.go"
  - "internal/apiclient/probe.go"
  - "internal/apiclient/probe_test.go"
  - "internal/apperr/apperr.go"
  - "internal/apperr/codes.go"
  - "internal/apperr/codes_test.go"
  - "internal/bus/codec.go"
  - "internal/bus/codec_test.go"
  - "internal/bus/server.go"
  - "internal/bus/server_test.go"
  - "internal/cli/cli.go"
  - "internal/cli/cli_test.go"
  - "internal/cli/resolve.go"
  - "internal/clock/clock.go"
  - "internal/clock/manual.go"
  - "internal/clock/manual_test.go"
  - "internal/control/control.go"
  - "internal/control/control_test.go"
  - "internal/control/operations.go"
  - "internal/engine/engine.go"
  - "internal/engine/engine_test.go"
  - "internal/fanlib/fanlib.go"
  - "internal/fanlib/fanlib_test.go"
  - "internal/fwbuild/cache.go"
  - "internal/fwbuild/cache_test.go"
  - "internal/fwbuild/elf.go"
  - "internal/fwbuild/elf_test.go"
  - "internal/fwbuild/fwbuild.go"
  - "internal/fwbuild/fwbuild_test.go"
  - "internal/fwbuild/lint.go"
  - "internal/fwbuild/lint_test.go"
  - "internal/fwbuild/testdata/resolved-example.yaml"
  - "internal/instance/instance.go"
  - "internal/instance/instance_test.go"
  - "internal/instance/lock.go"
  - "internal/instance/port.go"
  - "internal/lifecycle/lifecycle.go"
  - "internal/lifecycle/lifecycle_test.go"
  - "internal/lint/wallclock_test.go"
  - "internal/model/fan.go"
  - "internal/model/fan_test.go"
  - "internal/supervisor/firmware.go"
  - "internal/supervisor/policy.go"
  - "internal/supervisor/policy_test.go"
  - "internal/supervisor/supervisor.go"
  - "internal/supervisor/supervisor_test.go"
  - "protocol/PROTOCOL.md"
  - "protocol/vectors/frames.tsv"
  - "tests/cpp/test_protocol.cpp"
covered_digest: "v2:sha256:46575b1c39eb308dccfd440336887728f4fec643f1a30c88d7ca2bd6abc84401"
---

# Phase 1: Closed-Loop Fan Skeleton Verification Report

**Phase Goal:** Developer runs `sil up` and real ESPHome firmware, built in the pinned image and running directly on the host, drives simulated fans whose RPM flows back over the sim-bus; the loop is observable via `sil status`, survives restarts of either side, and stops cleanly with `sil down`
**Verified:** 2026-11-10T09:36:00Z (code at 784fbcf; 35495eb landed during the run and only touches `.planning/phases/02-*/02-UAT.md`)
**Status:** passed
**Re-verification:** Yes. The previous report (2026-11-09, `human_needed`, no gaps) had a stale fingerprint because Phases 2 and 3 changed shared covered files. Every truth was re-checked against the current code with fresh test runs.

All five roadmap success criteria still hold after Phases 2 and 3. The six human items of the previous report are settled by `01-UAT.md` (5 pass, 1 skipped with a reason, recorded as an override). No gaps.

## Evidence run by the verifier (not taken from SUMMARY.md)

| Check | Command | Result |
| ----- | ------- | ------ |
| Format, vet (incl. `sil_e2e`, `sil_ui` tags), unit tests with race detector, host C++ codec test | `make check` | exit 0, 17 Go packages ok, C++ `119 passed, 0 failed` |
| C++ codec with the pinned image's gcc | `make test-cpp-image` | `g++ (Debian 14.2.0-19) 14.2.0`, `119 passed, 0 failed` (same `frames.tsv` as the Go test) |
| Module checksums | `make mod-verify` | `all modules verified` |
| Pinned image present by digest | `docker image inspect` | `sha256:eab8cd6b…f1db64` |
| Full e2e, unfiltered (all phases), real firmware builds in the pinned image | `make e2e` | exit 0, 746 s, 57 of 57 top-level tests PASS, 0 FAIL, 0 SKIP |
| Debt markers (TBD/FIXME/XXX/TODO/HACK) in tracked source outside `.planning`, `prototype`, vendor | grep | none |
| Root-owned files in the e2e cache root | `find -user root` | none |
| Working tree after the run | `git status --short` | only the pre-existing `.planning/config.json`, `.gsd/`, `milestone.lock`, `state.json` |
| Leftover processes | `pgrep sil serve / program` | No e2e leftovers. One live instance `ct-main` (Phase 2 pytest contract test, `tests/test_contract.py`), started 09:34:59Z from a freshly rebuilt `bin/sil` by a process outside this verification. The e2e harness uses its own temp binary and cache root, so the instance is not an e2e leak. Not touched. |

## MVP mode discrepancy (warning, not a codebase gap)

ROADMAP marks Phase 1 `**Mode:** mvp`, but `user-story.validate` rejects the goal: it is not phrased as "As a …, I want to …, so that …." The strict MVP rule says to refuse verification until `/gsd mvp-phase 1` rewrites the goal. This re-run went ahead anyway: the orchestrator asked for the regeneration of a phase that is already accepted, and the previous verification and the UAT also ran against this goal text. The flow below is derived from the goal sentence instead of a user-story outcome clause. **Decision for the maintainer:** either reformat the goal with `/gsd mvp-phase 1`, or drop `mode: mvp` from Phase 1 in ROADMAP.md. Neither changes the codebase verdict.

## User Flow Coverage

Flow (derived from the goal): developer runs `sil up`, the firmware drives the fans, RPM comes back, `sil status` shows the loop, restarts of either side recover, `sil down` stops everything.

| Step | Expected | Evidence | Status |
| ---- | -------- | -------- | ------ |
| Run `sil up <name>` | Build in the pinned image, static binary, runs on the host, ready after an encrypted handshake | `TestTracer`, `TestStaticAndClean`, `TestReadyIsEncrypted`, `TestBuildCache`, `TestOfflineSeeded` (all PASS today) | VERIFIED |
| Command a fan | aioesphomeapi fan speed becomes the duty on the bus and in the engine | `TestLoop` (four fans), `TestFirmwareBus` | VERIFIED |
| RPM flows back | Model RPM goes over the bus to the firmware sensor as a 1 s mean, first-order lag, 0 below start | `TestRPMMean`, `TestModelThreshold`, `internal/model` and `internal/engine` unit tests | VERIFIED |
| Observe with `sil status` | Duty, rpm and key are visible, answered by the control API | `TestCLIContract`, `TestStatusDuringRestart`, `internal/cli` fake-server tests | VERIFIED |
| Restart either side | Resync without manual steps, API responsive during the gap | `TestSimRestartResync`, `TestFwRestartResync`, `TestRestartEitherOrder`, `TestSimGapAPIResponsive`, `TestFirmwareCrashAutoRestart` | VERIFIED |
| Run `sil down` | Both processes gone, no orphans | `TestSupervisorSIGTERM`, `TestSupervisorKillNoOrphan`, `TestDownFallback`, `TestTwoInstances` | VERIFIED |
| Outcome | The closed loop runs locally on real firmware with simulated fans, is observable, and stops cleanly | All steps above pass in one unfiltered e2e run | VERIFIED |

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | SC1: `sil up <name>` builds the example in the pinned image as a static binary, runs it on the host, reports ready only after a real encrypted handshake; the firmware stays alive with no API client; no root-owned files | VERIFIED | e2e `TestTracer`, `TestStaticAndClean`, `TestReadyIsEncrypted` (wrong key refused, PID alive 10 s with no client), `TestBuildCache`, `TestOfflineSeeded`. Pinned digest in `Makefile` and fwbuild; `elf.go` static check; `apiclient/probe.go` (unchanged since last verification). Note: since Phase 2 (49ebc39) the default config is `examples/rev33.yaml`. The Phase 1 `sil up` flows now exercise Rev 3.3, which pins the same `-static`, `reboot_timeout: 0s`, `encryption` and `port1-4.duty/rpm`. `fan-skeleton.yaml` (unchanged) is still exercised via `sil up --config` (`TestRev33Entities/input_not_available`), the harness `buildFirmware` (`TestFirmwareBus`, `TestHubSession`, `TestFakePeer`) and `TestCodegenValidation/fan-skeleton`. |
| 2 | SC2: with the key from `sil status`, an API client sets any of four fans; status shows the duty; firmware RPM (1 s mean) rises with first-order lag and stays 0 below start; model advances only via the injectable clock; lint fails on wall-clock calls | VERIFIED | e2e `TestLoop`, `TestModelThreshold`, `TestRPMMean`. Unit tests: `internal/model`, `internal/engine` with `clock.Manual`, `internal/lint/wallclock_test.go`, all green in `make check` with the Phase 2/3 code included. The key is still served on the socket status; the Phase 3 LAN handler redacts it (`internal/web/web.go` `redacting`), so SC2 is unaffected. |
| 3 | SC3: restart of simulator or firmware in either order resyncs; firmware API responsive while the simulator is gone; same golden vectors in Go and C++ | VERIFIED | e2e `TestSimRestartResync`, `TestFwRestartResync`, `TestRestartEitherOrder`, `TestSimGapAPIResponsive`, `TestStatusDuringRestart`, `TestHubSession`, `TestFakePeer`. `protocol/vectors/frames.tsv` (now with Phase 2 rgb rows) passes in `internal/bus/codec_test.go`, host g++ and the image gcc 14.2 (119/119). |
| 4 | SC4: two named instances side by side with their own run dir, port, MAC, name and key; taken port exits 2 with JSON; `sil down` stops both; killing the simulator leaves no orphan; fresh prefs per run | VERIFIED | e2e `TestTwoInstances`, `TestPrefixNames`, `TestPortInUse` (strengthened since the last run: a refused busy `--api-port` is not persisted), `TestPortClaimed`, `TestFreshPrefs`, `TestSupervisorKillNoOrphan`, `TestSupervisorSIGTERM`, `TestDownFallback`, `TestAlreadyRunningAndBusy`. `supervisor/firmware.go` (Setpgid, Pdeathsig, LockOSThread) and `policy.go` unchanged since the last verification. |
| 5 | SC5: misconfiguration fails loudly: host-incompatible component, sim platform without `channel:`, over-long socket path, unknown `fan.toml` key | VERIFIED | e2e `TestBannedComponents` (wifi, ledc, i2c), `TestCodegenValidation` (missing channel, `sun_path limit is 107`, duplicate channel now also across binary_sensor/light), `TestNamesAndPaths` (`socket_path_too_long`), `TestFanTomlUnknownKey`. Unit `fwbuild` lint, image-missing-never-pulls and stage-classification tests green. |
| 6 | 01-10 backstop: chan manifest order is stable across rebuilds | VERIFIED | Direct observation by the maintainer at current code (01-UAT.md test 6): two `--rebuild` builds with different program sha256 (40c193cc… vs 13a3ef01…), both hellos `chans=8`, identical chan order port1-4.duty (out), port1-4.rpm (in). The hub still emits chan frames by iterating `children_` in registration order (`sim_bus.cpp` `try_connect_`); Phase 2 only added the `type` and the optional `n`/`seg` fields. |
| 7 | 01-10 backstop: the hub drops the connection and reconnects with backoff when its pending TX exceeds 64 KiB | PASSED (override) | Override: maintainer skipped UAT test 5 "good enough for now". Accepted by maintainer (01-UAT.md) on 2026-11-10T09:19:48Z. Code present: `sim_bus.h:46` `kMaxTx = 65536`, `sim_bus.cpp:118` closes the connection when `tx_` exceeds it after a non-blocking send. Still no automated test exercises overflow. |

**Score:** 7/7 tracked truths verified (6 by test or observation, 1 by override); 0 present but behavior-unverified.

The other PLAN-level truths of 01-01..01-13 are covered by the named unit and e2e tests listed in the previous report. All of those tests are part of today's green `make check` and unfiltered `make e2e`. 01-13's acceptance truth ("`make check`, `make test-cpp-image` and `make e2e` without a filter are green") was re-established today.

### Behavior-dependent truths

The cancellation, cleanup and ordering invariants (firmware group stopped first on shutdown, no orphan on supervisor death, restart-in-flight ordering, the newest bus connection replaces the older one, engine drop-and-recreate) each have a named test that passed in today's run (`TestSupervisorSIGTERM`, `TestSupervisorKillNoOrphan`, `TestRestartEitherOrder`, `internal/bus` server tests, `TestSimRestartResync`).

### Prohibitions

| Plan | Prohibition | Tier | Disposition | Enforcement evidence |
| ---- | ----------- | ---- | ----------- | -------------------- |
| 01-05 | MUST NOT silently ignore/default/auto-correct an unknown, missing or invalid fan.toml key | test | VERIFIED | `internal/fanlib/fanlib_test.go` (located `unknown field max_rmp`, range/required cases), e2e `TestFanTomlUnknownKey` |
| 01-05 | MUST NOT present assumed fan constants as measurements | judgment | RESOLVED (human) | `fans/arctic-p12-pwm-pst.toml` labels start_duty, tau_up, tau_down `ASSUMED` next to the cited sources; maintainer accepted them in UAT test 2 |
| 01-08 | MUST NOT auto-pull/substitute the image, strip a banned component, or continue after a failed lint stage | test | VERIFIED | `TestBuildImageMissingNeverPulls`, `TestBuildDockerUnavailable`, `TestLintConfigBanned`, `TestLintStorage`, `TestBuildConfigFailureIsRedactedAndRepeatable`, e2e `TestBannedComponents` |
| 01-08 | MUST NOT depend on network after the one-time archive fetch | test | VERIFIED | `fwbuild_test.go` asserts `--network none` on the config/generate stages; e2e `TestOfflineSeeded` |
| 01-09 | MUST NOT silently move port, regenerate key or change MAC | test | VERIFIED | e2e `TestPortInUse` (sticky port never moves), `TestPortClaimed`, `internal/instance` tests |
| 01-09 | MUST NOT give an identity that can impersonate a real device (sil- prefix, 02: MAC) | test | VERIFIED | `instance_test.go:327` (02: locally administered), e2e `TestTwoInstances`/isolation (`sil-<name>` device names) |

### Required Artifacts

| Artifact | Status | Details |
| -------- | ------ | ------- |
| `components/sim_bus/*` (hub, output, sensor, codec header) | VERIFIED | Substantive. Phase 2 extended it (rgb channels, `mode: latest` sensor) without changing the Phase 1 paths (duty out, rpm window mean, backoff, TX cap). Built and run by e2e. |
| `examples/fan-skeleton.yaml`, `fans/arctic-p12-pwm-pst.toml` | VERIFIED | Unchanged since the last verification; built and loaded by e2e |
| `internal/{fwbuild,instance,lifecycle,supervisor,engine,model,fanlib,bus,control,cli,clock,apiclient,apperr,lint}` | VERIFIED | Wired from `cmd/sil/main.go`, exercised end to end. `model`, `clock`, `lint`, `apiclient` unchanged; the others grew for Phases 2/3 and all their tests pass. |
| `internal/control/operations.go` (new, Phase 3) | VERIFIED | Now carries the Phase 1 routes `GET /api/v1/status`, `POST /api/v1/sim/restart`, `POST /api/v1/fw/restart` from one operation table; added to `covered_files` |
| `protocol/PROTOCOL.md`, `protocol/vectors/frames.tsv` | VERIFIED | Shared by the Go and C++ tests, both green |
| `Makefile` targets check, test-cpp, test-cpp-image, mod-verify, e2e | VERIFIED | All ran today |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `sil up` | fwbuild, detached `sil serve`, firmware | `lifecycle.go` to `supervisor.go` to `firmware.go` | WIRED | TestTracer, TestTwoInstances |
| firmware sim_bus hub | `bus.Server` / engine | hello/chan/out/sync and welcome/in/sync | WIRED | TestHubSession, TestFirmwareBus |
| engine | `model.Step` on injected clock ticks | `clock.Clock` | WIRED | engine tests with `clock.Manual` |
| `sil status` / `sim restart` / `fw restart` | control API on api.sock | `control.Client` to `operations.go` table | WIRED | TestCLIContract, TestStatusDuringRestart, cli fake-server tests |
| supervisor shutdown | firmware group, run.json, sockets | signal handler, Setpgid/Pdeathsig | WIRED | TestSupervisorSIGTERM, TestSupervisorKillNoOrphan |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
| -------- | ---- | ------ | --------- | ------ |
| `sil status` ports[].duty | firmware `out` frames from the native API fan command | bus codec to engine snapshot | yes (TestLoop sets duties through aioesphomeapi) | FLOWING |
| `sil status` ports[].rpm and firmware `Fan N Speed` | model Step output sent as `in` frames, 1 s mean in `SimSensor` | engine to bus to firmware sensor | yes (TestRPMMean, TestModelThreshold) | FLOWING |

### Behavioral Spot-Checks and Probes

The unfiltered e2e suite is the behavioral check (table above). No `scripts/*/tests/probe-*.sh` exist and no plan declares one; Step 7c not applicable.

### Requirements Coverage

All 19 IDs are declared in Phase 1 PLAN `requirements:` fields and mapped to Phase 1 in REQUIREMENTS.md. No orphaned requirements.

| Requirement | Plans | Status | Evidence (today's runs) |
| ----------- | ----- | ------ | ----------------------- |
| BUILD-01 | 01-01, 01-08, 01-09 | SATISFIED | pinned digest, `--user`, TestStaticAndClean, TestBuildCache |
| BUILD-02 | 01-01, 01-08 | SATISFIED | `elf.go` static check, TestStaticAndClean |
| BUILD-03 | 01-08 | SATISFIED | two-stage lint, TestBannedComponents |
| BUILD-04 | 01-03, 01-09 | SATISFIED | TestFreshPrefs |
| BUILD-05 | 01-09 | SATISFIED | TestTwoInstances, TestPrefixNames |
| BUILD-06 | 01-08, 01-09 | SATISFIED | TestReadyIsEncrypted, TestPortInUse |
| BUILD-07 | 01-03, 01-11 | SATISFIED | TestSupervisorKillNoOrphan, TestDownFallback, UAT test 4 |
| BUS-01 | 01-01, 01-02, 01-06, 01-10 | SATISFIED | TestFirmwareBus, TestHubSession |
| BUS-02 | 01-10 | SATISFIED | restart-matrix e2e, UAT test 6 (manifest order) |
| BUS-03 | 01-07, 01-10 | SATISFIED | TestCodegenValidation (path length), TestHubSession (backoff); TX-cap part accepted by override |
| BUS-04 | 01-06 | SATISFIED | shared frames.tsv, Go plus C++ on host and image gcc |
| IO-01 | 01-01, 01-02, 01-04, 01-07 | SATISFIED | TestFirmwareBus, TestLoop |
| IO-02 | 01-01, 01-02, 01-07 | SATISFIED | TestRPMMean |
| IO-06 | 01-06, 01-07 | SATISFIED | TestCodegenValidation |
| MODEL-01 | 01-04, 01-05 | SATISFIED | model unit tests, TestModelThreshold |
| MODEL-02 | 01-05 | SATISFIED | fanlib tests, TestFanTomlUnknownKey; parameters accepted as M1 placeholders (UAT test 2) |
| MODEL-04 | 01-02, 01-04, 01-05 | SATISFIED | engine/clock tests, wall-clock lint |
| CLI-05 | 01-03, 01-12, 01-13 | SATISFIED | `apperr` registry test, TestCLIContract, TestCLIErrors |
| CLI-06 | 01-03, 01-10, 01-12 | SATISFIED | TestCLIContract, TestStatusDuringRestart, one operation table (`operations.go`) |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | | No debt markers, stubs or static fallbacks in Phase 1 files | | |

### Info

- `01-SECURITY.md` frontmatter is still `status: draft` (31/31 threats closed, `threats_open: 0`). This is a sign-off formality, not a goal gap.
- The default `sil up` config changed from `fan-skeleton.yaml` to `rev33.yaml` in Phase 2 (deliberate, documented in `internal/cli/cli.go`). The SC1 wording "the example firmware" still holds for both.

### Human Verification Required

None. All six items of the previous report are settled by `01-UAT.md` (tests 1-4 and 6 pass; test 5 skipped by the maintainer with a reason and recorded as an override above). No new human-only items were found.

### Gaps Summary

No gaps. With Phase 2 and 3 code in place, the phase goal is still demonstrated by a clean `make check`, `make test-cpp-image`, `make mod-verify` and an unfiltered, fully green `make e2e` (57/57) against the pinned image. One open decision sits outside the codebase verdict: Phase 1 is marked `mode: mvp` but its goal is not a user story (reformat it or drop the mode).

---

_Verified: 2026-11-10T09:36:00Z_
_Verifier: Claude (gsd-verifier)_
