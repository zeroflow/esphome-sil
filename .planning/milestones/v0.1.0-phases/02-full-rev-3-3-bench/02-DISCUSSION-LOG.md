# Phase 2: Full Rev 3.3 Bench - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-11-08
**Phase:** 02-full-rev-3-3-bench
**Areas discussed:** Button press model, LED state in status/watch, set/watch vocabulary, Example logic + test harness

---

## Button press model

| Question | Options | Selected |
|----------|---------|----------|
| Short/long hold time | 200 ms / 1500 ms; 100 ms / 1000 ms; only `--hold <dur>` | 200 ms / 1500 ms (+ `--hold`) |
| Blocking | Block until release; return immediately; block with `--async` | Block until release |
| Raw down/up | Yes `--down/--up`; no, presets only | Yes |
| Overlapping press | Reject exit 1 + JSON error; queue; you decide | Reject |

## LED state in status/watch

| Question | Options | Selected |
|----------|---------|----------|
| Status shape | Raw strip + named segments; raw strip only; per-partition objects (rgb/brightness/effect) | Raw strip + named segments |
| Segment-name source | Firmware HELLO manifest; simulator-side config; you decide | Firmware manifest |
| Bus frame rate | 20 Hz on change; 10 Hz; configurable default 20 Hz | 20 Hz on change |
| Watch LED frames | Every bus frame; throttle ~2 Hz; opt-in filter | Every bus frame |

## set/watch vocabulary

| Question | Options | Selected |
|----------|---------|----------|
| `sil set` syntax | key=value pairs (atomic); subcommands | key=value pairs |
| Fan swap while running | Instant unplug/replug; keep current RPM; only while stopped | Instant unplug/replug |
| Temp/humidity | Constant pushed periodically; constant + noise; ramp support | Constant, pushed periodically |
| Watch shape | Typed change events + leading snapshot; full snapshot per tick; JSON-patch diffs | Typed change events |

**Notes:** User moved on without detailing logs format, watch filters, persistence across sim restart → Claude's discretion.

## Example logic + test harness

| Question | Options | Selected |
|----------|---------|----------|
| Firmware logic | Thin product-like logic; pure IO; pull in product modules | Thin product-like logic |
| Test home | pytest in `tests/` via `make test-contract`; Go tests with build tag; split Go + pytest | pytest + `make test-contract` |
| HA check | Manual UAT checklist; throwaway HA container | Manual UAT checklist |
| Budget | < 60 s warm; no target | < 60 s warm |

## Claude's Discretion

- `sil logs` format/options; `sil watch` filters and exit behaviour
- Persistence of `sil set` values across sim/fw restart
- Fan choice at `sil up` and fan library lookup
- Example file name/location; exact JSON shapes

## Deferred Ideas

- Temperature ramps and sensor noise (scenarios / thermal model)
- Automated HA via throwaway container
- Real product modules (REAL-01)
- Second shipped fan with `below_min = "hold"` (optional in this phase)
