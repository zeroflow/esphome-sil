---
phase: 01-closed-loop-fan-skeleton
reviewed: 2026-11-09T03:57:28Z
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
  warning: 6
  info: 6
  total: 12
status: issues_found
---

# Phase 1: Code Review Report

**Reviewed:** 2026-11-09T03:57:28Z
**Depth:** standard
**Files Reviewed:** 83
**Status:** issues_found

## Summary

Reviewed the Go simulator (supervisor, bus server and codec, engine, model, control API, instance and lifecycle, fwbuild, fanlib, apiclient, clock, CLI), the C++ `sim_bus` component and its header-only codec, the ESPHome Python glue, the protocol document and the e2e harness. `go vet` and `go test -race ./...` pass on the current tree.

Beyond reading, I ran a differential check of the Go codec (`internal/bus/codec.go`) against the C++ parser (`components/sim_bus/sim_protocol.h`) on about 8,800 mutated lines (number-grammar edge cases, escapes, control bytes, nesting, duplicate keys). Both returned the same verdict and the same decoded fields every time, so the parser-differential risk (T-01-10) looks well controlled at the `Decode` level. The one divergence is one layer up, in the Go server's line framing (WR-04).

No critical defects found. The main concerns are lifecycle edge cases: a failed simulator restart leaves the supervisor reporting a stale engine, and a cancelled build can leave the Docker container running against a shared build directory. The remaining items are a protocol-framing mismatch, dead synchronisation and unused code. Security items already covered by 01-SECURITY.md are not repeated.

## Warnings

### WR-01: A failed `SimRestart` leaves the stopped engine in the slot and status never reports the failure

**File:** `internal/supervisor/supervisor.go:680-700`
**Issue:** `SimRestart` cancels the old engine (`stopEngine(old)`, line 683) and then calls `startEngine()`. If that returns an error (for example `bus.Listen` fails because the run directory vanished), the function returns without touching `s.slot`, which still points at the engine that was just stopped. `engine.stopped()` published `BusConnected=false`, so from then on `Status()` computes `phase = "starting"` with no `error` and no warning, forever. `engine_restarts` has also already been incremented (line 681). `waitReady`-style callers and `sil status` therefore see an instance that is neither ready nor failed. A second `sil sim restart` does recover (stopping the already-done slot is a no-op), but nothing tells the user that is needed.
**Fix:** Record the failure when the replacement engine does not start, and only count the restart once it did:
```go
sl, err := s.startEngine()
if err != nil {
    s.mu.Lock()
    s.failure = apperr.AsError(err) // phase becomes failed with the engine_failed error
    s.mu.Unlock()
    return control.Status{}, err
}
s.mu.Lock()
s.engRestarts++
s.failure = nil
...
```
Alternatively keep the old engine running until the new one is known to listen (not possible on the same socket path, so the failure marker is the practical fix).

### WR-02: Cancelling a build kills the `docker` client but not the container; a later build can race the orphan on the same build directory

**File:** `internal/fwbuild/fwbuild.go:309-315` (also `:225`, `:233`)
**Issue:** `b.docker` uses `exec.CommandContext(ctx, "docker", "run", ...)`. On cancellation Go sends SIGKILL to the `docker` CLI process. The CLI proxies signals to the container, but SIGKILL cannot be proxied, and the container belongs to dockerd, so it keeps compiling. It writes into `<CacheDir>/build` and `<CacheDir>/cache`, which are bind-mounted and reused by the next `sil up` of the same name. Triggers are a SIGTERM to `sil up` (CI job timeout, `kill`) or the 25-minute e2e context timeouts. `signal.NotifyContext` in `cli.Execute` turns SIGTERM into exactly this cancellation. A following build then runs concurrently with the stale compile in the same ninja/PlatformIO tree; the second run can record a `BuildRecord` for a torn program (only `AssertStatic` guards it).
**Fix:** Give the container a name and stop it on cancellation, or at least deliver SIGTERM (which the CLI proxies) instead of SIGKILL:
```go
cmd := exec.CommandContext(ctx, "docker", args...)
cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
```
`cmd.WaitDelay` is banned by the wall-clock lint, so pair this with a `--name sil-build-<instance>` and a `docker rm -f <name>` on the cancellation path (which also prevents two concurrent builds of one instance).

### WR-03: `probeCancel` is never called on shutdown and `probeWG` is never waited; the probe goroutine outlives `stopAll`

**File:** `internal/supervisor/supervisor.go:129-130, 374-384, 394-405`
**Issue:** `startProbe` creates a cancellable context and adds to `probeWG`, but `stopAll`/`shutdown` neither cancel the last probe nor wait for it. After shutdown the mainLoop has stopped consuming `fwExits`, so `s.fwRunning` stays `true` and `probeLoop` keeps retrying `apiclient.Probe` against the dead API port for up to `probeBudget` (20 s), logging a `ready_timeout` error through `setAPI` after the instance has "stopped". The process normally exits first, but the code claims an orderly shutdown ("stops the firmware group before run.json and the sockets go away") and the dead `WaitGroup` suggests the wait was intended. In tests with a fake `Starter` the goroutine leaks across test cases.
**Fix:** In `stopAll` after setting `stopping`:
```go
s.mu.Lock()
s.stopping = true
cancel := s.probeCancel
fw, sl := s.fw, s.slot
s.mu.Unlock()
if cancel != nil { cancel() }
...
s.probeWG.Wait() // after stopProc/stopEngine
```
or delete `probeWG` if no wait is wanted.

### WR-04: The Go server accepts CRLF-terminated lines that the protocol and the C++ side reject

**File:** `internal/bus/server.go:243-248` (framing); contract in `protocol/PROTOCOL.md` "Line grammar"
**Issue:** `read` frames with `bufio.NewScanner`, whose default `ScanLines` strips a trailing `\r` before `Decode` sees the line. PROTOCOL.md says carriage returns are errors (`bad_char`/`not_object`), and the C++ `proto::parse` rejects `\r`. I verified the Scanner behaviour: `{"k":"bye"}\r\n` yields `{"k":"bye"}`. So a CRLF-speaking peer works against the simulator and fails against the firmware, and the shared `frames.tsv` vectors cannot catch it because they exercise `Decode`, not the framing. It also hides a "line ends in `\r`" case from `Decode`'s length check (a 4095-byte payload plus `\r` passes).
**Fix:** Use a split function that splits on `\n` only (`bufio.ScanLines` minus the CR drop), for example:
```go
sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
    if i := bytes.IndexByte(data, '\n'); i >= 0 { return i + 1, data[:i], nil }
    if atEOF && len(data) > 0 { return len(data), data, nil }
    return 0, nil, nil
})
```
and add a CRLF row to the server tests.

### WR-05: A compile failure in the firmware under test is reported as a bench infrastructure error (`build_failed` is always exit 2)

**File:** `internal/fwbuild/fwbuild.go:270-287`
**Issue:** `esphome config` and `--only-generate` failures are classified by `stageFailure` (ESPHome exit means user-fixable `config_invalid`, exit 1), but a real `compile` failure is always `apperr.Infra("build_failed")` (exit 2), including a C++ error in the user's own firmware or lambda, which is exactly what the product repo is testing. `apperr` documents exit 1 as "user-fixable" and consumers branch on it, so the product repo's pytest cannot tell "my firmware does not compile" from "docker/toolcache is broken" without parsing the log tail. The offline-toolcache hints are the only case that is truly infrastructure.
**Fix:** Classify by cause: keep `build_failed` (exit 2) when the log shows `Failed to download`/`HTTPClientError` or docker itself failed (exit 125-127), and return a user-class error (for example a registered `compile_failed`, exit 1, carrying `output_tail`) when the container ran and the compiler reported errors. Register the new code in `apperr/codes.go` so `TestRegistryMatchesCallSites` stays green.

### WR-06: `sil up` leaves an unmanaged supervisor if it dies between `startServe` and `run.json`

**File:** `internal/lifecycle/lifecycle.go:282-322, 325-350`; `internal/supervisor/supervisor.go:206`
**Issue:** The supervisor is started with `Setsid` and `run.json` is written only after the engine, firmware and control socket are up (`supervisor.go:206`). `Up` handles SIGINT/SIGTERM through its context (and `abort()`), but SIGHUP (closed terminal) and SIGKILL of `sil up` in that window leave a running supervisor and firmware with no `run.json`. Afterwards `sil down` answers `stopped:false` (no run.json), `checkNotRunning` treats the instance as not running, and the next `sil up` wipes the run directory under the live orphan (`ResetRunDir`) and then fails with `port_in_use` for the orphan's API port. The user has to find and kill the process by hand.
**Fix:** Have `Up` register SIGHUP in its `signal.NotifyContext` list (`internal/cli/cli.go:146`) so it takes the `abort()` path, and let the supervisor write a provisional `run.json` (or pid file) before `startFirmware` so the lifecycle commands can see and stop it.

## Info

### IN-01: Unused constant `maxRestartGap` duplicates `control.MaxRestartGap`

**File:** `internal/supervisor/supervisor.go:58`
**Issue:** `maxRestartGap = 30 * time.Second` is never referenced; the real limit lives in `control.MaxRestartGap`. Two sources for one limit invite drift.
**Fix:** Delete the constant.

### IN-02: Dead or unused exports

**File:** `internal/fanlib/fanlib.go:106` (`IDs`), `internal/bus/codec.go:152` (`Frame.Keys`), `internal/instance/instance.go:162-163` (`FriendlyName`)
**Issue:** No production caller (only tests or none). `FriendlyName` is also not what the firmware uses: `examples/fan-skeleton.yaml:16` hand-writes `friendly_name: SiL ${sil_name}`, so the Go helper and the YAML can drift.
**Fix:** Remove them, or inject `friendly_name` as a substitution from `FriendlyName()` and lint it like the name.

### IN-03: Default socket path `/tmp/sil.sock` is a predictable world-creatable location

**File:** `components/sim_bus/__init__.py:47`
**Issue:** Hand-run firmware without `SIL_BUS` connects to a fixed `/tmp` path another local user can pre-create and then feed fake RPM or read duty frames. This is already listed as an unregistered flag in 01-SECURITY.md; noting that the fix is one line.
**Fix:** Make `path:` required (drop the default) or default to `${XDG_RUNTIME_DIR}`-relative at runtime in `SimBus::setup` and refuse a path whose parent is not owned by the user.

### IN-04: Redundant liveness check in `checkNotRunning`

**File:** `internal/lifecycle/lifecycle.go:230-231`
**Issue:** `instance.ReadRunFile` already returns `not_running` when `ProcessAlive` is false, so the second `!instance.ProcessAlive(...)` can never be true and re-reads `/proc` for nothing.
**Fix:** `rf, err := instance.ReadRunFile(p); if err != nil { return nil }`.

### IN-05: Docker `-v` is built by string concatenation of user-controlled paths

**File:** `internal/fwbuild/fwbuild.go:393-401` (`dockerArgs`)
**Issue:** `"-v", mount + ":" + mount + ":ro"` breaks for a repo or config path containing `:` (docker splits the spec at the wrong place and fails with a confusing message); a `,` is fine here but would not be with `--mount`. Not exploitable (read-only, caller's uid, own path), just a robustness gap.
**Fix:** Reject `:` in the mount root with a clear `config_invalid`, or use `--mount type=bind,source=...,target=...,readonly` (and reject `,`).

### IN-06: `.claude/CLAUDE.md` still says the architecture, conventions and skills are unmapped, and that Go is not installed

**File:** `.claude/CLAUDE.md` ("Conventions not yet established", "Architecture not yet mapped", "Go is not installed on this machine")
**Issue:** The repo now has a documented package layout, a build/test workflow (`make check`, `make e2e`), a wall-clock lint rule (only `internal/clock` may read wall time) and Go 1.27.3 installed under `~/.local/bin`. Agents reading this file get stale and partly wrong guidance (for example, nothing warns that `time.Now`, `context.WithTimeout` and `http.Server` timeouts fail `internal/lint`).
**Fix:** Replace the placeholder sections with the actual package map, the `clock.Clock` rule and the `make` targets.

---

_Reviewed: 2026-11-09T03:57:28Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
