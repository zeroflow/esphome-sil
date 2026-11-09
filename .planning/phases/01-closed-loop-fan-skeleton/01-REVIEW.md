---
phase: 01-closed-loop-fan-skeleton
reviewed: 2026-11-09T06:30:00Z
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
  warning: 1
  info: 1
  total: 2
status: issues_found
---

# Phase 1: Code Review Report (iteration 3)

**Reviewed:** 2026-11-09T06:30:00Z
**Depth:** standard
**Files Reviewed:** 83
**Status:** issues_found

## Summary

Re-reviewed the tree after the iteration-2 fixes (commits 73314f5, 5fbe66f, 1c8519a). `gofmt -l` is empty, `go vet ./...` is clean, and `go test -race -count=1 ./internal/fwbuild/ ./internal/supervisor/` passes. The e2e suite (needs Docker and the pinned image) was not re-run.

Status of the iteration-2 warnings:

| ID | Result |
|----|--------|
| WR-01 (container name collision) | Resolved. `containerName` appends the first 4 bytes of sha256(CacheDir). The instance name is restricted to `[a-z0-9-]` (`ValidateName`), so the result is a valid Docker name. Both the pre-run `docker rm -f` and the cancellation hook use the same scoped name. Builds into one cache dir are serialised by the instance lock. |
| WR-02 (`compile_failed` by exit code alone) | Resolved. `classifyCompileFailure` is a pure function that requires exit 1, no download marker, and a positively matched compiler diagnostic. `compile` is called once per build and the log is opened with `O_TRUNC`, so no stale text from an earlier attempt can drive the decision. The regex is only checked against representative logs, as the fix report states (see IN-02 for that residual risk). |
| WR-03 (SimRestart overwrites an existing failure) | Resolved for the stated case: a crash-loop failure survives a failed and a later successful `SimRestart`. A related path that still drops `engine_failed` is described in WR-01 below. |

No critical defects were found.

## Warnings

### WR-01: `FwRestart` clears an `engine_failed` failure while the engine is still dead

**File:** `internal/supervisor/supervisor.go:820` (`cycleFirmware`), related to `:704-726` (`SimRestart`)
**Issue:** After a `SimRestart` whose engine start fails, `s.slot` still holds the stopped engine, and `s.failure` is the only thing that makes `Status()` report `failed` (`engine_failed`). `cycleFirmware`, which backs `FwRestart`, unconditionally runs `s.apiReady, s.apiFailure, s.failure = false, nil, nil`. A `sil fw restart` after the failed sim restart therefore erases the engine failure although nothing has restarted the engine. The firmware is started, cannot connect to a bus that nobody listens on, `FwRestart` returns `ready_timeout`, and from then on `Status()` has no failure and no way to tell the user the simulator engine is down. That is the "starting forever" symptom the iteration-1 `SimRestart` fix was meant to remove. The clear in `cycleFirmware` exists to drop firmware-level failures (`firmware_crash_loop`, a failed auto-restart), not an engine failure.
**Fix:** Clear only firmware-scoped failures in `cycleFirmware`, mirroring the check in `SimRestart`:
```go
s.apiReady, s.apiFailure = false, nil
if s.failure != nil && s.failure.Code != "engine_failed" {
    s.failure = nil
}
```
Add a supervisor test: failing `SimRestart` (engine cannot start), then `FwRestart`, then assert `Status().Error.Code == "engine_failed"`.

## Info

### IN-01: Handling SIGHUP in every command defeats `nohup` for long builds (carried forward)

**File:** `internal/cli/cli.go:146`
**Issue:** `signal.NotifyContext` with SIGHUP installs a handler even when the process was started with SIGHUP ignored (Go documents that `Notify` re-enables an ignored SIGHUP). `nohup sil up ... &` therefore no longer survives a closing terminal during the build (up to the 25-minute build window): the build is cancelled and the container removed. This is probably the intended trade for the earlier WR-06, but it applies to all subcommands, not only `up`.
**Fix:** Check once at start whether SIGHUP is ignored (`signal.Ignored(syscall.SIGHUP)`) and add it to the notify list only when it is not.

### IN-02: The compile-failure classifier is only verified against hand-written logs

**File:** `internal/fwbuild/fwbuild.go:304-327`, `internal/fwbuild/fwbuild_test.go` (`TestClassifyCompileFailure`)
**Issue:** The `compilerDiagnostic` regex includes the bare ninja marker `^FAILED: `. Ninja prints it for any failed build edge, so a non-compiler failure that happens inside a ninja edge (for example a full disk while the compiler writes its output, if the log also carries a `FAILED:` line) is classified as the user's firmware. The fix report notes that no e2e test compiles a deliberately broken firmware, so the regex has never run against real 2026.10.1 output.
**Fix:** Add one e2e (or a captured golden log from a real broken `host:` compile under `internal/fwbuild/testdata/`) and assert `compile_failed`; consider requiring the `file:line:col: error:` form or `undefined reference to` and treating `FAILED:` only as a supporting marker.

---

_Reviewed: 2026-11-09T06:30:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
