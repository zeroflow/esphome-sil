---
status: testing
phase: 04-fan-model-v2
source: [04-VERIFICATION.md]
started: 2026-11-11T14:40:00Z
updated: 2026-11-11T14:40:00Z
---

## Current Test

number: 1
name: Web page look of the stepped tach trace and the load readout row (D5)
expected: |
  Per port scope: solid smooth shaft RPM trace plus a dimmer stepped tach trace in the same port colour;
  the readout row shows TACH RPM / M3/H / W without crowding the existing readouts, and shows an em dash for null values.
awaiting: user response

## Tests

### 1. Web page look of the stepped tach trace and the load readout row (D5)
how: `sil up`, open the printed URL with the shipped Arctic fan, sweep Fan 1 up and down
expected: Per port scope: solid smooth shaft RPM trace plus a dimmer stepped tach trace in the same port colour; the readout row shows TACH RPM / M3/H / W without crowding the existing readouts, and shows an em dash for null values
result: [pending]

### 2. Plausibility of the assumed values in the shipped fan files
how: Read fans/arctic-p12-pwm-pst.toml and fans/noctua-nf-a12x25-pwm.toml against the vendor pages
expected: Values labelled `assumed` (interior curve points, down curve, start delay 0.4 s, tau values, coast_tau 6 s, noise_rpm 20, Noctua 450 rpm floor) look plausible enough to ship until Phase 8 measures them; `datasheet` entries are backed by the recorded sources
result: [pending]

### 3. README readability
how: Skim README.md sections on schema 2, provenance, tach, airflow/power and --seed
expected: Reads clearly to a developer and matches `sil fans`, `sil status` and `sil watch` output
result: [pending]

### 4. Disposition of code review findings (WR-01..03, IN-01..03)
how: Decide in 04-REVIEW-DISPOSITION.md; WR-01 (never-stops floor validated on curve.up only) is recommended to fix before Phase 6 `fit` writes schema 2 files
expected: Maintainer disposition recorded for all six findings (currently all `open`)
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
