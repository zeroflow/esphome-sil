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

**Status:** Phase 1 (closed-loop fan skeleton), Phase 2 (full Rev 3.3 bench) and
Phase 3 (live web UI) are implemented. The CLI (`sil`) is the primary interface and is usable by
developers and coding agents alike (one JSON value on stdout per command,
progress and logs on stderr). Phase 3 adds a live web UI (see
[Web UI](#web-ui)); scenarios and the time factor are later phases. See [docs/initial-idea.md](docs/initial-idea.md) for the idea
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
bin/sil up dev                     # builds examples/rev33.yaml (first build is slow, later ones are cached)
bin/sil status dev                 # JSON: phase, api, ports, env, buttons, leds, firmware, sim
bin/sil set temperature=27 humidity=60 -i dev   # inputs; also port2.fan=noctua-nf-a12x25-pwm
bin/sil press usr1 -i dev          # 200 ms press; --long holds 1500 ms, --down / --up set the level
bin/sil press usr1 --down --max-hold 10s -i dev # held; the simulator releases it by itself after 10 s of sim time
bin/sil watch --kind duty,rpm --count 5 -i dev  # NDJSON: one snapshot, then one line per change
bin/sil history --window 60s -i dev # the last 60 s of duty and RPM per port, one sample every 100 ms
bin/sil logs --source all --tail 50 -i dev      # firmware and simulator logs, colour removed, key redacted
bin/sil fans -i dev                # the fan library and what is plugged into each port
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
  last 5 minutes of `sample` events, one per 100 ms of sim time with a duty and
  an RPM per port (null while the firmware is not on the sim-bus). The first
  sample after a simulator restart carries `"gap": true`.
- `sample` is an opt-in `sil watch` kind (`sil watch --kind sample`): ten events
  a second, so a default watch never carries it.
- `sim.behind_real_time` in `sil status` is true while the simulator handles its
  ticks 50 ms or more late; `sil watch` shows the changes as `sim` events with
  the value `behind` or `in_sync` (back in sync after 2 s of sim time without a
  late tick).
- `sil press BUTTON --down --max-hold D` (`1ms` to `60s`) releases the button by
  itself after D of sim time with a `button` event `{"v": false, "reason":
  "max_hold"}`, so a client that goes away cannot leave a button held.

## Web UI

Every running instance serves a live page. `sil up` prints the address
(`web UI: http://<lan-ip>:<port>/`) and `sil status` carries it as `web.url`;
open it in a browser on this machine, on another machine of the LAN or on a
phone. The page shows the four fan ports (RPM, duty and a rolling scope with the
last minute), the five status LEDs, sim time, time factor, the firmware link and
whether the simulator is behind real time. It also drives the bench: choose the
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

`tests/` holds two pytest suites that drive real bench instances through the
`sil` CLI and `aioesphomeapi`: a closed-loop contract test (the Rev 3.3 chain
from Home Assistant style fan commands to RPM, buttons, LEDs and inputs) and a
smoke test that the entities are visible and commandable.

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
Constants marked `ASSUMED` in both files (start duty, time constants, the
Noctua minimum duty and below-minimum behaviour) are not measured; replace them
by datasheet or measured values before relying on absolute numbers.

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
