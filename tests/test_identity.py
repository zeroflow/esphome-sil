"""HA-01 proxy (D-15): the device identity Home Assistant keys on is stable across restarts, and a
second instance is a separate device. The live Home Assistant check stays a manual UAT item."""

import time

from silbench import duties


def identity(bench, api):
    st = bench.status("ct-main")
    info = api.device_info()
    return {
        "mac": st["api"]["mac"],
        "key": st["api"]["key"],
        "port": st["api"]["port"],
        "device_name": st["api"]["device_name"],
        "info_name": info["name"],
        "info_mac": info["mac_address"].lower(),
        "entities": {e["name"]: (e["key"], e["object_id"]) for e in api.entities()},
    }


def test_identity_stable_across_restart(bench):
    before = identity(bench, bench.api("ct-main"))
    assert before["info_mac"] == before["mac"].lower()
    assert before["entities"], "the native API listed no entities before the restart"

    bench.down("ct-main")
    bench.up("ct-main")

    after = identity(bench, bench.api("ct-main"))  # a fresh connection with the same key
    assert after == before, "device identity changed across sil down / sil up: " + str(
        {k: (before[k], after[k]) for k in before if before[k] != after[k]}
    )


def test_second_instance_is_separate(bench):
    main_api = bench.api("ct-main")
    main = bench.status("ct-main")
    bench.up("ct-second")
    try:
        second = bench.status("ct-second")
        second_api = bench.api("ct-second")
        assert second["api"]["device_name"] == "sil-ct-second"
        assert second_api.device_info()["name"] == "sil-ct-second"
        for field in ("mac", "port", "key", "device_name"):
            assert second["api"][field] != main["api"][field], f"ct-second shares {field} with ct-main"
        assert main_api.device_info()["name"] == "sil-ct-main", "both connections must work at the same time"

        # a press on ct-second moves only ct-second
        main_duties = duties(main)
        bench.sil("press", "usr1", "-i", "ct-second")
        bench.wait("ct-second", lambda s: duties(s) == [0.1] * 4, 3, "ct-second duties 0.1 after its USR1 press")
        end = time.monotonic() + 1.0  # observation window for an absence: ct-main must stay put
        while time.monotonic() < end:
            assert duties(bench.status("ct-main")) == main_duties, "a press on ct-second changed ct-main"
            time.sleep(0.1)
    finally:
        bench.down("ct-second")
