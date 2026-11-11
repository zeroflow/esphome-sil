---
phase: 04
review: 04-REVIEW.md
titles: json
findings:
  - id: WR-01
    severity: warning
    disposition: open
    title: "D-10 \"never stops\" rule is enforced on `curve.up` only; a `curve.down` can drop a never-stopping fan to 0 rpm"
  - id: WR-02
    severity: warning
    disposition: open
    title: "Control characters in `name`, `[sources]` and `[measured]` strings are accepted and printed raw to the terminal"
  - id: WR-03
    severity: warning
    disposition: open
    title: "`locate` loses the line number when a string value holds an escaped quote followed by a bracket"
  - id: IN-01
    severity: info
    disposition: open
    title: "Stale comment on `engine.Config.Seed`"
  - id: IN-02
    severity: info
    disposition: open
    title: "`Plant.Params()` hands out slices that alias the plant's own parameters"
  - id: IN-03
    severity: info
    disposition: open
    title: "A schema 2 file with the `schema` key missing gets a schema 1 \"unknown field\" error"
open: 6
total: 6
recorded: 2026-11-11T13:24:39.269Z
---

# Phase 04: Code Review Disposition

| Finding | Severity | Disposition | Source |
|---------|----------|-------------|--------|
| WR-01 | warning | open | - |
| WR-02 | warning | open | - |
| WR-03 | warning | open | - |
| IN-01 | info | open | - |
| IN-02 | info | open | - |
| IN-03 | info | open | - |

Dispositions: `open` (recorded, not yet triaged), `fixed`, `skipped`, `deferred`.
Set `deferred` by hand and put the reason in the Source cell; both are preserved. A `|` in the reason is kept as prose and escaped on the next run.
Re-running the gate keeps every row it can. A row the current review no longer reports is kept and its Source cell flagged, so a finding does not leave this record silently. ONE exception: when a finding id is REUSED by a different finding, the earlier decision cannot keep a row — the id is taken — and it is dropped. A RECORDED decision (anything but `open`) is named on the console when that happens; a row still at `open` is replaced silently, because `open` records no decision to lose.
