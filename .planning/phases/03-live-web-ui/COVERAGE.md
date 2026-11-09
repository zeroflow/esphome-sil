# API Coverage — Phase 3 (Live Web UI)

No external API integration: Phase 3 adds no third-party API. Its HTTP and WebSocket surface is this project's own control API (the `control.Operations` table) served on a second transport, the instance's TCP web listener, plus an embedded browser page that is a client of that API.

- The ESPHome native API (firmware `api:`, Noise-encrypted) is consumed only through the unchanged Phase 2 e2e helpers (`e2e/api_client.py` in the pinned image, used by `setFan` and `waitFwEnv`), as the Home Assistant reaction proxy in `TestWebPlot`, `TestWebHold` and `TestWebAcceptance`. No new capability is used; the per-capability decisions stay as recorded in `../02-full-rev-3-3-bench/COVERAGE.md`.
- The Chrome DevTools Protocol client in `internal/cdptest` drives the locally installed Chrome for tests only. It is test tooling, never part of the `sil` product surface.
- `github.com/coder/websocket` and the vendored uPlot are libraries, not remote APIs; their provenance is handled in the plans' supply-chain rows (T-03-SC, T-03-27).
