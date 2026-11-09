---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 0
total_count: 1
last_updated: 2026-11-09T13:00:55.904Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 3 | stub | ui/web/index.html |  | readout value elements carry the placeholder -- and the #ports container is empty until 03-02 adds view.js (render and formatters) | open |  | 2026-11-09T13:00:55.904Z |  |

````json
[
  {
    "id": 1,
    "kind": "stub",
    "phase": "3",
    "file": "ui/web/index.html",
    "line": null,
    "description": "readout value elements carry the placeholder -- and the #ports container is empty until 03-02 adds view.js (render and formatters)",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-11-09T13:00:55.904Z",
    "resolved_at": null,
    "milestone": null
  }
]
````
