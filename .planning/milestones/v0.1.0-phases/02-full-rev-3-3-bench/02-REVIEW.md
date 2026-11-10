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
  warning: 1
  info: 10
  total: 11
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-11-09
**Depth:** standard
**Files Reviewed:** 104
**Status:** issues_found

## Summary

Iteration 3 (last). Both warnings from the previous review are resolved:

- **Previous WR-01 (`TestPortInUse`)**: resolved. The test now asserts the WR-05 contract. A busy explicit `--api-port` exits 2 `port_in_use` with no `identity.json` and no `run.json`. The sticky-busy-port case is covered by seeding an identity on a free port, holding that port, and running a plain `up`. The assertion that the seeded file is honoured (`detail.port == sticky`) also proves the state path in the first assertion is right, so the absence check is not vacuous.
- **Previous WR-02 (`Set` dead-engine detection)**: resolved. `Set` now decides on the real `engGap` flag instead of the failure code.

`engGap` checks, done by tracing the code:

- **Lock order.** Every place that takes two locks takes `inMu` and then `mu`. `Set` is `inMu`, then `currentEngine()`, then `mu` for `engGap` and `stopping`. `SimRestart` takes `mu` alone, releases it, and later takes `inMu` and then `mu`. `run()` also takes `inMu` and then `mu`. Nothing takes `mu` and then `inMu`, so there is no deadlock. `stopEngine` takes no lock, so a `Set` holding `inMu` inside `Apply` is released as soon as the engine is cancelled.
- **Ordering.** `engGap = true` is written under `mu` before `stopEngine(old)` cancels the engine. `Set` can only see `ErrStopped` from that engine after the cancel, and it reads `engGap` under `mu` afterwards. It therefore always sees the flag set. Clearing on install, on `startEngine` failure and on the stopping check all happen while `SimRestart` holds `inMu`. A `Set` that holds `inMu` across `Apply` and the flag read cannot see the flag cleared before the new engine is installed.
- **Lost updates.** A value recorded in the gap is read by `startEngine` under `inMu`, so it seeds the next engine. A gap Set followed by a failed `startEngine` keeps the record on purpose, and the failure is reported to the `SimRestart` caller.
- **Missed resets.** The flag is cleared on all four non-panic exits: sleep abort, `startEngine` error, stopping abort, and normal install. See IN-10 for the panic path.
- **Residual details.** IN-09 and IN-10 below.

`TestSupervisorRebootExit`: the new code does not touch it. The `newFake` supervisors have no slot or engine and never read or write `engGap`. The flake is a pre-existing race in the test itself. See WR-01. I could not reproduce it (200 runs with `-race` at `-cpu 1,4`, plus 2000 runs at `-cpu 1` with 8 busy loops), but the race is visible in the code.

Light confirmation pass over `internal/fwbuild/cache.go`, `fwbuild.go`, `internal/engine/engine.go` and `internal/instance/instance.go`: no new defects. Details:

- `collectRels` bounds its recursion with `maxTreeDepth`. It follows symlinks like the compiler and skips `__pycache__` and `.pyc`.
- `warn` indexes `w[cut]` only when `len(w) > maxWarningLen`, so the index is in range. It cuts on a rune boundary, dedupes after truncation, and caps the list.
- `stageFailure` limits `config_invalid` to exit codes 1 and 2.
- `LoadOrCreate` bind-tests an explicit port before persisting it, and leaves an already stored port unchanged.

`go vet ./...` is clean. `go test -race -count=3 ./internal/supervisor/` passes.

## Warnings

### WR-01: `TestSupervisorRebootExit` asserts the probe count before the probe is started (flaky)

**File:** `internal/supervisor/policy_test.go:327-336`, racing with `internal/supervisor/supervisor.go:374` and `internal/supervisor/supervisor.go:614-624`
**Issue:** The test waits for `restarts == 1 && running`, then immediately requires `probes.count() == 2`. In `autoRestart` the order is:

1. `s.restarts++` (line 614).
2. `startFirmware()`, which sets `fwRunning = true` (line 374).
3. `startProbe()`, which spawns a goroutine that increments the counter.

So the polled condition becomes true after step 2, before step 3 has run and before the probe goroutine has been scheduled. At that moment `probes.count()` is still 1 and the assertion fails with "2 launches and 1 probes, want 2 and 2". This is exactly the failure the fixer saw once under full parallel load. It is independent of the `engGap` change. It is a test-reliability defect that will fail CI at random.

**Fix:** Poll on the quantity that is asserted. The other launch and probe checks in the file have a similar shape, but they run after a real 50 ms wait or after `restarting` has been observed false, so only this test is exposed.
```go
eventually(t, "the restart and its probe", func() bool {
	r, _, _, running := s.snapshotState()
	return r == 1 && running && probes.count() == 2
})
```
Or wait for `restarting == false`, which `autoRestart` clears only after `startProbe()`. A probe goroutine that has been started but not yet scheduled still needs the `probes.count()` poll.

## Info

### IN-01: Comment in `light.py` states the wrong pixel size

**File:** `components/sim_bus/light.py:20`
**Issue:** "255 LEDs are 510 hex characters of pixels" is wrong. A pixel is 3 bytes, so 6 hex characters, which gives 1530 for 255 LEDs. `engine.go` (`ledMaxPxLen = 6 * ledMaxN`) has the right figure. The limit itself is correct.
**Fix:** Change the comment to "255 LEDs are 1530 hex characters of pixels".

### IN-02: Firmware hub gives no diagnostic for a failing connect, and `SIL_BUS` bypasses the 107-byte path check

**File:** `components/sim_bus/sim_bus.cpp:25-26`, `components/sim_bus/sim_bus.cpp:46-53`
**Issue:** The YAML `path` is length-checked in `validate_path`. A `SIL_BUS` value from the environment is not. `strncpy` truncates silently, so a hand-run firmware with a long `SIL_BUS` retries forever against a truncated path and logs nothing. `sil` guards its own paths with `CheckSocketPaths`, so only hand-run and third-party launches are affected.
**Fix:** In `try_connect_`, if `this->path_.size() >= sizeof(a.sun_path)`, log once with `ESP_LOGE`, close the fd and back off.

### IN-03: Control handlers accept trailing garbage after the JSON body

**File:** `internal/control/control.go:369-380`, `internal/control/control.go:395-405`, `internal/control/control.go:423-433`
**Issue:** `json.Decoder.Decode` reads one value and ignores the rest. `{"hold_ms":200}garbage` is accepted although `DisallowUnknownFields` is meant to make the API strict. Bodies are also truncated silently at 64 KiB by `io.LimitReader`, and the parse error then reads as "bad request body" rather than "too large".
**Fix:** After `Decode`, require EOF, for example `dec.Decode(&struct{}{}) != io.EOF` gives `invalid_argument`.

### IN-04: `lanHostPtr` is duplicated

**File:** `internal/lifecycle/lifecycle.go:548`, `internal/supervisor/supervisor.go:1312`
**Issue:** Two identical helpers exist in two packages. `instance` already owns `LANHost`, so the pointer form belongs there.
**Fix:** Add `instance.LANHostPtr()` and delete both copies.

### IN-05: Broker drops the final `lagged` marker when the stream ends

**File:** `internal/events/events.go:100-120`, `internal/events/events.go:166-178`
**Issue:** A subscriber that overflowed gets its `lagged` marker only when room for two events appears (`need = 2`). If the instance stops before another event is offered, `Broker.Close` closes the channel with `s.dropped > 0` still pending. The watcher sees `stopped` without learning that events were lost. This matters for consumers that treat "no `lagged` line" as "complete".
**Fix:** In `Close`, if `s.dropped > 0` and the channel has room, deliver the marker before `close(s.ch)`. If it has none, reserve one slot for it.

### IN-06: Test-support Python has dead state and a broad exception swallow

**File:** `tests/silbench.py:98`, `tests/silbench.py:163`, `tests/silbench.py:211`, `tests/silbench.py:317`, `tests/silbench.py:408`
**Issue:**
- `Bench._started` is added to in `up()` and never read.
- Several cleanup paths catch `BaseException`, which also swallows `KeyboardInterrupt` and `SystemExit`. Ctrl-C during teardown is ignored.
- `Api._info` is first assigned in `_connect()` rather than `__init__`.
- The `port_in_use` retry near line 157 is redundant for a sticky busy port since WR-05. It is harmless.

**Fix:** Remove `_started`, use `except Exception`, initialise `self._info = None` in `__init__`, and drop the retry once the e2e suite confirms it is unneeded.

### IN-07: `sim_light.h` uses `std::fill` without including `<algorithm>`

**File:** `components/sim_bus/sim_light.h:40`
**Issue:** `clear_effect_data()` calls `std::fill`, but the header only includes `<string>` and `<vector>`. It compiles through ESPHome's transitive includes, which a future ESPHome bump can break.
**Fix:** Add `#include <algorithm>` to `sim_light.h`.

### IN-08: `Set` has an unreachable `default:` arm with an untested rollback

**File:** `internal/supervisor/supervisor.go:1166-1168`
**Issue:** `Engine.Apply` returns only `nil`, `ErrStopped` or a context error. `actx` has no deadline and `context.WithoutCancel` drops the caller's cancellation, so a non-nil, non-`ErrStopped` error implies `actx.Err() != nil`, which the previous arm handles. The `default:` branch and its `rollback()` can never run, so no test exercises them.
**Fix:** Remove the arm (return `err` unchanged), or keep it with a comment saying it guards a future `Apply` error type.

### IN-09: A `Set` racing a shutdown abort of `SimRestart` reports `engine_failed` instead of `stopping`

**File:** `internal/supervisor/supervisor.go:768-772` (with `internal/supervisor/supervisor.go:1153-1161` and `internal/supervisor/supervisor.go:414-427`)
**Issue:** `shutdown()` closes `downReq` first and then waits for `opMu`. `s.stopping` is set only later, in `stopAll`, which runs after the `SimRestart` in flight has returned. When the gap sleep is aborted, `endEngGap()` clears `engGap` while `stopping` is still false, and the slot holds the stopped engine. A `Set` arriving in that window gets `ErrStopped`, reads `gap=false, stopping=false`, rolls back and returns `engine_failed: the simulator engine is not running`. That reads as an engine fault, although the cause is a shutdown. The fix report says a shutdown abort returns `stopping`, which holds only for the later `s.stopping` window. Harmless to state, since the instance is going down, but it is the mislabel WR-02 was meant to remove.
**Fix:** In the `!gap` branch of `Set`, treat a requested shutdown like `stopping`:
```go
if stopping || isDown(s.downReq) {
	return control.SetResult{}, errStopping()
}
```
Nothing was recorded for the stopping case before the check, so add `rollback()` there as well to keep the record equal to the engine.

### IN-10: `engGap` is not reset if `SimRestart` panics between the set and the clear

**File:** `internal/supervisor/supervisor.go:764-803`
**Issue:** `engGap` is set at line 764 and cleared by hand on each return path. A panic between (in `stopEngine`, `startEngine` or `engine.New`) is not covered. The flag then stays set, and every later `Set` on the dead engine would be accepted as a "gap" and reported as applied, which is the original WR-02 symptom. `net/http` recovers handler panics, so the instance would keep serving. This is theoretical, because those calls do not panic today, but all the other resets are explicit and this one is not defensive. The line 792 `s.stopping` branch is also effectively dead, because `stopAll` runs only under `opMu` and `SimRestart` holds it.
**Fix:** Clear the flag with a defer so every exit, including a panic, resets it. `Set`'s read stays correct because the install and the clear share one `mu` section.
```go
s.mu.Lock()
...
s.engGap = true
s.mu.Unlock()
defer s.endEngGap() // idempotent; the explicit clears can stay
```

---

_Reviewed: 2026-11-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
