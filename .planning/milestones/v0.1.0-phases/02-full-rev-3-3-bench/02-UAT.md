---
status: complete
phase: 02-full-rev-3-3-bench
source: [02-VERIFICATION.md]
started: 2026-11-09T10:45:00Z
updated: 2026-11-10T09:29:44Z
---

## Current Test

[testing complete]

## Tests

### 1. Live Home Assistant UAT (criterion 4, HA-01, HA-02)
steps: |
  1. `bin/sil up ha-a`; note `api.lan_host`, `api.port`, `api.key` from `sil status ha-a`
  2. In Home Assistant (ideally on another LAN machine): Settings → Devices → Add Integration → ESPHome, host = lan_host, port, encryption key
  3. Set Fan 1 to 40 % in HA; check `sil status` ports[0].duty == 0.4 and Fan 1 Speed ≈ 790 RPM in HA
  4. `sil set temperature=27 humidity=60`; `sil press usr1` (and `--down`/`--up`); watch HA follow
  5. `sil down ha-a && sil up ha-a`; same device reconnects, no duplicate
  6. `sil up ha-b`; appears as a separate device
  7. Delete both devices in HA; `sil down ha-a ha-b`
  (Full checklist: 02-08-SUMMARY.md, section "Pending Live Home Assistant UAT".)
expected: Device `SiL ha-a` with 13 entities, no lights; duty 0.4 / ~790 RPM; sensors and button follow within ~1 s; stable identity across restart; ha-b separate
result: pass

### 2. Review-fix concurrency changes (da1a9ca, e4f1046; not re-reviewed: 8542d9d, b976a6c)
steps: |
  Read `Set` and `SimRestart` in internal/supervisor/supervisor.go (the `engGap` flag, the `WithoutCancel` + shutdown
  context on `Apply`) and the tests TestSetAfterAFailedRestartWithAHeldFailureFails / TestSetInARestartGapIsKept.
expected: Set during a simulator restart gap is kept and seeds the next engine; a dead engine returns engine_failed with the record rolled back; shutdown returns stopping; no deadlock between inMu and mu
result: pass
note: "pass (accepted for now, note somewhere)" - accepted on the pre-review summary (race-tested TestSet*/TestSimRestart* x3, lock order inMu->mu at all three inMu sites); 8542d9d and b976a6c still not independently re-reviewed"

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]

## Deferred Follow-Ups

- test: 2
  idea: "Accepted for now: independent re-review of the Set/SimRestart concurrency fixes (8542d9d, b976a6c not re-reviewed after the review iteration cap; da1a9ca, e4f1046)"
  deferred_at: 2026-11-10
