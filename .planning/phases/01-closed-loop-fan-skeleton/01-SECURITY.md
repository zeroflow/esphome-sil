---
phase: "01"
slug: "closed-loop-fan-skeleton"
status: draft
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
block_on: high
created: "2026-11-09"
---

# Phase 01 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register built from the `<threat_model>` blocks of 01-01..01-13 PLAN.md plus the `## Threat Flags` / `## Threat Surface` sections of the SUMMARYs. T-01-SC is declared in every plan; it is consolidated into one row at the highest declared severity (high, from 01-02 and 01-09).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| local user -> sil CLI | instance names, `--config`, env (`SIL_INSTANCE`, `SIL_FAN_DIR`, `SIL_REPO`, XDG vars) become filesystem paths and docker arguments | paths, argv (untrusted input) |
| sil -> docker / pinned ESPHome image | builds run the image as the caller's uid; image trusted only by digest; repo/YAML tree mounted read-only | API key (env only), YAML tree |
| user YAML -> docker mount scope / component codegen | the config decides which host tree is mounted and which socket path and channels are compiled in | host files (read-only), socket path |
| ESPHome output -> logs and error messages | stage outputs and build dir embed the API key | API key (secret) |
| shared toolcache -> every instance build | library archives copied between instance data dirs | third-party archives |
| firmware native API (0.0.0.0:port) -> LAN | any LAN host can reach the API port; Noise-encrypted with per-instance PSK | HA API traffic |
| TCP port -> sil readiness probe | anything bound to the port may answer the handshake | handshake, name/MAC |
| other local processes -> bus.sock / api.sock | Unix sockets in the 0700 run dir; api.sock Status carries the API key | sim frames; API key |
| shared /tmp -> run-dir fallback (`/tmp/sil-$UID`) | other local users can pre-create paths in /tmp | run.json, socket paths, PIDs |
| identity / state dir at rest | identity.json holds the API key | API key (secret) |
| `sil down` fallback -> arbitrary PIDs | run.json PID may be stale, reused or planted | signals |
| CLI stdout -> consumers | agents/pytest parse stdout; status/up JSON carries the key by design | API key (by design) |
| Go module proxy -> go.mod/go.sum | third-party code enters the build | Go modules |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Evidence | Status |
|-----------|----------|-----------|----------|-------------|------------|----------|--------|
| T-01-01 | Tampering | instance.ValidateName / PathsFor | high | mitigate | Name regex before any path/argv use; exec with arg slices, never a shell | `internal/instance/instance.go:38,47,84` (regex, PathsFor validates first); all entry points go through PathsFor/ValidateName (`lifecycle.go:123,126,415,418`, `supervisor.go:136`, `cli/resolve.go:51,72,75`, `cli/cli.go:305`); every `exec.Command` takes an arg slice (`supervisor/firmware.go:74`, `lifecycle.go:288`, `fwbuild.go:221,229,306,343`); no `sh -c` in production code | closed |
| T-01-02 | Information disclosure | API key in fwbuild | high | mitigate | Key only via cmd.Env (`-e SIL_API_KEY` without value); never logged; build dirs 0700 | `fwbuild.go:307` (cmd.Env), `fwbuild.go:371` (`-e SIL_API_KEY` valueless), `fwbuild.go:107-108` (MkdirAll 0700), `fwbuild.go:115` (build.log 0600); test `fwbuild_test.go:146-149,175-177` asserts key absent from argv | closed |
| T-01-03 | Information disclosure | control.Serve (Status carries key) | high | mitigate | Control API only on api.sock, chmod 0600 in 0700 run dir; no TCP listener | `control/control.go:205-209` (ListenUnix + Chmod 0600); `supervisor.go:197` only listener; only TCP `net.Listen` is the transient TestBind (`instance/port.go:105-107`, closed immediately); run dir 0700 `instance.go:419-425` | closed |
| T-01-04 | Tampering | fwbuild.Image | medium | mitigate | Image by tag + sha256 digest; never pulled by sil or tests | `fwbuild.go:34` (tag@sha256 digest); preflight `docker image inspect` only, fails `image_missing` (`fwbuild.go:229-236`); tests `--pull=never` (`e2e/codegen_test.go:65`, `e2e/io_test.go:222`, `Makefile:43`), e2e preflight `e2e/harness_test.go:46`; `TestBuildImageMissingNeverPulls` | closed |
| T-01-05 | Information disclosure / Tampering | firmware native API on 0.0.0.0 | medium | mitigate | `api: encryption:` with per-instance 32-byte key mandatory; plaintext never shipped | `examples/fan-skeleton.yaml` (`encryption: key: !env_var SIL_API_KEY`); lint rejects missing key `fwbuild/lint.go:120-122`; 32 random bytes `instance.go:217-221`; probe flags `plaintext_api` `supervisor.go:456-457` | closed |
| T-01-06 | Denial of service | orphaned firmware after supervisor death | medium | mitigate | Setpgid + Pdeathsig SIGKILL from a LockOSThread goroutine; signal handler stops the firmware group first | `supervisor/firmware.go:68,78` (LockOSThread, `Setpgid: true, Pdeathsig: SIGKILL`); `supervisor.go:173` (NotifyContext SIGTERM/INT/HUP), `supervisor.go:216-220,374-381` (shutdown stops firmware before engine/run.json) | closed |
| T-01-07 | Tampering | fanlib.Load (fan.toml parameters) | medium | mitigate | Strict decode, required keys, range/order checks; invalid file stops `sil up` exit 1 | `fanlib/fanlib.go:142` (DisallowUnknownFields), `:187` (missing key), `:213-232` (finite/range/order); `lifecycle.go:145-146` (`fan_toml_invalid`, Failed = exit 1) before any write | closed |
| T-01-08 | Denial of service | fanlib reading SIL_FAN_DIR | low | mitigate | 64 KiB cap; only `<id>.toml` from validated id through fs.FS | `fanlib.go:37,132-137` (LimitReader 64 KiB+1), `fanlib.go:82,86` (id regex, rejects `..`), `fanlib.go:41-45,124` (os.DirFS / embed FS, `fsys.Open(id+".toml")`) | closed |
| T-01-09 | Denial of service | bus.Decode / proto::parse | medium | mitigate | 4096-byte cap before parse, no recursion (nesting = error), numbers <= 32 chars; decode error closes connection | Go: `bus/codec.go:250-251` (cap before parse), `:381` (ClassNested), `:188,427` (32-char numbers); C++: `sim_protocol.h:186` (cap), `:261` (NESTED), `:21,266` (32-char); close on error: `bus/server.go:248-256`, `sim_bus.cpp:175-179` | closed |
| T-01-10 | Tampering | parser differential Go vs C++ | medium | mitigate | One shared frames.tsv for both tests and both compilers | `internal/bus/codec_test.go:61` and `tests/cpp/test_protocol.cpp:3,157` read `protocol/vectors/frames.tsv`; `Makefile` `test-cpp` (host g++, in `check`) and `test-cpp-image` (image gcc, digest-pinned) | closed |
| T-01-11 | Denial of service | SimBus::loop rx buffer | medium | mitigate | Drop connection when >4096 bytes arrive without newline; non-blocking reads | `components/sim_bus/sim_bus.cpp:137-142` (over-long complete line), `:147-151` (no newline > MAX_LINE), `:132` (`MSG_DONTWAIT`), `:130` (64 reads per loop bound) | closed |
| T-01-12 | Spoofing | socket path truncation in sockaddr_un | medium | mitigate | validate_path rejects >107 bytes at config time | `components/sim_bus/__init__.py:15,19-29,47`; runtime `SIL_BUS` override (`sim_bus.cpp:25-26`, then `strncpy` `:48`) is length-checked on the sil side by `instance.CheckSocketPaths` (`instance.go:117-126`, called `lifecycle.go:130`) | closed |
| T-01-13 | Information disclosure | fwbuild stage outputs, build.log tail, errors | high | mitigate | Stage-1 stdout never logged; every message/tail redacted; key only in cmd.Env | `fwbuild.go:246-251` (stage-1 stdout to in-memory buffer only), `:273,298` (`cleanOutput(..., APIKey)` on tails), `:404-407` (ReplaceAll key -> `<redacted>`); `lifecycle.go:189,373-401` (`withLogTail`/`readTail` redact `<key>`); `TestBuildConfigFailureIsRedactedAndRepeatable` | closed |
| T-01-14 | Tampering / Elevation | docker mount scope for `--config` | medium | mitigate | Mount = YAML's git top-level (else its dir), read-only, same absolute path; never `/`; caller's uid | Re-verified 2026-11-09 after `12bedc2`: `fwbuild.go:343-358` (`mountRoot` picks RepoRoot / git top-level / dir, then runs `CheckMountRoot(root)` on the final root whichever branch chose it, `:354`), `:362-376` (`CheckMountRoot`/`broadMount`: `/` or `$HOME` after `filepath.Clean` -> Failed `config_invalid`); `b.mount` set only after the check (`:135-139`), `dockerArgs` has a single caller (`:310`); `:397` (`mount:mount:ro`), `:390` (`--user uid:gid`); outside-checkout fallback guarded too: `lifecycle.go:265-271` (`fwbuild.CheckMountRoot(root)` in `resolveConfig`). Tests: `TestMountRootRefusesBroadTrees` (`fwbuild_test.go:201`, YAML in `$HOME` and in `/`), `TestResolveConfigRefusesBroadMount` (`lifecycle_test.go:15`), both pass. End to end on a scratch build with a scratch `$HOME`: YAML directly in `$HOME` -> exit 1 `config_invalid`, both with no checkout (resolveConfig) and with `$HOME` a git repo and `SIL_REPO` set (mountRoot git branch, refused before any `docker run`). Residual: the check is lexical (WARNING below) | closed |
| T-01-15 | Tampering | image drift / toolcache poisoning | medium | mitigate | Digest + preflight, never pulled; image ID in build hash; toolcache written only after success via temp dir + rename in 0700 cache dir | `fwbuild/cache.go:57` (image ID in InputHash), `fwbuild.go:149-152`; write-back only after compile success `fwbuild.go:197-200`; `cache.go:336-349` (MkdirAll 0700, MkdirTemp, Rename, loser cleans up) | closed |
| T-01-16 | Spoofing | readiness (wrong process on API port) | high | mitigate | Noise handshake with instance PSK + name and MAC match (hello and HelloResponse); test-bind before build and spawn | `apiclient/probe.go:130-135` (Noise NN + `PresharedKey: key`), `:166` (server hello name/MAC), `:219` (HelloResponse); `supervisor.go:440-446,487-499` (`mismatch` checks ServerName, Name, MAC); test-bind `lifecycle.go:166,200` | closed |
| T-01-17 | Tampering / Elevation | `/tmp/sil-$UID` fallback (runtime base) | high | mitigate | Base must be owned by uid, mode 0700, else exit 2 `unsafe_runtime_dir`; never follow a foreign directory | Re-verified 2026-11-09 after `ce3bed4`. `sil up`: `instance.go:457-483` (`EnsureRunParent`/`ensurePrivateDir`: Mkdir 0700, Lstat, uid, perm) via `lifecycle.go:148`, before run.json is read (`:159`). Every client entry point now goes through the read-only `checkPrivateDir` (`cli/resolve.go:105-121`: Lstat, real dir, owner uid, `perm&0o077 == 0`): `status` / `sim restart` / `fw restart` via `connect` (`cli/cli.go:329`); `down <name>`, `SIL_INSTANCE`, name-less `down` via `DownCmd.Run` (`cli/cli.go:268`, before `lifecycle.Down` at `:271`); the name-less scan `resolve.go:62`. `lifecycle.Down` dials `p.APISock()` (`lifecycle.go:441`), and so does `checkNotRunning` (`:238`); no production code dials `rf.APISock` any more (grep). `cli.go:271` is the only production caller of `lifecycle.Down`. Tests: `TestUnsafeRuntimeBaseIsRefused` (`cli_test.go:473`, now also `down fan` and `down`), `TestDownDoesNotDialTheRunFileSocket` (`cli_test.go:493`), both pass. Re-ran the earlier reproduction on a scratch build of `12bedc2`: base 0755 -> `sil down fan`, `sil down` and `SIL_INSTANCE=fan sil down` all exit 2 `unsafe_runtime_dir`, the planted PID stays alive and the planted socket is never connected; a symlinked base -> exit 2; a private base whose run.json names a foreign socket -> that socket is never connected | closed |
| T-01-18 | Information disclosure | identity.json (API key) | high | mitigate | 0600 file in 0700 dir, tmp + rename; key never in errors, logs or argv | `instance.go:224,268-300` (MkdirAll+Chmod 0700, CreateTemp, Chmod 0600, Rename); test `instance_test.go:185-187`; firmware argv empty (`firmware.go:74`), serve argv `serve <name>` (`lifecycle.go:288`), docker argv keyless; probe errors carry no key (`probe.go:68`) | closed |
| T-01-19 | Denial of service | lock files | low | accept | flock released by the kernel on holder exit | Premise verified: `instance/lock.go:32,61` (`Flock LOCK_EX|LOCK_NB`), `:88` unlock. See Accepted Risks AR-01-01 | closed |
| T-01-20 | Denial of service | bus.Server (reconnect storms, slow/silent clients) | medium | mitigate | hello deadline 2 s, line cap, close on decode error, one active connection, firmware backoff 100 ms -> 2 s, firmware TX cap 64 KiB | `bus/server.go:19,242` (HelloTimeout 2 s read deadline), `:244` (Scanner cap MaxLine), `:248-256` (close on decode error), `:341-362` (newest replaces older), `:190` (write deadline); `sim_bus.h:37,40-41` (kMaxTx 65536, backoff 100/2000), `sim_bus.cpp:34-37,116-120` | closed |
| T-01-21 | Spoofing | bus.sock connection replacement | low | accept | Socket 0600 in user's 0700 run dir; manifest warnings expose a foreign peer | Premise verified: `bus/server.go:148` (0600), run dir 0700 `instance.go:419-425`; `engine/engine.go:304-306` (`checkManifest` -> warnings in Status). See AR-01-02 | closed |
| T-01-22 | Denial of service | `sil sim restart --gap` | low | mitigate | gap <= 30 s, restarts serialized by one mutex, status lock-free | `control/control.go:104,113-118` (Validate, NaN/neg/>30 rejected), route `:170`, supervisor `supervisor.go:665`; `opMu` `:669,750,538`; `Status` takes only `mu` `:570-577`; body capped 64 KiB `control.go:157` | closed |
| T-01-23 | Denial of service | firmware crash loop | medium | mitigate | CrashGuard stops after 5 unrequested exits in 30 s, instance failed | `supervisor/policy.go:13-14,53-66`; `supervisor.go:522-527` (`firmware_crash_loop`, no restart) | closed |
| T-01-24 | Tampering / Elevation | lifecycle.Down fallback signalling | high | mitigate | Signal only after /proc/<pid>/cmdline matches `sil serve <name>`; run.json PIDs never trusted alone | `lifecycle.go:473-474` (ProcessAlive gate before SIGTERM), `:479-480` (re-check before SIGKILL); `instance.go:360-380` (`kill(pid,0)` + cmdline `serve <name>`, plus, hardened in `56693c0`, argv[0] base name `sil` or `/proc/<pid>/exe` == `os.Executable()` via `sameExecutable` `:385-395`). These are still the only run.json-PID signal sites. Test `TestProcessAliveRequiresSil` (`instance_test.go:381`) passes; scratch reproduction: a `bash -c ... serve fan` process named in run.json is no longer signalled (`sil down fan` -> `stopped:false`, process alive). Hardening note confirmed (re-verify 2026-11-09). Residual: argv[0] is chosen by whoever execs the process (`exec -a sil`), so a same-user process can still pose as the supervisor; same-user is inside the trust boundary | closed |
| T-01-25 | Information disclosure | CLI output | medium | mitigate | Key only in status/up JSON on stdout; errors, stderr, --quiet never include it; run/state dirs 0700 | `cli/cli.go:216-223` (`fail` prints Code/Message only), error details built without Status (`supervisor.go:719-721,776-778`, `lifecycle.go:234-244`); test `cli_test.go:360,445` asserts key absent from stdout+stderr on error paths | closed |
| T-01-26 | Denial of service | bus.Server reader | medium | mitigate | Scanner capped at MaxLine; one active connection; socket chmod 0600 in 0700 run dir | `bus/server.go:243-244` (Buffer MaxLine), `:329-331` (ErrTooLong closes), `:347-362` (single active), `:148` (chmod 0600); run dir 0700 | closed |
| T-01-27 | Information disclosure | identity.json, lifecycle and supervisor logs | high | mitigate | identity.json 0600/0700 tmp + rename; key to fwbuild in Options and docker via cmd.Env only; firmware.log tail redacted | `instance.go:268-300`; `lifecycle.go:183` (Options.APIKey) -> `fwbuild.go:307`; `lifecycle.go:337-353` (`withLogTail(..., FirmwareLog/SilLog, key)`), `:388-401` (redaction); supervisor slog lines carry codes/messages only (`supervisor.go:420-422`) | closed |
| T-01-28 | Tampering | clock.Manual in production | low | mitigate | Manual only in _test.go; supervisor uses clock.NewReal() | `grep NewManual` outside `_test.go` and `internal/clock/` returns nothing (2026-11-09); `supervisor.go:158` (`clock.NewReal()`) | closed |
| T-01-29 | Tampering | exit-class drift | low | mitigate | Registry + AST consistency test in `make check` | `internal/apperr/codes_test.go:4-5,64-111,151,166` (go/ast scan, `TestRegistryMatchesCallSites`, `TestRegistryClasses`, `TestExitCodeFollowsRegistry`); `Makefile` `check: ... test` | closed |
| T-01-30 | Denial of service | control.Serve (slow or stalled client on api.sock) | low | accept | No http.Server timeout fields (wall-clock lint); api.sock 0600 in 0700 run dir; one goroutine per stalled conn | Premise verified: `control/control.go:218-221` (`&http.Server{Handler: h}` only, ReadHeaderTimeout deliberately absent), lint bans the fields `internal/lint/wallclock_test.go:47`; 0600 `control.go:209`; no TCP listener. See AR-01-03 | closed |
| T-01-SC | Tampering | Go modules (kong, go-toml, yaml/v3, flynn/noise, x/crypto, x/sys) | high | mitigate | Exact versions; GOSUMDB verified; Origin URLs checked and recorded; go.mod/go.sum unchanged after 01-02; pin file removed in 01-13 | `go.mod` exact versions; `go env GOSUMDB` = `sum.golang.org` (no GONOSUMDB/GOFLAGS overrides); `go mod verify` -> "all modules verified" (2026-11-09); Origin URLs recorded in `01-02-SUMMARY.md:135-149`; `git log -- go.mod go.sum` last change `09c47ef` (01-02); pin file removed `2d59536`. Human provenance review of flynn/noise and go.yaml.in/yaml/v3 still pending (declared backstop, not a gap) | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on (high) count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

### Open items

None (re-verify 2026-11-09). T-01-17 is closed by `ce3bed4` and T-01-14 by `12bedc2`; the T-01-24 hardening is `56693c0`. The remaining hardening notes are in the next section and are non-blocking.

---

## Unregistered Flags and Residual Warnings

| Flag | Source | Maps to | Assessment |
|------|--------|---------|------------|
| insecure_default_ipc_path (`sim_bus` `path:` default `/tmp/sil.sock`) | 01-01-SUMMARY; still present `components/sim_bus/__init__.py:47`; decision logged in `STATE.md:97`, open question in `01-07-SUMMARY.md:204` | none (T-01-12 covers truncation, T-01-21 covers same-user peers inside the 0700 run dir; neither covers a cross-user pre-created `/tmp` path) | **unregistered_flag (WARNING).** Not reachable on the supported path: the supervisor always sets `SIL_BUS=<0700 run dir>/bus.sock` (`supervisor.go:303`), the e2e harness sets it (`e2e/harness_test.go:236`), and the 01-07 follow-up to drop the default or require `path:` was not done. Hand-run firmware without `SIL_BUS` connects to a world-creatable path. Register as an accepted risk or make `path:` required. |
| first_build_network (first compile on `--network bridge`) | 01-08-SUMMARY; `fwbuild.go:191-195` | partial: T-01-15 (toolcache poisoning after fetch); T-01-SC covers Go modules only | **unregistered_flag (WARNING).** The integrity of the noise-c/libsodium archives fetched by PlatformIO on the first build is not covered by any registered threat; afterwards they are frozen in the 0700 toolcache. Register (likely transfer to PlatformIO registry checksums, or accept). |
| local_ipc_socket (chmod-after-listen window) | 01-02-SUMMARY | T-01-26 / T-01-03 | Informational. Both sockets are created inside the 0700 run dir (`instance.go:419-425`) under a verified private base (on `sil up`), so the umask window is not reachable by other users. |
| stale_run_json | 01-03-SUMMARY | T-01-24 / T-01-06 | Informational, resolved by 01-11: a dead/foreign PID is reported `stopped:false` and cleaned up (`lifecycle.go:430-432`). |
| vendor_cache_format (`freezeHTTPCache`) | 01-08-SUMMARY | T-01-15 | Informational. Safe while the image is digest-pinned; bump `RecipeVersion` with any image bump. |
| T-01-24 residual: weak cmdline match | auditor, 2026-11-09 | T-01-24 | **Resolved by `56693c0`** (re-verify 2026-11-09): `instance.ProcessAlive` (`instance.go:360-380`) also requires argv[0] base name `sil` or `/proc/<pid>/exe` == `os.Executable()` (`:385-395`); the non-sil `serve fan` process from the earlier reproduction is no longer signalled. Remaining: argv[0] can be set by the process launcher (`exec -a sil`), so a same-user process can still pose as the supervisor (inside the same-user trust boundary). Informational. |
| `checkNotRunning` dials `rf.APISock` | auditor, 2026-11-09 | T-01-17 | **Resolved by `ce3bed4`**: `checkNotRunning` now dials `p.APISock()` (`lifecycle.go:238`). Informational. |
| T-01-17 residual: missing base, check-then-use | auditor, re-verify 2026-11-09 | T-01-17 | Informational. `checkPrivateDir` accepts a base that does not exist (`resolve.go:107-108`). `lifecycle.Down` works out the paths again and reads run.json afterwards (`lifecycle.go:421-425`) with no second check. With `XDG_RUNTIME_DIR` unset, another user who creates `/tmp/sil-$UID` in exactly that window could steer one `down` call to a socket they own. The impact is limited: the dial target is always `<base>/<name>/api.sock`, and signals reach only a PID that passes the hardened `ProcessAlive` and that the caller may signal (other users' processes return EPERM). `connect` (status, restarts) has the same pattern. Hardening: put the gate inside the exported `lifecycle.Down` (today it has none; the CLI applies it at `cli.go:268`), or Lstat the base again after reading run.json. |
| T-01-14 residual: lexical mount check | auditor, re-verify 2026-11-09 | T-01-14 | **WARNING (non-blocking).** `broadMount` compares `filepath.Clean` strings (`fwbuild.go:373-376`), and fwbuild and lifecycle never call `EvalSymlinks`. A `--config` reached through a symlinked directory that points at `/` or `$HOME` (e.g. `~/link -> ~`, config `~/link/fw.yaml` outside any checkout) passes the check, and docker follows the symlink when bind-mounting. The path is chosen by the caller and the mount stays read-only with the caller's uid. Derived from the code, not reproduced, because reproducing it would start a real build. Hardening: `filepath.EvalSymlinks` on the root before `CheckMountRoot`. |

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01-01 | T-01-19 (low) | flock locks are released by the kernel when the holder exits; a stuck holder is a live sil process the user can see and stop. Premise verified: `instance/lock.go:32,61` | Plan 01-09 threat model (planner); premise re-verified by gsd-security-auditor | 2026-11-09 |
| AR-01-02 | T-01-21 (low) | bus.sock lives in the user's 0700 run dir with mode 0600; only same-user processes (already trusted on a local bench) can replace the firmware connection; manifest warnings make a foreign peer visible. Premise verified: `bus/server.go:148`, `instance.go:419-425`, `engine/engine.go:304-306` | Plan 01-10 threat model (planner); premise re-verified by gsd-security-auditor | 2026-11-09 |
| AR-01-03 | T-01-30 (low) | The control `http.Server` has no Read/ReadHeader/Write/Idle timeouts because they arm wall-clock timers the MODEL-04 lint bans; api.sock is 0600 inside the user's 0700 run dir, so only same-user processes can connect, a stalled connection costs one goroutine, and status stays answerable for other connections. Revisit when the Phase 3 TCP listener (A3) lands: a clock-driven connection guard becomes necessary there. Premise verified: `control/control.go:209,218-221`, `internal/lint/wallclock_test.go:47` | Plan 01-03 threat model (planner); premise re-verified by gsd-security-auditor | 2026-11-09 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-09 | 31 | 29 | 2 (1 blocking: T-01-17; 1 non-blocking: T-01-14) | gsd-security-auditor (Claude Opus 5.5), ASVS L1, block_on high |
| 2026-11-09 (re-verify after fix) | 31 | 31 | 0 | gsd-security-auditor (Claude Opus 5.5), ASVS L1, block_on high; re-verified T-01-17, T-01-14 and the T-01-24 note against `ce3bed4`, `56693c0`, `12bedc2` |

### Security Audit 2026-11-09 (re-verify after fix)

| Metric | Value |
|--------|-------|
| Scope | Re-verified T-01-17 (high) and T-01-14 (medium); confirmed the T-01-24 hardening note. The other 28 entries are carried over as recorded |
| Fix commits (`claude/main`) | `ce3bed4` (T-01-17), `56693c0` (T-01-24 hardening), `12bedc2` (T-01-14); no uncommitted implementation changes |
| Threats total | 31 |
| Closed | 31 |
| Open, blocking (>= high) | 0 |
| Open, non-blocking | 0 |
| threats_open | 0 |
| Unregistered flags | 2, carried over unchanged (insecure_default_ipc_path, first_build_network) |
| New residual notes | 2 (T-01-17 missing-base check-then-use: informational; T-01-14 lexical mount check: WARNING), both non-blocking |
| Tests | `go vet` and `go test -count=1` for `internal/cli`, `internal/instance`, `internal/lifecycle`, `internal/fwbuild`: all ok; the targeted regressions pass (`TestUnsafeRuntimeBaseIsRefused`, `TestDownDoesNotDialTheRunFileSocket`, `TestRunFileDoesNotChooseTheSocket`, `TestProcessAliveRequiresSil`, `TestMountRoot`, `TestMountRootRefusesBroadTrees`, `TestResolveConfigRefusesBroadMount`) |
| Reproduction | Scratch binary built from `12bedc2` outside the repo, scratch XDG/HOME dirs. Base 0755 with a planted run.json (sil-named PID, foreign socket): all three `down` forms exit 2 `unsafe_runtime_dir`, PID alive, socket never connected. Symlinked base: exit 2. Private base with a foreign socket in run.json: never connected. Non-sil `serve fan` PID: not signalled. YAML directly in `$HOME`: `config_invalid` on both the resolveConfig and the mountRoot paths |
| Note | Line references in the carried-over rows predate the fix commits. Lines after the edited functions have moved: by 3 to 5 in `lifecycle.go` and `instance.go` (e.g. T-01-01 `lifecycle.go:415,418` is now `:418,421`), by 4 after `run()` and by about 25 after `mountRoot` in `fwbuild.go` (e.g. T-01-02 `fwbuild.go:371` `-e SIL_API_KEY` is now `:396`); the cited code itself is unchanged |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed (re-verify 2026-11-09)
- [ ] `status: verified` set in frontmatter

**Approval:** pending
