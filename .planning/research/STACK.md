# Technology Stack: milestone v0.2.0 "Real Fans" (additions only)

**Project:** esphome-sil
**Researched:** 2026-11-10
**Scope:** only what is NEW for the better fan model, runtime fault injection and `sil fan-analyze`. The validated v0.1.0 stack (Go 1.27.3, kong v1.17.0, coder/websocket v1.8.15, go-toml/v2 v2.4.3, uPlot 1.6.32 vendored, NDJSON sim-bus, ESPHome 2026.10.1 image, flynn/noise v1.1.0, go.yaml.in/yaml/v3 v3.0.6) is unchanged; see `.planning/milestones/v0.1.0-research/STACK.md`.
**Overall confidence:** HIGH for the Go side and the ESPHome facts (read from the pinned image's source), MEDIUM for two on-hardware behaviours that need a spike (see "Spikes").

## Verdict in one table

| Question | Decision | New Go module? |
|----------|----------|----------------|
| Go ESPHome native API client | Hand-roll on top of the existing Noise code; extract a reusable `Conn` from `internal/apiclient/probe.go`; tiny internal protobuf wire helper; ~12 message types; golden vectors generated from the image's `aioesphomeapi` | **No** |
| Protobuf runtime | None. No `google.golang.org/protobuf`, no codegen. (Acceptable fallback if the hand-rolled helper grows: `google.golang.org/protobuf/encoding/protowire` only, never generated code) | No |
| Fitting / numerics | Hand-rolled `internal/fit` (stdlib `math`, `sort`, `slices`): isotonic regression, variable-projection + Nelder-Mead against the sim's own `internal/model`. No gonum | **No** |
| report.html | Self-contained single file: server-rendered tables + **inlined vendored uPlot 1.6.32** (read from `ui.FS()`) + data as embedded JSON. No SVG renderer, no CDN | No |
| Tach capture on ESP32-S2 | **Custom ESPHome component with a GPIO ISR that stores `esp_timer_get_time()` edge timestamps in a ring buffer**, pulled in chunks through an API action with response. Keep one stock `pulse_counter` (PCNT) channel as the "what the product sees" cross-check. Not `pulse_meter` for the truth path | No (firmware-side component) |
| Deterministic noise | `math/rand/v2`, `rand.New(rand.NewPCG(seed, stream))`, one stream per port and per noise purpose; stable value stream is locked by Go | No |

Net new Go dependencies for the whole milestone: **zero**. That is the point of this research.

---

## 1. Go ESPHome native API client

### What exists (checked)

| Option | Verdict | Why |
|--------|---------|-----|
| `github.com/mycontroller-org/esphome_api` v1.4.0 (2026-07-18, Apache-2.0, 18 stars) | **Reject** | Only Go client found. Its `go.mod` drags in cobra, viper, zap, `mycontroller-org/server/v2`, goment, tablewriter; it pins a pre-release `flynn/noise` and `protobuf v1.34.2`; its generated protos track an old `api.proto`. We would inherit ~30 modules to avoid ~600 lines, and still have to patch it for user-defined action responses (API 1.1x, ESPHome 2025.12+). |
| `aioesphomeapi` (Python, 46.12.0 inside the pinned image) | **Reference oracle only** | Used to generate golden vectors and as the cross-check in tests. The `sil` binary stays Python-free. |
| Generate Go from `api.proto` (`protoc-gen-go` + `google.golang.org/protobuf` v1.36.12) | **Reject** | `api.proto` is ~1,700 lines with ESPHome-private options (`api_options.proto`: `id`, `source`, `ifdef`, `field_ifdef`, ...) that must also be compiled; adds a codegen step and a runtime dep to read ~12 messages. The existing `probe.go` already proved the wire format is easy. |

### Recommendation: extend `internal/apiclient`, no new module

`probe.go` already contains the hard part: Noise_NNpsk0_25519_ChaChaPoly_SHA256 with prologue `NoiseAPIInit\0\0`, frame indicator `0x01` + big-endian u16 length, encrypted `[u16 type][u16 len][protobuf]`, the server hello (`0x01 name\0 mac\0`), handshake-reject frame, and a hand-rolled varint reader. Refactor, do not rewrite:

1. Extract `exchange()` into `Dial(ctx, addr, key, clk) (*Conn, Hello, error)`; keep `Probe` as a thin wrapper so the v0.1.0 readiness check and its tests do not change.
2. `Conn`: one reader goroutine, one mutex-guarded writer (`flynn/noise` `CipherState` is not goroutine-safe and nonces are implicit, so exactly one place encrypts and exactly one decrypts). Reader answers `PingRequest`(7) with `PingResponse`(8) and `DisconnectRequest`(5) with `DisconnectResponse`(6) itself and hands everything else to a typed channel. Host sends its own `PingRequest` every ~20 s (device keepalive is 60 s, disconnect at 2.5x, `api_connection.cpp`).
3. `internal/apiclient/pb` (new, ~120 lines): `AppendVarint/Tag/Bytes/Fixed32/Float`, `Next()` field iterator that skips unknown fields (forward compatible). Replaces `uvarint()` in `probe.go`. Fuzz target + table tests.
4. Messages to implement (IDs verified against the image's `api.proto`):

| Direction | Message (ID) | Used for |
|-----------|--------------|----------|
| both | Hello 1/2, Ping 7/8, Disconnect 5/6 | session |
| c->s | DeviceInfoRequest 9 / Response 10 | board identity, `esphome_version` pin check, MAC |
| c->s | ListEntitiesRequest 11, Done 19 | discovery |
| s->c | ListEntitiesServicesResponse 41 (name, **fixed32 key**, args, `supports_response`) | find the action keys |
| c->s | ExecuteServiceRequest 42 (`key`, args, `call_id`, `return_response`) | run test / set duty / fetch capture |
| s->c | ExecuteServiceResponse 131 (`call_id`, `success`, `error_message`, `response_data` bytes = JSON) | bulk edge download |
| s->c | ListEntitiesSensorResponse 16, SubscribeStatesRequest 20, SensorStateResponse 25 (`fixed32 key`, `float state`, `missing_state`) | live RPM preview and the stock `pulse_counter` cross-check channel |
| optional | SubscribeLogsRequest 28 / Response 29 | surfacing firmware log lines in the CLI |

Entity keys come from the `ListEntities*` answers; the client never computes ESPHome's FNV hash. Use only actions plus sensor states: no `fan`/`number`/`button` messages are needed, which keeps the hand-rolled surface small.
5. Conformance: new `protocol/vectors/api_vectors.ndjson`, generated once by running the image's `aioesphomeapi.api_pb2` offline (`docker run --network none ... python -I`), hex-encoded request and response frames. Exercised from Go tests exactly like `bus_vectors.ndjson`. This is the only guard against hand-rolled drift.
6. Message size: ESP32 `MAX_MESSAGE_SIZE` is 32 KiB per API message (Noise frame 65,535). Capture download must be chunked (<= 256 edges per response, about 3 KB of JSON).
7. Key handling: reuse the instance key generation (crypto/rand, base64, 0600). The measuring firmware is built with its own key; the CLI takes `--key-file` / `SIL_FA_KEY`; never print the key (the existing `ProbeError` convention).
8. Time: all deadlines through `internal/clock` (wall-clock lint is enforced; a real clock implementation is injected by the CLI).

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/flynn/noise` | v1.1.0 (already in go.mod; latest tag, Feb 2024, stable-quiet, no alternative of comparable standing) | Noise handshake and CipherStates | Reuse unchanged |
| `golang.org/x/crypto` | v0.58.0 (already indirect) | chacha20poly1305 via flynn/noise | Unchanged |
| `encoding/json` (stdlib, v2-backed in Go 1.27) | Go 1.27 | Parse `response_data` | The JSON is firmware-emitted ArduinoJson: plain, no duplicate keys |

**Confidence: HIGH** (protocol read from image source and existing working code; message IDs verified).

---

## 2. Fitting and numerics

### Decision: hand-rolled `internal/fit`, no gonum

| Candidate | Verdict | Why |
|-----------|---------|-----|
| `gonum.org/v1/gonum` v0.17.0 (2025-12-29, 4.7 MB zip) | **Reject** | Verified in the module source: `optimize` has Nelder-Mead, BFGS, L-BFGS, CG, CMA-ES, Newton, but **no Levenberg-Marquardt / nonlinear least squares and no isotonic regression**. The module's `go.mod` references x/tools, gonum/plot, fonts, pdf libs; importing `optimize` links BLAS/LAPACK/mat. We would pull a large dependency tree to get a 60-line simplex. |
| Hand-rolled | **Use** | The problems are tiny: 2 to 6 parameters, a few thousand points, one smooth objective. |

### What to write (all pure functions, stdlib only)

| Need | Method | Notes |
|------|--------|-------|
| Monotone duty to RPM curve per direction (up sweep, down sweep) | **Isotonic regression (PAV)**, then resample to a knot table (e.g. 5 % duty grid) | Real data is noisy and a LUT with non-monotone wiggles would make the sim's inverse (RPM to duty) ambiguous. Up and down are fitted separately; the gap is the hysteresis loop. |
| Step response / spin-up / coast-down | **Fit the sim's own model**: `objective(params) = RMS(model.Step replay - measured RPM at the measured edge times)`, minimised with **Nelder-Mead** (multi-start, ~60 lines) | Gradient-free is right: start delay (dead time) creates a kink; a coast-down with fan-law drag is not a pure exponential (`d w/dt = -a w - b w^2`), so the forward model matters. Fitting through `internal/model` guarantees "replay matches measurement" is the same code path that the done criterion checks. Do not fit a closed form and then hope the sim agrees. |
| Linear nuisance parameters (offset, gain) | **Variable projection**: solve them by closed-form linear least squares inside the objective | Shrinks the simplex to the truly nonlinear parameters (td, tau, drag split). |
| Tach quirks | Median + MAD of edge intervals at steady state gives jitter sigma; ppr from the ratio of edge rate to a reference RPM | Plain `slices.Sort`. |
| Stated tolerance | RMS, max abs error and fraction of samples within +/- x % between measured and replayed RPM; reported per test | |
| Uncertainty (optional) | Residual bootstrap using `math/rand/v2` PCG with a fixed seed | Keeps `fan.toml` provenance honest ("tau_up = 0.8 s +/- 0.1"). |

Integration: `internal/fit` imports `internal/model` (pure, no clock) and `internal/fanlib` types; it must not import `clock`/`engine`. Tests recover known parameters from data synthesised by the model plus PCG noise (closed loop, deterministic). If NM ever proves unreliable, upgrade to Levenberg-Marquardt with a numeric Jacobian and a 3x3/4x4 normal-equation solve (~100 lines); still no gonum.

### Writing the fitted `fan.toml`

go-toml v2's encoder does not emit comments, and the library files carry long provenance blocks (see `fans/arctic-p12-pwm-pst.toml`). **Render with `text/template`** (comments, units, measurement date, fit statistics, board/firmware/fan identifiers) and validate by round-tripping through the existing strict `fanlib` loader in the same test. No new library. The strict loader's nine-key schema must be extended (curve tables, noise, ppr, power/airflow datasheet block); keep `DisallowUnknownFields` semantics.

**Confidence: HIGH** (gonum contents verified in v0.17.0 source; method choice is standard practice, MEDIUM on NM robustness until exercised on real data).

---

## 3. report.html without a CDN

### Decision: one self-contained HTML file, uPlot inlined

| Option | Verdict | Why |
|--------|---------|-----|
| **Inline the already-vendored uPlot 1.6.32** (51 KB JS + 1.9 KB CSS) read from `ui.FS()` (`ui/web/vendor/`) | **Use** | Zero new vendored code, one sha256-gated copy (`ui/vendor_test.go` keeps guarding it), same look as the live UI, zoom/cursor for hunting the kick-start and coast-down edges, double-clickable offline file. |
| Hand-written Go SVG charts | Reject as primary | About 250+ lines of axis/tick/legend code to maintain, no zoom; static output is only needed for headline numbers, which are plain HTML tables. |
| Chart.js, Plotly, D3 | Reject | Weight, CDN habits, not vendored. |

Mechanics:
- `html/template` with `//go:embed report.tmpl` in a new `internal/fananalyze/report` package; `go:embed` of the vendor files is not needed, import `github.com/zeroflow/esphome-sil/ui` and `fs.ReadFile(ui.FS(), "vendor/uPlot.iife.min.js")`.
- The page is meaningful without JS: server-rendered summary (fan identity, firmware and ESPHome version, fitted parameters with uncertainty, tolerance verdict PASS/FAIL, per-test RMS and max error) as tables. Charts are progressive enhancement.
- Data goes in `<script type="application/json" id="report-data">...</script>` (HTML-escaped by `encoding/json`, so `</script>` cannot break out) so an agent can read numbers from the file. Also write `report.json` beside it; the CLI's stdout stays the JSON result.
- Charts: duty vs RPM (up/down sweep points + fitted curves = hysteresis), step responses (measured per-edge RPM vs sim replay, residual strip), start/stop (spin-up delay, coast-down), tach-jitter histogram. Decimate to at most ~5k points per series (min/max bucket decimation) before embedding.
- CI check: reuse the "no `http(s)://` reference" lint from the UI tests on the generated report (uPlot's own SVG namespace string must be allow-listed exactly as the page test already does).
- uPlot lives outside any reactive framework; the existing page is vanilla ES modules (Alpine was never vendored), so the report script is a short vanilla `<script>`; do not import `ui/web/js/*` modules (they assume the live API).

**Confidence: HIGH.**

---

## 4. Fast tach capture on ESP32-S2, ESPHome 2026.10.1

### Required resolution (derived, not assumed)

Tach has 2 pulses per revolution on both fans, rising-edge counting as in the product (`pulse_counter`, `multiply: 0.5`).

| Fan operating point | Pulse rate | Edge period |
|---------------------|-----------:|------------:|
| Noctua NF-A12x25 @ 450 RPM | 15 Hz | 66.7 ms |
| Arctic P12 @ 200 RPM | 6.7 Hz | 150 ms |
| either @ 2000 RPM | 67 Hz | 15 ms |

- Information arrives only at edge rate. Per-edge period measurement is therefore the best possible RPM estimator; a windowed count at 1 s (product behaviour) quantises to `60/(ppr*T)` = **30 RPM per pulse** and has 1 s time resolution. That is the *observable* the sim must reproduce (v0.2.0 "tach quantisation"), but it is useless as the *truth* for fitting tau (~0.3 to 2.5 s) or start delay.
- Needed: edge time stamps with resolution and accuracy well below 1 ms (a few percent of the shortest 15 ms period). `esp_timer_get_time()` gives 1 us; GPIO ISR latency on a single-core S2 at 240 MHz is typically microseconds, spikes of tens of us while Wi-Fi or flash-cache activity runs. That is 0.1 to 0.5 % of the shortest period, far below fan jitter itself. Time alignment between the duty write and the edges must be better than ~10 ms, so **both must be stamped on the same device clock**, not by the host.
- Network arrival time on the host (Wi-Fi, 5 to 50 ms jitter) is not good enough, which is why the data path below is device-timestamped.

### Options compared (read from `esphome/components/*` in the pinned image)

| Option | Gives | Verdict |
|--------|-------|---------|
| `pulse_counter` with PCNT (default `use_pcnt: true` on ESP32, `internal_filter` max 13 us) | Pulses per `update_interval`, no per-edge time | **Keep exactly one channel as cross-check** and for reproducing the product observable; useless as truth. All four PCNT units are used by the product, so the measuring firmware needs only one. |
| `pulse_meter` | ISR rising-edge `micros()`, but period is computed and **published from `loop()`** (ESPHome loop, 16 ms default), batching several edges as `delta/count`; no timestamps leave the device; publishes only on edges and times out to 0 | Acceptable quick spike (zero C++), **not** for the done criterion: loses per-edge time and gets Wi-Fi-jittered arrival time. |
| ESP32-S2 RMT RX / PCNT watch points | Hardware timing | Reject: RMT has 4 channels total, an idle threshold far below the 66-150 ms periods, and needs an ESP-IDF driver layer in an Arduino build; the ISR approach is already far better than the physics. (MCPWM capture is not available on the S2: MEDIUM, from training knowledge.) |
| **Custom component: GPIO ISR + `esp_timer_get_time()` into a ring buffer** | Every rising (and optionally falling) edge with 1 us stamps, lossless while the host keeps pulling | **Use.** Same mechanism pulse_meter already uses (`InternalGPIOPin::attach_interrupt`, `ISRInternalGPIOPin`, ISR in IRAM), minus the loop-side averaging. |

### Firmware design (new `components/fan_analyze/`, ESP32-S2, Arduino 3.3.12 as ESPHome 2026.10.1 recommends)

- Per channel: ISR stores a `uint32_t` microsecond stamp (software min-interval gate of ~1 ms rejects PWM crosstalk at 25 kHz, which a 13 us glitch filter would not); ring of 1024 entries; `loop()` extends to 64 bit using `esp_timer_get_time()` and tracks overruns (a counter the host can see: a lossless claim must be verifiable).
- PWM stays `ledc` at 25 kHz on GPIO12-15 with the same config as `hardware-rev-3.1.yaml` so the electrical path is the real one (S2 LEDC at 25 kHz gives 11-bit duty at 80 MHz APB, ~0.05 % steps; keep dither-free).
- The component owns the timeline: it stamps the actual `FloatOutput::set_level` call with `esp_timer_get_time()` and runs scripted steps itself (`run_step(port, from, to, hold_ms)`), so the step instant is exact and independent of Wi-Fi. The host never needs a synchronised clock.
- Control and data via **API user-defined actions** (ESPHome `api: actions:` with `supports_response`; present in 2026.10.1: `api/__init__.py` auto-loads `json` when `api.respond` has `data:`): `set_duty`, `run_step`, `capture_start/stop`, `fetch(port, from_index, max)` returning `{base_us, overruns, t: [...]}`. Live preview: plain `sensor:` entities (windowed RPM) over `SubscribeStates`.
- Keep one stock `pulse_counter` sensor (PCNT, `multiply: 0.5`, 1 s) on one channel to validate "host recomputes the product's RPM from raw edges" against the real product path.
- Do not use the ESPHome `fan:` component or anything that writes flash during a test (prefs, OTA): flash-cache-disabled windows delay ISRs that are not fully in IRAM. Disable `restore_mode`, no `ota` while measuring (flash via the normal path first).

### Build implications (this is the one place that is not hermetic)

- The host-sim build runs `--network none`; an ESP32-S2 Arduino build cannot: PlatformIO fetches the platform/toolchain/Arduino framework and ArduinoJson (via `json`, needed by `api.respond`). `internal/fwbuild` already has the pattern (one-time `network=bridge` fetch, then a persistent `/cache` volume, `HOME=/cache/home`, `PLATFORMIO_CACHE_DIR`). Add a "device" build mode that reuses the same pinned image and cache, allows network only on a cold cache, and bypasses the sim lint (`lint.go` currently rejects `esp32:`). Subsequent builds are offline from the cache: a vendor shutdown after the first fetch does not kill the pipeline. State this exception in the docs.
- The measuring firmware lives in this repo (not the product repo) and has its own top-level YAML; hardware pins come from the Rev 3.3 package or are copied verbatim.

**Confidence:** HIGH for ESPHome facts (source read), MEDIUM for hardware behaviour (see Spikes).

---

## 5. Deterministic noise RNG

`math/rand/v2`, stdlib, no dependency.

```go
r := rand.New(rand.NewPCG(runSeed, uint64(port)<<8|purpose)) // one Rand per (port, purpose)
v := r.NormFloat64()
```

- The Go team states that derived methods are "locked in" for v2 (`IntN`, `NormFloat64` bias fix "locked in the new value streams", go.dev/blog/randv2). Seeded runs are therefore reproducible across Go upgrades. Pin it anyway with a golden-value test (first N draws for a fixed seed) as a canary.
- Separate stream per port and per purpose (RPM jitter, tach dropout, fault timing): enabling a fault or changing the port count then never shifts another stream's values.
- Draw per **model step / per tach window**, never per wall tick; the engine step is `dt`-based, so noise must be a function of simulated time (sigma scaled by `sqrt(dt/dt0)` for additive noise) so results do not depend on tick rate. No `time.Now()` seeding (wall-clock lint); the run seed is a `sil up --seed` flag recorded in status/run info; default fixed (0), not random, so tests are repeatable by default.
- `*rand.Rand` is not goroutine-safe: own it inside the engine's single goroutine; do not use top-level `rand.Float64()`.
- `rand.PCG` implements `MarshalBinary`, so a future snapshot or replay can persist the stream state cheaply.
- Use the same PCG in `internal/fit` for bootstrap and for the synthetic-data recovery tests.

**Confidence: HIGH.**

---

## Everything else new (no research needed beyond a line)

| Area | Choice | Note |
|------|--------|------|
| CLI | kong v1.17.0 nested command `fan-analyze {run,sweep,step,startstop,fit,report}` | Existing; stdout JSON, stderr slog. |
| Fault injection | Plain Go in `internal/model`/`engine`; new JSON API routes via stdlib `ServeMux`; UI controls in vanilla JS | No library. Fault state is a struct on the port; PCG stream for intermittent tach. |
| Secrets/inputs | `go.yaml.in/yaml/v3` (existing) if reading a secrets file; not needed for the API key | `wifi-fancontroller/secrets.yaml` holds only Wi-Fi credentials; the API key is per build. |
| Test data | Recorded raw edge captures as `testdata/*.ndjson` golden inputs for `fit` | Lets the fit and report tests run without hardware. |

## What NOT to add

| Do not add | Reason | Instead |
|------------|--------|---------|
| `mycontroller-org/esphome_api` | Stale, ~30 transitive modules, old proto | Hand-rolled `Conn` over existing Noise code |
| `google.golang.org/protobuf` + protoc-gen-go | Codegen step and runtime for ~12 messages | `internal/apiclient/pb` wire helper with golden vectors |
| `gonum.org/v1/gonum` (any package) | No LM, no isotonic; heavy tree; binary growth | `internal/fit` |
| Any plotting/report library, headless-browser PDF, CDN | Locality | Inlined uPlot + HTML tables |
| `pulse_meter` as the truth capture | Loop-side averaging, no device stamps | Custom ISR timestamp component |
| Host-side timestamps for edges | Wi-Fi jitter | Device-side `esp_timer_get_time()` timeline |
| Python (`aioesphomeapi`) in the CLI | Constraint | Only as vector oracle in tests |
| `crypto/rand` or time-seeded RNG for noise | Non-reproducible runs | PCG with explicit seed |
| Lua / fault scripts | Out of scope in v0.2.0 | Runtime-switched faults only |

## Spikes to run early (hardware or protocol unknowns)

1. **ISR + PCNT on one pin.** Confirm an `attach_interrupt` ISR and the `pulse_counter` PCNT unit on the same tach GPIO coexist on ESP32-S2/Arduino 3.3.12 (pin setup both configure INPUT_PULLUP; expected to work, unverified).
2. **API action with response end to end.** One action returning ~256 ints through `api.respond`, called from a throwaway Go `Conn`, checked against `aioesphomeapi` (`execute_service` with `return_response`). Also confirm heap headroom with ArduinoJson at 256 entries per call on the S2.
3. **Edge loss under Wi-Fi load.** Run 4 channels at 2000 RPM for 60 s with a 250 ms pull cadence and verify `overruns == 0` and no gaps against the stock `pulse_counter` total.
4. **Cold-cache S2 build** through the pinned image: record cache size, time, and that a second build with `--network none` succeeds.

## Alternatives considered

| Category | Recommended | Alternative | Why not |
|----------|-------------|-------------|---------|
| API client | Hand-rolled on flynn/noise | mycontroller esphome_api | Stale deps, old proto |
| Protobuf | wire helper | `protowire` | Fine fallback, but a 120-line helper keeps `go.mod` unchanged |
| Bulk data path | Action with JSON response, chunked | `text_sensor` stream of base64 batches via `SubscribeStates` | Works with fewer message types, but is a hack on entity state (dedup, size) and has no request/response backpressure; keep as fallback if action responses misbehave (spike 2) |
| Bulk data path | Action with JSON response | `SubscribeLogs` line scraping | Logger-level dependent, lossy under load |
| Truth capture | ISR timestamps | PCNT counts | 30 RPM and 1 s resolution |
| Numerics | stdlib + simplex | gonum optimize | See section 2 |
| Report charts | inline uPlot | Go SVG | See section 3 |
| fan.toml writer | `text/template` | go-toml encoder | No comments |

## Installation

```bash
# Go side: no new modules. go.mod and go.sum stay as they are.
go mod tidy   # must be a no-op

# Golden API vectors (one-off, offline, uses the pinned image's aioesphomeapi 46.12.0)
docker run --rm --network none --entrypoint python3 \
  -v "$PWD/tests/tools:/w:ro" ghcr.io/esphome/esphome:2026.10.1 -I /w/gen_api_vectors.py > protocol/vectors/api_vectors.ndjson

# Measuring firmware (cold cache needs network once; then offline)
sil fan-analyze build --device-key-file run/fa.key   # wraps the pinned image, shared /cache
```

## Sources

- Pinned image `ghcr.io/esphome/esphome:2026.10.1`, source read: `components/pulse_counter/{sensor.py,pulse_counter_sensor.cpp,.h}` (PCNT default, 13 us filter cap), `components/pulse_meter/{sensor.py,pulse_meter_sensor.cpp,.h}`, `components/api/{api.proto,api_frame_helper.h,api_connection.cpp,__init__.py}` (message IDs, 32 KiB message cap, 60 s keepalive, `supports_response`, json auto-load), `components/esp32/__init__.py` (Arduino 3.3.12 recommended): HIGH
- `/home/claude/esphome-sil/internal/apiclient/probe.go`, `fans/*.toml`, `ui/web/vendor/VENDOR.md`, `internal/fwbuild/*.go`: HIGH (local code)
- `/home/claude/wifi-fancontroller/hardware-rev-3.1.yaml` (ledc 25 kHz GPIO12-15, pulse_counter GPIO16/17/18/21, `multiply: 0.5`): HIGH
- Go module proxy `proxy.golang.org`: gonum v0.17.0 (2025-12-29), mycontroller-org/esphome_api v1.4.0 (2026-07-18, go.mod read), google.golang.org/protobuf v1.36.12, flynn/noise v1.1.0 (latest tag), x/crypto v0.58.0: HIGH
- gonum v0.17.0 source downloaded and listed (`optimize/`, `interp/`; no Levenberg-Marquardt or isotonic code found by grep): HIGH
- `https://go.dev/blog/randv2` (derived-method value streams locked in): MEDIUM-HIGH (statement about PCG/ChaCha8 output stability is implicit; golden canary test recommended)
- `https://developers.esphome.io/architecture/api/protocol_details/` (Noise frame layout, handshake): HIGH
- Context7 `/gonum/gonum` (confirmed package scope only): MEDIUM
- ESP32-S2 lacks MCPWM, RMT channel count, ISR latency magnitudes: training knowledge, MEDIUM-LOW, none drive a decision
