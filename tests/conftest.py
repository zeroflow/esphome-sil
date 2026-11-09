"""Session fixtures: one isolated Rev 3.3 bench instance (ct-main) shared by the test modules."""

import time

import pytest

from silbench import Bench, pixel

_T0 = time.monotonic()

BOOT_SAMPLE_S = 3.0


def pytest_terminal_summary(terminalreporter):
    elapsed = time.monotonic() - _T0
    terminalreporter.write_line(f"contract session: {elapsed:.1f} s (budget < 60 s warm)")


@pytest.fixture(scope="session")
def bench():
    b = Bench.create()
    try:
        b.up("ct-main")
        # Board pixel samples taken before any API client exists: sil's own readiness probe does
        # not count, so the boot pulse is still running (D-17).
        deadline = time.monotonic() + BOOT_SAMPLE_S
        while time.monotonic() < deadline:
            b.boot_board.append(pixel(b.status("ct-main"), "board"))
            time.sleep(0.1)
        yield b
    finally:
        b.teardown()


@pytest.fixture(scope="session")
def api_main(bench):
    return bench.api("ct-main")
