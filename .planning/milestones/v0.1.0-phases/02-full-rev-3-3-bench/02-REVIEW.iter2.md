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
  warning: 5
  info: 7
  total: 12
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-11-09
**Depth:** standard
**Files Reviewed:** 104
**Status:** issues_found

## Summary

Non-test source (internal/, cmd/, components/sim_bus, tests/silbench.py, tests/tools/lockdiff.py, Makefile) was read in full. Test files were skimmed for reliability problems only (missing assertions, unconditional skips, leaked processes); none turned up.

Checks run during the review:

- `go vet ./...`, `gofmt -l` and `go test -count=1 ./...` are clean.
- `make test-cpp` passes (119 vectors).
- A differential fuzz of the Go codec (`internal/bus/codec.go`) against the C++ codec (`sim_protocol.h`) was run on 300,000 generated and mutated lines. It found no divergence in accept/reject, error class or decoded fields. The only differences were from the harness, where `bufio.Scanner` strips a trailing `\r`.

The code is careful overall. The wire protocol, the supervisor lock order (opMu, inMu, mu), the engine single-writer discipline and the key redaction in logs and build output all held up under tracing. No security vulnerability or data-loss defect was found. The findings below are correctness and robustness defects, each traced to a concrete path:

- a silently stale build cache
- a misclassified build failure
- state recorded before it is applied
- an unbounded peer-driven growth
- a persisted `--api-port` override that was never validated

## Warnings

### WR-01: Build cache hash ignores a component directory that is a symlink (stale firmware reused)

**File:** `internal/fwbuild/cache.go:83-108`
**Issue:** `hashTree` walks `root` with `filepath.WalkDir`. When `root` itself is a symlink, WalkDir calls `Lstat`, so the root entry is not a directory. The callback sees one non-directory entry, `os.Stat` shows it is a directory rather than a regular file, and nothing is recorded in `rels`.

The component tree then hashes as `tree componentN 0`. Edits to `sim_bus` C++ or Python sources, or to an included header, no longer change the input hash. `sil up` reports `build cache hit` and runs the old binary.

This was confirmed with a standalone WalkDir probe: `real/` reports 1 file, a symlink to it reports 0. The same applies to symlinked sub-directories inside a tree, because `d.IsDir()` is false for the link and the `info.Mode().IsRegular()` check then drops it. The comment "follows symlinks, like the compiler" is therefore true for files but not for directories.

`localInputs` builds the path as `abs(p)` from the resolved `path:` value without resolving links. A `path: ../components` that goes through a symlinked checkout is enough to trigger the stale build.
**Fix:** Resolve the root and walk with symlink-aware logic.
```go
if real, err := filepath.EvalSymlinks(root); err == nil {
    root = real
}
// ... inside the WalkDir callback, treat a symlink that Stat()s as a directory by recursing:
if d.Type()&fs.ModeSymlink != 0 {
    if info, err := os.Stat(p); err == nil && info.IsDir() {
        sub, err := collectRels(p) // same walk, rels prefixed with the link's relative path
        ...
    }
}
```
At minimum, resolve the root with `EvalSymlinks` and fail the hash (`build_failed`) rather than hash an empty tree when a non-regular, non-directory entry is skipped.

### WR-02: `Set` records the new inputs and fans before the engine has applied them

**File:** `internal/supervisor/supervisor.go:1110-1124`
**Issue:** `Set` writes `s.inputs` and `s.fans` under `inMu`, then calls `Apply`. `Apply` can fail with the caller's context error. That error is returned to the client as a failure and is not swallowed.

When the client has gone, or the context ended before the event was enqueued, the supervisor's record and the live engine disagree. `Status` and `watch` show the old values, while the next sim restart seeds the engine from the new ones. The "validated, then applied together" contract is then broken: the request is reported as failed but takes effect later, silently.

The reverse also exists. `ErrStopped` is swallowed (`!errors.Is(err, engine.ErrStopped)`) even when the current engine is permanently dead, for example after `engine_failed`. `Set` then reports success while nothing is applied.
**Fix:** Record the state only after `Apply` succeeded or the engine is known to be restarting. Alternatively, roll back on a context error.
```go
prevIn, prevFans := s.inputs, append([]engine.PortConfig(nil), s.fans...)
// apply to s.inputs / s.fans as now ...
if err := s.currentEngine().Apply(ctx, change); err != nil && !errors.Is(err, engine.ErrStopped) {
    s.inputs, s.fans = prevIn, prevFans // engine did not take it; keep record == engine
    return control.SetResult{}, err
}
```
Treat `ErrStopped` as success only while a `SimRestart` is in flight (`s.engRestarts` gap), not when `s.failure` is `engine_failed`.

### WR-03: Any docker failure other than 125..127 in the config/generate stage is reported as a user-fixable `config_invalid`

**File:** `internal/fwbuild/fwbuild.go:332-342`
**Issue:** `stageFailure` treats an `*exec.ExitError` as an ESPHome rejection unless its exit code is 125..127. A container killed by the OOM killer (docker exits 137), a docker client killed by a signal (`ExitCode()` is -1) or any other code is therefore reported as `config_invalid` (exit class 1, "fix your YAML"). The tail is usually empty.

The compile stage is stricter: `classifyCompileFailure` requires exit code 1 plus a positive compiler diagnostic. The two stages are inconsistent. A user would chase their YAML for what is bench infrastructure, and an agent that branches on exit class 1 versus 2 gets the wrong answer.
**Fix:** Require ESPHome's own failure exit code.
```go
if !errors.As(err, &ee) || ee.ExitCode() != 1 {
    return apperr.Infra("build_failed", what+": docker could not run the pinned image or the container was killed",
        map[string]any{"error": err.Error(), "log": b.o.LogPath})
}
```

### WR-04: Engine warnings are unbounded and are re-copied on every publish

**File:** `internal/engine/engine.go:527-534`, `internal/engine/engine.go:852-854`, `internal/engine/engine.go:289-300`
**Issue:** `warn` de-duplicates identical strings only. `handleFrame` builds a warning from the peer-supplied channel name (`"unknown channel %s (ignored)"`, up to about 4 KB). A firmware or any same-user process on the bus socket that sends `out` frames with ever-new `ch` values grows `e.warnings` without bound for the whole session. Warnings reset only on hello or disconnect.

Every `publish()` runs `build()`. That copies the full slice, and it runs on every 10 ms tick and every inbox event. `Status` and `watch` snapshots also serialize the whole list. The cost therefore grows with the number of distinct warnings, and the status JSON can reach megabytes.

Other warn sites interpolate peer data too: `peer_error: <msg>` via `evDisconnected`, and the channel names in `checkManifest`. The manifest is capped at 1024 chan frames; the `out` path is not capped at all.
**Fix:** Cap the list and truncate each entry.
```go
const maxWarnings, maxWarningLen = 64, 256

func (e *Engine) warn(w string) {
    if len(w) > maxWarningLen { w = w[:maxWarningLen] + "..." }
    for _, have := range e.warnings { if have == w { return } }
    if len(e.warnings) >= maxWarnings { return }
    e.warnings = append(e.warnings, w)
}
```

### WR-05: `sil up --api-port N` persists N to identity.json before N is known to be usable

**File:** `internal/lifecycle/lifecycle.go:167-173`, `internal/instance/instance.go:194-228`
**Issue:** `LoadOrCreate(p, o.APIPort)` writes the override port into `identity.json` (sticky), and only then does `Up` call `TestBind`. If the port is busy, `Up` fails with `port_in_use`, but the identity keeps the busy port.

Every later plain `sil up` (no flag) re-uses it ("sticky; a taken sticky port is reported by TestBind, never moved") and fails again with the same error until the user passes another `--api-port`. A typo or a transient conflict therefore permanently re-points the Home Assistant device port. `tests/silbench.py:Bench.up` already works around the symptom with a retry on `port_in_use`.

The first-use identity written before a failed build is documented as acceptable. A flag value the user explicitly chose and that was never validated is different.
**Fix:** Bind-test the requested port before it is persisted. Alternatively, write the identity only after `TestBind` succeeds.
```go
if o.APIPort != 0 {
    if err := instance.TestBind(o.APIPort); err != nil { return UpResult{}, err }
}
id, err := instance.LoadOrCreate(p, o.APIPort)
```

## Info

### IN-01: Comment in `light.py` states the wrong pixel size

**File:** `components/sim_bus/light.py:20`
**Issue:** "255 LEDs are 510 hex characters of pixels" is wrong. A pixel is 3 bytes, so 6 hex characters, which gives 1530 characters for 255 LEDs. `engine.go` (`ledMaxPxLen = 6 * ledMaxN`) has the right figure. The limit itself is correct, since 1530 plus framing is under 4095. Only the explanation misleads whoever adjusts `MAX_LEDS`.
**Fix:** Change the comment to "255 LEDs are 1530 hex characters of pixels".

### IN-02: Firmware hub gives no diagnostic for a failing connect, and `SIL_BUS` bypasses the 107-byte path check

**File:** `components/sim_bus/sim_bus.cpp:25-26`, `components/sim_bus/sim_bus.cpp:46-53`
**Issue:** The YAML `path` is length-checked in `validate_path`. A `SIL_BUS` value from the environment is not. `strncpy` into `sun_path` truncates silently, so a hand-run firmware with a long `SIL_BUS` retries forever against a truncated, non-existent path. No log line is produced at any point, because the connect failure branch only closes and backs off. `sil` itself guards its own paths with `CheckSocketPaths`, so this affects hand-run and third-party launches only.
**Fix:**
```cpp
if (this->path_.size() >= sizeof(a.sun_path)) {
  ESP_LOGE(TAG, "sim bus path is %u bytes; sun_path holds %u", ...);
  ::close(fd); this->bump_backoff_(); return;
}
```
Optionally log the first failed connect once (`errno`).

### IN-03: Control handlers accept trailing garbage after the JSON body

**File:** `internal/control/control.go:375-381`, `internal/control/control.go:400-405`, `internal/control/control.go:428-433`
**Issue:** `json.Decoder.Decode` reads one value and ignores the rest. A body like `{"hold_ms":200} {"x":` or `{"values":{...}}garbage` is accepted, although `DisallowUnknownFields` is clearly meant to make the API strict. Bodies are also truncated silently at 64 KiB by `io.LimitReader`; the resulting parse error reads as "bad request body" rather than "too large".
**Fix:** After `Decode`, require EOF.
```go
if dec.More() || dec.Decode(&struct{}{}) != io.EOF {
    reply(w, nil, apperr.Failed("invalid_argument", "bad request body: trailing data", nil)); return
}
```

### IN-04: `lanHostPtr` is duplicated

**File:** `internal/lifecycle/lifecycle.go:547-553`, `internal/supervisor/supervisor.go:1265-1271`
**Issue:** Two identical helpers exist in two packages. `instance` already owns `LANHost`, so the pointer form belongs there as well.
**Fix:** Add `instance.LANHostPtr()` or a shared helper and delete both copies.

### IN-05: Broker drops the final `lagged` marker when the stream ends

**File:** `internal/events/events.go:100-117`, `internal/events/events.go:176-188`
**Issue:** A subscriber that overflowed gets its `lagged` marker only when space for two events appears (`need = 2`). If the instance stops before another event is offered, `Broker.Close` closes the channel with `s.dropped > 0` still pending. The watcher then sees `stopped` without ever learning that events were lost. This matters for consumers that treat "no `lagged` line" as "complete".
**Fix:** In `Close`, if `s.dropped > 0` and the channel has room, deliver the marker before `close(s.ch)`. If it has none, reserve one slot for it.

### IN-06: Test-support Python has dead state and a broad exception swallow

**File:** `tests/silbench.py:100`, `tests/silbench.py:113`, `tests/silbench.py:228-232`
**Issue:**
- `Bench._started` is added to in `up()` and never read.
- `teardown()` catches `BaseException` around `down()`. That also swallows `KeyboardInterrupt` and `SystemExit`, so Ctrl-C during teardown is ignored and the remaining `down` calls still run, delaying the exit.
- `Api._info` is first assigned in `_connect()` rather than `__init__`.
**Fix:** Remove `_started`, use `except Exception`, and initialise `self._info = None` in `__init__`.

### IN-07: `sim_light.h` uses `std::fill` without including `<algorithm>`

**File:** `components/sim_bus/sim_light.h:40`
**Issue:** `clear_effect_data()` calls `std::fill`, but the header only includes `<string>` and `<vector>`. It compiles today through ESPHome's transitive includes, which is a build-time accident that a future ESPHome bump can break. The `.cpp` includes `<algorithm>`, but the header must stand on its own.
**Fix:** Add `#include <algorithm>` to `sim_light.h`.

---

_Reviewed: 2026-11-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
