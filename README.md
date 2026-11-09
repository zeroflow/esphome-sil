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

**Status:** Phase 1 (closed-loop fan skeleton) and Phase 2 (full Rev 3.3 bench)
are implemented. The CLI (`sil`) is the primary interface and is usable by
developers and coding agents alike (one JSON value on stdout per command,
progress and logs on stderr). The web UI, scenarios and the time factor are
later phases. See [docs/initial-idea.md](docs/initial-idea.md) for the idea
behind it.

Test pyramid this fits into: host SiL (this repo) -> hardware-in-the-loop rig
with tach/PWM loopback -> HIL with real fans.

## Requirements

- Linux (the ESPHome `host:` platform does not build elsewhere)
- docker with the pinned image `ghcr.io/esphome/esphome:2026.10.1` (builds the
  firmware; the first build needs network once to fill a local tool cache)
- Go 1.27.3 to build `sil` (a single static binary)
- [uv](https://docs.astral.sh/uv/) for the contract tests

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
