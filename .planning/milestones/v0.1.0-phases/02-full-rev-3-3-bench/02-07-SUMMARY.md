---
phase: 02-full-rev-3-3-bench
plan: 07
subsystem: testing
tags: [pytest, uv, aioesphomeapi, contract-test, lockfile-gate, rev33]
requires:
  - phase: 02-full-rev-3-3-bench
    provides: "sil CLI (up/status/set/press/watch/fw restart/down), examples/rev33.yaml, fan library with the Noctua fan (02-01..02-06)"
provides:
  - "tests/ uv project sil-contract, lock-pinned and hash-identical to the product HIL lock"
  - "make lock-check (version + sha256-set gate) and make test-contract (one command for TEST-01 and TEST-02)"
  - "bench fixture: isolated Rev 3.3 instances driven only through the sil CLI"
  - "TEST-02 smoke test, TEST-01 closed-loop chain, HA-01 identity proxy, FW-01 concurrency"
affects: [02-08, phase-3-web-ui, product-repo-consumers]
actuals:
  tokens: 8500
  tasks: 2
  commits: 2
plan_head_before: 7145c2ab9f63cdce7745b7cf42f82529a5f7efe9
plan_head_after: ce5193a7c38bc0b6bfca804cc41f209189277d95
tech-stack:
  added: [uv-managed pytest project, aioesphomeapi 46.12.0, pytest 9.2.0, pytest-timeout 2.4.0]
  patterns: ["deadline polling against sil status instead of sleeps", "dominant-channel LED colour checks", "lock hash diff against the product's reviewed lock before any install"]
key-files:
  created:
    - tests/pyproject.toml
    - tests/uv.lock
    - tests/tools/lockdiff.py
    - tests/conftest.py
    - tests/silbench.py
    - tests/test_api_smoke.py
    - tests/test_contract.py
    - tests/test_identity.py
  modified:
    - Makefile
key-decisions:
  - "test-contract depends on lock-check (security review): no install or run without the hash diff against the product lock"
  - "Test state root persists under ~/.cache/sil-contract/state (0700) so builds and identities stay warm; runtime dir is a fresh /tmp/sil-ct-* 0700 dir"
  - "A missing prerequisite (sil binary, docker, image) is a fixture error with the reason, never a skip"
requirements-completed: [TEST-01, TEST-02, HA-01, HA-02, FW-01]
coverage:
  - id: D1
    description: "make lock-check: every package of tests/uv.lock has the same version and sdist/wheel sha256 set as ../wifi-fancontroller/hil/uv.lock"
    requirement: "TEST-02"
    verification:
      - kind: other
        ref: "make lock-check (checked 20, missing [], mismatched [])"
        status: pass
    human_judgment: true
    rationale: "Maintainer review of the Python test dependencies (SUS from the legitimacy seam) is pending; the hash identity is the automated evidence until then"
  - id: D2
    description: "TEST-02: exact 13-entity HA contract set, device name and MAC, no light entity, fan command round trip with boundaries and adjacency, RPM/temperature/humidity/button over the native API"
    requirement: "TEST-02"
    verification:
      - kind: integration
        ref: "tests/test_api_smoke.py (3 tests) via make test-contract"
        status: pass
    human_judgment: false
  - id: D3
    description: "TEST-01: button -> duty -> RPM -> LED chain plus fan swap, driven and observed only through sil"
    requirement: "TEST-01"
    verification:
      - kind: e2e
        ref: "tests/test_contract.py#test_closed_loop_chain via make test-contract"
        status: pass
    human_judgment: false
  - id: D4
    description: "HA-01 proxy: identity stable across sil down/up, second instance is a separate device; FW-01 two instances isolated"
    requirement: "HA-01"
    verification:
      - kind: integration
        ref: "tests/test_identity.py (2 tests) via make test-contract"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live Home Assistant behaviour (add by host:port + key, control fans, see entities, restart -> same device, second instance -> separate device)"
    requirement: "HA-02"
    verification: []
    human_judgment: true
    rationale: "No Home Assistant instance in this run; aioesphomeapi is a proxy only. Manual UAT at end-of-phase verification."
duration: 26min
completed: 2026-11-09
status: complete
---

# Phase 2 Plan 07: Contract and Smoke Tests Summary

**One command (`make test-contract`) proves the whole Rev 3.3 loop in about 23 s warm: a hash-gated, lock-pinned pytest project drives isolated `sil` instances through the CLI for the TEST-01 button-duty-RPM-LED chain and checks the HA-visible entity set and fan round trip through aioesphomeapi.**

## Performance

- **Duration:** 26 min
- **Started:** 2026-11-09T07:55:00Z (approx.)
- **Completed:** 2026-11-09T08:22:00Z
- **Tasks:** 2
- **Files modified:** 9 (8 created, Makefile)

## Accomplishments

- `tests/` is a uv project (`sil-contract`), `package = false`, Linux-only environments, direct pins `aioesphomeapi==46.12.0`, `pytest==9.2.0`, `pytest-timeout==2.4.0`, and `constraint-dependencies` generated from every non-project package of the product HIL lock. `tests/uv.lock` is committed; every run uses `uv run --locked`.
- `make lock-check` (`tests/tools/lockdiff.py`, stdlib `tomllib`, run with `python3 -I`) ran and passed before the first `uv sync`. Verified it fails: a tampered hash gives exit 1 with `mismatched`, an unreadable input gives exit 2.
- TEST-02 (`test_api_smoke.py`): set equality of the 13 entities, object ids, no `LightInfo`, device name `sil-ct-main` and the MAC from `sil status`, empty list fails with its own message; Fan 1 speed 40 -> duty exactly 0.4 and `Fan 1 Speed` rises; 40 then 41 back to back ends at 0.41; 100 -> 1.0; off -> 0; settled RPM within 2 % of 789.5; `sil set temperature=24.5 humidity=40` arrives within 0.05; `Button USR2` follows `--down` / `--up`.
- TEST-01 (`test_contract.py`): one ordered chain with no API connection (D-18): blue-dominant boot sample taken before any API client, `fw restart` -> all duties 0, watch snapshot carries every status key, USR1 -> 0.1 with button true/false and port 1 duty events, three more presses -> 0.4, Fan 1 RPM within 2 % of 789.5 and fan1 LED green, long USR2 -> 1.0, USR3 -> 0.9, `set port2.fan=noctua-nf-a12x25-pwm` -> rpm above 1700 with a fan event.
- HA-01 proxy and FW-01 (`test_identity.py`): MAC, key, port, device name and every entity key/object id identical across `sil down`/`sil up`, with a fresh connection on the same key; `ct-second` has a different MAC, port, key and name `sil-ct-second`, both connections work at once, and a press on `ct-second` leaves `ct-main` untouched.

### Lockdiff report (`make lock-check`)

```json
{"checked": 20, "missing": [], "mismatched": []}
```

### Wall times and versions

| Run | Contract session |
|-----|------------------|
| Task 1 smoke only, first (cold ct-main build) | 16.8 s |
| Task 1 smoke only, warm | 9.6 s |
| 6 tests, first (cold ct-second build) | 29.6 s |
| 6 tests, warm run 1 | 22.5 s |
| 6 tests, warm run 2 (the D-16 number) | 22.2 s |
| 6 tests, final run with lock-check dependency | 23.2 s |

All below the 60 s warm budget. uv 0.10.2, CPython 3.13.12 (uv-managed; `requires-python >=3.12`, the product uses 3.12), pytest 9.2.0, aioesphomeapi 46.12.0. After every run `pgrep -f 'sil serv[e] ct-'` finds nothing.

## Task Commits

1. **Task 1: Tracer for the test layer** - `a870a5b` (test). Tracer feedback gate: `make lock-check && make test-contract` re-run end-to-end, `3 passed` twice (auto-verified, then expanded).
2. **Task 2: TEST-01 chain and HA-01 identity proxy** - `ce5193a` (test)

**Plan metadata:** committed with this SUMMARY (docs: complete plan)

## Files Created/Modified

- `tests/pyproject.toml` - uv project, exact pins, product-lock constraints, pytest config (timeout 120 s, signal method)
- `tests/uv.lock` - committed lock, hash-identical to the product HIL lock for all 20 packages
- `tests/tools/lockdiff.py` - version + sha256-set comparison (exit 0 / 1 / 2)
- `tests/silbench.py` - `Bench` (sil CLI wrapper, wait, watch), `Watcher`, `Api` on a background loop, colour helpers, `RESERVED`
- `tests/conftest.py` - session `bench` and `api_main` fixtures, wall-time summary line
- `tests/test_api_smoke.py`, `tests/test_contract.py`, `tests/test_identity.py` - the six tests
- `Makefile` - `LOCK_REF`, `lock-check`, `test-contract: build lock-check`

## Decisions Made

- The state root persists under `${XDG_CACHE_HOME:-~/.cache}/sil-contract/state` (override `SIL_CONTRACT_STATE`), created 0700; only the leaf directory is chmod-ed so an override pointing at a shared parent is never changed.
- `ct-main` / `ct-second` are reserved test names; `sil up` retries once with a free `--api-port` plus a pytest warning only on `port_in_use` (never triggered in these runs).
- `Watcher.next` consumes lines in order, so the chain asserts button true, button false, duty in the order the engine emits them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `test-contract` depends on `lock-check`**
- **Found during:** Task 2 (coordinator forwarded a security-review finding)
- **Issue:** the plan specified `test-contract: build`, so `uv run --locked` could install dependencies without the hash diff that stands in for the human package review (fail-open for T-02-SC).
- **Fix:** `test-contract: build lock-check`.
- **Files modified:** Makefile
- **Verification:** `make test-contract` prints the lockdiff report first and still reports `6 passed`.
- **Committed in:** `ce5193a`

**2. [Rule 1 - Bug] Optional typing errors in tests/silbench.py**
- **Found during:** Task 2 (Pyright, forwarded by the coordinator)
- **Issue:** `expect_exit` was inferred `int` but called with `None`; `Popen.stdout` and the API client are Optional.
- **Fix:** `expect_exit: int | None = 0`, guards for stdout and the client, `APIClient` imported from `aioesphomeapi.client` (the package root does not export it for Pyright).
- **Verification:** `pyright --pythonpath tests/.venv/bin/python` reports 0 errors; tests still pass.
- **Committed in:** `ce5193a`

---

**Total deviations:** 2 auto-fixed (1 missing critical, 1 bug)
**Impact on plan:** both are tightening only; no scope change. REQUIREMENTS.md: HA-01 and HA-02 were intentionally NOT marked complete, because the plan's prohibition forbids reporting them as verified against a live Home Assistant on the proxy alone. TEST-01, TEST-02 and FW-01 are marked.

## Issues Encountered

None. All six tests passed on the first full run; missing-prerequisite behaviour was checked by hand (`SIL_BIN=/nonexistent/sil` gives a setup ERROR with the path, not a skip).

## Needs Human

- Maintainer review of the Python test dependencies (SUS from the legitimacy seam); the evidence until then is the `make lock-check` hash identity above.
- Live Home Assistant UAT (criterion 4, HA-01/HA-02): add the instance by host:port + key, control fans, see RPM/temperature/humidity/buttons, restart keeps the device, second instance is a separate device. Not performed by these tests.
- Unclassified flagged assumptions EDGE-TEST-01-unclassified and EDGE-HA-02-unclassified, and A-02-07, remain open for review.

## User Setup Required

None - no external service configuration required (needs uv, docker and the pinned image already present).

## Known Stubs

None.

## Threat Flags

None. The test instances' native API listens on the LAN only for the run (T-02-19, accepted); the state root is 0700 and the runtime dir is a fresh 0700 temp dir removed at the end (T-02-18).

## Next Phase Readiness

Ready for 02-08. `make test-contract` is the criterion-5 gate; the live HA checklist stays a manual end-of-phase item.

## Self-Check: PASSED

All created files exist, commits `a870a5b` and `ce5193a` exist, `make lock-check` exits 0, `make test-contract` reports `6 passed`, no skip/xfail in tests/*.py, `grep -v '^ *#' tests/test_contract.py | grep -c 'Api('` prints 0, and no `sil serve ct-` process remains.

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*
