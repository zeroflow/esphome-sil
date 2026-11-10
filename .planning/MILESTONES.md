# Milestones

## v0.1.0 MVP (Shipped: 2026-11-10)

**Delivered:** Real ESPHome Rev 3.3 firmware runs closed-loop against simulated fans, buttons, LEDs and temperature/humidity on the developer's own machine, driven from the `sil` CLI/JSON API, Home Assistant and a live local web page.

**Phases completed:** 4 phases (1, 2, 3, 3.1 inserted), 36 plans, 72 tasks
**Timeline:** 2026-11-08 → 2026-11-10 (3 days), 257 commits, `d4c08fc..v0.1.0`
**Code:** ~37k lines Go, ~1.9k C++ (sim_bus components and tests), ~1.4k Python (ESPHome codegen, contract tests), ~1.6k first-party JS
**Requirements:** 40/40 v1 requirements complete (see `milestones/v0.1.0-REQUIREMENTS.md`)

**Key accomplishments:**
- Static `host:` firmware built in the digest-pinned ESPHome 2026.10.1 image from the repo's own `sim_bus` components, talking a frozen NDJSON sim-bus (golden vectors shared by the Go and C++ codecs) to a Go engine with an injectable clock and a `fan.toml` fan model.
- `sil up/status/down` with isolated named instances (own XDG dirs, sticky ports, MAC, key), real Noise handshake readiness, supervised firmware that reboots like a device, and resync after either side restarts.
- The full Rev 3.3 IO surface: USR1-3 presses through the firmware's own `on_click` logic, `partition` LED colours in status, temperature/humidity, fan swaps from a library, `sil watch` / `sil logs`, and Home Assistant control by host:port and key.
- `make test-contract`: one command proves the button → duty → RPM → LED chain and the HA-visible entity set through aioesphomeapi.
- An embedded, CDN-free web page as a pure client of the same API (one `control.Operations` table, parity checks), with uPlot scopes per port, virtual LEDs, button holds and environment controls behind a Host allowlist, CSRF protection and a CSP.
- Phase 3.1 cleared the audit's main tech debt: held buttons are released when the engine stops, the Set/SimRestart fixes were re-reviewed (three real defects fixed), and the hub's 64 KiB TX cap is now tested.

**Closeout:** override_closeout
**Known verification overrides:** VERIFICATION.md is `stale` for all four phases (later phases and review fixes touched files earlier verifiers covered); accepted on passed UATs and green test gates (user decision 2026-11-10). 1 newly acknowledged, 0 carried forward from a prior close (see STATE.md Deferred Items).

**Tech debt carried forward** (from `milestones/v0.1.0-MILESTONE-AUDIT.md`, minus what Phase 3.1 fixed):
- Page `MAX_HOLD_MS` (10 s) equals the USR2 `on_click max_length`; a hold reaching the backstop may fall outside the firmware's window.
- The page does not show `Status.error`, warnings, firmware restarts or last exit.
- Live Home Assistant UAT accepted on the aioesphomeapi proxy; LAN reachability from a real HA unverified.
- Known limit (user-approved 2026-11-10): a page that misses the release frame sends one refused best-effort `button.up` after a sim restart.
- Open review info items from Phases 1 and 2 (SIGHUP vs `nohup sil up`, broad compile-failure marker, `SIL_BUS` bypassing the 107-byte path check, and others); `control.OperationByName`/`MatchOperation` have test-only callers.

---
