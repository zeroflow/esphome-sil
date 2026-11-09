---
phase: 01-closed-loop-fan-skeleton
reviewed: 2026-11-09T05:30:00Z
depth: standard
files_reviewed: 83
files_reviewed_list:
  - .claude/CLAUDE.md
  - .gitignore
  - Makefile
  - cmd/sil/main.go
  - components/sim_bus/__init__.py
  - components/sim_bus/output.py
  - components/sim_bus/sensor.py
  - components/sim_bus/sim_bus.cpp
  - components/sim_bus/sim_bus.h
  - components/sim_bus/sim_output.cpp
  - components/sim_bus/sim_output.h
  - components/sim_bus/sim_protocol.h
  - components/sim_bus/sim_sensor.cpp
  - components/sim_bus/sim_sensor.h
  - e2e/api_client.py
  - e2e/build_test.go
  - e2e/cli_test.go
  - e2e/codegen_test.go
  - e2e/firmware_test.go
  - e2e/harness_test.go
  - e2e/io_test.go
  - e2e/isolation_test.go
  - e2e/loop_test.go
  - e2e/model_test.go
  - e2e/restart_test.go
  - e2e/supervise_test.go
  - e2e/testdata/with-i2c.yaml
  - e2e/testdata/with-ledc.yaml
  - e2e/testdata/with-wifi-ota.yaml
  - e2e/tracer_test.go
  - examples/fan-skeleton.yaml
  - fans/arctic-p12-pwm-pst.toml
  - fans/embed.go
  - go.mod
  - go.sum
  - internal/apiclient/probe.go
  - internal/apiclient/probe_test.go
  - internal/apperr/apperr.go
  - internal/apperr/codes.go
  - internal/apperr/codes_test.go
  - internal/bus/codec.go
  - internal/bus/codec_test.go
  - internal/bus/server.go
  - internal/bus/server_test.go
  - internal/cli/cli.go
  - internal/cli/cli_test.go
  - internal/cli/resolve.go
  - internal/clock/clock.go
  - internal/clock/manual.go
  - internal/clock/manual_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/fanlib/fanlib.go
  - internal/fanlib/fanlib_test.go
  - internal/fwbuild/cache.go
  - internal/fwbuild/cache_test.go
  - internal/fwbuild/elf.go
  - internal/fwbuild/elf_test.go
  - internal/fwbuild/fwbuild.go
  - internal/fwbuild/fwbuild_test.go
  - internal/fwbuild/lint.go
  - internal/fwbuild/lint_test.go
  - internal/fwbuild/testdata/resolved-example.yaml
  - internal/fwbuild/testdata/storage-example.json
  - internal/instance/instance.go
  - internal/instance/instance_test.go
  - internal/instance/lock.go
  - internal/instance/port.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/lint/wallclock_test.go
  - internal/model/fan.go
  - internal/model/fan_test.go
  - internal/supervisor/firmware.go
  - internal/supervisor/policy.go
  - internal/supervisor/policy_test.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - protocol/PROTOCOL.md
  - protocol/vectors/frames.tsv
  - tests/cpp/test_protocol.cpp
findings:
  critical: 0
  warning: 3
  info: 1
  total: 4
status: issues_found
---

# Phase 1: Code Review Report (iteration 2)

**Reviewed:** 2026-11-09T05:30:00Z
**Depth:** standard
**Files Reviewed:** 83
**Status:** issues_found

## Summary

Re-reviewed the tree after the iteration-1 fixes (commits 870b93f..86c9e1c). `gofmt`, `go vet ./...` and `go test -race -count=1 ./internal/...` are clean. The e2e suite (needs Docker and the pinned image) was not re-run.

Status of the iteration-1 warnings:

| ID | Result |
|----|--------|
| WR-01 (failed `SimRestart`) | Resolved for the stated case: `Status()` now reports `failed` with `engine_failed`, and a later successful restart clears it. The new `s.failure` write has a side effect on another failure, see WR-03 below. |
| WR-02 (orphan build container) | Resolved for cancellation and for a SIGKILLed `sil up`. The fixed container name introduces a collision, see WR-01 below. |
| WR-03 (probe goroutine) | Resolved. `startProbe` checks `stopping` and calls `probeWG.Add` under `s.mu`, and `stopAll` sets `stopping` under the same lock before `Wait`. The `Add`/`Wait` ordering is therefore safe. `Probe` honours its context, so `Wait` cannot hang. |
| WR-04 (CRLF framing) | Resolved. `splitLF` frames on LF only and the CRLF row in `TestBadFrameClosesTheSession` covers it. No empty-token or zero-advance hazard for `bufio.Scanner`. |
| WR-05 (compile failure class) | Implemented, but the classification rests on exit code 1 alone and is untested, see WR-02 below. |
| WR-06 (unmanaged supervisor) | Resolved. `run.json` is written before startup and removed after the shutdown defer on every exit path. `sil down` falls back to signalling the PID while the control socket is absent. SIGHUP now takes the `abort()` path. |

No critical defects. Three new warnings come from the fixes themselves.

## Warnings

### WR-01: The fixed build-container name collides across users and run roots, and `docker rm -f` then kills another build

**File:** `internal/fwbuild/fwbuild.go:322, 338` (`containerName`, `removeContainer(name)` before every stage)
**Issue:** The name is `sil-build-<sil_name>`. The Docker daemon is host-wide, but instance names are only unique inside one runtime/cache root (`PathsFor` derives them from `XDG_*` and the uid). The comment at line 318 justifies the unconditional `docker rm -f` with "Instances are serialised by the instance lock", yet that lock is per run root. Two users on the host who both run `sil up dev`, or two e2e environments (`harness_test.go` gives each `Env` its own `XDG_*` directories, and tests reuse short names) that build at the same time, share the name. The second build then force-removes the first one's live compile container, which fails the first build with a confusing docker/ESPHome error. Without the pre-removal it would be a `--name` conflict (exit 125), so the fix traded one failure mode for a harder-to-diagnose one. It can also remove an unrelated container that happens to carry the name.
**Fix:** Make the name unique to the cache directory, which is what the container actually owns, and keep the orphan cleanup scoped to it:
```go
func containerName(o Options) string {
    sum := sha256.Sum256([]byte(o.CacheDir))
    return "sil-build-" + o.Substitutions["sil_name"] + "-" + hex.EncodeToString(sum[:4])
}
```
Update `TestDockerArgsSeparateInstances` to match the prefix and add a case with two cache dirs and the same name.

### WR-02: `compile_failed` is chosen by `ExitCode() == 1` alone, so other build-environment failures are reported as the user's firmware, and the branch has no test

**File:** `internal/fwbuild/fwbuild.go:277-293`
**Issue:** Every `esphome compile` failure that exits 1 and does not contain the two strings `Failed to download` or `HTTPClientError` now becomes `compile_failed` (exit 1, "user-fixable"). ESPHome and PlatformIO also exit 1 for toolchain and environment problems that the user cannot fix by editing YAML: offline lookups of a package missing from the toolcache that surface as `UnknownPackageError`, a corrupted toolcache, DNS or connection errors worded differently from the two matched strings (`Temporary failure in name resolution`, `ConnectionError`), disk full, or a torn build directory. These land in the user-fixable class without the toolcache hint in `msg`, so a consumer branching on exit 1 will tell the user to fix their firmware. The fix report confirms nothing exercises the new branch except the registry scan, so a regression in either direction is invisible.
**Fix:** Positively identify a compiler diagnostic before choosing the user class, for example require a `error:` line from gcc/ld or an ESPHome `Compiling`/`*** [` failure marker in the log tail and keep `build_failed` otherwise:
```go
compilerErr := regexp.MustCompile(`(?m)^\S+:\d+:\d+: (fatal )?error:|undefined reference to|\*\*\* \[.*\] Error`)
if !download && errors.As(err, &ee) && ee.ExitCode() == 1 && compilerErr.Match(raw) {
```
Factor the decision into a pure function `classifyCompileFailure(exitCode int, log []byte) string` and table-test it with a gcc error log, a download failure, an `UnknownPackageError` log and exit 125/137.

### WR-03: A failed `SimRestart` overwrites an existing failure and the later clear never restores it

**File:** `internal/supervisor/supervisor.go:706-710, 720-722`
**Issue:** `s.failure = apperr.AsError(err)` is unconditional. If the instance is already failed for another reason, most plausibly `firmware_crash_loop` (set at `recordExit`, `s.failure` is the only record of it), a `sil sim restart` whose engine start fails replaces it with `engine_failed`. A later successful `sim restart` clears only `engine_failed`, so `s.failure` becomes nil while the firmware is still not running and not restarting. `Status()` then reports `phase: failed` with `error: null`, and the crash-loop diagnosis is gone for good. The same lossy single slot exists for `autoRestart` at line 571, but that path is only reached when `failure` was not already set.
**Fix:** Do not replace an existing failure, or keep the engine failure in its own field and merge in `Status()`:
```go
s.mu.Lock()
if s.failure == nil {
    s.failure = apperr.AsError(err)
}
s.mu.Unlock()
```
and clear on success only if the stored error is the `engine_failed` this call recorded. Add a supervisor test: crash-loop first, failing `SimRestart`, successful `SimRestart`, and assert `Status().Error.Code == "firmware_crash_loop"`.

## Info

### IN-01: Handling SIGHUP in every command defeats `nohup` for long builds

**File:** `internal/cli/cli.go:146`
**Issue:** `signal.NotifyContext` with SIGHUP installs a handler even when the process was started with SIGHUP ignored (Go documents that `Notify` re-enables an ignored SIGHUP). `nohup sil up ... &` therefore no longer survives a closing terminal during the build (up to the 25-minute build window): the build is cancelled and the container removed, where before the process kept running. This is probably the intended trade for WR-06, but it applies to all subcommands, not only `up`.
**Fix:** Check once at start whether SIGHUP is ignored (`signal.Ignored(syscall.SIGHUP)`) and only add it to the notify list when it is not.

---

_Reviewed: 2026-11-09T05:30:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
