---
phase: "02"
slug: "full-rev-3-3-bench"
status: secured
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
block_on: high
created: "2026-11-09"
audited_at: cae6922
---

# Phase 02 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| control client → api.sock (press, down, up, set, fans, watch, logs) | Unix socket 0600 in a 0700 run dir; CLI refuses a non-private runtime base | button ids, hold_ms, set values, fan ids, log source/tail |
| bus peer (firmware or same-user peer) → simulator engine | LED `px` frames and `chan` manifests (`n`, `seg`) are untrusted input | pixel hex, segment lists, channel manifests |
| simulator → firmware (bus.sock) | button levels and env values as frames | f32 values |
| firmware YAML → codegen | config untrusted until validated | LED count, partition ranges, labels |
| Go codec ↔ C++ codec | the two sides must not drift | NDJSON frames |
| control client / CLI flag → fan library | a fan id names a file in the library | fan id |
| control client → long-lived streams (watch, logs --follow) | streams hold server resources while the client wants | event and log lines |
| run dir files → control client | log content leaves the supervisor | firmware.log, sil.log lines (build.log never served) |
| PyPI → tests/.venv | third-party Python packages enter the developer's machine | wheels, sdists |
| test state root → local users | persistent test identities (API keys) | identity.json |
| firmware native API (0.0.0.0:port) → LAN | the address `sil` prints invites LAN clients | Noise-encrypted HA API traffic |
| status/up output → user | `lan_host` and the key appear together | API key (by design) |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation (evidence) | Status |
|-----------|----------|-----------|----------|-------------|------------------------|--------|
| T-02-01 | Denial of service | press handler | low | mitigate | `MaxHold` 60 s, `PressRequest.Validate` 1..60000, 64 KiB body cap, `DisallowUnknownFields`, re-validated in `Press`; `ErrButtonBusy` (control.go:154-170,393-412; supervisor.go:876,892,903; engine.go:771) | closed |
| T-02-02 | Tampering | button id | low | mitigate | `checkButton` against `engine.ButtonIDs` before any engine call; `url.PathEscape` client side (supervisor.go:856-864,889,932; control.go:682,698) | closed |
| T-02-03 | Spoofing | api.sock | low | accept | AR-02-01 | closed |
| T-02-SC/02-01 | Tampering | Go deps | low | mitigate | go.mod/go.sum unchanged since Phase 1 (09c47ef); `go mod verify` ok | closed |
| T-02-04 | Denial of service | LED frames | medium | mitigate | firmware dedupe + 50 ms gap (sim_light.cpp:47-52); 4096-byte line cap; 1024-frame send queue; `px` ≤ 1530 chars; deduped, capped warnings (engine.go:73,539-557,983-987) | closed |
| T-02-05 | Tampering | LED manifest | low | mitigate | `n` 1..255, `parseSegments` label regex / duplicates / bounds, strict hex length checks; prior strip kept on failure (engine.go:910-1000) | closed |
| T-02-06 | Information disclosure | LED state | low | accept | AR-02-02 | closed |
| T-02-SC/02-02 | Tampering | deps | low | mitigate | no dependency change | closed |
| T-02-07 | Tampering | `sil set` values | medium | mitigate | `decimalRE`, `parseNumber` rejects NaN/Inf/hex, fixed ranges, `unknown_key` allow-list; encoder drops non-finite (supervisor.go:956-1078; codec.go:57) | closed |
| T-02-08 | Denial of service | set handler | low | mitigate | 64 KiB body cap, `DisallowUnknownFields`, null `values` rejected; single `Apply`; no `opMu` (control.go:423-434; supervisor.go:1121,1150) | closed |
| T-02-09 | Tampering | input/engine consistency | low | mitigate | `inMu` held across record+Apply, initial start, and SimRestart read+start+install (supervisor.go:128-129,236-245,777-806,1132-1133) | closed |
| T-02-SC/02-03 | Tampering | deps | low | mitigate | no dependency change | closed |
| T-02-10 | Tampering | light codegen | low | mitigate | `num_leds` 1..255, partition range, label charset, duplicates, seg ≤ 2048 B in `FINAL_VALIDATE_SCHEMA` (light.py:26-41,112-138) | closed |
| T-02-11 | Tampering | codec drift | low | mitigate | 8 new rows in `protocol/vectors/frames.tsv` read by both codecs; `make test-cpp` in `check` (119 passed) | closed |
| T-02-SC/02-04 | Tampering | deps | low | mitigate | no dependency change | closed |
| T-02-12 | Tampering / Info disclosure | fan id → file | medium | mitigate | id must be in `lib.IDs()` before `Load` (set and `sil up --fan`); `idPattern`, `..` rejected; files only from `fs.FS` (supervisor.go:1093-1101,1180-1186; lifecycle.go:197-250; fanlib.go:41-45,90-93,124) | closed |
| T-02-13 | Denial of service | fan file size | low | mitigate | 64 KiB `LimitReader` cap on every load (fanlib.go:37,128-135) | closed |
| T-02-SC/02-05 | Tampering | deps | low | mitigate | no dependency change | closed |
| T-02-14 | Denial of service | event broker | medium | mitigate | 1024-event per-subscriber buffer, non-blocking publish with `lagged` marker, 16-subscriber cap shared with logs --follow → `too_many_streams` (events.go:16-18,97-153; watch.go:21-34) | closed |
| T-02-15 | Denial of service | streams on shutdown | medium | mitigate | broker closed and `stopped` closed before server cancel; follower ends with stopped line; forced `srv.Close()` after 2 s (supervisor.go:268-271,441-443; logs.go:285-317; control.go:579-583) | closed |
| T-02-16 | Information disclosure | `sil logs` | **high** | mitigate | sources limited to firmware.log/sil.log enum (build.log never served); every line passes `clean()` key redaction before the length cut; no partial first line served (logs.go:109-152,171,231-296; control.go:275); tests green | closed |
| T-02-17 | Tampering | logs params | low | mitigate | tail 1..10000 via `Atoi`, source enum, raw/follow ∈ {0,1}; no request string reaches a path (control.go:275-283,462-480) | closed |
| T-02-SC/02-06 | Tampering | deps | low | mitigate | stdlib/internal imports only; go.mod/go.sum unchanged | closed |
| T-02-SC/02-07 | Tampering | Python test deps | **high** | mitigate | exact `==` pins + transitive `constraint-dependencies` (tests/pyproject.toml:7-56); `lockdiff.py` requires identical version and sha256 set vs `../wifi-fancontroller/hil/uv.lock`, fails closed (exit 1/2); `test-contract: build lock-check` with `uv run --locked` (Makefile:56-64); gate run live: 20 checked, 0 missing/mismatched | closed |
| T-02-18 | Information disclosure | test state root | low | mitigate | 0700 state/runtime dirs, teardown rmtree in fixture `finally`; identity.json 0600 (silbench.py:82-110,213; conftest.py:31-32; instance.go:233,300) | closed |
| T-02-19 | Spoofing / Tampering | test instances on LAN | low | accept | AR-02-03 | closed |
| T-02-20 | Spoofing / Tampering | native API on LAN | medium | mitigate | lint rejects missing `api.encryption.key` (`lint_rejected`); example uses `!env_var SIL_API_KEY`; README firewall note (lint.go:120-122,178-190; fwbuild.go:149; rev33.yaml:37-38; README.md:178-184) | closed |
| T-02-21 | Information disclosure | key in status/up | low | accept | AR-02-04 | closed |
| T-02-22 | Information disclosure | lan_host probe | low | accept | AR-02-05 | closed |
| T-02-SC/02-08 | Tampering | deps | low | mitigate | go.mod/go.sum unchanged; `make lock-check` exit 0 | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-02-01 | T-02-03 (low) | Phase 1 api.sock boundary unchanged: 0600 socket in a 0700 run dir, CLI refuses a non-private runtime base; only the user's own processes reach it | plan 02-01 threat model; premise verified by gsd-security-auditor | 2026-11-09 |
| AR-02-02 | T-02-06 (low) | LED state is internal by product design (FW-02); only in `sil status` over the 0600 socket; all sim light/partition entities `internal: true` | plan 02-02; verified | 2026-11-09 |
| AR-02-03 | T-02-19 (low) | Test instances' native API is LAN-reachable but needs the per-instance Noise PSK; instances live only for the run and stop in finalizers; CI runners firewall the port | plan 02-07; verified | 2026-11-09 |
| AR-02-04 | T-02-21 (low) | Key already in status/up JSON since Phase 1; served only on 0600 api.sock; stderr hint omits the key; a LAN address is not secret on a local bench | plan 02-08; verified | 2026-11-09 |
| AR-02-05 | T-02-22 (low) | UDP connect to RFC 5737 `192.0.2.1:9` sends no packet and reveals nothing off-host | plan 02-08; verified | 2026-11-09 |

*Accepted risks do not resurface in future audit runs.*

---

## Residual Notes (non-blocking)

- **T-02-SC/02-07 trust anchor:** the reference lock is a sibling-repo file (`../wifi-fancontroller/hil/uv.lock`); the gate fails closed if it is missing. The human legitimacy review of the Python packages is still pending (STATE.md Needs Human).
- **T-02-04 bus-peer bypass:** the firmware-side dedupe/rate cap can be bypassed by a non-conforming same-user bus peer; cost is bounded by the line cap, broker buffers, the 16-stream cap and the single active bus connection. Inside the Phase 1 T-01-21 accepted boundary.
- **Carried from Phase 1:** `sim_bus` `path:` defaults to `/tmp/sil.sock` (always overridden by `SIL_BUS`); code-review IN-02 adds that a `SIL_BUS` value bypasses the 107-byte check.
- **Code-review info items IN-01..IN-09** (02-REVIEW-DISPOSITION.md): none weaken a Phase 2 mitigation. IN-03 (trailing data after a JSON body) does not defeat the T-02-01/T-02-08 caps.
- **Review-fix commits ce2c756..b976a6c** re-checked: no mitigation regressed.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-09 | 30 | 30 | 0 | gsd-security-auditor (ASVS L1, block_on high) at cae6922 |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [ ] Maintainer sign-off (pending; see STATE.md Needs Human)
