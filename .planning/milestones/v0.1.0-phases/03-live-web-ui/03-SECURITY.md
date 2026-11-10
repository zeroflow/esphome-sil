---
phase: "03"
slug: "live-web-ui"
status: secured
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
block_on: high
created: "2026-11-10"
audited_at: 09024c3
---

# Phase 03 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| LAN host → web listener (0.0.0.0:web_port) | Unauthenticated by D-08; page, `/api/v1/*`, `/ws` | HTTP requests, partial/slow requests, WS upgrades |
| web listener → `control.Control` | through the `redacting` wrapper, `NewLANHandler` and the stream budget | status, set, press, history, fans, watch |
| other web pages in the developer's browser → web listener | cross-site POST, cross-site WS, DNS rebinding | Origin, Sec-Fetch-Site, Host |
| `api.sock` (0600 in a 0700 run dir) | unchanged; full key and every operation | api.key, down, restarts, logs |
| browser → page assets | only code served by the `sil` binary (or `--ui-dir` for development) | HTML, JS, CSS |
| API strings → DOM | instance name, fan ids, segment labels, error text, hex | text |
| page input → `POST /api/v1/set`, button down/up | the server is the only validator | typed values, `max_hold_ms` |
| control client → history and down handlers | `window_s`, `max_hold_ms` are untrusted numbers | numbers |
| engine → broker → subscribers | 10 Hz samples; stream slots shared between browsers and the CLI | events |
| CLI flags → identity.json / spec.json | `--listen`, `--web-port`, `--allow-host`, `--ui-dir` | addresses, ports, names, a directory |
| Go proxy + sum DB → `go.sum` | `github.com/coder/websocket` v1.8.15 | module hashes |
| npm registry → `ui/web/vendor` | uPlot 1.6.32, vendored once, never installed | 3 files |
| test harness → local Chrome | `cdptest` DevTools endpoint during `make test-ui` / e2e | CDP |
| docs → LAN users | README states what the bench exposes | text |

---

## Threat Register

Evidence: file:line references are in the audit verdict recorded with this file's commit (gsd-security-auditor at 09024c3). Summary per row:

| Threat ID | Category | Component | Severity | Disposition | Mitigation (evidence) | Status |
|-----------|----------|-----------|----------|-------------|------------------------|--------|
| T-03-01 | Info disclosure | api.key on the web listener | **high** | mitigate | `redacting` wrapper (web.go) on Status, SimRestart, FwRestart, both Watch snapshot shapes, `/ws` and LAN watch; web_test + e2e `status_redacted`/`ws_snapshot` | closed |
| T-03-02 | Elevation of privilege | down / sim restart / fw restart over LAN | medium | mitigate | `LAN: false` rows; `NewLANHandler` → `refuseOperation` 403 `operation_not_allowed`; 0 Control calls proven | closed |
| T-03-03 | Spoofing | unauthenticated LAN API and page | medium | accept | AR-03-01 (D-08) | closed |
| T-03-04 | DoS | `/ws` streams | medium | mitigate | `acquireStream` cap 16, bounded per-subscriber queues | closed |
| T-03-05 | Info disclosure | static handler | low | mitigate | `FileServerFS` over embedded FS, GET/HEAD only (405 else) | closed |
| T-03-06 | Tampering | port selection | low | mitigate | sticky `AllocateWebPort`, `ListenWeb` → `port_in_use`, never moves | closed |
| T-03-07 | Elevation of privilege | routes outside the policy | medium | mitigate | both handlers register only `control.Operations` rows; TestOperationsTable "no route outside the table" | closed |
| T-03-08 | Tampering | UI drifting from CLI | low | mitigate | TestJSOpsInOperations, TestSnapshotEqualsStatus, TestParity* | closed |
| T-03-09 | Info disclosure | logs from LAN | low | mitigate | `logs` `LAN: false` (D-20); log lines key-redacted (T-02-16) | closed |
| T-03-10 | Tampering | external references | low | mitigate | TestNoExternalRefs; index.html references only same-origin files | closed |
| T-03-11 | Tampering | CSRF on set / buttons | **high** | mitigate | `http.NewCrossOriginProtection` in `web.Guard` (JSON `origin_rejected`); TestCSRF, TestCSRFCoversEveryMutatingRoute, e2e `csrf`; no CORS headers, no bypass | closed |
| T-03-12 | Spoofing | cross-site WS hijack of `/ws` | **high** | mitigate | `websocket.Accept(w, r, nil)` default origin check; no `InsecureSkipVerify`/`OriginPatterns`; TestWebSocketOrigin | closed |
| T-03-13 | Spoofing | DNS rebinding | **high** | mitigate | Host allowlist first in `web.Guard` (IP literals, localhost, hostname, `.local`, `--allow-host`) → 403 `host_rejected`; TestHostGuard, e2e `host_guard` | closed |
| T-03-14 | DoS | slow header / connection exhaustion | medium | mitigate | `web.Limit`: 64-conn cap, 10 s injected-clock Accept deadline; keep-alives off (pinned to Go 1.27.3 net/http behaviour) | closed |
| T-03-14b | DoS | trickled request body | medium | mitigate | `web.LimitBody` 10 s body deadline; 64 KiB body caps; TestStalledBodyIsClosed | closed |
| T-03-15 | DoS | zombie browser streams | medium | mitigate | 15 s ping / 10 s pong reap; 8-of-16 browser budget shared by `/ws` and LAN `GET /api/v1/watch` (review fix 75a92ed) | closed |
| T-03-16 | Tampering | clickjacking / injection | medium | mitigate | CSP `default-src 'none'; script-src 'self'; style-src 'self'; frame-ancestors 'none'`, XFO DENY, nosniff, no-referrer on every response incl. refusals | closed |
| T-03-17 | Info disclosure | `--ui-dir` | low | accept | AR-03-02 | closed |
| T-03-18 | DoS | silent port moves | low | mitigate | busy explicit port bind-tested before persisting; `port_claimed` across instances | closed |
| T-03-19 | Tampering | markup from API strings | medium | mitigate | `textContent`/attributes only; TestNoUnsafeDOM; no innerHTML/eval anywhere in ui/web/js | closed |
| T-03-20 | Tampering | style injection | low | mitigate | no inline style; CSSOM `setProperty` with computed numbers; no `unsafe-inline` | closed |
| T-03-21 | DoS | history ring / route | medium | mitigate | fixed 3000-sample ring; `0 < window_s <= 300`, NaN rejected; samples opt-in | closed |
| T-03-22 | DoS / Tampering | stuck hold | medium | mitigate | `max_hold_ms` 1..60000; engine `releaseExpired` with `max_hold` event | closed |
| T-03-23 | Tampering | malformed down/up bodies | low | mitigate | 64 KiB cap, `DisallowUnknownFields`, `up` refuses a body, `DownRequest.Validate` | closed |
| T-03-24 | Tampering | error text / fan names in DOM | medium | mitigate | as T-03-19 | closed |
| T-03-25 | Tampering | unvalidated page input | low | accept | AR-03-03 | closed |
| T-03-26 | Tampering | page op outside vocabulary | low | mitigate | single OPS table in api.js; TestJSOpsInOperations with self-check | closed |
| T-03-27 | Tampering | vendored uPlot files | **high** | mitigate | sha256 of each file = research values = VENDOR.md; npm sha512 recorded; TestVendorHashes with self-checks; JS has no fetch/XHR/eval/WebSocket | closed |
| T-03-28 | DoS | client memory | low | mitigate | page ring cap 620 + 64; TestRingBounded; server ring capped | closed |
| T-03-29 | Tampering | CSP weakening | low | mitigate | no `unsafe-inline`/`unsafe-eval` anywhere | closed |
| T-03-30 | DoS / Tampering | hold left on | medium | mitigate | page always sends `max_hold_ms: 10000`; all release paths; `up` with keepalive; server max hold backstop | closed |
| T-03-31 | Spoofing | stale values shown as live | low | mitigate | stale class dims readouts, LEDs, plots; TestStaleAndReconnect | closed |
| T-03-32 | Tampering | LAN user pressing buttons | medium | accept | AR-03-01 (D-08); MaxHold 60 s, page 10 s | closed |
| T-03-33 | Tampering | API strings in DOM | medium | mitigate | as T-03-19 | closed |
| T-03-34 | Elevation of privilege | cdptest Chrome debug port | low | mitigate | `--remote-debugging-port=0`, temp profile, process-group kill; not linked into `cmd/sil` | closed |
| T-03-35 | Info disclosure | users unaware of exposure | low | mitigate | README "unauthenticated" section + `--listen 127.0.0.1`; `sil up` notice; REQUIREMENTS/PITFALLS updated | closed |
| T-03-SC/03-01 | Tampering | Go module coder/websocket v1.8.15 | **high** | mitigate | go.sum h1 hashes = plan/research values; `go mod verify` ok; zero transitive additions | closed |
| T-03-SC/03-09 | Tampering | npm uplot@1.6.32 | **high** | mitigate | as T-03-27; vendored, never installed; no package.json/node_modules tracked; Alpine absent | closed |
| T-03-SC/03-02..08, 03-10..12 | Tampering | supply chain (per plan) | low | mitigate | no dependency change after e847058; `make lock-check` 20/0/0; `go mod verify` ok; stdlib CrossOriginProtection | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-03-01 | T-03-03, T-03-32 (medium) | Open, unauthenticated control API and page on the trusted LAN. Compensating controls verified: Host allowlist, CSRF and WS origin checks, `api.key` redacted, lifecycle and logs socket-only, connection/stream caps, max hold. `--listen 127.0.0.1` narrows it; `sil up` and the README state it | user (D-08, 03-CONTEXT) | 2026-11-09 |
| AR-03-02 | T-03-17 (low) | `--ui-dir` is a per-run developer flag: absolute dir with `index.html`, served through `FileServerFS(os.DirFS)` behind the Guard | plan 03-05; verified | 2026-11-10 |
| AR-03-03 | T-03-25 (low) | The page validates nothing by design (UIC-23); the server validators (T-02-07/08/12) are unchanged | plan 03-08; verified | 2026-11-10 |
| AR-03-04 | residual (low) | No per-IP connection cap (a household behind one NAT would share it); one LAN host can hold the 8 web stream slots and up to 64 connections for 10 s each. The CLI keeps its 8 slots on the socket. Inside AR-03-01 | plan 03-03 SUMMARY; verified | 2026-11-10 |
| (carried) | AR-02-04 | Key only on the 0600 `api.sock`: still true under D-18 (web handler redacts every key-bearing path; the page never shows it) | Phase 2 | 2026-11-09 |

*Accepted risks do not resurface in future audit runs.*

---

## Residual Notes (non-blocking)

- Plain NDJSON `GET /api/v1/watch` on the LAN has no ping/write deadline (only `/ws` pings); bounded by the 8-slot browser budget (AR-03-04).
- Code-review info items IN-02 (directory listing on the static handler, `--ui-dir` on the LAN bind, `MaxHeaderBytes` default), IN-05, IN-06, IN-07 open (03-REVIEW-DISPOSITION.md); none weakens a registered mitigation.
- By design: requests without Origin and Sec-Fetch-Site (curl, CLI) pass CrossOriginProtection; a raw `down` without `max_hold_ms` is allowed for CLI parity (the page always sends one).
- The uPlot npm-pack / `--ignore-scripts` / fresh-temp-dir steps were one-off and are evidenced by 03-09-SUMMARY only.
- Chrome-based `sil_ui` and docker `sil_e2e` suites were not re-run by the auditor; they passed in this session's regression gate (make test-ui, full make e2e).

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-10 | 48 | 48 | 0 | gsd-security-auditor (ASVS L1, block_on high) at 09024c3 |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [ ] Maintainer sign-off (pending; see STATE.md Needs Human)
