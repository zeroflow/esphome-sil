---
phase: 01-closed-loop-fan-skeleton
plan: 08
subsystem: firmware-build
tags: [go, esphome, docker, lint, yaml, elf, build-cache, toolcache, platformio]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 01)
    provides: "fwbuild.Build docker compile of the pinned image, e2e harness"
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "lifecycle.Up calling fwbuild.Build with ConfigPath, Rebuild, LogPath, Progress"
provides:
  - "fwbuild.Build pipeline: preflight, esphome config (offline), LintConfig, input hash skip, compile --only-generate (offline), LintStorage, compile, AssertStatic"
  - "fwbuild.{LintConfig, LintStorage, Identity, Problem}: banned-component and identity lint with lint_rejected detail.problems"
  - "fwbuild.{InputHash, HashInputs, BuildRecord, ReadRecord, WriteRecord, AssertStatic, RecipeVersion}"
  - "error codes docker_unavailable, image_missing, config_invalid, lint_rejected, not_static (build_failed existing)"
  - "shared toolcache $XDG_CACHE_HOME/sil/toolcache/{pio_components,pio_http}: new instances compile with --network none"
affects: [01-09, 01-10, 01-13]

actuals:
  tokens: 23400
  tasks: 2
  commits: 2
plan_head_before: 40ac27e8746a168541bd01ff39c0741eaae3fe0b
plan_head_after: 3f0ee4f52ee3455e128acac9595b034b5ac902ce

tech-stack:
  added: [go.yaml.in/yaml/v3 v3.0.6 (now imported by internal/fwbuild; go.mod/go.sum unchanged)]
  patterns:
    - "Pure lint over a yaml.Node tree: banned set in a switch (no package-level mutable state), problems collected never short-circuited, lint_rejected carries detail.problems []Problem"
    - "Stage outputs that embed the API key (esphome config stdout) stay in memory; every message and tail goes through cleanOutput (key to <redacted>, ANSI and entrypoint notice stripped) so the same failure renders identically twice"
    - "Build record sil-build.json written only after a fully successful build; removed before every compile"
    - "Toolcache derived from Options.CacheDir (two levels up, then toolcache/), never from the environment; parts written through temp dir + rename"

key-files:
  created:
    - internal/fwbuild/lint.go
    - internal/fwbuild/lint_test.go
    - internal/fwbuild/cache.go
    - internal/fwbuild/cache_test.go
    - internal/fwbuild/elf.go
    - internal/fwbuild/elf_test.go
    - internal/fwbuild/fwbuild_test.go
    - internal/fwbuild/testdata/resolved-example.yaml
    - internal/fwbuild/testdata/storage-example.json
    - e2e/build_test.go
    - e2e/testdata/with-wifi-ota.yaml
    - e2e/testdata/with-ledc.yaml
    - e2e/testdata/with-i2c.yaml
  modified:
    - internal/fwbuild/fwbuild.go
    - internal/deps/pin.go

key-decisions:
  - "The toolcache holds two parts, pio_components (library archives) and pio_http (PlatformIO registry responses with expiry rewritten to 2100). A toolcache with only pio_components is not a seed: the compile then runs with network and writes the missing part back"
  - "A compile whose libraries are not in the toolcache fails offline with a named build_failed (message says to delete the toolcache to refetch) instead of silently retrying with network"
  - "Mount root: RepoRoot when it contains the config, else the YAML's git top-level, else its directory; the git top-level is ignored when it is / or the home directory"
  - "A cache hit still runs stage 1 (esphome config + LintConfig, about 1 s) and AssertStatic; only stage 2 and the compile are skipped"
  - "config_invalid maps ESPHome exit codes only; docker's own 125-127 stay Infra build_failed"

requirements-completed: [BUILD-01, BUILD-02, BUILD-03, BUILD-06]

coverage:
  - id: D1
    description: "Preflight: docker unavailable and a missing pinned image stop with exit 2 (docker_unavailable, image_missing with the exact docker pull hint); sil never pulls or substitutes the image"
    requirement: "BUILD-01"
    verification:
      - kind: unit
        ref: "internal/fwbuild/fwbuild_test.go#TestBuildDockerUnavailable, #TestBuildImageMissingNeverPulls (stub docker)"
        status: pass
    human_judgment: false
  - id: D2
    description: "esphome config runs offline first; a non-zero ESPHome exit maps to exit 1 config_invalid carrying the config path and ESPHome's message with the API key redacted, identical on repeat, no sil-build.json written"
    requirement: "BUILD-03"
    verification:
      - kind: unit
        ref: "internal/fwbuild/fwbuild_test.go#TestBuildConfigFailureIsRedactedAndRepeatable"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestBannedComponents/e2e-ban-ledc and /e2e-ban-i2c"
        status: pass
    human_judgment: false
  - id: D3
    description: "The Go lint rejects wifi, ledc, pulse_counter, esp32_rmt_led_strip, i2c, ota, http_request, update, captive_portal, web_server, sntp, esp32 and json by exact key or platform, listing every problem with component, file and hint; LintStorage catches json and others auto-loaded by other components"
    requirement: "BUILD-03"
    verification:
      - kind: unit
        ref: "internal/fwbuild/lint_test.go#TestLintConfigBanned, #TestLintConfigReportsAllAtOnce, #TestLintConfigExactNamesOnly, #TestLintStorage (fixtures captured from the pinned image)"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestBannedComponents/e2e-ban-wifi (wifi and ota in one lint_rejected, twice identical, no sil-build.json)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Identity and API assertions: reboot_timeout zero, encryption key present, -static in build_flags, esphome.name / host.mac_address (case-insensitive) / api.port equal the sil-injected identity; a missing api block names api.encryption and api.reboot_timeout"
    requirement: "BUILD-06"
    verification:
      - kind: unit
        ref: "internal/fwbuild/lint_test.go#TestLintConfigIdentityAndApi, #TestLintConfigNoApiBlock"
        status: pass
    human_judgment: false
  - id: D5
    description: "Unchanged instances skip the compile (build.cached true, stderr build cache hit); changed component source, resolved config, image ID or recipe version change the hash; --rebuild always rebuilds"
    requirement: "BUILD-01"
    verification:
      - kind: unit
        ref: "internal/fwbuild/cache_test.go#TestInputHash, #TestBuildRecord"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestBuildCache"
        status: pass
    human_judgment: false
  - id: D6
    description: "The program is asserted static (no PT_INTERP/.interp, no DT_NEEDED) on every build and cache hit; a dynamic result fails not_static"
    requirement: "BUILD-02"
    verification:
      - kind: unit
        ref: "internal/fwbuild/elf_test.go#TestAssertStatic"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestStaticAndClean"
        status: pass
    human_judgment: false
  - id: D7
    description: "No root-owned file under state, run and cache dirs and an identical git status --porcelain --ignored before and after a build"
    requirement: "BUILD-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestStaticAndClean"
        status: pass
    human_judgment: false
  - id: D8
    description: "A brand-new instance compiles with --network none once the toolcache is seeded (build.log records network=none and no download); the toolcache is derived from CacheDir"
    requirement: "BUILD-01"
    verification:
      - kind: unit
        ref: "internal/fwbuild/cache_test.go#TestToolcache"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestOfflineSeeded"
        status: pass
    human_judgment: false
  - id: D9
    description: "BUILD-02 edge: whether a future component needing NSS/DNS breaks the static link is unclassified (EDGE-BUILD-02-unclassified)"
    requirement: "BUILD-02"
    verification: []
    human_judgment: true
    rationale: "The assertion fires on any dynamic result, but the run-in-image fallback documented in STACK.md is a maintainer decision when such a component appears; needs manual edge review"

duration: 20min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 08: Build Pipeline Summary

**`sil up` now refuses host-incompatible or mis-identified firmware configs before compiling (two-stage Go lint over `esphome config` output and the auto-loaded integrations), never pulls the ESPHome image, skips unchanged builds by input hash, builds new instances offline from a shared toolcache that includes frozen PlatformIO registry responses, and asserts a static ELF on every run.**

## Performance

- **Duration:** about 20 min
- **Tasks:** 2 (both TDD-style: tests written first, one commit per task)
- **Files:** 13 created, 2 modified; 2 commits
- **Measured `sil up` build times** (pinned image, bin/sil, fresh XDG cache root):

| Case | Time |
|------|------|
| Cold: new cache root, toolcache empty, compile with network (fetch + write-back) | 8.4 s |
| Warm: new instance, seeded toolcache, `--network none`, cold ccache | 7.1 s |
| Cache hit (same instance, unchanged): config + lint + static check only | 1.1 s |
| `--rebuild` (warm ccache, `--network none`) | 5.4 s |

  Full `make e2e` (14 tests) passes in 131 s; the five plan tests (`TestBuildCache`, `TestBannedComponents`, `TestStaticAndClean`, `TestOfflineSeeded`, `TestTracer`) in 44 s.

## Accomplishments

- **Lint (BUILD-03, BUILD-06):** `LintConfig` decodes the resolved config into a `yaml.Node` tree (custom tags such as `!lambda` never fail), rejects the 13 banned names by exact top-level key or `platform:` value, and asserts reboot_timeout zero (0s/0ms/0min), encryption key, `-static`, and the injected name/MAC/port. `LintStorage` applies the same set to `loaded_integrations`/`loaded_platforms`. Fixtures were captured from the pinned image, which also resolves assumption A5 (go.yaml.in/yaml/v3 parses real `esphome config` output).
- **Pipeline (BUILD-01):** docker/image preflight, offline config and generate stages, hash skip, compile network `none` or `bridge`, redacted 40-line tail on failure, `sil-build.json` only after success.
- **Static (BUILD-02):** `AssertStatic` via `debug/elf` runs after every compile and on every cache hit.
- **e2e fixtures:** `with-wifi-ota.yaml` (both pass `esphome config`, so the Go lint reports wifi and ota in one error), `with-ledc.yaml` and `with-i2c.yaml` (rejected by `esphome config` itself, confirmed against the image).

## Task Commits

1. **Task 1: Two-stage lint over the resolved config and the auto-loaded integrations** - `a5e4ebd` (feat)
2. **Task 2: Build pipeline with preflight, offline stages, hash skip, toolcache, static check and redacted log tail** - `3f0ee4f` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `internal/fwbuild/lint.go` - LintConfig, LintStorage, Identity, Problem
- `internal/fwbuild/cache.go` - InputHash, BuildRecord, local input extraction, toolcache seed/write-back, registry-cache freeze
- `internal/fwbuild/elf.go` - AssertStatic
- `internal/fwbuild/fwbuild.go` - pipeline (rewritten around a builder)
- `internal/fwbuild/{lint,cache,elf,fwbuild}_test.go` - unit tests; `fwbuild_test.go` drives the pipeline against a stub `docker` on PATH
- `internal/fwbuild/testdata/` - resolved config and storage JSON captured from the pinned image
- `internal/deps/pin.go` - blank import of go.yaml.in/yaml/v3 removed (real import landed, as the file's header prescribes)
- `e2e/build_test.go`, `e2e/testdata/*.yaml` - bench-level guard tests and fixtures

## Decisions Made

See key-decisions. In short: the toolcache carries frozen registry responses next to the archives; offline failures are named errors, not silent online retries; the mount root never widens to `/` or the home directory.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] "Seeded" offline compile still needed the PlatformIO registry for a new instance**
- **Found during:** Task 2 (first run of `TestOfflineSeeded`)
- **Issue:** Research (Pitfall 4, Pattern 2) assumed copying `pio_components` into a new instance is enough for `--network none`. The compile also resolves each library through PlatformIO's registry HTTP cache (`/cache/platformio/cache/http`, per instance, entries valid for 1 hour). A new instance has none, so the offline compile died with `HTTPClientError`; and even an existing instance would fail offline once its entries are older than an hour.
- **Fix:** the toolcache now has a second part `pio_http` (copy of the registry responses written back after the first online build, `db.data` expiries rewritten to 2100-01-01). `seedInstance` installs it into every instance before stage 2; a toolcache with only `pio_components` does not count as seeded (the compile then runs with network and fills the missing part). The format dependency is limited to the digest-pinned image's PlatformIO (`db.data` lines `<expiry>=<file>`, verified from `platformio/cache.py`). Freezing also pins the resolved library versions.
- **Files modified:** internal/fwbuild/cache.go, internal/fwbuild/fwbuild.go, internal/fwbuild/cache_test.go, e2e/build_test.go
- **Verification:** `TestToolcache`; `TestOfflineSeeded` passes with a new instance compiled under `network=none`; manual timing run
- **Committed in:** 3f0ee4f

**2. [Rule 3 - Blocking] Image entrypoint prints a cache notice on stdout**
- **Found during:** Task 1 (fixture capture)
- **Issue:** the first run against a fresh `/cache` mount prints "Creating cache directory ..." before the YAML on stdout, which would break parsing and make the same error message differ between the first and second run.
- **Fix:** `<CacheDir>/cache/platformio` is pre-created, `trimPreamble` drops anything before the first top-level key, `cleanOutput` filters the two notice lines from failure text. Covered by `TestLintConfigToleratesEntrypointPreamble` and the stub-docker test.
- **Files modified:** internal/fwbuild/lint.go, internal/fwbuild/fwbuild.go
- **Committed in:** a5e4ebd, 3f0ee4f

**3. [Rule 3 - Blocking] ESPHome reports config errors on stdout with exit 2**
- **Found during:** Task 2 (checking the ledc/i2c fixtures)
- **Issue:** `esphome config` prints "Failed config ..." on stdout, only the INFO lines on stderr, so a stderr-only tail would hide the component name.
- **Fix:** the config_invalid message uses stderr plus stdout (redacted, ANSI stripped, last 40 lines).
- **Files modified:** internal/fwbuild/fwbuild.go
- **Committed in:** 3f0ee4f

**4. [Rule 2 - Missing critical] Mount root must not widen to `/` or the home directory**
- **Found during:** Task 2 (threat T-01-14)
- **Issue:** the plan mounts the YAML's git top-level; in this home directory (itself a git checkout) that would expose the whole home read-only to ESPHome.
- **Fix:** `mountRoot` uses RepoRoot when it contains the config, accepts a git top-level only if it is not `/` or `$HOME`, otherwise the YAML's directory.
- **Files modified:** internal/fwbuild/fwbuild.go
- **Verification:** `TestMountRoot`
- **Committed in:** 3f0ee4f

**5. [Plan addition] Stub-docker unit tests and fixture detail**
- `internal/fwbuild/fwbuild_test.go` (not in the plan's file list) proves the prohibitions "never pull" and "no key in logs/argv" at unit level. The resolved fixture keeps ESPHome's literal `\033[8m...\033[28m` wrapper around the text `DUMMYKEY` (the plan said to replace the wrapped key; keeping the wrapper makes the fixture realistic), and the local component path was rewritten to `/repo/...`.

---

**Total deviations:** 4 auto-fixed (1 bug, 2 blocking, 1 missing critical) plus 1 plan addition
**Impact on plan:** deviation 1 was necessary for the offline truth to hold at all; no scope creep, no change to the interfaces other plans use (`Build`, `Options`, `Result` unchanged; lifecycle.go untouched).

## Issues Encountered

- A toolcache created earlier in the same e2e cache root (archives only) made the first `TestOfflineSeeded` run look seeded; resolved by the two-part seed definition above, which also self-heals such a toolcache.

## Known Stubs

None.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: vendor_cache_format | internal/fwbuild/cache.go | `freezeHTTPCache` rewrites PlatformIO's `db.data` expiry lines. Safe while the image stays pinned by digest; a bump of the ESPHome image must re-check the format (bump `RecipeVersion` with it). |
| threat_flag: first_build_network | internal/fwbuild/fwbuild.go | The first compile on a host (or after deleting the toolcache) runs with docker `bridge` networking to fetch noise-c/libsodium and registry metadata, announced on stderr. All other stages and all seeded compiles run `--network none`. |

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocking. For maintainer review: `EDGE-BUILD-02-unclassified` (a future component needing NSS/DNS would trip `not_static`; the run-in-image fallback in STACK.md would apply) and assumption A10 (`sensor: pulse_counter` is rejected by the Go lint regardless of what `esphome config` does; not probed against the image here).

## Next Phase Readiness

Ready for 01-09 (native-API ready probe) and later plans: `fwbuild.Build` keeps its signature, `lifecycle.Up` needs no change to benefit (config_invalid / lint_rejected / not_static / image_missing / docker_unavailable all arrive as `*apperr.Error` and are printed by the CLI). `LintConfig` expects `Identity{DeviceName: "sil-"+name, MAC, Port}`, which `Build` derives from the substitutions.

## Self-Check: PASSED

- All 13 created files exist on disk; commits `a5e4ebd` and `3f0ee4f` found in `git log`.
- `make check` passes (112 passed); `make e2e` (all 14 tests) passes, including the five plan tests with `--- PASS` lines each.
- Acceptance greps re-run: `yaml.Node` and `go.yaml.in/yaml/v3` in lint.go, `--network` and `image inspect` in fwbuild.go, `sil-build.json` in cache.go, `reboot_timeout` and `DUMMYKEY` in resolved-example.yaml; go.mod and go.sum unchanged.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*
