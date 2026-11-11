"""Spike S7b: the firmware-visible tach steps (roadmap criterion 3, decision D-02).

A schema 2 fan with a pulse tach (flat 1100 rpm, ppr 2, no noise) is put on port 3. The firmware's
`Fan 3 Speed` sensor (a 1 s time-weighted mean of the sim-bus rpm frames) must show the 30 rpm pulse
steps, not one constant value and not a smear.

Reading `Fan 3 Speed` through aioesphomeapi is a RECORDED EXCEPTION to the project rule that plant
observation goes through `sil`: the firmware-visible value criterion 3 is about (what Home Assistant
sees) exists only behind the native API, after sim_sensor's 1 s window; `sil status` tach_rpm is the
sim-side gate reading and cannot show it. The plant itself is observed through `sil status` alongside,
and the fan command (`api_main.fan`) is the allowed "commandable" use.
"""

import statistics
import threading
import time

WINDOW_S = 25
RPM = 1100.0


def _sample_status(bench, stop, out, errors):
    """`sil status` once a second: ports[2].tach_rpm and sim.bus_proto."""
    while not stop.is_set():
        try:
            st = bench.status("ct-main")
            out.append((st["ports"][2]["tach_rpm"], st["sim"]["bus_proto"]))
        except BaseException as exc:  # pytest.fail raises a BaseException subclass
            errors.append(exc)
            return
        stop.wait(1.0)


def test_firmware_sees_tach_steps(bench):
    # test_identity restarts ct-main (sil down / sil up), which drops the session `api_main` connection,
    # and this file runs after it: a fresh connection to the same instance, same key.
    api_main = bench.api("ct-main")
    bench.sil("set", "port3.fan=s7-steady", "-i", "ct-main")
    try:
        api_main.fan(3, 50)
        bench.wait(
            "ct-main",
            lambda s: s["ports"][2]["fan"] == "s7-steady"
            and abs(s["ports"][2]["rpm"] - RPM) < 1
            and s["ports"][2]["tach_rpm"] is not None,
            15,
            "port 3 s7-steady at 1100 rpm with a tach reading",
        )
        time.sleep(3)  # the firmware window now holds steady frames only

        stop = threading.Event()
        samples, errors = [], []
        sampler = threading.Thread(target=_sample_status, args=(bench, stop, samples, errors), daemon=True)
        sampler.start()
        try:
            values = api_main.collect("Fan 3 Speed", WINDOW_S)
        finally:
            stop.set()
            sampler.join(30)
        assert not errors, f"sil status sampling failed: {errors[0]!r}"

        mean = statistics.fmean(values) if values else float("nan")
        spread = (max(values) - min(values)) if values else float("nan")
        levels = len({round(v, 3) for v in values})
        low = sum(1 for v in values if v < mean)
        high = len(values) - low
        minority = min(low, high) / len(values) if values else 0.0
        on_grid = sum(1 for v in values if abs(v - 30 * round(v / 30)) <= 0.01) / len(values) if values else 0.0
        print(
            f"\nS7b: n={len(values)} mean={mean:.2f} spread={spread:.2f} levels={levels} "
            f"minority_share={minority:.3f} on_grid={on_grid:.3f} status_samples={len(samples)}"
        )

        assert len(values) >= 20, f"only {len(values)} Fan 3 Speed readings in {WINDOW_S} s"
        # (a) no bias, (b) bounded spread, (c) not one constant level
        assert abs(mean - RPM) <= 5, f"(a) mean {mean:.2f} rpm is not within 5 rpm of {RPM:g}"
        assert spread <= 90, f"(b) peak-to-peak spread {spread:.1f} rpm exceeds 90 rpm"
        assert levels >= 2, f"(c) a single level {sorted({round(v, 3) for v in values})}: the pulse steps are not visible"
        # (d) the step signature of criterion 3: the readings toggle between levels about one 30 rpm step apart
        assert spread >= 12, f"(d) peak-to-peak {spread:.1f} rpm below 12 rpm: no step signature"
        assert low >= 0.25 * len(values) and high >= 0.25 * len(values), (
            f"(d) toggle shares below/at-or-above the mean are {low}/{high} of {len(values)}, each must be >= 25 %"
        )

        # the plant, observed through sil: the gate reading is a whole multiple of 30, bus protocol unchanged
        assert samples, "no sil status samples were taken"
        tachs = {t for t, _ in samples}
        assert tachs <= {1080.0, 1110.0}, f"status tach_rpm took {sorted(tachs, key=str)}, expected only 1080 or 1110"
        assert {p for _, p in samples} == {1}, "sim.bus_proto is not 1"
    finally:
        api_main.fan(3, on=False)
        bench.sil("set", "port3.fan=arctic-p12-pwm-pst", "-i", "ct-main")
