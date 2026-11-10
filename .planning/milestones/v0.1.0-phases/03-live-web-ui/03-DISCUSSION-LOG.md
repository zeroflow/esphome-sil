# Phase 3: Live Web UI - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-11-08
**Phase:** 03-live-web-ui
**Areas discussed:** Visual language (user-proposed), Mobile vs PC (user-proposed), Reaching the page, Design type (user-proposed), Plots, Buttons & inputs

Initial area menu offered: Reaching the page, Layout & plots, Buttons & inputs, Parity check. User answered with free text "Visual language".

---

## Visual language

| Option | Description | Selected |
|--------|-------------|----------|
| Lab instrument | Dark panels, monospace numerals, strong trace colors, little chrome | ✓ |
| Clean dashboard | Neutral light card-based admin look | |
| Board view | Schematic Rev 3.3 PCB with readouts on headers | |

| Option | Description | Selected |
|--------|-------------|----------|
| Dark only | One tuned palette | ✓ |
| Follow the system | Dark + light via prefers-color-scheme | |
| Dark + manual toggle | Toggle stored in localStorage | |

| Option | Description | Selected |
|--------|-------------|----------|
| Glowing dots + hex | Glow in strip color, segment name + #rrggbb | ✓ |
| Glowing dots only | Pure visual | |
| Flat swatches + hex | Square chips | |

| Option | Description | Selected |
|--------|-------------|----------|
| One screen, no scroll | Front-panel layout on 1080p | ✓ |
| Roomy, scrolls | Bigger plots, stacked | |
| Plots dominate | One big shared plot + side rail | |

**Notes:** At the "more questions / next" check the user answered free text "Mobile vs PC".

## Mobile vs PC

| Option | Description | Selected |
|--------|-------------|----------|
| PC first, phone usable | Desktop no-scroll; single column + scroll below, 44 px targets | ✓ |
| Both first-class | Purpose-built phone layout | |
| PC only | Fixed desktop layout | |

## Reaching the page

| Option | Description | Selected |
|--------|-------------|----------|
| 127.0.0.1, --listen opt-in | Local default, LAN by flag | |
| LAN by default | Bind all interfaces always | ✓ |
| Always local, use SSH/tunnel | Never beyond 127.0.0.1 | |

| Option | Description | Selected |
|--------|-------------|----------|
| Per-instance token in URL | Capability URL, cookie, CLI header | |
| Read-only without token | Watch open, control needs token | |
| No protection (trusted LAN) | Origin check only | ✓ |

| Option | Description | Selected |
|--------|-------------|----------|
| Sticky web port per instance | Stored like the API port | ✓ |
| Ephemeral port, printed | New port per up | |
| Shared landing page | Needs always-on daemon | |

| Option | Description | Selected |
|--------|-------------|----------|
| Auto-reconnect, stale overlay | Retry with backoff, snapshot + history on reconnect | ✓ |
| Banner + manual reload | Simpler | |

**Notes:** The user accepted an unauthenticated control API on the LAN. Origin check kept.

## Design type (user-proposed refinement of visual language)

User free text: "Design type (less modern cyberpunk, more classic HP/Keysight)".

| Option | Description | Selected |
|--------|-------------|----------|
| Modern Keysight scope | Grey bezel, black graticule screen, yellow/green/blue/pink channels, softkeys | ✓ |
| Vintage HP (70s-90s) | Beige panels, 7-segment / phosphor readouts | |
| Blend | Modern layout with segment-style numerals | |

## Plots

| Option | Description | Selected |
|--------|-------------|----------|
| 4 small screens, one per port | Channel color per port, RPM solid + duty dashed | ✓ |
| One shared scope screen | All RPM traces, channel toggles | |
| Both: shared + per-port | Most info, hardest fit | |

| Option | Description | Selected |
|--------|-------------|----------|
| 60 s window, server history | ~5 min server ring via /api/v1/history | ✓ |
| Selectable window | Timebase softkeys | |
| 60 s, no server history | Reload starts empty | |

| Option | Description | Selected |
|--------|-------------|----------|
| Fixed to the fan's max_rpm | Like fixed V/div | ✓ |
| Autoscale | Fits visible data | |

## Buttons & inputs

| Option | Description | Selected |
|--------|-------------|----------|
| Two softkeys + hold | Presets + hold on cap | |
| Two softkeys only | Presets only | |
| Real press-and-hold only | down/up from pointer | ✓ |

| Option | Description | Selected |
|--------|-------------|----------|
| Release on cancel + server timeout | Client cancel events + API max_hold | ✓ |
| Release on cancel only | Client only | |
| Tie hold to WS connection | Per-connection ownership | |

| Option | Description | Selected |
|--------|-------------|----------|
| Entry field + apply, fan dropdown | One set per commit | ✓ |
| Knob/slider, live | Throttled stream | |
| Fields + step softkeys | ±step | |

| Option | Description | Selected |
|--------|-------------|----------|
| Status line message | Header message line + red outline | ✓ |
| Inline per control | Under the control | |

## Claude's Discretion

- Parity check mechanism (UI-06): user chose to write context without discussing it.
- Behind-real-time threshold, palette values, gamma, max-hold value, history length/rate, WS coalescing, Alpine vs vanilla.

## Deferred Ideas

- Timebase softkeys; shared comparison screen (P3).
- Per-instance token if the trusted-LAN assumption changes.
- Multi-instance landing page (needs a daemon; RUN-03 later).
- Light theme.
