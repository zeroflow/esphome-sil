"""Test client for the firmware's encrypted native API.

Runs inside the pinned ESPHome image (aioesphomeapi ships there):

    python3 api_client.py --port P info
    python3 api_client.py --port P set-fan --fan N --speed S
    python3 api_client.py --port P watch --seconds S

The Noise key comes from the environment variable SIL_API_KEY. Prints one JSON
object on stdout. Connection or key failure prints {"error": ...} and exits 3.
"""

import argparse
import asyncio
import json
import math
import os
import sys
import time

import aioesphomeapi


def _value(state):
    """JSON-friendly value of an entity state."""
    if isinstance(state, aioesphomeapi.FanState):
        return {"state": bool(state.state), "speed_level": state.speed_level}
    v = getattr(state, "state", None)
    if isinstance(v, float) and (math.isnan(v) or getattr(state, "missing_state", False)):
        return None
    return v


async def _connect(port):
    cli = aioesphomeapi.APIClient(
        "127.0.0.1", port, None, noise_psk=os.environ["SIL_API_KEY"]
    )
    await asyncio.wait_for(cli.connect(login=True), timeout=15)
    return cli


async def _entities(cli):
    ents, _ = await cli.list_entities_services()
    return ents, {e.key: e.name for e in ents}


async def cmd_info(cli, _args):
    info = await cli.device_info()
    ents, names = await _entities(cli)
    states = {}

    def on_state(st):
        states[names.get(st.key, str(st.key))] = _value(st)

    cli.subscribe_states(on_state)
    await asyncio.sleep(1.5)
    return {
        "device_name": info.name,
        "mac_address": info.mac_address,
        "esphome_version": info.esphome_version,
        "entities": sorted(names.values()),
        "states": states,
    }


async def cmd_set_fan(cli, args):
    ents, names = await _entities(cli)
    want = f"Fan {args.fan}"
    fan = next((e for e in ents if e.name == want), None)
    if fan is None:
        return {"error": f"no entity named {want!r}"}, 4
    cli.fan_command(fan.key, state=args.speed > 0, speed_level=args.speed)
    await asyncio.sleep(0.5)  # let the frame leave before we disconnect
    return {"ok": True, "fan": args.fan, "speed": args.speed}


async def cmd_watch(cli, args):
    ents, names = await _entities(cli)
    series = {}
    t0 = time.monotonic()

    def on_state(st):
        name = names.get(st.key, str(st.key))
        series.setdefault(name, []).append([round(time.monotonic() - t0, 3), _value(st)])

    cli.subscribe_states(on_state)
    await asyncio.sleep(args.seconds)
    return {"series": series}


async def main(argv):
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, required=True)
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("info")
    sf = sub.add_parser("set-fan")
    sf.add_argument("--fan", type=int, required=True)
    sf.add_argument("--speed", type=int, required=True)
    w = sub.add_parser("watch")
    w.add_argument("--seconds", type=float, required=True)
    args = ap.parse_args(argv)

    try:
        cli = await _connect(args.port)
    except Exception as exc:  # connection refused, bad key, timeout
        print(json.dumps({"error": f"{type(exc).__name__}: {exc}"}))
        return 3
    try:
        result = await {"info": cmd_info, "set-fan": cmd_set_fan, "watch": cmd_watch}[args.cmd](cli, args)
    finally:
        await cli.disconnect()
    code = 0
    if isinstance(result, tuple):
        result, code = result
    print(json.dumps(result))
    return code


if __name__ == "__main__":
    sys.exit(asyncio.run(main(sys.argv[1:])))
