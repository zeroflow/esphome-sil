---
phase: 01-closed-loop-fan-skeleton
verified: 2026-11-09T04:30:00Z
status: human_needed
score: 5/5 roadmap success criteria verified; 0 failed
behavior_unverified: 2
overrides_applied: 0
covered_digest: "v2:sha256:1e77dfd30ad9fb04da5640ddcb09bf8c0128022b8d6a53ca4d7a2017c59a91f7"
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
behavior_unverified_items:
  - truth: "The hub drops the connection and reconnects with backoff when its pending TX buffer exceeds 64 KiB, so a stalled simulator can never block or grow the firmware loop (01-10, verification: backstop)"
    test: "Run a firmware against a peer that accepts the bus socket but never reads, drive output changes until more than 64 KiB is queued"
    expected: "The firmware drops the connection, reconnects with backoff, its API stays responsive and its memory stays bounded"
    why_human: "kMaxTx and the drop path exist (sim_bus.h, sim_bus.cpp) but no Go, C++ or e2e test exercises overflow. Non-inferable truth; presence is not evidence."
  - truth: "The chan manifest order of a given config is stable across rebuilds (component registration order) (01-10, verification: backstop)"
    test: "Build the example twice (cleaned cache), connect a peer and compare the chan frame order of the hello snapshot"
    expected: "Identical order on both builds"
    why_human: "engine_test only checks order of a fake peer; no test compares firmware manifests across rebuilds."
human_verification:
  - test: "Maintainer provenance review of flynn/noise v1.1.0 and go.yaml.in/yaml/v3 v3.0.6 (01-02 backstop, SECURITY.md T-01-SC)"
    expected: "Maintainer confirms the modules are the intended upstreams. Automated checks (GOSUMDB, go mod verify, Origin URLs) pass, the human review has not happened."
    why_human: "01-02 states the unattended run does not claim it happened."
  - test: "Review the ASSUMED fan parameters in fans/arctic-p12-pwm-pst.toml (start_duty 0.10, tau_up 1.0 s, tau_down 2.5 s) and the low-confidence TechPowerUp cross-check"
    expected: "Maintainer accepts them as placeholders for M1 or replaces them with measured values"
    why_human: "The values are flagged ASSUMED in the file. Tests prove the model follows whatever the file says, not that it resembles a real fan."
  - test: "Run a real failing firmware compile (for example a C++ syntax error in a component) with sil up, once"
    expected: "Exit 1 with error.code compile_failed and a redacted log tail, not build_failed"
    why_human: "Review fixes a192260 and 5fbe66f classify from compiler diagnostics. The review flagged them as needing human verification and IN-02 notes there is no real failing-compile golden log. cab20ad (fw restart keeps engine_failed) was not re-reviewed after the iteration cap."
  - test: "Run sil up, kill the sil up client before run.json is written (6dbff8e), and run nohup sil up (SIGHUP, IN-01)"
    expected: "No orphan supervisor remains, and the documented nohup behaviour matches the intent"
    why_human: "Timing-dependent window. The review disposition marks WR-05, WR-06 and the iteration 2 fixes as needing human verification."
---

# Phase 1: Closed-Loop Fan Skeleton Verification Report

**Phase Goal:** Developer runs `sil up` and real ESPHome firmware, built in the pinned image and running directly on the host, drives simulated fans whose RPM flows back over the sim-bus; the loop is observable via `sil status`, survives restarts of either side, and stops cleanly with `sil down`
**Verified:** 2026-11-09
**Status:** human_needed
**Re-verification:** No, initial verification

All five roadmap success criteria hold against the current code (HEAD a456dee, which includes the security fixes ce3bed4/56693c0/12bedc2 and the review fixes 870b93f..cab20ad). The status is `human_needed` only because of declared backstop truths and flagged assumptions that no test can settle. There are no gaps.

## Evidence run by the verifier (not taken from SUMMARY.md)

| Check | Command | Result |
| ----- | ------- | ------ |
| Format, vet, unit tests with race detector, host C++ codec test | `make check` | exit 0, 14 Go packages ok, C++ `112 passed, 0 failed` |
| C++ codec test with the pinned image's gcc 14.2 | `make test-cpp-image` | `112 passed, 0 failed` (same golden vectors as Go) |
| Full e2e (docker, local pinned image, real firmware builds) | `make e2e` | exit 0, 277 s, 34 top-level tests PASS, 0 FAIL, 0 SKIP |
| Leftovers after e2e | `pgrep` for sil serve, program; `find -user root` in the e2e cache | no processes, no root-owned files |
| Debt markers (TBD/FIXME/XXX/TODO/HACK) in tracked source | grep | none |
| Wall-clock calls outside `internal/clock` | grep plus `internal/lint/wallclock_test.go` (passes in `make check`) | only a lint fixture string |
| Working tree | `git status` | only pre-existing unrelated `.planning` and `.gsd` changes |

## Goal Achievement

### Observable Truths (ROADMAP success criteria)

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | `sil up <name>` builds in the pinned image as a static binary, runs it on the host, reports ready only after a real encrypted handshake; firmware stays alive with no API client; no root-owned files | VERIFIED | e2e `TestTracer`, `TestStaticAndClean` (static ELF, nothing root-owned, tree unchanged), `TestReadyIsEncrypted` (Noise handshake, wrong key refused, firmware PID alive 10 s with no client), `TestBuildCache`, `TestOfflineSeeded`. Code: `internal/fwbuild/fwbuild.go`, `internal/apiclient/probe.go`; `examples/fan-skeleton.yaml` pins `-static`, `reboot_timeout: 0s`, `encryption.key`. |
| 2 | Key from `sil status`; aioesphomeapi sets any of 4 fans; `sil status` shows duty; RPM (1 s mean) rises with first-order lag, 0 below start threshold; model advances only via injectable clock; lint fails on wall-clock calls | VERIFIED | e2e `TestLoop` (four fans), `TestModelThreshold` (fan 2 at speed 8 stays 0, at 40 reaches target fraction), `TestRPMMean`. Unit: `internal/model` (exact exponential, hysteresis), `internal/engine` with `clock.Manual` (100 ticks equal composed `Step`), `internal/lint/wallclock_test.go`. Code: `internal/model/fan.go`, `components/sim_bus/sim_sensor.cpp` (time-weighted 1 s mean). |
| 3 | Restart of simulator or firmware in either order re-syncs without manual steps; firmware API responsive while simulator gone; same golden vectors in Go and C++ | VERIFIED | e2e `TestSimRestartResync`, `TestFwRestartResync`, `TestRestartEitherOrder`, `TestSimGapAPIResponsive`, `TestStatusDuringRestart`, `TestHubSession`, `TestFakePeer`. `protocol/vectors/frames.tsv` read by `internal/bus/codec_test.go` and `tests/cpp/test_protocol.cpp`; both pass, C++ also under the image gcc. |
| 4 | Two named instances side by side (own run dir, port, MAC, name, key); taken port exits 2 with JSON error; `sil down` stops both; killing the simulator leaves no orphan; fresh prefs per run | VERIFIED | e2e `TestTwoInstances`, `TestPrefixNames`, `TestPortInUse`, `TestPortClaimed`, `TestFreshPrefs`, `TestSupervisorKillNoOrphan`, `TestSupervisorSIGTERM`, `TestDownFallback`, `TestFirmwareCrashAutoRestart`. Code: `supervisor/firmware.go` (Setpgid, Pdeathsig, LockOSThread). Post-run process scan found no leftovers. |
| 5 | Misconfiguration fails loudly: host-incompatible component, sim platform without `channel:`, over-long socket path, unknown `fan.toml` key | VERIFIED | e2e `TestBannedComponents` (wifi, ledc, i2c), `TestCodegenValidation` (missing channel, `sun_path limit is 107`), `TestNamesAndPaths`/isolation (`socket_path_too_long` exit 2), `TestFanTomlUnknownKey` (`fan_toml_invalid`, key and line named, no build). |

**Score:** 5/5 roadmap criteria verified. 2 plan-level backstop truths present but behavior-unverified (below).

### Behavior-dependent truths

The cancel/cleanup/ordering invariants (shutdown stops firmware group first, no orphan on supervisor death, restart-in-flight ordering, newest bus connection replaces older, engine drop-and-recreate) each have a passing named e2e or unit test, so they are VERIFIED rather than presence-only.

Two plan-level truths are declared `verification: backstop` in 01-10 and have no exercising test (PRESENT_BEHAVIOR_UNVERIFIED, see `behavior_unverified_items`): the 64 KiB TX-buffer drop in `components/sim_bus/sim_bus.cpp`, and manifest order stability across rebuilds. Neither is a roadmap success criterion.

### Required Artifacts

| Artifact | Status | Details |
| -------- | ------ | ------- |
| `components/sim_bus/{__init__,output,sensor}.py`, `sim_bus.{h,cpp}`, `sim_output.*`, `sim_sensor.*`, `sim_protocol.h` | VERIFIED | Substantive (245-line hub, 322-line header-only codec); built and run by e2e |
| `examples/fan-skeleton.yaml` | VERIFIED | Four ports, encryption, `-static`, `reboot_timeout: 0s`, identity only by substitution/env |
| `internal/{fwbuild,instance,lifecycle,supervisor,engine,model,fanlib,bus,control,cli,clock,apiclient,apperr,lint}` | VERIFIED | Non-stub (about 12.9k lines incl. tests), wired from `cmd/sil/main.go`, exercised end to end |
| `fans/arctic-p12-pwm-pst.toml` + `fans/embed.go` | VERIFIED (values flagged) | Loads through the strict loader; three parameters are marked ASSUMED, see human verification |
| `protocol/PROTOCOL.md`, `protocol/vectors/frames.tsv` | VERIFIED | Shared by Go and C++ tests |
| `Makefile` (check, test-cpp, test-cpp-image, e2e, mod-verify) | VERIFIED | All targets ran |

### Key Link Verification

| From | To | Status | Details |
| ---- | -- | ------ | ------- |
| `sil up` | fwbuild, detached `sil serve`, firmware | WIRED | `lifecycle.go` to `supervisor.go` to `firmware.go`; TestTracer |
| firmware sim_bus hub | `bus.Server` / engine | WIRED | hello/welcome/snapshot/sync, TestHubSession |
| engine to model, injected clock | `model.Step` per tick | WIRED | engine tests with `clock.Manual` |
| `sil status` / restart commands | `control` API on api.sock | WIRED | `cli.go` uses `control.Client`; TestCLIContract, TestStatusDuringRestart |
| supervisor shutdown | firmware group, run.json, sockets | WIRED | TestSupervisorSIGTERM, TestSupervisorKillNoOrphan |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
| -------- | ---- | ------ | --------- | ------ |
| `sil status` ports[].duty | firmware `out` frames from the native API fan command | bus codec to engine snapshot | yes (TestLoop sets 40/20/60/100 through aioesphomeapi) | FLOWING |
| `sil status` ports[].rpm and firmware `Fan N Speed` | model Step output sent as `in` frames, 1 s mean in sim_sensor | engine to bus to firmware sensor | yes (TestRPMMean, TestModelThreshold) | FLOWING |

### Behavioral Spot-Checks and Probes

The whole e2e suite is the behavioral check (table above). No `probe-*.sh` scripts are declared or exist; Step 7c not applicable.

### Requirements Coverage

All 19 IDs from the phase request appear in at least one PLAN `requirements:` field and in REQUIREMENTS.md mapped to Phase 1. No orphaned requirements (every Phase 1 row in REQUIREMENTS.md is claimed by a plan).

| Requirement | Plans | Status | Evidence |
| ----------- | ----- | ------ | -------- |
| BUILD-01 | 01-01, 01-08, 01-09 | SATISFIED | pinned digest, `--user`, TestStaticAndClean, TestBuildCache |
| BUILD-02 | 01-01, 01-08 | SATISFIED | static ELF check (`elf.go`), TestStaticAndClean |
| BUILD-03 | 01-08 | SATISFIED | two-stage lint, TestBannedComponents |
| BUILD-04 | 01-03, 01-09 | SATISFIED | TestFreshPrefs |
| BUILD-05 | 01-09 | SATISFIED | TestTwoInstances, TestPrefixNames |
| BUILD-06 | 01-08, 01-09 | SATISFIED | TestReadyIsEncrypted, TestPortInUse |
| BUILD-07 | 01-03, 01-11 | SATISFIED | TestSupervisorKillNoOrphan, TestDownFallback |
| BUS-01 | 01-01, 01-02, 01-06, 01-10 | SATISFIED | TestFirmwareBus, TestHubSession |
| BUS-02 | 01-10 | SATISFIED | restart-matrix e2e |
| BUS-03 | 01-07, 01-10 | SATISFIED | TestCodegenValidation (path length), TestHubSession (backoff). The TX-cap part is behavior-unverified |
| BUS-04 | 01-06 | SATISFIED | shared frames.tsv, Go and C++ (host and image gcc) |
| IO-01 | 01-01, 01-02, 01-04, 01-07 | SATISFIED | TestFirmwareBus, TestLoop |
| IO-02 | 01-01, 01-02, 01-07 | SATISFIED | TestRPMMean |
| IO-06 | 01-06, 01-07 | SATISFIED | TestCodegenValidation |
| MODEL-01 | 01-04, 01-05 | SATISFIED | model unit tests, TestModelThreshold |
| MODEL-02 | 01-05 | SATISFIED | fanlib tests, TestFanTomlUnknownKey. Parameter values flagged ASSUMED |
| MODEL-04 | 01-02, 01-04, 01-05 | SATISFIED | engine/clock tests, wallclock lint |
| CLI-05 | 01-03, 01-12, 01-13 | SATISFIED | `apperr` registry AST test, TestCLIContract, TestCLIErrors |
| CLI-06 | 01-03, 01-10, 01-12 | SATISFIED | TestCLIContract, TestStatusDuringRestart |

### Security and review state

`01-SECURITY.md`: 31 of 31 threats closed, `threats_open: 0`, but the frontmatter is `status: draft` and sign-off is pending. Non-blocking residuals (lexical mount check `broadMount`, `/tmp/sil.sock` default path, first-build network integrity, T-01-17 check-then-use) are recorded there. `01-REVIEW-DISPOSITION.md`: all warnings fixed, IN-01/IN-02 open, info only. The final fix cab20ad was not re-reviewed.

### Anti-Patterns Found

None blocking. No debt markers, no stubs, no static fallbacks in rendered data.

### Human Verification Required

1. **Provenance of flynn/noise and go.yaml.in/yaml/v3** (01-02 backstop). Automated checks pass; maintainer review has not happened.
2. **ASSUMED fan parameters** in `fans/arctic-p12-pwm-pst.toml` (start_duty, tau_up, tau_down, low-confidence cross-check). Accept as M1 placeholders or measure.
3. **Real failing-compile classification** (`compile_failed`) and the unreviewed `cab20ad`. No golden log of a real failing compile exists.
4. **Startup-window orphan and SIGHUP behavior** (`6dbff8e`, IN-01): timing-dependent.
5. **Behavior-unverified backstop truths**: 64 KiB TX-buffer drop; manifest order stable across rebuilds.

### Gaps Summary

No gaps. The phase goal is achieved and demonstrated by a clean `make check`, `make test-cpp-image` and a full green `make e2e` against the real pinned image. The remaining items are maintainer sign-offs and two untested backstop invariants, hence `human_needed`.

---

_Verified: 2026-11-09_
_Verifier: Claude (gsd-verifier)_
