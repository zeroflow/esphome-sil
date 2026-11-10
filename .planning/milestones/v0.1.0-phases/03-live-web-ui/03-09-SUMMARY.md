---
phase: 03-live-web-ui
plan: 09
subsystem: ui
tags: [uplot, vendoring, supply-chain, sha256, embed, ui-spec]
status: complete

requires:
  - phase: 03-live-web-ui
    provides: "03-01 ui.FS() over //go:embed all:web; 03-04 TestNoExternalRefs with its vendor/ skip"
provides:
  - "ui/web/vendor: uPlot 1.6.32 (uPlot.iife.min.js, uPlot.min.css), LICENSE.uplot (MIT), VENDOR.md with the npm sha512 and a file/bytes/sha256 table"
  - "ui/vendor_test.go: TestVendorHashes (re-hash from ui.FS(), fails on mismatch, unlisted file, missing file, empty table), TestVendorServed (byte-identical, JavaScript and CSS content types through http.FileServerFS)"
  - "03-UI-SPEC.md amended to the researched stack and the built error behaviour"
affects: [03-10]

actuals:
  tokens: 2500
  tasks: 2
  commits: 2
plan_head_before: e4851b6fd4b2347c25a1a82c1825cddbfd478035
plan_head_after: 9baf61541cc2aa64c8e46dcc62a457c842b215d4
commits: 2

tech-stack:
  added: ["uPlot 1.6.32 (vendored, MIT)"]
  patterns:
    - "third-party browser code enters the repo once, from a hash-checked npm tarball, and is re-hashed on every make check against a table in VENDOR.md"
    - "vendor/ is excluded from the first-party URL scan and covered by hashes instead"

key-files:
  created:
    - ui/web/vendor/uPlot.iife.min.js
    - ui/web/vendor/uPlot.min.css
    - ui/web/vendor/LICENSE.uplot
    - ui/web/vendor/VENDOR.md
    - ui/vendor_test.go
  modified:
    - .planning/phases/03-live-web-ui/03-UI-SPEC.md

key-decisions:
  - "LICENSE.uplot is recorded in the VENDOR.md table (1078 bytes, sha256 8f989229...) so TestVendorHashes also covers the license text and the unlisted-file rule has no exception beyond VENDOR.md itself"
  - "TestVendorHashes checks its own detection against five altered in-memory trees (changed content, changed size, unlisted file, missing file, empty table) so it cannot pass by finding nothing"

requirements-completed: [UI-01, UI-02]

duration: 10 min
completed: 2026-11-09
---

# Phase 3 Plan 09: Vendored uPlot Summary

**uPlot 1.6.32 vendored from a sha512- and sha256-checked npm tarball into the sil binary, re-hashed and served in `make check`, with the UI-SPEC aligned to vanilla ES modules, plain null-gap arrays and the built error text.**

## Accomplishments

- Vendored uPlot 1.6.32 once, with no build step and no install: `npm pack uplot@1.6.32 --ignore-scripts` into a fresh temp directory, tarball sha512 checked, extracted, per-file sha256 and sizes checked, then only the three files copied (UI-01).
- `TestVendorHashes` and `TestVendorServed` run in `make check` (119 passed, 0 failed); the page can load the library from `/vendor/` with no network (UI-01, UI-02 plot library for 03-10).
- `TestNoExternalRefs` stays green: vendor/ is skipped by design (uPlot's credit comment holds a URL that is never fetched, Research Pitfall 5) and covered by the hashes instead.
- UI-SPEC amended (see below).

## Vendored hashes as checked

| item | expected (research) | observed |
|------|--------------------|----------|
| tarball sha512 (`npm view uplot@1.6.32 dist.integrity` and computed) | `sha512-KIMVnG68zvu5XXUbC4LQEPnhwOxBuLyW1AHtpm6IKTXImkbLgkMy+jabjLgSLMasNuGGzQm/ep3tOkyTxpiQIw==` | identical (registry and computed) |
| uPlot.iife.min.js | 51081 B, `19c8d4c6ad88929a79f4ae49d6f7161566dfd0ba3d15cc495e974f787eb78f1f` | identical |
| uPlot.min.css | 1857 B, `df630c6a8d6f8eeaff264b50f73ce5b114f646ffd9a0bb74f049b0a00135fa04` | identical |
| LICENSE.uplot (package/LICENSE) | not in research | 1078 B, `8f989229699b4fe2f1a0432d0e9edc338a8a911e250e2d1b01ecd770a5f5b1bd` (MIT, Leon Sorokin) |

## Task Commits

1. **Task 1 (tracer): vendor uPlot, VENDOR.md, TestVendorHashes, TestVendorServed** - `a4a3176` (feat)
2. **Task 2: UI-SPEC wording** - `9baf615` (docs)

Tracer gate: the tracer `<verify>` (`make check` plus the four named ui tests) is automated-only and passed end to end before expansion; no checkpoint (human_verify_mode end-of-phase).

## UI-SPEC amendment (task 2)

Dated line `Amended 2026-11-09 by 03-09` added under the header. Changes:

- Design System "Component library" and "Vendored assets", Registry Safety: vanilla ES modules and uPlot only; Alpine named only as not used (its standard build needs `unsafe-eval`, the legitimacy check flagged its release); uPlot 1.6.32 with sha256 and npm sha512 recorded in `VENDOR.md`, re-hashed by `TestVendorHashes`.
- Performance contract: plain arrays with `null` gaps; `Float64Array` appears only in the sentence explaining why it is not used (uPlot 1.6.32 draws across `NaN`, Research Pitfall 4).
- Port panel fan select: option text = fan name, value = fan id.
- Extra, as instructed by the orchestrator (03-08 follow-up): Component 7, the Error-state row and UIC-29 no longer say a non-JSON failure shows `HTTP {status} {statusText}`. They now state the built behaviour (D-17, 03-08): the trimmed response body, or nothing when the body is empty; nothing when there was no response at all, where `LINK` reports the connection. This is a correction of stale text to match the implemented and tested behaviour, not a design change; it is noted in the amendment line.

## Deviations from Plan

None - plan executed exactly as written. Two small additions inside the planned files: the LICENSE row in the VENDOR.md table (the plan's own Task 1 text lists LICENSE.uplot in the table), and the non-JSON error wording correction requested in the dispatch notes.

## Known Stubs

None.

## Threat Flags

None. T-03-27 and T-03-SC/03-09 mitigated as planned (sha512 before extraction, sha256 per file, `--ignore-scripts`, fresh temp dir, three files copied, hash test in `make check`).

## Issues Encountered

None. The `actuals.tokens` figure counts authored files only (chars/4 over the test, VENDOR.md and the UI-SPEC diff); the 53 KB of third-party vendored bytes are excluded because they were copied, not written.

## Self-Check: PASSED

- ui/web/vendor holds exactly LICENSE.uplot, VENDOR.md, uPlot.iife.min.js, uPlot.min.css; sha256 and byte counts as above.
- Commits a4a3176 and 9baf615 present; `make check` 119 passed, 0 failed; `make test-pkg PKG=./ui/ RUN='TestVendorHashes|TestVendorServed|TestNoExternalRefs|TestEmbedContents'` ok.
- `grep -c 'Amended 2026-11-09 by 03-09'` = 1; `Float64Array` only in the explaining sentence; Alpine only as not used.
