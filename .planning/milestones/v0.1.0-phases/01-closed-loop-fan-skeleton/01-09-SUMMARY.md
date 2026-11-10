---
phase: 01-closed-loop-fan-skeleton
plan: 09
subsystem: instance-isolation
tags: [go, noise, flock, xdg, ports, identity, readiness, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "instance.{PathsFor, LoadOrCreate, ResetRunDir, RunFile}, lifecycle.Up, supervisor.Serve, control.Status"
  - phase: 01-closed-loop-fan-skeleton (plan 04)
    provides: "api.reboot_timeout 0s and the encrypted native API in the example firmware"
  - phase: 01-closed-loop-fan-skeleton (plan 08)
    provides: "fwbuild.Build with identity substitutions, hash skip and lint"
provides:
  - "instance: strict ValidateName messages, CheckSocketPaths, StateRoot, EnsureRunParent, EqualMAC, ProcessAlive, AllocatePort, TestBind, PortHolder, LockInstance, LockAlloc"
  - "identity.json validated on every load (schema, name, 32-byte key, port range); identity_corrupt with a reset hint, never regenerated"
  - "sticky, collision-free API port from 6054 under a global flock; --api-port override with invalid_argument / port_claimed; port_in_use with the holder pid and command"
  - "apiclient.{Probe, Hello, ProbeError}: Noise_NNpsk0 handshake plus HelloRequest/HelloResponse, no protobuf dependency"
  - "supervisor readiness: api.ready = real encrypted handshake with the instance key and name/MAC match; phase ready additionally needs the bus"
  - "lifecycle.Up order: pure checks, lock, liveness, identity/port, test-bind, fresh run dir, build, test-bind, serve"
  - "e2e TestTwoInstances, TestPrefixNames, TestPortInUse, TestPortClaimed, TestFreshPrefs, TestReadyIsEncrypted (+ idle_hold), TestAlreadyRunningAndBusy, TestNamesAndPaths, helper isoAutoPortLock"
affects: [01-10, 01-11, 01-12, 01-13]

actuals:
  tokens: 17200
  tasks: 2
  commits: 2
plan_head_before: fa89dbdbe9dc412b9b88d69386749d75d3f5a128
plan_head_after: e77209a39e68078f0ecca9f9bc622a80141b19e3

tech-stack:
  added: []
  patterns:
    - "flock (LOCK_EX|LOCK_NB) locks: per-instance <state>/sil/<name>/.lock held for the whole `sil up`, global <state>/sil/.alloc.lock around identity creation and port allocation; the kernel releases them on exit"
    - "Probe policy on the supervisor clock: every 250 ms for 20 s, 2 s per attempt; plaintext or rejected answers fail at once, dial/timeout/protocol errors retry"
    - "Generation-counted readiness probe (startProbe) so a later firmware restart (01-11) can reset api.ready and supersede the previous probe"
    - "Pure checks before any state is written: a refused `sil up` leaves no identity, lock or run dir behind"

key-files:
  created:
    - internal/instance/port.go
    - internal/instance/lock.go
    - internal/apiclient/probe.go
    - internal/apiclient/probe_test.go
    - internal/supervisor/supervisor_test.go
    - e2e/isolation_test.go
  modified:
    - internal/instance/instance.go
    - internal/instance/instance_test.go
    - internal/lifecycle/lifecycle.go
    - internal/supervisor/supervisor.go
    - internal/deps/pin.go

key-decisions:
  - "Ready means the instance's own firmware completed a Noise handshake with the instance key and reported name sil-<name> and the derived MAC (case and colon insensitive) in both the server hello and the HelloResponse; a TCP connect never counts (T-01-16)"
  - "A plaintext or rejected answer on the API port fails readiness immediately (it is not our firmware, waiting cannot fix it); only dial, timeout and protocol errors are retried until the 20 s budget ends in ready_timeout"
  - "A sticky or explicit port that is taken is port_in_use (exit 2) and is never moved; an explicit --api-port is stored before the test-bind, so a later plain `sil up` names the same port until the user chooses another"
  - "Liveness (run.json PID alive with `serve <name>` in /proc cmdline) is checked before LoadOrCreate so --api-port cannot rewrite the identity of a running instance; a live PID whose control socket does not answer still counts as already_running (detail control_responding=false) rather than wiping a live run dir"
  - "Error codes unified to the plan's list: unsafe_runtime_dir (was run_dir_unsafe), invalid_argument (was invalid_port), identity_corrupt (was identity_invalid)"

requirements-completed: [BUILD-01, BUILD-04, BUILD-05, BUILD-06]

coverage:
  - id: D1
    description: "Two named instances run side by side with their own run dir, port from 6054, 02: MAC, sil-<name> device name and key; a fan command on one leaves the other's duty unchanged; sil down of each stops all four processes, and a restarted instance keeps port, key and MAC"
    requirement: "BUILD-05"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTwoInstances"
        status: pass
    human_judgment: false
  - id: D2
    description: "Sticky collision-free port allocation: lowest free port >= 6054 not claimed by another identity, --api-port override, invalid_argument, port_claimed naming the owner, port_in_use naming port and holder, no silent move, concurrent creations get distinct ports"
    requirement: "BUILD-05"
    verification:
      - kind: unit
        ref: "internal/instance/instance_test.go#TestAllocatePort, TestLoadOrCreate, TestConcurrentCreatesGetDistinctPorts, TestTestBindAndPortHolder"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestPortInUse|TestPortClaimed"
        status: pass
    human_judgment: false
  - id: D3
    description: "Strict names, disjoint prefix dirs, XDG_RUNTIME_DIR fallback, socket_path_too_long, unsafe_runtime_dir, identity_corrupt never regenerated, MAC derivation and case/colon-insensitive comparison"
    requirement: "BUILD-05"
    verification:
      - kind: unit
        ref: "internal/instance/instance_test.go#TestValidateName, TestPathsForRuntimeFallback, TestCheckSocketPaths, TestLoadOrCreateCorruptIsNeverRegenerated, TestMAC, TestResetRunDirRefusesUnsafeParent"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestNamesAndPaths|TestPrefixNames"
        status: pass
    human_judgment: false
  - id: D4
    description: "Fresh run dir per sil up (marker in prefs/ gone after down and up) and ESPHOME_PREFDIR=<run>/prefs in the firmware environment"
    requirement: "BUILD-04"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestFreshPrefs"
        status: pass
    human_judgment: false
  - id: D5
    description: "Concurrent and repeated sil up: second concurrent up is instance_busy (flock), up of a running instance is already_running, stale or foreign run.json PIDs are treated as not running"
    requirement: "BUILD-01"
    verification:
      - kind: unit
        ref: "internal/instance/instance_test.go#TestLocks, TestProcessAlive"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestAlreadyRunningAndBusy"
        status: pass
    human_judgment: false
  - id: D6
    description: "sil up reports ready only after a real Noise handshake with the instance key and an identity match; wrong key cannot connect while the right one works; with no API client the firmware keeps running for 10 s (same pid, restarts 0, last_exit null, phase ready)"
    requirement: "BUILD-06"
    verification:
      - kind: unit
        ref: "internal/apiclient/probe_test.go#TestProbeSuccess, TestProbeWrongKey, TestProbePlaintextAPI, TestProbeSilentServerTimesOut, TestProbeClosedPort; internal/supervisor/supervisor_test.go#TestMismatch"
        status: pass
      - kind: e2e
        ref: "make e2e E2E_RUN=TestReadyIsEncrypted/idle_hold"
        status: pass
    human_judgment: false
  - id: D7
    description: "api_identity_mismatch when another process (or another instance's firmware) answers on the instance's port, observed end to end"
    requirement: "BUILD-06"
    verification:
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestMismatch"
        status: pass
    human_judgment: true
    rationale: "The identity check is unit-tested and the test-bind catches the realistic port race before the build, but no e2e provokes a foreign process answering a valid Noise handshake on the port (it needs a firmware that lost the bind race); a maintainer may want a manual spot check"

duration: 15min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 09: Instance Isolation and Real Readiness Summary

**Every `sil` instance now has a strict name, its own XDG dirs, a sticky collision-free API port from 6054, a stable MAC and key, flock-guarded concurrent `sil up`, and is "ready" only after a real Noise_NNpsk0 handshake with its own key plus a matching name and MAC.**

## Performance

- **Duration:** about 15 min
- **Started:** 2026-11-09T02:03Z (approx.)
- **Completed:** 2026-11-09T02:18Z
- **Tasks:** 2 (both `auto`, TDD not enforced for this plan)
- **Files:** 6 created, 5 modified; 2 task commits
- **Verification:** `make check` (112 unit tests, race) and the full `make e2e` (22 tests, 213 s) pass. The nine plan-listed e2e tests plus `TestReadyIsEncrypted/idle_hold` print `--- PASS`.

## Accomplishments

- **Identity and ports (D-06..D-09):** `LoadOrCreate` validates schema, name, 32-byte key and port range and answers a bad file with `identity_corrupt` and a reset hint, without touching it. Creation and port changes run under the global allocation lock; `AllocatePort` takes the lowest bindable port from 6054 that no other `identity.json` claims, `--api-port` is range-checked and checked against other instances (`invalid_argument`, `port_claimed` naming the owner), and a taken port is `port_in_use` with the holder's pid and command from `/proc/net/tcp{,6}` and `/proc/*/fd`.
- **Safe paths and locks:** `XDG_RUNTIME_DIR` unset, empty or relative falls back to `/tmp/sil-$UID/<name>` (verified 0700 and owned, else `unsafe_runtime_dir`); socket paths over 107 bytes are `socket_path_too_long` before anything is built; `LockInstance` makes the second concurrent `sil up` `instance_busy`; liveness uses the PID plus `/proc/<pid>/cmdline` so a stale or reused PID is not "running".
- **Real readiness (BUILD-06):** `apiclient.Probe` speaks the Noise native-API handshake with `flynn/noise` and a hand-written protobuf reader (no new dependency). The supervisor probes every 250 ms for up to 20 s after the firmware starts; `api.ready` needs a successful handshake and a name/MAC match, and `phase: ready` additionally needs the bus. Plaintext, rejected, mismatch and timeout each end in `phase: failed` with their own code, which `sil up` returns unchanged (exit 2).
- **Isolation proven end to end:** two auto-port instances side by side (distinct dir, port, MAC, name, key; independent duty; all four PIDs gone after both downs; a restarted instance keeps port, key and MAC), prefix names with disjoint dirs, fresh prefs, encrypted ready with a 10 s idle hold, wrong key refused.

## Task Commits

1. **Task 1: Strict identity, sticky collision-free ports, safe run dirs and locks in `sil up`** - `1895c72` (feat)
2. **Task 2: Real encrypted readiness (Noise probe with identity check) and the bench-level isolation tests** - `e77209a` (feat)

**Plan metadata:** the `docs(01-09)` commit that adds this SUMMARY, STATE.md and ROADMAP.md follows.

## Files Created/Modified

- `internal/instance/port.go` - `AllocatePort`, `TestBind`, `PortHolder`, claimed-port scan
- `internal/instance/lock.go` - `LockInstance` (instance_busy), `LockAlloc` (10 s clock-based wait)
- `internal/instance/instance.go` - strict names, `CheckSocketPaths`, `StateRoot`, `EnsureRunParent`, `EqualMAC`, `ProcessAlive`, validated `LoadOrCreate`
- `internal/instance/instance_test.go` - table tests for every behavior in the plan
- `internal/lifecycle/lifecycle.go` - new `Up` order, `checkNotRunning`, two `TestBind` calls
- `internal/apiclient/probe.go` (+ `probe_test.go`) - Noise probe with a fake responder test
- `internal/supervisor/supervisor.go` (+ `supervisor_test.go`) - probe loop, `startProbe`, identity `mismatch`, status wiring
- `internal/deps/pin.go` - flynn/noise blank import removed (now imported for real)
- `e2e/isolation_test.go` - the nine e2e tests and `isoAutoPortLock`

## Decisions Made

See `key-decisions`. Wire-level facts worth keeping: the first client write is `01 00 00` followed by the handshake frame `01 <len16> 00 <msg1>`; the server hello is `01 <name>\0 <mac>\0` (MAC lower-case without colons, hence `EqualMAC`); psk placement 0 with `HandshakeNN`; the initiator encrypts with the first returned cipher state and decrypts with the second.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Liveness check moved ahead of `LoadOrCreate`**
- **Found during:** Task 1 (Up order design)
- **Issue:** the plan's order (`LoadOrCreate`, then liveness) would store `--api-port N` into the identity of an instance that is already running and about to be refused with `already_running`, leaving identity and live firmware inconsistent.
- **Fix:** `checkNotRunning` runs first. A live PID whose control socket does not answer is still `already_running` (detail `control_responding: false`) so a live run dir is never wiped.
- **Files modified:** internal/lifecycle/lifecycle.go
- **Verification:** `TestAlreadyRunningAndBusy`
- **Committed in:** 1895c72

**2. [Rule 2 - Missing critical] Pure checks and the fan check before anything is written**
- **Found during:** Task 1 (01-05 summary note, `TestCLIErrors` "no state" property)
- **Issue:** `fanlib.Load` ran after `LoadOrCreate`, so a rejected fan file left `identity.json`; creating the instance lock first would also leave a state dir after `config_not_found`, `socket_path_too_long` or `unsafe_runtime_dir`.
- **Fix:** `Up` runs name, paths, `CheckSocketPaths`, `resolveConfig`, `fanlib.Load` and the new `instance.EnsureRunParent` before `LockInstance`; a refused `up` leaves no state (`TestNamesAndPaths` asserts it).
- **Files modified:** internal/lifecycle/lifecycle.go, internal/instance/instance.go
- **Committed in:** 1895c72

**3. [Rule 3 - Blocking] Error codes renamed to the plan's list**
- **Found during:** Task 1
- **Issue:** 01-03 used `run_dir_unsafe`, `invalid_port`, `identity_invalid`; the plan's interface names `unsafe_runtime_dir`, `invalid_argument`, `identity_corrupt`.
- **Fix:** renamed in code and in the existing unit test (no e2e or other plan referenced the old names).
- **Files modified:** internal/instance/instance.go, internal/instance/instance_test.go
- **Committed in:** 1895c72

**4. [Plan wording] `EqualMAC` is exported**
- **Issue:** the plan's behavior list says `equalMAC`, but the supervisor (another package) needs it.
- **Fix:** exported as `instance.EqualMAC`.

**5. [Plan wording] Files outside `files_modified`**
- `internal/deps/pin.go` lost its flynn/noise blank import (the file's own header says to delete it when the real import lands; go.mod and go.sum are unchanged), and `internal/supervisor/supervisor_test.go` was added to unit-test the identity check.

**6. [Plan wording] Plaintext and rejected answers fail readiness at once**
- The plan lists them as failure outcomes without a delay; they are not retried, since waiting cannot turn a foreign or plaintext listener into the instance's firmware. Dial, timeout and protocol errors are retried until `ready_timeout`.

---

**Total deviations:** 3 auto-fixed (1 bug, 1 missing critical, 1 blocking) plus 3 wording-level notes
**Impact on plan:** no scope creep; every plan truth is covered by a passing test.

## Issues Encountered

- None blocking; all tests passed on the first full run.
- An explicit `--api-port P` that turns out to be taken is stored before the test-bind (D-07: it "updates the stored port"), so a later plain `sil up <name>` reports `port_in_use` for P until the user passes another `--api-port`. This is consistent with "never silently move" but may surprise; flagged for 01-12 (CLI polish).
- `api_identity_mismatch` is covered by a unit test of the check and by the early test-bind, not by an e2e that makes a foreign firmware answer a valid handshake (see coverage D7).

## Known Stubs

None.

## Threat Flags

None. The new network surface is the supervisor's client connection to 127.0.0.1:<api port> (no listener added). T-01-16..18 are mitigated as planned (identity-checked Noise readiness; `unsafe_runtime_dir` for foreign or loose `/tmp/sil-$UID`; 0600/0700 identity files, key never in errors or logs: the probe takes raw key bytes and its errors and the supervisor logs carry no key material).

## Needs Human

Nothing blocks. Optional: a manual spot check of `api_identity_mismatch` (start two instances configured with the same API port by editing one `identity.json`, bypassing the claim check) if the maintainer wants end-to-end evidence beyond the unit test.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- **01-11 (auto-restart):** call `supervisor.startProbe()` after every firmware restart; it cancels the previous probe and resets `api.ready`. `Status.Firmware.Restarts` is still never incremented (the idle-hold test only asserts 0).
- **01-10 (sim restart):** the engine restart leaves `api.ready` true (the native API is independent of the bus); `phase` returns to `starting` while the bus is down.
- **01-12:** a stale `run.json` (supervisor SIGKILLed) is already treated as not running by `sil up`; `sil status` on it still reports `control_unreachable` and could map to `phase: dead` / `not_running` (Pitfall 12).

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

## Self-Check: PASSED

All created files exist, both task commits (1895c72, e77209a) are in the log, `make check` and the full `make e2e` pass.
