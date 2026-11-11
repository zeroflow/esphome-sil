---
phase: 04-fan-model-v2
plan: 03
subsystem: fan-library
tags: [go, fanlib, fan.toml, provenance, sil-fans, toml-loader, located-errors]

requires:
  - phase: 04-fan-model-v2
    provides: "04-01 schema 2 loader (loadV2, validateV2, Params.Provenance/Sources, ValuePaths, Kinds, lineOf)"
provides:
  - control.NewFanInfo, the single FanInfo builder, and FanInfo.{Schema, Provenance, Values, Sources}
  - "`sil fans` stdout JSON with schema, provenance counts and the full per-value map; stderr summary per fan and --verbose value list"
  - fanlib.locate, a table-aware line scanner used by every schema 2 error
  - schema 2 caps, [tach], [datasheet] and [measured] validation and mapping into Params.Tach/Params.Datasheet
  - explicit `schema = 1` through the moved v1 code (v1.go); schema >= 3 refused as needing a newer sil
affects: [04-04, 04-07, 04-08, 04-09, 04-10]

requirements-completed: [MODEL-08, MODEL-12]

plan_head_before: b22d848dfe8a10feccc803a9a9293c335471797e
plan_head_after: 62b1385d3bb6a45c1027d5340f14fad8a8cd9a20

actuals:
  tokens: 18800
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "One FanInfo builder (control.NewFanInfo); schema 1 reports null provenance, never made-up provenance"
    - "locate(data, table, key) matches a key by its full dotted path, so repeated key names (duty, rpm) resolve inside their own table"
    - "Human view of a JSON command goes to stderr (dropped by --quiet); stdout stays exactly one JSON value"

key-files:
  created:
    - internal/fanlib/v1.go
  modified:
    - internal/control/control.go
    - internal/control/control_test.go
    - internal/supervisor/supervisor.go
    - internal/cli/cli.go
    - internal/cli/cli_test.go
    - e2e/fans_test.go
    - internal/fanlib/fanlib.go
    - internal/fanlib/fanlib_test.go
    - internal/fanlib/v2.go
    - internal/fanlib/v2_test.go

key-decisions:
  - "04-01's lineOf/entryLine/tableHeader are replaced by one locate scanner that matches full dotted paths (bare, quoted, dotted-in-table, header fallback) and skips multi-line array bodies"
  - "[measured] is accepted as an optional table; measured.tolerance is a value with provenance, date/board/esphome/recording_sha256 are record identifiers with none (A7)"
  - "v1 files get Params.Schema 1 (the 04-01 test literal in fanlib_test.go gained Schema: 1)"
  - "Provenance value cap is checked before the kind, so an oversized entry is reported as too long, not as an unknown kind"

patterns-established:
  - "Schema 2 rule order (a) required keys, (b) finite, (c) ranges and caps, (d) curve shape, (e) D-10 floor, (f) provenance; documented at the top of v2.go"
  - "Sorted iteration over maps (sources, provenance) so the first reported error is stable"

coverage:
  - id: D1
    description: "`sil fans` prints one JSON value with one key `fans`; each entry has schema, provenance counts, a per-value map and sources (null for a schema 1 fan), for every fan whatever flags are given"
    requirement: MODEL-12
    verification:
      - kind: unit
        ref: "internal/control/control_test.go#TestNewFanInfo"
        status: pass
      - kind: unit
        ref: "internal/cli/cli_test.go#TestFansSummaryOnStderr"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN='TestFansProvenance|TestFanChoice' (e2e/fans_test.go#TestFansProvenance)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The stderr summary line per fan and the --verbose value list in ValuePaths order; --quiet drops both"
    requirement: MODEL-12
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestFansSummaryOnStderr"
        status: pass
      - kind: e2e
        ref: "e2e/fans_test.go#TestFansProvenance"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every schema 2 semantic error names its key and carries the line of that key inside its own table; caps (64 points, 32 sources, 512/128/64 bytes, delay 60, tau 600, rpm 100000) are enforced and located"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2ErrorsAreLocatedInTheirTable"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2Caps"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2FirstErrorIsStable"
        status: pass
    human_judgment: false
  - id: D4
    description: "[tach], [datasheet] and [measured] are validated when present; [tach] and [datasheet] map into Params; measured.tolerance needs provenance and the four record keys take none"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2TachTable"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2DatasheetTable"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2MeasuredTable"
        status: pass
    human_judgment: false
  - id: D5
    description: "Explicit schema = 1 loads through the unchanged v1 path with Params.Schema 1; schema 3 says it needs a newer sil; schema 0 and below are refused; adjacency and empty-input edge cases"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestSchemaProbeVariants"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2Adjacency"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2Empty"
        status: pass
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go (v1 tests unchanged apart from Schema: 1 in the TestLoadValidFile literal)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Prohibition: sil never fetches, resolves or links a [sources] entry (load time, sil fans)"
    requirement: MODEL-12
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2LoadNeverFetchesASource"
        status: pass
      - kind: unit
        ref: "internal/cli/cli_test.go#TestFansNeverDereferencesSources"
        status: pass
    human_judgment: false
  - id: D7
    description: "A fan file is read once per load and loading is a pure function of the file bytes, so editing a file under SIL_FAN_DIR never changes a plant that is already running (backstop truth)"
    requirement: MODEL-08
    verification: []
    human_judgment: true
    rationale: "Backstop truth, no new test: loadFrom reads the file once with io.ReadAll and the engine holds the loaded Params; replug and sil up load again. The plan marked it verification: backstop."

duration: 25min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 03: Fan Provenance in `sil fans` and Schema 2 Loader Hardening Summary

**`sil fans` now reports each fan's schema and the provenance of every value (counts, per-value map and sources in the JSON, a summary and a `--verbose` list on stderr), and the schema 2 loader reports every error at its own line through a table-aware scanner, with size caps and checked [tach], [datasheet] and [measured] tables.**

## Performance

- **Duration:** about 25 min (start time not recorded at launch; the ledger base commit is `b22d848`)
- **Completed:** 2026-11-11T05:55Z
- **Tasks:** 2 (1 tracer, 1 TDD), plus a small follow-up test commit for the sources prohibition
- **Files modified:** 11 (1 created, 10 modified)

## Accomplishments

- Tracer through all layers: `fanlib` Params, `supervisor.Fans` (via `control.NewFanInfo`), the control API, the CLI renderer and a real-instance e2e (`TestFansProvenance`, 4.6 s). Stdout stays one JSON value with one key (`TestFanChoice/fans_lists_the_library` unchanged and green).
- Schema 1 fans report `schema: 1` and JSON `null` for provenance, values and sources (D-06: nothing invented); a schema 2 fan without `[sources]` reports `{}`.
- `locate` replaces 04-01's `lineOf`/`entryLine`: it follows `[headers]`, matches bare, quoted and dotted keys by full path, falls back to the header line for a table-valued key, and skips multi-line array bodies so an element like `[1]` cannot be taken for a header.
- Loader hardening: 64 points, 32 sources, 512-byte texts, 128-byte names, 64-byte provenance values, delay 60 s, tau 600 s, rpm 100000; `[tach]` (ppr 1..16, noise 0..1000), `[datasheet]` (each > 0, capped), `[measured]` (five non-empty strings); the v1 code moved into `v1.go` and accepts `schema = 1`.

## Task Commits

1. **Task 1: `sil fans` shows schema and per-value provenance (tracer)** - `75dc1d2` (feat)
2. **Task 2 RED: failing tests for located errors, caps and the optional tables** - `c0ca9e5` (test)
3. **Task 2 GREEN: loader hardening, v1.go move, schema probe** - `76b98b6` (feat)
4. **Prohibition test: sources are never fetched** - `62b1385` (test)

**Plan metadata:** committed with this SUMMARY (docs).

The tracer feedback gate (auto mode) re-ran the tracer `<verify>` (`make test-pkg` on control/cli/supervisor, `make check`, `make e2e E2E_RUN='TestFansProvenance|TestFanChoice'`) before Task 2 began and passed.

## Files Created/Modified

- `internal/control/control.go` - `FanInfo` additions, `ProvenanceCounts`, `ValueProvenance`, `NewFanInfo`
- `internal/supervisor/supervisor.go` - `Fans` builds entries with `control.NewFanInfo`
- `internal/cli/cli.go` - `FansCmd.Verbose`, `writeFansSummary` (stderr), new command help
- `internal/fanlib/v1.go` (new) - `rawFan` (with `Schema`), `loadV1`, `validate`, `keyLine`, moved verbatim
- `internal/fanlib/fanlib.go` - schema probe dispatch (absent/1 to v1, 2 to v2, above 2 and below 1 refused), package doc
- `internal/fanlib/v2.go` - `locate`, caps, `checkOptionalTables`, `[tach]`/`[datasheet]` mapping, documented key list and rule order
- tests: `control_test.go`, `cli_test.go`, `fanlib_test.go` (one literal), `v2_test.go`, `e2e/fans_test.go`

## Decisions Made

- `locate` matches the full dotted path rather than a (table, key) pair, which makes dotted keys inside a table (`down.duty` under `[curve]`) and quoted provenance keys work with one mechanism. The header line is the fallback for table-valued keys and for cap errors on a whole table (`sources`).
- Whitespace-only `[measured]` strings count as empty, like an empty `name`.
- The stderr `--verbose` line is `    <path padded to 22>  <kind>` then, with a source, `  <id> <text>`; the text is printed, never opened.

## Edge Assumption Review (execution-time, user decision 2026-11-11)

- **A7** ([measured] accepted now as an optional table of five strings, tolerance a value): reviewed against `TestV2MeasuredTable` (all five present with a tolerance entry loads and tolerance is counted under its kind; missing or empty key of each of the five fails naming it; an entry for date, board, esphome or recording_sha256 fails as naming no value; 513 bytes fails; no table loads) and `TestV2ProvenanceRules`. Behaviour implemented exactly as the assumption states. Resolution: accepted for this phase; stays flagged for Phase 6/8, which may need to amend the key set (cost: one validation table in `checkOptionalTables`, one `ValuePaths` entry).
- **A-D08**: already resolved by the user on 2026-11-10; implemented (stdout full, human text on stderr).
- Plan-level edge truths reviewed against their tests: adjacency (`TestV2Adjacency`: zero-width band loads, equal curve duty rejected at that curve's `duty` line, equal rpm loads, duplicate provenance key is a located TOML error with a column, `Stop.Duty`/` stop.duty`/`stop.duty `/`STOP.DUTY` name no value), empty input (`TestV2Empty`), and ordering (`TestV2FirstErrorIsStable`, 25 repetitions, sorted-first of two bad kinds). All hold.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Existing tests pinned the old FanInfo shape and the old [measured] and probe behaviour**
- **Found during:** Tasks 1 and 2
- **Issue:** `control_test.go#TestFansRoute` expected the five-key JSON body; `v2_test.go#TestV2ProvenanceRules` built a `[measured]` with only `tolerance` (now invalid: all five keys are required when the table is present); `v2_test.go#TestV2SchemaProbe` asserted "schema must be 2" and that `schema = 1` fails (the opposite of this plan's behaviour).
- **Fix:** `TestFansRoute` expects the additive body (`"schema":0,"provenance":null,...` for a zero Params); the measured case gets the other four keys; `TestV2SchemaProbe` is replaced by `TestSchemaProbeVariants`.
- **Files modified:** internal/control/control_test.go, internal/fanlib/v2_test.go
- **Commits:** `75dc1d2`, `c0ca9e5`

**2. [Rule 1 - Bug] "v1 files get Params.Schema 1" conflicts with TestLoadValidFile's literal**
- **Found during:** Task 2 GREEN
- **Issue:** the plan says both that v1 loads get `Params.Schema` 1 and that the v1 tests in `fanlib_test.go` pass unmodified. `TestLoadValidFile` compares with `reflect.DeepEqual` against a literal without Schema.
- **Fix:** added `Schema: 1` to that one literal. Every v1 assertion and line number is otherwise untouched. The acceptance command `git diff --exit-code v0.1.0 -- internal/fanlib/fanlib_test.go` could not hold in any case: 04-01 already changed that file (`reflect.DeepEqual` instead of `!=`, documented in its deviation 2). The diff against HEAD before this plan is now exactly one line.
- **Files modified:** internal/fanlib/fanlib_test.go
- **Commit:** `76b98b6`

**3. [Rule 2 - Missing critical] No test for the "never dereference a source" prohibition**
- **Found during:** Task 2 close-out
- **Issue:** the prohibition is declared `verification: test` but no task listed a test for it.
- **Fix:** `TestV2LoadNeverFetchesASource` (httptest server as the source URL, zero hits while loading) and `TestFansNeverDereferencesSources` (printed verbatim on stdout and stderr, zero hits).
- **Files modified:** internal/fanlib/v2_test.go, internal/cli/cli_test.go
- **Commit:** `62b1385`

---

**Total deviations:** 3 auto-fixed (2 Rule 1, 1 Rule 2)
**Impact on plan:** none on scope; two are consequences of the new behaviour on older test pins, one closes a verification gap of a declared prohibition.

## TDD Gate Compliance

Task 2 (`tdd="true"`): RED commit `c0ca9e5` `test(04-03)` precedes GREEN commit `76b98b6` `feat(04-03)`. At RED, `TestV2Caps`, `TestV2TachTable`, `TestV2DatasheetTable`, `TestV2MeasuredTable`, `TestSchemaProbeVariants`, the dotted-key case of `TestV2ErrorsAreLocatedInTheirTable` and two cases of `TestV2Empty` failed on assertions for the planned behaviour (no error, wrong key or wrong message); none failed on compile errors or zero-test discovery. `TestV2Adjacency`, `TestV2FirstErrorIsStable` and the other located-error cases passed at RED because 04-01's `lineOf` already located them; they are acceptance tests for behaviour that 04-01 shipped and this plan keeps, as in 04-01. Separate `gsd_run check tdd-red-evidence` records were not produced; the failing output is as listed. No REFACTOR commit. Task 1 is a tracer and was committed once with its tests.

## Issues Encountered

None beyond the deviations above.

## Known Stubs

None. `Params.Tach` and `Params.Datasheet` are now populated by the loader; their behaviour (tach pulses, datasheet readouts) lands in 04-04 and 04-06 as already recorded in 04-01.

## Threat Flags

None. T-04-06 (caps, mitigated by `TestV2Caps`), T-04-07 (sources plain text, `TestV2LoadNeverFetchesASource`, `TestFansNeverDereferencesSources`) and T-04-08 (required provenance, `TestV2ProvenanceRules`, `TestV2Empty`, null for schema 1) are covered. No new network or file surface.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-07 (uitest fake bench) can switch its literal `FanInfo` to `control.NewFanInfo`; the literal still compiles.
- 04-08 (shipped fans to schema 2) and 04-10 (README) can rely on: every value needs a provenance entry; `measured.tolerance` is a value, the other four `[measured]` keys are not; caps and the key list at the top of `internal/fanlib/v2.go`.
- Verification run: `make check` green, `make e2e E2E_RUN='TestFansProvenance|TestFanChoice|TestFanSwap|TestModelV2|TestFanTomlUnknownKey'` green.

## Self-Check: PASSED

Created and modified files exist on disk (`internal/fanlib/v1.go`, `v2.go`, `internal/control/control.go`, `e2e/fans_test.go`); commits `75dc1d2`, `c0ca9e5`, `76b98b6`, `62b1385` exist; `control.NewFanInfo(` matches one line in supervisor.go; `type rawFan` is in v1.go only; `TestFansSummaryOnStderr`, `TestV2ErrorsAreLocatedInTheirTable` and `TestFansProvenance` run and pass without "no tests to run".
