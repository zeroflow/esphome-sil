# Phase 1: Closed-Loop Fan Skeleton - Pattern Map

**Mapped:** 2026-11-09
**Files analyzed:** 33 (new; no existing files modified except .gitignore/README)
**Analogs found:** 9 / 33 (repo is greenfield; all Go code has no in-repo analog)

All analog paths below are git-tracked (verified via `git ls-files`): everything under `prototype/`.

## File Classification

| New File | Role | Data Flow | Closest Analog | Match |
|----------|------|-----------|----------------|-------|
| `components/sim_bus/__init__.py` | config (ESPHome hub schema) | codegen | `prototype/components/sim_bus/__init__.py` | exact (extend) |
| `components/sim_bus/output.py` | component platform | codegen | `prototype/components/sim_bus/output.py` | exact |
| `components/sim_bus/sensor.py` | component platform | codegen | `prototype/components/sim_bus/sensor.py` | exact |
| `components/sim_bus/sim_bus.h/.cpp` | service (socket client hub) | event-driven / streaming | `prototype/components/sim_bus/sim_bus.{h,cpp}` | exact (extend) |
| `components/sim_bus/sim_protocol.h` | utility (header-only codec) | transform | parsing helpers `sim_bus.cpp` L104-121 | partial |
| `components/sim_bus/sim_output.h`, `sim_sensor.h` | component | request-response | `SimOutput`/`SimSensor` in `sim_bus.h` L50-61 | exact (split) |
| `examples/fan-skeleton.yaml` | config | n/a | `prototype/sim.yaml` | role-match |
| `tests/cpp/test_protocol.cpp` | test | transform | none | none |
| `protocol/PROTOCOL.md`, `protocol/vectors/frames.tsv` | doc/fixture | n/a | none (spec in `.claude/CLAUDE.md` "Sim-bus protocol decision") | none |
| `internal/bus/` (codec + Server) | service | streaming (UDS NDJSON) | `prototype/tools/fake_sim.py` (peer behaviour only) | partial |
| `internal/model/`, `internal/clock/`, `internal/engine/` | model / utility / actor | transform, event-driven | none | none |
| `internal/fanlib/`, `fans/arctic-p12-pwm-pst.toml`, `fans/embed.go` | config loader | file-I/O | none (go-toml snippet in RESEARCH Code Examples) | none |
| `internal/supervisor/`, `internal/apiclient/`, `internal/build/`, `internal/identity/`, `internal/rundir/`, `internal/control/`, `internal/cli/`, `cmd/sil/main.go` | Go services/CLI | various | none | none |
| `e2e/` client script | test | request-response | `prototype/tools/api_client.py` | role-match |
| `go.mod`, `Makefile` | config | n/a | none | none |

## Pattern Assignments

### `components/sim_bus/__init__.py` (hub config)
**Analog:** `prototype/components/sim_bus/__init__.py`

Copy as-is (L1-36), then add: `validate_path` (<=107 bytes, message `sim_bus path is N bytes; sun_path limit is 107.`), `SIL_BUS` env override, `PollingComponent` base for the 1 s RPM mean, `setup_priority::BUS`. Channel names move to `portN.duty` style (IO-06).
```python
CONFIG_SCHEMA = cv.Schema({
    cv.GenerateID(): cv.declare_id(SimBus),
    cv.Optional(CONF_PATH, default="/tmp/sil.sock"): cv.string,
}).extend(cv.COMPONENT_SCHEMA)
CHILD_SCHEMA = cv.Schema({
    cv.GenerateID(CONF_SIM_BUS_ID): cv.use_id(SimBus),
    cv.Required(CONF_CHANNEL): cv.string,
})
async def register_child(var, config):
    hub = await cg.get_variable(config[CONF_SIM_BUS_ID])
    cg.add(var.set_channel(config[CONF_CHANNEL])); cg.add(var.set_bus(hub)); cg.add(hub.register_child(var))
```

### `output.py` / `sensor.py`
**Analogs:** `prototype/components/sim_bus/output.py` L1-20, `sensor.py` L1-18. Copy unchanged.
```python
CONFIG_SCHEMA = (output.FLOAT_OUTPUT_SCHEMA.extend({cv.Required(CONF_ID): cv.declare_id(SimOutput)})
    .extend(CHILD_SCHEMA).extend(cv.COMPONENT_SCHEMA))
async def to_code(config):
    var = cg.new_Pvariable(config[CONF_ID]); await cg.register_component(var, config)
    await output.register_output(var, config); await register_child(var, config)
```
Sensor: `sensor.sensor_schema(SimSensor, accuracy_decimals=1)` then `var = await sensor.new_sensor(config)`. Note: `register_child` call order is the same in both. Do not copy `binary_sensor.py`/`light.py` (out of scope).

### `sim_bus.h/.cpp` (socket client hub)
**Analog:** `prototype/components/sim_bus/sim_bus.{h,cpp}`

Keep: non-blocking AF_UNIX connect (L126-143), `wake_register_fd`, `flush_` with `MSG_NOSIGNAL|MSG_DONTWAIT` and EAGAIN keep-remainder (L150-157), 500 ms retry in `loop()` (L159-163), line framing by `find('\n')` (L171-175), `on_connected()` replay of last output.
```cpp
int fd = ::socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
...
wake_register_fd(fd);
this->tx_ += "{\"k\":\"hello\",\"proto\":1}\n";
for (auto *c : this->children_) c->on_connected();
```
Gaps to close (from CONTEXT): manifest/snapshot after HELLO, string escaping (`\"`, `\\`; current `get_str` stops at first `"`), dedupe outbound state, `strncpy` truncation guard (also checked in Python), setup priority `BUS` instead of `HARDWARE`, `SIL_BUS` env path, drop `SimBinarySensor`/`SimLight` and their includes (L8-9 of header), 1 s RPM mean via `PollingComponent`. Remove `last_try_` using `millis()` only if the time factor needs it; keep otherwise. Float format `%.6g` (L186) differs from Go; use golden vectors (Pitfall 9).

### `examples/fan-skeleton.yaml`
**Analog:** `prototype/sim.yaml`. Keep `esphome.build_flags: [-static]`, `host:`, `external_components` local path (change `path: components` to repo-root relative / injected), hub block, `output: platform: sim_bus`, `fan: platform: speed` with `speed_count: 100`, `restore_mode: ALWAYS_OFF`, and sensor with `unit_of_measurement: RPM`, `accuracy_decimals: 0`.
```yaml
fan:
  - platform: speed
    output: pwm_fan1
    name: "Fan 1"
    id: fan1
    speed_count: 100
    restore_mode: ALWAYS_OFF
```
Extend to 4 ports; add `api: encryption:` and `reboot_timeout: 0s`; remove temp, binary_sensor and light blocks; name/MAC/key injected by `sil` (not in YAML, D-17). Hub `path` hard-coded `/tmp/sil-proto.sock` must become the `SIL_BUS` override.

### `internal/bus/` Server and tests (Go)
Peer behaviour reference: `prototype/tools/fake_sim.py` L1-6 (unlink stale path, bind, listen, accept single connection; send `{"k":"in","ch":...,"v":...}` with compact separators `(",",":")`). Python is a reference only; no Python in the simulator. Use RESEARCH Pattern 6 for the spec and `protocol/vectors/frames.tsv` for conformance.

### `internal/fanlib/` (Go, strict TOML)
No in-repo analog; use RESEARCH Code Examples (verified):
```go
err := toml.NewDecoder(r).DisallowUnknownFields().Decode(&fan)
var sm *toml.StrictMissingError
if errors.As(err, &sm) { for _, e := range sm.Errors { row, col := e.Position() } }
```

### `internal/supervisor/` firmware spawn (Go)
No in-repo analog; use RESEARCH "Supervisor firmware spawn" (LockOSThread, `Setpgid`, `Pdeathsig: SIGKILL`, `ESPHOME_PREFDIR`, `SIL_BUS` env) and "Static-link assertion" (`debug/elf` `.interp`).

### `e2e/` client
**Analog:** `prototype/tools/api_client.py` (aioesphomeapi client, role-match; not read in depth). Use the RESEARCH "e2e client inside the pinned image" docker invocation with `APIClient("127.0.0.1", port, None, noise_psk=key)`.

## Shared Patterns

### Child registration (all sim_bus platforms)
**Source:** `prototype/components/sim_bus/__init__.py` L18-36. `CHILD_SCHEMA` plus `register_child` apply to every platform file.

### Component class layout
**Source:** `prototype/components/sim_bus/sim_bus.h` L15-26 (`SimChild` with `on_value`/`on_connected`). Apply to `SimOutput` and `SimSensor`; both multi-inherit `Component` and `SimChild`.

### Logging
Go: `log/slog` to stderr only; stdout reserved for result JSON (STACK.md, D-04). C++: `static const char *const TAG = "sim_bus";` with `ESP_LOGI/ESP_LOGW` (`sim_bus.cpp` L102).

### Time
Go: only `internal/clock` may call `time.Now/Sleep/After/NewTicker` (lint, MODEL-04). Tests use `testing/synctest`.

### Test locations
C++ and Go tests never under `components/` (ESPHome compiles every `.cpp` there). Put C++ test in `tests/cpp/`.

## No Analog Found

All Go code (`cmd/`, `internal/*`, `go.mod`, `Makefile`), `fans/*.toml`, `protocol/*`, `tests/cpp/test_protocol.cpp`, `sim_protocol.h` (partially seeded by `get_str`/`get_num` at `sim_bus.cpp` L104-121). Planner should use RESEARCH.md Patterns 1-10 and Code Examples, and the library choices in STACK.md (kong, coder/websocket, go-toml v2, stdlib ServeMux).

## Metadata

**Analog search scope:** whole repo (`git ls-files`: 17 non-planning files, only `prototype/` is relevant)
**Files scanned:** 8 read (prototype components, sim.yaml, fake_sim.py head), CONTEXT.md, RESEARCH.md sections
**Pattern extraction date:** 2026-11-09
