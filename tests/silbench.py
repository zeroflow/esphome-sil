"""Helpers for the contract and smoke tests.

Everything about the simulator goes through the `sil` binary (one JSON value per call, NDJSON for
`watch`); the firmware's native API is reached directly with aioesphomeapi, the same library Home
Assistant uses. The tests never skip: a missing prerequisite fails with its reason.
"""

import asyncio
import json
import os
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import warnings
from pathlib import Path

import aioesphomeapi
import pytest
from aioesphomeapi.client import APIClient

REPO = Path(__file__).resolve().parent.parent
# Instance names reserved for the tests (they share the developer's build cache dir by name).
RESERVED = ("ct-main", "ct-second")
CONFIG = REPO / "examples" / "rev33.yaml"

SIL_TIMEOUT_S = 180
API_TIMEOUT_S = 15

# sil variables that would pull a call towards some other instance or key.
_DROP_ENV = ("SIL_INSTANCE", "SIL_API_KEY", "SIL_FAN_DIR")


def _rgb(hexcolor):
    h = hexcolor.lstrip("#")
    return int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16)


def blue_dominant(hexcolor):
    """Pure blue above the noise floor: red 0, green 0, blue >= 0x20 (Pitfall 5: the pulse dims)."""
    r, g, b = _rgb(hexcolor)
    return r == 0 and g == 0 and b >= 0x20


def green_dominant(hexcolor):
    """Pure green above the noise floor (static green at 50 % is #002500, gamma corrected)."""
    r, g, b = _rgb(hexcolor)
    return r == 0 and b == 0 and g >= 0x20


def pixel(status, segment):
    """The first pixel (hex string) of a named LED segment of a status/snapshot object."""
    leds = status["leds"]
    return leds["strip"][leds["segments"][segment][0]]


def duties(status):
    return [p["duty"] for p in status["ports"]]


def free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("0.0.0.0", 0))
        return s.getsockname()[1]


def _last(text, n=600):
    text = text if isinstance(text, str) else json.dumps(text)
    return text if len(text) <= n else text[:n] + "..."


def state_root():
    base = os.environ.get("SIL_CONTRACT_STATE")
    if base:
        return Path(base)
    cache = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(cache) / "sil-contract" / "state"


def _private_dir(path):
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.chmod(0o700)


class Bench:
    """A private sil environment: persistent state root, fresh short runtime dir, real XDG cache."""

    def __init__(self, sil_bin, state, runtime, env):
        self.sil_bin = sil_bin
        self.state = state
        self.runtime = runtime
        self.env = env
        self.boot_board = []
        self._apis = []
        self._watchers = []
        self._started = set()

    @classmethod
    def create(cls):
        sil_bin = Path(os.environ.get("SIL_BIN") or REPO / "bin" / "sil")
        if not sil_bin.is_file() or not os.access(sil_bin, os.X_OK):
            pytest.fail(f"sil binary not found or not executable: {sil_bin} (run `make build`, or set SIL_BIN)", pytrace=False)
        if not CONFIG.is_file():
            pytest.fail(f"bench firmware config missing: {CONFIG}", pytrace=False)
        state = state_root()
        _private_dir(state)
        runtime = Path(tempfile.mkdtemp(prefix="sil-ct-", dir="/tmp"))
        runtime.chmod(0o700)
        env = {k: v for k, v in os.environ.items() if k not in _DROP_ENV}
        env.update(
            SIL_REPO=str(REPO),
            XDG_STATE_HOME=str(state),
            XDG_RUNTIME_DIR=str(runtime),
        )
        return cls(sil_bin, state, runtime, env)

    # -- sil CLI ---------------------------------------------------------------------------------

    def sil(self, *args, expect_exit: int | None = 0):
        """Run sil; stdout must be exactly one JSON value. Returns (obj, exit_code)."""
        try:
            proc = subprocess.run(
                [str(self.sil_bin), *args],
                env=self.env,
                capture_output=True,
                text=True,
                timeout=SIL_TIMEOUT_S,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            pytest.fail(f"sil {' '.join(args)} could not run: {exc}", pytrace=False)
        dec = json.JSONDecoder()
        out = proc.stdout.strip()
        try:
            obj, end = dec.raw_decode(out)
            if out[end:].strip():
                raise ValueError("more than one JSON value")
        except ValueError as exc:
            pytest.fail(
                f"sil {' '.join(args)} (exit {proc.returncode}) did not print exactly one JSON value ({exc})\n"
                f"stdout: {_last(proc.stdout)}\nstderr: {_last(proc.stderr)}",
                pytrace=False,
            )
        if expect_exit is not None and proc.returncode != expect_exit:
            pytest.fail(
                f"sil {' '.join(args)}: exit {proc.returncode}, expected {expect_exit}: {_last(obj)}\n"
                f"stderr: {_last(proc.stderr)}",
                pytrace=False,
            )
        return obj, proc.returncode

    def up(self, name):
        args = ["up", name, "--config", str(CONFIG), "--quiet"]
        obj, code = self.sil(*args, expect_exit=None)
        err = obj.get("error") if isinstance(obj, dict) else None
        if err and err.get("code") == "port_in_use":
            port = free_port()
            warnings.warn(f"sil up {name}: sticky API port is in use, retrying with --api-port {port}")
            obj, code = self.sil(*args, "--api-port", str(port), expect_exit=None)
        if code != 0:
            pytest.fail(f"sil up {name} failed (exit {code}): {_last(obj)}", pytrace=False)
        self._started.add(name)
        return obj

    def down(self, name):
        # not_running (exit 1) or an already stopped instance is fine here
        return self.sil("down", name, expect_exit=None)[0]

    def status(self, name):
        return self.sil("status", name)[0]

    def wait(self, name, pred, timeout=5.0, what="condition"):
        """Poll `sil status` every 0.1 s until pred(status) is truthy; fail with the last status."""
        deadline = time.monotonic() + timeout
        st = None
        while True:
            st = self.status(name)
            if pred(st):
                return st
            if time.monotonic() >= deadline:
                pytest.fail(
                    f"timed out after {timeout:g} s waiting for {what} on {name}; last status: "
                    f"phase={st.get('phase')} duties={duties(st)} rpm={[round(p['rpm'], 1) for p in st['ports']]} "
                    f"buttons={st.get('buttons')} strip={st['leds']['strip']}",
                    pytrace=False,
                )
            time.sleep(0.1)

    def watch(self, name, *args):
        w = Watcher(self, name, args)
        self._watchers.append(w)
        return w

    def api(self, name):
        st = self.status(name)
        a = Api(st["api"]["port"], st["api"]["key"])
        self._apis.append(a)
        return a

    # -- teardown --------------------------------------------------------------------------------

    def teardown(self):
        for w in self._watchers:
            w.close()
        for a in self._apis:
            a.close()
        for name in RESERVED:
            try:
                self.down(name)
            except BaseException:  # a finalizer must not mask the test result
                pass
        shutil.rmtree(self.runtime, ignore_errors=True)


class Watcher:
    """`sil watch` as a subprocess; a reader thread parses its NDJSON lines."""

    def __init__(self, bench, name, args):
        self._err = tempfile.TemporaryFile("w+")
        self._proc = subprocess.Popen(
            [str(bench.sil_bin), "watch", "-i", name, *args],
            env=bench.env,
            stdout=subprocess.PIPE,
            stderr=self._err,
            text=True,
        )
        self._lines = []
        self._cursor = 0
        self._cond = threading.Condition()
        self._eof = False
        self._thread = threading.Thread(target=self._read, daemon=True)
        self._thread.start()

    def _read(self):
        stdout = self._proc.stdout
        if stdout is None:  # never: Popen was started with stdout=PIPE
            stdout = iter(())
        for raw in stdout:
            raw = raw.strip()
            if not raw:
                continue
            try:
                obj = json.loads(raw)
            except ValueError:
                obj = {"k": "_unparsable", "raw": raw}
            with self._cond:
                self._lines.append(obj)
                self._cond.notify_all()
        with self._cond:
            self._eof = True
            self._cond.notify_all()

    def next(self, pred=lambda line: True, timeout=5.0):
        """The first not yet consumed line matching pred (consumes everything up to it)."""
        deadline = time.monotonic() + timeout
        with self._cond:
            while True:
                while self._cursor < len(self._lines):
                    line = self._lines[self._cursor]
                    self._cursor += 1
                    if pred(line):
                        return line
                if self._eof:
                    pytest.fail(f"sil watch ended before a matching line arrived; stderr: {self._stderr()}", pytrace=False)
                left = deadline - time.monotonic()
                if left <= 0:
                    pytest.fail(
                        f"no matching sil watch line within {timeout:g} s; "
                        f"{len(self._lines) - self._cursor} unmatched lines were skipped",
                        pytrace=False,
                    )
                self._cond.wait(left)

    def lines(self):
        with self._cond:
            return list(self._lines)

    def _stderr(self):
        try:
            self._err.seek(0)
            return _last(self._err.read())
        except (OSError, ValueError):
            return ""

    def close(self):
        if self._proc.poll() is None:
            self._proc.terminate()
            try:
                self._proc.wait(5)
            except subprocess.TimeoutExpired:
                self._proc.kill()
                self._proc.wait()
        self._thread.join(5)
        try:
            self._err.close()
        except OSError:
            pass


class Api:
    """aioesphomeapi on a private event loop thread, with a latest-state map keyed by entity name."""

    def __init__(self, port, key, host="127.0.0.1"):
        self._loop = asyncio.new_event_loop()
        self._thread = threading.Thread(target=self._loop.run_forever, daemon=True)
        self._thread.start()
        self._cond = threading.Condition()
        self._states = {}
        self._cli: APIClient | None = None
        self._closed = False
        self._entities = []
        self._by_name = {}
        self._names = {}
        try:
            self._call(self._connect(host, port, key), API_TIMEOUT_S * 3)
        except BaseException:
            self.close()
            raise

    def _call(self, coro, timeout=API_TIMEOUT_S):
        fut = asyncio.run_coroutine_threadsafe(coro, self._loop)
        try:
            return fut.result(timeout)
        except Exception as exc:  # concurrent.futures.TimeoutError, API errors, ...
            fut.cancel()
            pytest.fail(f"native API call failed: {type(exc).__name__}: {exc}", pytrace=False)

    async def _connect(self, host, port, key):
        cli = APIClient(host, port, None, noise_psk=key, client_info="sil-contract")
        await asyncio.wait_for(cli.connect(login=True), API_TIMEOUT_S)
        self._cli = cli
        self._info = await asyncio.wait_for(cli.device_info(), API_TIMEOUT_S)
        ents, _ = await asyncio.wait_for(cli.list_entities_services(), API_TIMEOUT_S)
        self._entities = list(ents)
        self._by_name = {e.name: e for e in ents}
        self._names = {e.key: e.name for e in ents}
        cli.subscribe_states(self._on_state)

    def _on_state(self, st):
        name = self._names.get(st.key)
        if name is None:
            return
        if isinstance(st, aioesphomeapi.FanState):
            value = {"state": bool(st.state), "speed_level": st.speed_level}
        elif getattr(st, "missing_state", False):
            value = None
        else:
            value = st.state
        with self._cond:
            self._states[name] = value
            self._cond.notify_all()

    def device_info(self):
        return {"name": self._info.name, "mac_address": self._info.mac_address, "esphome_version": self._info.esphome_version}

    def entities(self):
        """One dict per entity: name, key, object_id, type (aioesphomeapi info class name)."""
        return [
            {"name": e.name, "key": e.key, "object_id": e.object_id, "type": type(e).__name__}
            for e in self._entities
        ]

    def fan(self, n, speed=None, on=True):
        """HA-style fan command: speed_level 0..100 (speed_count 100); on=False switches the fan off."""
        ent = self._by_name.get(f"Fan {n}")
        if ent is None:
            pytest.fail(f"no entity named 'Fan {n}' on the native API", pytrace=False)

        cli = self._cli
        if cli is None:
            pytest.fail("native API client is not connected", pytrace=False)

        async def send():
            if on:
                cli.fan_command(ent.key, state=True, speed_level=speed)
            else:
                cli.fan_command(ent.key, state=False)

        self._call(send())

    def state(self, name):
        with self._cond:
            return self._states.get(name)

    def wait_state(self, name, pred, timeout=5.0):
        """Wait until pred(latest value of the entity) holds; fail with the last value."""
        deadline = time.monotonic() + timeout
        with self._cond:
            while True:
                value = self._states.get(name)
                if name in self._states and pred(value):
                    return value
                left = deadline - time.monotonic()
                if left <= 0:
                    pytest.fail(f"timed out after {timeout:g} s waiting on state of {name!r}; last value: {value!r}", pytrace=False)
                self._cond.wait(min(left, 0.2))

    def close(self):
        """Disconnect; tolerates a connection the firmware already dropped."""
        if self._closed:
            return
        self._closed = True
        cli = self._cli
        if cli is not None and self._loop.is_running():
            try:
                asyncio.run_coroutine_threadsafe(cli.disconnect(), self._loop).result(5)
            except BaseException:
                pass
        if self._loop.is_running():
            self._loop.call_soon_threadsafe(self._loop.stop)
        self._thread.join(5)
        if not self._thread.is_alive():
            self._loop.close()
