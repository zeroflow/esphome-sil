---
phase: 03-live-web-ui
fixed_at: 2026-11-09T18:00:00Z
review_path: .planning/phases/03-live-web-ui/03-REVIEW.md
iteration: 2
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 3: Code Review Fix Report

**Fixed at:** 2026-11-09
**Source review:** .planning/phases/03-live-web-ui/03-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 1 (WR-01; IN-* out of scope, except IN-04 which the same change covers)
- Fixed: 1
- Skipped: 0

Work was done on the main tree (branch `claude/main`, no worktree, no stash), one commit.

## Fixed Issues

### WR-01: One stalled button request blocks every later press of that cap

**Files modified:** `ui/web/js/api.js`, `ui/web/js/buttons.js`, `ui/web/js/app.js`, `uitest/bench_test.go`, `uitest/hold_test.go`
**Commit:** bfbb808
**Applied fix:**
- `call()` takes `timeoutMs`. With it, the request gets an `AbortController` and a timer; the timer is
  cleared in a `finally` once the answer, body included, has been read. The abort rejects with the
  browser's AbortError, which is not an `ApiError`, so nothing is shown. Without `timeoutMs` the
  behaviour is unchanged. The response handling moved into a small `perform()` helper.
- `buttons.js`: `REQUEST_TIMEOUT_MS = 3000` (real time, far below the 10 s `MAX_HOLD_MS`) on both
  `button.down` and `button.up`. The reviewer's `AbortSignal.timeout` was replaced by AbortController
  plus a timer, as requested.
- Semantics, written as a comment on `down()`: a timed-out down counts as a down that never registered,
  like a refused one. It resolves `false`, so the cap is not holding, the release of that press sends no
  up (it would only be refused as `button_not_held`), and the chain moves on so the next press goes
  through. Nothing is shown (D-17: the message line carries only server text, and an abort has none;
  LINK reports the connection). The page never lights a cap itself, only the server's report does. If
  the server did apply that down after all, the cap shows the hold the server reports and the engine's
  max hold (D-15) releases it; a later press would then get the server's own `button_busy` text.
- A timed-out up needs no extra handling: `.catch(say)` ignores non-`ApiError` rejections and the chain
  continues. The hold it belonged to is then released by the engine's max hold.

**Logic change, requires human verification:** the state handling after a timeout (what the cap and
`holding` mean for a down the page gave up on, and the "server applied it late" case) is a semantic
decision. The tests cover the no-apply case only.

**Test:** `uitest.TestUSRStalledRequestTimesOut`, with a new bench helper `StallNext(op)` (the next call
of a button op is recorded, then neither answered nor applied until the client aborts). The test presses
USR1, releases and presses again while the first down hangs, and checks:
- the second down is not sent while the first hangs, and the cap is not lit;
- the second down reaches the bench at least 2.5 s after the first (only after the timeout), and the
  hold stands (lit cap);
- no up was sent for the timed-out down;
- the message line is empty and LINK reads LIVE;
- the release sends exactly one up (two downs, one up in total).

Seen to fail without the timeout (timeout set to 600 s: "waiting for 2 button.down calls, got 1") and
to pass with it.

**IN-04 (covered by the same change):** `call("history", ...)` now passes `timeoutMs: HISTORY_TIMEOUT_MS`
(10 s, in `app.js`). The abort rejects into the existing `failJoin`, which resets the scopes and plays the
held samples, so a hung history request no longer holds the plot back. No dedicated test: the timeout is
10 s of real time, and `failJoin` itself is the already-tested error path of the join. IN-04 is therefore
resolved by this commit.

## Verification

Gates ran in the main checkout (`/home/claude/esphome-sil`, branch `claude/main`), not in a worktree, so
the numbers are reproducible from this tree:
- `make check`: passed (fmt-check, vet, all Go packages, test-cpp 119 passed).
- `make test-ui`: passed (all uitest tests, including the new one).
- `make e2e E2E_RUN='TestWebHold|TestWebAcceptance'`: both passed.

---

_Fixed: 2026-11-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
