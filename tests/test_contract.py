"""TEST-01: the closed loop on a Rev 3.3 bench, driven and observed only through `sil` (D-14, D-18).

button -> firmware -> duty -> simulated fan -> RPM -> firmware -> LED. Every stimulus is a `sil`
call; every observation is `sil status` or the `sil watch` stream, so each step shows the firmware's
reaction (duty from its out frames, pixels it wrote), never only the value the simulator set.
"""

from silbench import blue_dominant, duties, green_dominant, pixel

NAME = "ct-main"
# target RPM at duty 0.4 of fans/arctic-p12-pwm-pst.toml: min_rpm + (duty - min_duty) / (1 - min_duty) * (max - min)
RPM_AT_040 = 200 + (0.4 - 0.05) / 0.95 * 1600  # 789.5
RPM_TOL = 0.02


def all_duty(want):
    return lambda s: duties(s) == [want] * 4


def test_closed_loop_chain(bench):
    # 1. boot: the board LED pulsed blue before any API client connected. The pulse dims, so any one
    #    of the samples taken since boot is enough.
    assert bench.boot_board, "no board LED sample was taken before the contract chain"
    assert any(blue_dominant(c) for c in bench.boot_board), (
        f"no blue-dominant board sample during boot (the pulse); samples: {sorted(set(bench.boot_board))}"
    )

    # 2. a firmware restart gives a known start: restore_mode ALWAYS_OFF -> every duty 0
    bench.sil("fw", "restart", NAME)
    st = bench.wait(
        NAME,
        lambda s: s["phase"] == "ready" and s["firmware"]["bus_connected"] and duties(s) == [0] * 4,
        20,
        "firmware back on the bus with every duty 0 after fw restart",
    )

    # 3. a watch on the instance starts with a snapshot carrying every status key
    w = bench.watch(NAME)
    snap = w.next(lambda line: True, 5)
    assert snap.get("k") == "snapshot", f"first watch line is not a snapshot: {snap.get('k')!r}"
    missing = (set(st) | {"k", "t", "seq"}) - set(snap)
    assert not missing, f"snapshot lacks status keys {sorted(missing)}"

    # 4. USR1 press -> every duty 0.1 within 2 s; the watch saw the button go down and up, then port 1's duty
    bench.sil("press", "usr1", "-i", NAME)
    bench.wait(NAME, all_duty(0.1), 2, "every duty 0.1 after one USR1 press")
    w.next(lambda e: e.get("k") == "button" and e.get("id") == "usr1" and e.get("v") is True, 3)
    w.next(lambda e: e.get("k") == "button" and e.get("id") == "usr1" and e.get("v") is False, 3)
    w.next(lambda e: e.get("k") == "duty" and e.get("port") == 1 and e.get("v") == 0.1, 3)

    # 5. three more presses, each awaited -> every duty 0.4 (Fan 1 is the only one asserted beyond duty:
    #    the first press lands exactly on start_duty 0.10 for the others, Pitfall 12)
    for want in (0.2, 0.3, 0.4):
        bench.sil("press", "usr1", "-i", NAME)
        bench.wait(NAME, all_duty(want), 2, f"every duty {want} after another USR1 press")
    bench.wait(
        NAME,
        lambda s: abs(s["ports"][0]["rpm"] - RPM_AT_040) <= RPM_AT_040 * RPM_TOL,
        15,
        f"Fan 1 rpm within 2 % of {RPM_AT_040} (duty 0.4)",
    )
    # the firmware lights the fan 1 LED green once it reads a turning fan (dominant channel, Pitfall 5)
    bench.wait(NAME, lambda s: green_dominant(pixel(s, "fan1")), 6, "fan 1 LED green")

    # 6. a long USR2 press -> every fan to 100 %, USR3 steps down to 90 %
    bench.sil("press", "usr2", "--long", "-i", NAME)
    bench.wait(NAME, all_duty(1.0), 3, "every duty 1.0 after a long USR2 press")
    bench.sil("press", "usr3", "-i", NAME)
    bench.wait(NAME, all_duty(0.9), 3, "every duty 0.9 after USR3")

    # 7. swapping the fan on port 2 changes its plant: the Noctua spins above 1700 rpm at 90 %
    bench.sil("set", "port2.fan=noctua-nf-a12x25-pwm", "-i", NAME)
    st = bench.wait(
        NAME,
        lambda s: s["ports"][1]["fan"] == "noctua-nf-a12x25-pwm" and s["ports"][1]["rpm"] > 1700,
        8,
        "port 2 on the Noctua fan above 1700 rpm",
    )
    w.next(lambda e: e.get("k") == "fan" and e.get("port") == 2 and e.get("v") == "noctua-nf-a12x25-pwm", 3)
    assert st["ports"][0]["fan"] == "arctic-p12-pwm-pst", "the fan swap changed port 1 as well"
