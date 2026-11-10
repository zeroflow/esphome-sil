# Phase 4: Fan Model v2 - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-11-10
**Phase:** 04-fan-model-v2
**Areas discussed:** Shipped library migration, fan.toml provenance syntax, Start/stop/coast semantics, RPM vs tach readout

---

## Shipped library migration

| Option | Description | Selected |
|--------|-------------|----------|
| Migrate both now | Schema 2 with assumed values; v0.1.0 files kept as schema 1 fixtures | ✓ |
| Keep shipped files schema 1 | Library unchanged until Phase 8 | |
| Add v2 siblings | Parallel `-v2` files, default stays v1 | |

| Option (exact-RPM tests) | Description | Selected |
|--------|-------------|----------|
| Ideal test fan via SIL_FAN_DIR | Tests pin an ideal fan; new tests for v2 fans | ✓ |
| Rewrite assertions to tolerance | Keep default fan, compare within bands | |
| You decide | | |

| Option (unsourced values) | Description | Selected |
|--------|-------------|----------|
| Plausible assumed values | Hysteresis, ~0.4 s delay, few rpm noise, marked assumed | ✓ (after follow-up) |
| Neutral values | Down = up, no delay, noise 0 | |

**User's choice:** First answered "why not measure?". Claude explained that the measuring tooling arrives in Phases 6-7 and that Phase 8 measures the Arctic properly; the user then chose plausible assumed values.

| Option (datasheet numbers) | Description | Selected |
|--------|-------------|----------|
| Fetch vendor pages in research | URLs recorded, unconfirmed values marked assumed, SKU settled | ✓ |
| Use research summary values | | |

---

## fan.toml provenance syntax

| Option | Description | Selected |
|--------|-------------|----------|
| Parallel [provenance] table | Dotted keys, plain values | ✓ |
| Inline per value | `{ v = ..., src = ... }` | |
| Per table | One provenance key per table | |

| Option | Selected |
|--------|----------|
| Required for every value | ✓ |
| Omitted means assumed | |

| Option (sources) | Selected |
|--------|----------|
| [sources] table with ids | ✓ |
| Keep one `source` string | |

| Option (`sil fans`) | Selected |
|--------|----------|
| Summary by default, full with --verbose | ✓ |
| Always full per-value list | |

---

## Start/stop/coast semantics

| Option (kick) | Selected |
|--------|----------|
| Fan-internal start boost | |
| Required minimum drive to start | |
| No kick in v2 | ✓ |

The kick conflicted with MODEL-06 and success criterion 2. Options: amend the requirement and defer the kick (✓), or keep a minimal optional kick.

| Option (coast) | Selected |
|--------|----------|
| Own coast_tau_s, required | ✓ |
| Optional, falls back to tau_down | |

| Option (never stops) | Selected |
|--------|----------|
| stop_duty = 0 + curve point at duty 0 | ✓ |
| Explicit never_stops = true | |

| Option (restart while coasting) | Selected |
|--------|----------|
| Restart after start delay | ✓ |
| Catch immediately while coasting | |

---

## RPM vs tach readout

| Question | Choice |
|----------|--------|
| Status/watch | Both `rpm` (shaft) and `tach_rpm` ✓ (alternative: only firmware-seen) |
| Scope | Two traces, tach stepped ✓ (alternatives: shaft only, tach only) |
| Seed | Fixed default, `sil up --seed` overrides ✓ (alternative: random per run, printed) |
| No datasheet | null / — ✓ (alternative: omit fields) |

## Claude's Discretion

Curve interpolation, exact key names and layout, airflow/power exponents, exact assumed numbers, timing of `fanlib.Marshal`.

## Deferred Ideas

- Start kick (revisit after Phase 8 data)
- Catching a coasting rotor without a start delay
