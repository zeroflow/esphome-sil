# esphome-sil

Software-in-the-loop test bench for ESPHome firmware, in the spirit of an
automotive labcar. The ESPHome `host:` platform runs the real firmware modules
on Linux; simulated IO components (`sim_bus`) connect them over a local socket
to a plant simulator, so control loops can be tested closed-loop, locally and
fast, before firmware reaches real hardware.

First domain: the [WiFi Fan Controller](https://github.com/zeroflow/wifi-fancontroller)
(Rev 3.3 as reference): four PWM fan ports with RPM feedback, temperature and
humidity, three user buttons and a five-LED status strip.

Everything runs on your own machine; no cloud simulator involved.

**Status:** Phase 1 (closed-loop fan skeleton), Phase 2 (full Rev 3.3 bench),
Phase 3 (live web UI) and Phase 4 (fan model v2: schema 2 fan files with
provenance, pulse tach, airflow and power) are implemented. The CLI (`sil`) is
the primary interface and is usable by developers and coding agents alike (one
JSON value on stdout per command, progress and logs on stderr). Phase 3 adds a
live web UI (see [Web UI](#web-ui)); scenarios and the time factor are later
phases. See [docs/initial-idea.md](docs/initial-idea.md) for the idea
behind it.

Test pyramid this fits into: host SiL (this repo) -> hardware-in-the-loop rig
with tach/PWM loopback -> HIL with real fans.

## Requirements

- Linux (the ESPHome `host:` platform does not build elsewhere)
- docker with the pinned image `ghcr.io/esphome/esphome:2026.10.1` (builds the
  firmware; the first build needs network once to fill a local tool cache)
- Go 1.27.3 to build `sil` (a single static binary)
- [uv](https://docs.astral.sh/uv/) for the contract tests

## Quick start

```bash
make build                         # bin/sil, a static binary
bin/sil up dev                     # builds examples/rev33.yaml (first build is slow, later ones are cached); --seed N fixes the tach noise
bin/sil status dev                 # JSON: phase, api, ports (rpm, tach_rpm, airflow_m3h, power_w), env, buttons, leds, firmware, sim
bin/sil set temperature=27 humidity=60 -i dev   # inputs; also port2.fan=noctua-nf-a12x25-pwm
bin/sil press usr1 -i dev          # 200 ms press; --long holds 1500 ms, --down / --up set the level
bin/sil press usr1 --down --max-hold 10s -i dev # held; the simulator releases it by itself after 10 s of sim time
bin/sil watch --kind duty,rpm,tach,load --count 5 -i dev  # NDJSON: one snapshot, then one line per change
bin/sil history --window 60s -i dev # the last 60 s of duty, RPM and tach per port, one sample every 100 ms
bin/sil logs --source all --tail 50 -i dev      # firmware and simulator logs, colour removed, key redacted
bin/sil fans -i dev                # the fan library (JSON, provenance summary on stderr) and what is plugged into each port
bin/sil sim restart dev            # fresh simulator, the firmware keeps running and resyncs
bin/sil fw restart dev             # restart the firmware process with the same program and prefs
bin/sil down dev
```

`-i NAME` (or `$SIL_INSTANCE`) selects the instance of the verbs that take
further arguments; with a single live instance it can be omitted. Instance names
are `a-z`, `0-9` and `-`. Several instances run side by side, each with its own
run directory, API port, MAC and key. Exit codes: 0 ok, 1 the thing under test
failed or the request was refused, 2 the bench itself failed.

Four additions exist for the web page and are available to every client:

- `sil history [--window D] [-i NAME]` prints the plot history as one JSON value
  (`GET /api/v1/history?window_s=W`, `0 < W <= 300`): the simulator keeps the
  last 5 minutes of `sample` events, one per 100 ms of sim time with a duty, an
  RPM and a `tach` value per port (null while the firmware is not on the sim-bus;
  `tach` is also null in a port's first second). The first sample after a
  simulator restart carries `"gap": true`.
- `sample` is an opt-in `sil watch` kind (`sil watch --kind sample`): ten events
  a second, so a default watch never carries it.
- `sim.behind_real_time` in `sil status` is true while the simulator handles its
  ticks 50 ms or more late; `sil watch` shows the changes as `sim` events with
  the value `behind` or `in_sync` (back in sync after 2 s of sim time without a
  late tick).
- `sil press BUTTON --down --max-hold D` (`1ms` to `60s`) releases the button by
  itself after D of sim time with a `button` event `{"v": false, "reason":
  "max_hold"}`, so a client that goes away cannot leave a button held.
- When the simulator stops with a button held (`sil sim restart`, `sil down` or
  an engine failure), the stream first gets the release `{"v": false,
  "reason": "engine_stopped"}` and `sil status` shows the button released. After
  a simulator restart the firmware sees the release when it resyncs with the
  new simulator; after `sil down` or an engine failure the whole instance stops,
  firmware included, so there is no later resync.

A `sim` event with the value `restarted` means the simulator state was rebuilt.
Values set during the restart gap (`sil set`) seed the new simulator but arrive
in no event, so a client re-reads the state after `restarted` (`sil status`, or
the snapshot of a new `sil watch`; the page does this by itself).

## Web UI

Every running instance serves a live page. `sil up` prints the address
(`web UI: http://<lan-ip>:<port>/`) and `sil status` carries it as `web.url`;
open it in a browser on this machine, on another machine of the LAN or on a
phone. The page shows the four fan ports (RPM, duty and a rolling scope with the
last minute, which also draws the tach trace as a stepped dimmer line), the TACH
RPM, AIRFLOW (m3/h) and POWER (W) readouts of every port (`—` when the value is
null, for example AIRFLOW and POWER of a fan without datasheet values), the five
status LEDs, sim time, time factor, the firmware link and whether the simulator
is behind real time. It also drives the bench: choose the
fan of a port, hold the USR1..USR3 caps (a short hold is a click, about 1.5 s is
the long press), and set temperature and humidity. It is a pure client of the
same HTTP/WebSocket API the CLI uses (`sil status`, `sil watch`, `sil history`,
`sil fans`, `sil set`, `sil press`); every value it shows and every action it
takes can be reproduced with `sil`. If the connection is lost, the header reads
`DISCONNECTED, RETRYING`, the values dim, and the page reconnects by itself
(also across `sil sim restart` and `sil down` + `sil up` of the same instance);
no reload is needed. It needs no internet: all assets are inside the `sil`
binary, there is no CDN and no font download.

**The page and the HTTP API are unauthenticated.** By default they listen on all
interfaces (`--listen 0.0.0.0`), so anyone who can reach the address can watch
and drive the bench. This is a decision for a trusted LAN (Phase 3 D-08). `sil up`
prints the same notice. What protects you without a login: a Host allowlist
(IP addresses, `localhost` and this machine's name), cross-origin checks on the
WebSocket and on every state-changing request (a page you visit cannot drive the
bench), limits on connections and streams, `api.key` shown as `<redacted>` on
the network listener, and `down`, `sim restart`, `fw restart` and `sil logs`
refused there.

| Flag of `sil up` | Effect |
|------------------|--------|
| `--listen 127.0.0.1` | keep the page and API on this machine (any other IP picks that interface) |
| `--allow-host NAME` | also accept requests addressed to a DNS name, repeatable (for a reverse proxy or a local DNS entry) |
| `--web-port N` | web port; it is sticky per instance (first free port from 8054, stored next to the API port), so a bookmark or phone shortcut keeps working across `sil down` + `sil up`; a taken port fails with exit 2 instead of moving |
| `--ui-dir DIR` | serve the page from a directory instead of the binary (UI development, no Go rebuild) |

Machine-only operations stay on the private Unix socket: the firmware key
(`api.key`, read it from `sil status` on this machine), `sil down`,
`sil sim restart`, `sil fw restart` and `sil logs`. On shared networks and on CI
runners use `--listen 127.0.0.1` or firewall the web port, like the API port.

## The Rev 3.3 example

`examples/rev33.yaml` (the default of `sil up`) mirrors the product's IO surface
with sim components: four fans on `output` platform `sim_bus` (PWM duty) with
RPM `sensor`s, temperature and humidity (`sensor` platform `sim_bus`), three
`binary_sensor` buttons and an addressable `light` strip of five LEDs with
`partition` lights for the segments. Home Assistant sees 13 entities (Fan 1..4,
Fan 1..4 Speed, Temperature, Humidity, Button USR1..USR3); the LEDs are internal
and visible in `sil status` and `sil watch`. The minimal four-fan config stays
available as `--config examples/fan-skeleton.yaml`.

The example carries a thin, product-like behaviour so the bench can be driven end
to end (it is not the real product code):

| Trigger | Behaviour |
|---------|-----------|
| boot | board LED pulses blue until a real API client connects, then static green (the readiness probe of `sil` itself does not count) |
| a fan turns (RPM above 0) | its LED segment is green, off when it stops |
| USR1 press | all fans +10 % |
| USR3 press | all fans -10 % (0 turns a fan off) |
| USR2 long press (`sil press usr2 --long`) | all fans 100 % |

Inputs: temperature -40..125 degrees Celsius and humidity 0..100 %RH (HDC1080
bounds, an assumption).

## Using the components in your firmware

The simulated IO lives in one ESPHome external component, `sim_bus`, with the
platforms `output`, `sensor`, `binary_sensor` and `light`. The example uses it
from the checkout:

```yaml
external_components:
  - source:
      type: local
      path: ../components
    components: [sim_bus]

sim_bus:
  id: bus
```

A product repo uses the same block pointing at a tagged release once one exists
(there is no tag yet; until then use the local form against a checkout):

```yaml
external_components:
  - source: github://zeroflow/esphome-sil@vX.Y
    components: [sim_bus]
```

`sil up` lints the YAML first and names components that cannot run on the host
platform, with a replacement hint: `ledc` -> `output` platform `sim_bus`,
`pulse_counter` -> `sensor` platform `sim_bus`, `esp32_rmt_led_strip` -> `light`
platform `sim_bus` with `partition` lights, `i2c` -> `sensor` platform `sim_bus`
with `mode: latest` for temperature/humidity. The firmware must keep
`esphome.build_flags: [-static]` and `api: encryption:`; `sil` injects the name,
MAC, port (substitutions `sil_name`, `sil_mac`, `sil_port`) and the key (env
`SIL_API_KEY`).

## Contract tests

`tests/` holds pytest suites that drive real bench instances through the
`sil` CLI and `aioesphomeapi`: a closed-loop contract test (the Rev 3.3 chain
from Home Assistant style fan commands to RPM, buttons, LEDs and inputs), a
smoke test that the entities are visible and commandable, and a tach test
(`test_tach_steps.py`) that reads `Fan 3 Speed` through the native API, like Home
Assistant, and checks that the firmware-visible value moves in the fan's 30 rpm
pulse steps. That is the one place where the plant side is read through
`aioesphomeapi` rather than `sil`: the firmware-visible value exists only there.
The bench uses one session fan library: the pinned v0.1.0 fixtures
(`testdata/fans-v0.1.0`, so the exact v0.1.0 numbers stay testable) plus
`testdata/fans-v2/s7-steady.toml`; a `SIL_FAN_DIR` of your own never leaks in.

```bash
make lock-check       # the Python dependencies must match the product HIL lock (tests/uv.lock vs LOCK_REF)
make test-contract    # builds sil, runs lock-check, then pytest (needs docker, the pinned image and uv)
```

`make test-contract` depends on `make lock-check`, so dependencies are never
installed before the hash comparison with `../wifi-fancontroller/hil/uv.lock`
(override with `LOCK_REF=`). The tests use the reserved instances `ct-main` and
`ct-second`; do not use these names yourself. Their state lives in
`~/.cache/sil-contract/state` (override with `SIL_CONTRACT_STATE`). A warm run
takes about half a minute.

The Go side has its own suites: `make check` (format, vet, unit tests, the
sim-bus C++ codec test) and `make e2e` (full runs against the pinned image),
plus `make test-cpp-image` (the codec test with the image's compiler) and
`make mod-verify`. The page is tested with headless Chrome (`make test-ui`,
needs `google-chrome` or `SIL_CHROME`; the e2e acceptance of the page, `TestWebAcceptance`,
runs in `make e2e`).

## Fan library

`sil fans` lists the plant models in `fans/*.toml`: `arctic-p12-pwm-pst`
(default) and `noctua-nf-a12x25-pwm`. `sil up --fan ID` plugs one into all four
ports, `sil set portN.fan=ID` changes a single port at runtime. `SIL_FAN_DIR`
points at a directory of your own fan files instead of the embedded library.

**Schema 2.** The shipped fans are schema 2 files (`schema = 2`) with the tables
`[stop]`, `[start]`, `[dynamics]`, `[curve.up]`, an optional `[curve.down]`, an
optional `[tach]` and `[datasheet]`, an optional `[measured]`, `[sources]` and
`[provenance]`; `fans/arctic-p12-pwm-pst.toml` is the worked example.

```toml
schema = 2
name = "My fan"
[sources]
vendor_page = "https://example.com/my-fan"      # plain text; sil never fetches it
[stop]
duty = 0.05                                     # below this duty a spinning fan stops (0 = never stops)
[start]
duty = 0.10                                     # a stopped fan needs at least this duty ...
delay_s = 0.4                                   # ... for this long before it spins
[dynamics]
tau_up_s = 1.0
tau_down_s = 2.5
coast_tau_s = 6.0                               # unpowered coast-down after a stop
[curve.up]
duty = [0.05, 0.50, 1.00]
rpm = [200, 900, 1800]
[tach]
ppr = 2
noise_rpm = 20.0
[provenance]
"stop.duty" = "datasheet:vendor_page"
"start.duty" = "assumed"
# ... one entry for every value above
```

- All keys of the tables you use are required, `coast_tau_s` included; there are
  no defaults. Curves are strictly increasing in duty and non-decreasing in rpm,
  2 to 64 points. `stop.duty = 0` with a curve point at duty 0 means the fan
  never stops.
- Every value has a provenance entry `kind` or `kind:source_id` under
  `[provenance]`. The kinds are `assumed`, `datasheet`, `measured` and
  `unobservable`. A curve table counts as one value. Sources are named ids in
  `[sources]` with plain text; `sil` never fetches them.
- What counts as `datasheet`: a value a vendor page states; a value derived
  arithmetically from such figures, with the derivation in a comment (the
  Arctic's 1.2 W is 0.1 A x 12 V); or a value from an official vendor datasheet
  PDF wherever it is hosted, with its URL in `[sources]`. Anything else is
  `assumed` until measured.
- `[measured]` identifies a measurement record: `date`, `board`, `esphome` and
  `recording_sha256` take no provenance entry; `measured.tolerance` is a value
  and needs one.
- `[tach]` makes the fan report pulses, not an ideal rpm: `ppr` pulses per
  revolution, and `noise_rpm`, the standard deviation in rpm of the noise in one
  1 s tach reading (the shipped fans use 20, two thirds of the 30 rpm step). The
  noise goes on the tach only; status `rpm` stays the noise-free shaft speed.
  Without `[tach]` the fan has an ideal tach.
- `[datasheet]` (`airflow_m3h`, `power_w`, `ref_rpm`) gives the airflow and power
  at `ref_rpm`; the simulator scales them with the fan laws (airflow with rpm,
  power with rpm cubed).
- Loading is strict: an unknown key is rejected with `file:line:col`, a missing
  key is named, a violated rule names its value.
- Flat schema 1 files (the v0.1.0 format) still load and behave exactly as
  before. `testdata/fans-v0.1.0` holds the v0.1.0 files byte for byte; the exact
  tests pin them through `SIL_FAN_DIR`.
- Which shipped values are assumed: everything except a few vendor figures. The
  start duty and delay, the time constants, the curves (the up curve's end points come
  from the vendor page or datasheet, so `curve.up` is `assumed:<source>`), the
  down curve, the tach `ppr` and `noise_rpm` are `assumed`; only the Arctic's
  stop duty and the `[datasheet]` airflow, power and reference rpm of both fans
  carry `datasheet`. The Noctua file stays assumed until v0.3. Phase 8 measures
  the Arctic against hardware; until then do not rely on absolute numbers.

**What the tools show.** `sil fans` prints JSON (per fan: `schema`, the
provenance counts, the per-value provenance map) and, on stderr, a summary per
fan; `--verbose` lists every value with its provenance and source. In
`sil status` every port carries:

| Field | Meaning |
|-------|---------|
| `rpm` | the shaft speed, noise free |
| `tach_rpm` | the 1 s gate reading of the tach: for a fan with `[tach]` the pulse count of the last second times 30 (at ppr 2), which is what the firmware's tach path reads; for an ideal tach the 1 s mean of the shaft. `null` only in a port's first second after sim start, sim restart or a fan swap |
| `airflow_m3h`, `power_w` | the fan laws on `rpm` from the fan's `[datasheet]`; `null` when the fan has no datasheet, never 0 |

`sil watch` has the event kinds `tach` and `load` (airflow and power) next to
`duty`, `rpm`, `state` and `fan`, and history samples carry `tach`. The page
shows the tach trace and the TACH RPM, AIRFLOW and POWER readouts, with `—` when
one of those has no value. `sil up --seed N` sets the seed of the tach noise
(default: a fixed seed, so runs repeat); `sil status` shows it as `sim.seed`.

## Home Assistant

The firmware of every instance is a normal ESPHome device with the native API
(encrypted, a stable per-instance key). You can add a running instance to your
own Home Assistant, on this machine or on any machine of the LAN.

1. `bin/sil up ha-a`. The JSON result and `sil status ha-a` carry an `api`
   object: `lan_host` (the IPv4 address of the interface that holds the default
   route, `null` when this host has none), `port` and `key`. `sil up` also
   prints the same hint on stderr.
2. In Home Assistant: Settings -> Devices & Services -> Add Integration ->
   ESPHome. Enter `host` = `lan_host`, `port` = `api.port`, and the encryption
   key `api.key` when asked. There is no mDNS on the host platform, so the
   device is not discovered automatically. When `lan_host` is `null` read the
   address of this machine yourself.
3. The device `SiL ha-a` appears with 13 entities: Fan 1..4, Fan 1..4 Speed,
   Temperature, Humidity and Button USR1..USR3. The status LEDs are internal to
   the firmware and show up in `sil status` (`leds`), not in Home Assistant.

What stays the same: the MAC address, device name, API key and API port of an
instance are stored under `$XDG_STATE_HOME/sil/<name>` (default
`~/.local/state/sil/<name>`). `sil down ha-a && sil up ha-a` therefore keeps the
same Home Assistant device, with no duplicate; Home Assistant reconnects by
itself. A second instance (`sil up ha-b`) has its own identity and shows up as a
separate device.

Remove a device: delete it on its Home Assistant device page (Delete). To start
over with a fresh identity, run `sil down NAME`, delete
`$XDG_STATE_HOME/sil/NAME` and delete the device in Home Assistant first, then
`sil up NAME` again.

**Security note.** The firmware's native API listens on `0.0.0.0:<port>`, so it
is reachable from the LAN. Every connection needs the instance key (Noise
encryption is mandatory and checked at build time), and `sil status` is only
served over a private (`0600`) Unix socket. On shared networks and on CI runners
firewall the API port anyway.

## License

GPL-3.0, same as the WiFi Fan Controller.
