---
phase: 02-full-rev-3-3-bench
updated: 2026-11-09
---

# Phase 02 — Review Disposition

Code review ran with `--fix --auto` and stopped at the cap of 3 iterations. Reports for each iteration:

| Iteration | Review | Fix report |
|-----------|--------|------------|
| 1 | `02-REVIEW.iter2.md` | `02-REVIEW-FIX.iter2.md` |
| 2 | `02-REVIEW.iter3.md` | `02-REVIEW-FIX.iter3.md` |
| 3 (final) | `02-REVIEW.md` | `02-REVIEW-FIX.md` |

The iteration-3 fixes (8542d9d, b976a6c) were not re-reviewed because the loop hit its cap.

| Iter | Finding | Severity | Disposition | Evidence |
|------|---------|----------|-------------|----------|
| 1 | WR-01 symlinked component dir hashes as empty tree (stale firmware cache hit) | warning | fixed | ce2c756 |
| 1 | WR-02 `Set` records inputs before engine applies them; swallows ErrStopped on dead engine | warning | fixed (needs human verification: cancellation semantics changed) | e4f1046 |
| 1 | WR-03 any non-125..127 docker exit reported as `config_invalid` | warning | fixed (deviation: exit 1 or 2 = config_invalid, confirmed by re-review) | 1469f38 |
| 1 | WR-04 engine warning list unbounded | warning | fixed | 291aebe |
| 1 | WR-05 busy `--api-port` persisted as sticky before bind test | warning | fixed | febfe21 |
| 2 | WR-01 `TestPortInUse` e2e broken by WR-05 fix | warning | fixed (single e2e test run green; full e2e not re-run) | 58d3e7e |
| 2 | WR-02 `Set` dead-engine detection keyed on failure code (double fault) | warning | fixed (needs human verification: concurrency/state logic, `engGap` flag) | da1a9ca |
| 3 | WR-01 `TestSupervisorRebootExit` asserts probe count before probe starts (flaky) | warning | fixed (not re-reviewed: iteration cap; 50× -race green) | 8542d9d |
| 3 | IN-10 `engGap` not reset on panic in `SimRestart` | info | fixed (not re-reviewed: iteration cap) | b976a6c |
| 1–3 | IN-01 `light.py` pixel-size comment wrong | info | open | 02-REVIEW.md |
| 1–3 | IN-02 no diagnostic for failing bus connect; `SIL_BUS` bypasses 107-byte check | info | open | 02-REVIEW.md |
| 1–3 | IN-03 control handlers accept trailing data after JSON body | info | open | 02-REVIEW.md |
| 1–3 | IN-04 `lanHostPtr` duplicated | info | open | 02-REVIEW.md |
| 1–3 | IN-05 broker drops final `lagged` marker at stream end | info | open | 02-REVIEW.md |
| 1–3 | IN-06 `tests/silbench.py` dead state, broad `BaseException` catch | info | open | 02-REVIEW.md |
| 1–3 | IN-07 `sim_light.h` missing `<algorithm>` include | info | open | 02-REVIEW.md |
| 2–3 | IN-08 unreachable `default:` arm in `Set`, untested rollback | info | open | 02-REVIEW.md |
| 3 | IN-09 `Set` racing shutdown abort reports `engine_failed` not `stopping` | info | open | 02-REVIEW.md |
