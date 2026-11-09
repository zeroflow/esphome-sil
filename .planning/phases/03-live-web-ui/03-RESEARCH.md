# Phase 3: Live Web UI - Research

**Researched:** 2026-11-09
**Domain:** Go net/http + WebSocket server surface on an existing supervisor/engine; vendored-library vanilla-JS single-page instrument UI; headless-Chrome test strategy
**Confidence:** HIGH on codebase facts, stack versions and the test/vendoring mechanics (read and executed this session); MEDIUM on security hardening choices and the "behind real time" threshold (design decisions flagged `[ASSUMED]`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Carried forward (not re-discussed)**
- One HTTP/JSON API, one operation vocabulary; the CLI is its client (CLI-06, Phase 1). The UI has **no logic of its own**: press timing, history, derived flags (connected, behind real time) all come from the engine/API.
- Press presets 200 ms / 1500 ms and raw `--down`/`--up`; press on a held button is rejected (02 D-01..D-04).
- LEDs = final strip colors + named segments from the HELLO manifest, <= 20 Hz (02 D-05..D-08).
- `set key=value`, all-or-nothing, fan swap = instant unplug/replug, defaults 22 C / 45 %RH (02 D-09..D-11).
- `watch` = snapshot first, then typed events (02 D-12). The WS should carry the same shapes.
- Stack from research: vendored uPlot 1.6.32 (+ Alpine 3.17.5 or vanilla JS, planner's call), system font stack, `--ui-dir` dev flag, charts and typed arrays kept outside any reactive proxy.

**Visual language**
- **D-01:** The character is a **modern Keysight (InfiniiVision-style) bench oscilloscope**, classic HP/Keysight instrument, explicitly **not** modern/neon "cyberpunk". Dark grey bezel frame. Black screen areas with a faint graticule grid. Crisp sans-serif labels with **tabular numerals** for readouts. Grey, softkey-like buttons. Sober and professional. This is a style reference only: no vendor names, logos or trademarks on the page.
- **D-02:** **Dark only.** One tuned palette, no light theme, no toggle.
- **D-03:** **Scope channel colors per port:** port 1 yellow, port 2 green, port 3 blue, port 4 pink/magenta. The color identifies the port everywhere (plot traces, port label, readouts).
- **D-04:** **Virtual LEDs:** round "lens" dots with a soft glow in the actual strip color. Off is a dark lens; dim is visibly dim (apply a simple display gamma). Under each LED: segment name (from the manifest) and `#rrggbb`, exactly what `sil status` reports.
- **D-05:** **Density: one screen, no scroll on a desktop (~1280 px and up, 1080p laptop).** Layout: a header bar with run info and a message line; a grid of the 4 port panels (2x2 or 4x1, planner's call); a bottom strip with LEDs, USR buttons and environment inputs. Like an instrument front panel.
- **D-06:** **PC first, phone usable.** Below desktop width, panels stack in one column and the page scrolls. Touch targets are at least 44 px; plots, readouts and buttons stay usable. No separate phone design.

**Reaching the page**
- **D-07:** The instance's HTTP server (API + WS + page) **binds on all interfaces (LAN) by default**, so a PC or phone on the LAN can open it. A `--listen <addr>` flag (e.g. `127.0.0.1`) narrows it. This **changes the research/Phase 1 assumption of a 127.0.0.1-only API**. The planner reconciles with what Phase 1 built. Reversibility: costly (consumer scripts and the CLI's discovery (`run.json`) will assume the address and port).
- **D-08:** **No auth, no token (trusted LAN).** The user explicitly accepted an open control API on the LAN. Keep the cheap protections that cost no UX: **Origin check on the WS upgrade and on mutating requests** (blocks CSRF from pages the developer visits). Document clearly in the README and in `sil up` output that the API is unauthenticated on the LAN. Note: this narrows REQUIREMENTS "Auth / remote access for web UI: out of scope (local bench only)". "Local" now means the trusted LAN. The planner should update that row and PITFALLS 19's "bind 127.0.0.1" advice accordingly. The security review should treat open-on-LAN as an accepted risk, not a finding to fix.
- **D-09:** **Sticky web port per instance**, picked once and stored in the identity dir like the API port (Phase 1 D-07), from a distinct range (e.g. starting at 8054). It is the same single HTTP server the CLI uses; no second listener. `--web-port` overrides and updates it. A taken sticky port fails with exit 2 and a JSON error, never a silent move. `sil up` and `sil status` print the full URL using the host's LAN IP. Reversibility: costly (bookmarks, phone shortcuts and scripts depend on the stored port).
- **D-10:** **WS loss -> auto-reconnect with a stale overlay.** The header shows "disconnected, retrying". Readouts and LEDs dim/grey as stale and keep their last values (no fake zeros). Reconnect with backoff. On reconnect, the page takes a fresh snapshot + history and the plots repaint. No manual reload, including across `sil sim restart`.

**Plots**
- **D-11:** **4 small scope screens, one per port**, inside each port panel. RPM is the solid trace and duty a thinner or dashed trace in the same channel color, on a second axis.
- **D-12:** **Rolling 60 s sim-time window.** The server keeps a bounded history ring (about 5 min) behind `GET /api/v1/history` (a CLI-reachable operation as well), so a reload or reconnect repaints immediately. Memory stays flat on both sides over a long run.
- **D-13:** **Fixed axes:** RPM 0 to the plugged fan's `max_rpm` (exposed via status/the fan library), duty 0-100 %. A fan swap re-scales once. No autoscale.

**Buttons & inputs**
- **D-14:** **USR1-3 are real press-and-hold buttons** on the page: pointerdown -> `press --down`, pointerup -> `--up`. The firmware measures hold time, so a long press comes from holding. No preset softkeys on the page (presets stay in the CLI). The button cap lights from the **server-reported** held state, not from local JS state.
- **D-15:** **Stuck-hold guard.** The page sends `up` on pointerup, pointercancel, pointerleave, window blur and tab hidden. In addition, **`down` takes a max-hold parameter** in the API (e.g. `max_hold=10s`; the CLI gets it too as `sil press usr1 --down --max-hold 10s`). The engine auto-releases in sim time and emits that as an event in watch/status. The page always sends a max hold, and the planner picks the value. The safeguard lives in the API, not in JS.
- **D-16:** **Temperature/humidity** are numeric entry fields, applied on Enter or with an apply softkey: one `set` per commit, no stream of intermediate values. The **fan per port** is a dropdown in each port panel, filled from a fan-library list operation in the API. It applies immediately (instant swap per 02 D-10), with no confirm.
- **D-17:** **Errors** go to a scope-style **message line in the header bar**. It shows the API's JSON error message verbatim for a few seconds and puts a red outline on the offending field, which keeps the user's input. No UI-invented messages.

### Claude's Discretion
- **Parity check mechanism (UI-06):** not discussed. Leaning: the UI reads only `/api/v1/*` + `/ws`, the WS frames reuse the exact Go types of `status`/`watch`, and an automated test asserts both (e.g. a Go test that diffs the WS snapshot against `GET /api/v1/status`, plus a check that every endpoint the JS calls is in the operation list). A headless browser test (Playwright, Python side under `tests/`) is allowed if the planner finds it worth the dependency. It must also verify "works with the network blocked" (no `http(s)://` in the served assets).
- "Simulator behind real time" warning: the definition and threshold (computed in the engine, exposed in status), and how it is shown when everything is fine at x1 (the field is present now, so the layout does not move later).
- Exact palette values, graticule density, font sizes, LED gamma, max-hold value, the history ring length and sample rate, the WS coalescing rate (research: snapshot every ~50 ms, drop-oldest per client), and Alpine vs vanilla.
- A `/gsd-ui-phase 3` pass may refine the visual contract (UI-SPEC.md) within D-01..D-06.

### Deferred Ideas (OUT OF SCOPE)
- Selectable timebase softkeys (10 s / 60 s / 5 min): later UI polish (P3).
- One shared scope screen comparing all ports: P3.
- Per-instance token / capability URL: available if the trusted-LAN assumption ever changes.
- Multi-instance landing page: needs an always-on process, which M1 rules out (RUN-03 `sil list` covers discovery later).
- Light theme: not planned.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| UI-01 | Simulator serves a local web page (embedded, no build step, vendored libs, no runtime CDN), live over WebSocket | `ui/` embed package + `internal/web` handler; WS adapter reuses `Control.Watch`; vendored files measured and hashed (Standard Stack); offline browser test with `--host-resolver-rules` (Validation Architecture) |
| UI-02 | Per port: selectable fan, duty, RPM, live duty/RPM plot (bounded buffer) | `sample` event + server history ring + `GET /api/v1/history`; client ring with `null` gaps (plain arrays, not Float64Array); `max_rpm` joined from `GET /api/v1/fans`; verified uPlot config |
| UI-03 | Virtual LEDs with current color/state | `leds.strip[]` / `leds.segments{}` already in Status and `leds` events; inversion of the segment map is presentation only |
| UI-04 | Short and long press for USR1-3, set temperature/humidity | `down`/`up` routes exist; `max_hold` is new (engine auto-release); `POST /api/v1/set` exists; touch needs `releasePointerCapture` |
| UI-05 | Run info: sim time, time factor, firmware connected, behind-real-time warning | `sim.time_s`, `sim.time_factor`, `firmware.bus_connected` exist; `sim.behind_real_time` is new, computed in the engine from tick lateness |
| UI-06 | Everything shown/done is reachable via CLI/API with the same JSON (parity check) | `control.Operations` table driving route registration + CLI mapping; WS snapshot == `GET status`; `data-src` DOM parity; page-state == status; four automated checks |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

Extracted from `/home/claude/esphome-sil/.claude/CLAUDE.md` and `/home/claude/CLAUDE.md`; treated with the authority of locked decisions:

- Simulator, CLI and web server are **Go, single static binary, embedded UI; no Python in the simulator**. Build with `CGO_ENABLED=0`. Product-repo tests may be pytest driving `sil` via CLI/JSON.
- **Web UI: no build step, no external CDN at runtime; vendored plot library (uPlot).** Fonts: system stack only; CI must check the final HTML for `http(s)://` references.
- Stack picks: `github.com/coder/websocket` v1.8.15, stdlib `ServeMux` (method + wildcard patterns), `embed` + `--ui-dir` dev flag, `log/slog` to stderr only (stdout is the CLI's JSON), kong CLI, `testing` stdlib only.
- **Avoid:** `testify`, Gin/Echo/Fiber, Viper, Cobra+Viper, htmx, petite-vue, any CDN/Google Fonts/unpkg at runtime, Python in the simulator.
- uPlot/Alpine pitfall: never keep chart instances or ring buffers inside a reactive proxy.
- Consumer interface: semver tags, `0.x`; sim-bus protocol, web UI, `fan.toml` schema and Lua API are internal (so `sample`, `history`, `max_hold` shapes may still change).
- License GPL-3.0 (vendored uPlot and Alpine are MIT, compatible; ship their license text).
- Repo conventions in force (Phase 1/2): wall-clock lint (`internal/lint/wallclock_test.go` scans every `.go` file including tests and e2e), `apperr.Registry` AST scan (literal codes, one exit class each), 0700 run dir with 0600 `api.sock`, stdout JSON / stderr human text, exit 0/1/2.
- GSD enforcement: file changes only through a GSD workflow; commits from Claude use the attribution lines from the session reminder.
- Production `watermeter-inference` on port 8001 must never be touched; this host already listens on 8000, 8002, 8086 (see Environment Availability).

## Summary

The server side is mostly built and the plan is mostly **adding one more front door to the same handler**, plus four small engine/API additions the UI contract needs. `control.NewHandler(Control)` already exposes every operation the page needs (`status`, `fans`, `set`, button `down`/`up`, `watch`) over `<run>/api.sock`; Phase 3 serves that same handler on a TCP listener (sticky port, all interfaces by default) behind a guard middleware, adds a `/ws` route that wraps `Control.Watch` (so WS frames are byte-identical to `sil watch` NDJSON lines), and serves the embedded page. Four things do **not** exist yet and must be built server-side: (1) a 10 Hz `sample` event plus a bounded history ring behind `GET /api/v1/history` and `sil history` (the existing `rpm` event is a once-per-sim-second sample and `duty` is change-only, which is too coarse for a 60 s scope and carries no heartbeat for SIM TIME); (2) `sim.behind_real_time`, derived in the engine from tick lateness (nothing today detects it); (3) `max_hold` on button `down` with engine auto-release in sim time; (4) a `control.Operations` table that drives route registration and maps each operation to its CLI command, which is what makes the UI-06 parity check mechanical instead of grep-based.

The decision that most changes the threat model is D-07/D-08: Phase 2 accepted `api.key` in `Status` **because** it only left over a 0600 socket (AR-02-04). Serving `Status` over the LAN breaks that premise; the key is the Noise PSK of the firmware's native API on 0.0.0.0. Recommendation: redact `api.key` (and only that) on the network listener. D-08's Origin check alone also does not stop DNS rebinding (an attacker page rebound to the LAN IP is same-origin from the browser's view), so add a Host-header allowlist; Go 1.27's stdlib `http.CrossOriginProtection` covers CSRF for mutating requests, and `coder/websocket`'s default origin check covers the WS upgrade.

On the front end, use **vanilla ES modules, not Alpine**: Alpine's standard build needs `'unsafe-eval'` in CSP (its CSP build is a separate package), the legitimacy tool flags `alpinejs` as SUS (`too-new`), and the page is a `render(state)` over ~40 elements. Two verified facts change the UI-SPEC's performance contract: uPlot 1.6.32 does **not** render `NaN` in a `Float64Array` as a gap (it draws a line across it; `null` in a plain array gaps correctly), and the vendored libraries themselves contain `https://` strings (Alpine's error text, uPlot's credit comment), so the no-`http(s)://` check must scan only first-party files and the network-blocked guarantee must be proven behaviourally in a browser. Headless Chrome 157 is installed and works offline here as uid 1002 without `--no-sandbox`; drive it from a small Go CDP client (already-planned `coder/websocket`) under a `sil_ui` build tag, not Playwright (adding Python Playwright to `tests/` would break the `lock-check` against the product repo's lock).

**Primary recommendation:** Serve the existing control handler on a sticky-port TCP listener behind a Host/Origin guard with `api.key` redacted, add `sample` + history ring, `behind_real_time`, `max_hold` and an `Operations` table server-side first, then build a vanilla-JS page (uPlot only, vendored with recorded sha256) and prove UI-06 with four automated parity checks driven by headless Chrome through a minimal Go CDP harness.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Serving the page and vendored assets | API / Backend (`sil serve` HTTP server, `ui/` embed) | Browser (executes it) | One binary, one server (D-09); no CDN |
| Live state delivery | API / Backend (`/ws` wrapping `Control.Watch`) | Browser (reducer applies events) | Frames are the exact `SnapshotLine`/`Event` Go types |
| Plot history (bounded) | API / Backend (broker-side ring, `GET /history`) | Browser (60 s + 2 s render window) | D-12: reload/reconnect repaints from the server |
| Press hold timing and max-hold auto-release | API / Backend (engine, sim time) | Browser (only sends down/up) | D-15: safeguard lives in the API, not JS |
| Derived flags (firmware connected, behind real time) | API / Backend (engine/supervisor) | Browser (renders the flag) | CONTEXT carried-forward "no logic of its own"; Anti-Pattern 3 |
| Fan / env / button operations | API / Backend (`Control`) | CLI and browser (both clients) | CLI-06: one vocabulary |
| Max RPM for fixed axis | API / Backend (`GET /fans` `max_rpm`) | Browser (joins `ports[].fan` to it) | Avoids a Status change |
| LED display gamma, glow, graticule, number formatting | Browser | n/a | Pure presentation (UI-SPEC) |
| Reconnect/backoff, stale overlay, dirty-field state | Browser | n/a | Presentation state; no sim logic |
| Cross-origin / DNS-rebinding defence | API / Backend (guard middleware on the TCP listener) | Browser (sends Origin/Sec-Fetch-*) | Cannot be done client-side |
| `api.key` exposure policy | API / Backend (redaction wrapper for TCP) | n/a | Key must not leave the 0600 socket (AR-02-04 premise) |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/coder/websocket` | v1.8.15 (proxy `Time` 2026-06-15) | `/ws` upgrade, per-frame write, ping, `CloseRead` | Already the project pick; module `go.mod` is `module github.com/coder/websocket` / `go 1.23` with no requires (zero deps); ISC license; repo pushed 2026-11-06 `[VERIFIED: proxy.golang.org/github.com/coder/websocket/@v/v1.8.15.mod, api.github.com]` |
| uPlot | 1.6.32 (npm modified 2025-03-14) | Scope plots | Project pick; `npm view uplot` OK; MIT `[VERIFIED: npm registry, gsd-tools package-legitimacy: OK, 1.13M weekly downloads]` |
| stdlib `net/http` `ServeMux`, `http.CrossOriginProtection`, `embed`, `io/fs` | Go 1.27.3 | Routing, CSRF check, embedded assets | `go doc net/http.CrossOriginProtection` confirms the type and `Handler`/`SetDenyHandler`/`AddTrustedOrigin` `[VERIFIED: go doc on this host]` |
| Vanilla JS (ES modules, `<script type="module">`) | n/a | Page logic | See "Alpine vs vanilla" below |
| Google Chrome (headless) | 157.0.8103.38 at `/usr/bin/google-chrome` | Browser tests via CDP | Installed; ran JS and exposed `DevToolsActivePort` as uid 1002 without `--no-sandbox` `[VERIFIED: executed this session]` |

**Vendored files (measured this session from `npm pack uplot@1.6.32 alpinejs@3.17.5`):**

| File | Bytes | sha256 |
|------|-------|--------|
| `uPlot.iife.min.js` | 51081 | `19c8d4c6ad88929a79f4ae49d6f7161566dfd0ba3d15cc495e974f787eb78f1f` |
| `uPlot.min.css` | 1857 | `df630c6a8d6f8eeaff264b50f73ce5b114f646ffd9a0bb74f049b0a00135fa04` |
| `LICENSE` (uPlot, MIT) | n/a | vendor beside the files |
| (only if Alpine is chosen) `alpine cdn.min.js` | 55935 | `b4036e9e9ac22fa6f6ae1ba292e015e1f3e9c85d0d79e73e4c8eb9bb139b522e` |

npm tarball integrity: uplot `sha512-KIMVnG68zvu5XXUbC4LQEPnhwOxBuLyW1AHtpm6IKTXImkbLgkMy+jabjLgSLMasNuGGzQm/ep3tOkyTxpiQIw==`, alpinejs `sha512-XewMqRC6zjy8iaUPn86dxjvooAsNai3hUtQbdn0A5H2w7Jt10xHGZ1SczN5+Dvp3UKkuSEt9moy/UaUWOwKdnQ==` `[VERIFIED: npm view dist.integrity]`. Record these in `ui/web/vendor/VENDOR.md` and enforce with a Go test that re-hashes the files.

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `testing` + `net/http/httptest` + `clock.Manual` | stdlib / repo | Server-side unit tests, deterministic engine tests | All Go-side tests |
| Hand-written CDP client (~200 lines) on `coder/websocket` | n/a | Drive headless Chrome (navigate, evaluate, input, screenshot, network log) | Browser tests under build tag `sil_ui` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Vanilla ES modules | Alpine.js 3.17.5 | Standard build requires `'unsafe-eval'` in CSP; the strict-CSP variant is a separate package `@alpinejs/csp` `[CITED: github.com/alpinejs/alpine docs/advanced/csp.md via Context7 /alpinejs/alpine]`. `alpinejs` was flagged `SUS` / `too-new` by the legitimacy tool (published 2026-10-20). CONTEXT leaves this to the planner. |
| Go CDP client | Python Playwright under `tests/` | `tests/uv.lock` is gated by `make lock-check` against `../wifi-fancontroller/hil/uv.lock`; Playwright is not in that lock, so it would fail the gate. Playwright browsers are cached under `~/.cache/ms-playwright` but the Python package is not installed. |
| Go CDP client | `chromedp` | Adds a multi-module dependency tree to `go.mod` for test-only use; hand-rolled client reuses `coder/websocket` |
| Go CDP client | Node + puppeteer-core | Needs `npm install`; unnecessary when Chrome is scriptable over CDP |
| plain JS arrays with `null` | `Float64Array` + `NaN` (UI-SPEC "Performance contract") | **NaN is not a gap in uPlot 1.6.32** (measured below); arrays with `null` are |

**Installation (one-off, vendoring only; not part of any build):**
```bash
mkdir -p ui/web/vendor && cd "$(mktemp -d)"
npm pack uplot@1.6.32 --silent && tar -xzf uplot-1.6.32.tgz
cp package/dist/uPlot.iife.min.js package/dist/uPlot.min.css package/LICENSE /path/to/repo/ui/web/vendor/
sha256sum package/dist/uPlot.iife.min.js package/dist/uPlot.min.css   # compare with VENDOR.md
```
```bash
go get github.com/coder/websocket@v1.8.15   # then: go mod verify
```

**Version verification:** `npm view uplot version` = 1.6.32 (modified 2025-03-14); `npm view alpinejs version` = 3.17.5 (modified 2026-10-20); `proxy.golang.org/github.com/coder/websocket/@latest` = v1.8.15; `sum.golang.org/lookup/github.com/coder/websocket@v1.8.15` returned `h1:6B2JPeOGlpff2Uz6vOEH1Vzpi0iUz20A+lPVhPHtNUA=` and go.mod hash `h1:NX3SzP+inril6yawo5CQXx8+fk145lPDC6pumgx0mVg=` `[VERIFIED]`.

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| uplot | npm | published 2025-03-14 (v1.6.32), long-lived project | 1,132,895/wk | github.com/leeoniya/uPlot | OK | Approved (vendored once; never installed at build or runtime) |
| alpinejs | npm | v3.17.5 published 2026-10-20 | 946,666/wk | github.com/alpinejs/alpine | SUS (`too-new`) | **Not recommended** (vanilla chosen); if the planner picks Alpine anyway, a `checkpoint:human-verify` before vendoring is required |
| github.com/coder/websocket | Go proxy | v1.8.15 2026-06-15, project renamed from nhooyr.io/websocket, 5565 stars | n/a (tool does not support the Go ecosystem) | github.com/coder/websocket | not tool-checked | Approved on proxy-origin + `sum.golang.org` evidence (same unattended method Phase 1 used for `flynn/noise`); `go mod verify` in `make check`. Tag `[ASSUMED]` for "legitimacy" only because `gsd-tools package-legitimacy` rejects `--ecosystem go`; Context7 lists it as High reputation. |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** `alpinejs` (only matters if Alpine is chosen; vanilla is recommended)

## Architecture Patterns

### System Architecture Diagram

```
 Browser (PC / phone on LAN)                      sil serve <name> (one process)
 ───────────────────────────                      ──────────────────────────────────────────────
                                                    TCP listener  <listen>:<web_port>  (sticky, D-09)
  GET /            ───────────────────────────────▶ conn cap + header-phase deadline (clock)
  GET /js/*.js                                       │
  GET /vendor/*                                      ▼
                                                  web.guard   Host allowlist ─▶ 403 JSON error
                                                    │         http.CrossOriginProtection (non-GET) ─▶ 403 JSON error
                                                    │         security headers (CSP, nosniff, DENY)
                                                    ▼
  POST /api/v1/set                                web mux
  POST /api/v1/buttons/{id}/down|up ──────────────▶  /api/v1/*  ─▶ control.NewHandler(redacted Control)  ◀── same handler ── api.sock (0600) ◀── sil CLI
  GET  /api/v1/fans | /history | /status             /ws         ─▶ WS adapter: Control.Watch(kinds+sample) ─▶ 1 JSON text frame per line
  WS   /ws  ◀── snapshot, events, samples ◀─────────  /          ─▶ ui.FS (embed) or --ui-dir (disk)
                                                    │
                                                    ▼
                                      supervisor (control.Control)
                                        Status / Set / Fans / SetButton(max_hold) / History / Watch
                                           │                         │
                                events.Broker (seq, 16 subs) ◀── Emit ── engine goroutine (10 ms tick, sim clock)
                                   + sample ring (3000)                 ├ sample every 100 ms sim time  (NEW)
                                                                        ├ behind_real_time from tick lateness (NEW)
                                                                        └ max_hold auto-release         (NEW)
                                                                           │ sim-bus (bus.sock, UDS)
                                                                           ▼
                                                                      firmware (host build)

 Browser internals:  link.js (WS, backoff, buffer-while-fetching-history)
                       ─▶ store.js (snapshot + typed-event reducer, state mirrors GET status)
                       ─▶ view.js (cached DOM refs, rAF-coalesced writes)   ─▶ plot.js (uPlot x4, rings with null gaps)
                     controls.js (pointer events → down/up+max_hold; set; fan select) ─▶ api.js (OPS table, verbatim errors)
```

### Recommended Project Structure
```
ui/
├── embed.go                 # package ui; //go:embed all:web ; func FS() fs.FS (go:embed cannot reach parent dirs)
└── web/
    ├── index.html           # static markup; favicon data:, ; <script type=module src=/js/app.js>
    ├── app.css              # --bezel/--screen/--softkey/--ch1..4/--warn/--error custom properties
    ├── js/
    │   ├── app.js           # wiring
    │   ├── api.js           # OPS table (method+path) and fetch wrapper; errors verbatim
    │   ├── link.js          # WS lifecycle, backoff 0.5s→5s, buffer frames while history loads
    │   ├── store.js         # reducer: snapshot + events → state (mirror of GET status)
    │   ├── ring.js          # bounded series with null gaps (plain arrays)
    │   ├── plot.js          # uPlot per port (module scope, never in reactive state)
    │   ├── view.js          # DOM writes, gamma/glow, stale overlay, formatters
    │   └── controls.js      # USR pointer handling, env fields (dirty), fan select
    └── vendor/
        ├── uPlot.iife.min.js, uPlot.min.css, LICENSE.uplot
        └── VENDOR.md        # version, source, sha256 + sha512 integrity
internal/web/                # handler.go (compose), guard.go (Host/Origin/headers), ws.go, redact.go, listener.go (cap + header deadline)
internal/control/            # + operations.go (Operations table), History types, max_hold on down
internal/events/             # + SampleKind, sample ring inside Broker
internal/engine/             # + sample emission, behind-real-time, max-hold auto-release
internal/instance/           # + web port (identity), Spec.Listen/AllowHosts/UIDir, RunFile web fields
uitest/                      # //go:build sil_ui — CDP client + browser tests (own TestMain, no docker)
```

### Pattern 1: One handler, two listeners
**What:** `ServeListener(ctx, ln, h)` already takes any `net.Listener`. Keep `api.sock` serving `control.NewHandler(s)` (full `Status`, used by the CLI). Add a TCP listener serving `web.NewHandler(redact(s), opts)`, which mounts `control.NewHandler` for `/api/v1/` so CLI and browser share route code.
**When to use:** always; satisfies D-09 "same single HTTP server, no second listener" in the sense of one HTTP *implementation* with two transports, and keeps the 0600 socket as the only place the key appears.
**Reconciliation with Phase 1:** `control.go:5-7` says "Phase 1 serves the API only on <run>/api.sock ... A 127.0.0.1 TCP listener can be added later behind the same handler." Phase 3 does exactly that, with the bind address changed to the LAN per D-07. `[VERIFIED: internal/control/control.go:5-7]`
**Example:**
```go
// Source: internal/control/control.go ServeListener (existing) + this research
ln, err := net.Listen("tcp4", net.JoinHostPort(spec.Listen, strconv.Itoa(id.WebPort)))
// ... wrap: web.CapListener(ln, 64, clk, headerBudget)  -> conn cap and header-phase deadline
go func() { webDone <- control.ServeListener(srvCtx, webLn, web.NewHandler(web.Options{...}, redactingControl{s})) }()
```

### Pattern 2: WS adapter = `Watch` + one frame per line
**What:** The `/ws` handler calls `Control.Watch` with a `send` that writes each value as one text frame, encoded with the same encoder as `streamNDJSON` (`SetEscapeHTML(false)`). The first frame is `SnapshotLine` (Status fields at top level plus `t`,`k`,`seq`), then `Event`s, then `StoppedLine`. No new shapes, so the WS "reuses the exact Go types" by construction. `[VERIFIED: internal/supervisor/watch.go:51-98, internal/control/control.go:208-216,500-529]`
**Browser constraint:** a failed WebSocket handshake gives the page no status or body, so errors that happen after the decision to stream (`too_many_streams`, `stopping`) must be sent **after** the upgrade as a final `{"error":{code,message}}` text frame followed by a close, so the message line can show them verbatim (D-17). Origin/Host rejections happen before the upgrade (HTTP 403 JSON); the page just sees `DISCONNECTED, RETRYING`.
**Example:**
```go
// Source: coder/websocket docs (Context7 /coder/websocket) adapted
c, err := websocket.Accept(w, r, &websocket.AcceptOptions{}) // default: request host authorized, no InsecureSkipVerify
if err != nil { return }                                      // Accept already answered 403
defer c.CloseNow()
ctx := c.CloseRead(r.Context())                               // write-only conn; keeps ping/pong/close working
err = ctrl.Watch(ctx, req, func(v any) error {
    wctx, cancel := clock.WithTimeout(ctx, clk, writeBudget)  // NOT context.WithTimeout: the wall-clock lint bans it
    defer cancel()
    return c.Write(wctx, websocket.MessageText, encodeLine(v))
})
```
Run a ping loop on `clk.NewTicker(15s)` with `clock.WithTimeout(ctx, clk, 10s)` per `c.Ping`; a dead phone must free its stream slot (see Pitfall 2).

### Pattern 3: Sample event + broker-side ring (history == same events)
**What:** The engine emits `{"t":<µs>,"k":"sample","v":[{"port":1,"duty":0.4,"rpm":812.5}, ...]}` every 100 ms of **sim** time from its tick (`nextSample += 100ms`). `duty`/`rpm` are `null` while the bus is disconnected (UI-SPEC UIC-09: gaps, never fake zeros). `Broker.Publish` appends `sample` events to a fixed-capacity ring (3000 = 5 min at 10 Hz) **under the same lock that assigns `seq`**, so `GET /api/v1/history` is consistent with every subscriber by construction. `sample` is opt-in: `events.Kinds` (the "empty means all" set used by `sil watch`) stays as is, so default `sil watch` output does not become a 10 Hz flood; the page requests `kinds = Kinds + sample`.
**Why a new event rather than the existing ones:** `RPMEventStep = time.Second` and rpm events fire only on change, duty events only on change `[VERIFIED: internal/engine/engine.go:34-37]`; an idle bench emits nothing, so SIM TIME would freeze and a spin-up with `tau_up` 1.0 s would be 2-3 points. Samples are also the SIM TIME heartbeat (`t`), satisfying "always sim time from the API, never a browser clock".
**Join on (re)connect (UI-SPEC):** open WS (frame 1 = snapshot with `seq` S) → buffer frames → `GET /api/v1/history?window_s=62` (returns `seq` H and samples) → apply history → apply buffered samples with `seq > H` → go live. Dedupe key is `seq` **within one connection epoch**; after an instance restart the broker's seq restarts at 0, so a reconnect must replace, not merge with, the old rings (UI-SPEC already says "replace everything").

### Pattern 4: Behind-real-time from tick lateness (engine, injectable clock)
**What exists today:** nothing detects it. Searching the Go sources for behind/lag/overrun finds no engine support `[VERIFIED: grep over internal/ and cmd/, no engine hits]`. The real clock's ticker drops ticks when the receiver is slow (`select { case rt.c <- r.Now(): default: }`, `internal/clock/clock.go:104-108`), and the engine integrates by `dt := now - last` (`engine.go:445-447`) with `tick = 10 * time.Millisecond` (`engine.go:30`). At factor 1 sim time **is** monotonic wall time, so the only observable "falling behind" is **late or missed ticks**.
**Definition (recommended, `[ASSUMED]` threshold):** in the tick branch compute `late := clk.Now() - now` (tick stamp vs processing time) and `gap := dt`. The engine is *behind* when `late > 50 ms` or `dt > 50 ms` (five ticks) on any tick; it returns to *in sync* after 2 s of sim time with no such tick (hysteresis, so the chip does not flicker). Expose `sim.behind_real_time` (bool, additive field in `SimStatus` next to `time_factor`) and publish a `sim` event with `v: "behind"` / `"in_sync"` on transitions (default `sil watch` shows it). Optional diagnostic `sim.lag_ms`. When the time factor later exceeds 1 the same definition (processing lag against the scaled tick) keeps working.
**Deterministic test:** `clock.Manual` `Advance(200 * time.Millisecond)` delivers one tick with `dt = 200ms` → flag true; 2 s of 10 ms ticks → false. `[VERIFIED: manual.go Advance delivers each due tick with Now set to its due time]`; to force a single late tick, Advance once by 200 ms after `BlockUntil` registers the ticker is not enough (Manual delivers every due tick), so the engine-level test should drive the lateness check through a small internal hook (`e.noteTick(now, clkNow)`) rather than through Manual's per-tick delivery.

### Pattern 5: `max_hold` on `down`
**What:** `POST /api/v1/buttons/{id}/down` accepts an optional JSON body `{"max_hold_ms": N}` (empty body keeps today's behavior, mirroring how `sim/restart` treats an empty body, `control.go:367-388`); validated like `PressRequest` (`1..MaxHold` = 60 000 ms). The engine stores `holdUntil[id] = simNow + max_hold` and the tick branch auto-releases (`buttons[id]=false`, `emit button v:false`, `send buttonFrame`) with `reason:"max_hold"` on the event (additive `Event.Reason`, `omitempty`). The page always sends `max_hold_ms: 10000` (UI-SPEC default). `sil press usr1 --down --max-hold 10s` is the CLI surface. A redundant `up` after an auto-release is answered `button_not_held` (existing strict rule) and shown verbatim; the page should clear its local "I started a hold" flag when the server reports the button released, so it normally does not send that `up`.

### Pattern 6: `control.Operations` table (the UI-06 spine)
**What:** Replace the inline `mux.HandleFunc` calls in `NewHandler` with a table `[]Operation{ {Name, Method, Pattern, CLI, UI bool, Handler}, ...}` that both registers routes and is exported for tests. Today the only list of routes is the registration code itself (`control.go:359-492`), so "every endpoint the JS calls is in the operation list" has nothing to compare against. `CLI` is the command string (`"status"`, `"set"`, `"press --down|--up"`, `"history"`, `"fans"`, `"watch"`); a CLI-package test asserts each non-empty `CLI` parses with kong. `[VERIFIED: internal/control/control.go:357-494 routes registered inline]`

### Anti-Patterns to Avoid
- **Logic in the page:** no press timing, no `behind` computation, no connected derivation, no range validation. The page's reducer applies typed events to a snapshot; it never derives flags.
- **A second API shape for the browser:** do not add UI-only endpoints or a UI-specific Status. If the page needs a value, add it to `Status`/`Event`/`History` and the CLI gets it too.
- **Wrapping `http.ResponseWriter` in middleware without `Hijacker`/`Flusher`:** breaks WS upgrade and NDJSON streaming. Use `http.NewResponseController` or do not wrap.
- **`context.WithTimeout`, `time.Now`, `http.Server{ReadTimeout...}` anywhere:** the lint fails the build (`wallclock_test.go:34-44`); use `clock.WithTimeout`, the injected clock, `Deadline`.
- **Float64Array + NaN for gappy series** (see Pitfall 4).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| CSRF for mutating requests | Custom Origin/Referer parsing | `http.NewCrossOriginProtection()` with `SetDenyHandler` returning the API's JSON error | Stdlib, uses `Sec-Fetch-Site` then Origin-vs-Host; allows non-browser requests with neither header (the CLI and curl) `[VERIFIED: go doc net/http.CrossOriginProtection]` |
| WS upgrade origin check | Own `Origin` comparison | `websocket.Accept` default (request host authorized; `InsecureSkipVerify` false) | Library-tested; GET is "safe" so `CrossOriginProtection` does not cover the upgrade |
| WS framing, ping/pong, close | Own frame code | `coder/websocket` `Write`/`Ping`/`CloseRead` | Concurrent-write safe, context-first |
| Event fan-out, backpressure, `lagged` | New per-client queues | Existing `events.Broker` (1024/sub, 16 subs, `lagged` marker) | Already built and tested; the WS adapter is just another subscriber |
| Static serving + content types | Custom file handler | `http.FileServerFS(ui.FS())` / `http.FileServer(http.Dir(uiDir))` for `--ui-dir` | Built-in MIME table covers `.js`/`.css`/`.html`; add `nosniff` |
| Plot rendering, axes, grid | Canvas drawing code | uPlot with fixed scales (verified config below) | Streaming-grade, 51 KB |
| Sticky port allocation | Second allocator | Generalize `AllocatePort`/`claimedPorts` in `internal/instance/port.go` by field name (`api_port` / `web_port`) | Same lock, same collision rules |
| Browser automation | Python Playwright (breaks `lock-check`), chromedp (dependency tree) | ~200-line CDP client over `coder/websocket` | Chrome is installed; CDP methods needed are few |
| Hashing vendored files | Shell scripts only | A Go test reading `VENDOR.md` and re-hashing | Runs in `make check`, no new tooling |

**Key insight:** every hard part of this phase already exists in the repo as a tested mechanism (broker, `Control.Watch`, sticky ports, clock, apperr registry). The risk is not building them again but bypassing them (a UI-only shape, a wall-clock timeout, an unregistered error code).

## Common Pitfalls

### Pitfall 1: Serving `Status` on the LAN leaks the firmware's Noise key
**What goes wrong:** `Status.API.Key` is `s.id.Key` (`supervisor.go:669`) and the firmware's native API listens on 0.0.0.0 (`instance/port.go:101-104` test-binds `0.0.0.0`). Anyone on the LAN who can reach the web port can read the PSK and then talk to (or impersonate) the firmware API, or add the device to their Home Assistant. Phase 2's accepted-risk entry AR-02-04 rests on "served only on 0600 api.sock" `[VERIFIED: 02-SECURITY.md Accepted Risks Log AR-02-04: "Key already in status/up JSON since Phase 1; served only on 0600 api.sock; stderr hint omits the key"]`. `Down`, `SimRestart` and `FwRestart` also return `Status`, and the `watch` snapshot embeds it.
**How to avoid:** wrap `Control` for the TCP listener with a redactor that blanks `API.Key` in `Status`, in the three Status-returning operations, and in `SnapshotLine`s passed to `Watch`'s `send`. The CLI keeps the full Status over the socket. UI-06 parity is then defined per listener (WS snapshot equals `GET status` on the *same* listener). The page never displays the key. Record this as superseding AR-02-04 in the Phase 3 threat register. `[ASSUMED]` that the user accepts this (it narrows, not widens, D-08) - listed in Open Questions.
**Warning signs:** `curl http://<lan-ip>:<web-port>/api/v1/status | grep key` returns a key.

### Pitfall 2: Zombie WebSocket clients exhaust the 16-stream cap and lock out the CLI
**What goes wrong:** `events.MaxSubscribers = 16` is shared by `watch`, `logs --follow` and now every open tab/phone (`events.go:14-19`, `supervisor/watch.go:21-34`). A phone that sleeps leaves a half-open TCP connection; until the server notices, the slot is held. The page reconnects in a loop, so a few devices can make `sil watch` answer `too_many_streams`.
**How to avoid:** server-side ping every 15 s with a 10 s per-ping `clock.WithTimeout`; on failure close and release (`defer release()` in `Watch` already frees the slot when `send` errors). Cap browser WS connections at 8 of the 16 (a counter in `internal/web`, same `too_many_streams` code, so CLI always has headroom). Send the error as a final frame (Pattern 2). `[ASSUMED]` split of 8/16.
**Warning signs:** `sil watch` fails with `too_many_streams` while only browsers are open.

### Pitfall 3: Origin check alone does not stop DNS rebinding
**What goes wrong:** with the LAN bind (D-07), an attacker page `evil.example` can rebind its name to `192.168.x.y`; the browser then sends `Host: evil.example:8054` and `Origin: http://evil.example:8054`, which are equal, so both `CrossOriginProtection` and `websocket.Accept`'s default check pass, and the page can read `/api/v1/status` and POST `/api/v1/set`.
**How to avoid:** a Host-header allowlist before everything else: accept IP literals (v4 and bracketed v6), `localhost`, `os.Hostname()`, `<hostname>.local`, plus repeatable `--allow-host`. Anything else answers 403 with the JSON error shape (new registered code, e.g. `host_rejected`; `origin_rejected` for the CSRF deny handler). The CLI over `api.sock` is unaffected (guard only on TCP). `[ASSUMED]` the exact allowlist rule; the mechanism is standard.
**Warning signs:** `curl -H 'Host: evil.example' http://127.0.0.1:<port>/api/v1/status` returns 200.

### Pitfall 4: uPlot draws a line across `NaN`; Float64Array cannot hold `null`
**What goes wrong:** UI-SPEC says "`Float64Array` ring buffers" and "Missing samples are `null`: draw a gap". Measured in Chrome 157 with uPlot 1.6.32 (width-3 white line, column sampled inside the gap): plain array with `null` -> gap column has **0** lit pixels; `Float64Array` with `NaN` -> **3** lit pixels (the line crosses the gap). `[VERIFIED: executed this session; scratchpad uptest/t.html output {"nullArray":{"before":3,"gap":0,"after":3},"typedNaN":{"before":3,"gap":3,"after":3}}]`
**How to avoid:** series values in plain `Array`s with `null`; keep rings in module scope; bounded by compaction (append; when `length > cap + slack`, `splice(0, length - cap)`). Cap = 62 s x 10 Hz = 620 plus slack. The UI-SPEC's intent (module scope, bounded, no reactive proxy) is preserved; only the container type changes. Note in the plan that this deviates from the "Float64Array" wording.
**Also:** a simulator-restart gap produces no samples (no `null`s), so the client breaks the line when consecutive `t` differ by more than 2.5 sample periods (presentation rule, not interpolation).

### Pitfall 5: Vendored libraries contain `https://` strings
**What goes wrong:** the "no `http(s)://` in served assets" CI check (CLAUDE.md, CONTEXT) fails on vendored files: `alpinejs cdn.min.js` has 2 occurrences of `https://alpinejs.dev/plugins/${r}` (error text) and `uPlot.iife.min.js` has `https://github.com/leeoniya/uPlot` (credit comment) `[VERIFIED: grep over the packed dist files]`. Neither triggers a request.
**How to avoid:** scan first-party files only (`index.html`, `app.css`, `js/*.js`) for `https?://` in a Go test; prove the network-blocked guarantee **behaviourally** in Chrome (all requests same-origin, `--host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE 127.0.0.1"`, which I verified turns `fetch("https://example.com/")` from `ok` into `Failed to fetch` on this host).

### Pitfall 6: Touch pointers are implicitly captured, so `pointerleave` never fires
**What goes wrong:** D-15 relies on `pointerleave` to release a held USR cap. For touch, the Pointer Events spec says the UA sets pointer capture to the target on `pointerdown` for direct-manipulation devices, and captured events are treated as inside the capturing element `[CITED: https://www.w3.org/TR/pointerevents3/ section 4.1.3, partial read; the pointerleave consequence is my inference, section 9.4 was not read]`. A finger sliding off a button would keep it held until the 10 s max-hold.
**How to avoid:** in the `pointerdown` handler call `event.target.releasePointerCapture(event.pointerId)` (UI-SPEC's "no `setPointerCapture`" is about not *adding* capture), also listen for `lostpointercapture` and `touchcancel`-equivalent `pointercancel`. Verify with CDP `Input.dispatchTouchEvent` (touchStart on the cap, touchMove outside, expect an `up` request).

### Pitfall 7: No `ReadHeaderTimeout` on a LAN-facing listener
**What goes wrong:** Phase 1 dropped server timeouts to satisfy the wall-clock lint (`bannedFields` lists `ReadHeaderTimeout`, `wallclock_test.go:36-44`; recorded in STATE.md Needs Human, accepted for the 0600 socket). On a LAN port a slow-header client holds a goroutine and fd indefinitely.
**How to avoid (no lint change):** (a) `srv.SetKeepAlivesEnabled(false)` so each connection serves one request (the client already does the same: `DisableKeepAlives: true`, `control.go:608-613`); (b) a listener wrapper whose `Accept` calls `conn.SetReadDeadline(clk.Deadline(10s))`. In Go 1.27.3's `net/http` the server sets a header deadline only if configured (`server.go:2058`) and clears the read deadline to zero once the headers are read (`server.go:1103`, `c.rwc.SetReadDeadline(wholeReqDeadline)` with zero when no `ReadTimeout`), so the Accept-time deadline behaves as a header timeout and leaves long streams and WS untouched `[VERIFIED: read $GOROOT/src/net/http/server.go lines 1038-1040, 1103, 2058]`; (c) a connection-count cap (64). Add a test with a slow-header client (short budget via parameter). This depends on stdlib internals, hence the test.
**Alternative:** allow-list `ReadHeaderTimeout` for `internal/web` in the lint with a comment (transport-level protection should be real time, not scaled time). The planner picks; (b)+(c) needs no lint edit.

### Pitfall 8: Status has no `max_rpm`; fan option text vs id
**What goes wrong:** `PortStatus` is `{Port, Fan, Duty, RPM, State}` (`control.go:117-123`); `Fan` is the fan **id** (`supervisor.go:682`: `Fan: p.Fan`, where the engine sets `Fan: p.cfg.FanID`, `engine.go:312`). UI-SPEC wants `max_rpm` on the status or fan record, and "options are the fan-library names", while `set port1.fan=` accepts **ids** (`parseFan` checks `slices.Contains(ids, id)`, `supervisor.go` unknown_fan).
**How to avoid:** join `ports[n].fan` to `GET /api/v1/fans` (`FanInfo{ID, Name, MinRPM, MaxRPM, BelowMin}`, `control.go:195-201`), which the page fetches once per (re)connect anyway; no Status change. Select `value = FanInfo.ID`, visible text `FanInfo.Name`, `title = id`. Flag the spec wording in Open Questions.

### Pitfall 9: Hidden tabs batch frames
**What goes wrong:** `requestAnimationFrame` stops in background tabs; if ring appends happen inside rAF the data is lost or dumped late (PITFALLS 19).
**How to avoid:** append to rings in the WS message handler (always runs), coalesce only the redraw (rAF, <= 20/s). On `visibilitychange` to visible, redraw once.

### Pitfall 10: Stale-state edge cases around `sil down` / `sil up`
**What goes wrong:** the broker survives `sim restart` so the WS stays open then, but `sil down` ends every stream with a `stopped` line; the page must treat `stopped` like a close and retry forever. With the sticky web port, the page reconnects to the next `sil up` by itself, but the new instance's sim clock restarts at 0 and `seq` at 0.
**How to avoid:** treat any close/`stopped` as link loss; on reconnect always clear rings and state before applying snapshot + history.

### Pitfall 11: New wire fields vs fakes and golden tests
**What goes wrong:** adding methods to `control.Control` (`History`) breaks `fakeControl` in `internal/cli/cli_test.go` and `internal/control/control_test.go`; adding a `sample` kind interacts with `events_test.go` `UnknownKind(Kinds)`; `WatchRequest.Validate` rejects unknown kinds.
**How to avoid:** list these test-file updates in the plan; keep `Kinds` unchanged, add `SampleKind` and make `UnknownKind` accept it; make default subscription (empty kinds) exclude `sample` explicitly.

### Pitfall 12: Identity and spec extensions
**What goes wrong:** `readIdentity` validates `api_port` strictly (`instance.go`), `Identity` is JSON schema 1, and `claimedPorts` only reads `api_port`. A stored identity from Phase 1/2 has no web port.
**How to avoid:** add `WebPort int json:"web_port,omitempty"`; absent -> allocate at the next `LoadOrCreate` under the same flock; web range `8054..9053` (disjoint from the API range 6054..7053 by construction: `DefaultAPIPort = 6054`, `PortScanSpan = 1000`); `claimedPorts` generalized by field; explicit `--web-port` must differ from the instance's own `api_port`; taken sticky port -> `port_in_use` (exit 2), reusing `TestBind` but binding the **listen address** (not always `0.0.0.0`). `--listen`, `--allow-host`, `--ui-dir` are per-run, so they go into `spec.json` (`Spec`), which `sil serve <name>` already reads; add `web_port`/`web_listen` to `run.json` (`RunFile` is decoded without `DisallowUnknownFields`, so older readers are unaffected).

## Code Examples

### Fixed-scale scope screen (verified to construct and lay out in Chrome 157 + uPlot 1.6.32)
```js
// Source: executed in headless Chrome 157 against vendored uPlot 1.6.32 (scratchpad uptest/t2.html).
// Output: xsplits 11 (10 divisions), rsplits [0,450,900,1350,1800], rvals ["0","","900","","1800"],
//         scales x=[40,100] rpm=[0,1800] duty max 100. Pixel look not asserted.
const WIN = 60;
function makeScope(el, color, maxRpm, getTmax) {
  return new uPlot({
    width: el.clientWidth, height: el.clientHeight,
    legend: { show: false }, cursor: { show: false }, padding: [8, 8, 8, 8],
    scales: {
      x:    { time: false, auto: false, range: () => [getTmax() - WIN, getTmax()] },
      rpm:  { auto: false, range: [0, maxRpm] },
      duty: { auto: false, range: [0, 100] },
    },
    axes: [
      { scale: "x", values: () => [], size: 0, gap: 0, ticks: { show: false },
        grid: { stroke: "#20252b", width: 1 },
        splits: (u, i, min, max) => Array.from({ length: 11 }, (_, k) => min + (k * (max - min)) / 10) },
      { scale: "rpm", stroke: color, ticks: { show: false }, grid: { stroke: "#20252b", width: 1 },
        splits: () => [0, maxRpm / 4, maxRpm / 2, (maxRpm * 3) / 4, maxRpm],
        values: (u, s) => s.map((v, i) => (i % 2 === 0 ? String(Math.round(v)) : "")) },
      { scale: "duty", side: 1, stroke: color, ticks: { show: false }, grid: { show: false },
        splits: () => [0, 25, 50, 75, 100],
        values: (u, s) => s.map((v, i) => (i % 2 === 0 ? String(v) : "")) },
    ],
    series: [
      {},
      { scale: "rpm",  stroke: color, width: 2, spanGaps: false, points: { show: false } },
      { scale: "duty", stroke: color, width: 1, dash: [4, 4], spanGaps: false, points: { show: false },
        paths: uPlot.paths.stepped({ align: 1 }) },
    ],
  }, [xs, rpmSeries, dutySeries], el);   // xs / series: plain Arrays, null = gap
}
// Note: the first draw is deferred (scales are null until the next task); read u.scales after a tick.
```
Fan swap: `u.setScale("rpm", {min: 0, max: newMax})` and redraw (RPM `range` closure over a mutable `maxRpm`).

### Button hold handler (touch-safe)
```js
// Source: Pointer Events spec behaviour (touch implicit capture) + UI-SPEC Interaction Contract
cap.addEventListener("pointerdown", (e) => {
  if (e.button !== 0) return;
  cap.releasePointerCapture?.(e.pointerId);        // touch: make pointerleave fire again
  held = true; api.buttonDown(id, { max_hold_ms: 10000 });
});
for (const t of ["pointerup", "pointercancel", "pointerleave", "lostpointercapture"])
  cap.addEventListener(t, release);
addEventListener("blur", releaseAll);
document.addEventListener("visibilitychange", () => document.hidden && releaseAll());
function release() { if (!held) return; held = false; api.buttonUp(id); }   // lights only from server state
```

### Guard middleware (outermost on the TCP handler)
```go
// Source: go doc net/http.CrossOriginProtection (verified) + this research
cop := http.NewCrossOriginProtection()
cop.SetDenyHandler(jsonError(apperr.Failed("origin_rejected", "cross-origin request rejected", nil)))
return hostGuard(allowed, securityHeaders(cop.Handler(mux)))
// hostGuard: host, _, _ := net.SplitHostPort(r.Host); allow ip literal | localhost | hostname | hostname.local | --allow-host
// securityHeaders: Content-Security-Policy, X-Content-Type-Options: nosniff, X-Frame-Options: DENY, Cache-Control: no-cache
```
Recommended CSP for vanilla JS: `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`. uPlot sets inline styles through the CSSOM; whether `style-src 'self'` alone is enough is `[ASSUMED]` until the browser test asserts "no CSP violation in the console"; fallback is `style-src 'self' 'unsafe-inline'` (style only; scripts stay strict).

### Parity check: `data-src` DOM sweep (browser test)
```js
// Every displayed value carries data-src="ports.0.rpm" (a path into GET /api/v1/status JSON) and data-fmt.
const status = await (await fetch("/api/v1/status")).json();
for (const el of document.querySelectorAll("[data-src]")) {
  const want = lookup(status, el.dataset.src);                // must exist in the API JSON
  if (want === undefined) throw new Error("not in API: " + el.dataset.src);
  assertFormatted(el.textContent, want, el.dataset.fmt);      // rpm int, pct 1 decimal, simtime, x1, hex
}
// and the inverse: fail if a visible readout element lacks data-src.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Hand-written Origin/Referer CSRF checks | `net/http.CrossOriginProtection` (Sec-Fetch-Site + Origin vs Host) | stdlib since Go 1.25; present in 1.27.3 | No custom CSRF code |
| `gorilla/websocket` + hand mutex | `coder/websocket` (context API, concurrent-write safe) | project decision | Matches `clock.WithTimeout` pattern |
| Alpine with `'unsafe-eval'` | Vanilla modules with strict `script-src 'self'` | this phase | Strict CSP achievable |
| Float64Array rings with NaN gaps | Plain arrays with `null` for uPlot gaps | measured here, uPlot 1.6.32 | Deviation from UI-SPEC container type |

**Deprecated/outdated:**
- PITFALLS 19 "Bind `127.0.0.1` by default ... consider a per-run token": superseded by D-07/D-08 (update the doc).
- REQUIREMENTS Out-of-Scope row "Auth / remote access for web UI | Local bench only": narrowed by D-08 ("local" = trusted LAN); update the row.
- 02-SECURITY AR-02-04 premise ("served only on 0600 api.sock"): superseded by redaction on the TCP listener.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Redacting `api.key` on the TCP listener (CLI keeps it over the socket) is acceptable to the user | Pitfall 1 | If the user wants the key in the web API, parity text changes; security review will otherwise flag a HIGH info-disclosure |
| A2 | Behind-real-time = tick `late > 50 ms` or `dt > 50 ms`, cleared after 2 s without such a tick | Pattern 4 | Too sensitive flickers the chip; too lax hides real starvation (UIC-30 is explicitly unresolved) |
| A3 | Host allowlist = IP literals, `localhost`, hostname, `hostname.local`, `--allow-host` | Pitfall 3 | A user reaching the bench by a DNS name sees 403 until they pass `--allow-host` |
| A4 | Cap browser WS at 8 of the 16 shared streams | Pitfall 2 | Too low blocks a 9th tab; too high lets browsers starve the CLI |
| A5 | `sample` rate 10 Hz, ring 3000 (5 min), page max-hold 10 s | Pattern 3, 5 | Defaults come from UI-SPEC; low risk |
| A6 | `style-src 'self'` suffices for uPlot's CSSOM styling | Code Examples (CSP) | Console CSP violations; fallback `'unsafe-inline'` for styles only |
| A7 | `releasePointerCapture` in `pointerdown` restores `pointerleave` on touch | Pitfall 6 | Held cap stays until max-hold; verify with CDP touch events |
| A8 | `down`, `fw/restart`, `sim/restart`, `logs` stay reachable on the LAN listener (accepted open control API) | Pattern 1 | A LAN host can stop the bench; an allowlist of UI routes would be stricter but narrows "same API" |
| A9 | Web-port range 8054..9053 is acceptable although it contains 8086 and 8123 | Pitfall 12 | Sticky port later taken by another service -> `port_in_use` exit 2 (by design) |
| A10 | `github.com/coder/websocket` is legitimate (tool does not support the Go ecosystem; evidence is proxy origin, sum DB, Context7 reputation, GitHub metadata) | Package Legitimacy | A tool-checked verdict is not available |
| A11 | Fan `<option>` value = id, text = name (UI-SPEC says names; `set` needs ids) | Pitfall 8 | Spec wording mismatch only |

## Open Questions

1. **Redact `api.key` on the LAN listener (A1) and should `down`/restarts be reachable there (A8)?**
   - What we know: D-08 accepted an open control API; AR-02-04 relied on the socket; the key unlocks the firmware's native API.
   - What's unclear: whether "open control API" was meant to include the key and process-control operations.
   - Recommendation: redact the key (cheap, no UX cost); keep operations open per D-08 and say so in README and `sil up` stderr; ask the user once at plan review.
2. **Fan select text: name or id?** UI-SPEC copy says names "exactly as returned" and `set portN.fan=<name>`, but the API key is `id`. Recommendation: label = `name`, value/`title` = `id`.
3. **Float64Array vs plain arrays.** UI-SPEC's Performance contract names `Float64Array`; measured behavior requires `null`-capable arrays. Recommendation: plain arrays; update the contract wording.
4. **Behind-real-time threshold.** Resolved as A2 pending user/UAT feedback; the page only renders the flag.
5. **Does the live phone UAT need the CSP `unsafe-inline` fallback?** Settle in the browser test (A6).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | build, all Go tests | ✓ | go1.27.3 linux/amd64 (`/home/claude/.local/bin/go`) | none needed |
| Google Chrome (headless) | browser tests, offline proof, viewport/touch checks | ✓ | 157.0.8103.38 (`/usr/bin/google-chrome`) | none; `TestMain` of `uitest` should fail loudly with a clear message when absent (same style as `e2e`) |
| Docker + pinned ESPHome image | optional full-stack smoke (`sil_e2e`) | ✓ | Docker 29.9.3; `ghcr.io/esphome/esphome:2026.10.1` present | the browser tests use a fake `Control`/engine and need neither |
| Node / npm | one-off vendoring (`npm pack`), optional JS unit tests | ✓ | node v22.23.4, npm 10.9.9 | vendoring needs network once; JS unit tests can run inside Chrome instead |
| Python playwright | (not used) | ✗ | not installed | Go CDP client |
| Network (npm, proxy.golang.org) | vendoring, `go get` | ✓ | reachable this session | n/a; runtime needs none |
| Free TCP ports 8054+ | web port | ✓ with caveats | host listens on 8000, 8002, 8086 | allocator scans for a bindable port first time; sticky afterwards |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** Python Playwright (replaced by the Go CDP harness).

## Validation Architecture

> `.planning/config.json` has `workflow.nyquist_validation: false`, but the orchestrator requires this section explicitly; it is included on that instruction.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib, `-race`), `httptest`, `clock.Manual`; browser tests: headless Chrome 157 over a hand-written CDP client under build tag `sil_ui` |
| Config file | none for Go; Makefile targets (`check`, new `test-ui`); `vet` extended with `-tags sil_ui` |
| Quick run command | `go test -race -count=1 ./internal/web/... ./internal/control/... ./internal/events/... ./internal/engine/... ./internal/instance/... ./internal/cli/... ./internal/lint/... ./ui/...` |
| Full suite command | `make check && make test-ui` (and optionally `make e2e E2E_RUN=UI`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| UI-01 | Page served from the binary: embed contains `index.html`, `/` returns 200 `text/html`, assets have correct MIME, `--ui-dir` serves disk | unit | `go test ./ui/... ./internal/web/... -run 'TestServe|TestEmbed|TestUIDir'` | ❌ Wave 0 |
| UI-01 | No `https?://` in first-party assets; vendored files match `VENDOR.md` sha256 | unit | `go test ./ui/... -run 'TestNoExternalRefs|TestVendorHashes'` | ❌ Wave 0 |
| UI-01 | Works offline: only same-origin requests, no CSP console errors, LINK reaches LIVE, SIM TIME advances | browser | `go test -tags sil_ui ./uitest -run TestOfflineLoadAndLive` | ❌ Wave 0 |
| UI-01 | WS frames equal NDJSON lines of `Watch`; Origin/Host/CSRF rejections; header deadline; stream cap | unit | `go test ./internal/web/... -run 'TestWS|TestGuard|TestSlowHeader|TestStreamCap'` | ❌ Wave 0 |
| UI-02 | `sample` events at 100 ms sim time, `null` when disconnected; ring bounded; `GET /history` consistent with `seq`; Manual-clock 1 h soak keeps ring <= 3000 | unit | `go test ./internal/engine/... ./internal/events/... ./internal/control/... -run 'Sample|History|Soak'` | ❌ Wave 0 |
| UI-02 | Client rings bounded and gap-correct (push 1e6 samples, length <= cap; `null` gap rendered with 0 lit pixels); fan select lists `/fans`, change posts `{"values":{"port1.fan":"<id>"}}`; fixed axes re-scale once on swap | browser | `go test -tags sil_ui ./uitest -run 'TestRingBounded|TestPlotGap|TestFanSelect'` | ❌ Wave 0 |
| UI-03 | LED lens color = gamma(strip[i]); caption hex equals API string; segment names joined per index; lens count = `leds.strip.length` | browser | `go test -tags sil_ui ./uitest -run TestLEDs` | ❌ Wave 0 |
| UI-04 | Mouse hold and touch hold-and-slide-off send `down` with `max_hold_ms` then `up`; cap lights only from server state; env fields send one `set` per commit; server error shown verbatim and field outlined | browser | `go test -tags sil_ui ./uitest -run 'TestUSRHold|TestUSRTouchLeave|TestEnvSet|TestErrorLine'` | ❌ Wave 0 |
| UI-04 | `down` with `max_hold_ms` validates 1..60000, engine auto-releases in sim time, event carries `reason:max_hold`; `sil press --down --max-hold` parses | unit | `go test ./internal/engine/... ./internal/control/... ./internal/cli/... -run 'MaxHold'` | ❌ Wave 0 |
| UI-05 | `behind_real_time` flips on a late tick and clears after 2 s; header fields render `x1`, CONNECTED/DISCONNECTED, BEHIND chip, stale overlay on WS loss, reconnect with backoff | unit + browser | `go test ./internal/engine/... -run Behind` ; `go test -tags sil_ui ./uitest -run 'TestHeader|TestStaleAndReconnect'` | ❌ Wave 0 |
| UI-06 | (a) every `(method, path)` the page uses is in `control.Operations` and has a CLI mapping | unit + browser | `go test ./internal/control/... ./internal/cli/... -run 'TestOperations'` ; `go test -tags sil_ui ./uitest -run TestParityRequests` | ❌ Wave 0 |
| UI-06 | (b) WS first frame minus `t,k,seq` equals `GET /api/v1/status` on the same listener | unit | `go test ./internal/web/... -run TestSnapshotEqualsStatus` | ❌ Wave 0 |
| UI-06 | (c) every `[data-src]` path exists in the status JSON and text matches the formatter; no readout without `data-src` | browser | `go test -tags sil_ui ./uitest -run TestParityDisplayedValues` | ❌ Wave 0 |
| UI-06 | (d) page reducer state equals `GET /status` at quiescence after a scripted session | browser | `go test -tags sil_ui ./uitest -run TestParityPageState` | ❌ Wave 0 |
| (security) | `api.key` absent on TCP, present on socket; rebinding Host rejected; cross-origin POST rejected; CLI works | unit | `go test ./internal/web/... -run 'TestRedact|TestHostGuard|TestCSRF'` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** the quick run command for the touched package(s), plus `go vet ./...`.
- **Per wave merge:** `make check` (fmt, vet incl. `-tags sil_ui`, `go test -race ./...`, C++ codec test) and `make test-ui`.
- **Phase gate:** `make check && make test-ui` green, then one real-stack smoke (`make e2e E2E_RUN=UI`, docker) and the human UAT items (phone on the LAN, Home Assistant reacting) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `ui/embed.go`, `ui/web/**`, `ui/web/vendor/VENDOR.md` + vendored files, `ui/*_test.go` (hash and external-ref tests)
- [ ] `internal/web/` package with `*_test.go` (guard, redact, ws, listener, static)
- [ ] `internal/control/operations.go` and the `History`/`max_hold` types; update `fakeControl` in `internal/control/control_test.go` and `internal/cli/cli_test.go`
- [ ] `internal/events` sample ring + tests; `internal/engine` sample/behind/max-hold + `clock.Manual` tests
- [ ] `uitest/` (build tag `sil_ui`): CDP client, Chrome launcher (`--headless=new --disable-gpu --no-first-run --user-data-dir=<tmp> --remote-debugging-port=0 --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE 127.0.0.1"`, read `DevToolsActivePort`), fake-control server fixture, `TestMain` that fails clearly if Chrome is missing. Must obey the wall-clock lint (use the injected clock for polling; the lint scans test files too).
- [ ] Makefile: `test-ui` target, `vet` adds `-tags sil_ui`, `.PHONY` list
- [ ] Framework install: none (Go stdlib + system Chrome)

## Security Domain

> `security_enforcement` is enabled (`workflow.security_enforcement: true`, ASVS level 1, block on high).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (accepted: no auth, trusted LAN, D-08) | documented accepted risk in README, `sil up` stderr, 03-SECURITY |
| V3 Session Management | no (no sessions) | n/a |
| V4 Access Control | partial | Host allowlist, CSRF/Origin check, key redaction; operations stay open on LAN (A8) |
| V5 Input Validation | yes | existing server-side validators (`PressRequest.Validate`, `parseSet`, 64 KiB body caps, `DisallowUnknownFields`); new `max_hold_ms` and `history` params validated the same way; page does no validation |
| V6 Cryptography | no new | nothing hand-rolled; the Noise key is merely not exposed |
| V13 API / V14 Config | yes | CSP, `nosniff`, `X-Frame-Options: DENY`, `Cache-Control`, JSON error shape for rejections |

### Known Threat Patterns for Go HTTP + WS + static SPA on a LAN

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| CSRF from a page the developer visits (POST `/set`, `/buttons/*/down`) | Tampering | `http.CrossOriginProtection` on non-GET + WS origin check |
| Cross-site WebSocket hijacking | Spoofing/Info disclosure | `websocket.Accept` default origin check, never `InsecureSkipVerify` |
| DNS rebinding to the LAN IP | Spoofing/Info disclosure/Tampering | Host allowlist (Pitfall 3) |
| Firmware API key disclosed via `/status`, `/watch`, `down`/restart replies | Info disclosure | Redaction wrapper on the TCP listener; supersedes AR-02-04 (Pitfall 1) |
| Log disclosure via `/logs` | Info disclosure | existing `clean()` key redaction before the length cut (T-02-16) still applies; now LAN-reachable (accepted with D-08) |
| Slow-header / connection exhaustion on the LAN port | DoS | keep-alives off, Accept-time header deadline via `clock.Deadline`, connection cap 64 (Pitfall 7) |
| Stream-slot exhaustion by zombie WS | DoS | server ping + 8-of-16 browser cap (Pitfall 2) |
| XSS via API-sourced strings (fan names, segment labels, error messages) | Tampering | `textContent` only, never `innerHTML`; strict CSP `script-src 'self'`; first-party code reviewed for `innerHTML`/`eval` |
| Clickjacking of the USR buttons | Tampering | `frame-ancestors 'none'` + `X-Frame-Options: DENY` |
| Path traversal through `--ui-dir` or static handler | Info disclosure | `http.FileServer` rooted FS (cleaned paths); `--ui-dir` is a developer flag; reject non-directories at `sil up` |
| Supply chain of vendored files | Tampering | sha256 + npm sha512 recorded in `VENDOR.md`, re-hashed in `make check`, MIT license shipped |
| Unauthenticated control of the bench from any LAN host | Spoofing/Elevation | **accepted risk (D-08)**; document; treat as accepted, not a finding |

## Sources

### Primary (HIGH confidence)
- Repository code read this session: `internal/control/control.go`, `internal/supervisor/{supervisor.go,watch.go}`, `internal/events/events.go`, `internal/engine/engine.go`, `internal/clock/clock.go`, `internal/instance/{instance.go,port.go,lanhost.go}`, `internal/cli/{cli.go,resolve.go}`, `internal/apperr/*`, `internal/lint/wallclock_test.go`, `Makefile`, `go.mod`, `fans/*.toml`, `.planning/{REQUIREMENTS,STATE}.md`, `03-CONTEXT.md`, `03-UI-SPEC.md`, `02-SECURITY.md`, `.planning/research/PITFALLS.md` (Pitfall 19)
- `$GOROOT/src/net/http/server.go` (Go 1.27.3) lines 1038-1040, 1103, 2058; `go doc net/http.CrossOriginProtection`
- Context7 `/coder/websocket` (AcceptOptions, OriginPatterns, CloseRead) and `/alpinejs/alpine` (CSP build, `unsafe-eval`), `/leeoniya/uplot`
- npm registry (`npm view`, `npm pack`) for uplot 1.6.32 and alpinejs 3.17.5; `proxy.golang.org` and `sum.golang.org` for coder/websocket v1.8.15; GitHub API for repo metadata
- Executed experiments (scratchpad): headless Chrome 157 JS execution and CDP port exposure as uid 1002; `--host-resolver-rules` blocking external fetch; uPlot `null` vs `NaN` gap rendering; uPlot fixed-scale/stepped/dashed config construction

### Verified in-repo values (verbatim, read this session)
- `internal/events/events.go:14-19`: `MaxSubscribers = 16` and `SubscriberBuffer = 1024`; `:23`: `var Kinds = []string{"duty", "rpm", "state", "fan", "button", "env", "leds", "firmware", "sim"}`
- `internal/engine/engine.go:30-31`: `tick         = 10 * time.Millisecond`, `rpmFrameStep = 50 * time.Millisecond`; `:37`: `const RPMEventStep = time.Second`; `:445-447`: `case now := <-ticker.C():` / `dt := now - last`
- `internal/control/control.go:108-114`: `SimStatus{ TimeS float64 `json:"time_s"`; TimeFactor int `json:"time_factor"`; EngineRestarts int; BusProto int }`; `:117-123`: `PortStatus{ Port int; Fan string; Duty float64; RPM float64; State string }`; `:195-201`: `FanInfo{ ID, Name string; MinRPM, MaxRPM float64; BelowMin string }`
- `internal/control/control.go:359-444` routes: `GET /api/v1/status`, `POST /api/v1/down`, `POST /api/v1/sim/restart`, `POST /api/v1/fw/restart`, `POST /api/v1/buttons/{id}/press`, `.../down`, `.../up`, `POST /api/v1/set`, `GET /api/v1/fans`; `:445`, `:456`: `GET /api/v1/watch`, `GET /api/v1/logs`
- `internal/supervisor/supervisor.go:669`: `Key: s.id.Key`; `:673`: `TimeFactor: 1`
- `internal/instance/instance.go:25-34`: `DefaultAPIPort = 6054`, `PortScanSpan   = 1000`, `MinAPIPort     = 1024`, `MaxAPIPort     = 65535`
- `internal/lint/wallclock_test.go:36-44`: `"Server":    setOf("ReadTimeout", "ReadHeaderTimeout", "WriteTimeout", "IdleTimeout")`; context `WithTimeout`/`WithDeadline` and `time.Now/Since/Sleep/After/Tick/NewTimer/NewTicker` are banned; the lint walks every `.go` file except `.git`, `testdata`, `prototype`, `internal/clock`
- `internal/clock/clock.go:104-108`: `case rt.c <- r.Now():` / `default:` (slow receivers miss ticks)

### Secondary (MEDIUM confidence)
- https://www.w3.org/TR/pointerevents3/ section 4.1.3 (implicit capture for direct-manipulation devices; only the first 100 000 characters were read)

### Tertiary (LOW confidence)
- Behind-real-time thresholds, Host allowlist rule, 8/16 stream split, CSP `style-src` sufficiency: design judgment, flagged `[ASSUMED]`

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - versions, hashes and legitimacy checked against registries and executed; `coder/websocket` legitimacy by evidence rather than tool.
- Architecture: HIGH for the reuse of `Control`/broker/ServeListener (read in source); MEDIUM for the new pieces (sample ring, Operations table) that are design proposals consistent with the code.
- Pitfalls: HIGH for NaN/null, vendored `https://`, key exposure, status/fans join (all observed); MEDIUM for touch capture (spec partly read, test required) and the header-deadline trick (verified in source, needs a test because it depends on stdlib internals).

**Research date:** 2026-11-09
**Valid until:** 2026-12-09 for stack versions (uPlot is low-churn; `coder/websocket` active); Go stdlib behavior claims are tied to Go 1.27.3.
