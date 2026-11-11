---
phase: 04-fan-model-v2
reviewed: 2026-11-11T00:00:00Z
depth: standard
files_reviewed: 69
files_reviewed_list:
  - e2e/fans_test.go
  - e2e/harness_test.go
  - e2e/library_v2_test.go
  - e2e/loop_test.go
  - e2e/model_test.go
  - e2e/model_v2_test.go
  - e2e/seed_test.go
  - e2e/web_test.go
  - fans/arctic-p12-pwm-pst.toml
  - fans/noctua-nf-a12x25-pwm.toml
  - internal/cli/cli.go
  - internal/cli/cli_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/engine/curve_test.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/engine/main_test.go
  - internal/engine/startstop_test.go
  - internal/engine/surfaces_test.go
  - internal/engine/tach_test.go
  - internal/events/events.go
  - internal/fanlib/fanlib.go
  - internal/fanlib/fanlib_test.go
  - internal/fanlib/fixtures_test.go
  - internal/fanlib/v1.go
  - internal/fanlib/v2.go
  - internal/fanlib/v2_test.go
  - internal/instance/instance.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/lint/wallclock_test.go
  - internal/model/curve.go
  - internal/model/curve_test.go
  - internal/model/derive.go
  - internal/model/derive_test.go
  - internal/model/fan.go
  - internal/model/fan_v010_test.go
  - internal/model/noise.go
  - internal/model/plant.go
  - internal/model/s7_test.go
  - internal/model/startstop_test.go
  - internal/model/tach.go
  - internal/model/tach_test.go
  - internal/supervisor/main_test.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - internal/supervisor/surfaces_test.go
  - README.md
  - testdata/fans-v0.1.0/arctic-p12-pwm-pst.toml
  - testdata/fans-v0.1.0/noctua-nf-a12x25-pwm.toml
  - testdata/fans-v2/curve-test.toml
  - testdata/fans-v2/load-test.toml
  - testdata/fans-v2/s7-steady.toml
  - testdata/fans-v2/startstop-test.toml
  - tests/silbench.py
  - tests/test_tach_steps.py
  - uitest/bench_test.go
  - uitest/layout_test.go
  - uitest/live_test.go
  - uitest/parity_test.go
  - uitest/plot_test.go
  - ui/web/app.css
  - ui/web/index.html
  - ui/web/js/app.js
  - ui/web/js/plot.js
  - ui/web/js/ring.js
  - ui/web/js/store.js
  - ui/web/js/view.js
findings:
  critical: 0
  warning: 3
  info: 3
  total: 6
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-11-11
**Depth:** standard
**Files Reviewed:** 69
**Status:** issues_found

## Summary

The core of the phase holds up under adversarial reading. I traced the schema 2 loader (`internal/fanlib/v2.go`), the model (`internal/model/{fan,curve,plant,tach,noise,derive}.go`), the engine and supervisor plumbing, the CLI, and the UI changes.

- **v0.1.0 bit-identity:** I compared the old `Step` with the new `advance`/`segment`/`Curve.At`. The legacy linear target, the hold-fan extra point `(0, MinRPM)`, the tau selection and the 0.5 rpm snap all reduce to the same floating point operations when the schema 2 fields are zero. The ideal-tach path calls `advance` exactly once per `Step`, so the shaft is unchanged.
- **Bus protocol and stdout:** the sim-bus protocol is unchanged. The `sil fans` summary goes to stderr, and `--quiet` discards it.
- **Determinism:** the noise is a stateless keyed hash. The lint bans the global `math/rand` source under `internal/model`, tests included.
- **JSON encoding:** a nil `*float64` inside `Event.V` encodes as `"v":null` and a `Load` with two nils encodes as `{"airflow_m3h":null,"power_w":null}`. I checked both by running them under Go 1.27.3.
- **Test suites:** `go vet ./internal/...` is clean. The model, fanlib, control and lint suites pass.
- **UI:** it renders only through `textContent`, so there is no injection path, and there is no CDN reference.

I found no critical issues. The three warnings are validation and robustness gaps in the fan file loader and its human-facing output. I reproduced each one with a throwaway probe test in a scratch copy of the repo. No source file was modified.

## Warnings

### WR-01: D-10 "never stops" rule is enforced on `curve.up` only; a `curve.down` can drop a never-stopping fan to 0 rpm

**File:** `internal/fanlib/v2.go:283-288` (rule), `internal/model/fan.go:236-241` (consumer)

**Issue:** When `stop.duty = 0`, rule (e) requires the first `curve.up` point to be at duty 0 with rpm > 0. Nothing checks `curve.down`.

I loaded a file with `stop.duty = 0`, `curve.up = [(0,450),(1,2000)]` and `curve.down = [(0.5,0),(1,2000)]`. It loads without error, with `BelowMin = "hold"` and `MinRPM = 0`.

At runtime the fan is `Running = true` (hold). Once the duty falls, the plant takes `target = downCurve().At(duty)`, which is 0 for any duty up to 0.5. The rpm decays to 0 and the 0.5 rpm snap fires. The result is a fan that `sil status` reports as `"running"` at 0 rpm.

That contradicts the documented meaning of `stop.duty = 0` ("the fan never stops", with its floor rpm as a curve point at duty 0).

`sil fans` also reports `min_rpm: 0` for a never-stops fan. The page does not rely on it, but it is wrong.

**Fix:** apply the floor rule to the down curve when one is present.

```go
// (e) D-10 for both directions.
if *stopDuty == 0 {
    for _, c := range []struct{ name string; c model.Curve }{{"curve.up", upCurve}, {"curve.down", downCurve}} {
        if len(c.c) == 0 {
            continue // no down curve: the up curve is used
        }
        if !(c.c[0].Duty == 0 && c.c[0].RPM > 0) {
            return model.Params{}, fail(c.name+".duty",
                "stop.duty = 0 (the fan never stops) needs the first %s point at duty 0 with rpm > 0 (the floor rpm); got duty %g rpm %g",
                c.name, c.c[0].Duty, c.c[0].RPM)
        }
    }
}
```

Add a v2 test for the down-curve case.

### WR-02: Control characters in `name`, `[sources]` and `[measured]` strings are accepted and printed raw to the terminal

**File:** `internal/fanlib/v2.go:239-243, 353-368, 377-387`; `internal/cli/cli.go:469-494` (`writeFansSummary`)

**Issue:** the loader checks length, emptiness and UTF-8 validity (go-toml does that), but not control characters. A TOML string such as `"a\u001b[2J..."` or `"\u001b]0;title\u0007"` loads fine. I confirmed that `name` and a source text keep their raw ESC and BEL bytes in `model.Params`.

`sil fans` then writes `f.Name` and the source text with `%s`/string concatenation to stderr. On a terminal that is an escape-sequence injection point.

The fan library can come from `SIL_FAN_DIR`, and the README presents fan files as something agents and humans edit, so a file from a PR or an agent could carry it. The JSON output is escaped and the UI uses `textContent`, so only the terminal summary is exposed. The impact is low, but the loader is advertised as strict and capped (T-04-06).

**Fix:** reject control characters at load, for example `strings.ContainsFunc(s, unicode.IsControl)`, for `name`, every `[sources]` text and every `[measured]` string. Alternatively, print those fields with `%q` in `writeFansSummary`. Doing both is cheapest.

### WR-03: `locate` loses the line number when a string value holds an escaped quote followed by a bracket

**File:** `internal/fanlib/v2.go:568-590` (`bracketDelta`), `v2.go:624`

**Issue:** `bracketDelta` toggles `quote` on any `"` or `'`, ignoring a backslash escape inside a basic string. For the assignment `name = "a \" [ b"` it counts the `[` as an opening bracket. `locate` then sets `depth = 1` and treats every following line as the inside of a multi-line array.

I confirmed the effect. With `name = "a \" [ b"` and a later `duty = 5`, the error is `f: stop.duty (5) must be in [0, 1)`: no `:line`, because `locate` returned 0. `locate("...", "stop", "duty")` returns 0 in that file.

Every located error after such a line loses its position. That breaks the D-14 guarantee that an error names its location. Triple-quoted multi-line strings are not tracked either, so their lines can be mistaken for assignments or headers.

**Fix:** make `bracketDelta` skip the character after a backslash while inside a basic (`"`) string, and do not treat a backslash as an escape inside a literal (`'`) string.

```go
case quote == '"' && c == '\\':
    skip = true // the next rune is escaped
```

Add a fixture with an escaped quote and a bracket, and one with a `"""` string, to the locate tests.

## Info

### IN-01: Stale comment on `engine.Config.Seed`

**File:** `internal/engine/engine.go:132-134`

**Issue:** the comment says the seed "has no effect until the tach noise of plan 04-04; the supervisor sets it from spec.json (plan 04-05)". Both plans are done. The seed now keys every noise bucket (`model.NewPlant` in `New` and in `handleApply`).

**Fix:** reword it to say that Seed keys the tach noise of every port's plant, and that a replug or an engine restart rebuilds the plant with the same seed. The noise stream therefore restarts at bucket 0, because buckets are counted in plant time.

### IN-02: `Plant.Params()` hands out slices that alias the plant's own parameters

**File:** `internal/model/plant.go:127-128`; `internal/model/fan.go:19-51`

**Issue:** the comment on `Params` says it is not comparable because it holds slices and a map. `Params()` returns a shallow copy: `Curve.Up`, `Curve.Down`, `Provenance`, `Sources` and `Datasheet` are shared with the plant. The engine goroutine reads `Curve` on every tick. A caller that mutates the returned value would change the running model without any synchronisation.

No caller does this today, so there is no bug. The immutability is held up only by convention.

**Fix:** document "read-only" on `Params()`, or deep-copy the curves in `NewPlant`.

### IN-03: A schema 2 file with the `schema` key missing gets a schema 1 "unknown field" error

**File:** `internal/fanlib/fanlib.go:139-141`

**Issue:** a file without `schema` is read as schema 1, which preserves the v0.1.0 behaviour. If someone writes a schema 2 body and forgets `schema = 2`, they get `unknown field stop` (or `curve`) from the strict v1 decoder. The error does not mention the schema.

**Fix:** keep the v0.1.0 semantics but improve the message. When the v1 strict decode fails on an unknown field whose first path element is a schema 2 table (`stop`, `start`, `dynamics`, `curve`, `tach`, `datasheet`, `measured`, `provenance`, `sources`), append: `(a schema 2 file needs schema = 2 at the top)`.

---

_Reviewed: 2026-11-11_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
