# esphome-sil

Software-in-the-loop test bench for ESPHome firmware, in the spirit of an
automotive labcar. The ESPHome `host:` platform runs the real firmware modules
on Linux; simulated IO components connect them to a plant simulator, so control
loops can be tested closed-loop, locally and fast, before firmware reaches real
hardware.

First domain: the [WiFi Fan Controller](https://github.com/zeroflow/wifi-fancontroller)
(fan closed loop, status LEDs, buttons, later fault models and a rack thermal
model).

Planned shape:

- `sim_*` ESPHome external components (PWM out, fan speed in, LED out, button in,
  temperature) talking to a simulator process over a local socket
- CLI and JSON API as the primary interface (`sil up`, `sil run scenario.toml`),
  usable by developers and coding agents alike
- a simple local web UI on top of the same API
- a fan library with measured parameter sets and fault profiles
- configurable time factor per run (x1 for normal tests, x100+ for thermal and
  long-run tests)

Everything runs on your own machine; no cloud simulator involved.

**Status:** idea stage. See [docs/initial-idea.md](docs/initial-idea.md).

Test pyramid this fits into: host SiL (this repo) → hardware-in-the-loop rig
with tach/PWM loopback → HIL with real fans.

## License

GPL-3.0, same as the WiFi Fan Controller.
