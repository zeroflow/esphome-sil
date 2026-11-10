---
phase: 03-live-web-ui
updated: 2026-11-09
---

# Phase 03 — Review Disposition

The code review ran with `--fix --auto` and converged in iteration 3: the final `03-REVIEW.md` has status `clean` (0 critical, 0 warning). The intermediate review and fix reports were removed on convergence; the commits below are the record.

| Iter | Finding | Severity | Disposition | Evidence |
|------|---------|----------|-------------|----------|
| 1 | CR-01 snapshot of a failed instance (Status.error) treated as stream error frame → page never renders, reconnect loop | critical | fixed | dc6c3ad (uitest TestFailedInstanceSnapshotRenders, web TestWSSnapshotOfFailedInstanceKeepsK) |
| 1 | WR-01 browser stream cap counted only on /ws; `GET /api/v1/watch` on the LAN listener uncapped | warning | fixed | 75a92ed (TestLANWatchSharesTheBrowserBudget) |
| 1 | WR-02 `button.up` without fetch keepalive; release on tab hide/close can be cancelled | warning | fixed | 633a59b (TestUSRUpSurvivesPageTeardown) |
| 1 | WR-03 fast re-press can send down before the previous up arrives | warning | fixed (client per-cap promise chain) | 4bd7507 (TestUSRRepressWaitsForUp) |
| 2 | WR-01 no request timeout; a stalled button request wedges the cap's chain | warning | fixed (needs human verification: timed-out-down semantics) | bfbb808 (TestUSRStalledRequestTimesOut) |
| 1–3 | IN-04 history fetch has no timeout | info | fixed | bfbb808 |
| 1–3 | IN-01 `commit()` discards edits typed while the request is in flight | info | open | 03-REVIEW.md |
| 1–3 | IN-02 static handler lists directories; `--ui-dir` served on the LAN bind; `MaxHeaderBytes` default 1 MiB | info | open | 03-REVIEW.md |
| 1–3 | IN-03 unused `maxRestartGap`, duplicate `lanHostPtr`, odd scheme string in `WebURL` | info | open | 03-REVIEW.md |
| 1–3 | IN-05 SIM TIME can step back by up to one tick (`engine.go:548` assigns `simNow` unconditionally) | info | open | 03-REVIEW.md |
| 2–3 | IN-06 tab closed while a down is in flight loses the release until the 10 s max hold | info | open | 03-REVIEW.md |
| 3 | IN-07 a timed-out down applied >3 s late makes the next press fail `button_busy` (not reachable on a local sim) | info | open | 03-REVIEW.md |
