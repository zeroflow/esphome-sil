# API Coverage — Phase 4 (Fan Model v2)

No external API integration: Phase 4 extends the plant model, the `fan.toml` schema and this project's own control API; no third-party API is added.

- New fields (tach, airflow, power, provenance, seed) are exposed through the project's own control API (`control.Operations`, `control.NewFanInfo`), the CLI renderer and the existing web page, which stays a client of that API.
- The ESPHome native API is consumed only by the existing e2e helpers (e.g. `test_firmware_sees_tach_steps` via `bench.api(...)`, `TestFansProvenance`) as the Home Assistant reaction proxy, with the same per-instance port and key as every other e2e instance (T-04-21). No new capability is used; the per-capability decisions stay as recorded in `../../milestones/v0.1.0-phases/02-full-rev-3-3-bench/COVERAGE.md`.
- Vendor datasheet pages cited as provenance in `fans/*.toml` are documentation sources, not runtime APIs.
