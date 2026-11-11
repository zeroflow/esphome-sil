---
phase: "04"
slug: "fan-model-v2"
status: secured
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
block_on: high
created: "2026-11-11"
audited_at: 2c38f54
---

# Phase 04 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Fan file → loader | Human- or agent-edited `fan.toml` (schema 1 or 2) from the fan dir or `SIL_FAN_DIR` is parsed by `internal/fanlib` | Untrusted TOML text (numbers, curves, sources, provenance), capped at 64 KiB |
| CLI flag → lifecycle | `sil up --seed N` and `--fan ID` | Integer seed, fan id (path component) |
| Engine → sim-bus / events | Tach frames and tach/load events fan out to the firmware and to watch/WS clients | Change-driven numeric frames and events |
| Control API → LAN page | Status, watch and fans rows (tach_rpm, airflow, power, provenance, sources) over the unauthenticated LAN listener (Phase 3 D-08) | Non-secret simulation state; `api.key` stays redacted |
| Test harness → firmware native API | e2e and contract benches (`e2e-libv2`, `ct-main`) | Per-instance Noise key, per-instance port |

No new listener, endpoint, auth path or third-party API is added in this phase (see COVERAGE.md).

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-04-01 | Tampering / DoS | internal/fanlib/v2.go validateV2 | high | mitigate | NaN/Inf rejected in every number incl. curve arrays, range checks; `TestV2CurveShapeRules` | closed |
| T-04-02 | DoS | internal/model/curve.go Curve.At | medium | mitigate | ≥ 2 points, strictly increasing duty (no zero-width division), At clamps and never extrapolates | closed |
| T-04-03 | Tampering | user-edited schema 2 file | medium | mitigate | `rawV2` decoded with `DisallowUnknownFields` (v2.go:134), provenance has no default; IN-03 fix adds the missing-`schema` hint | closed |
| T-04-04 | Tampering (integrity) | testdata/fans-v0.1.0 | medium | mitigate | `TestV010FixturesArePinned` (sha256), `TestV010FixturesLoadAsV010` | closed |
| T-04-05 | DoS | internal/model/fan.go advance (huge delay/tau) | low | accept | AR-04-01 | closed |
| T-04-06 | DoS | internal/fanlib/v2.go (arrays, sources, strings) | medium | mitigate | 64 KiB cap, 64 points/curve, 32 sources, 512-byte texts, 128-byte name, 64-byte provenance; `TestV2Caps` | closed |
| T-04-07 | Information disclosure / Spoofing | FanInfo.sources in `sil fans` and the LAN fans row | low | mitigate | Sources are plain text, never fetched or linked, not rendered on the page; `TestV2LoadNeverFetchesASource`, `TestFansNeverDereferencesSources`; WR-02 fix rejects control characters and quotes them in `sil fans` | closed |
| T-04-08 | Repudiation | provenance of a value | medium | mitigate | Required entry per present value, known kinds and defined ids only; schema 1 reports null; `TestV2ProvenanceRules`, `TestV2Empty` | closed |
| T-04-09 | Tampering | fan id in Load (path traversal) | low | accept | AR-04-02 | closed |
| T-04-10 | Tampering | internal/lifecycle/lifecycle.go checkSeed | low | mitigate | Seed capped at 2^53-1 (`maxSeed`, lifecycle.go:675) with invalid_argument before any write; kong parses uint64; `TestCheckSeed`, e2e `TestUpSeed/refused` | closed |
| T-04-11 | DoS | engine → bus (tach frames) | low | mitigate | 50 ms cadence with change-only rule (`rpmFrameStep`, engine.go:31) | closed |
| T-04-12 | Information disclosure | internal/model/noise.go | low | accept | AR-04-03 | closed |
| T-04-13 | DoS | internal/engine/engine.go emitTach/emitLoad | low | mitigate | Tach at most once per sim second per port, load only on change at the 1 s sample; `TestEngineTachEvents`, `TestEngineLoadEvents` | closed |
| T-04-14 | Repudiation (transparency) | status/watch airflow, power, tach_rpm | medium | mitigate | Pointer fields with explicit null, never 0 for unknown; `TestStatusCarriesTachAndLoad`, `TestEngineLoadEvents` | closed |
| T-04-15 | Tampering (XSS) | ui/web/js/view.js readouts | low | mitigate | Readouts via `textContent`; no `innerHTML` in ui/web/js; sources never rendered | closed |
| T-04-16 | Information disclosure / locality | ui/web/index.html, plot.js | low | mitigate | No new external reference; vendor-hash and external-reference tests (`TestNoVendorMarks`, `TestNoCSPViolations`); uPlot stays vendored 1.6.32 | closed |
| T-04-17 | Repudiation (transparency) | fans/*.toml [provenance] | medium | mitigate | `TestShippedProvenanceMatchesEvidence` pins the exact path → kind:source map; UAT test 2 confirmed plausibility | closed |
| T-04-18 | Tampering (integrity) | exact-RPM suites after the switch | medium | mitigate | TestMain / e2e default / Python bench pins at the hash-pinned v0.1.0 fixtures | closed |
| T-04-19 | Tampering | tests/silbench.py session fan dir | low | mitigate | Session dir inside the 0700 runtime dir, removed at teardown; caller's `SIL_FAN_DIR` dropped (`_DROP_ENV`, silbench.py:33) | closed |
| T-04-20 | Information disclosure | native API of ct-main | low | accept | AR-04-04 | closed |
| T-04-21 | Information disclosure | native API of the e2e-libv2 instance | low | accept | AR-04-04 | closed |
| T-04-SC | Tampering | npm/pip/cargo/go module installs | low | accept | AR-04-05 | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-04-01 | T-04-05 (low) | `time.Duration` arithmetic cannot overflow in a realistic run; the loader caps `delay_s` at 60 s and taus at 600 s | plan (04-02 threat model) | 2026-11-11 |
| AR-04-02 | T-04-09 (low) | Unchanged `idPattern` and `..` check in `fanlib.Load`; `TestLoadRejectsUnsafeIDs` still passes | plan (04-03 threat model) | 2026-11-11 |
| AR-04-03 | T-04-12 (low) | The keyed hash is reproducible test noise, not security randomness; no secret involved | plan (04-04 threat model) | 2026-11-11 |
| AR-04-04 | T-04-20, T-04-21 (low) | Same exposure as every existing e2e/contract instance: encrypted native API, per-instance key and port, torn down by test cleanup; no new listener | plan (04-09, 04-10 threat models) | 2026-11-11 |
| AR-04-05 | T-04-SC (low) | No package installed, upgraded or vendored in this phase; go-toml v2.4.3 already pinned; `make test-contract` keeps `lock-check` | plan (all 04 threat models) | 2026-11-11 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-11 | 22 | 22 | 0 | /gsd-secure-phase orchestrator, L1 grep-depth short-circuit (register authored at plan time, ASVS L1, block_on high) at 2c38f54; mitigation tests re-run green after review fixes 6ebeb3f..62091f8 |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: secured` set in frontmatter

**Approval:** verified 2026-11-11
