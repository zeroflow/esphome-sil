# Phase 4: Fan Model v2 - Pattern Map

**Mapped:** 2026-11-10
**Files analyzed:** 24 (new and modified)
**Analogs found:** 22 / 24

All analog paths below were checked with `git ls-files` (tracked source). No `.gsd/` mirror paths are used.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/model/fan.go` (MOD: Params fields, curve-driven Step) | model | transform | itself (v0.1.0 `Step`) | exact |
| `internal/model/curve.go` (NEW) | model | transform | `internal/model/fan.go` target maths (lines 70-76) | role-match |
| `internal/model/plant.go` (NEW) | model | transform (stateful wrapper) | `internal/model/fan.go` `State`/`Initial`/`Step` | role-match |
| `internal/model/tach.go` (NEW) | model | streaming (accumulator, frame drain) | `fan.go` exact-exponential step + engine `sendRPM` | partial |
| `internal/model/noise.go` (NEW) | utility | transform (keyed hash) | none in repo (RESEARCH Code Example 2) | none |
| `internal/model/derive.go` (NEW) | utility | transform | `fan.go` `linear` maths | partial |
| `internal/model/fan_v010_test.go` (NEW, frozen reference) | test | differential | `internal/model/fan_test.go` | exact |
| `internal/model/plant_test.go`, `tach_test.go`, `noise_test.go` (NEW) | test | transform | `internal/model/fan_test.go` | exact |
| `internal/fanlib/fanlib.go` (MOD: probe, dispatch) | loader | file-I/O | itself | exact |
| `internal/fanlib/v1.go` (NEW, moved `rawFan`/`validate`) | loader | file-I/O | `fanlib.go:59-70, 183-252` | exact |
| `internal/fanlib/v2.go`, `provenance.go` (NEW) | loader | file-I/O | `fanlib.go` `validate` + `decodeError` | role-match |
| `internal/fanlib/fanlib_test.go` (MOD) | test | file-I/O | itself | exact |
| `fans/*.toml` (MOD to schema 2) and `testdata/fans-v0.1.0/*.toml` (NEW, byte copies) | config | n/a | `fans/arctic-p12-pwm-pst.toml` | exact |
| `internal/engine/engine.go` (MOD: `plant.Step`, `Frame`, seed) | service | event-driven | itself, lines 266, 294, 554, 793, 1007-1018 | exact |
| `internal/control/control.go` (MOD: PortStatus, FanInfo, SimStatus) | model | request-response | itself, `FanInfo` lines 229-235 | exact |
| `internal/supervisor/supervisor.go` (MOD: `Fans`, status mapping, seed) | service | request-response | itself, line ~1269 | exact |
| `internal/events/events.go` (MOD: `Sample.Tach`, kinds `tach`/`load`) | model | pub-sub | itself, lines 24, 37-41 | exact |
| `internal/cli/cli.go` (MOD: `FansCmd --verbose`, `UpCmd --seed`, watch kinds help) | controller (CLI) | request-response | `UpCmd` / `FansCmd` lines 66-77, 128-131 | exact |
| `internal/instance/instance.go` (MOD: `Spec.Seed`) | config | file-I/O | existing `Spec` fields | exact |
| `ui/web/js/store.js`, `plot.js`, `app.js`, `view.js` (MOD) | component | event-driven | `store.js:37-40`, `plot.js` stepped series | exact |
| `internal/lint/wallclock_test.go` (MOD: ban `math/rand` in model) | test | static check | itself | exact |
| `e2e/*`, `uitest/*`, `tests/silbench.py` (MOD: pin `SIL_FAN_DIR`) | test | n/a | `TestShippedArcticFan` pattern (`t.Setenv("SIL_FAN_DIR", "")`) | exact |

## Pattern Assignments

### `internal/model/fan.go` (model, transform) - extend, do not replace

**Analog:** itself (`internal/model/fan.go`, 96 lines).

**Params zero-value contract** (lines 10-19): keep all seven legacy fields; new optional fields (`Curve`, `StartDelay`, `CoastTau`, `Tach`, `Datasheet`, `Schema`, `Provenance`, `Sources`) must have zero value == v0.1.0 behaviour. Metadata sits on `Params` so the supervisor's `FanLibrary` interface stays unchanged.

**Step skeleton to preserve** (lines 39-95): pure function, `dt <= 0` returns `s`, NaN duty to 0, clamp [0,1]:
```go
switch {
case math.IsNaN(duty): duty = 0
case duty < 0: duty = 0
case duty > 1: duty = 1
}
running := s.Running
switch {
case !running && duty >= p.StartDuty: running = true
case running && duty < p.MinDuty:     running = p.BelowMin == "hold"
}
```
Keep `>=` start and `<` stop comparison operators.

**Exact-exponential update and snap** (lines 79-93), reuse unchanged:
```go
rpm += (target - rpm) * (1 - math.Exp(-dt.Seconds()/tau))
...
if target == 0 && rpm < 0.5 { rpm = 0 }
```
**Bit-equal interpolation** (line 75): `p.MinRPM + (duty-p.MinDuty)/(1-p.MinDuty)*(p.MaxRPM-p.MinRPM)`. `Curve.At` must use `y0 + (x-x0)/(x1-x0)*(y1-y0)`.

**Doc-comment style:** semantics list in the `Step` comment (lines 27-38). Update it, keep the "no clock, no I/O" package doc.

### `internal/model/plant.go`, `tach.go`, `curve.go`, `noise.go`, `derive.go` (NEW)

**Analog:** `internal/model/fan.go` for package style (pure, `time.Duration` dt, no clock). `State` is a plain value struct returned by `Step`; `Plant` wraps `Params`, `State`, tach and noise and exposes `Step`, `Frame`, `Current`, `Shaft`, `Reading`, `Outputs`, `Running`. The engine calls only `Plant`.

For tach and noise use RESEARCH Code Examples 2 and 3 (splitmix64 keyed hash, bucket-split exact integral). There is no in-repo analog for the hash. Do not use `math/rand`.

### `internal/model/fan_v010_test.go` and new model tests (test)

**Analog:** `internal/model/fan_test.go`.

**Test conventions** (lines 1-45): literal fan params (not loaded from fanlib), small helpers:
```go
var arctic = Params{Name: "Arctic P12 PWM PST", MinDuty: 0.05, StartDuty: 0.10, BelowMin: "stop",
	MinRPM: 200, MaxRPM: 1800, TauUp: 1.0, TauDown: 2.5}
func relDiff(a, b float64) float64 { ... }
func settle(p Params, s State, duty float64, d time.Duration) State { /* 10 ms steps */ }
```
`fan_test.go` stays untouched (regression guard, includes dt-composability test). The frozen v0.1.0 `Step` lives in `_test.go`, copied verbatim including its degenerate `else { rpm = target }` branch for tau <= 0, and is compared with `==` on `RPM` and `Running` over randomised duty and dt scripts. Include the corner `min_duty = 0, below_min = "stop"` (Pitfall 4).

### `internal/fanlib/fanlib.go` (+ `v1.go`, `v2.go`, `provenance.go`) (loader, file-I/O)

**Analog:** `internal/fanlib/fanlib.go`.

**Located error type** (lines 52-69) reused as is: `Error{File, Line, Col, Key, Msg}`; `Error()` renders `file:line:col: msg`.

**Strict decode** (lines 118-123):
```go
var raw rawFan
dec := toml.NewDecoder(bytes.NewReader(data))
dec.DisallowUnknownFields()
if err := dec.Decode(&raw); err != nil { return model.Params{}, decodeError(file, err) }
return validate(file, data, raw)
```
Schema 2: first decode `struct{ Schema *int }` leniently (RESEARCH Code Example 4), then dispatch. Absent `schema` goes to the moved v1 path. Add `Schema *int` to `rawFan` so an explicit `schema = 1` is accepted.

**decodeError** (lines 138-157): handles `*toml.StrictMissingError` (`first.Position()`, `first.Key()`) and `*toml.DecodeError`. Reuse for both versions.

**Pointer-field presence and named missing-key errors** (lines 163-181) for required keys:
```go
if !p.ok { return model.Params{}, &Error{File: file, Key: p.key, Msg: "missing required key " + p.key} }
```
**Finite-number check** (lines 197-210) and ordered switch of range rules with `fail(key, fmt, ...)`. Reuse the `fail` closure shape. nan/inf parse in go-toml, so the check is mandatory in v2 too.

**Line locating:** `keyLine` (lines 242-252) is wrong for v2 (repeated `duty`). Write a table-tracking scanner for `table.key` and quoted dotted keys in `[provenance]`. Keep `keyLine` for v1.

**Constants kept:** `maxFileBytes = 64 << 10`, `idPattern`, `EnvFanDir`, `Source()`, `fileLabel`, `IDs()`.

### `fans/*.toml` (config)

**Analog:** `fans/arctic-p12-pwm-pst.toml` (v1 layout, 9 keys, `# ASSUMED` comments). Rewrite as schema 2 using RESEARCH Code Example 5 (`[sources]`, `[stop]`, `[start]`, `[dynamics]`, `[curve.up]`, `[curve.down]`, `[tach]`, `[datasheet]`, `[provenance]`). Copy the current v1 files byte for byte to `testdata/fans-v0.1.0/` first (same ids). Vendor values: 200-1800 rpm, 95.70 m3/h, 0.1 A at 12 V from the Arctic page. The page states no watts; per the D-04 amendment of 2026-11-10, power_w = 1.2 (0.1 A × 12 V) is `datasheet:arctic_page` with the derivation in a comment. Noctua values from the official datasheet PDF are `datasheet` wherever it is hosted. Shipped `tach.noise_rpm` is 20 (D-03 amendment; std of a 1 s reading). Embedding is `fans/embed.go` (`//go:embed *.toml`), unchanged.

### `internal/engine/engine.go` (service, event-driven)

**Analog:** itself. Integration points:
- line 266 `state model.State` becomes `plant *model.Plant`; lines 294 and 793 `model.Initial(pc.Fan)` become `model.NewPlant(params, port, seed)` (on start and fan swap); `Config` gains `Seed`.
- line 554 tick loop:
```go
p.state = model.Step(p.cfg.Fan, p.state, p.duty, dt)
e.emitState(p)
```
becomes `p.plant.Step(p.duty, dt)`.
- lines 561-563 frame cadence (`rpmFrameStep = 50 * time.Millisecond`, line 31, must stay 50 ms):
```go
if e.connected && e.simNow >= e.nextRPM { e.nextRPM = e.simNow + rpmFrameStep; e.sendRPM(false) }
```
- `sendRPM` (lines 1007-1018): change-only send of `float32(p.state.RPM)`; switch the value to `plant.Frame()` (drains pulses; ideal tach returns shaft rpm so v0.1.0 frames are identical). `inSnapshot` (line 685) must use `plant.Current()` (no drain).
- `emitRPM`/`emitSample`/`emitState`: add `tach` and `load` events and `Sample.Tach` alongside the existing ones.

### `internal/control/control.go`, `internal/supervisor/supervisor.go` (request-response)

**Analog:** `control.FanInfo` (lines 229-235) and its construction (supervisor.go ~1269):
```go
res.Fans = append(res.Fans, control.FanInfo{ID: id, Name: p.Name, MinRPM: p.MinRPM, MaxRPM: p.MaxRPM, BelowMin: p.BelowMin})
```
Duplicated in `uitest/bench_test.go:396`. Add one `control.NewFanInfo(id, model.Params)` and use it in both. New fields are additive with JSON tags (`schema`, `provenance`, `values`; `tach_rpm`, `airflow_m3h`, `power_w` as `*float64` always present, null when unknown). `SchemaVersion` stays 1 (control.go:33). Error pattern for load failures:
```go
return control.FansResult{}, apperr.Failed("fan_toml_invalid", err.Error(), map[string]any{"fan": id, "error": err.Error()})
```
No new apperr codes needed for schema 2 load errors; the seed range error uses the already registered `invalid_argument` (see the `apperr.Registry` map in `internal/apperr/codes.go`).

### `internal/events/events.go` (pub-sub)

**Analog:** itself. `Kinds` (line 24) gets `tach` and `load`; `Sample` (lines 37-41) gets `Tach *float64 \`json:"tach"\`` with the same nil-while-bus-down rule as Duty/RPM:
```go
type Sample struct {
	Port int      `json:"port"`
	Duty *float64 `json:"duty"`
	RPM  *float64 `json:"rpm"`
}
```
Update the `Event` doc comment value list.

### `internal/cli/cli.go` (CLI)

**Analog:** `UpCmd` (lines 66-77) and `FansCmd` (lines 128-131), kong struct tags:
```go
Fan string `help:"Library fan plugged into all four ports ..."`
type FansCmd struct { Instance string `name:"instance" short:"i" help:"..."` }
```
Add `Seed` to `UpCmd` (optional, `help:`), `Verbose bool` to `FansCmd`. Add `tach, load` to the `WatchCmd.Kind` help (line 142). Output must keep exactly one top-level key (e2e `fans_lists_the_library`), so summary and verbose data live inside each fan entry.

### `ui/web/js/store.js`, `plot.js`, `app.js` (component, event-driven)

**Analog:** `store.js:37-40`:
```js
case "duty": case "rpm": case "state": case "fan": {
  const p = portAt(frame.port);
  if (p) { p[frame.k] = frame.v; }
  return none;
}
```
Add `tach` and `load` cases (load sets `airflow_m3h` and `power_w`; reset both to null on `fan`). `plot.js` already uses `uPlot.paths.stepped({ align: 1 })` for the duty series: copy for the dimmer tach series. Clamp the plotted tach to the fixed axis (Pitfall 7). `app.js` `KINDS` string and `uitest/parity_test.go::sessionSteps` need rows for both kinds. Page shows an em dash for null airflow/power.

### Test pinning (e2e, uitest, Python)

**Analog:** `TestShippedArcticFan` uses `t.Setenv("SIL_FAN_DIR", "")` to see the embedded library. Exact-RPM tests point `SIL_FAN_DIR` at `testdata/fans-v0.1.0`. Must-fix items:
- `e2e/model_test.go:160-185` `TestFanTomlUnknownKey` hunts a `max_rpm` line in the shipped file; change the typo target to a v2 key (e.g. `noise_rpm` to `noise_rmp`).
- `e2e/fans_test.go`, `e2e/web_test.go`, `uitest/plot_test.go` (max 1800), engine tests using `fanlib.DefaultFan`.
- `tests/silbench.py:33` `_DROP_ENV = ("SIL_INSTANCE", "SIL_API_KEY", "SIL_FAN_DIR")` drops the pin; the Bench must set `SIL_FAN_DIR` for `sil up` itself.
- Contract test asserts spread/bias/levels plus the step signature of the amended roadmap criterion 3 (peak-to-peak >= 12 rpm, at least a quarter of the readings on each side of the mean), not exact multiples of 30 (RESEARCH Pitfall 1). The lattice half of the signature is gated hermetically by S7a.

### `internal/lint/wallclock_test.go`

**Analog:** itself (AST-only ban tables `bannedTime`, `bannedFuncs` keyed by import path). Add a ban on top-level `math/rand` calls under `internal/model`, using the same `setOf(...)` map shape.

## Plan file-name mapping (added at plan revision, 2026-11-10)

The plans use some file names that differ from the classification table above. Every new or modified file of a plan maps to an analog section here.

| Plan file (plan) | PATTERNS section / analog | Note |
|---|---|---|
| `internal/model/curve_test.go` (04-01), `startstop_test.go` (04-02), `derive_test.go` (04-06), `s7_test.go` (04-04) | "fan_v010_test.go and new model tests" → `internal/model/fan_test.go` | literal Params, settle helper, local `rand.New(rand.NewPCG(..))` only; s7_test.go also follows RESEARCH Code Example 6 |
| `internal/model/tach_test.go` (04-04) | same, replaces the `plant_test.go` and `noise_test.go` rows | the Plant, tach and noise tests (incl. TestNoiseMoments, TestNoiseAmplitudeIsGateStd) live in tach_test.go; Plant-vs-v0.1.0 tests live in fan_v010_test.go (04-02). No plant_test.go or noise_test.go is created |
| `internal/fanlib/v2.go` (04-01, 04-03) | "fanlib.go (+ v1.go, v2.go, provenance.go)" | the provenance rules (rule f) live in v2.go; no provenance.go is created |
| `internal/fanlib/v2_test.go` (04-01, 04-03), `fixtures_test.go` (04-02) | `internal/fanlib/fanlib_test.go` (`load`, `asError`, `base`/`with`, TestSilFanDirOverridesTheEmbeddedLibrary) | fanlib_test.go itself is rewritten for the shipped fans in 04-08 |
| `internal/engine/curve_test.go` (04-01), `startstop_test.go` (04-02), `tach_test.go` (04-04), `surfaces_test.go` (04-06), `main_test.go` (04-08) | "engine.go" → `internal/engine/engine_test.go` harness (newHarness, setDuty, advanceTo, replug, fanConfig) | main_test.go applies the TestShippedArcticFan SIL_FAN_DIR pattern in a TestMain |
| `internal/supervisor/surfaces_test.go` (04-06), `supervisor_test.go` (04-05), `main_test.go` (04-08) | "control.go, supervisor.go" → `internal/supervisor/watch_test.go` (watchSup, startWatch), fanSupervisor, policy_test.go SimRestart tests | |
| `internal/lifecycle/lifecycle.go`, `lifecycle_test.go` (04-05) | no row above; analog: its own pure pre-write checks `checkWebOptions`/`resolveFan` and TestCheckWebOptions | `lifecycle.DefaultSeed` lives here (the default of the `sil up` flag); internal/model has no default seed |
| `internal/instance/instance.go` (04-05) | "internal/instance/instance.go (MOD: Spec.Seed)" | also RunFile.Seed |
| `ui/web/js/ring.js`, `ui/web/index.html`, `ui/web/app.css` (04-07) | "store.js, plot.js, app.js" plus the files themselves (Ring push/trim, port template, fixed-width `.readout .value`) | |
| `uitest/plot_test.go`, `parity_test.go`, `layout_test.go`, `bench_test.go` (04-07) | "Test pinning" (uitest runs on the shipped library) and the files themselves | bench_test.go switches to `control.NewFanInfo` |
| `testdata/fans-v2/*.toml` (04-01, 04-02, 04-06, 04-09) | "fans/*.toml" and RESEARCH Code Example 5 | test-fixture header comment, every value `assumed:test` |
| `e2e/model_v2_test.go` (04-01), `library_v2_test.go` (04-08), `seed_test.go` (04-05), `fans_test.go`/`web_test.go` (04-03, 04-09) | "Test pinning" → `e2e/model_test.go` (modelAPI, modelSetFan, TestFanTomlUnknownKey), `e2e/restart_test.go` (rsInstance.restart), `e2e/web_test.go` TestWebPlot | |
| `tests/test_tach_steps.py` (04-09) | "Test pinning" → `tests/test_api_smoke.py` (api_main usage), `tests/silbench.py` | |

## Shared Patterns

### Zero value is v0.1.0
**Source:** `internal/model/fan.go` `Params`/`Step`. **Apply to:** all model and loader changes. New optional fields default to legacy behaviour; v1 loader output is a legacy-only `Params`.

### Strict, located load errors
**Source:** `internal/fanlib/fanlib.go` `Error`, `decodeError`, `fail` closure. **Apply to:** every v2 validation rule (provenance kinds, undefined source ids, missing or extra provenance entries, non-finite numbers, curve monotonicity).

### Additive status/event fields
**Source:** `control.go` (`SchemaVersion = 1`), `events.go`. **Apply to:** all new JSON fields (always present, `null` when unknown, never a made-up zero).

### Error codes through the apperr.Registry map
**Source:** `internal/apperr/codes.go`: `var Registry = map[string]int{...}`, a package variable mapping each code to its exit class (`fan_toml_invalid`: ExitFailed). It is not a type. Errors are built with `apperr.Failed(code, msg, detail)` / `apperr.Infra(...)` from `internal/apperr/apperr.go`, and `TestRegistryMatchesCallSites` checks every call site's literal code against the map. **Apply to:** any new load or seed error.

### No clock in model
**Source:** `internal/lint/wallclock_test.go`; engine owns cadence (`tick` 10 ms, `rpmFrameStep` 50 ms). **Apply to:** `internal/model/*`.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/model/noise.go` | utility | transform | No hash or PRNG in repo; use RESEARCH Code Example 2 |
| `internal/model/tach.go` (pulse accumulator and 1 s gate) | model | streaming | No accumulator exists; combine `fan.go` exact exponential with RESEARCH Pattern 4 and Code Example 3 |

## Metadata

**Analog search scope:** `internal/model`, `internal/fanlib`, `internal/engine`, `internal/control`, `internal/events`, `internal/supervisor`, `internal/cli`, `internal/lint`, `fans`, `ui/web/js`, `e2e`, `tests`
**Files read in full or by targeted range:** model/fan.go, model/fan_test.go (head), fanlib/fanlib.go, fans/arctic-p12-pwm-pst.toml, engine.go (ranges), events.go, control.go, supervisor.go, cli.go, store.js, wallclock_test.go, e2e/model_test.go
**Pattern extraction date:** 2026-11-10 (file-name mapping and corrections added at plan revision, 2026-11-10)
**Note:** RESEARCH.md was read to line 425 of 565 (vendor table and later sections not needed for pattern mapping).
