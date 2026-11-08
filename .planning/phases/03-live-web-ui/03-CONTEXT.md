# Phase 3: Live Web UI - Context

**Gathered:** 2026-11-08
**Status:** Ready for planning

<domain>
## Phase Boundary

A local web page, embedded in the `sil` binary (no build step, vendored libs, no runtime CDN), that watches and drives one running bench live over WebSocket. Per fan port: plugged-in fan (changeable), duty, RPM, live duty/RPM plot with bounded history. Virtual LEDs. USR1-3 press. Temperature/humidity input. Run info (sim time, time factor fixed at 1, firmware connected, behind-real-time warning). The page is a pure client of the same `/api/v1` + WS the CLI uses, and an automated parity check proves it.

Requirements: UI-01, UI-02, UI-03, UI-04, UI-05, UI-06.

Not in this phase: fault switches, plug/unplug faults (P2); fan photos, drag & drop, board view (P3); time factor > 1; auth or user accounts; a multi-instance landing page / always-on daemon.

</domain>

<decisions>
## Implementation Decisions

### Carried forward (not re-discussed)
- One HTTP/JSON API, one operation vocabulary; the CLI is its client (CLI-06, Phase 1). The UI has **no logic of its own**: press timing, history, derived flags (connected, behind real time) all come from the engine/API.
- Press presets 200 ms / 1500 ms and raw `--down`/`--up`; press on a held button is rejected (02 D-01..D-04).
- LEDs = final strip colors + named segments from the HELLO manifest, ≤ 20 Hz (02 D-05..D-08).
- `set key=value`, all-or-nothing, fan swap = instant unplug/replug, defaults 22 °C / 45 %RH (02 D-09..D-11).
- `watch` = snapshot first, then typed events (02 D-12). The WS should carry the same shapes.
- Stack from research: vendored uPlot 1.6.32 (+ Alpine 3.17.5 or vanilla JS, planner's call), system font stack, `--ui-dir` dev flag, charts and typed arrays kept outside any reactive proxy.

### Visual language
- **D-01:** The character is a **modern Keysight (InfiniiVision-style) bench oscilloscope**, classic HP/Keysight instrument, explicitly **not** modern/neon "cyberpunk". Dark grey bezel frame. Black screen areas with a faint graticule grid. Crisp sans-serif labels with **tabular numerals** for readouts. Grey, softkey-like buttons. Sober and professional. This is a style reference only: no vendor names, logos or trademarks on the page.
- **D-02:** **Dark only.** One tuned palette, no light theme, no toggle.
- **D-03:** **Scope channel colors per port:** port 1 yellow, port 2 green, port 3 blue, port 4 pink/magenta. The color identifies the port everywhere (plot traces, port label, readouts).
- **D-04:** **Virtual LEDs:** round "lens" dots with a soft glow in the actual strip color. Off is a dark lens; dim is visibly dim (apply a simple display gamma). Under each LED: segment name (from the manifest) and `#rrggbb`, exactly what `sil status` reports.
- **D-05:** **Density: one screen, no scroll on a desktop (~1280 px and up, 1080p laptop).** Layout: a header bar with run info and a message line; a grid of the 4 port panels (2×2 or 4×1, planner's call); a bottom strip with LEDs, USR buttons and environment inputs. Like an instrument front panel.
- **D-06:** **PC first, phone usable.** Below desktop width, panels stack in one column and the page scrolls. Touch targets are at least 44 px; plots, readouts and buttons stay usable. No separate phone design.

### Reaching the page
- **D-07:** The instance's HTTP server (API + WS + page) **binds on all interfaces (LAN) by default**, so a PC or phone on the LAN can open it. A `--listen <addr>` flag (e.g. `127.0.0.1`) narrows it. This **changes the research/Phase 1 assumption of a 127.0.0.1-only API**. The planner reconciles with what Phase 1 built. — **Reversibility:** costly — consumer scripts and the CLI's discovery (`run.json`) will assume the address and port; changing the default later changes what users and agents connect to.
- **D-08:** **No auth, no token (trusted LAN).** The user explicitly accepted an open control API on the LAN. Keep the cheap protections that cost no UX: **Origin check on the WS upgrade and on mutating requests** (blocks CSRF from pages the developer visits). Document clearly in the README and in `sil up` output that the API is unauthenticated on the LAN. Note: this narrows REQUIREMENTS "Auth / remote access for web UI: out of scope (local bench only)". "Local" now means the trusted LAN. The planner should update that row and PITFALLS 19's "bind 127.0.0.1" advice accordingly. The security review should treat open-on-LAN as an accepted risk, not a finding to fix.
- **D-09:** **Sticky web port per instance**, picked once and stored in the identity dir like the API port (Phase 1 D-07), from a distinct range (e.g. starting at 8054). It is the same single HTTP server the CLI uses; no second listener. `--web-port` overrides and updates it. A taken sticky port fails with exit 2 and a JSON error, never a silent move. `sil up` and `sil status` print the full URL using the host's LAN IP. — **Reversibility:** costly — bookmarks, phone shortcuts and scripts depend on the stored port.
- **D-10:** **WS loss → auto-reconnect with a stale overlay.** The header shows "disconnected, retrying". Readouts and LEDs dim/grey as stale and keep their last values (no fake zeros). Reconnect with backoff. On reconnect, the page takes a fresh snapshot + history and the plots repaint. No manual reload, including across `sil sim restart`.

### Plots
- **D-11:** **4 small scope screens, one per port**, inside each port panel. RPM is the solid trace and duty a thinner or dashed trace in the same channel color, on a second axis.
- **D-12:** **Rolling 60 s sim-time window.** The server keeps a bounded history ring (about 5 min) behind `GET /api/v1/history` (a CLI-reachable operation as well), so a reload or reconnect repaints immediately. Memory stays flat on both sides over a long run.
- **D-13:** **Fixed axes:** RPM 0 to the plugged fan's `max_rpm` (exposed via status/the fan library), duty 0-100 %. A fan swap re-scales once. No autoscale.

### Buttons & inputs
- **D-14:** **USR1-3 are real press-and-hold buttons** on the page: pointerdown → `press --down`, pointerup → `--up`. The firmware measures hold time, so a long press comes from holding. No preset softkeys on the page (presets stay in the CLI). The button cap lights from the **server-reported** held state, not from local JS state.
- **D-15:** **Stuck-hold guard.** The page sends `up` on pointerup, pointercancel, pointerleave, window blur and tab hidden. In addition, **`down` takes a max-hold parameter** in the API (e.g. `max_hold=10s`; the CLI gets it too as `sil press usr1 --down --max-hold 10s`). The engine auto-releases in sim time and emits that as an event in watch/status. The page always sends a max hold, and the planner picks the value. The safeguard lives in the API, not in JS.
- **D-16:** **Temperature/humidity** are numeric entry fields, applied on Enter or with an apply softkey: one `set` per commit, no stream of intermediate values. The **fan per port** is a dropdown in each port panel, filled from a fan-library list operation in the API. It applies immediately (instant swap per 02 D-10), with no confirm.
- **D-17:** **Errors** go to a scope-style **message line in the header bar**. It shows the API's JSON error message verbatim for a few seconds and puts a red outline on the offending field, which keeps the user's input. No UI-invented messages.

### Claude's Discretion
- **Parity check mechanism (UI-06):** not discussed. Leaning: the UI reads only `/api/v1/*` + `/ws`, the WS frames reuse the exact Go types of `status`/`watch`, and an automated test asserts both (e.g. a Go test that diffs the WS snapshot against `GET /api/v1/status`, plus a check that every endpoint the JS calls is in the operation list). A headless browser test (Playwright, Python side under `tests/`) is allowed if the planner finds it worth the dependency. It must also verify "works with the network blocked" (no `http(s)://` in the served assets).
- "Simulator behind real time" warning: the definition and threshold (computed in the engine, exposed in status), and how it is shown when everything is fine at x1 (the field is present now, so the layout does not move later).
- Exact palette values, graticule density, font sizes, LED gamma, max-hold value, the history ring length and sample rate, the WS coalescing rate (research: snapshot every ~50 ms, drop-oldest per client), and Alpine vs vanilla.
- A `/gsd-ui-phase 3` pass may refine the visual contract (UI-SPEC.md) within D-01..D-06.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope and requirements
- `.planning/PROJECT.md` — core value, constraints (no build step, no runtime CDN, vendored plot lib, Go single binary)
- `.planning/REQUIREMENTS.md` — UI-01..UI-06; Out of Scope rows "UI-only capabilities" and "Auth / remote access for web UI" (the latter is narrowed by D-07/D-08)
- `.planning/ROADMAP.md` §Phase 3 — goal and the 5 success criteria (acceptance gate)
- `.planning/phases/01-closed-loop-fan-skeleton/01-CONTEXT.md` — identity dir, sticky API port pattern (D-06/D-07) reused for the web port; supervisor/engine split; CLI-05 exit codes
- `.planning/phases/02-full-rev-3-3-bench/02-CONTEXT.md` — press, LED, set and watch semantics (D-01..D-12) that the UI must reuse unchanged

### Research
- `.planning/research/STACK.md` — uPlot 1.6.32 / Alpine 3.17.5 vendoring (files, sizes), `coder/websocket`, `embed` + `--ui-dir`, reactive-proxy pitfall, "browser stack decision"
- `.planning/research/ARCHITECTURE.md` — `api.Control` (Status, Set, Press, History, Subscribe, Logs), `state.Hub` snapshot fan-out, observation path, Anti-Pattern 3 (logic in the UI)
- `.planning/research/FEATURES.md` §"Web UI (minimum)" — per-port panel, plot, LEDs with brightness/gamma, run info, reconnect, temp fields
- `.planning/research/PITFALLS.md` Pitfall 19 (WS backpressure, memory, UI-only logic, local security) and Pitfall 7 (LED colors are final, brightness-scaled pixels)
- `docs/initial-idea.md` §"Simple web UI in P1" — original minimum

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- No Go or UI code exists yet. Phases 1-2 will deliver the engine, `/api/v1`, `run.json`, `status`/`set`/`press`/`watch` and the snapshot types the UI renders. Phase 3 adds the WS adapter (if not already there), `history`, a fan-library list operation, `max_hold` on press, and the static `ui/` tree.
- `prototype/` has no UI.

### Established Patterns
- Identity-stored sticky port with exit-2-on-conflict (Phase 1 D-07). D-09 copies it for the web port.
- stdout JSON / stderr progress; exit codes 0/1/2 (CLI-05). New CLI flags (`--listen`, `--web-port`, `--max-hold`) follow them.

### Integration Points
- `ui/` with its own `embed.go` (`go:embed` cannot reach parent dirs), served by the instance's single HTTP server.
- `GET /api/v1/history`, the fan library list, and `press` with `max_hold` join the shared operation vocabulary (CLI + UI).
- `sil up` / `sil status` output gain the page URL (LAN IP + web port).

</code_context>

<specifics>
## Specific Ideas

- "Less modern cyberpunk, more classic HP/Keysight": think InfiniiVision front panel, with the screen, graticule, channel colors and softkeys. Restraint over glow. The LED glow is the one place light effects are wanted.
- The phone use case: standing at the rig and poking the bench from the phone. That is why the default bind is LAN, and why buttons are real holds with 44 px+ targets.
- Fixed RPM axis to `max_rpm` mirrors a scope's fixed V/div, so curves are comparable across ports and over time.

</specifics>

<deferred>
## Deferred Ideas

- Selectable timebase softkeys (10 s / 60 s / 5 min): later UI polish (P3).
- One shared scope screen comparing all ports: P3.
- Per-instance token / capability URL: available if the trusted-LAN assumption ever changes.
- Multi-instance landing page: needs an always-on process, which M1 rules out (RUN-03 `sil list` covers discovery later).
- Light theme: not planned.

</deferred>

---

*Phase: 03-live-web-ui*
*Context gathered: 2026-11-08*
