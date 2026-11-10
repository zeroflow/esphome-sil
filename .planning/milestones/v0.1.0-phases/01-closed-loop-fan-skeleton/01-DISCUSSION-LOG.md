# Phase 1: Closed-Loop Fan Skeleton - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-11-08
**Phase:** 01-closed-loop-fan-skeleton
**Areas discussed:** Run lifecycle, Instance identity, Fan model & shipped fan, Build experience

---

## Run lifecycle

| Option | Description | Selected |
|--------|-------------|----------|
| Detach | Background per-instance process, wait for handshake, JSON, exit 0 | ✓ |
| Foreground | Stays attached, Ctrl-C = down | |
| Both, detach default | `--foreground` flag | |

| Option | Description | Selected |
|--------|-------------|----------|
| Supervisor + sim split | Supervisor owns firmware, engine restartable (`sil sim restart`) | ✓ |
| Attach mode only | Sim restart only via `--no-firmware` | |
| Firmware grace timeout | Firmware outlives dead sim for N s | |

| Option | Description | Selected |
|--------|-------------|----------|
| Auto-restart, count it | Same prefs, count + reason, crash-loop guard | ✓ |
| Stop and mark failed | No auto-restart | |

| Option | Description | Selected |
|--------|-------------|----------|
| Progress on stderr, JSON on stdout | `--quiet`, full log in run dir | ✓ |
| Stream full ESPHome output | Always pass through | |

| Option | Description | Selected |
|--------|-------------|----------|
| Restart only, no rebuild | `sil fw restart`; RUN-02 stays v2 | ✓ |
| Restart + rebuild now | Pull RUN-02 forward | |
| Neither | Signals in tests only | |

---

## Instance identity

| Option | Description | Selected |
|--------|-------------|----------|
| XDG split | Identity in XDG_STATE_HOME, run dir in XDG_RUNTIME_DIR | ✓ |
| Project-local .sil/ | Everything in working tree | |
| Hybrid | Identity in `.sil/`, run dir in XDG_RUNTIME_DIR | |

| Option | Description | Selected |
|--------|-------------|----------|
| Auto, then sticky | Free port from 6054, stored, `--api-port` override | ✓ |
| Always explicit or 6053 | | |
| Derived from name hash | | |

| Option | Description | Selected |
|--------|-------------|----------|
| MAC derived, key stored | `02:`+hash(name); random key 0600 | ✓ |
| Both derived from name | HMAC with user secret | |
| Both stored | | |

| Option | Description | Selected |
|--------|-------------|----------|
| sil-<name> | Prefix avoids production collisions | ✓ |
| Exactly <name> | | |

---

## Fan model & shipped fan

| Option | Description | Selected |
|--------|-------------|----------|
| Arctic P12 PWM PST | Stops at low duty, ~200-1800 RPM | ✓ |
| Noctua NF-A12x25 PWM | Never stops | |
| Generic placeholder | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Per-fan parameter | `below_min = stop/hold` | ✓ |
| Always stop | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, exactly that | start_duty to start, min_duty to stall, state kept between | ✓ |
| Different | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Separate spin-up / spin-down | `tau_up`, `tau_down` | ✓ |
| Single tau | | |

---

## Build experience

| Option | Description | Selected |
|--------|-------------|----------|
| Skip when unchanged | Input hash, `--rebuild` forces | ✓ |
| Always build | | |

| Option | Description | Selected |
|--------|-------------|----------|
| XDG cache per instance | `$XDG_CACHE_HOME/sil/build/<name>/` | ✓ |
| In the working tree | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Example default, path optional | `--config` for others | ✓ |
| Config always required | | |

| Option | Description | Selected |
|--------|-------------|----------|
| After `esphome config` | Lint resolved YAML in Go | ✓ |
| Go pre-parse of raw YAML | | |

---

## Claude's Discretion

- Supervisor/engine separation mechanism, JSON shapes, Go package layout, restart-matrix test approach
- Encrypted handshake approach for the ready check
- Reuse vs rewrite of prototype components

## Deferred Ideas

- `sil rebuild` / `fw restart --rebuild` (RUN-02, v2)
- `sil down --forget`
- Second shipped fan with `below_min = "hold"`
