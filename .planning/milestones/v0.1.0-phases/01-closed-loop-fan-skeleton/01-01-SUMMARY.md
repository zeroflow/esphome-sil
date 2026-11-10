---
phase: 01-closed-loop-fan-skeleton
plan: 01
subsystem: firmware-seam
tags: [esphome, docker, go, sim-bus, unix-socket, aioesphomeapi, e2e]

requires: []
provides:
  - "Go module github.com/zeroflow/esphome-sil (go 1.27) with Makefile targets build/test/test-pkg/vet/fmt-check/check/mod-verify/e2e"
  - "internal/apperr, internal/clock (Real, WithTimeout, TimedOut), internal/fwbuild (digest-pinned docker compile)"
  - "components/sim_bus ESPHome external component (hub + output + sensor, SIL_BUS override)"
  - "examples/fan-skeleton.yaml: 4 fan ports, encrypted api, reboot_timeout 0s, -static"
  - "e2e harness (cacheRoot, newFWIdentity, buildFirmware, startFirmware, listenBus, runAPIClient, ...) and TestFirmwareBus"
affects: [01-02, 01-03, 01-04, 01-06, 01-07, 01-08, 01-10]

actuals:
  tokens: 10500
  tasks: 2
  commits: 2
plan_head_before: 80817bc2bf41bd370896feccf568819b3741af23
plan_head_after: f389e6fe72800b7503cae8036f0f6de568e316c2

tech-stack:
  added: [Go 1.27 stdlib only, aioesphomeapi 46.12.0 (shipped in the pinned image)]
  patterns:
    - "docker run of the digest-pinned ESPHome image with --user uid:gid, repo mounted :ro at its own path, build output and HOME in a per-instance cache dir"
    - "API key reaches docker only through cmd.Env (-e SIL_API_KEY without value)"
    - "firmware reads SIL_BUS at runtime; YAML path is only a default"
    - "e2e cache root per worktree (SIL_E2E_CACHE or ~/.cache/sil-e2e/<sha256(repo root)[:12]>)"

key-files:
  created:
    - go.mod
    - Makefile
    - internal/apperr/apperr.go
    - internal/clock/clock.go
    - internal/fwbuild/fwbuild.go
    - components/sim_bus/__init__.py
    - components/sim_bus/output.py
    - components/sim_bus/sensor.py
    - components/sim_bus/sim_bus.h
    - components/sim_bus/sim_bus.cpp
    - examples/fan-skeleton.yaml
    - e2e/harness_test.go
    - e2e/firmware_test.go
    - e2e/api_client.py
  modified: []

key-decisions:
  - "ESPHOME_NOGITIGNORE=1 is set in the build container: with the repo mounted read-only ESPHome's compile otherwise dies writing examples/.gitignore"
  - "fwbuild.Result.Cached is always false in this plan; the hash-skip pipeline arrives in 01-08"
  - "Helpers local to one e2e test file are prefixed with that file's stem (fw*) per the plan's naming rule"

requirements-completed: [BUILD-01, BUILD-02, BUS-01, IO-01, IO-02]

coverage:
  - id: D1
    description: "fwbuild.Build compiles examples/fan-skeleton.yaml in the digest-pinned image as the caller's uid with the key only in the environment, leaving the working tree clean and no root-owned cache files"
    requirement: "BUILD-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFirmwareBus#TestFirmwareBus"
        status: pass
    human_judgment: false
  - id: D2
    description: "Built program is a static ELF (no PT_INTERP, no .interp) and runs directly on the host"
    requirement: "BUILD-02"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFirmwareBus#TestFirmwareBus (debug/elf assertion + running firmware)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Firmware connects to the SIL_BUS socket (retrying every 500 ms until a peer listens) and sends hello proto 1 plus port1..4.duty"
    requirement: "BUS-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFirmwareBus#TestFirmwareBus"
        status: pass
    human_judgment: false
  - id: D4
    description: "Fan speeds set through the encrypted native API appear as out frames port1..4.duty 0.4/0.2/0.6/1.0"
    requirement: "IO-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFirmwareBus#TestFirmwareBus/fan1_duty and /four_ports"
        status: pass
    human_judgment: false
  - id: D5
    description: "An in frame on port1.rpm shows up as Fan 1 Speed 1234 at the native API"
    requirement: "IO-02"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFirmwareBus#TestFirmwareBus/rpm_in"
        status: pass
    human_judgment: false

duration: 12min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 01: Firmware Seam Summary

**Real ESPHome firmware built from the repo's own sim_bus component in the digest-pinned image, running as a static program on the host, exchanging duty and RPM over a Unix sim-bus while the encrypted native API drives four fans.**

## Performance

- **Duration:** about 12 min
- **Completed:** 2026-11-09T00:47Z
- **Tasks:** 2
- **Files:** 14 created, 0 modified (prototype/ untouched)
- **Measured build times** (pinned image, `--user` uid, cache root `~/.cache/sil-e2e/cf077fb401d5`): cold first successful build 7 s; warm rebuild with a fresh key/port/MAC 2 s. Full `make e2e E2E_RUN=TestFirmwareBus` wall time: 13.8 s including the four API subtests.

## Accomplishments

- `fwbuild.Build` runs the pinned image with `--user uid:gid`, repo mounted read-only at its own path, build and HOME under the per-instance CacheDir, identity as `-s` substitutions and the key only via the process environment; returns the static `program`.
- `components/sim_bus` (moved from prototype, light and binary_sensor dropped) honours `SIL_BUS`, uses `setup_priority::BUS`, retries the non-blocking connect every 500 ms, sends `hello` proto 1 and replays every output on connect.
- `examples/fan-skeleton.yaml` takes name, MAC, port and key only from injected values, with `reboot_timeout: 0s`, `-static` and four `ALWAYS_OFF` fan ports.
- `TestFirmwareBus` proves: static ELF, no root-owned cache files, clean working tree, hello plus four zero duty frames after a delayed peer start, `port1.duty` 0.4 from Fan 1 speed 40, ports 2-4 at 0.2/0.6/1.0, and `in port1.rpm 1234` visible as `Fan 1 Speed`.

## Task Commits

1. **Task 1: Tracer, pinned-image static build to host run to sim-bus hello and duty frames** - `6764ee9` (feat)
2. **Task 2: Four fans through the encrypted native API, RPM fed back** - `f389e6f` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `go.mod`, `Makefile` - module and build/test/e2e targets
- `internal/apperr/apperr.go` - JSON error type with exit classes 1/2
- `internal/clock/clock.go` - Clock interface, Real clock, clock-driven WithTimeout/TimedOut
- `internal/fwbuild/fwbuild.go` - pinned-image docker compile
- `components/sim_bus/*` - ESPHome hub, output and sensor platforms (Python + C++)
- `examples/fan-skeleton.yaml` - Phase 1 example firmware
- `e2e/harness_test.go`, `e2e/firmware_test.go`, `e2e/api_client.py` - docker-backed end-to-end harness and test

## Decisions Made

- Build container gets `ESPHOME_NOGITIGNORE=1` (see deviation).
- `Result.Cached` stays false until 01-08 builds the hash-skip pipeline behind the same signature.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] ESPHome compile writes .gitignore into the read-only config dir**
- **Found during:** Task 1 (first e2e run)
- **Issue:** `esphome compile` calls `writer.write_gitignore()` into the YAML's directory; with the repo mounted `:ro` (required to keep the working tree clean) the build died with `OSError: [Errno 30] Read-only file system: .../examples/.gitignore`. Research Pitfall 5 had assumed `:ro` alone sufficed.
- **Fix:** pass `-e ESPHOME_NOGITIGNORE=1` (the image's `ENV_NOGITIGNORE`, checked in `esphome/__main__.py:write_cpp`) in `fwbuild.dockerArgs`.
- **Files modified:** internal/fwbuild/fwbuild.go
- **Verification:** TestFirmwareBus passes, git status identical before and after
- **Committed in:** 6764ee9 (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary for the read-only mount design; no scope change. Later plans (01-08 lint stages running `esphome config` and `--only-generate` in the same container shape) should carry the same variable.

## Issues Encountered

None beyond the deviation above.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: insecure_default_ipc_path | components/sim_bus/__init__.py | Hub option `path` defaults to the fixed `/tmp/sil.sock` (kept from the prototype, as the plan specifies "a short /tmp path"). Any local user could pre-create that path if the firmware runs without `SIL_BUS`. Not exploitable on the supported path: `sil` and the e2e harness always set `SIL_BUS` to a socket inside a 0700 run dir, which overrides the default at runtime. Suggested follow-up (01-07 codegen validation): drop the default or require `SIL_BUS`/explicit `path`. |

## Known Stubs

None.

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocking. Optional: decide in 01-07 whether the hub `path` default should be removed (see Threat Flags).

## Next Phase Readiness

Ready for 01-02 (Go simulator and fan model closing the loop). Stable names for later plans: channels `portN.duty` / `portN.rpm`, substitutions `sil_name`/`sil_mac`/`sil_port`, env `SIL_API_KEY`/`SIL_BUS`/`SIL_E2E_CACHE`, entities `Fan N` / `Fan N Speed`. The e2e docker precondition (daemon + local pinned image) is enforced in `TestMain`.

## Self-Check: PASSED

- All 14 key files exist on disk; commits `6764ee9` and `f389e6f` found in `git log`.
- `make check` passes; `make e2e E2E_RUN=TestFirmwareBus` prints PASS for TestFirmwareBus and its four subtests api_info, fan1_duty, four_ports, rpm_in.
- Acceptance greps re-run: SIL_BUS getenv, `setup_priority::BUS`, no SimLight/SimBinarySensor, `reboot_timeout: 0s`, `SIL_API_KEY`, `-static`, `ALWAYS_OFF`, image digest, `sil-e2e`, `SIL_E2E_CACHE`, `noise_psk` all match; `SIL_API_KEY=` appears only in cmd.Env assignments; `git status --porcelain prototype` is empty.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
