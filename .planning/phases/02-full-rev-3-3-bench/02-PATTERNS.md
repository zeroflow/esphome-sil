# Phase 2: Full Rev 3.3 Bench - Pattern Map

**Mapped:** 2026-11-09
**Files analyzed:** 30 new/modified
**Analogs found:** 28 / 30

All analog paths below were checked against `git ls-files` and are tracked. `prototype/` is tracked but frozen (gofmt excluded); it is a starting point to move from, not a place to edit. `examples/rev33.yaml` does not exist yet in git (RESEARCH built it in a scratch run); the planner must write it from the RESEARCH "Code Examples" section plus the `examples/fan-skeleton.yaml` header style.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `components/sim_bus/binary_sensor.py` (new) | ESPHome platform schema/codegen | event-driven (push level) | `prototype/components/sim_bus/binary_sensor.py` + `components/sim_bus/output.py` | exact |
| `components/sim_bus/light.py` (new) | ESPHome platform codegen | streaming (frames out) | `prototype/components/sim_bus/light.py` + `components/sim_bus/output.py` | exact |
| `components/sim_bus/sim_binary_sensor.h/.cpp` (new) | C++ child | event-driven | `components/sim_bus/sim_sensor.h/.cpp` | role-match |
| `components/sim_bus/sim_light.h/.cpp` (new) | C++ child | streaming, rate-capped | `components/sim_bus/sim_output.h/.cpp` (+ RESEARCH SimLight) | role-match |
| `components/sim_bus/sensor.py` (mod: `mode: latest`) | codegen | transform | itself | exact |
| `components/sim_bus/__init__.py` (mod: extend domains in `check_unique_channels`) | hub schema | config | itself | exact |
| `components/sim_bus/sim_bus.h/.cpp` (mod: `type()/pixels()/segments()`, `set_out_px`, `last_px_` replay) | hub | event-driven | itself (`set_out`, `last_out_`, `try_connect_`) | exact |
| `components/sim_bus/sim_protocol.h`, `protocol/PROTOCOL.md`, `protocol/vectors/frames.tsv` (mod: additive `px`, `n`, `seg`) | protocol | transform | themselves; `tests/cpp/test_protocol.cpp` | exact |
| `internal/bus/server.go`, `codec.go` (mod: `Channel{N, Seg}`) | bus server | event-driven | itself (`Channel` L36, chan case ~L296) | exact |
| `internal/engine/engine.go` (mod: World, optional channels, buttons, env) | engine | event-driven | itself (`checkManifest` L306, `handleFrame`, `sendRPM`) | exact |
| `internal/control/control.go` (mod: Status fields, Set/Press/Watch/Logs routes + client methods) | control API | request-response + streaming | itself (`NewHandler`, `reply`, `Client.do`) | exact |
| `internal/supervisor/supervisor.go` (mod: Set, Press, event broker, recorded inputs) | supervisor | request-response | `SimRestart` L677, `Status` L583 | exact |
| `internal/cli/cli.go` (mod: `set`, `press`, `watch`, `logs`) | CLI | request-response + streaming | `SimRestartCmd`/`StatusCmd`, `connect` | exact |
| `internal/apperr/codes.go` (mod: new codes) | registry | n/a | itself | exact |
| `internal/lifecycle/lifecycle.go` (mod: default config, fans per port) | lifecycle | CRUD | itself L30, L194 | exact |
| `internal/fwbuild/lint.go` (mod: hint text) | lint | transform | itself | exact |
| Go unit tests (`*_test.go` per package) | test | n/a | `internal/engine/engine_test.go`, `internal/cli/cli_test.go`, `internal/bus/server_test.go` | exact |
| `e2e/*_test.go` (new Phase 2 e2e) | test | e2e | `e2e/io_test.go`, `e2e/harness_test.go` | exact |
| `examples/rev33.yaml` (new) | firmware config | n/a | `examples/fan-skeleton.yaml` | role-match |
| `fans/*.toml` (optional second fan) | config | n/a | `fans/arctic-p12-pwm-pst.toml` | exact |
| `tests/pyproject.toml`, `uv.lock`, `conftest.py`, `test_contract.py`, `test_api_smoke.py` (new) | pytest | subprocess + aioesphomeapi | `../wifi-fancontroller/hil/pyproject.toml` (external, untracked here), `e2e/api_client.py` | partial |
| `Makefile` (mod: `test-contract`) | build | n/a | itself (`e2e`, `test-cpp`) | exact |
| `README.md` (HA steps) | docs | n/a | itself | exact |

## Pattern Assignments

### `components/sim_bus/binary_sensor.py` (codegen, push)
**Analog:** `prototype/components/sim_bus/binary_sensor.py` (verified compiling; move to `components/`). Copy whole file (18 lines):
```python
SimBinarySensor = sim_bus_ns.class_("SimBinarySensor", binary_sensor.BinarySensor, cg.Component)
CONFIG_SCHEMA = (
    binary_sensor.binary_sensor_schema(SimBinarySensor)
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)
async def to_code(config):
    var = await binary_sensor.new_binary_sensor(config)
    await cg.register_component(var, config)
    await register_child(var, config)
```
Imports match `components/sim_bus/sensor.py` L1-5 (`from . import CHILD_SCHEMA, register_child, sim_bus_ns`). Pitfall 1: the engine must send an initial level 0 `in` on connect (first state swallowed).

### `components/sim_bus/light.py`
**Analog:** `prototype/components/sim_bus/light.py` (shown in full above: `light.ADDRESSABLE_LIGHT_SCHEMA.extend({cv.GenerateID(CONF_OUTPUT_ID): cv.declare_id(SimLight), cv.Required(CONF_NUM_LEDS): ...})`, `to_code` = `new_Pvariable(config[CONF_OUTPUT_ID])`, `light.register_light`, `register_component`, `set_num_leds`, `register_child`). Add the `derive_segments` function and optional `segment_labels` from RESEARCH "light.py segment derivation" (D-06 resolved: derived from `partition` lights at codegen, `cg.add(var.set_segments(...))`). Segments must be derived at final-validate or to_code time via `CORE.config`.

### `components/sim_bus/sim_binary_sensor.h/.cpp`
**Analog:** `components/sim_bus/sim_sensor.h` L1-33 (guard `#ifdef USE_SENSOR` -> `USE_BINARY_SENSOR`; `class X : public base, public Component, public SimChild`; `direction()` returns `"in"`; `on_value(float v)` -> `publish_state(v > 0.5f)`; namespace `esphome::sim_bus`; `static const char *const TAG`; `dump_config` using `LOG_BINARY_SENSOR`). Keep `on_disconnected()` leaving state unchanged (Pitfall 10).

### `components/sim_bus/sim_light.h/.cpp`
**Analog:** `components/sim_bus/sim_output.h/.cpp` for structure (`direction() == "out"`, `if (this->bus_ != nullptr) this->bus_->set_out(...)`) and the full SimLight listing in RESEARCH Code Examples (dedupe by hex string, `kMinGapMs = 50`, `loop()` flush, `on_connected` pending). Guard with `#ifdef USE_LIGHT`.

### `components/sim_bus/sim_bus.h/.cpp` (hub)
Extend the existing `SimChild` (virtuals `direction/on_value/on_connected/on_disconnected`, sim_bus.h L16-33) with `type()` default `"f32"`, `pixels()`, `segments()`. Mirror `set_out` + `last_out_` (sim_bus.h L55-58, L73-76) for `set_out_px` + `last_px_`; replay order is outs, then px, then sync. Keep the header platform-free (comment L2-4).

### `components/sim_bus/sensor.py` (mod)
Add `cv.Optional("mode", default="window_mean"): cv.one_of("window_mean","latest", lower=True)` on the schema; `cg.add(var.set_publish_latest(...))` in `to_code`. Current file uses `.extend(cv.polling_component_schema("1s"))` and `sensor.new_sensor`; keep that for RPM. Pitfall 2: temperature/humidity use `mode: latest`.

### `components/sim_bus/__init__.py` (mod)
`check_unique_channels` iterates `for domain in ("output", "sensor"):` (L~63); extend to `("output","sensor","binary_sensor","light")`. For lights the id lives under `CONF_OUTPUT_ID` rather than `CONF_ID`, so adapt the ident lookup (`entry.get(CONF_ID)` returns None for lights; fall back to `CONF_OUTPUT_ID`). `CHANNEL_RE` already admits `temperature`, `usr1`, `leds`.

### `internal/bus/server.go` / `codec.go`
`type Channel struct{ Ch, Dir, Type string }` (server.go L36); the `chan` case (~L296) reads `ch`, `dir`, `type` via `f.GetStr`. Add `N int64`, `Seg string` via `f.GetInt("n")` / `GetStr("seg")`, same style as `hello.Chans, _ = f.GetInt("chans")`. Keep `Proto = 1` (additive). Frame builders use `bus.NewFrame("in").Str("ch", ch).Num("v", v).Int("t", ...)`; add `Str("px", hex)` frames the same way. Add vectors to `protocol/vectors/frames.tsv` (shared by Go test and `tests/cpp/test_protocol.cpp` via `make test-cpp`).

### `internal/engine/engine.go` (World, buttons, env)
**Analog:** itself. Excerpts to follow:
```go
// checkManifest: ports required, everything else warning "unknown channel %s (ignored)" (L332, L353)
for _, w := range want {
    c, ok := have[w.ch]
    switch {
    case !ok: e.warn(fmt.Sprintf("missing channel %s (%s)", w.ch, w.dir))
    case c.Dir != w.dir: e.warn(...)
    case c.Type != "f32": e.warn(...)
    }
}
// handleFrame: only Kind()=="out"; ch via f.GetStr("ch"), v via f.GetNum("v"); clamp
// sendRPM(force): send only on float32 change, e.send(bus.NewFrame("in").Str("ch", ch).Num("v", v).Int("t", e.micros()))
```
Change per RESEARCH Pattern 1: optional channels (`temperature`, `humidity`, `usr1..3`, `leds` type `rgb`) enable status fields; add `leds` out-frame handling (store hex, emit event); on connect/resync send initial 0 for each button and env values (re-send ~10 s per D-11). Time via `e.micros()` / injected clock only (see `internal/lint/wallclock_test.go`). Test style: `internal/engine/engine_test.go` (Manual clock, `guard = 5 * time.Second`).

### `internal/control/control.go` (Status + routes)
Status is extend-only: `Fields are extended by later phases, never renamed.` Add `Env`, `Buttons`, `LEDs{Strip []string, Segments map[string][]int}` with `omitempty` where optional (L41-51); `PortStatus` L89-95 already has `Fan`. New route pattern, copy from `sim/restart` (L~157-180): `io.LimitReader(r.Body, 1<<16)`, `json.NewDecoder` + `DisallowUnknownFields`, `req.Validate()`, `reply(w, res, err)`. Add `POST /api/v1/set`, `POST /api/v1/press`, `GET /api/v1/watch` (streaming skeleton in RESEARCH, needs `http.Flusher`, `application/x-ndjson`), `GET /api/v1/logs`. Client side: copy `Client.SimRestart` / `Client.do` (L164-196); streaming needs its own request path (Pitfall 8, not `do`). Extend the `Control` interface (L~100) and its mocks.

### `internal/supervisor/supervisor.go`
**Analog:** `SimRestart` (L677-757): takes `s.opMu`, checks `s.isStopping()` -> `errStopping()`, `withDown(ctx)`. For `Set`/`Press`: validate first (`apperr.Failed("invalid_argument"/new codes, msg, detail map)`), apply as one engine op, then record inputs under `s.mu` to seed the next engine on `startEngine` (L265) (Pitfall 10, Discretion item). `Status` (L583-626) shows the snapshot pattern: copy under `s.mu`, build `control.Status`, loop `snap.Ports`; extend with env/buttons/LEDs.

### `internal/cli/cli.go`
**Analog:** `SimRestartCmd` + `Run` (L~83-90, L~270-282) and `StatusCmd.Run`. Grammar struct style:
```go
type SimRestartCmd struct {
    Name string        `arg:"" optional:"" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
    Gap  time.Duration `help:"..."`
}
func (c *SimRestartCmd) Run(env *runEnv) error {
    _, client, err := connect(c.Name)
    if err != nil { return err }
    st, err := client.SimRestart(env.context, control.SimRestartRequest{...})
    if err != nil { return err }
    env.result = st
    return nil
}
```
Register new commands as fields on `CLI` (`Set SetCmd \`cmd:"" ...\``). Pitfall 7: positional instance name vs variadic `key=value` args, resolve ordering in `resolve.go`. Streaming commands (`watch`, `logs -f`) set `env.silent = true` (as `ServeCmd.Run`) and write NDJSON lines directly to stdout (`writeJSON` per line); `--long/--hold/--down/--up` flags on `PressCmd`. Errors: return `apperr.Failed(...)`; `fail()` prints the single stdout JSON error and exit 1/2. Test style: `internal/cli/cli_test.go` via `Execute(args, stdout, stderr)`.

### `internal/apperr/codes.go`
Every new code must be registered in `Registry` with its class (Exit 1 block alphabetical, Exit 2 block), or `TestRegistryMatchesCallSites` fails (Pitfall 6). Likely new Exit 1: `unknown_key`, `value_out_of_range`, `unknown_fan`, `button_held`, `button_not_held`, `input_not_available`; keep literal code strings at call sites (non-literal fails the scan).

### `internal/lifecycle/lifecycle.go`
`defaultConfig = "examples/fan-skeleton.yaml"` (L30) and `Fans: []string{fan, fan, fan, fan}` (L194). Switch the default only in the last plan behind `make e2e` (Pitfall 14); update `--config` help in `UpCmd`.

### `examples/rev33.yaml`
**Analog:** `examples/fan-skeleton.yaml` header (substitutions `sil_name/sil_mac/sil_port`, `esphome: name: sil-${sil_name}`, `build_flags: [-static]`, `host: mac_address`, `api: port, reboot_timeout: 0s, encryption.key: !env_var SIL_API_KEY`, `external_components: type: local, path: ../components, components: [sim_bus]`, `sim_bus: id: bus`, `output:` x4 platform sim_bus with `channel: portN.duty`, `fan: platform: speed ... speed_count: 100, restore_mode: ALWAYS_OFF`). Never hand-write identity. Add `sensor` rpm (`mode` default), Temperature/Humidity `mode: latest`, `binary_sensor` Button USRn, `light` sim_bus + `partition` lights (`internal: true`), `on_client_connected` with `client_info != "sil"` (Pitfall 3). Full verified file: RESEARCH "examples/rev33.yaml". Must pass the banned-component lint (`internal/fwbuild/lint.go`).

### `e2e/*_test.go` (Phase 2)
**Analog:** `e2e/harness_test.go` helpers: `newEnv(t)`, `e.Up(t, name, extra...)` (picks free port, registers `sil down` cleanup), `e.Sil(t, args...)` (exactly one JSON value on stdout; returns `out, exit, stderr`), `e.Status`, `e.WaitStatus(t, name, timeout, pred)` (200 ms poll via `sysClock`), `e.APIClient`. Build tag `//go:build sil_e2e`, package `e2e`. `e2e/io_test.go`'s `ioPeer` (fake simulator, keeps frames with connection numbers) is the analog for protocol-level tests of the light frame rate and dedupe (Pitfall 4).

### `tests/` pytest project (TEST-01/02)
No in-repo Python test analog (partial). Use `e2e/api_client.py` for the aioesphomeapi connect/Noise-key usage and `../wifi-fancontroller/hil/pyproject.toml` (uv, aioesphomeapi) for the project file, plus RESEARCH Pattern 8 and the TEST-01 sketch. Instance isolation: temp dir `/tmp/sil-ct-*` as `XDG_RUNTIME_DIR`/`XDG_STATE_HOME`, unique instance `ct-<hex>`, `sil up --quiet`, `sil down` in finalizer; polling with deadlines.

### `Makefile`
Copy target style from `e2e:` / `test-cpp:` (`.PHONY` list at top, `PATH` export, `$(GO)`). Add `test-contract: build` then `uv run --project tests --frozen pytest tests`.

## Shared Patterns

### Stdout/stderr and exit codes (CLI-05)
**Source:** `internal/cli/cli.go` `Execute`, `writeJSON`, `fail`. **Apply to:** every new command. One JSON value on stdout (marshal first); progress/errors on stderr; `apperr.Failed` -> exit 1, `apperr.Infra` -> exit 2.

### Error construction and HTTP mapping
**Source:** `internal/control/control.go` `reply` (ExitFailed -> 400, else 500), `apperr.Failed("invalid_argument", msg, map[string]any{...})`. **Apply to:** all new control handlers and validators.

### Additive status schema
**Source:** `control.Status` comment "extended, never renamed". **Apply to:** all status/watch snapshot work; the first `watch` line reuses the Status shape.

### Injected clock, no wall clock
**Source:** `internal/clock`, `internal/lint/wallclock_test.go`; engine `e.micros()`, supervisor `s.clk.Sleep`. **Apply to:** hold timing in `press` (200 ms / 1500 ms sim time), 10 s env resend, watch timestamps `t`.

### Channel naming and uniqueness
**Source:** `components/sim_bus/__init__.py` `CHANNEL_RE`, `check_unique_channels`. **Apply to:** all new platforms; keep one name set (`temperature`, `humidity`, `usr1..3`, `leds`) in Go constants, YAML, vectors and docs.

### Protocol conformance
**Source:** `protocol/vectors/frames.tsv` read by Go tests and `tests/cpp/test_protocol.cpp` (`make test-cpp`). **Apply to:** every new frame/field.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| Streaming `GET /api/v1/watch` handler and client | control API | streaming/pub-sub | No streaming endpoint exists; use RESEARCH skeleton (subscribe before snapshot, seq dedupe) |
| Event broker in supervisor/engine | pub-sub | event-driven | No fan-out exists yet; design from RESEARCH Pattern 5 |
| `tests/*.py` pytest project | test | subprocess | No Python pytest in this repo; use the external HIL project and `e2e/api_client.py` |

## Metadata

**Analog search scope:** `components/`, `internal/`, `cmd/`, `e2e/`, `examples/`, `protocol/`, `prototype/`, `Makefile`
**Files scanned:** about 95 tracked files listed; about 14 read
**Pattern extraction date:** 2026-11-09
