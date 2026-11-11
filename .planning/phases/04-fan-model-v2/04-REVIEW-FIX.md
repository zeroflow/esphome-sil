---
phase: 04-fan-model-v2
fixed_at: 2026-11-11T14:50:00Z
review_path: /home/claude/esphome-sil/.planning/phases/04-fan-model-v2/04-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-11-11T14:50:00Z
**Source review:** /home/claude/esphome-sil/.planning/phases/04-fan-model-v2/04-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6 (fix_scope: all)
- Fixed: 6
- Skipped: 0

## Fixed Issues

### WR-01: D-10 "never stops" rule is enforced on `curve.up` only; a `curve.down` can drop a never-stopping fan to 0 rpm

**Files modified:** `internal/fanlib/v2.go`, `internal/fanlib/v2_test.go`
**Commit:** 6ebeb3f
**Applied fix:** Rule (e) now also requires the first `curve.down` point to be at duty 0 with rpm > 0 when `stop.duty = 0` and a down curve is present. The error key is `curve.down.duty`, located at the down curve's own duty line. The `curve.up` error keeps its existing key (`stop.duty`). `TestV2NeverStopsFormDerivesHold` now uses a never-stops file whose down curve carries the floor, and it covers three rejected down curves plus a never-stops file without a down curve. The shipped `fans/noctua-nf-a12x25-pwm.toml` (the only never-stops file) already satisfies the rule and still loads (`TestShippedNoctuaFan`).
**Status note:** logic change (validation rule), requires human verification.

### WR-02: Control characters in `name`, `[sources]` and `[measured]` strings are accepted and printed raw to the terminal

**Files modified:** `internal/fanlib/v2.go`, `internal/fanlib/v2_test.go`, `internal/cli/cli.go`, `internal/cli/cli_test.go`
**Commit:** fdac7ae
**Applied fix:** Both halves of the suggested fix. The loader rejects any `unicode.IsControl` rune (tab and newline included) in `name`, every `[sources]` text and every `[measured]` string, with a located error "must not contain control characters". `writeFansSummary` passes the fan id, name, source id and source text through a new `terminalSafe` helper, which prints the text unchanged when it is clean and Go-quoted when it holds a control character. Normal output is byte-identical, so the existing summary tests are untouched. The quoting also covers schema 1 names, which the loader does not check. Tests: `TestV2RejectsControlCharacters` (loader) and `TestFansSummaryQuotesControlCharacters` (CLI).

### WR-03: `locate` loses the line number when a string value holds an escaped quote followed by a bracket

**Files modified:** `internal/fanlib/v2.go`, `internal/fanlib/v2_test.go`
**Commit:** 34ddfd9
**Applied fix:** `bracketDelta` is replaced by a small `valueScan` state machine. It skips the character after a backslash inside basic strings, treats literal (single-quote) strings as escape free, and follows multi-line basic (`"""`) and literal (`'''`) strings across lines, so neither bracket characters inside strings nor header-like lines inside a multi-line string are misread. This goes slightly beyond the review's one-line suggestion because the review also flagged untracked triple-quoted strings. `TestLocateStringsAndEscapes` adds an end-to-end case (`name = "a \" [ b"` then a bad `stop.duty` keeps its line) and nine direct `locate` cases. I confirmed the new test fails against the previous `locate` (7 failing subtests) and passes now.
**Status note:** logic change (scanner), requires human verification.

### IN-01: Stale comment on `engine.Config.Seed`

**Files modified:** `internal/engine/engine.go`
**Commit:** 39c2f09
**Applied fix:** Reworded the comment: the seed keys the tach noise of every port's plant, a replug or an engine restart rebuilds the plant with the same seed, and the noise stream restarts at bucket 0 because buckets are counted in plant time. Comment only, no test.

### IN-02: `Plant.Params()` hands out slices that alias the plant's own parameters

**Files modified:** `internal/model/fan.go`, `internal/model/plant.go`, `internal/model/fan_test.go`
**Commit:** a9760fe
**Applied fix:** The deep-copy option. New `Params.Clone()` copies the curves, provenance, sources and datasheet (nil stays nil). `NewPlant` stores a clone of its argument and `Plant.Params()` returns a clone, so neither the caller's value nor the returned value aliases the running model. `Params()` has no callers today, and `NewPlant` runs only at engine start and on a replug, so there is no per-tick cost. `TestPlantOwnsItsParams` mutates both the source and the returned value and checks that the plant is unchanged.

### IN-03: A schema 2 file with the `schema` key missing gets a schema 1 "unknown field" error

**Files modified:** `internal/fanlib/fanlib.go`, `internal/fanlib/v2_test.go`
**Commit:** 62091f8
**Applied fix:** For a file with no `schema` key, `loadFrom` passes a v1 "unknown field" error through `hintSchema2`. If the first element of the field path is a schema 2 table (`sources`, `stop`, `start`, `dynamics`, `curve`, `tach`, `datasheet`, `measured`, `provenance`), the message gets "(a schema 2 file needs schema = 2 at the top)" appended. v0.1.0 semantics are unchanged. An explicit `schema = 1` and a plain schema 1 typo such as `max_rmp` keep their old messages. Test: `TestMissingSchemaKeyHintsAtSchema2`.

## Skipped Issues

None.

## Verification

Verification ran in an isolated git worktree under `.claude/worktrees/` (since removed). It does not depend on any state outside git, so it reproduces from the main checkout at HEAD 62091f8.

- `go vet ./...`: clean (this also compiles the e2e and uitest packages)
- `gofmt -l .`: no files listed
- `go test ./internal/...`: all packages pass
- The e2e suite was not run, because no fix touched e2e code.
- Shipped fan files (`fans/*.toml`) still load: `TestShippedNoctuaFan` and the arctic equivalent pass with the stricter validation.
- `bin/sil` was not rebuilt and the running `uat` instance was not touched. It still runs the old loader until the maintainer rebuilds.

The six fix commits were fast-forwarded onto `claude/main`. REVIEW-FIX.md is not committed; the orchestrator handles that.

---

_Fixed: 2026-11-11T14:50:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
