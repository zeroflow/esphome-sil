# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v0.1.0 — MVP

**Shipped:** 2026-11-10
**Phases:** 4 (1, 2, 3, 3.1 inserted) | **Plans:** 36 | **Commits:** 257 over 3 days

### What Was Built
- Static ESPHome `host:` firmware from the repo's own `sim_bus` components, built in the digest-pinned 2026.10.1 image, talking a frozen NDJSON sim-bus to a Go engine with an injectable clock and a `fan.toml` fan model.
- `sil` CLI/JSON API with isolated instances, supervised firmware, resync after restarts, and the full Rev 3.3 IO surface (fans, USR buttons, `partition` LEDs, temperature/humidity), usable from Home Assistant.
- `make test-contract` (CLI-driven contract test plus aioesphomeapi smoke test) and an embedded, CDN-free live web page that is a pure client of the same API.

### What Worked
- Walking skeleton first: Phase 1 pushed one duty/RPM path through every risky layer (image build, static link, sim-bus, engine, supervisor) before widening, so Phase 2 and 3 were additive.
- Injectable clock from day one plus a wall-clock lint: `clock.Manual` made engine, supervisor, web-listener and button-hold tests deterministic, and several flakes turned out to be test races rather than product bugs.
- Shared golden vectors for the Go and C++ codecs kept the hand-rolled C++ parser from drifting.
- One `control.Operations` table behind both listeners plus parity checks kept the page from growing logic the CLI cannot reach.
- Independent re-review (Phase 3.1) of fixes made under an iteration cap found three real defects, each reproduced red under `-race` first.

### What Was Inefficient
- The unattended `/gsd-autonomous` run produced a long "Needs Human" backlog (assumed fan constants, autonomous decisions, deferred UATs) that had to be triaged afterwards; several items are still open.
- Review-fix loops that hit the 3-iteration cap left fixes unreviewed, which needed a separate inserted phase (3.1).
- Verification reports went stale for every phase as later phases and review fixes touched covered files, so the milestone closed on an override instead of fresh verification.
- Code-review incremental scoping would have overwritten a REVIEW.md that still had open Info findings; the fix pass had to be run against the existing report by hand.
- About 40 `fix(...)` commits: review rounds caught a lot, but some of it (missing tests named in plan checks) could have been in the plans.

### Patterns Established
- `[verified]` / `[source]` tags in research, and recorded decisions with rationale in PROJECT.md Key Decisions.
- Every error code registered in `apperr.Registry` and enforced by an AST scan; one JSON value per CLI call.
- Vendored browser assets with recorded sha256/sha512 and a `make check` re-hash; no runtime CDN.
- Tests for race-prone code are written red under `-race` before the fix.
- Known limits are pinned by a test and logged as user-approved decisions, not left implicit.

### Key Lessons
1. Re-run the verifier (or plan for it) after review-fix rounds that touch covered files, before the milestone close, so the close can be a verified one.
2. Do not let a fix loop end at an iteration cap without a scheduled independent re-review; budget it as a plan.
3. Keep autonomous decisions few and surface them at once; a morning list of 20 "needs review" rows is a tax.
4. Hardware-shaped assumptions (fan constants, HA reachability) need a real measurement or live run; proxies (aioesphomeapi) prove the API, not the deployment.

### Cost Observations
- Model mix: not tracked (adaptive profile: opus for planning, sonnet for execution/fixes, haiku for plan checks)
- Sessions: not tracked
- Notable: 36 plans in about 3 days, most under 30 minutes each per STATE.md metrics.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Commits | Phases | Key Change |
|-----------|---------|--------|------------|
| v0.1.0 | 257 | 4 | Walking skeleton + autonomous run; inserted debt phase before close |

### Cumulative Quality

| Milestone | Verification | UAT | Notes |
|-----------|--------------|-----|-------|
| v0.1.0 | 4/4 passed (stale at close, override) | 17/18 passed, 1 skipped (TX cap, later tested in 3.1) | 40/40 requirements |

### Top Lessons (Verified Across Milestones)

1. (Needs a second milestone to cross-validate.)
