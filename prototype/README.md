# sim_bus prototype (research spike, 2026-11-08)

Throwaway spike from project research, kept as a starting skeleton for
Phase 1. It is **not** the final component layout or protocol: Phase 1 moves
the real components to `components/` at the repo root (so
`github://zeroflow/esphome-sil@vX.Y` works) and replaces the peer scripts with
the Go simulator.

What it proved against `ghcr.io/esphome/esphome:2026.10.1`:

- `esphome: build_flags: [-static]` gives a statically linked `host:` binary
  that runs on a Debian 12 host (the dynamic one needs GLIBC 2.38).
- One `sim_bus` hub plus platforms: float output (stock `fan: speed` on top),
  sensor, binary sensor, addressable light (stock `light: partition` on top).
- Round trip: fan command from aioesphomeapi arrives on the bus as
  `{"k":"out","ch":"fan1_pwm","v":0.4}`; values sent by the peer
  (`{"k":"in",...}`) show up as API states.

## Layout

| Path | What |
|---|---|
| `components/sim_bus/` | Hub (non-blocking Unix socket client, flat NDJSON) and platforms |
| `sim.yaml` | Minimal host config using the components |
| `tools/fake_sim.py` | Python peer: listens on the socket, prints firmware frames, sends a few inputs |
| `tools/api_client.py` | aioesphomeapi client: lists entities, sends a fan command, prints states |

## Try it

```bash
cd prototype
docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -v "$PWD:/config" \
  ghcr.io/esphome/esphome:2026.10.1 compile sim.yaml
python3 tools/fake_sim.py &          # creates /tmp/sil-proto.sock
ESPHOME_PREFDIR=$(mktemp -d) .esphome/build/sil-proto/.pioenvs/sil-proto/program &
uv run --with aioesphomeapi tools/api_client.py
```

## Known gaps (to fix in Phase 1/2, see `.planning/research/`)

- No HELLO manifest / full state snapshot on reconnect beyond outputs re-sending
  their last value; no protocol version check on the Go side.
- `SimSensor` is a plain `Component`, not a `PollingComponent` (no
  `update_interval`, no 1 s RPM mean).
- `SimLight` sends a frame on every `write_state` (observed 66 identical frames
  in 5 s idle): needs change detection and a rate cap.
- Socket path is silently truncated at the 107-byte `sun_path` limit; codegen
  must validate it. Default `/tmp` path is not per-instance.
- `send_text` does not escape strings; the hand-written parser assumes flat,
  well-formed objects.
- No `reboot_timeout: 0s`, no API encryption, constant default MAC, setup
  priority `HARDWARE` instead of `BUS`.
