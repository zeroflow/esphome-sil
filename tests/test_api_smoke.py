"""TEST-02: what Home Assistant sees of the firmware, through the same library (aioesphomeapi).

The native API is used only to check that entities are visible and commandable. Every effect is
confirmed against `sil status`, so the simulator's reading of the firmware's output is compared with
the entity state the firmware publishes.
"""

from silbench import duties

# The 13 entities of the product HIL contract (name -> object id).
EXPECTED = {
    "Button USR1": "button_usr1",
    "Button USR2": "button_usr2",
    "Button USR3": "button_usr3",
    "Fan 1": "fan_1",
    "Fan 2": "fan_2",
    "Fan 3": "fan_3",
    "Fan 4": "fan_4",
    "Fan 1 Speed": "fan_1_speed",
    "Fan 2 Speed": "fan_2_speed",
    "Fan 3 Speed": "fan_3_speed",
    "Fan 4 Speed": "fan_4_speed",
    "Temperature": "temperature",
    "Humidity": "humidity",
}

RPM_TOL = 0.02  # +-2 % of the expected RPM
ENV_TOL = 0.05  # temperature / humidity


def close(value, want, rel=0.0, absolute=0.0):
    return value is not None and abs(value - want) <= max(abs(want) * rel, absolute)


def test_entity_set(bench, api_main):
    st = bench.status("ct-main")
    info = api_main.device_info()
    assert info["name"] == "sil-ct-main", f"device name {info['name']!r}"
    assert info["mac_address"].lower() == st["api"]["mac"].lower(), (
        f"device MAC {info['mac_address']} differs from sil status {st['api']['mac']}"
    )

    ents = api_main.entities()
    assert ents, "the native API listed no entities at all (empty entity list)"
    names = [e["name"] for e in ents]
    assert len(names) == len(set(names)), f"duplicate entity names: {sorted(names)}"
    # Set equality, order not asserted: neither a missing nor an extra entity is acceptable.
    assert set(names) == set(EXPECTED), (
        f"entity set differs; missing {sorted(set(EXPECTED) - set(names))}, extra {sorted(set(names) - set(EXPECTED))}"
    )
    assert len(ents) == 13
    assert not [e for e in ents if e["type"] == "LightInfo"], "a light entity is visible to Home Assistant"
    for e in ents:
        assert e["object_id"] == EXPECTED[e["name"]], f"{e['name']}: object id {e['object_id']!r}"


def test_fan_round_trip(bench, api_main):
    # speed 40 -> duty exactly 0.4, and the fan's RPM sensor rises
    api_main.fan(1, 40)
    bench.wait("ct-main", lambda s: s["ports"][0]["duty"] == 0.4, 3, "Fan 1 duty exactly 0.4")
    api_main.wait_state("Fan 1 Speed", lambda v: v is not None and v > 0, 8)

    # adjacent values back to back: the last command wins
    api_main.fan(1, 40)
    api_main.fan(1, 41)
    st = bench.wait("ct-main", lambda s: s["ports"][0]["duty"] == 0.41, 3, "Fan 1 duty exactly 0.41 (last wins)")
    assert st["ports"][0]["duty"] == 0.41

    # boundaries: full speed and off
    api_main.fan(1, 100)
    bench.wait("ct-main", lambda s: s["ports"][0]["duty"] == 1.0, 3, "Fan 1 duty exactly 1.0")
    api_main.fan(1, on=False)
    bench.wait("ct-main", lambda s: s["ports"][0]["duty"] == 0, 3, "Fan 1 duty 0 after switching off")
    assert duties(bench.status("ct-main"))[0] == 0


def test_inputs_visible_over_api(bench, api_main):
    # the RPM entity follows the fan command: 40 % of the shipped fan settles at 789.5 rpm
    api_main.fan(1, 40)
    want = 200 + (0.4 - 0.05) / 0.95 * 1600
    api_main.wait_state("Fan 1 Speed", lambda v: close(v, want, rel=RPM_TOL), 15)

    # temperature and humidity set through sil arrive at the firmware and are published
    bench.sil("set", "temperature=24.5", "humidity=40", "-i", "ct-main")
    api_main.wait_state("Temperature", lambda v: close(v, 24.5, absolute=ENV_TOL), 3)
    api_main.wait_state("Humidity", lambda v: close(v, 40, absolute=ENV_TOL), 3)

    # a button level held through sil is the entity state (press and release inside the click window)
    bench.sil("press", "usr2", "--down", "-i", "ct-main")
    api_main.wait_state("Button USR2", lambda v: v is True, 3)
    bench.sil("press", "usr2", "--up", "-i", "ct-main")
    api_main.wait_state("Button USR2", lambda v: v is False, 3)
    api_main.fan(1, on=False)
