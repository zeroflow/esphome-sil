---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 1
total_count: 2
last_updated: 2026-11-09T13:58:18.589Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 3 | stub | ui/web/index.html |  | readout value elements carry the placeholder -- and the #ports container is empty until 03-02 adds view.js (render and formatters) | fixed |  | 2026-11-09T13:00:55.904Z | 2026-11-09T13:09:04.669Z |
| 2 | 3 | stub | ui/web/index.html |  | empty .scope well per port panel is a placeholder until 03-10 draws the uPlot scope in it | open |  | 2026-11-09T13:58:18.589Z |  |

````json
[
  {
    "id": 1,
    "kind": "stub",
    "phase": "3",
    "file": "ui/web/index.html",
    "line": null,
    "description": "readout value elements carry the placeholder -- and the #ports container is empty until 03-02 adds view.js (render and formatters)",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-11-09T13:00:55.904Z",
    "resolved_at": "2026-11-09T13:09:04.669Z",
    "milestone": null
  },
  {
    "id": 2,
    "kind": "stub",
    "phase": "3",
    "file": "ui/web/index.html",
    "line": null,
    "description": "empty .scope well per port panel is a placeholder until 03-10 draws the uPlot scope in it",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-11-09T13:58:18.589Z",
    "resolved_at": null,
    "milestone": null
  }
]
````
