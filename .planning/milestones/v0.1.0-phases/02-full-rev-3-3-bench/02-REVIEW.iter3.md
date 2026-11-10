---
phase: 02-full-rev-3-3-bench
reviewed: 2026-11-09T00:00:00Z
depth: standard
files_reviewed: 104
files_reviewed_list:
  - cmd/sil/main.go
  - components/sim_bus/binary_sensor.py
  - components/sim_bus/__init__.py
  - components/sim_bus/light.py
  - components/sim_bus/output.py
  - components/sim_bus/sensor.py
  - components/sim_bus/sim_binary_sensor.h
  - components/sim_bus/sim_bus.cpp
  - components/sim_bus/sim_bus.h
  - components/sim_bus/sim_light.cpp
  - components/sim_bus/sim_light.h
  - components/sim_bus/sim_output.cpp
  - components/sim_bus/sim_output.h
  - components/sim_bus/sim_protocol.h
  - components/sim_bus/sim_sensor.cpp
  - components/sim_bus/sim_sensor.h
  - e2e/api_client.py
  - e2e/build_test.go
  - e2e/cli_test.go
  - e2e/codegen_test.go
  - e2e/fans_test.go
  - e2e/firmware_test.go
  - e2e/harness_test.go
  - e2e/io_test.go
  - e2e/isolation_test.go
  - e2e/leds_test.go
  - e2e/loop_test.go
  - e2e/model_test.go
  - e2e/restart_test.go
  - e2e/rev33_test.go
  - e2e/supervise_test.go
  - e2e/testdata/with-i2c.yaml
  - e2e/testdata/with-ledc.yaml
  - e2e/testdata/with-wifi-ota.yaml
  - e2e/tracer_test.go
  - e2e/watch_test.go
  - examples/fan-skeleton.yaml
  - examples/rev33.yaml
  - fans/arctic-p12-pwm-pst.toml
  - fans/embed.go
  - fans/noctua-nf-a12x25-pwm.toml
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
  - internal/cli/logs_test.go
  - internal/cli/resolve.go
  - internal/cli/watch_test.go
  - internal/clock/clock.go
  - internal/clock/manual.go
  - internal/clock/manual_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/events/events.go
  - internal/events/events_test.go
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
  - internal/instance/instance.go
  - internal/instance/instance_test.go
  - internal/instance/lanhost.go
  - internal/instance/lanhost_test.go
  - internal/instance/lock.go
  - internal/instance/port.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/lint/wallclock_test.go
  - internal/model/fan.go
  - internal/model/fan_test.go
  - internal/supervisor/firmware.go
  - internal/supervisor/logs.go
  - internal/supervisor/logs_test.go
  - internal/supervisor/policy.go
  - internal/supervisor/policy_test.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - internal/supervisor/watch.go
  - internal/supervisor/watch_test.go
  - Makefile
  - tests/conftest.py
  - tests/cpp/test_protocol.cpp
  - tests/pyproject.toml
  - tests/silbench.py
  - tests/test_api_smoke.py
  - tests/test_contract.py
  - tests/test_identity.py
  - tests/tools/lockdiff.py
findings:
  critical: 0
  warning: 2
  info: 8
  total: 10
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-11-09
**Depth:** standard
**Files Reviewed:** 104
**Status:** issues_found

## Summary

This is the second pass after the fixer's commits ce2c756, e4f1046, 1469f38, 291aebe and febfe21. The changed files were re-read in full against the diffs: `internal/fwbuild/cache.go`, `internal/fwbuild/fwbuild.go`, `internal/supervisor/supervisor.go`, `internal/engine/engine.go`, `internal/instance/instance.go` and their tests. The rest of the scope got a lighter pass, and the previous info findings were re-checked against the current source.

Status of the previous warnings:

| ID | Verdict |
|----|---------|
| WR-01 (symlinked component dirs not hashed) | Resolved. `collectRels` uses `os.ReadDir` and `os.Stat`, which follow links. The depth bound turns a loop into `build_failed`. Relative paths and the hash framing are unchanged. A dangling link still errors, as before. |
| WR-02 (`Set` records before apply) | Mostly resolved. The cancellation reasoning is sound. One residual gap remains (new WR-02 below). |
| WR-03 (`config_invalid` too broad) | Resolved. Exit codes 1 and 2 are the right set: 2 for a rejected YAML and 1 for any other `EsphomeError`. The deviation from "exactly 1" is justified. A Python traceback also exits 1 and is still shown as `config_invalid` with its tail. That is acceptable, because the tail carries the message. |
| WR-04 (unbounded warnings) | Resolved. The cap and the rune-boundary cut are correct: `w[cut]` is in range because `len(w) > 256`, and the loop terminates. The duplicate check runs before the suppression note, so a repeated note is not appended. Only 63 real entries fit, which is acceptable and documented by the test. |
| WR-05 (busy `--api-port` persisted) | Resolved in `instance` and covered by a unit test. The fix breaks an e2e test that was not run (new WR-01 below). |

WR-02 cancellation reasoning, traced:

- `context.WithoutCancel(ctx)` combined with `withDown` is sound. `Apply` can then return a context error only when a shutdown cancelled it, and the `actx.Err() != nil` arm maps that to `stopping`.
- Dropping client cancellation holds `inMu` until the engine replies. Only `Set`, `SimRestart` and startup take `inMu`. `SimRestart` cancels the engine before it takes `inMu`, which makes `Apply` return `ErrStopped`. A wedged engine therefore cannot deadlock a restart.
- The restart-gap acceptance is race-free. `startEngine` and the slot swap run entirely under `inMu`, so a `Set` either lands before the new engine reads `inputs` or sees the new engine.
- Lock order `inMu` then `mu` in the new `engine_failed` check matches the documented order.
- The `default:` arm is unreachable (see IN-08).

## Warnings

### WR-01: e2e `TestPortInUse` still asserts the pre-WR-05 sticky busy port and now fails

**File:** `e2e/isolation_test.go:252-256`
**Issue:** The test first runs `up e2e-iso-c --api-port <busy>` and expects `port_in_use`. It then runs `up e2e-iso-c` with no flag and expects `port_in_use` again, with `detail.port == port`. The comment says "The stored (sticky) port is taken: still port_in_use, no silent move."

After febfe21, the first call fails before `identity.json` is written (that is the point of WR-05). The second call finds no identity and allocates the lowest free port from 6054. It proceeds to a docker build and a firmware start instead of failing with `port_in_use`.

`env.Sil` does not inject an `--api-port` (only `env.Up` does). The test therefore fails on the exit code, or runs a multi-minute build before failing, and may leave an `e2e-iso-c` instance running. The fixer did not run the `sil_e2e` suite, so this was not caught. It is also the only e2e coverage of the sticky-port path, and that coverage is now gone.
**Fix:** Make the test describe the new contract. After the busy `--api-port` call, assert that no `identity.json` was stored. To keep the sticky-busy-port check, first create the identity on a free port (a first `up` or `instance.LoadOrCreate`), then hold that port and expect `port_in_use` with no flag.
```go
// busy override is rejected and not stored
out := isoExpectError(t, env, 2, "port_in_use", "up", "e2e-iso-c", "--api-port", fmt.Sprint(port))
// ... existing detail assertions ...
if _, err := os.Stat(filepath.Join(env.State, "sil", "e2e-iso-c", "identity.json")); err == nil {
    t.Errorf("a busy --api-port was persisted")
}
```
Use whatever accessor the harness has for the state directory.

### WR-02: `Set` treats `ErrStopped` as a restart gap whenever the failure is not `engine_failed`, so a dead engine can still report success

**File:** `internal/supervisor/supervisor.go:1134-1143` (with `internal/supervisor/supervisor.go:776-780`)
**Issue:** The fix uses "`s.failure.Code == engine_failed`" as the sign that the engine is dead for good. `SimRestart` records `engine_failed` only `if s.failure == nil` (lines 776-780). It deliberately keeps an existing failure such as `firmware_crash_loop`.

If a `SimRestart` fails to start the new engine while another failure is held, then:

- The slot keeps the stopped engine.
- No `engine_failed` is recorded.
- `Set` reaches `ErrStopped`, sees `failed == false`, keeps the record and returns `Applied`.

Nothing is applied until another `SimRestart` succeeds. This is the exact hazard WR-02 described (success reported while nothing is applied). It is much narrower than before, because it needs a double fault, but the discriminator is a proxy and not the real state.

The same applies in the instant between an unexpected engine death and the shutdown it triggers, which is harmless there.
**Fix:** Decide on real state. Track "a restart is in flight" under `mu`, set in `SimRestart` before `stopEngine(old)` and cleared when the new slot is installed or `startEngine` fails. Accept `ErrStopped` only while that flag is set. Otherwise roll back and return `engine_failed`.
```go
s.mu.Lock(); gap := s.engGap; s.mu.Unlock()
if !gap { rollback(); return control.SetResult{}, apperr.Infra("engine_failed", "...", nil) }
```
This would also drop the dependence on the failure slot.

## Info

### IN-01: Comment in `light.py` states the wrong pixel size

**File:** `components/sim_bus/light.py:20`
**Issue:** "255 LEDs are 510 hex characters of pixels" is wrong. A pixel is 3 bytes, so 6 hex characters, which gives 1530 for 255 LEDs. `engine.go` (`ledMaxPxLen = 6 * ledMaxN`) has the right figure. The limit itself is correct. Only the explanation misleads.
**Fix:** Change the comment to "255 LEDs are 1530 hex characters of pixels".

### IN-02: Firmware hub gives no diagnostic for a failing connect, and `SIL_BUS` bypasses the 107-byte path check

**File:** `components/sim_bus/sim_bus.cpp:25-26`, `components/sim_bus/sim_bus.cpp:46-53`
**Issue:** The YAML `path` is length-checked in `validate_path`. A `SIL_BUS` value from the environment is not. `strncpy` truncates silently, so a hand-run firmware with a long `SIL_BUS` retries forever against a truncated path. No log line is produced, because the connect failure branch only closes and backs off. `sil` guards its own paths with `CheckSocketPaths`, so this affects hand-run and third-party launches only.
**Fix:** In `try_connect_`, if `this->path_.size() >= sizeof(a.sun_path)`, log once with `ESP_LOGE`, close the fd and back off. Optionally log the first failed connect with `errno`.

### IN-03: Control handlers accept trailing garbage after the JSON body

**File:** `internal/control/control.go:375-381`, `internal/control/control.go:400-405`, `internal/control/control.go:428-433`
**Issue:** `json.Decoder.Decode` reads one value and ignores the rest. A body like `{"hold_ms":200}garbage` is accepted, although `DisallowUnknownFields` is meant to make the API strict. Bodies are also truncated silently at 64 KiB by `io.LimitReader`, and the parse error then reads as "bad request body" rather than "too large".
**Fix:** After `Decode`, require EOF, for example `dec.Decode(&struct{}{}) != io.EOF` gives `invalid_argument`.

### IN-04: `lanHostPtr` is duplicated

**File:** `internal/lifecycle/lifecycle.go:548-553`, `internal/supervisor/supervisor.go:1293-1298`
**Issue:** Two identical helpers exist in two packages. `instance` already owns `LANHost`, so the pointer form belongs there.
**Fix:** Add `instance.LANHostPtr()` and delete both copies.

### IN-05: Broker drops the final `lagged` marker when the stream ends

**File:** `internal/events/events.go:100-117`, `internal/events/events.go:172-180`
**Issue:** A subscriber that overflowed gets its `lagged` marker only when space for two events appears (`need = 2`). If the instance stops before another event is offered, `Broker.Close` closes the channel with `s.dropped > 0` still pending. The watcher sees `stopped` without learning that events were lost. This matters for consumers that treat "no `lagged` line" as "complete".
**Fix:** In `Close`, if `s.dropped > 0` and the channel has room, deliver the marker before `close(s.ch)`. If it has none, reserve one slot for it.

### IN-06: Test-support Python has dead state and a broad exception swallow

**File:** `tests/silbench.py:98`, `tests/silbench.py:163`, `tests/silbench.py:211`, `tests/silbench.py:317`, `tests/silbench.py:333`, `tests/silbench.py:408`
**Issue:**
- `Bench._started` is added to in `up()` and never read.
- Several cleanup paths catch `BaseException`, which also swallows `KeyboardInterrupt` and `SystemExit`. Ctrl-C during teardown is ignored.
- `Api._info` is first assigned in `_connect()` rather than `__init__`.
- The `port_in_use` retry at line 157 is now redundant for a sticky busy port (WR-05), because the sticky port is no longer polluted by a failed override. It is harmless.

**Fix:** Remove `_started`, use `except Exception`, initialise `self._info = None` in `__init__`, and drop the retry once the e2e suite confirms it is unneeded.

### IN-07: `sim_light.h` uses `std::fill` without including `<algorithm>`

**File:** `components/sim_bus/sim_light.h:40`
**Issue:** `clear_effect_data()` calls `std::fill`, but the header only includes `<string>` and `<vector>`. It compiles today through ESPHome's transitive includes, which a future ESPHome bump can break. The `.cpp` includes `<algorithm>`, but the header must stand on its own.
**Fix:** Add `#include <algorithm>` to `sim_light.h`.

### IN-08: `Set` has an unreachable `default:` arm with an untested rollback

**File:** `internal/supervisor/supervisor.go:1147-1149`
**Issue:** `Engine.Apply` returns only `nil`, `ErrStopped` or a context error. `actx` has no deadline and `context.WithoutCancel` drops the caller's cancellation, so a non-nil, non-`ErrStopped` error implies `actx.Err() != nil`, which the previous arm handles. The `default:` branch and its `rollback()` can never run, so no test exercises them. It is harmless defensive code, but it reads as if a real error path exists.
**Fix:** Either remove the arm (return `err` unchanged) or keep it with a comment saying it guards a future `Apply` error type.

---

_Reviewed: 2026-11-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
